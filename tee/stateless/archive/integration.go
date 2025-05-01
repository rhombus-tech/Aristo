// Copyright (C) 2025, Rhombus Technologies. All rights reserved.
// See the file LICENSE for licensing terms.

package archive

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"time"

	"github.com/rhombus-tech/vm/tee/stateless/core"
)

// ZKArchiveIntegration connects the ZK archival system with the existing
// stateless verification layer while preserving the robust dual-format parameter handling
type ZKArchiveIntegration struct {
	// The core stateless chain implementation
	chain core.StatelessChain
	
	// The ZK archival service
	archiver *ZKArchiver
	
	// The verifier for validating blocks and proofs
	verifier core.StatelessVerifier
	
	// Helper methods for state access and verification
	verifyTransition func(ctx context.Context, fromHeight, toHeight uint64, params []byte) (bool, error)
	getStateValue    func(ctx context.Context, stateRoot [32]byte, key []byte) ([]byte, error)
}

// NewZKArchiveIntegration creates a new integration between the ZK archival system
// and the existing stateless verification layer
func NewZKArchiveIntegration(
	chain core.StatelessChain,
	verifier core.StatelessVerifier,
	config ZKArchiveConfig,
	circuit ZKCircuit,
	verifyFunc func(ctx context.Context, fromHeight, toHeight uint64, params []byte) (bool, error),
	getStateFunc func(ctx context.Context, stateRoot [32]byte, key []byte) ([]byte, error),
) (*ZKArchiveIntegration, error) {
	// Create the ZK archiver
	archiver, err := NewZKArchiver(config, chain, circuit)
	if err != nil {
		return nil, fmt.Errorf("failed to create ZK archiver: %w", err)
	}
	
	return &ZKArchiveIntegration{
		chain:            chain,
		archiver:         archiver,
		verifier:         verifier,
		verifyTransition: verifyFunc,
		getStateValue:    getStateFunc,
	}, nil
}

// Start begins the ZK archival process
func (i *ZKArchiveIntegration) Start(ctx context.Context) error {
	return i.archiver.Start(ctx)
}

// Stop ends the ZK archival process
func (i *ZKArchiveIntegration) Stop() {
	i.archiver.Stop()
}

// VerifyHistoricalStateTransition verifies a historical state transition
// using either ZK proofs (if available) or direct verification
func (i *ZKArchiveIntegration) VerifyHistoricalStateTransition(
	ctx context.Context,
	fromHeight,
	toHeight uint64,
	params []byte,
) (bool, error) {
	// First try to verify using ZK proofs (much more efficient)
	verified, err := i.archiver.VerifyHistoricalTransition(ctx, fromHeight, toHeight)
	if err == nil && verified {
		log.Printf("Successfully verified state transition %d->%d using ZK proofs", 
			fromHeight, toHeight)
		return true, nil
	}
	
	log.Printf("ZK verification failed or not available for %d->%d, falling back to direct verification",
		fromHeight, toHeight)
	
	// If ZK verification fails or is not available, fall back to direct verification
	// using the existing stateless verifier
	
	// Parameters might be in either format (length-prefixed or direct)
	// Use the robust dual-format parameter handling already implemented
	var parsedParams []byte
	var format string
	
	// First check if it's length-prefixed
	if len(params) >= 4 {
		length := binary.LittleEndian.Uint32(params[:4])
		// Validate reasonable length
		if length > 0 && length <= 1024*1024 && int(length+4) <= len(params) {
			parsedParams = params[4:4+length]
			format = "length-prefixed"
		}
	}
	
	// If not length-prefixed, use direct format
	if parsedParams == nil {
		parsedParams = params
		format = "direct"
	}
	
	log.Printf("Using %s parameter format for direct verification", format)
	
	// Perform direct verification using the callback function
	return i.verifyTransition(ctx, fromHeight, toHeight, parsedParams)
}

// QueryHistoricalState queries the historical state at a given height
// using the most efficient method available (ZK proofs or direct access)
func (i *ZKArchiveIntegration) QueryHistoricalState(
	ctx context.Context,
	height uint64,
	key []byte,
) ([]byte, error) {
	// Get the current chain height
	currentHeight, err := i.chain.GetHeight(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get current chain height: %w", err)
	}
	
	// Key might be in either format (length-prefixed or direct)
	// Use the robust dual-format parameter handling
	var parsedKey []byte
	var format string
	
	// First check if it's length-prefixed
	if len(key) >= 4 {
		length := binary.LittleEndian.Uint32(key[:4])
		// Validate reasonable length
		if length > 0 && length <= 1024*1024 && int(length+4) <= len(key) {
			parsedKey = key[4:4+length]
			format = "length-prefixed"
		}
	}
	
	// If not length-prefixed, use direct format
	if parsedKey == nil {
		parsedKey = key
		format = "direct"
	}
	
	log.Printf("Using %s key format for historical state query", format)
	
	// If the requested height is close to the current height, just access it directly
	if currentHeight - height < 100 {
		// Direct access for recent history
		return i.queryDirectHistoricalState(ctx, height, parsedKey)
	}
	
	// For older history, try using a ZK-based approach
	return i.queryZKHistoricalState(ctx, height, parsedKey)
}

// queryDirectHistoricalState queries historical state directly from blocks
func (i *ZKArchiveIntegration) queryDirectHistoricalState(
	ctx context.Context,
	height uint64,
	key []byte,
) ([]byte, error) {
	// Get the block at the target height by creating a block ID from the height
	// This is a workaround since our interface expects GetBlock by ID
	blocks, err := i.getAllBlocks(ctx)
	if err != nil {
		return nil, err
	}
	
	// Find the block with the specified height
	var targetBlock core.StatelessBlock
	for _, block := range blocks {
		if block.Height() == height {
			targetBlock = block
			break
		}
	}
	
	if targetBlock == nil {
		return nil, fmt.Errorf("failed to get block at height %d", height)
	}
	
	// Use the callback function to access state at this height
	return i.getStateValue(ctx, targetBlock.StateRoot(), key)
}

// queryZKHistoricalState queries historical state using ZK proofs as a guide
func (i *ZKArchiveIntegration) queryZKHistoricalState(
	ctx context.Context,
	height uint64,
	key []byte,
) ([]byte, error) {
	// Find the closest archived reference state
	refHeight := i.archiver.findClosestReferenceState(height)
	
	// Get all blocks to find the one with refHeight
	blocks, err := i.getAllBlocks(ctx)
	if err != nil {
		return nil, err
	}
	
	// Find the block with refHeight
	var refBlock core.StatelessBlock
	for _, block := range blocks {
		if block.Height() == refHeight {
			refBlock = block
			break
		}
	}
	
	if refBlock == nil {
		return nil, fmt.Errorf("failed to get reference block at height %d", refHeight)
	}
	
	// If the reference state is exactly what we want, use it directly
	if refHeight == height {
		return i.getStateValue(ctx, refBlock.StateRoot(), key)
	}
	
	// Otherwise, we need to:
	// 1. Verify the state transition from refHeight to height using ZK proofs
	// 2. Then directly query the value at height
	
	verified, err := i.archiver.VerifyHistoricalTransition(ctx, refHeight, height)
	if err != nil || !verified {
		return nil, fmt.Errorf("failed to verify transition from reference height %d to target height %d: %w",
			refHeight, height, err)
	}
	
	// Now we can safely query the target height
	return i.queryDirectHistoricalState(ctx, height, key)
}

// GetCompressionStats returns statistics about the storage compression achieved
func (i *ZKArchiveIntegration) GetCompressionStats() CompressionStats {
	metrics := i.archiver.GetMetrics()
	
	return CompressionStats{
		TotalBlocksArchived:     metrics.TotalBlocksArchived,
		TotalStorageSavedBytes:  i.archiver.GetTotalStorageSaved(),
		CompressionRatio:        metrics.CompressionRatio,
		VerificationLatencyMs:   metrics.VerificationLatencyMs,
		RecursiveProofsGenerated: metrics.RecursiveProofsGenerated,
	}
}

// CompressionStats contains statistics about the ZK compression system
type CompressionStats struct {
	TotalBlocksArchived     uint64
	TotalStorageSavedBytes  uint64
	CompressionRatio        float64
	VerificationLatencyMs   uint64
	RecursiveProofsGenerated uint64
}

// Helper method to get all blocks from the chain
func (i *ZKArchiveIntegration) getAllBlocks(ctx context.Context) ([]core.StatelessBlock, error) {
	// Get the current height
	height, err := i.chain.GetHeight(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get chain height: %w", err)
	}
	
	// Collect all blocks
	var blocks []core.StatelessBlock
	for h := uint64(0); h <= height; h++ {
		// This is a bit inefficient but necessary to work with the interface
		latestRoot, err := i.chain.GetLatestStateRoot(ctx)
		if err != nil {
			continue
		}
		
		// Try to find block using its ID based on height data
		blockID := makeBlockIDFromHeight(h, latestRoot)
		block, err := i.chain.GetBlock(ctx, blockID)
		if err == nil {
			blocks = append(blocks, block)
		}
	}
	
	return blocks, nil
}

// Uses makeBlockIDFromHeight from utilities.go

// CreateZKArchivedVerificationLayer creates a new verification layer that incorporates
// ZK-based state archival while preserving the existing robust dual-format parameter handling
func CreateZKArchivedVerificationLayer(
	ctx context.Context, 
	chain core.StatelessChain,
	verifier core.StatelessVerifier,
	zkConfig ZKArchiveConfig,
	verifyFunc func(ctx context.Context, fromHeight, toHeight uint64, params []byte) (bool, error),
	getStateFunc func(ctx context.Context, stateRoot [32]byte, key []byte) ([]byte, error),
) (*ZKArchiveIntegration, error) {
	// Create a mock circuit for now - in production, you'd use a real ZK library
	circuit := NewMockZKCircuit()
	
	// Create the integration layer
	integration, err := NewZKArchiveIntegration(chain, verifier, zkConfig, circuit, verifyFunc, getStateFunc)
	if err != nil {
		return nil, err
	}
	
	// Start the archival process
	err = integration.Start(ctx)
	if err != nil {
		return nil, err
	}
	
	log.Printf("Started ZK-archived verification layer with batch size %d and %d recursive levels",
		zkConfig.BatchSize, zkConfig.RecursiveProofLevels)
	
	// Perform an initial archival run to capture existing chain state
	go func() {
		// Wait a bit to ensure everything is initialized
		time.Sleep(5 * time.Second)
		
		// Log the initial compression stats
		stats := integration.GetCompressionStats()
		log.Printf("Initial ZK archival stats: %+v", stats)
	}()
	
	return integration, nil
}
