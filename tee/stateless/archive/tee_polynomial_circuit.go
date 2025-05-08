// Copyright (C) 2025, Rhombus Technologies. All rights reserved.
// See the file LICENSE for licensing terms.

package archive

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"time"

	"github.com/rhombus-tech/vm/tee/stateless/core"
)

// TEEPolynomialCircuit implements the ZKCircuit interface using TEE-backed polynomial commitments
// It supports SGX, SEV and TDX for different workload types including AI operations
type TEEPolynomialCircuit struct {
	teeEndpoint string
	client      *http.Client
	teeClient   *MeshClient
	
	// AI workload specific configuration
	aiCapable   bool
	maxModelSize int64
	batchSize   int
	
	// Maximum batch size for polynomial commitments
	maxBatchSize int
	
	// Size of field elements in bytes
	fieldElementSize int
	
	// Whether to use acceleration
	useAcceleration bool
	
	// Whether to use the mesh network
	useMeshNetwork bool
	
	// Region for mesh network operations
	region string
	
	// For performance tracking
	lastOperationTime time.Duration
}

// NewTEEPolynomialCircuit creates a new ZK circuit using TEE-backed polynomial commitments
// with a single TEE endpoint (legacy mode)
func NewTEEPolynomialCircuit(teeEndpoint string, options ...CircuitOption) *TEEPolynomialCircuit {
	circuit := &TEEPolynomialCircuit{
		teeEndpoint:     teeEndpoint,
		client:          &http.Client{},
		teeClient:       nil,
		aiCapable:       false,
		maxModelSize:    0,
		batchSize:       0,
		maxBatchSize:    100,
		fieldElementSize: 32, // pasta_curves::Fp is 32 bytes
		useAcceleration: true,
		useMeshNetwork:  false,
	}
	
	// Apply options
	for _, option := range options {
		option(circuit)
	}
	
	return circuit
}

// Helper function to check if a TEE type is in a slice
func containsTEEType(types []TEEType, target TEEType) bool {
	for _, t := range types {
		if t == target {
			return true
		}
	}
	return false
}

// NewMeshTEEPolynomialCircuit creates a new TEEPolynomialCircuit with a MeshClient
// It supports both standard and AI-optimized configurations
func NewMeshTEEPolynomialCircuit(teeEndpoint string, region string, options ...CircuitOption) *TEEPolynomialCircuit {
	// Create base circuit
	circuit := NewTEEPolynomialCircuit(teeEndpoint, options...)
	
	// Configure for TEE mesh network usage
	circuit.useMeshNetwork = true
	circuit.region = region
	
	// Add TDX support for AI workloads if needed
	aiCapable := false
	for _, opt := range options {
		// Check if this option enables AI capabilities by applying it to a temporary circuit
		tempCircuit := &TEEPolynomialCircuit{}
		opt(tempCircuit)
		if tempCircuit.aiCapable {
			aiCapable = true
			break
		}
	}
	
	// Configure the mesh client
	config := MeshClientConfig{
		ConnectionTimeout:       60 * time.Second,
		MaxRetries:              3,
		RegionalPreference:      true,
		CircuitBreakerThreshold: 5,
		AttestationCacheTTL:     10 * time.Minute,
	}
	
	// Add TDX support if AI capabilities are enabled
	if aiCapable {
		config.PreferredTEETypes = []TEEType{TEETypeIntelSGX, TEETypeSEV, TEETypeTDX}
		config.AIEnabled = true
		config.BatchSize = 128 // Default batch size for AI operations
		config.MaxModelSize = 1 << 30 // 1GB default max model size
	} else {
		config.PreferredTEETypes = []TEEType{TEETypeIntelSGX, TEETypeSEV}
	}
	
	// Create the mesh client
	meshClient := NewMeshClient(teeEndpoint, region, config)
	
	// Set mesh client on circuit
	circuit.teeClient = meshClient
	
	return circuit
}

// CircuitOption configures the TEE polynomial circuit
type CircuitOption func(*TEEPolynomialCircuit)

// WithMeshNetwork enables or disables the use of the mesh network
func WithMeshNetwork(use bool) CircuitOption {
	return func(c *TEEPolynomialCircuit) {
		c.useMeshNetwork = use
	}
}

// WithRegion sets the region for mesh network operations
func WithRegion(region string) CircuitOption {
	return func(c *TEEPolynomialCircuit) {
		c.region = region
	}
}

// WithMaxBatchSize sets the maximum batch size for polynomial commitments
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

// WithAICapabilities enables AI workload support with TDX
func WithAICapabilities() CircuitOption {
	return func(c *TEEPolynomialCircuit) {
		c.aiCapable = true
		c.batchSize = 128 // Default batch size for AI operations
		c.maxModelSize = 1 << 30 // 1GB default max model size
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
