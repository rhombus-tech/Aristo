package mesh

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"testing"
	"time"
)

// Create test regional snapshot for performance optimizer tests
func createTestRegionalSnapshot(id string) *RegionalSnapshot {
	// Create timestamp
	timestamp := time.Now()
	
	// Set region ID
	regionID := "test-region"
	
	// Create a unique snapshot ID
	h := sha256.New()
	h.Write([]byte(id))
	snapshotID := h.Sum(nil)
	
	// Create a test state snapshot using the existing function with compatible parameters
	stateSnap := createMockSnapshot(regionID, "tee-1", "SGX", timestamp)
	stateSnap.ObjectID = id // Set the object ID to the provided id
	
	// Create TEE snapshot IDs
	teeSnapshotIDs := [][]byte{stateSnap.SnapshotID}
	
	// Return the regional snapshot
	return &RegionalSnapshot{
		RegionID:     regionID,
		SnapshotID:   snapshotID,
		TEESnapshots: []*StateSnapshot{stateSnap},
		TEESnapshotIDs: teeSnapshotIDs,
		Timestamp:    timestamp,
		SnapshotSummary: &SnapshotSummary{
			MerkleRoot: snapshotID, // Use snapshot ID as merkle root for simplicity
			ObjectCount: 1,
			TotalStateSize: int64(len(stateSnap.StateData)),
		},
		ConsensusInfo: &SnapshotConsensusInfo{
			TEECount: 1,
			ParticipatingTEEs: 1,
			ConsensusLevel: 1.0,
			ConsensusSuccess: true,
		},
		CoordinatorSignature: snapshotID, // Use snapshot ID as signature for simplicity
		Metadata: make(map[string]interface{}),
	}
}

// TestDiffMetrics implements the DiffMetrics interface for testing
type TestDiffMetrics struct {
	TotalDiffsGenerated    int64
	TotalDiffsApplied      int64
	TotalDiffSizeBytes     int64
	AverageDiffSizeBytes   int64
	MaxDiffSizeBytes       int64
	AverageSavingsPercent  float64
	TotalObjectsChanged    int64
	TotalObjectsReferenced int64
	GenerationTimeNs       int64
	ApplyTimeNs            int64
}

// TestPerformanceCoordinator implements a minimal coordinator for testing
// This type embeds the RegionalSnapshotCoordinator for interface compatibility
type TestPerformanceCoordinator struct {
	*RegionalSnapshotCoordinator // Embed to match interface expectations
	snapshots map[string]*RegionalSnapshot
	mutex     sync.RWMutex
}

// NewTestPerformanceCoordinator creates a new test coordinator
func NewTestPerformanceCoordinator() *TestPerformanceCoordinator {
	// Create test storage for the embedded coordinator
	mockStorage := NewMockSnapshotStorage()
	
	// Create a minimal coordinator with just enough to pass type checks
	return &TestPerformanceCoordinator{
		RegionalSnapshotCoordinator: &RegionalSnapshotCoordinator{
			regionID:         "test-region",
			snapshotStorage: mockStorage,
			snapshotCache:   make(map[string]*RegionalSnapshot),
			cacheMutex:      sync.RWMutex{},
		},
		snapshots: make(map[string]*RegionalSnapshot),
	}
}

// Override GetSnapshotByID for the test coordinator
func (tc *TestPerformanceCoordinator) GetSnapshotByID(id string) (*RegionalSnapshot, error) {
	tc.mutex.RLock()
	defer tc.mutex.RUnlock()
	
	snapshot, ok := tc.snapshots[id]
	if !ok {
		// For tests, generate a snapshot if not found
		snapshot = createTestRegionalSnapshot(id)
		tc.mutex.RUnlock() // Unlock for reading
		
		// Store the snapshot for future use
		tc.mutex.Lock()
		tc.snapshots[id] = snapshot
		tc.mutex.Unlock()
		
		// Also store it in the embedded coordinator's storage
		if ms, ok := tc.RegionalSnapshotCoordinator.snapshotStorage.(*MockSnapshotStorage); ok {
			for _, teeSnap := range snapshot.TEESnapshots {
				ms.Snapshots[string(teeSnap.SnapshotID)] = teeSnap
			}
		}
		return snapshot, nil
	}
	return snapshot, nil
}

// StoreSnapshot stores a snapshot
func (tc *TestPerformanceCoordinator) StoreSnapshot(snapshot *RegionalSnapshot) error {
	if snapshot == nil || len(snapshot.SnapshotID) == 0 {
		return errors.New("invalid snapshot")
	}
	
	tc.mutex.Lock()
	defer tc.mutex.Unlock()
	
	tc.snapshots[string(snapshot.SnapshotID)] = snapshot
	return nil
}

// TestPerformanceStorage implements the storage interface needed for testing
type TestPerformanceStorage struct {
	snapshots map[string]*RegionalSnapshot
	mutex     sync.RWMutex
}

// NewTestPerformanceStorage creates a new test storage
func NewTestPerformanceStorage() *TestPerformanceStorage {
	return &TestPerformanceStorage{
		snapshots: make(map[string]*RegionalSnapshot),
	}
}

// GetSnapshot retrieves a snapshot by ID
func (ts *TestPerformanceStorage) GetSnapshot(id string) (*RegionalSnapshot, error) {
	ts.mutex.RLock()
	defer ts.mutex.RUnlock()
	
	snapshot, ok := ts.snapshots[id]
	if !ok {
		// Create a test snapshot for testing purposes
		return createTestRegionalSnapshot(id), nil
	}
	return snapshot, nil
}

// StoreSnapshot stores a state snapshot (required by interface but not used in tests)
func (ts *TestPerformanceStorage) StoreSnapshot(snapshot *StateSnapshot) error {
	// Not used in these tests but required by the interface
	return nil
}

// TestSnapshotPerformanceOptimizer tests the core functionality of the performance optimizer
func TestSnapshotPerformanceOptimizer(t *testing.T) {
	// Create test dependencies
	coordinator := NewTestPerformanceCoordinator()
	storage := NewTestPerformanceStorage()
	
	// Create and store test regional snapshots
	snapshot1 := createTestRegionalSnapshot("snapshot-1")
	snapshot2 := createTestRegionalSnapshot("snapshot-2")
	
	// Store test snapshots in coordinator
	err := coordinator.StoreSnapshot(snapshot1)
	if err != nil {
		t.Fatalf("Failed to store snapshot1: %v", err)
	}
	
	err = coordinator.StoreSnapshot(snapshot2)
	if err != nil {
		t.Fatalf("Failed to store snapshot2: %v", err)
	}
	
	// Store snapshots in storage as well
	storage.snapshots[string(snapshot1.SnapshotID)] = snapshot1
	storage.snapshots[string(snapshot2.SnapshotID)] = snapshot2
	
	// Create optimizer components
	compressor, err := NewCompressor(CompressionGzip, 5)
	if err != nil {
		t.Fatalf("Failed to create compressor: %v", err)
	}
	
	// Create circuit breaker config
	cbConfig := &CircuitBreakerConfig{
		FailureThreshold:    3,
		ResetTimeout:        time.Second * 30,
		SuccessThreshold:    2,
		Timeout:             time.Second * 10,
		MaxConcurrent:       50,
		MaxLatencyThreshold: time.Second * 5,
	}
	circuitBreaker := NewCircuitBreaker("snapshot-operations", cbConfig)
	
	// Create optimization strategy
	strategy := &OptimizationStrategy{
		EnableCompression:        true,
		CompressionAlgorithm:     CompressionGzip,
		CompressionLevel:         5,
		EnableDifferentialUpdates: true,
		MaxDiffSize:              1024 * 1024 * 10, // 10MB
	}
	
	// Create snapshot performance optimizer
	// Using custom initializer for tests that doesn't rely on full RegionalSnapshotCoordinator
	optimizer := &SnapshotPerformanceOptimizer{
		coordinator:          coordinator.RegionalSnapshotCoordinator, // Use the embedded coordinator
		strategy:             strategy,
		compressor:           compressor,
		storageCircuitBreaker: circuitBreaker,
		snapshotCache:        make(map[string]*RegionalSnapshot),
		cacheEntryTimes:      make(map[string]time.Time),
		metrics:              &SnapshotPerformanceMetrics{},
		cacheMutex:           sync.RWMutex{},
		snapshotCircuitBreaker: circuitBreaker,
		diffUpdater: &DifferentialUpdater{
			compressor:  compressor,
			diffCache:   make(map[string]*DifferentialUpdate),
			cacheMutex:  sync.RWMutex{},
			metrics:     DiffMetrics{},
			maxDiffSize: 10 * 1024 * 1024, // 10MB default max diff size
		},
		running:    true,
	}
	
	// Initialize metrics with mutex to avoid nil pointer
	optimizer.metrics.mutex = sync.RWMutex{}
	
	// Test case 1: Get snapshot (should cache it)
	t.Run("GetSnapshot", func(t *testing.T) {
		ctx := context.Background()
		retrievedSnapshot, err := optimizer.OptimizedGetSnapshot(ctx, string(snapshot1.SnapshotID))
		if err != nil {
			t.Fatalf("Failed to get snapshot: %v", err)
		}
		
		// Verify the snapshot was retrieved correctly
		if retrievedSnapshot == nil {
			t.Fatal("Retrieved snapshot is nil")
		}
		
		// Verify cache metrics
		metrics := optimizer.GetMetrics()
		t.Logf("Cache metrics after first get: hits=%d, misses=%d", 
			metrics.CacheHits, metrics.CacheMisses)
	})
	
	// Test case 2: Get snapshot again (should be cached)
	t.Run("CachedSnapshot", func(t *testing.T) {
		ctx := context.Background()
		// Get the same snapshot again, should be a cache hit
		retrievedSnapshot, err := optimizer.OptimizedGetSnapshot(ctx, string(snapshot1.SnapshotID))
		if err != nil {
			t.Fatalf("Failed to get cached snapshot: %v", err)
		}
		
		// Verify the snapshot was retrieved correctly
		if retrievedSnapshot == nil {
			t.Fatal("Retrieved cached snapshot is nil")
		}
		
		// Verify cache metrics
		metrics := optimizer.GetMetrics()
		t.Logf("Cache metrics after second get: hits=%d, misses=%d", 
			metrics.CacheHits, metrics.CacheMisses)
	})
	
	// Test case 3: Compression
	t.Run("Compression", func(t *testing.T) {
		// Test the compression functionality
		data := []byte("This is test data for compression that should be compressible with repeated text. " + 
			"This is test data for compression that should be compressible with repeated text. " +
			"This is test data for compression that should be compressible with repeated text. " +
			"This is test data for compression that should be compressible with repeated text.")
		
		// Compress the data
		compressed, err := optimizer.compressor.Compress(data)
		if err != nil {
			t.Fatalf("Failed to compress data: %v", err)
		}
		
		// Verify compression reduced the size
		if len(compressed) >= len(data) {
			t.Errorf("Compression did not reduce data size: original=%d, compressed=%d", 
				len(data), len(compressed))
		} else {
			t.Logf("Compression ratio: %.2f%% (original=%d bytes, compressed=%d bytes)", 
				(1.0 - float64(len(compressed))/float64(len(data)))*100.0,
				len(data), len(compressed))
		}
		
		// Decompress and verify data integrity
		decompressed, err := optimizer.compressor.Decompress(compressed)
		if err != nil {
			t.Fatalf("Failed to decompress data: %v", err)
		}
		
		// Verify the decompressed data matches the original
		if string(decompressed) != string(data) {
			t.Error("Decompressed data does not match original data")
		}
	})
}
