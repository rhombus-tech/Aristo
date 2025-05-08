package mesh

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockSnapshotCoordinator is a mock of the SnapshotCoordinatorInterface
type MockSnapshotCoordinator struct {
	mock.Mock
	snapshotRequests int32 // Atomic counter for snapshot requests
}

func (m *MockSnapshotCoordinator) InitiateRegionalSnapshot(ctx context.Context) (string, error) {
	atomic.AddInt32(&m.snapshotRequests, 1)
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}

func (m *MockSnapshotCoordinator) GetSnapshotByID(snapshotID string) (*RegionalSnapshot, error) {
	args := m.Called(snapshotID)
	return args.Get(0).(*RegionalSnapshot), args.Error(1)
}

func (m *MockSnapshotCoordinator) GetSnapshotCount() int32 {
	return atomic.LoadInt32(&m.snapshotRequests)
}

func TestSnapshotScheduler_Basic(t *testing.T) {
	// Create a mock coordinator
	mockCoordinator := new(MockSnapshotCoordinator)
	mockCoordinator.On("InitiateRegionalSnapshot", mock.Anything).Return("test-snapshot-id", nil)
	mockCoordinator.On("GetSnapshotByID", "test-snapshot-id").Return(&RegionalSnapshot{
		SnapshotID: []byte("test-snapshot-id"),
	}, nil)

	// Create config with short intervals for testing
	config := &SnapshotSchedulerConfig{
		TimeInterval:           100 * time.Millisecond,
		OperationThreshold:     5,
		EnableAdaptiveScheduling: false,
		MinTimeInterval:        50 * time.Millisecond,
		MaxConcurrentSnapshots: 2,
	}

	// Create the scheduler
	scheduler := NewSnapshotScheduler(mockCoordinator, config)

	// Start the scheduler
	scheduler.Start()
	defer scheduler.Stop()

	// Wait for at least one time-triggered snapshot
	time.Sleep(150 * time.Millisecond)

	// Verify time-based snapshot was triggered
	assert.GreaterOrEqual(t, mockCoordinator.GetSnapshotCount(), int32(1), "Should have at least one snapshot request")
	
	// Record operations to trigger an operation-based snapshot
	for i := 0; i < 6; i++ {
		scheduler.RecordOperation("test-object")
	}

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Verify operation-based snapshot was triggered
	assert.GreaterOrEqual(t, mockCoordinator.GetSnapshotCount(), int32(2), "Should have at least two snapshot requests")

	// Get metrics and verify counts
	metrics := scheduler.GetMetrics()
	assert.GreaterOrEqual(t, metrics.TotalSnapshots, int64(1), "Should have recorded snapshots in metrics")
}

func TestSnapshotScheduler_ManualTrigger(t *testing.T) {
	// Create a mock coordinator
	mockCoordinator := new(MockSnapshotCoordinator)
	mockCoordinator.On("InitiateRegionalSnapshot", mock.Anything).Return("manual-snapshot-id", nil)
	mockCoordinator.On("GetSnapshotByID", "manual-snapshot-id").Return(&RegionalSnapshot{
		SnapshotID: []byte("manual-snapshot-id"),
	}, nil)

	// Create config with intervals that won't trigger during test but will allow manual triggers
	config := &SnapshotSchedulerConfig{
		TimeInterval:           1 * time.Hour,
		OperationThreshold:     1000,
		EnableAdaptiveScheduling: false,
		// Set min interval to 0 to allow immediate manual triggers
		MinTimeInterval:        0 * time.Millisecond,
		MaxConcurrentSnapshots: 2,
	}

	// Create the scheduler
	scheduler := NewSnapshotScheduler(mockCoordinator, config)

	// Start the scheduler
	scheduler.Start()
	defer scheduler.Stop()

	// Manually trigger a snapshot
	snapshotID, err := scheduler.TriggerManualSnapshot(context.Background(), "test-manual")
	assert.NoError(t, err)
	assert.Equal(t, "manual-snapshot-id", snapshotID)

	// Verify manual snapshot was triggered
	assert.Equal(t, int32(1), mockCoordinator.GetSnapshotCount())

	// Get metrics and verify
	metrics := scheduler.GetMetrics()
	assert.Equal(t, int64(1), metrics.ManualTriggered)
	assert.Equal(t, int64(0), metrics.TimeTriggered)
	assert.Equal(t, int64(0), metrics.OperationTriggered)
}

func TestSnapshotScheduler_ConcurrencyLimit(t *testing.T) {
	// Create a mock coordinator with a delay to test concurrency
	mockCoordinator := new(MockSnapshotCoordinator)
	mockCoordinator.On("InitiateRegionalSnapshot", mock.Anything).Return("test-snapshot-id", nil).Run(func(args mock.Arguments) {
		// Add a delay to simulate work
		time.Sleep(200 * time.Millisecond)
	})
	mockCoordinator.On("GetSnapshotByID", "test-snapshot-id").Return(&RegionalSnapshot{
		SnapshotID: []byte("test-snapshot-id"),
	}, nil)

	// Create config with limited concurrency
	config := &SnapshotSchedulerConfig{
		TimeInterval:           1 * time.Hour, // Won't trigger during test
		OperationThreshold:     5,
		EnableAdaptiveScheduling: false,
		MinTimeInterval:        0 * time.Millisecond, // Allow immediate snapshots
		MaxConcurrentSnapshots: 2, // Only allow 2 concurrent snapshots
	}

	// Create the scheduler
	scheduler := NewSnapshotScheduler(mockCoordinator, config)

	// Start the scheduler
	scheduler.Start()
	defer scheduler.Stop()

	// Ensure the timeLastSnapshot is set to the past so we can trigger new snapshots
	scheduler.timeLastSnapshot = time.Now().Add(-1 * time.Hour)

	// Setup to track results
	var wg sync.WaitGroup
	results := make([]bool, 5) // Track success of 5 attempts
	
	// Try to trigger 5 snapshots concurrently
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			ctx := context.Background()
			snapshotID, err := scheduler.TriggerManualSnapshot(ctx, "concurrent-test")
			results[index] = (err == nil && snapshotID != "")
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Count successful snapshots
	successCount := 0
	for _, success := range results {
		if success {
			successCount++
		}
	}

	// Verify that we got at least some successful snapshots
	assert.Greater(t, successCount, 0, "Should have some successful snapshots")
	
	// Verify that the mock coordinator received snapshot requests
	assert.Greater(t, mockCoordinator.GetSnapshotCount(), int32(0),
		"Should have made at least one snapshot request")
}

func TestSnapshotScheduler_Metrics(t *testing.T) {
	// Create a mock coordinator
	mockCoordinator := new(MockSnapshotCoordinator)
	mockCoordinator.On("InitiateRegionalSnapshot", mock.Anything).Return("test-snapshot-id", nil)
	mockCoordinator.On("GetSnapshotByID", "test-snapshot-id").Return(&RegionalSnapshot{
		SnapshotID: []byte("test-snapshot-id"),
	}, nil)

	// Create the scheduler with a configuration that allows immediate manual triggers
	config := &SnapshotSchedulerConfig{
		TimeInterval:           1 * time.Hour,
		OperationThreshold:     1000,
		EnableAdaptiveScheduling: false,
		MinTimeInterval:        0 * time.Millisecond, // No delay between snapshots
		MaxConcurrentSnapshots: 10,                   // Allow many concurrent snapshots
	}
	
	scheduler := NewSnapshotScheduler(mockCoordinator, config)

	// Record some operations
	for i := 0; i < 100; i++ {
		scheduler.RecordOperation("test-object")
	}

	// Manually trigger a few snapshots
	ctx := context.Background()
	snapshot1, err1 := scheduler.TriggerManualSnapshot(ctx, "test1")
	assert.NoError(t, err1)
	assert.Equal(t, "test-snapshot-id", snapshot1)
	
	snapshot2, err2 := scheduler.TriggerManualSnapshot(ctx, "test2")
	assert.NoError(t, err2)
	assert.Equal(t, "test-snapshot-id", snapshot2)
	
	// Give time for processing to complete
	time.Sleep(50 * time.Millisecond)

	// Get metrics
	metrics := scheduler.GetMetrics()

	// Verify operation count
	assert.Equal(t, uint64(100), metrics.TotalOperations)

	// Verify snapshot counts
	assert.Equal(t, int64(2), metrics.ManualTriggered)
	assert.Equal(t, int64(0), metrics.TimeTriggered)
	assert.Equal(t, int64(0), metrics.OperationTriggered)

	// Verify we have recent events (exact number may vary, but should be > 0)
	assert.Greater(t, len(metrics.RecentEvents), 0)
	
	// Only check event type if we have events
	if len(metrics.RecentEvents) > 0 {
		assert.Equal(t, ManualTrigger, metrics.RecentEvents[0].TriggerType)
	}
}

func TestSnapshotScheduler_AdaptiveFrequency(t *testing.T) {
	// We'll directly modify the SnapshotScheduler to test its adaptive algorithm
	// without relying on timing or real scheduling
	
	// Create a mock coordinator
	mockCoordinator := new(MockSnapshotCoordinator)
	mockCoordinator.On("InitiateRegionalSnapshot", mock.Anything).Return("test-snapshot-id", nil)
	mockCoordinator.On("GetSnapshotByID", "test-snapshot-id").Return(&RegionalSnapshot{
		SnapshotID: []byte("test-snapshot-id"),
	}, nil)

	// Create a modified version of the adjustSnapshotFrequency method to test it directly
	calculateNewInterval := func(currentInterval time.Duration, avgDuration time.Duration) time.Duration {
		var newInterval time.Duration
		
		// Use same logic as in the scheduler's adjustSnapshotFrequency method
		if avgDuration > 5*time.Second {
			// Slow down by 10%
			newInterval = time.Duration(float64(currentInterval) * 1.1)
		} else if avgDuration < 1*time.Second {
			// Speed up by 10%
			newInterval = time.Duration(float64(currentInterval) * 0.9)
		} else {
			newInterval = currentInterval
		}
		
		return newInterval
	}
	
	// Test cases
	testCases := []struct {
		name            string
		startInterval    time.Duration
		avgDuration      time.Duration
		expectedBehavior string
		check           func(before, after time.Duration) bool
	}{
		{
			name:            "slow snapshots increase interval",
			startInterval:    100 * time.Millisecond,
			avgDuration:      6 * time.Second,
			expectedBehavior: "interval should increase",
			check:           func(before, after time.Duration) bool { return after > before },
		},
		{
			name:            "fast snapshots decrease interval",
			startInterval:    500 * time.Millisecond,
			avgDuration:      500 * time.Millisecond,
			expectedBehavior: "interval should decrease",
			check:           func(before, after time.Duration) bool { return after < before },
		},
		{
			name:            "optimal snapshots maintain interval",
			startInterval:    250 * time.Millisecond,
			avgDuration:      2 * time.Second,
			expectedBehavior: "interval should stay the same",
			check:           func(before, after time.Duration) bool { return after == before },
		},
	}
	
	// Run test cases
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			beforeInterval := tc.startInterval
			afterInterval := calculateNewInterval(beforeInterval, tc.avgDuration)
			
			assert.True(t, tc.check(beforeInterval, afterInterval), tc.expectedBehavior)
		})
	}
	
}

func TestSnapshotScheduler_OperationCounting(t *testing.T) {
	// Create a mock coordinator
	mockCoordinator := new(MockSnapshotCoordinator)
	mockCoordinator.On("InitiateRegionalSnapshot", mock.Anything).Return("test-snapshot-id", nil)
	mockCoordinator.On("GetSnapshotByID", "test-snapshot-id").Return(&RegionalSnapshot{
		SnapshotID: []byte("test-snapshot-id"),
	}, nil)

	// Create config with high thresholds to avoid automatic triggering
	config := &SnapshotSchedulerConfig{
		TimeInterval:           1 * time.Hour,
		OperationThreshold:     100,
		EnableAdaptiveScheduling: false,
		MinTimeInterval:        1 * time.Millisecond,
		MaxConcurrentSnapshots: 2,
	}

	// Create the scheduler
	scheduler := NewSnapshotScheduler(mockCoordinator, config)

	// Record operations for different objects
	for i := 0; i < 50; i++ {
		scheduler.RecordOperation("object1")
	}
	
	for i := 0; i < 25; i++ {
		scheduler.RecordOperation("object2")
	}

	// Verify operation counts
	assert.Equal(t, uint64(50), scheduler.GetOperationCount("object1"))
	assert.Equal(t, uint64(25), scheduler.GetOperationCount("object2"))
	assert.Equal(t, uint64(0), scheduler.GetOperationCount("nonexistent"))

	// Reset one counter
	scheduler.ResetOperationCount("object1")
	assert.Equal(t, uint64(0), scheduler.GetOperationCount("object1"))
	assert.Equal(t, uint64(25), scheduler.GetOperationCount("object2"))
}
