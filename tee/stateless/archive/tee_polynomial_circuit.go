// Copyright (C) 2025, Rhombus Technologies. All rights reserved.
// See the file LICENSE for licensing terms.

package archive

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/rhombus-tech/vm/tee/stateless/core"
)

// TEEPolynomialCircuit implements the ZKCircuit interface using
// the TEE-backed polynomial commitment operations
type TEEPolynomialCircuit struct {
	// TEE controller endpoint
	teeEndpoint string
	
	// Maximum batch size for polynomial commitments
	maxBatchSize int
	
	// Field element size in bytes (using pasta_curves::Fp)
	fieldElementSize int
	
	// Whether to use hardware acceleration if available
	useAcceleration bool
	
	// For performance tracking
	lastOperationTime time.Duration
}

// NewTEEPolynomialCircuit creates a new ZK circuit using TEE-backed polynomial commitments
func NewTEEPolynomialCircuit(teeEndpoint string, options ...CircuitOption) *TEEPolynomialCircuit {
	circuit := &TEEPolynomialCircuit{
		teeEndpoint:     teeEndpoint,
		maxBatchSize:    100,
		fieldElementSize: 32, // pasta_curves::Fp is 32 bytes
		useAcceleration: true,
	}
	
	// Apply options
	for _, option := range options {
		option(circuit)
	}
	
	return circuit
}

// CircuitOption configures the TEE polynomial circuit
type CircuitOption func(*TEEPolynomialCircuit)

// WithMaxBatchSize sets the maximum batch size for polynomial operations
func WithMaxBatchSize(size int) CircuitOption {
	return func(c *TEEPolynomialCircuit) {
		c.maxBatchSize = size
	}
}

// WithAcceleration configures hardware acceleration
func WithAcceleration(use bool) CircuitOption {
	return func(c *TEEPolynomialCircuit) {
		c.useAcceleration = use
	}
}

// PolynomialProof represents a proof generated with polynomial commitments
type PolynomialProof struct {
	// Block range information
	StartHeight uint64
	EndHeight   uint64
	
	// State roots
	StartStateRoot [sha256.Size]byte
	EndStateRoot   [sha256.Size]byte
	
	// The polynomial commitment
	Commitment []byte
	
	// TEE attestation data
	AttestationData []byte
	
	// Degree of the polynomial
	Degree uint32
	
	// Additional metadata
	Metadata []byte
}

// GenerateProof creates a ZK proof for a range of blocks using polynomial commitments
func (t *TEEPolynomialCircuit) GenerateProof(
	ctx context.Context,
	blocks []core.StatelessBlock,
	startState [sha256.Size]byte,
	endState [sha256.Size]byte,
	startHeight,
	endHeight uint64,
) ([]byte, error) {
	startTime := time.Now()
	defer func() {
		t.lastOperationTime = time.Since(startTime)
	}()
	
	// Validate inputs
	if len(blocks) == 0 {
		return nil, fmt.Errorf("cannot generate proof for empty block list")
	}
	
	if startHeight+uint64(len(blocks))-1 != endHeight {
		return nil, fmt.Errorf("block count (%d) doesn't match height range (%d-%d)",
			len(blocks), startHeight, endHeight)
	}
	
	// Encode blocks into a matrix format suitable for polynomial commitment
	// Each row represents a block with its state transition
	blockMatrix, err := t.encodeBlocksToMatrix(blocks)
	if err != nil {
		return nil, fmt.Errorf("failed to encode blocks: %w", err)
	}
	
	// Call TEE controller for secure_commit operation
	commitment, attestation, err := t.callTEESecureCommit(ctx, blockMatrix)
	if err != nil {
		return nil, fmt.Errorf("TEE secure_commit failed: %w", err)
	}
	
	// Create the proof structure
	proof := PolynomialProof{
		StartHeight:    startHeight,
		EndHeight:      endHeight,
		StartStateRoot: startState,
		EndStateRoot:   endState,
		Commitment:     commitment,
		AttestationData: attestation,
		Degree:         uint32(len(blocks)),
		Metadata:       nil, // Optional metadata can be added here
	}
	
	// Serialize the proof
	return serializeProof(proof)
}

// VerifyProof verifies a proof for a specific height range
func (t *TEEPolynomialCircuit) VerifyProof(
	ctx context.Context,
	proofBytes []byte,
	startHeight,
	endHeight uint64,
) (bool, error) {
	startTime := time.Now()
	defer func() {
		t.lastOperationTime = time.Since(startTime)
	}()
	
	// Deserialize the proof
	proof, err := deserializeProof(proofBytes)
	if err != nil {
		return false, fmt.Errorf("failed to deserialize proof: %w", err)
	}
	
	// Verify proof range
	if proof.StartHeight > startHeight || proof.EndHeight < endHeight {
		return false, fmt.Errorf("proof range (%d-%d) doesn't cover requested range (%d-%d)",
			proof.StartHeight, proof.EndHeight, startHeight, endHeight)
	}
	
	// Generate evaluation point from the requested heights
	// This approach allows partial verification within a larger proof range
	pointVector, err := t.generatePointVector(startHeight, endHeight)
	if err != nil {
		return false, fmt.Errorf("failed to generate point vector: %w", err)
	}
	
	// Call TEE controller for secure_open_at_point operation
	verified, err := t.callTEESecureOpenAtPoint(ctx, proof.Commitment, pointVector)
	if err != nil {
		return false, fmt.Errorf("TEE secure_open_at_point failed: %w", err)
	}
	
	// Verify TEE attestation
	if !t.verifyAttestation(proof.AttestationData) {
		return false, fmt.Errorf("invalid TEE attestation")
	}
	
	return verified, nil
}

// GenerateRecursiveProof combines multiple proofs into a single recursive proof
func (t *TEEPolynomialCircuit) GenerateRecursiveProof(
	ctx context.Context,
	proofs [][]byte,
	startHeight,
	endHeight uint64,
) ([]byte, error) {
	startTime := time.Now()
	defer func() {
		t.lastOperationTime = time.Since(startTime)
	}()
	
	// Validate inputs
	if len(proofs) == 0 {
		return nil, fmt.Errorf("cannot generate recursive proof with no input proofs")
	}
	
	// Deserialize the individual proofs
	individualProofs := make([]PolynomialProof, len(proofs))
	for i, proofBytes := range proofs {
		proof, err := deserializeProof(proofBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize proof %d: %w", i, err)
		}
		individualProofs[i] = proof
	}
	
	// Combine the commitments into a higher-level matrix
	// Each row represents a commitment from an individual proof
	commitmentMatrix, err := t.combineCommitments(individualProofs)
	if err != nil {
		return nil, fmt.Errorf("failed to combine commitments: %w", err)
	}
	
	// Call TEE controller for recursive secure_commit operation
	recursiveCommitment, attestation, err := t.callTEESecureCommit(ctx, commitmentMatrix)
	if err != nil {
		return nil, fmt.Errorf("TEE recursive secure_commit failed: %w", err)
	}
	
	// Find the overall start and end state roots
	var overallStartRoot, overallEndRoot [sha256.Size]byte
	overallStartRoot = individualProofs[0].StartStateRoot
	overallEndRoot = individualProofs[len(individualProofs)-1].EndStateRoot
	
	// Create the recursive proof
	recursiveProof := PolynomialProof{
		StartHeight:    startHeight,
		EndHeight:      endHeight,
		StartStateRoot: overallStartRoot,
		EndStateRoot:   overallEndRoot,
		Commitment:     recursiveCommitment,
		AttestationData: attestation,
		Degree:         uint32(len(proofs)),
		Metadata:       nil, // Optional metadata can be added here
	}
	
	// Serialize the recursive proof
	return serializeProof(recursiveProof)
}

// VerifyRecursiveProof verifies a recursive proof
func (t *TEEPolynomialCircuit) VerifyRecursiveProof(
	ctx context.Context,
	proofBytes []byte,
	startHeight,
	endHeight uint64,
) (bool, error) {
	// For recursive proofs, we use the same verification approach as regular proofs
	// The difference is in how the proofs were generated
	return t.VerifyProof(ctx, proofBytes, startHeight, endHeight)
}

// Helper methods are implemented in tee_polynomial_helpers.go
