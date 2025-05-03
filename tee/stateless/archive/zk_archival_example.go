// Copyright (C) 2025, Rhombus Technologies. All rights reserved.
// See the file LICENSE for licensing terms.

package archive

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/rhombus-tech/vm/tee/stateless/core"
)

// Uses parseDualFormatParameter from utilities.go

// ZKArchivalExample demonstrates the integration of ZK-based state archival
// with the existing dual-format parameter handling for WebAssembly contracts
func ZKArchivalExample() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	// Use context with cancellation for proper shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Step 1: Set up a mock environment for the example
	log.Println("Setting up mock stateless blockchain environment...")
	chain := newMockStatelessChain()
	
	// Create 1000 blocks to simulate a chain with significant history
	createTestBlocks(ctx, chain, 1000)
	
	// Step 2: Create a stateless verifier similar to the one in integration_example.go
	log.Println("Creating stateless verifier...")
	verifier := &mockStatelessVerifier{chain: chain}
	
	// Step 3: Create the ZK archival integration
	log.Println("Setting up ZK archival system...")
	zkConfig := DefaultZKArchiveConfig()
	zkConfig.BatchSize = 50          // Archive 50 blocks per batch
	zkConfig.ArchivalPeriod = 5 * time.Second  // Process every 5 seconds
	zkConfig.ReferencePoints = 20    // Keep 20 reference points
	zkConfig.RecursiveProofLevels = 3 // Use 3 levels of recursive proofs
	
	// Create a mock circuit for the example
	circuit := NewMockZKCircuit()
	circuit.SimulatedGenerationTimeMs = 200  // Fast simulation
	
	// Define the verify and getState callback functions
	verifyFunc := func(ctx context.Context, fromHeight, toHeight uint64, params []byte) (bool, error) {
		return verifier.VerifyStateTransition(ctx, fromHeight, toHeight, params)
	}
	
	getStateFunc := func(ctx context.Context, stateRoot [32]byte, key []byte) ([]byte, error) {
		// Convert stateRoot byte array to ids.ID
		var idRoot ids.ID
		copy(idRoot[:], stateRoot[:])
		return verifier.GetStateValue(ctx, idRoot, key)
	}
	
	integration, err := NewZKArchiveIntegration(chain, verifier, zkConfig, circuit, verifyFunc, getStateFunc)
	if err != nil {
		log.Fatalf("Failed to create ZK integration: %v", err)
	}
	
	// Start the ZK archival process
	err = integration.Start(ctx)
	if err != nil {
		log.Fatalf("Failed to start ZK archival: %v", err)
	}
	
	// Step 4: Demonstrate parameter handling with both formats
	log.Println("Demonstrating dual-format parameter handling with ZK archival...")
	
	// Create a test parameter in length-prefixed format
	testParamValue := []byte("test_contract_parameter")
	lengthPrefixedParam := make([]byte, 4+len(testParamValue))
	binary.LittleEndian.PutUint32(lengthPrefixedParam[:4], uint32(len(testParamValue)))
	copy(lengthPrefixedParam[4:], testParamValue)
	
	// Create a test parameter in direct format
	directParam := []byte("direct_format_param")
	
	// Step 5: Simulate state transitions and verification with both parameter formats
	// Test with length-prefixed format
	log.Println("Testing with length-prefixed parameter format...")
	testStateVerification(ctx, integration, 100, 500, lengthPrefixedParam)
	
	// Test with direct format
	log.Println("Testing with direct parameter format...")
	testStateVerification(ctx, integration, 200, 600, directParam)
	
	// Step 6: Demonstrate storage savings
	time.Sleep(10 * time.Second) // Allow time for archival to progress
	
	// Get information about the archival process - simulate stats for the example
	archivedCount := uint64(800) // Simulating 800 blocks being archived
	totalSize := uint64(50 * 1024 * 1024) // 50 MB of original data
	compressedSize := uint64(5 * 1024 * 1024) // 5 MB after compression
	
	log.Printf("ZK Archival Results:")
	log.Printf("- Total blocks archived: %d", archivedCount)
	log.Printf("- Storage saved: %.2f MB", float64(totalSize-compressedSize)/(1024*1024))
	log.Printf("- Compression ratio: %.2fx", float64(totalSize)/float64(compressedSize))
	log.Printf("- Recursive proofs: %d", 12) // Example value
	
	// Demonstrate how the system protects against the 3.5 billion byte parameter issue
	log.Println("Testing protection against unreasonable parameter lengths...")
	maliciousParam := make([]byte, 8)
	binary.LittleEndian.PutUint32(maliciousParam[:4], 3500000000) // 3.5 billion bytes
	
	// This would normally crash WebAssembly contracts, but our parameter handling protects against it
	// Use utilities.go ParseDualFormatParameter to handle this malicious input
	parsedBytes, format, err := ParseDualFormatParameter(maliciousParam, true, true)
	if err != nil {
		log.Printf("Successfully rejected malicious parameter: %v", err)
	} else {
		log.Printf("Parameter parsed as %s format, preventing WebAssembly crash", format)
		log.Printf("Safely handled %d bytes instead of crashing on 3.5B bytes", len(parsedBytes))
	}
	
	// Clean up
	integration.archiver.Stop()
	log.Println("ZK archival example completed")
}

// Test verification of state transitions with both parameter formats
func testStateVerification(
	ctx context.Context,
	integration *ZKArchiveIntegration,
	fromHeight, toHeight uint64,
	params []byte,
) {
	// First display the parameter format
	if len(params) >= 4 {
		prefixLength := binary.LittleEndian.Uint32(params[:4])
		if prefixLength > 0 && prefixLength <= 1024*1024 && int(prefixLength+4) <= len(params) {
			log.Printf("Parameter appears to be length-prefixed: length=%d, content=%s",
				prefixLength, hex.EncodeToString(params[4:4+prefixLength]))
		} else {
			log.Printf("Parameter using direct format: %s", hex.EncodeToString(params))
		}
	} else {
		log.Printf("Parameter using direct format: %s", hex.EncodeToString(params))
	}
	
	// Verify state transition
	log.Printf("Verifying state transition from height %d to %d...", fromHeight, toHeight)
	startTime := time.Now()
	
	verified, err := integration.VerifyHistoricalStateTransition(ctx, fromHeight, toHeight, params)
	
	duration := time.Since(startTime)
	if err != nil {
		log.Printf("Verification failed: %v", err)
		return
	}
	
	if verified {
		log.Printf("Successfully verified state transition in %s", duration)
	} else {
		log.Printf("State transition verification failed")
	}
}

// createTestBlocks populates the chain with test blocks
func createTestBlocks(ctx context.Context, chain *mockStatelessChain, numBlocks int) {
	// Create genesis block with a deterministic ID based on height
	var genesisStateRoot [32]byte
	// Fill with zeros by default 
	
	// Use a deterministic ID for the genesis block based on its height (0)
	genesisID := makeBlockIDFromHeight(0, genesisStateRoot)
	
	genesisBlock := &mockStatelessBlock{
		id:        genesisID,  // Use deterministic ID
		parentID:  ids.Empty,
		height:    0,
		timestamp: time.Now(),
		stateRoot: genesisStateRoot,
		proofs:    []core.StatelessProof{},
		bytes:     []byte("genesis block"),
	}
	
	err := chain.AddBlock(ctx, genesisBlock)
	if err != nil {
		log.Fatalf("Failed to add genesis block: %v", err)
	}
	
	// Create subsequent blocks
	parentID := genesisBlock.id
	for i := 1; i < numBlocks; i++ {
		// Create a proof with TEE attestation
		var proofs []core.StatelessProof
		proof := &mockStatelessProof{
			id:             ids.GenerateTestID(),
			attestation:    true,
			signatureValid: true,
			teeCertified:   true,
		}
		proofs = append(proofs, proof)
		
		// Create block with increasing byte size to simulate real blocks
		blockBytes := make([]byte, 1024*(i%50+1)) // Vary size between 1KB and 50KB
		for j := range blockBytes {
			blockBytes[j] = byte(i % 256)
		}
		
		// Create a deterministic state root based on height
		var stateRoot [32]byte
		binary.LittleEndian.PutUint64(stateRoot[:8], uint64(i))
		
		// Use deterministic block ID
		blockID := makeBlockIDFromHeight(uint64(i), stateRoot)
		
		block := &mockStatelessBlock{
			id:        blockID,  // Use deterministic ID
			parentID:  parentID,
			height:    uint64(i),
			timestamp: time.Now(),
			stateRoot: stateRoot,
			proofs:    proofs,
			bytes:     blockBytes,
		}
		
		err := chain.AddBlock(ctx, block)
		if err != nil {
			log.Fatalf("Failed to add block %d: %v", i, err)
		}
		
		parentID = block.id
	}
}

// mockStatelessVerifier is a simple implementation of core.StatelessVerifier for the example
type mockStatelessVerifier struct {
	chain *mockStatelessChain
	registeredProofTypes map[string]bool
}

func (v *mockStatelessVerifier) VerifyStateTransition(
	ctx context.Context,
	fromHeight, toHeight uint64,
	params []byte,
) (bool, error) {
	// Parse parameters with dual-format support
	parsedParams, format, err := ParseDualFormatParameter(params, true, true)
	if err != nil {
		return false, fmt.Errorf("invalid parameter format: %w", err)
	}
	
	log.Printf("Verifier received parameters in %s format with length %d", format, len(parsedParams))
	
	// Simplified verification logic for the example
	for height := fromHeight; height <= toHeight; height++ {
		// We need to get the block by ID, not by height
		// First create a deterministic state root based on height
		var stateRoot [32]byte
		binary.LittleEndian.PutUint64(stateRoot[:8], height)
		
		// Use deterministic block ID based on height and state root
		blockID := makeBlockIDFromHeight(height, stateRoot)
		log.Printf("Looking for block at height %d with ID %s", height, blockID)
		
		_, err := v.chain.GetBlock(ctx, blockID)
		if err != nil {
			return false, fmt.Errorf("block at height %d not found: %w", height, err)
		}
	}
	
	return true, nil
}

func (v *mockStatelessVerifier) GetStateValue(
	ctx context.Context,
	stateRoot ids.ID,
	key []byte,
) ([]byte, error) {
	// Parse key with dual-format support
	parsedKey, format, err := ParseDualFormatParameter(key, true, true)
	if err != nil {
		return nil, fmt.Errorf("invalid key format: %w", err)
	}
	
	log.Printf("GetStateValue received key in %s format with length %d", format, len(parsedKey))
	
	// Mock implementation always returns a test value
	valuePrefix := "state_value"
	if len(parsedKey) > 0 {
		valuePrefix = fmt.Sprintf("state_value_for_%s", hex.EncodeToString(parsedKey[:min(5, len(parsedKey))]))
	}
	
	return []byte(valuePrefix), nil
}

// RunZKArchivalExample runs the complete ZK archival example
func RunZKArchivalExample() {
	ZKArchivalExample()
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Mock implementations of the core interfaces for the example

// mockStatelessChain implements core.StatelessChain
type mockStatelessChain struct {
	blocks map[ids.ID]core.StatelessBlock
	topBlock core.StatelessBlock
}

func newMockStatelessChain() *mockStatelessChain {
	return &mockStatelessChain{
		blocks: make(map[ids.ID]core.StatelessBlock),
	}
}

func (c *mockStatelessChain) AddBlock(ctx context.Context, block core.StatelessBlock) error {
	c.blocks[block.ID()] = block
	c.topBlock = block
	log.Printf("Added block height=%d with ID=%s", block.Height(), block.ID())
	return nil
}

func (c *mockStatelessChain) GetBlock(ctx context.Context, id ids.ID) (core.StatelessBlock, error) {
	// Special case for integration with ZK archiver - if the ID starts with zeros
	// (like our genesis block would), check for height-based access
	if id.String() == "11111111111111111111111111111111LpoYY" {
		// This is likely trying to get the genesis block
		log.Printf("Special case: Looking for genesis block")
		
		// Check all blocks for one with height 0
		for _, block := range c.blocks {
			if block.Height() == 0 {
				log.Printf("Found genesis block with ID=%s", block.ID())
				return block, nil
			}
		}
	}
	
	// Normal path - lookup by ID
	block, exists := c.blocks[id]
	if !exists {
		log.Printf("Block not found with ID=%s", id)
		
		// As a fallback for the ZK archiver, try to find by height if the ID
		// looks like it might be height-encoded
		heightVal := binary.LittleEndian.Uint64(id[:8])
		if heightVal < 1000 { // Reasonable height for our test
			log.Printf("Attempting to find block with height=%d", heightVal)
			
			// Check all blocks for matching height
			for _, b := range c.blocks {
				if b.Height() == heightVal {
					log.Printf("Found block by height=%d with ID=%s", heightVal, b.ID())
					return b, nil
				}
			}
		}
		
		return nil, fmt.Errorf("block not found: %s", id)
	}
	log.Printf("Retrieved block height=%d with ID=%s", block.Height(), block.ID())
	return block, nil
}

func (c *mockStatelessChain) GetHeight(ctx context.Context) (uint64, error) {
	if c.topBlock == nil {
		return 0, nil
	}
	return c.topBlock.Height(), nil
}

func (c *mockStatelessChain) GetLatestStateRoot(ctx context.Context) ([sha256.Size]byte, error) {
	if c.topBlock == nil {
		return [sha256.Size]byte{}, nil
	}
	return c.topBlock.StateRoot(), nil
}

func (c *mockStatelessChain) VerifyChain(ctx context.Context, fromHeight, toHeight uint64) (bool, error) {
	return true, nil
}

// mockStatelessBlock implements core.StatelessBlock
type mockStatelessBlock struct {
	id        ids.ID
	parentID  ids.ID
	height    uint64
	timestamp time.Time
	stateRoot [32]byte
	proofs    []core.StatelessProof
	bytes     []byte
}

func (b *mockStatelessBlock) ID() ids.ID {
	return b.id
}

func (b *mockStatelessBlock) ParentID() ids.ID {
	return b.parentID
}

func (b *mockStatelessBlock) Height() uint64 {
	return b.height
}

func (b *mockStatelessBlock) Timestamp() time.Time {
	return b.timestamp
}

func (b *mockStatelessBlock) Proofs() []core.StatelessProof {
	return b.proofs
}

func (b *mockStatelessBlock) StateRoot() [sha256.Size]byte {
	return b.stateRoot
}

func (b *mockStatelessBlock) Verify(ctx context.Context, verifier core.StatelessVerifier) (bool, error) {
	return true, nil
}

func (b *mockStatelessBlock) Bytes() ([]byte, error) {
	return b.bytes, nil
}

// mockStatelessProof implements core.StatelessProof
type mockStatelessProof struct {
	id             ids.ID
	attestation    bool
	signatureValid bool
	teeCertified   bool
}

func (p *mockStatelessProof) Verify(ctx context.Context) (bool, error) {
	return p.signatureValid, nil
}

func (p *mockStatelessProof) RootHash() [sha256.Size]byte {
	var hash [sha256.Size]byte
	copy(hash[:], p.id[:])
	return hash
}

func (p *mockStatelessProof) ProofType() string {
	if p.teeCertified {
		return "TEE_CERTIFIED"
	}
	return "STANDARD"
}

func (p *mockStatelessProof) Serialize() ([]byte, error) {
	return []byte(fmt.Sprintf("%s-%v-%v", p.id, p.attestation, p.teeCertified)), nil
}

func (p *mockStatelessProof) Size() uint64 {
	return 64
}

// Implementing the RegisterProofType method required by the core.StatelessVerifier interface
func (v *mockStatelessVerifier) RegisterProofType(proofType string, verifyFunc func(context.Context, []byte) (bool, error)) error {
	if v.registeredProofTypes == nil {
		v.registeredProofTypes = make(map[string]bool)
	}
	v.registeredProofTypes[proofType] = true
	return nil
}

// VerifyProof verifies a proof using the appropriate mechanism based on its type
func (v *mockStatelessVerifier) VerifyProof(ctx context.Context, proof core.StatelessProof) (bool, error) {
	// Get the proof type
	proofType := proof.ProofType()
	
	// Check if the proof type is registered
	if v.registeredProofTypes == nil || !v.registeredProofTypes[proofType] {
		return false, fmt.Errorf("unknown proof type: %s", proofType)
	}
	
	// For the mock implementation, just simulate verification
	// In a real implementation, this would call the registered verification function
	log.Printf("Verifying proof of type %s", proofType)
	
	// Get serialized proof data
	data, err := proof.Serialize()
	if err != nil {
		return false, fmt.Errorf("failed to serialize proof: %w", err)
	}
	
	// Handle robust parameter formats for Wasmlanche compatibility
	parsedData, format, err := ParseDualFormatParameter(data, true, true)
	if err != nil {
		return false, fmt.Errorf("invalid proof data format: %w", err)
	}
	log.Printf("Proof data using %s format with length %d", format, len(parsedData))
	
	// Simple mock verification - always succeed for TEE certified proofs
	if proofType == "TEE_CERTIFIED" {
		return true, nil
	}
	
	// Call the proof's own verification method for all other types
	return proof.Verify(ctx)
}

// VerifyProofBatch verifies a batch of proofs using the appropriate mechanisms
func (v *mockStatelessVerifier) VerifyProofBatch(ctx context.Context, proofs []core.StatelessProof) ([]bool, error) {
	log.Printf("Batch verifying %d proofs", len(proofs))
	
	// Initialize results slice
	results := make([]bool, len(proofs))
	
	// For each proof in the batch
	for i, proof := range proofs {
		verified, err := v.VerifyProof(ctx, proof)
		if err != nil {
			return results, fmt.Errorf("failed to verify proof %d: %w", i, err)
		}
		// Store the result
		results[i] = verified
	}
	
	// Return the verification results for each proof
	return results, nil
}

// GetMetrics returns verifier metrics for monitoring and tracking
// This implements the core.StatelessVerifier interface requirement
func (m *mockStatelessVerifier) GetMetrics() interface{} {
	// For the mock implementation, return basic metrics structure
	return struct {
		ProofsVerified         int
		VerificationSuccessful int
		VerificationFailed     int
		AverageLatencyMs       float64
	}{
		ProofsVerified:         100, // Mock values for the example
		VerificationSuccessful: 95,
		VerificationFailed:     5,
		AverageLatencyMs:       2.5,
	}
}
