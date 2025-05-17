package mesh

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFederationCoordinator tests the basic functionality of the federation coordinator
func TestFederationCoordinator(t *testing.T) {
	// Skip for quick tests
	if testing.Short() {
		t.Skip("Skipping federation tests in short mode")
	}
	
	// Create dependencies for testing
	stateManager := NewDefaultStateManager()
	snapshotStorage := NewMockSnapshotStorage()
	stateManagerForSnapshot := &MockStateManager{}
	
	// Create snapshot coordinator
	regionID := "test-region-1"
	snapshotPolicy := DefaultRegionalSnapshotPolicy()
	snapshotCoordinator := NewRegionalSnapshotCoordinator(
		regionID,
		snapshotPolicy,
		snapshotStorage,
		stateManagerForSnapshot,
	)
	
	// Create federation coordinator
	federationID := "test-federation"
	federationPolicy := DefaultFederationPolicy()
	coordinator := NewFederationCoordinator(
		federationID,
		regionID,
		federationPolicy,
		snapshotCoordinator,
		stateManager,
	)
	
	// Verify coordinator was created correctly
	assert.Equal(t, federationID, coordinator.federationID)
	assert.Equal(t, regionID, coordinator.localRegionID)
	
	// Register additional regions
	region2 := coordinator.AddRegion("test-region-2", "localhost:5002", false)
	region3 := coordinator.AddRegion("test-region-3", "localhost:5003", true)
	
	// Verify regions were added
	assert.Equal(t, "localhost:5002", region2.Endpoint)
	assert.True(t, region3.AdminCapabilities)
	
	// Get all regions
	regions := coordinator.GetRegions()
	assert.Equal(t, 3, len(regions))
	assert.Contains(t, regions, "test-region-1")
	assert.Contains(t, regions, "test-region-2")
	assert.Contains(t, regions, "test-region-3")
	
	// Connect to regions (simulated)
	ctx := context.Background()
	err := coordinator.ConnectToRegion(ctx, "test-region-2")
	assert.NoError(t, err)
	
	// Verify metrics
	metrics := coordinator.GetFederationMetrics()
	assert.Equal(t, "active", metrics.RegionHealthStatus["test-region-2"])
}

// TestStateConflictResolution tests conflict resolution between regions
func TestStateConflictResolution(t *testing.T) {
	// Skip for quick tests
	if testing.Short() {
		t.Skip("Skipping federation tests in short mode")
	}
	
	// Create dependencies for testing
	stateManager := NewDefaultStateManager()
	snapshotStorage := NewMockSnapshotStorage()
	stateManagerForSnapshot := &MockStateManager{}
	
	// Create snapshot coordinator
	regionID := "region-1"
	snapshotPolicy := DefaultRegionalSnapshotPolicy()
	snapshotCoordinator := NewRegionalSnapshotCoordinator(
		regionID,
		snapshotPolicy,
		snapshotStorage,
		stateManagerForSnapshot,
	)
	
	// Create test states for conflict resolution
	region1State := []byte(`{"value": "region1 data", "timestamp": 1000}`)
	region2State := []byte(`{"value": "region2 data", "timestamp": 2000}`)
	
	// Create federation coordinator with timestamp conflict resolution
	federationID := "test-federation"
	federationPolicy := DefaultFederationPolicy()
	federationPolicy.ConflictResolutionMode = "timestamp"
	coordinator := NewFederationCoordinator(
		federationID,
		regionID,
		federationPolicy,
		snapshotCoordinator,
		stateManager,
	)
	
	// Add another region
	otherRegion := coordinator.AddRegion("region-2", "localhost:5002", false)
	
	// Verify the other region was properly added
	assert.Equal(t, "region-2", otherRegion.RegionID)
	assert.Equal(t, "localhost:5002", otherRegion.Endpoint)
	assert.False(t, otherRegion.AdminCapabilities)
	
	// Simulate different last contact times for regions
	coordinator.regions["region-1"].LastContactTime = time.Now().Add(-1 * time.Hour) // Older
	coordinator.regions["region-2"].LastContactTime = time.Now()                     // Newer
	
	// Create conflict map
	conflictStates := map[string][]byte{
		"region-1": region1State,
		"region-2": region2State,
	}
	
	// Test timestamp-based resolution
	resolvedState, err := coordinator.ResolveStateConflict("test-object", conflictStates)
	assert.NoError(t, err)
	assert.Equal(t, region2State, resolvedState, "Should choose region-2's state based on timestamp")
	
	// Test admin-based resolution
	federationPolicy.ConflictResolutionMode = "authority"
	coordinator.regions["region-1"].AdminCapabilities = true  // Make region-1 an admin
	
	resolvedState, err = coordinator.ResolveStateConflict("test-object", conflictStates)
	assert.NoError(t, err)
	assert.Equal(t, region1State, resolvedState, "Should choose region-1's state as it has admin capabilities")
}

// TestFederatedSnapshot tests creation of federated snapshots
func TestFederatedSnapshot(t *testing.T) {
	// Skip for quick tests
	if testing.Short() {
		t.Skip("Skipping federation tests in short mode")
	}
	
	// Create dependencies for testing
	stateManager := NewDefaultStateManager()
	snapshotStorage := NewMockSnapshotStorage()
	stateManagerForSnapshot := &MockStateManager{}
	
	// Create snapshot coordinator
	regionID := "region-1"
	snapshotPolicy := DefaultRegionalSnapshotPolicy()
	snapshotCoordinator := NewRegionalSnapshotCoordinator(
		regionID,
		snapshotPolicy,
		snapshotStorage,
		stateManagerForSnapshot,
	)
	
	// Create federation coordinator
	federationID := "test-federation"
	federationPolicy := DefaultFederationPolicy()
	coordinator := NewFederationCoordinator(
		federationID,
		regionID,
		federationPolicy,
		snapshotCoordinator,
		stateManager,
	)
	
	// Add a few regions
	coordinator.AddRegion("region-2", "localhost:5002", false)
	coordinator.AddRegion("region-3", "localhost:5003", false)
	
	// Register minimum required TEEs (3) for snapshot creation
	snapshotCoordinator.RegisterTEE("tee-1", "SGX")
	snapshotCoordinator.RegisterTEE("tee-2", "SEV")
	snapshotCoordinator.RegisterTEE("tee-3", "SGX")
	
	// Create a federated snapshot
	ctx := context.Background()
	snapshot, err := coordinator.CreateFederatedSnapshot(ctx, []string{"region-1", "region-2", "region-3"})
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	
	// Verify snapshot properties
	assert.Equal(t, federationID, snapshot.FederationID)
	assert.NotEmpty(t, snapshot.SnapshotID)
	assert.NotNil(t, snapshot.ConsensusMetadata)
	
	// Verify metrics were updated
	metrics := coordinator.GetFederationMetrics()
	assert.Equal(t, int64(1), metrics.TotalFederatedSnapshots)
}

// TestSynchronizeObject tests object synchronization across regions
func TestSynchronizeObject(t *testing.T) {
	// Skip for quick tests
	if testing.Short() {
		t.Skip("Skipping federation tests in short mode")
	}
	
	// Create dependencies for testing
	stateManager := NewDefaultStateManager()
	snapshotStorage := NewMockSnapshotStorage()
	stateManagerForSnapshot := &MockStateManager{}
	
	// Add some test state
	objectID := "test-object"
	testState := []byte(`{"value": "test data", "timestamp": 1000}`)
	err := stateManager.SetState(objectID, testState)
	require.NoError(t, err)
	
	// Create snapshot coordinator
	regionID := "region-1"
	snapshotPolicy := DefaultRegionalSnapshotPolicy()
	snapshotCoordinator := NewRegionalSnapshotCoordinator(
		regionID,
		snapshotPolicy,
		snapshotStorage,
		stateManagerForSnapshot,
	)
	
	// Create federation coordinator
	federationID := "test-federation"
	federationPolicy := DefaultFederationPolicy()
	coordinator := NewFederationCoordinator(
		federationID,
		regionID,
		federationPolicy,
		snapshotCoordinator,
		stateManager,
	)
	
	// Add a couple regions and connect to them (simulated)
	coordinator.AddRegion("region-2", "localhost:5002", false)
	coordinator.AddRegion("region-3", "localhost:5003", false)
	
	ctx := context.Background()
	err = coordinator.ConnectToRegion(ctx, "region-2")
	require.NoError(t, err)
	err = coordinator.ConnectToRegion(ctx, "region-3")
	require.NoError(t, err)
	
	// Synchronize the object
	err = coordinator.SynchronizeObject(ctx, objectID, []string{"region-1", "region-2", "region-3"})
	assert.NoError(t, err)
	
	// Verify regions are marked as synchronized
	assert.Contains(t, coordinator.regions["region-2"].SynchronizedObjects, objectID)
	assert.Contains(t, coordinator.regions["region-3"].SynchronizedObjects, objectID)
	
	// Verify metrics
	metrics := coordinator.GetFederationMetrics()
	assert.Equal(t, int64(1), metrics.CrossRegionOperations)
	assert.Equal(t, int64(1), metrics.SuccessfulCrossRegionOps)
}

// mockCallback implements the CrossRegionCallbackHandler for testing
type mockCallback struct {
	onStateChangeCalled  bool
	onConsensusReqCalled bool
	onFedSnapshotCalled  bool
	shouldAgree          bool
}

func (m *mockCallback) OnStateChange(objectID string, sourceRegion string, newState []byte) error {
	m.onStateChangeCalled = true
	return nil
}

func (m *mockCallback) OnConsensusRequest(objectID string, proposedValue []byte) (bool, error) {
	m.onConsensusReqCalled = true
	return m.shouldAgree, nil
}

func (m *mockCallback) OnFederatedSnapshot(snapshot *FederatedSnapshot) error {
	m.onFedSnapshotCalled = true
	return nil
}

// TestCallbackHandlers tests registration and usage of callback handlers
func TestCallbackHandlers(t *testing.T) {
	// Skip for quick tests
	if testing.Short() {
		t.Skip("Skipping federation tests in short mode")
	}
	
	// Create dependencies for testing
	stateManager := NewDefaultStateManager()
	snapshotStorage := NewMockSnapshotStorage()
	stateManagerForSnapshot := &MockStateManager{}
	
	// Create snapshot coordinator
	regionID := "region-1"
	snapshotPolicy := DefaultRegionalSnapshotPolicy()
	snapshotCoordinator := NewRegionalSnapshotCoordinator(
		regionID,
		snapshotPolicy,
		snapshotStorage,
		stateManagerForSnapshot,
	)
	
	// Create federation coordinator
	federationID := "test-federation"
	federationPolicy := DefaultFederationPolicy()
	coordinator := NewFederationCoordinator(
		federationID,
		regionID,
		federationPolicy,
		snapshotCoordinator,
		stateManager,
	)
	
	// Create and register mock callbacks
	mockCB := &mockCallback{shouldAgree: true}
	coordinator.RegisterCallbackHandler("stateChange", mockCB)
	coordinator.RegisterCallbackHandler("consensus", mockCB)
	
	// Verify callbacks were registered
	assert.Equal(t, 2, len(coordinator.callbackHandlers))
	assert.Contains(t, coordinator.callbackHandlers, "stateChange")
	assert.Contains(t, coordinator.callbackHandlers, "consensus")
}
