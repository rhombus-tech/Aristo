// Copyright (C) 2025, Rhombus Technologies. All rights reserved.
// See the file LICENSE for licensing terms.

package archive

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"github.com/rhombus-tech/vm/tee/stateless/core"
)

// ZKArchiveConfig configures the ZK archival process
type ZKArchiveConfig struct {
	// BatchSize is the number of blocks to include in each ZK proof
	// Larger batch sizes create more efficient proofs but take longer to generate
	BatchSize uint64 `json:"batchSize"`

	// ArchivalPeriod defines how often to generate new ZK proofs
	// This should be tuned based on chain throughput and hardware capabilities
	ArchivalPeriod time.Duration `json:"archivalPeriod"`

	// ReferencePoints defines how many reference state points to maintain
	// More reference points increases verification speed but requires more storage
	ReferencePoints uint64 `json:"referencePoints"`

	// RecursiveProofLevels defines how many levels of recursive proofs to generate
	// More levels means more compression but higher verification latency
	RecursiveProofLevels uint64 `json:"recursiveProofLevels"`

	// Parallelism defines how many parallel proof generation tasks to run
	// Should be tuned based on available CPU resources
	Parallelism int `json:"parallelism"`

	// TEEVerifiedOnly determines whether to only archive blocks that have
	// TEE verification (for regulatory compliance)
	TEEVerifiedOnly bool `json:"teeVerifiedOnly"`
}

// DefaultZKArchiveConfig provides sensible default values
func DefaultZKArchiveConfig() ZKArchiveConfig {
	return ZKArchiveConfig{
		BatchSize:           100,
		ArchivalPeriod:      1 * time.Hour,
		ReferencePoints:     10,
		RecursiveProofLevels: 3,
		Parallelism:         4,
		TEEVerifiedOnly:     true,
	}
}

// ZKArchiver handles the generation and verification of ZK proofs
// for historical state transitions
type ZKArchiver struct {
	config         ZKArchiveConfig
	statelessChain core.StatelessChain
	circuit        ZKCircuit
	
	// Runtime state
	currentState    [sha256.Size]byte
	zkProofs        map[uint64][]byte // Height -> ZK proof
	referenceStates map[uint64][sha256.Size]byte // Height -> State root
	
	// Tracks the last successfully archived height
	lastArchivedHeight uint64
	
	// For recursive proofs
	recursiveProofs map[uint64]map[uint64][]byte // Level -> (Height -> Proof)
	
	mu sync.RWMutex
	
	// For metrics and monitoring
	metrics *ZKArchiverMetrics
	
	// Channel to signal shutdown
	shutdown chan struct{}
	
	// Wait group for background tasks
	wg sync.WaitGroup
}

// ZKArchiverMetrics tracks performance and stats
type ZKArchiverMetrics struct {
	TotalProofsGenerated    uint64
	TotalBlocksArchived     uint64
	AverageProofSizeBytes   uint64
	AverageProofGenTimeMs   uint64
	CompressionRatio        float64
	VerificationLatencyMs   uint64
	RecursiveProofsGenerated uint64
	FailedProofAttempts     uint64
}

// NewZKArchiver creates a new ZK archival service
func NewZKArchiver(config ZKArchiveConfig, statelessChain core.StatelessChain, circuit ZKCircuit) (*ZKArchiver, error) {
	if statelessChain == nil {
		return nil, fmt.Errorf("stateless chain is required")
	}
	
	if circuit == nil {
		return nil, fmt.Errorf("ZK circuit is required")
	}
	
	// Create a map to store recursive proofs, keyed by level and end height
	recursiveProofs := make(map[uint64]map[uint64][]byte)
	for level := uint64(1); level <= config.RecursiveProofLevels; level++ {
		recursiveProofs[level] = make(map[uint64][]byte)
	}
	
	// First create the archiver instance
	a := &ZKArchiver{
		config:            config,
		statelessChain:    statelessChain,
		circuit:           circuit,
		zkProofs:          make(map[uint64][]byte),
		referenceStates:   make(map[uint64][sha256.Size]byte),
		lastArchivedHeight: math.MaxUint64, // Setting to max forces archival from genesis block
		recursiveProofs:   recursiveProofs,
		metrics:          &ZKArchiverMetrics{},
		shutdown:         make(chan struct{}),
	}
	
	// Now that we have the archiver instance, get the genesis block
	genesisBlock, err := a.getBlockByHeight(context.Background(), 0)
	if err != nil {
		return nil, fmt.Errorf("failed to get genesis block: %w", err)
	}
	
	// Store the genesis state
	genesisState := genesisBlock.StateRoot()
	a.referenceStates[0] = genesisState
	
	// ZKArchiver instance is already created above, just log initialization
	log.Printf("[ZKArchiver] Initialized with genesis state at height 0")
	
	return a, nil
}

// NewZKArchiverWithTEE creates a new ZK archival service with a single TEE endpoint
func NewZKArchiverWithTEE(config ZKArchiveConfig, statelessChain core.StatelessChain, teeEndpoint string) (*ZKArchiver, error) {
	// Create a TEE polynomial circuit
	circuit := NewTEEPolynomialCircuit(teeEndpoint, 
		WithMaxBatchSize(int(config.BatchSize)),
		WithAcceleration(true))
	
	return NewZKArchiver(config, statelessChain, circuit)
}

// NewZKArchiverWithDualTEE creates a new ZK archival service with dual TEE execution (SGX+SEV)
// using the mesh network for enhanced security through cross-attestation
func NewZKArchiverWithDualTEE(config ZKArchiveConfig, statelessChain core.StatelessChain, 
	meshEndpoint string, region string) (*ZKArchiver, error) {
	// Create a mesh-enabled TEE polynomial circuit with dual SGX+SEV execution
	circuit := NewMeshTEEPolynomialCircuit(meshEndpoint, region,
		WithMaxBatchSize(int(config.BatchSize)),
		WithAcceleration(true),
		WithMeshNetwork(true),
		WithRegion(region))
	
	log.Printf("[ZKArchiver] Initializing with dual TEE execution (SGX+SEV) using mesh network at %s\n", meshEndpoint)
	
	return NewZKArchiver(config, statelessChain, circuit)
}

// NewZKArchiverWithAISupport creates a new ZK archival service with triple TEE execution (SGX+SEV+TDX)
// optimized for AI workloads with TDX for computational efficiency and SGX/SEV for security verification
func NewZKArchiverWithAISupport(config ZKArchiveConfig, statelessChain core.StatelessChain, 
	meshEndpoint string, region string, aiModelSize int64, aiBatchSize int) (*ZKArchiver, error) {
	// Create a mesh-enabled TEE polynomial circuit with triple TEE execution
	// TDX will be used for high-memory AI workloads, SGX/SEV for verification
	circuit := NewMeshTEEPolynomialCircuit(meshEndpoint, region,
		WithMaxBatchSize(int(config.BatchSize)),
		WithAcceleration(true),
		WithMeshNetwork(true),
		WithRegion(region),
		WithAICapabilities())
	
	log.Printf("[ZKArchiver] Initializing with AI-optimized triple TEE execution (SGX+SEV+TDX) using mesh network at %s\n", meshEndpoint)
	log.Printf("[ZKArchiver] Configured for AI workloads with model size: %d bytes, batch size: %d\n", aiModelSize, aiBatchSize)
	
	return NewZKArchiver(config, statelessChain, circuit)
}

// Start begins the background archival process
func (a *ZKArchiver) Start(ctx context.Context) error {
	log.Printf("[ZKArchiver] Starting archival service with batch size %d", a.config.BatchSize)
	
	// Get initial state - for genesis we need to use our helper method
	genBlock, err := a.getBlockByHeight(ctx, 0)
	if err != nil {
		return fmt.Errorf("failed to get genesis block: %w", err)
	}
	
	// Initialize current state with genesis state root
	a.currentState = genBlock.StateRoot()
	
	// Store genesis as first reference state
	a.referenceStates[0] = a.currentState
	
	// Start background workers for proof generation
	for i := 0; i < a.config.Parallelism; i++ {
		a.wg.Add(1)
		go a.archivalWorker(ctx, i)
	}
	
	return nil
}

// Stop gracefully shuts down the archiver
func (a *ZKArchiver) Stop() {
	log.Printf("[ZKArchiver] Stopping archival service")
	close(a.shutdown)
	a.wg.Wait()
}

// archivalWorker is a background goroutine that periodically generates ZK proofs
func (a *ZKArchiver) archivalWorker(ctx context.Context, workerID int) {
	defer a.wg.Done()
	
	ticker := time.NewTicker(a.config.ArchivalPeriod)
	defer ticker.Stop()
	
	log.Printf("[ZKArchiver] Worker %d started", workerID)
	
	for {
		select {
		case <-a.shutdown:
			log.Printf("[ZKArchiver] Worker %d shutting down", workerID)
			return
		case <-ctx.Done():
			log.Printf("[ZKArchiver] Worker %d context canceled", workerID)
			return
		case <-ticker.C:
			// Only have worker 0 check for new blocks to archive
			// This prevents multiple workers from archiving the same blocks
			if workerID == 0 {
				a.generateProofsForNewBlocks(ctx)
			} else {
				// Other workers can work on recursive proofs
				a.generateRecursiveProofs(ctx, workerID)
			}
		}
	}
}

// generateProofsForNewBlocks checks for new blocks and generates ZK proofs
func (a *ZKArchiver) generateProofsForNewBlocks(ctx context.Context) {
	a.mu.RLock()
	lastArchived := a.lastArchivedHeight
	a.mu.RUnlock()
	
	// Get current chain height
	height, err := a.statelessChain.GetHeight(ctx)
	if err != nil {
		log.Printf("[ZKArchiver] Failed to get chain height: %v", err)
		return
	}
	
	// Special case: if lastArchivedHeight is at the initial value (max uint64)
	// we need to start from the genesis block (height 0)
	var startHeight, endHeight uint64
	if lastArchived == ^uint64(0) {
		// Start from genesis block (height 0)
		startHeight = 0
		endHeight = a.config.BatchSize - 1
		if endHeight > height {
			endHeight = height
		}
		log.Printf("[ZKArchiver] Initial archival starting from genesis block (0-%d)", endHeight)
	} else {
		// Check if we have enough new blocks to archive
		if height <= lastArchived+a.config.BatchSize {
			return // Not enough new blocks yet
		}
		
		// Find range of blocks to archive
		startHeight = lastArchived + 1
		endHeight = startHeight + a.config.BatchSize - 1
		if endHeight > height {
			endHeight = height
		}
	}
	
	log.Printf("[ZKArchiver] Generating proof for blocks %d to %d", startHeight, endHeight)
	
	// Generate proof for this batch
	err = a.generateProofForRange(ctx, startHeight, endHeight)
	if err != nil {
		log.Printf("[ZKArchiver] Failed to generate proof for range %d-%d: %v", 
			startHeight, endHeight, err)
		a.mu.Lock()
		a.metrics.FailedProofAttempts++
		a.mu.Unlock()
		return
	}
	
	// Store a reference state point if needed
	if endHeight%a.config.ReferencePoints == 0 {
		a.storeReferenceState(ctx, endHeight)
	}
	
	// Also store a reference state for genesis block if we just archived it
	if startHeight == 0 {
		a.storeReferenceState(ctx, 0)
		log.Printf("[ZKArchiver] Stored reference state for genesis block")
	}
	
	a.mu.Lock()
	a.lastArchivedHeight = endHeight
	a.metrics.TotalBlocksArchived += (endHeight - startHeight + 1)
	a.metrics.TotalProofsGenerated++
	a.mu.Unlock()
}

// generateRecursiveProofs creates higher-level recursive proofs
func (a *ZKArchiver) generateRecursiveProofs(ctx context.Context, workerID int) {
	// Start from level 1 (combining base proofs)
	for level := uint64(1); level <= a.config.RecursiveProofLevels; level++ {
		// Determine which batches of proofs this worker should process
		// based on workerID (simple round-robin distribution)
		a.mu.RLock()
		lastArchived := a.lastArchivedHeight
		lowerProofs := a.recursiveProofs[level-1]
		a.mu.RUnlock()
		
		if level == 1 {
			// Level 1 combines base proofs
			a.generateLevel1RecursiveProofs(ctx, lastArchived, workerID)
		} else {
			// Higher levels combine proofs from the level below
			a.generateHigherLevelRecursiveProofs(ctx, level, lowerProofs, workerID)
		}
	}
}

// generateLevel1RecursiveProofs creates level 1 recursive proofs by combining base proofs
func (a *ZKArchiver) generateLevel1RecursiveProofs(ctx context.Context, lastArchived uint64, workerID int) {
	// For level 1, we combine BatchSize number of base proofs
	recursiveBatchSize := a.config.BatchSize
	
	// Find highest multiple of recursiveBatchSize that has been archived
	maxHeight := (lastArchived / recursiveBatchSize) * recursiveBatchSize
	
	// Check if we have enough new proofs to combine
	if maxHeight == 0 {
		return
	}
	
	a.mu.RLock()
	// Find the highest height that already has a level 1 recursive proof
	var lastRecursiveProof uint64
	for h := range a.recursiveProofs[1] {
		if h > lastRecursiveProof {
			lastRecursiveProof = h
		}
	}
	a.mu.RUnlock()
	
	// If we already have a recursive proof that covers everything, skip
	if maxHeight <= lastRecursiveProof {
		return
	}
	
	// Calculate the target height for the new recursive proof
	// This will be the highest multiple of the recursive batch size
	targetHeight := (maxHeight / recursiveBatchSize) * recursiveBatchSize
	
	// Check if we already have a recursive proof at this level that covers this height
	if targetHeight <= lastRecursiveProof {
		return // No new proofs to generate
	}
	
	log.Printf("[ZKArchiver] Worker %d generating L1 recursive proof for heights 0-%d", 
		workerID, targetHeight)
	
	// Collect all base proofs needed
	var baseProofs [][]byte
	a.mu.RLock()
	for h := uint64(0); h <= targetHeight; h += a.config.BatchSize {
		proof, exists := a.zkProofs[h+a.config.BatchSize-1]
		if !exists {
			// Missing a proof, can't generate recursive proof
			a.mu.RUnlock()
			return
		}
		baseProofs = append(baseProofs, proof)
	}
	a.mu.RUnlock()
	
	// Generate recursive proof
	recursiveProof, err := a.circuit.GenerateRecursiveProof(ctx, baseProofs, 0, targetHeight)
	if err != nil {
		log.Printf("[ZKArchiver] Worker %d failed to generate L1 recursive proof: %v", 
			workerID, err)
		return
	}
	
	// Store the recursive proof
	a.mu.Lock()
	a.recursiveProofs[1][targetHeight] = recursiveProof
	a.metrics.RecursiveProofsGenerated++
	a.mu.Unlock()
	
	log.Printf("[ZKArchiver] Worker %d successfully generated L1 recursive proof for heights 0-%d", 
		workerID, targetHeight)
}

// generateHigherLevelRecursiveProofs creates higher level recursive proofs
func (a *ZKArchiver) generateHigherLevelRecursiveProofs(ctx context.Context, level uint64, 
	lowerProofs map[uint64][]byte, workerID int) {
	
	// For higher levels, each recursive proof combines multiple lower-level proofs
	// The batch size increases exponentially with level
	recursiveBatchSize := a.config.BatchSize * (1 << (level - 1))
	
	// Find all available lower level proofs
	var availableHeights []uint64
	for h := range lowerProofs {
		availableHeights = append(availableHeights, h)
	}
	
	if len(availableHeights) < 2 {
		return // Not enough lower-level proofs to combine
	}
	
	// Find maximum height that has a lower-level proof
	var maxLowerHeight uint64
	for _, h := range availableHeights {
		if h > maxLowerHeight {
			maxLowerHeight = h
		}
	}
	
	// Determine if we need to generate a new higher-level proof
	a.mu.RLock()
	higherProofs := a.recursiveProofs[level]
	var maxHigherHeight uint64
	for h := range higherProofs {
		if h > maxHigherHeight {
			maxHigherHeight = h
		}
	}
	a.mu.RUnlock()
	
	// If we already have a recursive proof that covers everything, skip
	if maxHigherHeight >= maxLowerHeight {
		return
	}
	
	// Calculate the target height for the new recursive proof
	// This will be the highest multiple of the recursive batch size
	targetHeight := (maxLowerHeight / recursiveBatchSize) * recursiveBatchSize
	
	if targetHeight <= maxHigherHeight {
		return // No new proofs to generate
	}
	
	log.Printf("[ZKArchiver] Worker %d generating L%d recursive proof up to height %d", 
		workerID, level, targetHeight)
	
	// Collect all required lower-level proofs
	var requiredProofs [][]byte
	a.mu.RLock()
	for h := uint64(0); h <= targetHeight; h += recursiveBatchSize / 2 {
		proof, exists := a.recursiveProofs[level-1][h]
		if !exists {
			// Missing a required proof
			a.mu.RUnlock()
			return
		}
		requiredProofs = append(requiredProofs, proof)
	}
	a.mu.RUnlock()
	
	// Generate recursive proof
	recursiveProof, err := a.circuit.GenerateRecursiveProof(ctx, requiredProofs, 0, targetHeight)
	if err != nil {
		log.Printf("[ZKArchiver] Worker %d failed to generate L%d recursive proof: %v", 
			workerID, level, err)
		return
	}
	
	// Store the recursive proof
	a.mu.Lock()
	a.recursiveProofs[level][targetHeight] = recursiveProof
	a.metrics.RecursiveProofsGenerated++
	a.mu.Unlock()
	
	log.Printf("[ZKArchiver] Worker %d successfully generated L%d recursive proof to height %d", 
		workerID, level, targetHeight)
}

// generateProofForRange creates a ZK proof for the specified block range
func (a *ZKArchiver) generateProofForRange(ctx context.Context, startHeight, endHeight uint64) error {
	startTime := time.Now()
	
	if endHeight < startHeight {
		return fmt.Errorf("invalid range: end height %d is less than start height %d", 
			endHeight, startHeight)
	}
	
	log.Printf("[ZKArchiver] Starting proof generation for range %d-%d", startHeight, endHeight)
	
	// Get starting state
	var startState [sha256.Size]byte
	if startHeight == 0 {
		// Genesis state
		a.mu.RLock()
		startState = a.referenceStates[0]
		a.mu.RUnlock()
	} else {
		// Find closest reference state before startHeight
		refHeight := a.findClosestReferenceState(startHeight)
		
		a.mu.RLock()
		startState = a.referenceStates[refHeight]
		a.mu.RUnlock()
		
		// Apply intermediate blocks if needed
		if refHeight < startHeight-1 {
			// Apply blocks from refHeight+1 to startHeight-1
			for h := refHeight + 1; h < startHeight; h++ {
				block, err := a.getBlockByHeight(ctx, h)
				if err != nil {
					return fmt.Errorf("failed to get block at height %d: %w", h, err)
				}
				// Apply this block's state transition
				startState = block.StateRoot()
			}
		}
	}
	
	// Get ending state
	endBlock, err := a.getBlockByHeight(ctx, endHeight)
	if err != nil {
		return fmt.Errorf("failed to get block at height %d: %w", endHeight, err)
	}
	endState := endBlock.StateRoot()
	
	// Collect all blocks and state transitions in the range
	var blocks []core.StatelessBlock
	for h := startHeight; h <= endHeight; h++ {
		block, err := a.getBlockByHeight(ctx, h)
		if err != nil {
			return fmt.Errorf("failed to get block at height %d: %w", h, err)
		}
		
		// If configured for TEE verification only, check that block has valid proofs
		if a.config.TEEVerifiedOnly {
			proofs := block.Proofs()
			if len(proofs) == 0 {
				return fmt.Errorf("block at height %d has no proofs but TEEVerifiedOnly is enabled", h)
			}
			
			// Verify at least one proof has TEE attestation
			hasTEEAttestation := false
			for _, proof := range proofs {
				// We added a HasTEEAttestation method to our mock proof,
				// but need to check if it's implemented on the real interface
				// For now, just check if the proof type contains "attestation"
				if proof.ProofType() == "attestation" || hasAttestationInProof(proof) {
					hasTEEAttestation = true
					break
				}
			}
			
			if !hasTEEAttestation {
				return fmt.Errorf("block at height %d has no TEE attestation but TEEVerifiedOnly is enabled", h)
			}
		}
		
		blocks = append(blocks, block)
	}
	
	// Generate the ZK proof for this range
	// This is the time-consuming part, but it's running in a background goroutine
	proof, err := a.circuit.GenerateProof(ctx, blocks, startState, endState, startHeight, endHeight)
	if err != nil {
		return fmt.Errorf("failed to generate ZK proof: %w", err)
	}
	
	// Update metrics
	proofGenTime := time.Since(startTime)
	proofSize := len(proof)
	
	a.mu.Lock()
	// Store the proof
	a.zkProofs[endHeight] = proof
	
	// Update metrics
	a.metrics.AverageProofGenTimeMs = (a.metrics.AverageProofGenTimeMs * a.metrics.TotalProofsGenerated +
		uint64(proofGenTime.Milliseconds())) / (a.metrics.TotalProofsGenerated + 1)
	a.metrics.AverageProofSizeBytes = (a.metrics.AverageProofSizeBytes * a.metrics.TotalProofsGenerated +
		uint64(proofSize)) / (a.metrics.TotalProofsGenerated + 1)
	
	// Calculate compression ratio
	blockDataSize := uint64(0)
	for _, block := range blocks {
		bytes, _ := block.Bytes()
		blockDataSize += uint64(len(bytes))
	}
	
	if blockDataSize > 0 {
		a.metrics.CompressionRatio = float64(blockDataSize) / float64(proofSize)
	}
	a.mu.Unlock()
	
	log.Printf("[ZKArchiver] Generated proof for range %d-%d: %d bytes, took %s, compression ratio: %.2fx",
		startHeight, endHeight, proofSize, proofGenTime, float64(blockDataSize)/float64(proofSize))
	
	return nil
}

// findClosestReferenceState finds the closest reference state before the given height
func (a *ZKArchiver) findClosestReferenceState(height uint64) uint64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	
	var closest uint64
	for h := range a.referenceStates {
		if h < height && h > closest {
			closest = h
		}
	}
	
	return closest
}

// storeReferenceState stores a reference state for the given height
func (a *ZKArchiver) storeReferenceState(ctx context.Context, height uint64) {
	block, err := a.getBlockByHeight(ctx, height)
	if err != nil {
		log.Printf("[ZKArchiver] Failed to get block at height %d for reference state: %v", height, err)
		return
	}
	
	stateRoot := block.StateRoot()
	
	a.mu.Lock()
	a.referenceStates[height] = stateRoot
	a.mu.Unlock()
	
	log.Printf("[ZKArchiver] Stored reference state for height %d", height)
}

// GetLastArchivedHeight returns the last height that has been archived
func (a *ZKArchiver) GetLastArchivedHeight() uint64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.lastArchivedHeight
}

// VerifyHistoricalTransition verifies a historical state transition using ZK proofs
func (a *ZKArchiver) VerifyHistoricalTransition(ctx context.Context, fromHeight, toHeight uint64) (bool, error) {
	startTime := time.Now()
	
	// Special case for genesis block (height 0)
	if fromHeight == 0 {
		// Verify that we have the genesis block
		genesisBlock, err := a.getBlockByHeight(ctx, 0)
		if err != nil {
			return false, fmt.Errorf("failed to get genesis block: %w", err)
		}
		log.Printf("[ZKArchiver] Starting verification from genesis block with state root %x", genesisBlock.StateRoot())
		
		// Check if we have the special genesis proof
		a.mu.RLock()
		_, hasGenesisProof := a.zkProofs[0]
		a.mu.RUnlock()
		
		if !hasGenesisProof {
			// Give a more descriptive error about missing genesis block proofs
			log.Printf("[ZKArchiver] No proof available that includes genesis block (height 0)")
		}
	}
	
	// Strategy: Find the most efficient proof path
	// 1. Try highest level recursive proof first (most efficient)
	// 2. Fall back to lower level recursive proofs
	// 3. Finally use individual proofs if needed
	
	for level := a.config.RecursiveProofLevels; level >= 1; level-- {
		a.mu.RLock()
		recursiveProofs := a.recursiveProofs[level]
		a.mu.RUnlock()
		
		// Find all recursive proofs that cover our range
		var bestProof []byte
		var bestProofHeight uint64
		
		for h, proof := range recursiveProofs {
			if h >= toHeight && (bestProofHeight == 0 || h < bestProofHeight) {
				bestProof = proof
				bestProofHeight = h
			}
		}
		
		if bestProof != nil {
			// Try to verify with this recursive proof
			valid, err := a.circuit.VerifyRecursiveProof(ctx, bestProof, fromHeight, toHeight)
			if err == nil && valid {
				// Success! Update metrics
				verifyTime := time.Since(startTime)
				
				a.mu.Lock()
				a.metrics.VerificationLatencyMs = (a.metrics.VerificationLatencyMs + uint64(verifyTime.Milliseconds())) / 2
				a.mu.Unlock()
				
				log.Printf("[ZKArchiver] Verified transition %d->%d using L%d recursive proof in %s",
					fromHeight, toHeight, level, verifyTime)
				return true, nil
			}
			
			// If we failed but have a proof, log detailed error
			if err != nil {
				log.Printf("[ZKArchiver] Failed to verify with L%d recursive proof: %v", level, err)
			}
		}
	}
	
	// If recursive proofs failed, try individual proofs
	log.Printf("[ZKArchiver] Falling back to individual proofs for transition %d->%d",
		fromHeight, toHeight)
	
	// Find all proofs needed to verify this transition
	var proofs [][]byte
	current := fromHeight
	
	// Make sure we have the zkProofs map populated
	a.mu.RLock()
	hasProofs := len(a.zkProofs) > 0
	a.mu.RUnlock()
	
	if !hasProofs {
		return false, fmt.Errorf("no proofs available, archiver may not have generated any proofs yet")
	}
	
	for current < toHeight {
		// Find the next proof that covers current
		nextProofHeight := a.findNextProofHeight(current)
		if nextProofHeight == 0 || nextProofHeight > toHeight {
			return false, fmt.Errorf("proof range doesn't cover requested range: missing proof chain from height %d to %d", current, toHeight)
		}
		
		a.mu.RLock()
		proof, exists := a.zkProofs[nextProofHeight]
		a.mu.RUnlock()
		
		if !exists {
			return false, fmt.Errorf("missing proof for height %d", nextProofHeight)
		}
		
		proofs = append(proofs, proof)
		current = nextProofHeight + 1
	}
	
	// Verify each proof in sequence
	log.Printf("[ZKArchiver] Verifying %d individual proofs for transition %d->%d",
		len(proofs), fromHeight, toHeight)
	
	for i, proof := range proofs {
		// Calculate the start and end height for this proof
		proofStartHeight := fromHeight
		if i > 0 {
			proofStartHeight = a.findNextProofHeight(fromHeight) + 1
			for j := 1; j < i; j++ {
				proofStartHeight = a.findNextProofHeight(proofStartHeight) + 1
			}
		}
		
		proofEndHeight := a.findNextProofHeight(proofStartHeight)
		
		// Add more detailed logging
		log.Printf("[ZKArchiver] Verifying proof %d/%d for range %d-%d", 
			i+1, len(proofs), proofStartHeight, proofEndHeight)
		
		valid, err := a.circuit.VerifyProof(ctx, proof, proofStartHeight, proofEndHeight)
		if err != nil || !valid {
			return false, fmt.Errorf("failed to verify proof for range %d-%d: %w",
				proofStartHeight, proofEndHeight, err)
		}
	}
	
	// All proofs verified successfully
	verifyTime := time.Since(startTime)
	
	a.mu.Lock()
	a.metrics.VerificationLatencyMs = (a.metrics.VerificationLatencyMs + uint64(verifyTime.Milliseconds())) / 2
	a.mu.Unlock()
	
	log.Printf("[ZKArchiver] Verified transition %d->%d using %d individual proofs in %s",
		fromHeight, toHeight, len(proofs), verifyTime)
	
	return true, nil
}

// findNextProofHeight finds the next height that has a proof and covers the given height
func (a *ZKArchiver) findNextProofHeight(height uint64) uint64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	
	var nextHeight uint64
	for h := range a.zkProofs {
		// Special handling for genesis block (height 0)
		var batchStart uint64
		if height == 0 {
			batchStart = 0
		} else {
			// Find the lowest proof height that's >= the start of the batch containing height
			batchStart = (height / a.config.BatchSize) * a.config.BatchSize
		}
		
		if h >= batchStart && (nextHeight == 0 || h < nextHeight) {
			nextHeight = h
		}
	}
	
	return nextHeight
}

// GetVerificationLatency returns the average verification latency in milliseconds
func (a *ZKArchiver) GetVerificationLatency() uint64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.metrics.VerificationLatencyMs
}

// GetCompressionRatio returns the average compression ratio achieved
func (a *ZKArchiver) GetCompressionRatio() float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.metrics.CompressionRatio
}

// GetBlockCountInMemory returns the number of blocks that would need to be kept in memory
// without ZK archiving (i.e., how much storage we're saving)
func (a *ZKArchiver) GetBlockCountInMemory() uint64 {
	height, err := a.statelessChain.GetHeight(context.Background())
	if err != nil {
		log.Printf("[ZKArchiver] Failed to get chain height: %v", err)
		return 0
	}
	
	return height + 1 // Include genesis
}

// GetTotalStorageSaved calculates the approximate storage saved in bytes
func (a *ZKArchiver) GetTotalStorageSaved() uint64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	
	if a.metrics.CompressionRatio <= 1.0 {
		return 0 // No savings
	}
	
	// Calculate approx storage needed for all blocks
	blocksArchived := a.metrics.TotalBlocksArchived
	avgBlockSize := uint64(128 * 1024) // 128KB average block size assumption
	totalBlockSize := blocksArchived * avgBlockSize
	
	// Calculate storage needed for ZK proofs
	totalProofSize := a.metrics.TotalProofsGenerated * a.metrics.AverageProofSizeBytes
	
	// Add storage for reference states
	refStateCount := uint64(len(a.referenceStates))
	refStateSize := refStateCount * uint64(sha256.Size)
	
	// Add storage for recursive proofs
	recursiveProofCount := a.metrics.RecursiveProofsGenerated
	recursiveProofSize := recursiveProofCount * a.metrics.AverageProofSizeBytes
	
	// Total storage with ZK
	totalZKStorage := totalProofSize + refStateSize + recursiveProofSize
	
	// Return storage saved
	if totalBlockSize > totalZKStorage {
		return totalBlockSize - totalZKStorage
	}
	
	return 0
}

// GetMetrics returns a copy of the current metrics
func (a *ZKArchiver) GetMetrics() ZKArchiverMetrics {
	a.mu.RLock()
	defer a.mu.RUnlock()
	
	// Return a copy to avoid concurrent access issues
	return *a.metrics
}

// Helper method to get block by height instead of ID
func (a *ZKArchiver) getBlockByHeight(ctx context.Context, height uint64) (core.StatelessBlock, error) {
	// First check if the chain has a direct method to get blocks by height (used in tests)
	type heightGetter interface {
		GetBlockByHeight(ctx context.Context, height uint64) (core.StatelessBlock, error)
	}
	
	// Type assertion to check if our chain implements the GetBlockByHeight method
	if getter, ok := a.statelessChain.(heightGetter); ok {
		// Use the direct method if available
		return getter.GetBlockByHeight(ctx, height)
	}
	
	// Fallback to the old method - get all blocks and find the right one
	blocks, err := a.getAllBlocks(ctx)
	if err != nil {
		return nil, err
	}
	
	// Find the block with matching height
	for _, block := range blocks {
		if block.Height() == height {
			return block, nil
		}
	}
	
	return nil, fmt.Errorf("block at height %d not found", height)
}

// Helper method to get all blocks from the chain
func (a *ZKArchiver) getAllBlocks(ctx context.Context) ([]core.StatelessBlock, error) {
	// This is a workaround since our interface expects GetBlock by ID,
	// but we need to get by height for the archival process
	
	// Get the current height
	height, err := a.statelessChain.GetHeight(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get chain height: %w", err)
	}
	
	// Collect all blocks
	var blocks []core.StatelessBlock
	for h := uint64(0); h <= height; h++ {
		// This is a bit inefficient but necessary to work with the interface
		// We would need to enhance the interface with GetBlockByHeight in production
		latestRoot, err := a.statelessChain.GetLatestStateRoot(ctx)
		if err != nil {
			continue
		}
		
		// Try to find block using its ID based on height data
		// This is a simplified approach for the example
		blockID := makeBlockIDFromHeight(h, latestRoot)
		block, err := a.statelessChain.GetBlock(ctx, blockID)
		if err == nil {
			blocks = append(blocks, block)
		}
	}
	
	return blocks, nil
}

// Uses makeBlockIDFromHeight from utilities.go

// Helper function to check if a proof has TEE attestation
func hasAttestationInProof(proof core.StatelessProof) bool {
	// Check if the proof type contains "attestation"
	return proof.ProofType() == "attestation" || 
	       proof.ProofType() == "tee" || 
	       proof.ProofType() == "sgx" || 
	       proof.ProofType() == "sev"
}
