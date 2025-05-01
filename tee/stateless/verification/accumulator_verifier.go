// Package verification provides verification components for stateless blockchain
package verification

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"
	
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	
	"github.com/rhombus-tech/vm/coordination"
	"github.com/rhombus-tech/vm/tee/stateless/core"
	"github.com/rhombus-tech/vm/tee/stateless/proofs"
)

var (
	// ErrAccumulatorVerification indicates accumulator verification failed
	ErrAccumulatorVerification = errors.New("accumulator verification failed")
	
	// ErrAccumulatorUnavailable indicates accumulator service is unavailable
	ErrAccumulatorUnavailable = errors.New("accumulator service unavailable")
)

// AccumulatorVerifier uses the RSA accumulator for efficient stateless verification
type AccumulatorVerifier struct {
	verifier *Verifier
	client   *coordination.AccumulatorClient
	sgxTEE   string
	sevTEE   string
	log      logging.Logger
	crossTEEValidation bool // Whether to require both TEE types to validate
}

// NewAccumulatorVerifier creates a new accumulator verifier
func NewAccumulatorVerifier(
	verifier *Verifier,
	sgxEndpoint string,
	sevEndpoint string,
	log logging.Logger,
	crossTEEValidation bool,
) (*AccumulatorVerifier, error) {
	// Add HTTP scheme if not present for compatibility with TEE config format
	if sgxEndpoint != "" && !strings.HasPrefix(sgxEndpoint, "http") {
		sgxEndpoint = "http://" + sgxEndpoint
	}
	if sevEndpoint != "" && !strings.HasPrefix(sevEndpoint, "http") {
		sevEndpoint = "http://" + sevEndpoint
	}
	
	log.Debug(fmt.Sprintf("Initializing accumulator verifier with endpoints: SGX=%s, SEV=%s, crossValidation=%v", 
		sgxEndpoint, sevEndpoint, crossTEEValidation))
	
	// Create accumulator client with cross-validation as configured
	client := coordination.NewAccumulatorClient(sgxEndpoint, sevEndpoint, crossTEEValidation)
	
	// Check if accumulator is available with a reasonable timeout
	healthCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	checkHealth := make(chan bool, 1)
	checkErr := make(chan error, 1)
	
	go func() {
		healthy, err := client.HealthCheck()
		checkHealth <- healthy
		if err != nil {
			checkErr <- err
		} else {
			checkErr <- nil
		}
	}()
	
	select {
	case <-healthCtx.Done():
		return nil, fmt.Errorf("%w: health check timed out", ErrAccumulatorUnavailable)
	case healthy := <-checkHealth:
		err := <-checkErr
		if err != nil || !healthy {
			return nil, fmt.Errorf("%w: %v", ErrAccumulatorUnavailable, err)
		}
	}
	
	return &AccumulatorVerifier{
		verifier: verifier,
		client:   client,
		sgxTEE:   "sgx",
		sevTEE:   "sev",
		log:      log,
		crossTEEValidation: crossTEEValidation,
	}, nil
}

// VerifyProof verifies a proof using the TEE accumulator
func (av *AccumulatorVerifier) VerifyProof(
	ctx context.Context,
	proof core.StatelessProof,
) (bool, error) {
	// First verify using the standard verifier
	valid, err := av.verifier.VerifyProof(ctx, proof)
	if err != nil || !valid {
		return false, fmt.Errorf("standard verification failed: %w", err)
	}
	
	// If it passes standard verification, submit to accumulator
	// for cryptographic commitment
	switch p := proof.(type) {
	case *proofs.StateProof:
		return av.verifyStateProof(ctx, p)
	case *proofs.ExecutionProof:
		return av.verifyExecutionProof(ctx, p)
	default:
		// If we don't have a specific accumulator handler, we consider it valid
		// if it passed the standard verification
		return true, nil
	}
}

// verifyStateProof verifies a state proof using the accumulator
func (av *AccumulatorVerifier) verifyStateProof(
	ctx context.Context,
	proof *proofs.StateProof,
) (bool, error) {
	// Serialize proof for accumulator
	serialized, err := proof.Serialize()
	if err != nil {
		return false, fmt.Errorf("failed to serialize proof: %w", err)
	}
	
	// Submit to accumulator
	valid, format, err := av.client.ValidateDualFormatParameter(serialized)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrAccumulatorVerification, err)
	}
	
	if !valid {
		return false, ErrAccumulatorVerification
	}
	
	av.log.Debug(fmt.Sprintf("Accumulator verified state proof: proofType=%s fromRoot=%x toRoot=%x format=%s",
		proof.ProofType(), proof.FromRoot, proof.ToRoot, format))
	
	return true, nil
}

// verifyExecutionProof verifies an execution proof using the accumulator
func (av *AccumulatorVerifier) verifyExecutionProof(
	ctx context.Context,
	proof *proofs.ExecutionProof,
) (bool, error) {
	// Serialize proof for accumulator
	serialized, err := proof.Serialize()
	if err != nil {
		return false, fmt.Errorf("failed to serialize proof: %w", err)
	}
	
	// Submit to accumulator
	valid, format, err := av.client.ValidateDualFormatParameter(serialized)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrAccumulatorVerification, err)
	}
	
	if !valid {
		return false, ErrAccumulatorVerification
	}
	
	av.log.Debug(fmt.Sprintf("Accumulator verified execution proof: proofType=%s txID=%s stateRoot=%x format=%s",
		proof.ProofType(), proof.TxID.String(), proof.StateRoot, format))
	
	return true, nil
}

// VerifyBatch verifies a batch of proofs using the accumulator
func (av *AccumulatorVerifier) VerifyBatch(
	ctx context.Context,
	proofs []core.StatelessProof,
) (bool, error) {
	// Check context for cancellation
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
		// Continue processing
	}
	
	if len(proofs) == 0 {
		return true, nil
	}
	
	// First, verify each proof individually with the standard verifier
	for i, proof := range proofs {
		valid, err := av.verifier.VerifyProof(ctx, proof)
		if err != nil || !valid {
			return false, fmt.Errorf("proof %d failed standard verification: %w", i, err)
		}
	}
	
	// Now we'll prepare a batch using length-prefixed format for optimal
	// compatibility with the accumulator
	// This follows our dual-format parameter validation approach
	
	// First we serialize all the proofs
	serializedProofs := make([][]byte, len(proofs))
	for i, proof := range proofs {
		var err error
		serializedProofs[i], err = serializeProof(proof)
		if err != nil {
			return false, fmt.Errorf("failed to serialize proof %d: %w", i, err)
		}
	}
	
	// Prepare the batch header
	batchID := ids.GenerateTestID()
	buf := bytes.NewBuffer(nil)
	
	// Add batch ID to the buffer (32 bytes)
	buf.Write(batchID[:])
	
	// Add timestamp (8 bytes)
	timestampBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(timestampBytes, uint64(time.Now().UnixNano()))
	buf.Write(timestampBytes)
	
	// Add number of proofs (4 bytes)
	countBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(countBytes, uint32(len(proofs)))
	buf.Write(countBytes)
	
	// Add each proof hash (instead of the full proofs to save space)
	for _, serialized := range serializedProofs {
		proofHash := sha256.Sum256(serialized)
		buf.Write(proofHash[:])
	}
	
	// Prepare the full batch parameter in length-prefixed format
	batchParamData := buf.Bytes()
	batchParam := make([]byte, 4+len(batchParamData))
	
	// Add length prefix (4 bytes)
	binary.LittleEndian.PutUint32(batchParam[:4], uint32(len(batchParamData)))
	
	// Add the actual data
	copy(batchParam[4:], batchParamData)
	
	// Submit the batch to the accumulator
	valid, format, err := av.client.ValidateDualFormatParameter(batchParam)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrAccumulatorVerification, err)
	}
	
	if !valid {
		return false, ErrAccumulatorVerification
	}
	
	av.log.Debug(fmt.Sprintf("Accumulator verified batch: batchSize=%d batchID=%s format=%s",
		len(proofs), batchID.String(), format))
	
	return true, nil
}

// serializeProof serializes a proof for accumulator submission
func serializeProof(proof core.StatelessProof) ([]byte, error) {
	switch p := proof.(type) {
	case *proofs.StateProof:
		return p.Serialize()
	case *proofs.ExecutionProof:
		return p.Serialize()
	default:
		return nil, fmt.Errorf("unsupported proof type: %T", proof)
	}
}

// GetAccumulatorStats returns statistics from the accumulator
func (av *AccumulatorVerifier) GetAccumulatorStats() map[string]interface{} {
	return av.client.GetStats()
}
