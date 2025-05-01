// Copyright (C) 2025, Rhombus Technologies. All rights reserved.
// See the file LICENSE for licensing terms.

package archive

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"log"
	"time"

	"github.com/rhombus-tech/vm/tee/stateless/core"
)

// MockZKCircuit is a test implementation of the ZKCircuit interface
// that simulates ZK proof generation and verification without actually
// implementing cryptographic zero-knowledge proofs.
//
// This implementation is useful for testing the archival system and
// demonstrating the workflow before integrating a real ZK library.
type MockZKCircuit struct {
	// SimulatedProofSizeBytes is the size of generated mock proofs
	SimulatedProofSizeBytes int
	
	// SimulatedGenerationTimeMs is the time to sleep when generating proofs
	// to simulate computation time
	SimulatedGenerationTimeMs int
	
	// SimulatedVerificationTimeMs is the time to sleep when verifying proofs
	// to simulate verification time
	SimulatedVerificationTimeMs int
	
	// FailureRate is the probability (0.0-1.0) that proof generation/verification will fail
	FailureRate float64
}

// NewMockZKCircuit creates a new mock ZK circuit with default simulation parameters
func NewMockZKCircuit() *MockZKCircuit {
	return &MockZKCircuit{
		SimulatedProofSizeBytes:    4096,  // 4KB proofs
		SimulatedGenerationTimeMs:  5000,  // 5 seconds to generate
		SimulatedVerificationTimeMs: 50,    // 50ms to verify
		FailureRate:                0.0,    // No failures by default
	}
}

// GenerateProof creates a mock ZK proof for the given blocks
func (m *MockZKCircuit) GenerateProof(
	ctx context.Context,
	blocks []core.StatelessBlock,
	startState [sha256.Size]byte,
	endState [sha256.Size]byte,
	startHeight,
	endHeight uint64,
) ([]byte, error) {
	// Simulate computation time
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(time.Duration(m.SimulatedGenerationTimeMs) * time.Millisecond):
		// Continue processing
	}
	
	// Validate inputs
	if len(blocks) == 0 {
		return nil, fmt.Errorf("cannot generate proof for empty block list")
	}
	
	if startHeight+uint64(len(blocks))-1 != endHeight {
		return nil, fmt.Errorf("block count (%d) doesn't match height range (%d-%d)",
			len(blocks), startHeight, endHeight)
	}
	
	// Create a deterministic but unique "proof" based on the inputs
	proof := make([]byte, m.SimulatedProofSizeBytes)
	
	// First 32 bytes: hash of start state
	copy(proof[:32], startState[:])
	
	// Next 32 bytes: hash of end state
	copy(proof[32:64], endState[:])
	
	// Next 8 bytes: start height
	binary.LittleEndian.PutUint64(proof[64:72], startHeight)
	
	// Next 8 bytes: end height
	binary.LittleEndian.PutUint64(proof[72:80], endHeight)
	
	// Next bytes: block IDs and state roots
	offset := 80
	for _, block := range blocks {
		// Add block ID
		id := block.ID()
		copy(proof[offset:offset+32], id[:])
		offset += 32
		
		// Add state root
		stateRoot := block.StateRoot()
		copy(proof[offset:offset+32], stateRoot[:])
		offset += 32
		
		// Safety check
		if offset > m.SimulatedProofSizeBytes-64 {
			break
		}
	}
	
	// Calculate final hash for the remaining space
	h := sha256.Sum256(proof[:offset])
	for i := offset; i < m.SimulatedProofSizeBytes; i += 32 {
		remainingSpace := m.SimulatedProofSizeBytes - i
		if remainingSpace >= 32 {
			copy(proof[i:i+32], h[:])
		} else {
			copy(proof[i:m.SimulatedProofSizeBytes], h[:remainingSpace])
		}
	}
	
	log.Printf("[MockZKCircuit] Generated mock proof for heights %d-%d: %d bytes",
		startHeight, endHeight, len(proof))
	
	return proof, nil
}

// VerifyProof verifies a mock ZK proof
func (m *MockZKCircuit) VerifyProof(
	ctx context.Context,
	proof []byte,
	startHeight,
	endHeight uint64,
) (bool, error) {
	// Simulate verification time
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-time.After(time.Duration(m.SimulatedVerificationTimeMs) * time.Millisecond):
		// Continue processing
	}
	
	// Basic validation
	if len(proof) != m.SimulatedProofSizeBytes {
		return false, fmt.Errorf("invalid proof size: expected %d, got %d",
			m.SimulatedProofSizeBytes, len(proof))
	}
	
	// Extract embedded heights from the proof
	proofStartHeight := binary.LittleEndian.Uint64(proof[64:72])
	proofEndHeight := binary.LittleEndian.Uint64(proof[72:80])
	
	// Verify that the proof covers the requested range
	if proofStartHeight > startHeight || proofEndHeight < endHeight {
		return false, fmt.Errorf("proof range (%d-%d) doesn't cover requested range (%d-%d)",
			proofStartHeight, proofEndHeight, startHeight, endHeight)
	}
	
	log.Printf("[MockZKCircuit] Verified mock proof for heights %d-%d",
		startHeight, endHeight)
	
	return true, nil
}

// GenerateRecursiveProof generates a mock recursive proof
func (m *MockZKCircuit) GenerateRecursiveProof(
	ctx context.Context,
	proofs [][]byte,
	startHeight,
	endHeight uint64,
) ([]byte, error) {
	// Recursive proofs take longer to generate
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(time.Duration(m.SimulatedGenerationTimeMs*2) * time.Millisecond):
		// Continue processing
	}
	
	// Basic validation
	if len(proofs) == 0 {
		return nil, fmt.Errorf("cannot generate recursive proof with no input proofs")
	}
	
	// Create a new "recursive proof" (just a different structure than regular proofs)
	recursiveProof := make([]byte, m.SimulatedProofSizeBytes)
	
	// First 4 bytes: number of proofs combined
	binary.LittleEndian.PutUint32(recursiveProof[:4], uint32(len(proofs)))
	
	// Next 8 bytes: start height
	binary.LittleEndian.PutUint64(recursiveProof[4:12], startHeight)
	
	// Next 8 bytes: end height
	binary.LittleEndian.PutUint64(recursiveProof[12:20], endHeight)
	
	// Next 4 bytes per proof: hash of each proof
	offset := 20
	for _, proof := range proofs {
		h := sha256.Sum256(proof)
		copy(recursiveProof[offset:offset+32], h[:])
		offset += 32
		
		// Safety check
		if offset > m.SimulatedProofSizeBytes-64 {
			break
		}
	}
	
	// Calculate final hash for the remaining space
	h := sha256.Sum256(recursiveProof[:offset])
	for i := offset; i < m.SimulatedProofSizeBytes; i += 32 {
		remainingSpace := m.SimulatedProofSizeBytes - i
		if remainingSpace >= 32 {
			copy(recursiveProof[i:i+32], h[:])
		} else {
			copy(recursiveProof[i:m.SimulatedProofSizeBytes], h[:remainingSpace])
		}
	}
	
	log.Printf("[MockZKCircuit] Generated mock recursive proof combining %d proofs for heights %d-%d",
		len(proofs), startHeight, endHeight)
	
	return recursiveProof, nil
}

// VerifyRecursiveProof verifies a mock recursive proof
func (m *MockZKCircuit) VerifyRecursiveProof(
	ctx context.Context,
	proof []byte,
	startHeight,
	endHeight uint64,
) (bool, error) {
	// Recursive proofs are slightly faster to verify than regular proofs
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-time.After(time.Duration(m.SimulatedVerificationTimeMs) * time.Millisecond):
		// Continue processing
	}
	
	// Basic validation
	if len(proof) != m.SimulatedProofSizeBytes {
		return false, fmt.Errorf("invalid recursive proof size: expected %d, got %d",
			m.SimulatedProofSizeBytes, len(proof))
	}
	
	// Extract embedded heights from the proof
	proofStartHeight := binary.LittleEndian.Uint64(proof[4:12])
	proofEndHeight := binary.LittleEndian.Uint64(proof[12:20])
	
	// Verify that the proof covers the requested range
	if proofStartHeight > startHeight || proofEndHeight < endHeight {
		return false, fmt.Errorf("recursive proof range (%d-%d) doesn't cover requested range (%d-%d)",
			proofStartHeight, proofEndHeight, startHeight, endHeight)
	}
	
	log.Printf("[MockZKCircuit] Verified mock recursive proof for heights %d-%d",
		startHeight, endHeight)
	
	return true, nil
}
