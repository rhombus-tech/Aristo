// Package proofs provides implementations of stateless blockchain proofs
package proofs

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"time"
	
	// Local interfaces package
	"github.com/rhombus-tech/vm/tee/stateless/core"
)

const (
	// MaxProofSize limits proof size for security
	MaxProofSize = 1024 * 16 // 16KB
	
	// StateProofType is the identifier for state transition proofs
	StateProofType = "state"
	
	// MaxStateProofBatchSize is the maximum number of proofs that can be processed in a single batch
	MaxStateProofBatchSize = 10000
)

var (
	// ErrInvalidProofSize indicates the proof exceeds the maximum allowed size
	ErrInvalidProofSize = errors.New("invalid proof size: exceeds maximum allowed")
	
	// ErrInvalidProofFormat indicates the proof format is not recognized
	ErrInvalidProofFormat = errors.New("invalid proof format")
	
	// ErrInvalidSignature indicates signature verification failed
	ErrInvalidSignature = errors.New("invalid signature in proof")
	
	// ErrInvalidStateTransition indicates the state transition is invalid
	ErrInvalidStateTransition = errors.New("invalid state transition")
	
	// ErrUntrustedMeasurement indicates the TEE measurement is not trusted
	ErrUntrustedMeasurement = errors.New("untrusted TEE measurement")
)

// StateProof is a proof of a state transition
type StateProof struct {
	FromRoot      [sha256.Size]byte
	ToRoot        [sha256.Size]byte
	TransitionID  [32]byte
	TEEMeasurement [32]byte
	Timestamp     uint64
	RegionID      string
	Signature     []byte
	TEEType       string // "SGX" or "SEV"
}

// NewStateProof creates a new state transition proof
func NewStateProof(
	fromRoot, 
	toRoot [sha256.Size]byte, 
	transitionID [32]byte,
	teeMeasurement [32]byte,
	timestamp uint64,
	regionID string,
	teeType string,
) *StateProof {
	return &StateProof{
		FromRoot:       fromRoot,
		ToRoot:         toRoot,
		TransitionID:   transitionID,
		TEEMeasurement: teeMeasurement,
		Timestamp:      timestamp,
		RegionID:       regionID,
		TEEType:        teeType,
	}
}

// Ensure StateProof implements core.StatelessProof
var _ core.StatelessProof = (*StateProof)(nil)

// Verify checks if the state proof is valid
func (p *StateProof) Verify(ctx context.Context) (bool, error) {
	// 1. Check for context cancellation
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
		// Continue processing
	}
	
	// 2. Verify the proof size with proper bounds checking (protection against 3.5GB vulnerability)
	if p.Size() > MaxProofSize {
		return false, ErrInvalidProofSize
	}
	
	// 3. Structural validation
	if err := p.validateStructure(); err != nil {
		return false, err
	}
	
	// 4. TEE measurement verification
	if err := p.verifyTEEMeasurement(ctx); err != nil {
		return false, fmt.Errorf("TEE measurement verification failed: %w", err)
	}
	
	// 5. Signature verification
	if err := p.verifySignature(ctx); err != nil {
		return false, fmt.Errorf("signature verification failed: %w", err)
	}
	
	// 6. State transition validation
	if err := p.verifyStateTransition(); err != nil {
		return false, fmt.Errorf("state transition verification failed: %w", err)
	}
	
	return true, nil
}

// validateStructure validates the basic structure of the state proof
func (p *StateProof) validateStructure() error {
	// Check signature existence
	if len(p.Signature) == 0 {
		return ErrInvalidSignature
	}
	
	// Verify region ID is present
	if p.RegionID == "" {
		return errors.New("missing region ID")
	}
	
	// Verify TEE type is valid
	if p.TEEType != TEETypeSGX && p.TEEType != TEETypeSEV && p.TEEType != TEETypeTDX {
		return fmt.Errorf("invalid TEE type: %s", p.TEEType)
	}
	
	// Verify from/to roots are not identical (except for genesis block)
	if bytes.Equal(p.FromRoot[:], p.ToRoot[:]) && !bytes.Equal(p.FromRoot[:], make([]byte, len(p.FromRoot))) {
		return fmt.Errorf("%w: from and to roots are identical", ErrInvalidStateTransition)
	}
	
	return nil
}

// verifyTEEMeasurement verifies the TEE measurement against the attestation service
func (p *StateProof) verifyTEEMeasurement(ctx context.Context) error {
	// Use the TEEVerifier from execution_proof.go
	// Get attestation verifier from context
	verifierValue := ctx.Value("attestation_service")
	if verifierValue == nil {
		return errors.New("TEE verifier not found in context")
	}
	
	// Type assertion to the interface from execution_proof.go
	var verifier interface {
		VerifyMeasurement(ctx context.Context, teeType string, measurement []byte) (bool, error)
	}
	var ok bool
	if verifier, ok = verifierValue.(interface{
		VerifyMeasurement(ctx context.Context, teeType string, measurement []byte) (bool, error)
	}); !ok {
		return errors.New("invalid TEE verifier type in context")
	}
	
	// Check if the TEE measurement is trusted
	isTrusted, err := verifier.VerifyMeasurement(ctx, p.TEEType, p.TEEMeasurement[:])
	if err != nil {
		return fmt.Errorf("measurement verification error: %w", err)
	}
	if !isTrusted {
		return fmt.Errorf("%w for %s TEE", ErrUntrustedMeasurement, p.TEEType)
	}
	
	return nil
}

// Import the TEEVerifier interface from execution_proof.go via direct reference
// Note: In a production environment, this would be in a shared package

// verifySignature verifies the signature from the TEE
func (p *StateProof) verifySignature(ctx context.Context) error {
	// Use the TEEVerifier from execution_proof.go
	// Get signature verifier from context
	verifierValue := ctx.Value("attestation_service")
	if verifierValue == nil {
		return errors.New("TEE verifier not found in context")
	}
	
	// Type assertion to the interface from execution_proof.go
	var verifier interface {
		VerifySignature(ctx context.Context, teeType string, measurement []byte, enclaveID []byte, message []byte, signature []byte) (bool, error)
	}
	var ok bool
	if verifier, ok = verifierValue.(interface{
		VerifySignature(ctx context.Context, teeType string, measurement []byte, enclaveID []byte, message []byte, signature []byte) (bool, error)
	}); !ok {
		return errors.New("invalid TEE verifier type in context")
	}
	
	// Create message to verify
	signatureMessage := p.buildSignatureMessage()
	
	// Verify signature using the appropriate verifier
	isValid, err := verifier.VerifySignature(ctx, p.TEEType, p.TEEMeasurement[:], nil, signatureMessage, p.Signature)
	if err != nil {
		return fmt.Errorf("signature verification error: %w", err)
	}
	if !isValid {
		return ErrInvalidSignature
	}
	
	return nil
}

// buildSignatureMessage builds the message that was signed by the TEE
func (p *StateProof) buildSignatureMessage() []byte {
	// Create a buffer to build the message that was signed
	var message bytes.Buffer
	
	// Add fields in the same order they're serialized
	message.Write(p.FromRoot[:])
	message.Write(p.ToRoot[:])
	message.Write(p.TransitionID[:])
	message.Write(p.TEEMeasurement[:])
	
	timestampBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(timestampBytes, p.Timestamp)
	message.Write(timestampBytes)
	
	message.WriteString(p.TEEType)
	message.WriteString(p.RegionID)
	
	return message.Bytes()
}

// verifyStateTransition verifies that the state transition is valid
func (p *StateProof) verifyStateTransition() error {
	// Check if roots are non-zero (except for genesis state)
	isFromRootZero := isZeroArray(p.FromRoot[:])
	isToRootZero := isZeroArray(p.ToRoot[:])
	
	// Genesis block can have zero FromRoot, but must have non-zero ToRoot
	if isFromRootZero && isToRootZero {
		return fmt.Errorf("%w: both roots cannot be zero", ErrInvalidStateTransition)
	}
	
	// For non-genesis blocks, FromRoot cannot be zero
	if isFromRootZero && !isToRootZero && p.TransitionID != [32]byte{} {
		return fmt.Errorf("%w: from root cannot be zero for non-genesis transition", ErrInvalidStateTransition)
	}
	
	// ToRoot can never be zero
	if isToRootZero {
		return fmt.Errorf("%w: to root cannot be zero", ErrInvalidStateTransition)
	}
	
	// Check timestamp is reasonable
	if p.Timestamp == 0 {
		return fmt.Errorf("%w: timestamp cannot be zero", ErrInvalidStateTransition)
	}
	
	return nil
}

// isZeroArray checks if the byte array contains only zeros
func isZeroArray(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return false
		}
	}
	return true
}

// BatchVerifyStateProofs verifies multiple state proofs in parallel
// This is optimized for high-throughput verification in the timeserver architecture
func BatchVerifyStateProofs(ctx context.Context, proofs []*StateProof) ([]bool, []error) {
	if len(proofs) == 0 {
		return []bool{}, []error{}
	}
	
	// Cap batch size to prevent resource exhaustion attacks
	if len(proofs) > MaxStateProofBatchSize {
		return nil, nil // TODO: Consider returning an appropriate error
	}
	
	// Determine optimal number of workers and chunk size for the current hardware
	numWorkers := stateProofOptimalWorkerCount()
	chunkSize := stateProofOptimalChunkSize(len(proofs), numWorkers)
	
	// Prepare result arrays
	results := make([]bool, len(proofs))
	errors := make([]error, len(proofs))
	
	// Create a worker pool and distribute the work
	var wg sync.WaitGroup
	for workerID := 0; workerID < numWorkers; workerID++ {
		// Calculate the chunk for this worker
		startIdx := workerID * chunkSize
		endIdx := startIdx + chunkSize
		if endIdx > len(proofs) {
			endIdx = len(proofs) // Don't exceed array bounds
		}
		
		// Skip if this worker has no work
		if startIdx >= len(proofs) {
			continue
		}
		
		wg.Add(1)
		go func(start, end int) {
			defer wg.Done()
			
			// Process each proof in this worker's chunk
			for i := start; i < end; i++ {
				// Add jitter to avoid thundering herd on shared resources
				if numWorkers > 4 {
					jitter := time.Duration(rand.Intn(1000)) * time.Microsecond
					time.Sleep(jitter)
				}
				
				// Verify the proof and store the result
				isValid, err := proofs[i].Verify(ctx)
				results[i] = isValid
				errors[i] = err
			}
		}(startIdx, endIdx)
	}
	
	// Wait for all workers to complete
	wg.Wait()
	return results, errors
}

// stateProofOptimalWorkerCount determines the optimal number of workers for state proof verification
func stateProofOptimalWorkerCount() int {
	// Use 75% of available cores for this work
	optimalCount := int(float64(runtime.NumCPU()) * 0.75)
	
	// Ensure at least 2 workers and not more than system cores
	if optimalCount < 2 {
		return 2
	}
	if optimalCount > runtime.NumCPU() {
		return runtime.NumCPU()
	}
	
	return optimalCount
}

// stateProofOptimalChunkSize determines optimal chunk size for state proof batch verification
func stateProofOptimalChunkSize(batchSize, workerCount int) int {
	// Simple calculation for now, can be refined based on empirical performance testing
	chunkSize := batchSize / workerCount
	if chunkSize < 1 {
		return 1
	}
	return chunkSize
}

// RootHash returns the destination state root
func (p *StateProof) RootHash() [sha256.Size]byte {
	return p.ToRoot
}

// ProofType returns the type of proof
func (p *StateProof) ProofType() string {
	return StateProofType
}

// Serialize returns a binary representation of the proof
// Supports both your binary formats (length-prefixed and direct)
func (p *StateProof) Serialize() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	
	// For length-prefixed format (following WebAssembly convention):
	// Write a length prefix (4 bytes little-endian)
	data := p.serializeDirectFormat()
	lengthBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(lengthBytes, uint32(len(data)))
	
	buf.Write(lengthBytes)
	buf.Write(data)
	
	return buf.Bytes(), nil
}

// SerializeDirectFormat serializes without length prefix
// This matches your direct binary format used in Go tests
func (p *StateProof) SerializeDirectFormat() ([]byte, error) {
	return p.serializeDirectFormat(), nil
}

// serializeDirectFormat is the internal implementation for direct format serialization
func (p *StateProof) serializeDirectFormat() []byte {
	buf := bytes.NewBuffer(nil)
	
	// Write all fields in a deterministic order
	buf.Write(p.FromRoot[:])
	buf.Write(p.ToRoot[:])
	buf.Write(p.TransitionID[:])
	buf.Write(p.TEEMeasurement[:])
	
	timestampBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(timestampBytes, p.Timestamp)
	buf.Write(timestampBytes)
	
	// Write the region ID length and data
	regionIDBytes := []byte(p.RegionID)
	regionIDLenBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(regionIDLenBytes, uint16(len(regionIDBytes)))
	buf.Write(regionIDLenBytes)
	buf.Write(regionIDBytes)
	
	// Write the TEE type length and data
	teeTypeBytes := []byte(p.TEEType)
	teeTypeLenBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(teeTypeLenBytes, uint16(len(teeTypeBytes)))
	buf.Write(teeTypeLenBytes)
	buf.Write(teeTypeBytes)
	
	// Write signature length and data
	sigLenBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(sigLenBytes, uint16(len(p.Signature)))
	buf.Write(sigLenBytes)
	buf.Write(p.Signature)
	
	return buf.Bytes()
}

// ParseStateProof parses a binary representation into a StateProof
// This implementation supports both length-prefixed and direct binary formats
// following the dual-format parameter validation pattern we use throughout the system
func ParseStateProof(data []byte) (*StateProof, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("proof data too short: %d bytes", len(data))
	}
	
	// Check if this is a length-prefixed format
	// First try length-prefixed format if the first 4 bytes represent a reasonable length
	length := binary.LittleEndian.Uint32(data[:4])
	
	// Validate reasonable length (0 < len <= MaxProofSize)
	if length > 0 && length <= MaxProofSize && int(length) <= len(data)-4 {
		// This is a length-prefixed format
		return parseStateProofDirectFormat(data[4:4+length])
	} else {
		// Fall back to direct format
		return parseStateProofDirectFormat(data)
	}
}

// parseStateProofDirectFormat parses the direct format
func parseStateProofDirectFormat(data []byte) (*StateProof, error) {
	if len(data) < 32*4+8+2 { // Minimum size for fixed fields + first length
		return nil, fmt.Errorf("proof data too short for direct format: %d bytes", len(data))
	}
	
	pos := 0
	
	// Parse fixed-size fields
	p := &StateProof{}
	
	// FromRoot
	copy(p.FromRoot[:], data[pos:pos+32])
	pos += 32
	
	// ToRoot
	copy(p.ToRoot[:], data[pos:pos+32])
	pos += 32
	
	// TransitionID
	copy(p.TransitionID[:], data[pos:pos+32])
	pos += 32
	
	// TEEMeasurement
	copy(p.TEEMeasurement[:], data[pos:pos+32])
	pos += 32
	
	// Timestamp
	p.Timestamp = binary.LittleEndian.Uint64(data[pos:pos+8])
	pos += 8
	
	// RegionID
	if pos+2 > len(data) {
		return nil, errors.New("unexpected end of data while parsing RegionID length")
	}
	regionIDLen := binary.LittleEndian.Uint16(data[pos:pos+2])
	pos += 2
	
	if pos+int(regionIDLen) > len(data) {
		return nil, errors.New("unexpected end of data while parsing RegionID")
	}
	p.RegionID = string(data[pos:pos+int(regionIDLen)])
	pos += int(regionIDLen)
	
	// TEEType
	if pos+2 > len(data) {
		return nil, errors.New("unexpected end of data while parsing TEEType length")
	}
	teeTypeLen := binary.LittleEndian.Uint16(data[pos:pos+2])
	pos += 2
	
	if pos+int(teeTypeLen) > len(data) {
		return nil, errors.New("unexpected end of data while parsing TEEType")
	}
	p.TEEType = string(data[pos:pos+int(teeTypeLen)])
	pos += int(teeTypeLen)
	
	// Signature
	if pos+2 > len(data) {
		return nil, errors.New("unexpected end of data while parsing signature length")
	}
	sigLen := binary.LittleEndian.Uint16(data[pos:pos+2])
	pos += 2
	
	if pos+int(sigLen) > len(data) {
		return nil, errors.New("unexpected end of data while parsing signature")
	}
	p.Signature = make([]byte, sigLen)
	copy(p.Signature, data[pos:pos+int(sigLen)])
	
	return p, nil
}

// Size returns the size of the proof in bytes
func (p *StateProof) Size() uint64 {
	// Calculate the size of the proof
	// Fixed size fields
	size := 32*4 + 8 // 4 32-byte fields + 8-byte timestamp
	
	// Variable size fields
	size += 2 + len(p.RegionID)    // 2-byte length + RegionID
	size += 2 + len(p.TEEType)     // 2-byte length + TEEType
	size += 2 + len(p.Signature)   // 2-byte length + Signature
	
	return uint64(size)
}

// Sign signs the proof with the given TEE
func (p *StateProof) Sign(signer interface{}) error {
	// Create message to sign
	message := bytes.NewBuffer(nil)
	message.Write(p.FromRoot[:])
	message.Write(p.ToRoot[:])
	message.Write(p.TransitionID[:])
	message.Write(p.TEEMeasurement[:])
	
	timestampBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(timestampBytes, p.Timestamp)
	message.Write(timestampBytes)
	
	message.WriteString(p.TEEType)
	message.WriteString(p.RegionID)
	
	// Handle different types of signers
	switch s := signer.(type) {
	case ed25519.PrivateKey:
		// Use Ed25519 for signing - common for TEE attestation
		p.Signature = ed25519.Sign(s, message.Bytes())
		return nil
		
	case interface{ Sign(data []byte) ([]byte, error) }:
		// Interface for custom signers with a Sign method
		sig, err := s.Sign(message.Bytes())
		if err != nil {
			return fmt.Errorf("failed to sign with custom signer: %w", err)
		}
		p.Signature = sig
		return nil
		
	case nil:
		// If no signer is provided, fall back to a deterministic signature for testing
		// This should never be used in production
		hasher := sha256.New()
		hasher.Write(message.Bytes())
		p.Signature = hasher.Sum(nil)
		return nil
		
	default:
		return fmt.Errorf("unsupported signer type: %T", signer)
	}
}
