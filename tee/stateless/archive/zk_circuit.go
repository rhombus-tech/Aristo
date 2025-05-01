// Copyright (C) 2025, Rhombus Technologies. All rights reserved.
// See the file LICENSE for licensing terms.

package archive

import (
	"context"
	"crypto/sha256"

	"github.com/rhombus-tech/vm/tee/stateless/core"
)

// ZKCircuit defines the interface for generating and verifying ZK proofs
// This abstraction allows for different ZK implementations to be used
type ZKCircuit interface {
	// GenerateProof creates a ZK proof for a range of blocks
	// The proof verifies that applying the blocks to startState results in endState
	GenerateProof(
		ctx context.Context, 
		blocks []core.StatelessBlock, 
		startState [sha256.Size]byte,
		endState [sha256.Size]byte, 
		startHeight, 
		endHeight uint64,
	) ([]byte, error)
	
	// VerifyProof verifies a proof for a specific height range
	VerifyProof(
		ctx context.Context,
		proof []byte,
		startHeight,
		endHeight uint64,
	) (bool, error)
	
	// GenerateRecursiveProof combines multiple proofs into a single recursive proof
	// This enables proof compression and constant-sized state
	GenerateRecursiveProof(
		ctx context.Context,
		proofs [][]byte,
		startHeight,
		endHeight uint64,
	) ([]byte, error)
	
	// VerifyRecursiveProof verifies a recursive proof
	VerifyRecursiveProof(
		ctx context.Context,
		proof []byte,
		startHeight,
		endHeight uint64,
	) (bool, error)
}
