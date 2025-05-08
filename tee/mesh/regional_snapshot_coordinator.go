// Package mesh provides a mesh network for TEE-to-TEE communication
package mesh

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"
)

var (
	// ErrRegionalSnapshotCreationFailed indicates a failure during regional snapshot creation
	ErrRegionalSnapshotCreationFailed = errors.New("failed to create regional snapshot")
	
	// ErrInsufficientTEESnapshots indicates not enough TEE snapshots were provided
	ErrInsufficientTEESnapshots = errors.New("insufficient TEE snapshots for regional snapshot")
	
	// ErrSnapshotConsistencyFailed indicates inconsistency among TEE snapshots
	ErrSnapshotConsistencyFailed = errors.New("snapshot consistency check failed")
	
	// ErrRegionNotAuthorized indicates the region is not authorized to create a snapshot
	ErrRegionNotAuthorized = errors.New("region not authorized to create snapshot")
)

// RegionalSnapshotPolicy defines policies for regional snapshots
type RegionalSnapshotPolicy struct {
	MinTEECount            int      // Minimum number of TEEs required for a valid regional snapshot
	MinConsensusPercentage float64  // Minimum percentage of TEEs that must agree (0.0-1.0)
	MaxTimeDrift           int64    // Maximum allowed time drift between snapshots in seconds
	ConsistencyLevel       string   // "strict" or "permissive"
	AllowedRegions         []string // Regions allowed to participate
	RetentionPeriod        int      // Retention period in days
}

// DefaultRegionalSnapshotPolicy creates a default policy
func DefaultRegionalSnapshotPolicy() *RegionalSnapshotPolicy {
	return &RegionalSnapshotPolicy{
		MinTEECount:            3,
		MinConsensusPercentage: 0.67, // 2/3 majority
		MaxTimeDrift:           60,   // 1 minute
		ConsistencyLevel:       "strict",
		RetentionPeriod:        30, // 30 days
	}
}

// RegionalSnapshot represents a consolidated snapshot for an entire region
type RegionalSnapshot struct {
	// Core identification
	RegionID        string    // Region this snapshot represents
	SnapshotID      []byte    // Unique identifier for this snapshot (hash)
	Timestamp       time.Time // Time the snapshot was created
	
	// TEE snapshots included
	TEESnapshots    []*StateSnapshot     // Individual TEE snapshots
	TEESnapshotIDs  [][]byte             // IDs of TEE snapshots included
	SnapshotSummary *SnapshotSummary     // Summary of all included snapshots
	
	// Consensus data
	ConsensusInfo   *SnapshotConsensusInfo // Information about the consensus process
	
	// Verification
	CoordinatorSignature []byte   // Signature from the regional coordinator
	VerifierSignatures   [][]byte // Optional signatures from external verifiers
	
	// Blockchain anchoring (to be added in future integration)
	// BlockchainRef      string    // Reference to blockchain record
	// TimestampProof     []byte    // Proof of timestamp from timeserver-core
	
	// Metadata
	Metadata         map[string]interface{} // Additional metadata
}

// SnapshotSummary provides an aggregated view of snapshots from all TEEs
type SnapshotSummary struct {
	MerkleRoot       []byte              // Merkle root of all snapshot IDs
	StateRootHashes  map[string][]byte   // State root hashes by TEE ID
	ObjectCount      int                 // Number of distinct objects in all snapshots
	TotalStateSize   int64               // Total size of all state data
	RegionalMetrics  map[string]float64  // Aggregated metrics
}

// SnapshotConsensusInfo contains information about the snapshot consensus process
type SnapshotConsensusInfo struct {
	TEECount          int     // Total number of TEEs in the region
	ParticipatingTEEs int     // Number of TEEs that participated
	ConsensusLevel    float64 // Level of consensus achieved (0.0-1.0)
	ConsensusMethod   string  // Method used to achieve consensus
	ConsensusSuccess  bool    // Whether consensus was successful
}

// SnapshotStorageInterface defines the interface for snapshot storage
type SnapshotStorageInterface interface {
	StoreSnapshot(s *StateSnapshot) error
	GetSnapshot(snapshotID string) (*RegionalSnapshot, error)
}

// StateManagerInterface defines the interface for state management
type StateManagerInterface interface {
	SerializeState(objectID string, object interface{}) ([]byte, error)
}

// CoordinatorMetrics tracks performance metrics for the snapshot coordinator
type CoordinatorMetrics struct {
	TotalSnapshots            int64
	SuccessfulSnapshots       int64
	FailedSnapshots           int64
	AverageSnapshotTimeMs     int64
	MaxSnapshotTimeMs         int64
	MinSnapshotTimeMs         int64
	AverageSnapshotSizeBytes  int64
	MaxSnapshotSizeBytes      int64
	CompressionRatio          float64
	TotalBytesStored          int64
	TotalBytesCompressed      int64
	BlockchainAnchors         int64
	AverageAnchorTimeMs       int64
	LastSnapshotTime          time.Time
	TimeSinceLastSnapshotSec  int64
	LastMutex                 sync.RWMutex
}

// RegionalSnapshotCoordinator manages regional snapshots across all TEEs
type RegionalSnapshotCoordinator struct {
	regionID           string
	policy             *RegionalSnapshotPolicy
	teeRegistry        map[string]string
	snapshotStorage    SnapshotStorageInterface
	stateManager       StateManagerInterface
	mu                 sync.RWMutex
	ongoingCollections map[string]*SnapshotCollectionStatus
	latestRegionalSnapshot []byte // ID of the latest regional snapshot
	blockchainAnchorEnabled bool  // Whether blockchain anchoring is enabled
	blockchainClient      BlockchainClient
	blockchainEndpoint    string
	scheduler            *SnapshotScheduler
	compressionLevel      int // 0-9, where 0 is no compression and 9 is max compression
	metrics               *CoordinatorMetrics
	snapshotCache         map[string]*RegionalSnapshot
	cacheMutex            sync.RWMutex
}

// SnapshotCollectionStatus tracks the status of an ongoing snapshot collection
type SnapshotCollectionStatus struct {
	CollectionID     string
	StartTime        time.Time
	Deadline         time.Time
	TargetTEECount   int
	ReceivedSnapshots map[string]*StateSnapshot // TEE ID -> Snapshot
	CompletionStatus string // "pending", "completed", "failed"
}

// NewRegionalSnapshotCoordinator creates a new regional snapshot coordinator
func NewRegionalSnapshotCoordinator(
	regionID string,
	policy *RegionalSnapshotPolicy,
	snapshotStorage SnapshotStorageInterface,
	stateManager StateManagerInterface,
) *RegionalSnapshotCoordinator {
	if policy == nil {
		policy = DefaultRegionalSnapshotPolicy()
	}
	
	coord := &RegionalSnapshotCoordinator{
		regionID:            regionID,
		policy:              policy,
		teeRegistry:         make(map[string]string),
		snapshotStorage:     snapshotStorage,
		stateManager:        stateManager,
		ongoingCollections:  make(map[string]*SnapshotCollectionStatus),
		blockchainAnchorEnabled: false, // Disabled until blockchain integration
		compressionLevel:     6, // Default level (0-9, higher = more compression, slower)
		metrics:              &CoordinatorMetrics{MinSnapshotTimeMs: math.MaxInt64},
		snapshotCache:        make(map[string]*RegionalSnapshot),
	}
	return coord
}

// RegisterTEE registers a TEE with the coordinator
func (r *RegionalSnapshotCoordinator) RegisterTEE(teeID, teeType string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	r.teeRegistry[teeID] = teeType
}

// GetRegisteredTEEs returns all registered TEEs
func (r *RegionalSnapshotCoordinator) GetRegisteredTEEs() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	// Return a copy to avoid race conditions
	result := make(map[string]string, len(r.teeRegistry))
	for id, typ := range r.teeRegistry {
		result[id] = typ
	}
	
	return result
}

// InitiateRegionalSnapshot begins the process of collecting snapshots from all TEEs
func (r *RegionalSnapshotCoordinator) InitiateRegionalSnapshot(ctx context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	// Generate a unique collection ID
	collectionID := fmt.Sprintf("snapshot-collection-%s-%d", 
		r.regionID, time.Now().UnixNano())
	
	// Create a new collection status
	targetTEECount := len(r.teeRegistry)
	if targetTEECount < r.policy.MinTEECount {
		return "", fmt.Errorf("not enough registered TEEs: %d, minimum required: %d", 
			targetTEECount, r.policy.MinTEECount)
	}
	
	status := &SnapshotCollectionStatus{
		CollectionID:      collectionID,
		StartTime:         time.Now(),
		Deadline:          time.Now().Add(5 * time.Minute), // 5 minute deadline
		TargetTEECount:    targetTEECount,
		ReceivedSnapshots: make(map[string]*StateSnapshot),
		CompletionStatus:  "pending",
	}
	
	r.ongoingCollections[collectionID] = status
	
	// In a real implementation, this would trigger requests to all TEEs to create snapshots
	// For now, we'll just return the collection ID
	
	return collectionID, nil
}

// SubmitTEESnapshot submits a snapshot from a TEE to an ongoing collection
func (r *RegionalSnapshotCoordinator) SubmitTEESnapshot(
	collectionID string, 
	teeID string, 
	snapshot *StateSnapshot,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	// Verify the collection exists and is still pending
	collection, exists := r.ongoingCollections[collectionID]
	if !exists {
		return fmt.Errorf("snapshot collection not found: %s", collectionID)
	}
	
	if collection.CompletionStatus != "pending" {
		return fmt.Errorf("snapshot collection is no longer accepting submissions: %s", 
			collection.CompletionStatus)
	}
	
	// Verify the TEE is registered
	if _, exists := r.teeRegistry[teeID]; !exists {
		return fmt.Errorf("TEE not registered with this coordinator: %s", teeID)
	}
	
	// Verify the snapshot is from the correct TEE
	if snapshot.TEEID != teeID {
		return fmt.Errorf("snapshot TEE ID mismatch: expected %s, got %s", 
			teeID, snapshot.TEEID)
	}
	
	// Verify the snapshot is for the correct region
	if snapshot.RegionID != r.regionID {
		return fmt.Errorf("snapshot region mismatch: expected %s, got %s", 
			r.regionID, snapshot.RegionID)
	}
	
	// Add the snapshot to the collection
	collection.ReceivedSnapshots[teeID] = snapshot
	
	// Check if we have enough snapshots to create a regional snapshot
	if len(collection.ReceivedSnapshots) >= r.policy.MinTEECount {
		// In a real implementation, this would trigger consensus checking
		// and regional snapshot creation if appropriate
	}
	
	return nil
}

// FinalizeRegionalSnapshot creates a regional snapshot from collected TEE snapshots
func (r *RegionalSnapshotCoordinator) FinalizeRegionalSnapshot(
	ctx context.Context, 
	collectionID string,
) (*RegionalSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	// Verify the collection exists
	collection, exists := r.ongoingCollections[collectionID]
	if !exists {
		return nil, fmt.Errorf("snapshot collection not found: %s", collectionID)
	}
	
	// Verify we have enough snapshots
	receivedCount := len(collection.ReceivedSnapshots)
	if receivedCount < r.policy.MinTEECount {
		return nil, fmt.Errorf("%w: have %d, need %d", 
			ErrInsufficientTEESnapshots, receivedCount, r.policy.MinTEECount)
	}
	
	// Calculate the consensus level
	consensusLevel := float64(receivedCount) / float64(collection.TargetTEECount)
	if consensusLevel < r.policy.MinConsensusPercentage {
		return nil, fmt.Errorf("insufficient consensus: %.2f, required: %.2f", 
			consensusLevel, r.policy.MinConsensusPercentage)
	}
	
	// Perform snapshot consistency check
	if err := r.verifySnapshotConsistency(collection); err != nil {
		collection.CompletionStatus = "failed"
		return nil, fmt.Errorf("snapshot consistency verification failed: %w", err)
	}
	
	// Create the regional snapshot
	regionalSnapshot, err := r.createRegionalSnapshot(collection)
	if err != nil {
		collection.CompletionStatus = "failed"
		return nil, fmt.Errorf("%w: %v", ErrRegionalSnapshotCreationFailed, err)
	}
	
	// Mark the collection as completed
	collection.CompletionStatus = "completed"
	
	// Store the regional snapshot ID
	r.latestRegionalSnapshot = regionalSnapshot.SnapshotID
	
	// In a real implementation, this would also anchor the snapshot to the blockchain
	// and notify other regions about the new snapshot
	
	return regionalSnapshot, nil
}

// verifySnapshotConsistency checks that the collected snapshots are consistent
func (r *RegionalSnapshotCoordinator) verifySnapshotConsistency(
	collection *SnapshotCollectionStatus,
) error {
	// In a strict consistency check, we verify:
	// 1. All snapshots have timestamps within MaxTimeDrift of each other
	// 2. All snapshots have the same parent snapshot chain
	// 3. Object state is consistent across snapshots
	
	if len(collection.ReceivedSnapshots) < 2 {
		// Single snapshot is always consistent with itself
		return nil
	}
	
	// Find the earliest and latest timestamps
	var earliestTime, latestTime time.Time
	first := true
	
	for _, snapshot := range collection.ReceivedSnapshots {
		if first || snapshot.Timestamp.Before(earliestTime) {
			earliestTime = snapshot.Timestamp
		}
		if first || snapshot.Timestamp.After(latestTime) {
			latestTime = snapshot.Timestamp
		}
		first = false
	}
	
	// Check time drift
	maxDrift := time.Duration(r.policy.MaxTimeDrift) * time.Second
	if latestTime.Sub(earliestTime) > maxDrift {
		return fmt.Errorf("time drift between snapshots exceeds maximum: %v > %v",
			latestTime.Sub(earliestTime), maxDrift)
	}
	
	// If consistency level is "strict", perform additional checks
	if r.policy.ConsistencyLevel == "strict" {
		// In a real implementation, this would check for consistent state across TEEs
		// For example, by comparing MerkleDB root hashes
	}
	
	return nil
}

// createRegionalSnapshot creates a regional snapshot from the collected TEE snapshots
func (r *RegionalSnapshotCoordinator) createRegionalSnapshot(
	collection *SnapshotCollectionStatus,
) (*RegionalSnapshot, error) {
	// Create the regional snapshot
	teeSnapshots := make([]*StateSnapshot, 0, len(collection.ReceivedSnapshots))
	teeSnapshotIDs := make([][]byte, 0, len(collection.ReceivedSnapshots))
	
	for _, snapshot := range collection.ReceivedSnapshots {
		teeSnapshots = append(teeSnapshots, snapshot)
		teeSnapshotIDs = append(teeSnapshotIDs, snapshot.SnapshotID)
	}
	
	// Create a summary of the TEE snapshots
	summary, err := r.createSnapshotSummary(teeSnapshots)
	if err != nil {
		return nil, fmt.Errorf("failed to create snapshot summary: %w", err)
	}
	
	// Create consensus info
	consensusInfo := &SnapshotConsensusInfo{
		TEECount:          collection.TargetTEECount,
		ParticipatingTEEs: len(collection.ReceivedSnapshots),
		ConsensusLevel:    float64(len(collection.ReceivedSnapshots)) / float64(collection.TargetTEECount),
		ConsensusMethod:   "majority",
		ConsensusSuccess:  true,
	}
	
	// Create the regional snapshot with all aggregated data
	regionalSnapshot := &RegionalSnapshot{
		RegionID:           r.regionID,
		Timestamp:          time.Now().UTC(),
		TEESnapshots:       teeSnapshots,
		TEESnapshotIDs:     teeSnapshotIDs,
		SnapshotSummary:    summary,
		ConsensusInfo:      consensusInfo,
		Metadata:           make(map[string]interface{}),
	}
	
	// Generate ID after creating the snapshot
	regionalSnapshot.SnapshotID = computeRegionalSnapshotID(regionalSnapshot)
	
	// Generate a signature for the regional snapshot
	// In a real implementation, this would use a secure signing mechanism
	regionalSnapshot.CoordinatorSignature = r.signRegionalSnapshot(regionalSnapshot)
	
	// Generate a unique ID for the regional snapshot
	regionalSnapshot.SnapshotID = computeRegionalSnapshotID(regionalSnapshot)
	
	return regionalSnapshot, nil
}

// createSnapshotSummary creates a summary of the TEE snapshots
func (r *RegionalSnapshotCoordinator) createSnapshotSummary(
	snapshots []*StateSnapshot,
) (*SnapshotSummary, error) {
	if len(snapshots) == 0 {
		return nil, errors.New("no snapshots provided")
	}
	
	// Create a Merkle tree of snapshot IDs
	snapshotIDs := make([][]byte, len(snapshots))
	for i, snapshot := range snapshots {
		snapshotIDs[i] = snapshot.SnapshotID
	}
	merkleRoot := computeMerkleRoot(snapshotIDs)
	
	// Collect state root hashes by TEE ID
	stateRootHashes := make(map[string][]byte, len(snapshots))
	for _, snapshot := range snapshots {
		// In a real implementation, this would extract the state root hash
		// from the snapshot's MerkleDB metadata
		stateRootHashes[snapshot.TEEID] = []byte("mock-state-root")
	}
	
	// Calculate total state size
	var totalStateSize int64
	for _, snapshot := range snapshots {
		totalStateSize += int64(len(snapshot.StateData))
	}
	
	// Create regional metrics
	regionalMetrics := make(map[string]float64)
	// In a real implementation, this would aggregate metrics from all snapshots
	
	return &SnapshotSummary{
		MerkleRoot:      merkleRoot,
		StateRootHashes: stateRootHashes,
		ObjectCount:     len(snapshots),
		TotalStateSize:  totalStateSize,
		RegionalMetrics: regionalMetrics,
	}, nil
}

// signRegionalSnapshot generates a signature for a regional snapshot
func (r *RegionalSnapshotCoordinator) signRegionalSnapshot(
	snapshot *RegionalSnapshot,
) []byte {
	// In a real implementation, this would use a secure signing mechanism
	// For now, we'll create a mock signature
	h := sha256.New()
	h.Write([]byte(snapshot.RegionID))
	h.Write([]byte(snapshot.Timestamp.String()))
	for _, id := range snapshot.TEESnapshotIDs {
		h.Write(id)
	}
	
	return h.Sum(nil)
}

// GetLatestRegionalSnapshot returns the latest regional snapshot
func (r *RegionalSnapshotCoordinator) GetLatestRegionalSnapshot() (*RegionalSnapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	if r.latestRegionalSnapshot == nil {
		return nil, fmt.Errorf("no regional snapshot available")
	}
	
	// In a real implementation, this would retrieve the snapshot from storage
	// For now, we'll return a placeholder
	return nil, fmt.Errorf("regional snapshot retrieval not implemented")
}

// computeRegionalSnapshotID generates a unique ID for a regional snapshot
func computeRegionalSnapshotID(snapshot *RegionalSnapshot) []byte {
	h := sha256.New()
	h.Write([]byte(snapshot.RegionID))
	h.Write([]byte(snapshot.Timestamp.String()))
	for _, id := range snapshot.TEESnapshotIDs {
		h.Write(id)
	}
	h.Write(snapshot.CoordinatorSignature)
	
	return h.Sum(nil)
}

// computeMerkleRoot computes the Merkle root of a list of byte slices
func computeMerkleRoot(items [][]byte) []byte {
	if len(items) == 0 {
		return nil
	}
	
	if len(items) == 1 {
		h := sha256.New()
		h.Write(items[0])
		return h.Sum(nil)
	}
	
	// Simple implementation - in production, use a proper Merkle tree library
	h := sha256.New()
	for _, item := range items {
		h.Write(item)
	}
	
	return h.Sum(nil)
}

// GetSnapshotByID retrieves a snapshot by its ID with caching for performance
func (r *RegionalSnapshotCoordinator) GetSnapshotByID(snapshotID string) (*RegionalSnapshot, error) {
	// First check the cache for fast retrieval
	r.cacheMutex.RLock()
	if snapshot, ok := r.snapshotCache[snapshotID]; ok {
		r.cacheMutex.RUnlock()
		return snapshot, nil
	}
	r.cacheMutex.RUnlock()
	
	// Not in cache, retrieve from storage
	snapshot, err := r.snapshotStorage.GetSnapshot(snapshotID)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve snapshot %s from storage: %w", snapshotID, err)
	}
	
	// Add to cache for future retrievals
	r.cacheMutex.Lock()
	defer r.cacheMutex.Unlock()
	
	r.snapshotCache[snapshotID] = snapshot
	
	// Cache management: prevent unbounded growth
	if len(r.snapshotCache) > 100 { // Configurable max cache size
		// Simple cache eviction: remove oldest 20% of entries
		// In a production system, use a proper LRU cache
		toRemove := len(r.snapshotCache) / 5
		count := 0
		for k := range r.snapshotCache {
			if count >= toRemove {
				break
			}
			delete(r.snapshotCache, k)
			count++
		}
	}
	
	return snapshot, nil
}

// StartScheduler starts the periodic snapshot scheduler
func (r *RegionalSnapshotCoordinator) StartScheduler() {
	if r.scheduler != nil {
		r.scheduler.Start()
	}
}

// StopScheduler stops the periodic snapshot scheduler
func (r *RegionalSnapshotCoordinator) StopScheduler() {
	if r.scheduler != nil {
		r.scheduler.Stop()
	}
}

// SetCompressionLevel sets the compression level for snapshots (0-9)
func (r *RegionalSnapshotCoordinator) SetCompressionLevel(level int) {
	if level < 0 {
		level = 0
	} else if level > 9 {
		level = 9
	}
	r.compressionLevel = level
}

// GetPerformanceMetrics returns performance metrics for snapshot operations
func (r *RegionalSnapshotCoordinator) GetPerformanceMetrics() *CoordinatorMetrics {
	r.metrics.LastMutex.RLock()
	defer r.metrics.LastMutex.RUnlock()
	
	// Update time since last snapshot
	if !r.metrics.LastSnapshotTime.IsZero() {
		r.metrics.TimeSinceLastSnapshotSec = int64(time.Since(r.metrics.LastSnapshotTime).Seconds())
	}
	
	// Return a copy to prevent race conditions
	metricsCopy := *r.metrics
	return &metricsCopy
}

// StoreRegionalSnapshot stores a regional snapshot
func (r *RegionalSnapshotCoordinator) StoreRegionalSnapshot(
	snapshot *RegionalSnapshot,
) error {
	// In a real implementation, this would store the snapshot in a persistent store
	// For now, we'll just return success
	return nil
}

// EnableBlockchainAnchoring enables anchoring of regional snapshots to the blockchain
func (r *RegionalSnapshotCoordinator) EnableBlockchainAnchoring(enabled bool, endpoint string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	r.blockchainAnchorEnabled = enabled
	r.blockchainEndpoint = endpoint
	
	// Clear any existing client when changing settings
	r.blockchainClient = nil
}

// AnchorToBlockchain anchors a regional snapshot to the blockchain
func (r *RegionalSnapshotCoordinator) AnchorToBlockchain(
	ctx context.Context,
	snapshot *RegionalSnapshot,
) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	if !r.blockchainAnchorEnabled {
		return nil
	}
	
	// Create a blockchain client if needed
	if r.blockchainClient == nil {
		if r.blockchainEndpoint == "" {
			return fmt.Errorf("blockchain endpoint not configured")
		}
		r.blockchainClient = NewDefaultBlockchainClient(r.blockchainEndpoint)
	}
	
	// Convert the snapshot to the minimal format for blockchain storage
	anchorData, err := CreateBlockchainAnchorData(snapshot)
	if err != nil {
		return fmt.Errorf("failed to create blockchain anchor data: %w", err)
	}
	
	// Serialize the anchor data for blockchain storage
	anchorJSON, err := json.Marshal(anchorData)
	if err != nil {
		return fmt.Errorf("failed to serialize blockchain anchor data: %w", err)
	}
	
	// Submit the anchor data to the blockchain
	txID, err := r.blockchainClient.AnchorData(ctx, anchorJSON)
	if err != nil {
		return fmt.Errorf("failed to anchor data to blockchain: %w", err)
	}
	
	// Store the transaction ID as part of the snapshot's metadata
	if snapshot.Metadata == nil {
		snapshot.Metadata = make(map[string]interface{})
	}
	snapshot.Metadata["blockchain_tx_id"] = txID
	snapshot.Metadata["blockchain_anchor_time"] = time.Now().UTC().Format(time.RFC3339)
	
	return nil
}
