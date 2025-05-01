// Package verification provides verification capabilities for stateless blockchain
package verification

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	
	// Local package references 
	"github.com/rhombus-tech/vm/tee/stateless/core"
	"github.com/rhombus-tech/vm/tee/stateless/proofs"
)

var (
	// ErrUnknownProofType indicates an unknown proof type
	ErrUnknownProofType = errors.New("unknown proof type")
	
	// ErrInvalidProof indicates a general proof validation failure
	ErrInvalidProof = errors.New("invalid proof")
)

// Mock attestation service interface - simplified for this implementation
type AttestationService interface {
	VerifyAttestation(attestation []byte) (bool, error)
}

// Verifier implements the StatelessVerifier interface
type Verifier struct {
	attestationSvc AttestationService
	
	// Registry of proof verifiers by type
	verifiers      map[string]func(context.Context, []byte) (bool, error)
	verifiersMutex sync.RWMutex
	
	// Cache of verified proofs to avoid redundant verification
	verifiedCache      map[[sha256.Size]byte]bool
	verifiedCacheMutex sync.RWMutex
}

// NewVerifier creates a new stateless verifier
func NewVerifier(attestationSvc AttestationService) (*Verifier, error) {
	v := &Verifier{
		attestationSvc: attestationSvc,
		verifiers:      make(map[string]func(context.Context, []byte) (bool, error)),
		verifiedCache:  make(map[[sha256.Size]byte]bool),
	}
	
	// Register the built-in state proof verifier
	err := v.RegisterProofType(proofs.StateProofType, v.verifyStateProof)
	if err != nil {
		return nil, err
	}
	
	return v, nil
}

// VerifyProof checks if a proof is valid without requiring state access
func (v *Verifier) VerifyProof(ctx context.Context, proof core.StatelessProof) (bool, error) {
	// Check if we've already verified this proof
	rootHash := proof.RootHash()
	
	v.verifiedCacheMutex.RLock()
	if verified, exists := v.verifiedCache[rootHash]; exists {
		v.verifiedCacheMutex.RUnlock()
		return verified, nil
	}
	v.verifiedCacheMutex.RUnlock()
	
	// Verify the proof based on its type
	proofType := proof.ProofType()
	
	v.verifiersMutex.RLock()
	verifier, exists := v.verifiers[proofType]
	v.verifiersMutex.RUnlock()
	
	if !exists {
		return false, fmt.Errorf("%w: %s", ErrUnknownProofType, proofType)
	}
	
	// Serialize the proof for verification
	proofBytes, err := proof.Serialize()
	if err != nil {
		return false, fmt.Errorf("failed to serialize proof: %w", err)
	}
	
	// Verify the proof
	verified, err := verifier(ctx, proofBytes)
	if err != nil {
		return false, err
	}
	
	// Cache the result
	v.verifiedCacheMutex.Lock()
	v.verifiedCache[rootHash] = verified
	v.verifiedCacheMutex.Unlock()
	
	return verified, nil
}

// VerifyProofBatch efficiently verifies multiple proofs in a batch
func (v *Verifier) VerifyProofBatch(ctx context.Context, proofs []core.StatelessProof) ([]bool, error) {
	if len(proofs) == 0 {
		return nil, nil
	}
	
	// Create channels for parallel verification
	type verificationResult struct {
		index    int
		verified bool
		err      error
	}
	
	resultChan := make(chan verificationResult, len(proofs))
	
	// Verify proofs in parallel
	for i, proof := range proofs {
		go func(idx int, p core.StatelessProof) {
			verified, err := v.VerifyProof(ctx, p)
			resultChan <- verificationResult{idx, verified, err}
		}(i, proof)
	}
	
	// Collect results
	results := make([]bool, len(proofs))
	var firstError error
	
	for i := 0; i < len(proofs); i++ {
		result := <-resultChan
		results[result.index] = result.verified
		if result.err != nil && firstError == nil {
			firstError = result.err
		}
	}
	
	return results, firstError
}

// RegisterProofType registers a new proof type with the verifier
func (v *Verifier) RegisterProofType(
	proofType string,
	verifier func(context.Context, []byte) (bool, error),
) error {
	if proofType == "" {
		return errors.New("proof type cannot be empty")
	}
	
	if verifier == nil {
		return errors.New("verifier function cannot be nil")
	}
	
	v.verifiersMutex.Lock()
	defer v.verifiersMutex.Unlock()
	
	if _, exists := v.verifiers[proofType]; exists {
		return fmt.Errorf("proof type already registered: %s", proofType)
	}
	
	v.verifiers[proofType] = verifier
	return nil
}

// verifyStateProof verifies a serialized state proof
func (v *Verifier) verifyStateProof(ctx context.Context, proofBytes []byte) (bool, error) {
	// Parse the state proof
	stateProof, err := proofs.ParseStateProof(proofBytes)
	if err != nil {
		return false, fmt.Errorf("failed to parse state proof: %w", err)
	}
	
	// Verify the proof
	// This handles both format types thanks to the binary format detection
	return stateProof.Verify(ctx)
}
