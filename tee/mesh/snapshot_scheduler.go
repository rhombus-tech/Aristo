// Package mesh provides a mesh network for TEE-to-TEE communication
package mesh

import (
	"context"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// OperationCounter interface allows different parts of the system to report operations
type OperationCounter interface {
	// RecordOperation records that an operation occurred for a specific object
	RecordOperation(objectID string)
	
	// GetOperationCount returns the current operation count for an object
	GetOperationCount(objectID string) uint64
	
	// ResetOperationCount resets the counter for a specific object
	ResetOperationCount(objectID string)
}

// SnapshotTriggerType defines the type of trigger for snapshot creation
type SnapshotTriggerType string

const (
	// TimeTrigger indicates a snapshot was triggered by time
	TimeTrigger SnapshotTriggerType = "time"
	
	// OperationCountTrigger indicates a snapshot was triggered by operation count
	OperationCountTrigger SnapshotTriggerType = "operation_count"
	
	// ManualTrigger indicates a snapshot was triggered manually
	ManualTrigger SnapshotTriggerType = "manual"
	
	// AdaptiveTrigger indicates a snapshot was triggered by the adaptive algorithm
	AdaptiveTrigger SnapshotTriggerType = "adaptive"
)

// SnapshotSchedulerConfig defines configuration for the snapshot scheduler
type SnapshotSchedulerConfig struct {
	// Time-based schedule
	TimeInterval time.Duration // How often to create snapshots based on time
	
	// Operation-based schedule
	OperationThreshold uint64 // How many operations before creating a snapshot
	
	// Adaptive scheduling
	EnableAdaptiveScheduling bool  // Whether to use adaptive scheduling
	MinTimeInterval         time.Duration // Minimum time between snapshots
	MaxTimeInterval         time.Duration // Maximum time between snapshots
	
	// Performance optimization
	MaxConcurrentSnapshots int // Maximum number of concurrent snapshot operations
	
	// Performance impact limits
	MaxCPUPercent     float64 // Maximum CPU utilization percentage for background tasks
	MaxMemoryPercent  float64 // Maximum memory utilization percentage
	SnapshotImpactMs  int64   // Maximum milliseconds of impact per operation
	
	// Regional settings
	RegionID string // Region identifier
}

// DefaultSnapshotSchedulerConfig returns sensible default settings
func DefaultSnapshotSchedulerConfig() *SnapshotSchedulerConfig {
	return &SnapshotSchedulerConfig{
		TimeInterval:           15 * time.Minute,
		OperationThreshold:     10000,
		EnableAdaptiveScheduling: true,
		MinTimeInterval:        1 * time.Minute,
		MaxTimeInterval:        1 * time.Hour,
		MaxConcurrentSnapshots: 2,
		MaxCPUPercent:          70.0,
		MaxMemoryPercent:       80.0,
		SnapshotImpactMs:       10, // Max 10ms impact on mesh operations
	}
}

// SnapshotCoordinatorInterface defines the operations required by the SnapshotScheduler
type SnapshotCoordinatorInterface interface {
	// InitiateRegionalSnapshot triggers a regional snapshot creation
	InitiateRegionalSnapshot(ctx context.Context) (string, error)
	
	// GetSnapshotByID retrieves a snapshot by its ID
	GetSnapshotByID(snapshotID string) (*RegionalSnapshot, error)
}

// SnapshotScheduler manages periodic snapshots based on time and operation count
type SnapshotScheduler struct {
	config      *SnapshotSchedulerConfig
	coordinator SnapshotCoordinatorInterface
	
	// Operation counting
	operationCounts     map[string]uint64 // objectID -> operation count
	operationsMutex     sync.RWMutex
	
	// Scheduling state
	timeLastSnapshot    time.Time
	adaptiveInterval    time.Duration
	schedulerRunning    bool
	processingSnapshot  int32 // Atomic counter for concurrent snapshot operations
	
	// Trigger reporting
	lastTriggerType     SnapshotTriggerType
	lastTriggerTime     time.Time
	
	// Performance tracking
	snapshotDurations   []time.Duration // Recent snapshot durations
	snapshotDurationsMu sync.RWMutex
	maxDurations        int // Maximum number of durations to track
	
	// Metrics collection
	metrics             *SnapshotMetrics
	
	// Lifecycle management
	ctx                 context.Context
	cancel              context.CancelFunc
	wg                  sync.WaitGroup
}

// SnapshotTriggerEvent contains information about a snapshot trigger
type SnapshotTriggerEvent struct {
	TriggerType      SnapshotTriggerType
	TriggerTime      time.Time
	ObjectID         string // If triggered by specific object
	OperationCount   uint64 // If triggered by operation count
	Duration         time.Duration // How long the snapshot took
	SnapshotID       []byte // ID of the created snapshot
}

// SnapshotMetrics collects metrics about snapshot operations
type SnapshotMetrics struct {
	TotalSnapshots           int64
	SuccessfulSnapshots      int64
	FailedSnapshots          int64
	AverageSnapshotTimeMs    int64
	MaxSnapshotTimeMs        int64
	MinSnapshotTimeMs        int64
	OperationTriggered       int64
	TimeTriggered            int64
	AdaptiveTriggered        int64
	ManualTriggered          int64
	TotalOperations          uint64
	LastSnapshotStatus       string
	LastSnapshotTime         time.Time
	TimeSinceLastSnapshotSec int64
	
	// Recent trigger events for detailed analysis
	RecentEvents             []SnapshotTriggerEvent
	recentEventsMu           sync.RWMutex
	maxEvents                int
}

// NewSnapshotMetrics creates a new metrics collector
func NewSnapshotMetrics() *SnapshotMetrics {
	return &SnapshotMetrics{
		RecentEvents: make([]SnapshotTriggerEvent, 0, 10),
		maxEvents:    10,
		MinSnapshotTimeMs: math.MaxInt64, // Initialize to high value
	}
}

// RecordTriggerEvent records a snapshot trigger event
func (m *SnapshotMetrics) RecordTriggerEvent(event SnapshotTriggerEvent) {
	m.recentEventsMu.Lock()
	defer m.recentEventsMu.Unlock()
	
	// Add to recent events, keeping only the most recent ones
	m.RecentEvents = append(m.RecentEvents, event)
	if len(m.RecentEvents) > m.maxEvents {
		m.RecentEvents = m.RecentEvents[1:]
	}
	
	// Update metrics
	timeMs := event.Duration.Milliseconds()
	if timeMs > m.MaxSnapshotTimeMs {
		m.MaxSnapshotTimeMs = timeMs
	}
	if timeMs < m.MinSnapshotTimeMs {
		m.MinSnapshotTimeMs = timeMs
	}
	
	// Update counts
	m.TotalSnapshots++
	
	switch event.TriggerType {
	case TimeTrigger:
		m.TimeTriggered++
	case OperationCountTrigger:
		m.OperationTriggered++
	case AdaptiveTrigger:
		m.AdaptiveTriggered++
	case ManualTrigger:
		m.ManualTriggered++
	}
	
	// Update average time (weighted average)
	if m.TotalSnapshots == 1 {
		m.AverageSnapshotTimeMs = timeMs
	} else {
		m.AverageSnapshotTimeMs = (m.AverageSnapshotTimeMs*(m.TotalSnapshots-1) + timeMs) / m.TotalSnapshots
	}
	
	// Update last snapshot time
	m.LastSnapshotTime = event.TriggerTime
	m.TimeSinceLastSnapshotSec = int64(time.Since(event.TriggerTime).Seconds())
	m.LastSnapshotStatus = "success"
}

// RecordFailure records a snapshot failure
func (m *SnapshotMetrics) RecordFailure() {
	m.FailedSnapshots++
	m.LastSnapshotStatus = "failed"
}

// GetMetricsSnapshot returns a copy of the current metrics
func (m *SnapshotMetrics) GetMetricsSnapshot() *SnapshotMetrics {
	m.recentEventsMu.RLock()
	defer m.recentEventsMu.RUnlock()
	
	// Create a deep copy of metrics
	copy := *m
	copy.RecentEvents = make([]SnapshotTriggerEvent, len(m.RecentEvents))
	copy.TimeSinceLastSnapshotSec = int64(time.Since(m.LastSnapshotTime).Seconds())
	
	for i, event := range m.RecentEvents {
		copy.RecentEvents[i] = event
	}
	
	return &copy
}

// NewSnapshotScheduler creates a new snapshot scheduler
func NewSnapshotScheduler(
	coordinator SnapshotCoordinatorInterface,
	config *SnapshotSchedulerConfig,
) *SnapshotScheduler {
	if config == nil {
		config = DefaultSnapshotSchedulerConfig()
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	return &SnapshotScheduler{
		config:           config,
		coordinator:      coordinator,
		operationCounts:  make(map[string]uint64),
		timeLastSnapshot: time.Now(),
		adaptiveInterval: config.TimeInterval,
		metrics:          NewSnapshotMetrics(),
		maxDurations:     10,
		snapshotDurations: make([]time.Duration, 0, 10),
		ctx:              ctx,
		cancel:           cancel,
	}
}

// Start begins the snapshot scheduler
func (s *SnapshotScheduler) Start() {
	if s.schedulerRunning {
		return
	}
	
	s.schedulerRunning = true
	
	// Start the time-based scheduler
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.timeBasedScheduler()
	}()
	
	// Start the adaptive scheduler if enabled
	if s.config.EnableAdaptiveScheduling {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.adaptiveScheduler()
		}()
	}
}

// Stop stops the snapshot scheduler
func (s *SnapshotScheduler) Stop() {
	if !s.schedulerRunning {
		return
	}
	
	s.cancel()
	s.wg.Wait()
	s.schedulerRunning = false
}

// timeBasedScheduler periodically triggers snapshots based on time
func (s *SnapshotScheduler) timeBasedScheduler() {
	ticker := time.NewTicker(s.config.TimeInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if s.shouldCreateSnapshot() {
				s.triggerSnapshot(TimeTrigger, "time-based", 0)
			}
		}
	}
}

// adaptiveScheduler adjusts snapshot frequency based on system load
func (s *SnapshotScheduler) adaptiveScheduler() {
	// Check system load every minute
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.adjustSnapshotFrequency()
		}
	}
}

// adjustSnapshotFrequency changes snapshot interval based on system load
func (s *SnapshotScheduler) adjustSnapshotFrequency() {
	// In a real implementation, we would:
	// 1. Check system load metrics (CPU, memory, operation rate)
	// 2. Adjust the interval based on a formula
	
	// For now, we'll use a simple formula based on average snapshot duration
	s.snapshotDurationsMu.RLock()
	avgDuration := s.calculateAverageSnapshotDuration()
	s.snapshotDurationsMu.RUnlock()
	
	// If snapshots are taking longer, increase the interval
	if avgDuration > 5*time.Second {
		// Slow down by 10%
		newInterval := time.Duration(float64(s.adaptiveInterval) * 1.1)
		if newInterval > s.config.MaxTimeInterval {
			newInterval = s.config.MaxTimeInterval
		}
		s.adaptiveInterval = newInterval
	} else if avgDuration < 1*time.Second {
		// Speed up by 10%
		newInterval := time.Duration(float64(s.adaptiveInterval) * 0.9)
		if newInterval < s.config.MinTimeInterval {
			newInterval = s.config.MinTimeInterval
		}
		s.adaptiveInterval = newInterval
	}
}

// calculateAverageSnapshotDuration computes the average snapshot duration
// Caller must hold the snapshotDurationsMu lock
func (s *SnapshotScheduler) calculateAverageSnapshotDuration() time.Duration {
	if len(s.snapshotDurations) == 0 {
		return 0
	}
	
	var total time.Duration
	for _, d := range s.snapshotDurations {
		total += d
	}
	
	return total / time.Duration(len(s.snapshotDurations))
}

// RecordOperation implements OperationCounter interface
func (s *SnapshotScheduler) RecordOperation(objectID string) {
	// Record the operation
	s.operationsMutex.Lock()
	s.operationCounts[objectID]++
	currentCount := s.operationCounts[objectID]
	s.operationsMutex.Unlock()
	
	// Update global metrics
	atomic.AddUint64(&s.metrics.TotalOperations, 1)
	
	// Check if we've reached the threshold
	if currentCount >= s.config.OperationThreshold && s.shouldCreateSnapshot() {
		s.triggerSnapshot(OperationCountTrigger, objectID, currentCount)
	}
}

// GetOperationCount implements OperationCounter interface
func (s *SnapshotScheduler) GetOperationCount(objectID string) uint64 {
	s.operationsMutex.RLock()
	defer s.operationsMutex.RUnlock()
	
	return s.operationCounts[objectID]
}

// ResetOperationCount implements OperationCounter interface
func (s *SnapshotScheduler) ResetOperationCount(objectID string) {
	s.operationsMutex.Lock()
	defer s.operationsMutex.Unlock()
	
	s.operationCounts[objectID] = 0
}

// shouldCreateSnapshot checks if we should create a snapshot now
func (s *SnapshotScheduler) shouldCreateSnapshot() bool {
	// Don't create a snapshot if too many are already in progress
	if atomic.LoadInt32(&s.processingSnapshot) >= int32(s.config.MaxConcurrentSnapshots) {
		return false
	}
	
	// Don't create a snapshot if it's been too recent
	minTimeSinceLastSnapshot := s.config.MinTimeInterval
	if time.Since(s.timeLastSnapshot) < minTimeSinceLastSnapshot {
		return false
	}
	
	return true
}

// triggerSnapshot initiates a snapshot creation
func (s *SnapshotScheduler) triggerSnapshot(
	triggerType SnapshotTriggerType,
	objectID string,
	operationCount uint64,
) {
	// Increment the processing counter
	atomic.AddInt32(&s.processingSnapshot, 1)
	defer atomic.AddInt32(&s.processingSnapshot, -1)
	
	// Record the trigger type
	s.lastTriggerType = triggerType
	s.lastTriggerTime = time.Now()
	
	// Create a context with timeout
	ctx, cancel := context.WithTimeout(s.ctx, 2*time.Minute)
	defer cancel()
	
	// Measure the snapshot duration
	startTime := time.Now()
	
	// Initiate the snapshot
	snapshotID, err := s.coordinator.InitiateRegionalSnapshot(ctx)
	
	// Record the duration
	duration := time.Since(startTime)
	
	// Store the duration for adaptive scheduling
	s.snapshotDurationsMu.Lock()
	s.snapshotDurations = append(s.snapshotDurations, duration)
	if len(s.snapshotDurations) > s.maxDurations {
		s.snapshotDurations = s.snapshotDurations[1:]
	}
	s.snapshotDurationsMu.Unlock()
	
	// Reset operation counter if successful
	if err == nil && triggerType == OperationCountTrigger {
		s.ResetOperationCount(objectID)
	}
	
	// Record metrics
	event := SnapshotTriggerEvent{
		TriggerType:    triggerType,
		TriggerTime:    startTime,
		ObjectID:       objectID,
		OperationCount: operationCount,
		Duration:       duration,
	}
	
	if err == nil {
		// Only set snapshot ID if successful
		if snapshotID != "" {
			snapshot, _ := s.coordinator.GetSnapshotByID(snapshotID)
			if snapshot != nil {
				event.SnapshotID = snapshot.SnapshotID
			}
		}
		s.metrics.RecordTriggerEvent(event)
	} else {
		s.metrics.RecordFailure()
	}
	
	// Update the last snapshot time
	if err == nil {
		s.timeLastSnapshot = time.Now()
	}
}

// TriggerManualSnapshot allows manual triggering of snapshots
func (s *SnapshotScheduler) TriggerManualSnapshot(ctx context.Context, reason string) (string, error) {
	if !s.shouldCreateSnapshot() {
		return "", fmt.Errorf("cannot create snapshot at this time: too many in progress or too recent")
	}
	
	// Increment the processing counter
	atomic.AddInt32(&s.processingSnapshot, 1)
	defer atomic.AddInt32(&s.processingSnapshot, -1)
	
	// Record the trigger type
	s.lastTriggerType = ManualTrigger
	s.lastTriggerTime = time.Now()
	
	// Measure the snapshot duration
	startTime := time.Now()
	
	// Initiate the snapshot
	snapshotID, err := s.coordinator.InitiateRegionalSnapshot(ctx)
	
	// Record the duration
	duration := time.Since(startTime)
	
	// Store the duration for adaptive scheduling
	s.snapshotDurationsMu.Lock()
	s.snapshotDurations = append(s.snapshotDurations, duration)
	if len(s.snapshotDurations) > s.maxDurations {
		s.snapshotDurations = s.snapshotDurations[1:]
	}
	s.snapshotDurationsMu.Unlock()
	
	// Record metrics
	event := SnapshotTriggerEvent{
		TriggerType:    ManualTrigger,
		TriggerTime:    startTime,
		ObjectID:       reason,
		OperationCount: 0,
		Duration:       duration,
	}
	
	if err == nil {
		// Only set snapshot ID if successful
		if snapshotID != "" {
			snapshot, _ := s.coordinator.GetSnapshotByID(snapshotID)
			if snapshot != nil {
				event.SnapshotID = snapshot.SnapshotID
			}
		}
		s.metrics.RecordTriggerEvent(event)
		s.timeLastSnapshot = time.Now()
	} else {
		s.metrics.RecordFailure()
	}
	
	return snapshotID, err
}

// GetMetrics returns snapshot metrics
func (s *SnapshotScheduler) GetMetrics() *SnapshotMetrics {
	return s.metrics.GetMetricsSnapshot()
}

// GetCurrentInterval returns the current snapshot interval
func (s *SnapshotScheduler) GetCurrentInterval() time.Duration {
	if s.config.EnableAdaptiveScheduling {
		return s.adaptiveInterval
	}
	return s.config.TimeInterval
}

// GetSnapshotSchedulerStatus returns the current status of the scheduler
func (s *SnapshotScheduler) GetSnapshotSchedulerStatus() map[string]interface{} {
	status := map[string]interface{}{
		"running":               s.schedulerRunning,
		"concurrent_snapshots":  atomic.LoadInt32(&s.processingSnapshot),
		"max_concurrent":        s.config.MaxConcurrentSnapshots,
		"time_interval_seconds": s.GetCurrentInterval().Seconds(),
		"last_snapshot_time":    s.timeLastSnapshot,
		"time_since_last":       time.Since(s.timeLastSnapshot).String(),
		"last_trigger_type":     string(s.lastTriggerType),
		"last_trigger_time":     s.lastTriggerTime,
		"metrics":               s.GetMetrics(),
		"adaptive_enabled":      s.config.EnableAdaptiveScheduling,
	}
	
	return status
}
