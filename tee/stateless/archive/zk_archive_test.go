// Copyright (C) 2025, Rhombus Technologies. All rights reserved.
// See the file LICENSE for licensing terms.

package archive

import (
	"context"
	"fmt"
	"log"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/rhombus-tech/vm/tee/stateless/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockStatelessBlock implements the core.StatelessBlock interface for testing
type MockStatelessBlock struct {
	id        ids.ID
	parentID  ids.ID
	height    uint64
	timestamp int64
	stateRoot [32]byte
	proofs    []core.StatelessProof
	bytes     []byte
}

func (b *MockStatelessBlock) ID() ids.ID                  { return b.id }
func (b *MockStatelessBlock) ParentID() ids.ID            { return b.parentID }
func (b *MockStatelessBlock) Height() uint64              { return b.height }
func (b *MockStatelessBlock) Timestamp() time.Time        { return time.Unix(b.timestamp, 0) }
func (b *MockStatelessBlock) StateRoot() [32]byte         { return b.stateRoot }
func (b *MockStatelessBlock) Proofs() []core.StatelessProof { return b.proofs }
func (b *MockStatelessBlock) Bytes() ([]byte, error)      { return b.bytes, nil }
func (b *MockStatelessBlock) Verify(ctx context.Context, verifier core.StatelessVerifier) (bool, error) {
	return true, nil
}

// MockStatelessProof implements the core.StatelessProof interface for testing
type MockStatelessProof struct {
	id              ids.ID
	attestation     bool
	signatureValid  bool
	teeCertified    bool
	rootHash        [32]byte
}

func (p *MockStatelessProof) Verify(ctx context.Context) (bool, error) { return p.signatureValid, nil }
func (p *MockStatelessProof) RootHash() [32]byte                       { return p.rootHash }
func (p *MockStatelessProof) ProofType() string                        { return "test" }
func (p *MockStatelessProof) Serialize() ([]byte, error)               { return p.id[:], nil }
func (p *MockStatelessProof) Size() uint64                             { return 32 }
func (p *MockStatelessProof) HasTEEAttestation() bool                  { return p.attestation }

// MockStatelessChain implements the core.StatelessChain interface for testing
type MockStatelessChain struct {
	blocks    map[uint64]core.StatelessBlock
	blockByID map[ids.ID]core.StatelessBlock
	height    uint64
}

func NewMockStatelessChain() *MockStatelessChain {
	return &MockStatelessChain{
		blocks:    make(map[uint64]core.StatelessBlock),
		blockByID: make(map[ids.ID]core.StatelessBlock),
		height:    0,
	}
}

func (c *MockStatelessChain) AddBlock(ctx context.Context, block core.StatelessBlock) error {
	c.blocks[block.Height()] = block
	c.blockByID[block.ID()] = block
	if block.Height() > c.height {
		c.height = block.Height()
	}
	return nil
}

func (c *MockStatelessChain) GetBlock(ctx context.Context, id ids.ID) (core.StatelessBlock, error) {
	block, exists := c.blockByID[id]
	if !exists {
		return nil, fmt.Errorf("block not found")
	}
	return block, nil
}

// Helper method for tests that doesn't match the interface
func (c *MockStatelessChain) GetBlockByHeight(ctx context.Context, height uint64) (core.StatelessBlock, error) {
	block, exists := c.blocks[height]
	if !exists {
		return nil, fmt.Errorf("block not found")
	}
	return block, nil
}

func (c *MockStatelessChain) GetHeight(ctx context.Context) (uint64, error) {
	return c.height, nil
}

func (c *MockStatelessChain) GetLatestStateRoot(ctx context.Context) ([32]byte, error) {
	if c.height == 0 {
		return [32]byte{}, nil
	}
	block, exists := c.blocks[c.height]
	if !exists {
		return [32]byte{}, fmt.Errorf("block not found")
	}
	return block.StateRoot(), nil
}

func (c *MockStatelessChain) VerifyChain(ctx context.Context, fromHeight, toHeight uint64) (bool, error) {
	return true, nil
}

// createTestChain creates a mock chain with the specified number of blocks
func createTestChain(t *testing.T, numBlocks int) *MockStatelessChain {
	chain := NewMockStatelessChain()
	ctx := context.Background()
	
	// Create genesis block
	var rootHash [32]byte
	copy(rootHash[:], "genesis_root_hash_example_data_32by")
	
	genesisBlock := &MockStatelessBlock{
		id:        ids.GenerateTestID(),
		parentID:  ids.Empty,
		height:    0,
		timestamp: time.Now().Unix(),
		stateRoot: rootHash,
		proofs:    []core.StatelessProof{},
		bytes:     []byte("genesis block"),
	}
	
	err := chain.AddBlock(ctx, genesisBlock)
	require.NoError(t, err)
	
	// Create subsequent blocks
	parentID := genesisBlock.id
	for i := 1; i < numBlocks; i++ {
		// Create a proof with TEE attestation for every other block
		var proofs []core.StatelessProof
		var proofRootHash [32]byte
		copy(proofRootHash[:], fmt.Sprintf("proof_root_hash_block_%d_data_32by", i))
		
		proof := &MockStatelessProof{
			id:             ids.GenerateTestID(),
			attestation:    i%2 == 0, // Every other block has attestation
			signatureValid: true,
			teeCertified:   i%2 == 0,
			rootHash:       proofRootHash,
		}
		proofs = append(proofs, proof)
		
		var blockRootHash [32]byte
		copy(blockRootHash[:], fmt.Sprintf("block_%d_root_hash_example_data_32b", i))
		
		block := &MockStatelessBlock{
			id:        ids.GenerateTestID(),
			parentID:  parentID,
			height:    uint64(i),
			timestamp: time.Now().Unix(),
			stateRoot: blockRootHash,
			proofs:    proofs,
			bytes:     []byte(fmt.Sprintf("block %d", i)),
		}
		
		err := chain.AddBlock(ctx, block)
		require.NoError(t, err)
		
		parentID = block.id
	}
	
	return chain
}

func TestZKArchiver_Basic(t *testing.T) {
	// Create a mock chain with 10 blocks
	chain := createTestChain(t, 10)
	
	// Create a mock ZK circuit
	circuit := NewMockZKCircuit()
	circuit.SimulatedGenerationTimeMs = 50 // Make tests faster
	circuit.SimulatedVerificationTimeMs = 10
	
	// Create archiver with test config
	config := ZKArchiveConfig{
		BatchSize:           3,
		ArchivalPeriod:      100 * time.Millisecond,
		ReferencePoints:     2,
		RecursiveProofLevels: 2,
		Parallelism:         1,
		TEEVerifiedOnly:     false, // Allow non-TEE blocks for testing
	}
	
	archiver, err := NewZKArchiver(config, chain, circuit)
	require.NoError(t, err)
	
	// Start the archiver
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	
	err = archiver.Start(ctx)
	require.NoError(t, err)
	
	// Wait for archival to happen
	time.Sleep(500 * time.Millisecond)
	
	// Check that some blocks were archived
	lastArchived := archiver.GetLastArchivedHeight()
	assert.Greater(t, lastArchived, uint64(0), "No blocks were archived")
	
	// Test verification of a state transition
	result, err := archiver.VerifyHistoricalTransition(ctx, 0, 3)
	assert.NoError(t, err)
	assert.True(t, result, "Verification failed")
	
	// Check metrics
	metrics := archiver.GetMetrics()
	assert.Greater(t, metrics.TotalProofsGenerated, uint64(0), "No proofs were generated")
	assert.Greater(t, metrics.TotalBlocksArchived, uint64(0), "No blocks were archived")
	
	// Clean up
	archiver.Stop()
}

func TestZKArchiver_RecursiveProofs(t *testing.T) {
	// Create a mock chain with 20 blocks
	chain := createTestChain(t, 20)
	
	// Create a mock ZK circuit
	circuit := NewMockZKCircuit()
	circuit.SimulatedGenerationTimeMs = 50 // Make tests faster
	circuit.SimulatedVerificationTimeMs = 10
	
	// Create archiver with test config for recursive proofs
	config := ZKArchiveConfig{
		BatchSize:           2,
		ArchivalPeriod:      100 * time.Millisecond,
		ReferencePoints:     5,
		RecursiveProofLevels: 2,
		Parallelism:         2,
		TEEVerifiedOnly:     false, // Allow non-TEE blocks for testing
	}
	
	archiver, err := NewZKArchiver(config, chain, circuit)
	require.NoError(t, err)
	
	// Start the archiver
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	err = archiver.Start(ctx)
	require.NoError(t, err)
	
	// Wait for archival and recursive proof generation
	time.Sleep(1 * time.Second)
	
	// Check that some blocks were archived
	lastArchived := archiver.GetLastArchivedHeight()
	assert.Greater(t, lastArchived, uint64(0), "No blocks were archived")
	
	// Test verification using recursive proofs
	result, err := archiver.VerifyHistoricalTransition(ctx, 0, 8)
	assert.NoError(t, err)
	assert.True(t, result, "Verification failed")
	
	// Check metrics for recursive proofs
	metrics := archiver.GetMetrics()
	log.Printf("Metrics: %+v", metrics)
	
	// Verify storage savings
	savedBytes := archiver.GetTotalStorageSaved()
	log.Printf("Storage saved: %d bytes", savedBytes)
	
	// Clean up
	archiver.Stop()
}

func TestZKArchiver_TEEVerifiedOnly(t *testing.T) {
	// Create a mock chain with 10 blocks
	chain := createTestChain(t, 10)
	
	// Create a mock ZK circuit
	circuit := NewMockZKCircuit()
	circuit.SimulatedGenerationTimeMs = 50 // Make tests faster
	
	// Create archiver with TEEVerifiedOnly=true
	config := ZKArchiveConfig{
		BatchSize:           2,
		ArchivalPeriod:      100 * time.Millisecond,
		ReferencePoints:     2,
		RecursiveProofLevels: 1,
		Parallelism:         1,
		TEEVerifiedOnly:     true, // Only TEE-verified blocks
	}
	
	archiver, err := NewZKArchiver(config, chain, circuit)
	require.NoError(t, err)
	
	// Start the archiver
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	
	err = archiver.Start(ctx)
	require.NoError(t, err)
	
	// Wait for archival to happen
	time.Sleep(500 * time.Millisecond)
	
	// Check that only TEE-verified blocks were archived
	metrics := archiver.GetMetrics()
	
	// Since we set up the mock chain to have TEE attestation for even-numbered blocks,
	// and our batch size is 2, we'll have partial batches and some failures
	assert.Greater(t, metrics.FailedProofAttempts, uint64(0), 
		"Expected some failed proof attempts due to TEEVerifiedOnly=true")
	
	// Clean up
	archiver.Stop()
}
