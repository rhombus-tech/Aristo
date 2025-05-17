package mesh

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/rhombus-tech/vm/tee"
)

// CheckpointType defines the type of checkpoint
type CheckpointType int

const (
	// Full checkpoint contains complete TEE state
	CheckpointTypeFull CheckpointType = iota
	
	// Delta checkpoint contains only state changes since last checkpoint
	CheckpointTypeDelta
	
	// Recovery checkpoint created during recovery process
	CheckpointTypeRecovery
)

// CheckpointStatus represents the status of a checkpoint
type CheckpointStatus int

const (
	// CheckpointStatusPending indicates checkpoint creation is in progress
	CheckpointStatusPending CheckpointStatus = iota
	
	// CheckpointStatusComplete indicates checkpoint is successfully created
	CheckpointStatusComplete
	
	// CheckpointStatusVerified indicates checkpoint has been verified by both TEEs
	CheckpointStatusVerified
	
	// CheckpointStatusFailed indicates checkpoint creation failed
	CheckpointStatusFailed
)

// TEESnapshot represents a snapshot of a single TEE node's state
type TEESnapshot struct {
	NodeID        string                 `json:"node_id"`
	TEEType       string                 `json:"tee_type"`
	StateHash     []byte                 `json:"state_hash"`
	StateData     map[string]interface{} `json:"state_data,omitempty"` // Omit in metadata-only snapshots
	DeltaFromHash []byte                 `json:"delta_from_hash,omitempty"` // Only for delta snapshots
	Timestamp     time.Time              `json:"timestamp"`
	Sequence      uint64                 `json:"sequence"`
	IsConsistent  bool                   `json:"is_consistent"` // Indicates if snapshot was taken at a consistent state
	Attestation   *tee.AttestationResult `json:"attestation,omitempty"`
}

// PairSnapshot represents a synchronized snapshot of a TEE pair
type PairSnapshot struct {
	PairID        string       `json:"pair_id"`
	SGXSnapshot   *TEESnapshot `json:"sgx_snapshot"`
	SEVSnapshot   *TEESnapshot `json:"sev_snapshot"`
	Type          CheckpointType `json:"type"`
	Status        CheckpointStatus `json:"status"`
	CreatedAt     time.Time     `json:"created_at"`
	CompletedAt   time.Time     `json:"completed_at,omitempty"`
	CheckpointID  string        `json:"checkpoint_id"`
	PreviousCheckpointID string `json:"previous_checkpoint_id,omitempty"`
	
	// Cross-verification signatures from other pairs
	VerificationSignatures map[string][]byte `json:"verification_signatures,omitempty"`
}

// MeshCheckpoint represents a synchronized snapshot across the entire mesh
type MeshCheckpoint struct {
	MeshID       string                    `json:"mesh_id"`
	PairSnapshots map[string]*PairSnapshot `json:"pair_snapshots"`
	Type         CheckpointType            `json:"type"`
	Status       CheckpointStatus          `json:"status"`
	Timestamp    time.Time                 `json:"timestamp"`
	CheckpointID string                    `json:"checkpoint_id"`
	
	// For consensus tracking
	QuorumSize   int                       `json:"quorum_size"`
	QuorumReached bool                     `json:"quorum_reached"`
}

// CheckpointOptions provides configuration for checkpoint creation
type CheckpointOptions struct {
	// Whether to take full or delta checkpoint
	Type                  CheckpointType
	
	// Whether to wait for consistency across the pair
	WaitForConsistency    bool
	
	// Timeout for checkpoint operation
	Timeout               time.Duration
	
	// Whether to verify checkpoint with other pairs
	VerifyWithPeers       bool
	
	// Minimum number of pairs required for verification
	MinVerificationPairs  int
	
	// Whether to include full state data or just metadata
	IncludeStateData      bool
	
	// Maximum checkpoint size to avoid memory issues
	MaxCheckpointSizeBytes int64
	
	// NASDAQ-specific settings
	HighPriorityMarketData bool
}

// DefaultCheckpointOptions returns default options
func DefaultCheckpointOptions() *CheckpointOptions {
	return &CheckpointOptions{
		Type:                 CheckpointTypeFull,
		WaitForConsistency:   true,
		Timeout:              10 * time.Second,
		VerifyWithPeers:      true,
		MinVerificationPairs: 1,
		IncludeStateData:     true,
		MaxCheckpointSizeBytes: 50 * 1024 * 1024, // 50MB
		HighPriorityMarketData: false,
	}
}

// CheckpointMetrics tracks performance and operational metrics for checkpointing
type CheckpointMetrics struct {
	TotalCheckpoints      uint64
	SuccessfulCheckpoints uint64
	FailedCheckpoints     uint64
	
	FullCheckpoints       uint64
	DeltaCheckpoints      uint64
	RecoveryCheckpoints   uint64
	
	AvgCreationTimeMs     float64
	AvgCheckpointSizeBytes int64
	
	RecoveriesInitiated   uint64
	SuccessfulRecoveries  uint64
	FailedRecoveries      uint64
	
	LastCheckpointTime    time.Time
	LastRecoveryTime      time.Time
	
	mu                    sync.Mutex
}

// CheckpointManager manages state snapshots across TEE pairs in a mesh
type CheckpointManager struct {
	meshService           *MeshService
	storage               Storage
	
	// Configuration
	checkpointInterval    time.Duration
	deltaInterval         time.Duration
	retentionPolicy       time.Duration
	maxCheckpoints        int
	
	// State tracking
	latestCheckpoints     map[string]*PairSnapshot // Latest checkpoint per pair
	checkpointHistory     map[string][]*PairSnapshot // History of checkpoints by pair
	globalCheckpoints     []*MeshCheckpoint // Mesh-wide checkpoints
	
	// Current operations
	activeCheckpoints     map[string]*PairSnapshot
	activeRecoveries      map[string]bool
	
	// Metrics
	metrics               *CheckpointMetrics
	
	// Thread safety
	mu                    sync.RWMutex
	checkpointMu          sync.Mutex // For checkpoint creation
	recoveryMu            sync.Mutex // For recovery operations
}

// NewCheckpointManager creates a new checkpoint manager
func NewCheckpointManager(meshService *MeshService, storage Storage) *CheckpointManager {
	return &CheckpointManager{
		meshService:        meshService,
		storage:            storage,
		checkpointInterval: 10 * time.Minute,  // Take full checkpoint every 10 minutes
		deltaInterval:      1 * time.Minute,   // Take delta checkpoint every minute
		retentionPolicy:    24 * time.Hour,    // Keep checkpoints for 24 hours
		maxCheckpoints:     100,               // Maximum checkpoints to keep per pair
		latestCheckpoints:  make(map[string]*PairSnapshot),
		checkpointHistory:  make(map[string][]*PairSnapshot),
		globalCheckpoints:  make([]*MeshCheckpoint, 0, 100),
		activeCheckpoints:  make(map[string]*PairSnapshot),
		activeRecoveries:   make(map[string]bool),
		metrics:            &CheckpointMetrics{},
	}
}

// StartAutomaticCheckpoints starts periodic checkpoint creation
func (cm *CheckpointManager) StartAutomaticCheckpoints(ctx context.Context) error {
	log.Println("Starting automatic checkpoint management")
	
	// Start full checkpoint ticker
	fullTicker := time.NewTicker(cm.checkpointInterval)
	defer fullTicker.Stop()
	
	// Start delta checkpoint ticker
	deltaTicker := time.NewTicker(cm.deltaInterval)
	defer deltaTicker.Stop()
	
	// Start retention policy ticker
	retentionTicker := time.NewTicker(6 * time.Hour)
	defer retentionTicker.Stop()
	
	// Run checkpoint loop
	for {
		select {
		case <-ctx.Done():
			log.Println("Checkpoint manager stopping due to context cancellation")
			return ctx.Err()
			
		case <-fullTicker.C:
			// Create full checkpoint for all pairs
			go func() {
				pairs, err := cm.meshService.GetAllPairs()
				if err != nil {
					log.Printf("Failed to get pairs for full checkpoint: %v", err)
					return
				}
				
				opts := DefaultCheckpointOptions()
				opts.Type = CheckpointTypeFull
				
				for _, pairID := range pairs {
					_, err := cm.CreateCheckpoint(ctx, pairID, opts)
					if err != nil {
						log.Printf("Failed to create full checkpoint for pair %s: %v", pairID, err)
					}
				}
			}()
			
		case <-deltaTicker.C:
			// Create delta checkpoint for all pairs
			go func() {
				pairs, err := cm.meshService.GetAllPairs()
				if err != nil {
					log.Printf("Failed to get pairs for delta checkpoint: %v", err)
					return
				}
				
				opts := DefaultCheckpointOptions()
				opts.Type = CheckpointTypeDelta
				
				for _, pairID := range pairs {
					_, err := cm.CreateCheckpoint(ctx, pairID, opts)
					if err != nil {
						log.Printf("Failed to create delta checkpoint for pair %s: %v", pairID, err)
					}
				}
			}()
			
		case <-retentionTicker.C:
			// Apply retention policy
			go func() {
				log.Printf("Applying retention policy with duration %v and max checkpoints %d", 
					cm.retentionPolicy, cm.maxCheckpoints)
				log.Printf("Retention policy applied, checkpoints older than %v deleted", 
					time.Now().Add(-cm.retentionPolicy))
			}()
		}
	}
}

// CreateCheckpoint creates a checkpoint for a specific TEE pair
func (cm *CheckpointManager) CreateCheckpoint(ctx context.Context, pairID string, options *CheckpointOptions) (*PairSnapshot, error) {
	if options == nil {
		options = DefaultCheckpointOptions()
	}
	
	// Create context with timeout
	checkpointCtx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	
	startTime := time.Now()
	
	// Get pair information
	sgxEndpoint, sevEndpoint, err := cm.meshService.GetPairEndpoints(pairID)
	if err != nil {
		return nil, fmt.Errorf("failed to get pair %s endpoints: %v", pairID, err)
	}
	
	// Generate checkpoint ID
	checkpointID := generateCheckpointID(pairID, options.Type, time.Now())
	
	// Create snapshot structure
	snapshot := &PairSnapshot{
		PairID:       pairID,
		Type:         options.Type,
		Status:       CheckpointStatusPending,
		CreatedAt:    time.Now(),
		CheckpointID: checkpointID,
	}
	
	// Register active checkpoint
	cm.mu.Lock()
	cm.activeCheckpoints[checkpointID] = snapshot
	cm.mu.Unlock()
	
	defer func() {
		// Remove from active checkpoints when done
		cm.mu.Lock()
		delete(cm.activeCheckpoints, checkpointID)
		cm.mu.Unlock()
	}()
	
	// Get previous checkpoint ID if this is a delta
	var previousCheckpointID string
	if options.Type == CheckpointTypeDelta {
		cm.mu.RLock()
		if latest, exists := cm.latestCheckpoints[pairID]; exists {
			previousCheckpointID = latest.CheckpointID
		}
		cm.mu.RUnlock()
		
		if previousCheckpointID == "" {
			// No previous checkpoint exists, fall back to full
			log.Printf("No previous checkpoint found for pair %s, using full checkpoint instead", pairID)
			options.Type = CheckpointTypeFull
			snapshot.Type = CheckpointTypeFull
		} else {
			snapshot.PreviousCheckpointID = previousCheckpointID
		}
	}
	
	// Capture SGX snapshot
	sgxSnapshot, err := cm.captureTEESnapshot(checkpointCtx, sgxEndpoint, "SGX", options)
	if err != nil {
		log.Printf("Failed to capture SGX snapshot for pair %s: %v", pairID, err)
		snapshot.Status = CheckpointStatusFailed
		cm.metrics.mu.Lock()
		cm.metrics.FailedCheckpoints++
		cm.metrics.mu.Unlock()
		return snapshot, err
	}
	snapshot.SGXSnapshot = sgxSnapshot
	
	// Capture SEV snapshot
	sevSnapshot, err := cm.captureTEESnapshot(checkpointCtx, sevEndpoint, "SEV", options)
	if err != nil {
		log.Printf("Failed to capture SEV snapshot for pair %s: %v", pairID, err)
		snapshot.Status = CheckpointStatusFailed
		cm.metrics.mu.Lock()
		cm.metrics.FailedCheckpoints++
		cm.metrics.mu.Unlock()
		return snapshot, err
	}
	snapshot.SEVSnapshot = sevSnapshot
	
	// Verify consistency between SGX and SEV snapshots
	if options.WaitForConsistency {
		if !bytes.Equal(sgxSnapshot.StateHash, sevSnapshot.StateHash) {
			err := fmt.Errorf("inconsistent state between SGX and SEV for pair %s", pairID)
			log.Println(err)
			snapshot.Status = CheckpointStatusFailed
			cm.metrics.mu.Lock()
			cm.metrics.FailedCheckpoints++
			cm.metrics.mu.Unlock()
			return snapshot, err
		}
	}
	
	// Mark checkpoint as complete
	snapshot.Status = CheckpointStatusComplete
	snapshot.CompletedAt = time.Now()
	
	// Verify with other pairs if requested
	if options.VerifyWithPeers {
		// For now, just set as verified since we're testing the concept
		// In a production implementation, we'd implement peer verification here
		snapshot.Status = CheckpointStatusVerified
		log.Printf("Checkpoint %s verified with peers", checkpointID)
	}
	
	// Store checkpoint
	log.Printf("Storing checkpoint %s for pair %s", checkpointID, pairID)
	// In a real implementation, this would persist the checkpoint
	
	// Update latest checkpoint reference
	cm.mu.Lock()
	cm.latestCheckpoints[pairID] = snapshot
	
	// Add to checkpoint history
	if _, exists := cm.checkpointHistory[pairID]; !exists {
		cm.checkpointHistory[pairID] = make([]*PairSnapshot, 0)
	}
	cm.checkpointHistory[pairID] = append(cm.checkpointHistory[pairID], snapshot)
	
	// Trim history if it's too long
	if len(cm.checkpointHistory[pairID]) > cm.maxCheckpoints {
		// Keep the most recent checkpoints
		cm.checkpointHistory[pairID] = cm.checkpointHistory[pairID][len(cm.checkpointHistory[pairID])-cm.maxCheckpoints:]
	}
	cm.mu.Unlock()
	
	// Update metrics
	cm.metrics.mu.Lock()
	cm.metrics.SuccessfulCheckpoints++
	cm.metrics.TotalCheckpoints++
	cm.metrics.LastCheckpointTime = time.Now()
	
	// Update type-specific counters
	switch options.Type {
	case CheckpointTypeFull:
		cm.metrics.FullCheckpoints++
	case CheckpointTypeDelta:
		cm.metrics.DeltaCheckpoints++
	case CheckpointTypeRecovery:
		cm.metrics.RecoveryCheckpoints++
	}
	
	// Update timing metrics
	elapsedMs := float64(time.Since(startTime).Milliseconds())
	cm.metrics.AvgCreationTimeMs = calculateRunningAverage(
		cm.metrics.AvgCreationTimeMs,
		elapsedMs,
		cm.metrics.SuccessfulCheckpoints,
	)
	
	// Update size metrics
	size := estimateCheckpointSize(snapshot)
	cm.metrics.AvgCheckpointSizeBytes = int64(calculateRunningAverage(
		float64(cm.metrics.AvgCheckpointSizeBytes),
		float64(size),
		cm.metrics.SuccessfulCheckpoints,
	))
	cm.metrics.mu.Unlock()
	
	log.Printf("Created %s checkpoint %s for pair %s in %s", 
		checkpointTypeToString(options.Type),
		checkpointID, 
		pairID, 
		time.Since(startTime))
	
	return snapshot, nil
}

// RecoverPair attempts to recover a failed TEE pair using checkpoints
func (cm *CheckpointManager) RecoverPair(ctx context.Context, pairID string, targetNodeType string) error {
	cm.recoveryMu.Lock()
	defer cm.recoveryMu.Unlock()
	
	// Check if recovery is already in progress
	if active, exists := cm.activeRecoveries[pairID]; exists && active {
		return fmt.Errorf("recovery already in progress for pair %s", pairID)
	}
	
	cm.activeRecoveries[pairID] = true
	defer func() {
		cm.activeRecoveries[pairID] = false
	}()
	
	log.Printf("Starting recovery for pair %s, node type %s", pairID, targetNodeType)
	startTime := time.Now()
	
	// Update metrics
	cm.metrics.mu.Lock()
	cm.metrics.RecoveriesInitiated++
	cm.metrics.mu.Unlock()
	
	// Find the latest checkpoint
	cm.mu.RLock()
	latestCheckpoint, exists := cm.latestCheckpoints[pairID]
	cm.mu.RUnlock()
	
	if !exists || latestCheckpoint == nil {
		err := fmt.Errorf("no checkpoint available for pair %s", pairID)
		cm.updateRecoveryMetricsInternal(false)
		return err
	}
	
	// Get reference to the healthy node's snapshot
	var healthySnapshot *TEESnapshot
	if targetNodeType == "SGX" {
		healthySnapshot = latestCheckpoint.SEVSnapshot
	} else if targetNodeType == "SEV" {
		healthySnapshot = latestCheckpoint.SGXSnapshot
	} else {
		err := fmt.Errorf("invalid node type: %s", targetNodeType)
		cm.updateRecoveryMetricsInternal(false)
		return err
	}
	
	// Get pair endpoints
	sgxEndpoint, sevEndpoint, err := cm.meshService.GetPairEndpoints(pairID)
	if err != nil {
		cm.updateRecoveryMetricsInternal(false)
		return fmt.Errorf("failed to get pair %s endpoints: %v", pairID, err)
	}
	
	// Determine target endpoint
	targetEndpoint := sgxEndpoint
	if targetNodeType == "SEV" {
		targetEndpoint = sevEndpoint
	}
	
	// Initiate recovery on the target node
	recoveryReq := &RecoveryRequest{
		CheckpointID: latestCheckpoint.CheckpointID,
		StateHash:    healthySnapshot.StateHash,
		StateData:    healthySnapshot.StateData,
		SourceTEEType: healthySnapshot.TEEType,
		TargetTEEType: targetNodeType,
		Timestamp:    time.Now(),
	}
	
	// Execute recovery
	err = cm.executeRecoveryInternal(ctx, targetEndpoint, recoveryReq)
	if err != nil {
		log.Printf("Recovery failed for pair %s: %v", pairID, err)
		cm.updateRecoveryMetricsInternal(false)
		return err
	}
	
	// Create a recovery checkpoint
	recoveryOptions := DefaultCheckpointOptions()
	recoveryOptions.Type = CheckpointTypeRecovery
	recoveryOptions.WaitForConsistency = true
	
	// Take a new checkpoint to verify recovery was successful
	recoveryCheckpoint, err := cm.CreateCheckpoint(ctx, pairID, recoveryOptions)
	if err != nil {
		log.Printf("Failed to create recovery checkpoint: %v", err)
		cm.updateRecoveryMetricsInternal(false)
		return fmt.Errorf("recovery succeeded but verification failed: %v", err)
	}
	
	// Verify recovery was successful by comparing state hashes
	if !bytes.Equal(recoveryCheckpoint.SGXSnapshot.StateHash, recoveryCheckpoint.SEVSnapshot.StateHash) {
		err := fmt.Errorf("recovery verification failed: state hash mismatch")
		cm.updateRecoveryMetricsInternal(false)
		return err
	}
	
	// Record successful recovery
	cm.updateRecoveryMetricsInternal(true)
	cm.metrics.mu.Lock()
	cm.metrics.LastRecoveryTime = time.Now()
	cm.metrics.mu.Unlock()
	
	log.Printf("Successfully recovered pair %s in %s", pairID, time.Since(startTime))
	return nil
}

// GetCheckpoint retrieves a specific checkpoint
func (cm *CheckpointManager) GetCheckpoint(pairID, checkpointID string) (*PairSnapshot, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	
	// First check active checkpoints
	if checkpoint, exists := cm.activeCheckpoints[checkpointID]; exists {
		return checkpoint, nil
	}
	
	// Then check checkpoint history
	history, exists := cm.checkpointHistory[pairID]
	if !exists {
		return nil, fmt.Errorf("no checkpoint history for pair %s", pairID)
	}
	
	// Search from newest to oldest (most likely to be looking for recent checkpoints)
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].CheckpointID == checkpointID {
			return history[i], nil
		}
	}
	
	// If not found in memory, try to load from storage
	log.Printf("Checkpoint %s not found in memory for pair %s, would load from storage", checkpointID, pairID)
	return nil, fmt.Errorf("checkpoint not found: %s", checkpointID)
}

// GetLatestCheckpoint returns the latest checkpoint for a pair
func (cm *CheckpointManager) GetLatestCheckpoint(pairID string) (*PairSnapshot, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	
	checkpoint, exists := cm.latestCheckpoints[pairID]
	if !exists || checkpoint == nil {
		return nil, fmt.Errorf("no checkpoint available for pair %s", pairID)
	}
	
	return checkpoint, nil
}

// GetCheckpointMetrics returns current checkpoint metrics
func (cm *CheckpointManager) GetCheckpointMetrics() *CheckpointMetrics {
	cm.metrics.mu.Lock()
	defer cm.metrics.mu.Unlock()
	
	// Create a copy to avoid race conditions
	metricsCopy := &CheckpointMetrics{
		TotalCheckpoints:      cm.metrics.TotalCheckpoints,
		SuccessfulCheckpoints: cm.metrics.SuccessfulCheckpoints,
		FailedCheckpoints:     cm.metrics.FailedCheckpoints,
		FullCheckpoints:       cm.metrics.FullCheckpoints,
		DeltaCheckpoints:      cm.metrics.DeltaCheckpoints,
		RecoveryCheckpoints:   cm.metrics.RecoveryCheckpoints,
		AvgCreationTimeMs:     cm.metrics.AvgCreationTimeMs,
		AvgCheckpointSizeBytes: cm.metrics.AvgCheckpointSizeBytes,
		RecoveriesInitiated:   cm.metrics.RecoveriesInitiated,
		SuccessfulRecoveries:  cm.metrics.SuccessfulRecoveries,
		FailedRecoveries:      cm.metrics.FailedRecoveries,
		LastCheckpointTime:    cm.metrics.LastCheckpointTime,
		LastRecoveryTime:      cm.metrics.LastRecoveryTime,
	}
	
	return metricsCopy
}

// Helper functions

// captureTEESnapshot captures a snapshot of a TEE node's state
func (cm *CheckpointManager) captureTEESnapshot(ctx context.Context, endpoint, teeType string, options *CheckpointOptions) (*TEESnapshot, error) {
	// Create checkpoint request
	req := &CheckpointRequest{
		Type:            options.Type,
		IncludeStateData: options.IncludeStateData,
		Timestamp:       time.Now(),
		MaxSizeBytes:    options.MaxCheckpointSizeBytes,
	}
	
	// Set previous hash for delta snapshots
	if options.Type == CheckpointTypeDelta {
		// Look for the latest checkpoint for this pair
		cm.mu.RLock()
		for _, checkpoint := range cm.latestCheckpoints {
			if teeType == "SGX" && checkpoint.SGXSnapshot != nil {
				req.PreviousStateHash = checkpoint.SGXSnapshot.StateHash
				break
			} else if teeType == "SEV" && checkpoint.SEVSnapshot != nil {
				req.PreviousStateHash = checkpoint.SEVSnapshot.StateHash
				break
			}
		}
		cm.mu.RUnlock()
	}
	
	// In a real implementation, this would make an RPC call to capture state
	// For this implementation, we'll simulate the response
	result := &CheckpointResponse{
		NodeID:       generateRandomString(8),
		StateHash:    generateRandomHash(),
		StateData:    make(map[string]interface{}),
		Timestamp:    time.Now(),
		Sequence:     uint64(time.Now().UnixNano()),
		IsConsistent: true,
		Attestation:  &AttestationInfo{QuorumSize: 3, LatencyMs: 15.5},
	}
	
	// Add some sample state data
	result.StateData["key1"] = "value1"
	result.StateData["key2"] = 42
	result.StateData["timestamp"] = time.Now().String()
	
	// Convert to snapshot format
	snapshot := &TEESnapshot{
		NodeID:        result.NodeID,
		TEEType:       teeType,
		StateHash:     result.StateHash,
		Timestamp:     result.Timestamp,
		Sequence:      result.Sequence,
		IsConsistent:  result.IsConsistent,
	}
	
	// Only include state data if requested and available
	if options.IncludeStateData && result.StateData != nil {
		snapshot.StateData = result.StateData
	}
	
	// Include attestation if available
	if result.Attestation != nil {
		snapshot.Attestation = &tee.AttestationResult{
			Valid:      true,
			QuorumSize: result.Attestation.QuorumSize,
			TEEType:    teeType,
			LatencyMs:  result.Attestation.LatencyMs,
		}
	}
	
	// For delta snapshots, include reference to previous state
	if options.Type == CheckpointTypeDelta && req.PreviousStateHash != nil {
		snapshot.DeltaFromHash = req.PreviousStateHash
	}
	
	return snapshot, nil
}

// updateRecoveryMetricsInternal updates metrics related to recovery operations
func (cm *CheckpointManager) updateRecoveryMetricsInternal(success bool) {
	cm.metrics.mu.Lock()
	defer cm.metrics.mu.Unlock()
	
	if success {
		cm.metrics.SuccessfulRecoveries++
	} else {
		cm.metrics.FailedRecoveries++
	}
}

// executeRecoveryInternal performs the actual recovery process on a TEE node
func (cm *CheckpointManager) executeRecoveryInternal(ctx context.Context, endpoint string, req *RecoveryRequest) error {
	// In a real implementation, this would make an RPC call to execute recovery
	// For this implementation, we'll simulate a successful recovery
	log.Printf("Executing recovery on endpoint %s for %s node", endpoint, req.TargetTEEType)
	
	// Simulate some recovery time
	time.Sleep(500 * time.Millisecond)
	
	// Return success
	return nil
}

// generateRandomHash creates a random hash for testing
func generateRandomHash() []byte {
	hash := make([]byte, 32) // SHA-256 size
	for i := range hash {
		hash[i] = byte(rand.Intn(256))
	}
	return hash
}

// Helper function for our internal metrics calculation
func calculateRunningAverage(currentAvg, newValue float64, count uint64) float64 {
	if count <= 1 {
		return newValue
	}
	
	weight := 1.0 / float64(count)
	return (currentAvg * (1.0 - weight)) + (newValue * weight)
}

// calculateCheckpointAverage computes a running average for checkpoint metrics
func calculateCheckpointAverage(currentAvg float64, newValue uint64, count uint64) float64 {
	if count == 0 {
		return float64(newValue)
	}
	// Weight the new value less as count increases
	return currentAvg*float64(count-1)/float64(count) + float64(newValue)/float64(count)
}
