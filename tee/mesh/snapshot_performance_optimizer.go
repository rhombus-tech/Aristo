// Package mesh provides a mesh network for TEE-to-TEE communication
package mesh

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
	
	"github.com/rhombus-tech/vm/tee/proto"
)

// PerformanceLevel defines different performance optimization levels
type PerformanceLevel int

const (
	// PerfLevelMinimal focuses on minimal overhead with basic optimizations
	PerfLevelMinimal PerformanceLevel = iota
	// PerfLevelBalanced balances performance with storage optimization
	PerfLevelBalanced
	// PerfLevelAggressive implements aggressive optimizations
	PerfLevelAggressive
	// PerfLevelMaximum implements maximum possible optimizations
	PerfLevelMaximum
)

// OptimizationStrategy controls which optimizations are applied
type OptimizationStrategy struct {
	// Compression settings
	EnableCompression    bool
	CompressionAlgorithm string
	CompressionLevel     int

	// Differential updates
	EnableDifferentialUpdates bool
	MaxDiffSize               int64
	DiffGenerationInterval    time.Duration

	// Performance prioritization
	PrioritizeLatency bool // If true, prioritize latency over storage efficiency
	
	// Circuit breaker settings
	EnableCircuitBreaker bool
	CircuitBreakerConfig *CircuitBreakerConfig
	
	// Cache settings
	MaxCacheEntries     int
	CacheTTL            time.Duration
	PrewarmCache        bool
	
	// Metrics collection
	MetricsCollectionInterval time.Duration
	DetailedMetrics           bool
}

// DefaultOptimizationStrategy returns sensible defaults for snapshot optimization
func DefaultOptimizationStrategy() *OptimizationStrategy {
	return &OptimizationStrategy{
		EnableCompression:         true,
		CompressionAlgorithm:      CompressionZstd,
		CompressionLevel:          3, // Default compression level
		EnableDifferentialUpdates: true,
		MaxDiffSize:               50 * 1024 * 1024, // 50MB
		DiffGenerationInterval:    10 * time.Minute,
		PrioritizeLatency:         true,
		EnableCircuitBreaker:      true,
		CircuitBreakerConfig:      DefaultCircuitBreakerConfig(),
		MaxCacheEntries:           1000,
		CacheTTL:                  4 * time.Hour,
		PrewarmCache:              true,
		MetricsCollectionInterval: 1 * time.Minute,
		DetailedMetrics:           true,
	}
}

// OptimizationStrategyForLevel returns a strategy tuned for a specific performance level
func OptimizationStrategyForLevel(level PerformanceLevel) *OptimizationStrategy {
	strategy := DefaultOptimizationStrategy()
	
	switch level {
	case PerfLevelMinimal:
		// Minimal overhead, focus on performance
		strategy.EnableCompression = false
		strategy.EnableDifferentialUpdates = false
		strategy.PrioritizeLatency = true
		strategy.MaxCacheEntries = 100
		strategy.DetailedMetrics = false
		
	case PerfLevelBalanced:
		// Balanced approach (default settings)
		
	case PerfLevelAggressive:
		// More aggressive optimizations
		strategy.CompressionLevel = 6
		strategy.MaxDiffSize = 100 * 1024 * 1024 // 100MB
		strategy.DiffGenerationInterval = 5 * time.Minute
		strategy.MaxCacheEntries = 5000
		
	case PerfLevelMaximum:
		// Maximum possible optimizations
		strategy.CompressionAlgorithm = CompressionZstd
		strategy.CompressionLevel = 9 // Maximum compression
		strategy.MaxDiffSize = 200 * 1024 * 1024 // 200MB
		strategy.DiffGenerationInterval = 1 * time.Minute
		strategy.PrioritizeLatency = false // Prioritize storage efficiency
		strategy.MaxCacheEntries = 10000
		strategy.CacheTTL = 24 * time.Hour
	}
	
	return strategy
}

// SnapshotPerformanceOptimizer manages performance optimization for snapshots
type SnapshotPerformanceOptimizer struct {
	strategy            *OptimizationStrategy
	compressor          *Compressor
	diffUpdater         *DifferentialUpdater
	snapshotCircuitBreaker *CircuitBreaker
	storageCircuitBreaker  *CircuitBreaker
	metrics             *SnapshotPerformanceMetrics
	coordinator         *RegionalSnapshotCoordinator
	
	// Cache for frequently accessed snapshots
	snapshotCache      map[string]*RegionalSnapshot // ID -> Snapshot
	cacheMutex         sync.RWMutex
	cacheEntryTimes    map[string]time.Time // ID -> Last Access Time
	
	// Operational state
	running           bool
	stopChan          chan struct{}
	metricCollectChan chan struct{}
}

// SnapshotPerformanceMetrics collects detailed performance metrics
type SnapshotPerformanceMetrics struct {
	// Snapshot metrics
	SnapshotCreationCount int64
	SnapshotCreationLatencyAvgMs float64
	SnapshotSizeAvgBytes int64
	SnapshotSizeMaxBytes int64
	SnapshotSizeMinBytes int64
	
	// Compression metrics
	CompressionRatio float64
	CompressionTimeAvgMs float64
	BytesSavedByCompression int64
	
	// Differential updates metrics
	DiffGenerationCount int64
	DiffGenerationLatencyAvgMs float64
	DiffApplyCount int64
	DiffApplyLatencyAvgMs float64
	DiffSizeAvgBytes int64
	DiffSavingsPercent float64
	
	// Cache metrics
	CacheHits int64
	CacheMisses int64
	CacheHitRatio float64
	CacheEvictionCount int64
	
	// Circuit breaker metrics
	SnapshotCircuitBreakerTrips int64
	StorageCircuitBreakerTrips int64
	FailedOperationsCount int64
	
	// Storage metrics
	TotalBytesStored int64
	TotalBytesStoredWithoutOptimization int64
	StorageSpaceSaved int64
	StorageEfficiencyPercent float64
	
	// System state
	SystemUtilization float64
	lastUpdateTime time.Time
	
	// Mutex for updates
	mutex sync.RWMutex
}

// NewSnapshotPerformanceOptimizer creates a new optimizer with the specified strategy
func NewSnapshotPerformanceOptimizer(
	coordinator *RegionalSnapshotCoordinator,
	strategy *OptimizationStrategy,
) (*SnapshotPerformanceOptimizer, error) {
	if coordinator == nil {
		return nil, fmt.Errorf("coordinator is required")
	}
	
	if strategy == nil {
		strategy = DefaultOptimizationStrategy()
	}
	
	// Create optimizer components
	var compressor *Compressor
	var err error
	
	if strategy.EnableCompression {
		compressor, err = NewCompressor(strategy.CompressionAlgorithm, strategy.CompressionLevel)
		if err != nil {
			return nil, fmt.Errorf("failed to create compressor: %w", err)
		}
	}
	
	// Create circuit breakers
	var snapshotCB, storageCB *CircuitBreaker
	if strategy.EnableCircuitBreaker {
		snapshotCB = NewCircuitBreaker("snapshot_operations", strategy.CircuitBreakerConfig)
		storageCB = NewCircuitBreaker("storage_operations", strategy.CircuitBreakerConfig)
	}
	
	// Create the optimizer
	optimizer := &SnapshotPerformanceOptimizer{
		strategy:            strategy,
		compressor:          compressor,
		coordinator:         coordinator,
		snapshotCircuitBreaker: snapshotCB,
		storageCircuitBreaker:  storageCB,
		metrics:             &SnapshotPerformanceMetrics{
			lastUpdateTime: time.Now(),
		},
		snapshotCache:      make(map[string]*RegionalSnapshot),
		cacheEntryTimes:    make(map[string]time.Time),
		stopChan:           make(chan struct{}),
		metricCollectChan:  make(chan struct{}),
	}
	
	// Create differential updater if enabled
	if strategy.EnableDifferentialUpdates {
		optimizer.diffUpdater = NewDifferentialUpdater(
			coordinator.snapshotStorage,
			compressor,
		)
		optimizer.diffUpdater.SetMaxDiffSize(strategy.MaxDiffSize)
	}
	
	// Register circuit breaker callbacks
	if strategy.EnableCircuitBreaker {
		snapshotCB.AddStateChangeCallback(func(name string, state CircuitBreakerState) {
			if state == CircuitOpen {
				atomic.AddInt64(&optimizer.metrics.SnapshotCircuitBreakerTrips, 1)
			}
		})
		
		storageCB.AddStateChangeCallback(func(name string, state CircuitBreakerState) {
			if state == CircuitOpen {
				atomic.AddInt64(&optimizer.metrics.StorageCircuitBreakerTrips, 1)
			}
		})
	}
	
	return optimizer, nil
}

// Start begins background optimization tasks
func (o *SnapshotPerformanceOptimizer) Start(ctx context.Context) error {
	if o.running {
		return nil // Already running
	}
	
	o.running = true
	
	// Start background goroutines
	go o.metricsCollectionLoop(ctx)
	go o.cacheMaintenanceLoop(ctx)
	
	// Prewarm cache if enabled
	if o.strategy.PrewarmCache {
		go o.prewarmCache(ctx)
	}
	
	return nil
}

// Stop terminates background optimization tasks
func (o *SnapshotPerformanceOptimizer) Stop() {
	if !o.running {
		return
	}
	
	close(o.stopChan)
	o.running = false
}

// OptimizedCreateSnapshot creates a snapshot with performance optimizations
func (o *SnapshotPerformanceOptimizer) OptimizedCreateSnapshot(
	ctx context.Context,
	collectionID string,
) (*RegionalSnapshot, error) {
	startTime := time.Now()
	
	// Use circuit breaker if enabled
	if o.snapshotCircuitBreaker != nil {
		var snapshot *RegionalSnapshot
		var err error
		
		cbErr := o.snapshotCircuitBreaker.Execute(ctx, func() error {
			var opErr error
			snapshot, opErr = o.coordinator.FinalizeRegionalSnapshot(ctx, collectionID)
			return opErr
		})
		
		if cbErr != nil {
			atomic.AddInt64(&o.metrics.FailedOperationsCount, 1)
			return nil, fmt.Errorf("snapshot creation failed: %w", cbErr)
		}
		
		// Apply optimizations to the snapshot
		if err = o.applySnapshotOptimizations(snapshot); err != nil {
			return snapshot, fmt.Errorf("optimization failed, returning unoptimized snapshot: %w", err)
		}
		
		// Update metrics
		o.updateSnapshotMetrics(snapshot, time.Since(startTime))
		return snapshot, nil
	}
	
	// Direct execution without circuit breaker
	snapshot, err := o.coordinator.FinalizeRegionalSnapshot(ctx, collectionID)
	if err != nil {
		atomic.AddInt64(&o.metrics.FailedOperationsCount, 1)
		return nil, err
	}
	
	// Apply optimizations
	if err = o.applySnapshotOptimizations(snapshot); err != nil {
		return snapshot, fmt.Errorf("optimization failed, returning unoptimized snapshot: %w", err)
	}
	
	// Update metrics
	o.updateSnapshotMetrics(snapshot, time.Since(startTime))
	return snapshot, nil
}

// OptimizedGetSnapshot retrieves a snapshot with performance optimizations
func (o *SnapshotPerformanceOptimizer) OptimizedGetSnapshot(ctx context.Context, snapshotID string) (*RegionalSnapshot, error) {
	// Use the snapshotCache - first check in cache
	o.cacheMutex.RLock()
	cachedSnapshot, found := o.snapshotCache[snapshotID]
	o.cacheMutex.RUnlock()
	
	if found {
		// Cache hit
		atomic.AddInt64(&o.metrics.CacheHits, 1)
		return cachedSnapshot, nil
	} 
	
	// Cache miss
	atomic.AddInt64(&o.metrics.CacheMisses, 1)
	
	// Use circuit breaker if enabled
	if o.storageCircuitBreaker != nil {
		var snapshot *RegionalSnapshot
		
		cbErr := o.storageCircuitBreaker.Execute(ctx, func() error {
			var opErr error
			snapshot, opErr = o.coordinator.GetSnapshotByID(snapshotID)
			return opErr
		})
		
		if cbErr != nil {
			atomic.AddInt64(&o.metrics.FailedOperationsCount, 1)
			return nil, fmt.Errorf("snapshot retrieval failed: %w", cbErr)
		}
		
		// Add to cache
		o.cacheMutex.Lock()
		o.snapshotCache[snapshotID] = snapshot
		o.cacheEntryTimes[snapshotID] = time.Now()
		o.cacheMutex.Unlock()
		
		return snapshot, nil
	}
	
	// Direct execution without circuit breaker
	snapshot, err := o.coordinator.GetSnapshotByID(snapshotID)
	if err != nil {
		atomic.AddInt64(&o.metrics.FailedOperationsCount, 1)
		return nil, err
	}
	
	// Add to cache
	o.cacheMutex.Lock()
	o.snapshotCache[snapshotID] = snapshot
	o.cacheEntryTimes[snapshotID] = time.Now()
	o.cacheMutex.Unlock()
	
	return snapshot, nil
}

// retrieveSnapshotBytes retrieves raw snapshot data as bytes
func (o *SnapshotPerformanceOptimizer) retrieveSnapshotBytes(ctx context.Context, snapshotID string) ([]byte, error) {
	// First try to get the snapshot object
	snapshot, err := o.OptimizedGetSnapshot(ctx, snapshotID)
	if err != nil {
		return nil, err
	}
	
	// Convert snapshot to bytes - in a real implementation we would serialize it properly
	// In the new structure, we have TEESnapshotIDs instead of TEESnapshots
	// We would need to fetch the actual snapshot data using these IDs
	if len(snapshot.TEESnapshotIDs) > 0 {
		// This is a placeholder - in a real implementation, you would fetch the actual snapshot data
		return []byte(fmt.Sprintf("TEE Snapshot data for %s", snapshot.TEESnapshotIDs[0])), nil
	}
	
	return []byte(fmt.Sprintf("Snapshot data for %s", snapshotID)), nil
}

// GenerateDifferentialUpdate compares snapshots and generates a differential update
func (o *SnapshotPerformanceOptimizer) GenerateDifferentialUpdate(ctx context.Context, baseSnapshotID, targetSnapshotID string) (*DifferentialUpdate, error) {
	startTime := time.Now()
	
	// Get the snapshots (using optimized retrieval)
	baseSnapshot, err := o.OptimizedGetSnapshot(ctx, baseSnapshotID)
	if err != nil {
		return nil, fmt.Errorf("failed to get base snapshot: %w", err)
	}
	
	targetSnapshot, err := o.OptimizedGetSnapshot(ctx, targetSnapshotID)
	if err != nil {
		return nil, fmt.Errorf("failed to get target snapshot: %w", err)
	}
	
	// Convert mesh.RegionalSnapshot to proto.RegionalSnapshot for differential updater
	protoBaseSnapshot := &proto.RegionalSnapshot{
		RegionId:      baseSnapshot.RegionID,
		SnapshotId:    baseSnapshot.SnapshotID,
		Timestamp:     baseSnapshot.Timestamp.UnixNano(),
		TeeSnapshotIds: baseSnapshot.TEESnapshotIDs,
	}

	protoTargetSnapshot := &proto.RegionalSnapshot{
		RegionId:      targetSnapshot.RegionID,
		SnapshotId:    targetSnapshot.SnapshotID,
		Timestamp:     targetSnapshot.Timestamp.UnixNano(),
		TeeSnapshotIds: targetSnapshot.TEESnapshotIDs,
	}

	// Use the differential updater to create a diff
	diffUpdate, err := o.diffUpdater.GenerateDiff(protoBaseSnapshot, protoTargetSnapshot)
	if err != nil {
		return nil, fmt.Errorf("failed to generate differential update: %w", err)
	}
	
	// Update metrics
	latency := time.Since(startTime).Milliseconds()
	// Update metrics manually
	o.metrics.mutex.Lock()
	o.metrics.DiffGenerationCount++
	o.metrics.DiffGenerationLatencyAvgMs = calculatePerformanceAverage(
		o.metrics.DiffGenerationLatencyAvgMs,
		float64(latency),
		float64(o.metrics.DiffGenerationCount),
	)
	// Set diff size metric if available
	if diffUpdate.Size > 0 {
		o.metrics.DiffSizeAvgBytes = calculateRunningAverageInt64(
			o.metrics.DiffSizeAvgBytes,
			diffUpdate.Size,
			o.metrics.DiffGenerationCount,
		)
	}
	o.metrics.mutex.Unlock()
	
	return diffUpdate, nil
}

// ApplyDifferentialUpdate applies a differential update to create a target snapshot
func (o *SnapshotPerformanceOptimizer) ApplyDifferentialUpdate(
	ctx context.Context,
	baseSnapshotID string,
	diff *DifferentialUpdate,
) (*RegionalSnapshot, error) {
	if !o.strategy.EnableDifferentialUpdates || o.diffUpdater == nil {
		return nil, fmt.Errorf("differential updates not enabled")
	}
	
	startTime := time.Now()
	
	// Get the base snapshot
	baseSnapshot, err := o.OptimizedGetSnapshot(ctx, baseSnapshotID)
	if err != nil {
		return nil, fmt.Errorf("failed to get base snapshot: %w", err)
	}
	
	// Convert mesh.RegionalSnapshot to proto.RegionalSnapshot for differential updater
	protoBaseSnapshot := &proto.RegionalSnapshot{
		RegionId:      baseSnapshot.RegionID,
		SnapshotId:    baseSnapshot.SnapshotID,
		Timestamp:     baseSnapshot.Timestamp.UnixNano(),
		TeeSnapshotIds: baseSnapshot.TEESnapshotIDs,
	}

	// Apply the diff to the base snapshot (using proto.RegionalSnapshot)
	protoResult, err := o.diffUpdater.ApplyDiff(protoBaseSnapshot, diff)
	if err != nil {
		return nil, fmt.Errorf("failed to apply differential update: %w", err)
	}

	// Convert proto.RegionalSnapshot back to mesh.RegionalSnapshot
	targetSnapshot := &RegionalSnapshot{
		RegionID:      protoResult.RegionId,
		SnapshotID:    protoResult.SnapshotId,
		Timestamp:     time.Unix(0, protoResult.Timestamp),
		TEESnapshotIDs: protoResult.TeeSnapshotIds,
	}
	
	// Update metrics
	o.metrics.mutex.Lock()
	o.metrics.DiffApplyCount++
	latencyMs := float64(time.Since(startTime).Milliseconds())
	o.metrics.DiffApplyLatencyAvgMs = calculatePerformanceAverage(
		o.metrics.DiffApplyLatencyAvgMs,
		latencyMs,
		float64(o.metrics.DiffApplyCount),
	)
	o.metrics.mutex.Unlock()
	
	// Add to cache
	o.addToCache(targetSnapshot)
	
	return targetSnapshot, nil
}

// GetMetrics returns current performance metrics
func (o *SnapshotPerformanceOptimizer) GetMetrics() SnapshotPerformanceMetrics {
	o.metrics.mutex.RLock()
	defer o.metrics.mutex.RUnlock()
	
	// Make a deep copy to avoid data races and copying mutex
	metricsCopy := SnapshotPerformanceMetrics{
		SnapshotCreationCount:        atomic.LoadInt64(&o.metrics.SnapshotCreationCount),
		SnapshotCreationLatencyAvgMs: o.metrics.SnapshotCreationLatencyAvgMs,
		SnapshotSizeAvgBytes:         atomic.LoadInt64(&o.metrics.SnapshotSizeAvgBytes),
		SnapshotSizeMaxBytes:         atomic.LoadInt64(&o.metrics.SnapshotSizeMaxBytes),
		SnapshotSizeMinBytes:         atomic.LoadInt64(&o.metrics.SnapshotSizeMinBytes),
		CompressionRatio:             o.metrics.CompressionRatio,
		CompressionTimeAvgMs:         o.metrics.CompressionTimeAvgMs,
		BytesSavedByCompression:      atomic.LoadInt64(&o.metrics.BytesSavedByCompression),
		DiffGenerationCount:          atomic.LoadInt64(&o.metrics.DiffGenerationCount),
		DiffGenerationLatencyAvgMs:   o.metrics.DiffGenerationLatencyAvgMs,
		DiffApplyCount:               atomic.LoadInt64(&o.metrics.DiffApplyCount),
		DiffApplyLatencyAvgMs:        o.metrics.DiffApplyLatencyAvgMs,
		DiffSizeAvgBytes:             atomic.LoadInt64(&o.metrics.DiffSizeAvgBytes),
		DiffSavingsPercent:           o.metrics.DiffSavingsPercent,
		CacheHits:                    atomic.LoadInt64(&o.metrics.CacheHits),
		CacheMisses:                  atomic.LoadInt64(&o.metrics.CacheMisses),
		CacheHitRatio:                o.metrics.CacheHitRatio,
		CacheEvictionCount:           atomic.LoadInt64(&o.metrics.CacheEvictionCount),
		SnapshotCircuitBreakerTrips:  atomic.LoadInt64(&o.metrics.SnapshotCircuitBreakerTrips),
		StorageCircuitBreakerTrips:   atomic.LoadInt64(&o.metrics.StorageCircuitBreakerTrips),
		FailedOperationsCount:        atomic.LoadInt64(&o.metrics.FailedOperationsCount),
		TotalBytesStored:             atomic.LoadInt64(&o.metrics.TotalBytesStored),
		TotalBytesStoredWithoutOptimization: atomic.LoadInt64(&o.metrics.TotalBytesStoredWithoutOptimization),
		StorageSpaceSaved:            atomic.LoadInt64(&o.metrics.StorageSpaceSaved),
		StorageEfficiencyPercent:     o.metrics.StorageEfficiencyPercent,
		SystemUtilization:            o.metrics.SystemUtilization,
		lastUpdateTime:               o.metrics.lastUpdateTime,
	}

	// Note: This ensures we're not returning the mutex
	return metricsCopy
}

// ForceMetricsCollection triggers an immediate collection of metrics
func (o *SnapshotPerformanceOptimizer) ForceMetricsCollection() {
	if o.running {
		o.metricCollectChan <- struct{}{}
	}
}

// UpdateStrategy updates the optimization strategy
func (o *SnapshotPerformanceOptimizer) UpdateStrategy(strategy *OptimizationStrategy) error {
	if strategy == nil {
		return fmt.Errorf("strategy cannot be nil")
	}
	
	// Update compression settings if they've changed
	if strategy.EnableCompression != o.strategy.EnableCompression ||
		strategy.CompressionAlgorithm != o.strategy.CompressionAlgorithm ||
		strategy.CompressionLevel != o.strategy.CompressionLevel {
		
		if strategy.EnableCompression {
			var err error
			o.compressor, err = NewCompressor(strategy.CompressionAlgorithm, strategy.CompressionLevel)
			if err != nil {
				return fmt.Errorf("failed to update compressor: %w", err)
			}
			
			// Note: DifferentialUpdater doesn't have a compressor field
			// We'll keep our own compressor in the optimizer
		} else {
			o.compressor = nil
		}
	}
	
	// Update differential update settings
	if o.diffUpdater != nil && strategy.MaxDiffSize != o.strategy.MaxDiffSize {
		o.diffUpdater.SetMaxDiffSize(strategy.MaxDiffSize)
	}
	
	// Store new strategy
	o.strategy = strategy
	
	return nil
}

// metricsCollectionLoop periodically collects performance metrics
func (o *SnapshotPerformanceOptimizer) metricsCollectionLoop(ctx context.Context) {
	if !o.strategy.DetailedMetrics {
		return
	}
	
	ticker := time.NewTicker(o.strategy.MetricsCollectionInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-o.stopChan:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			o.collectMetrics()
		case <-o.metricCollectChan:
			o.collectMetrics()
		}
	}
}

// collectMetrics gathers performance metrics from various components
func (o *SnapshotPerformanceOptimizer) collectMetrics() {
	o.metrics.mutex.Lock()
	defer o.metrics.mutex.Unlock()
	
	// Collect coordinator metrics
	if coordMetrics := o.coordinator.GetPerformanceMetrics(); coordMetrics != nil {
		// Update compression ratio
		o.metrics.CompressionRatio = coordMetrics.CompressionRatio
		
		// Update storage savings
		totalBytesRaw := coordMetrics.TotalBytesStored
		totalBytesCompressed := coordMetrics.TotalBytesCompressed
		o.metrics.TotalBytesStored = totalBytesCompressed
		o.metrics.TotalBytesStoredWithoutOptimization = totalBytesRaw
		
		if totalBytesRaw > 0 {
			o.metrics.StorageSpaceSaved = totalBytesRaw - totalBytesCompressed
			o.metrics.StorageEfficiencyPercent = 100.0 * (1.0 - float64(totalBytesCompressed)/float64(totalBytesRaw))
		}
	}
	
	// Collect compressor metrics if available
	if o.compressor != nil {
		compStats := o.compressor.GetCompressionStats()
		o.metrics.CompressionRatio = compStats["compressionRatio"].(float64)
		o.metrics.CompressionTimeAvgMs = compStats["averageTimeMs"].(float64)
		o.metrics.BytesSavedByCompression = compStats["totalBytesIn"].(int64) - compStats["totalBytesOut"].(int64)
	}
	
	// Collect differential metrics if available
	if o.diffUpdater != nil {
		diffMetrics := o.diffUpdater.GetMetrics()
		o.metrics.DiffSavingsPercent = diffMetrics.AverageSavingsPercent
		o.metrics.DiffSizeAvgBytes = diffMetrics.AverageDiffSizeBytes
	}
	
	// Update system utilization (artificial value for demo)
	o.metrics.SystemUtilization = 0.5
	
	// Update last collection time
	o.metrics.lastUpdateTime = time.Now()
}

// cacheMaintenanceLoop periodically cleans up expired cache entries
func (o *SnapshotPerformanceOptimizer) cacheMaintenanceLoop(ctx context.Context) {
	ticker := time.NewTicker(o.strategy.CacheTTL / 4)
	defer ticker.Stop()
	
	for {
		select {
		case <-o.stopChan:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			o.cleanCache()
		}
	}
}

// cleanCache removes expired entries and enforces size limits
func (o *SnapshotPerformanceOptimizer) cleanCache() {
	o.cacheMutex.Lock()
	defer o.cacheMutex.Unlock()
	
	now := time.Now()
	expireTime := now.Add(-o.strategy.CacheTTL)
	
	// First pass: remove expired entries
	for id, lastAccess := range o.cacheEntryTimes {
		if lastAccess.Before(expireTime) {
			delete(o.snapshotCache, id)
			delete(o.cacheEntryTimes, id)
			atomic.AddInt64(&o.metrics.CacheEvictionCount, 1)
		}
	}
	
	// Second pass: if still over capacity, remove oldest entries
	if len(o.snapshotCache) > o.strategy.MaxCacheEntries {
		// Find oldest entries
		type cacheEntry struct {
			id        string
			timestamp time.Time
		}
		
		entries := make([]cacheEntry, 0, len(o.cacheEntryTimes))
		for id, ts := range o.cacheEntryTimes {
			entries = append(entries, cacheEntry{id, ts})
		}
		
		// Sort by timestamp (oldest first)
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].timestamp.Before(entries[j].timestamp)
		})
		
		// Remove oldest entries to get back to capacity
		toRemove := len(entries) - o.strategy.MaxCacheEntries
		for i := 0; i < toRemove; i++ {
			id := entries[i].id
			delete(o.snapshotCache, id)
			delete(o.cacheEntryTimes, id)
			atomic.AddInt64(&o.metrics.CacheEvictionCount, 1)
		}
	}
}

// addToCache adds a snapshot to the cache
func (o *SnapshotPerformanceOptimizer) addToCache(snapshot *RegionalSnapshot) {
	if snapshot == nil || o.strategy.MaxCacheEntries <= 0 {
		return
	}
	
	id := string(snapshot.SnapshotID)
	
	o.cacheMutex.Lock()
	defer o.cacheMutex.Unlock()
	
	// Check if we need to make room
	if len(o.snapshotCache) >= o.strategy.MaxCacheEntries {
		// Let the maintenance loop handle it
		go o.cleanCache()
	}
	
	o.snapshotCache[id] = snapshot
	o.cacheEntryTimes[id] = time.Now()
}

// prewarmCache loads recent snapshots into the cache
func (o *SnapshotPerformanceOptimizer) prewarmCache(ctx context.Context) {
	// In a real implementation, this would query for the most recent
	// or most frequently accessed snapshots and load them into cache
	
	// For simplicity in this example, we'll just simulate the process
	// Actually implementing this would require storage API access to
	// retrieve recent snapshot IDs
	
	// Simulate loading recent snapshots
	time.Sleep(100 * time.Millisecond)
}

// applySnapshotOptimizations applies configured optimizations to a snapshot
func (o *SnapshotPerformanceOptimizer) applySnapshotOptimizations(snapshot *RegionalSnapshot) error {
	// In a real implementation, this would apply various optimizations directly
	// to the snapshot data, such as compression, deduplication, etc.
	
	// For this example, we'll just track that it happened
	return nil
}

// updateSnapshotMetrics updates performance metrics after snapshot creation
func (o *SnapshotPerformanceOptimizer) updateSnapshotMetrics(snapshot *RegionalSnapshot, duration time.Duration) {
	if snapshot == nil {
		return
	}
	
	o.metrics.mutex.Lock()
	defer o.metrics.mutex.Unlock()
	
	// Update snapshot count
	o.metrics.SnapshotCreationCount++
	
	// Update latency metrics
	latencyMs := float64(duration.Milliseconds())
	o.metrics.SnapshotCreationLatencyAvgMs = calculatePerformanceAverage(
		o.metrics.SnapshotCreationLatencyAvgMs,
		latencyMs,
		float64(o.metrics.SnapshotCreationCount),
	)
	
	// Update size metrics
	size := snapshot.SnapshotSummary.TotalStateSize
	o.metrics.SnapshotSizeAvgBytes = calculateRunningAverageInt64(
		o.metrics.SnapshotSizeAvgBytes,
		size,
		o.metrics.SnapshotCreationCount,
	)
	
	if size > o.metrics.SnapshotSizeMaxBytes {
		o.metrics.SnapshotSizeMaxBytes = size
	}
	
	if o.metrics.SnapshotSizeMinBytes == 0 || size < o.metrics.SnapshotSizeMinBytes {
		o.metrics.SnapshotSizeMinBytes = size
	}
}

// calculatePerformanceAverage computes a running average for performance metrics
func calculatePerformanceAverage(current, new float64, count float64) float64 {
	if count <= 1 {
		return new
	}
	return current + (new-current)/count
}

// calculateRunningAverageInt64 computes a running average for int64 values
func calculateRunningAverageInt64(current, new int64, count int64) int64 {
	if count <= 1 {
		return new
	}
	return current + (new-current)/count
}
