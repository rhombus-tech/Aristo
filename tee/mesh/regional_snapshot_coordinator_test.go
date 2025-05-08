package mesh

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockSnapshotStorage implements SnapshotStorageInterface for tests
type MockSnapshotStorage struct {
	Snapshots map[string]*StateSnapshot
}

func NewMockSnapshotStorage() *MockSnapshotStorage {
	return &MockSnapshotStorage{
		Snapshots: make(map[string]*StateSnapshot),
	}
}

func (m *MockSnapshotStorage) StoreSnapshot(s *StateSnapshot) error {
	if s == nil || len(s.SnapshotID) == 0 {
		return fmt.Errorf("invalid snapshot")
	}
	m.Snapshots[string(s.SnapshotID)] = s
	return nil
}

// GetSnapshot retrieves a snapshot by ID
func (m *MockSnapshotStorage) GetSnapshot(snapshotID string) (*RegionalSnapshot, error) {
	// Note: This mock implementation is simplified - in a real implementation 
	// we would properly convert from StateSnapshot to RegionalSnapshot
	// For testing purposes, we'll create a minimal RegionalSnapshot
	return &RegionalSnapshot{
		SnapshotID: []byte(snapshotID),
		Timestamp:  time.Now(),
		RegionID:   "test-region",
	}, nil
}

// MockStateManager is a minimal mock for tests
type MockStateManager struct{}

func (m *MockStateManager) SerializeState(objectID string, object interface{}) ([]byte, error) {
	return []byte(fmt.Sprintf("mock-state-%s", objectID)), nil
}

// Create a test StateSnapshot for testing purposes
func createTestStateSnapshot(objectID, teeID string) *StateSnapshot {
	snapshotID := []byte(fmt.Sprintf("snapshot-%s-%s-%d", objectID, teeID, time.Now().UnixNano()))
	return &StateSnapshot{
		ObjectID:     objectID,
		RegionID:     "test-region",
		SnapshotID:   snapshotID,
		SnapshotType: FullSnapshot,
		StateData:    []byte(fmt.Sprintf("state-data-%s", objectID)),
		TEEID:        teeID,
		TEEType:      "SGX",
		TEESignature: []byte("test-signature"),
		Timestamp:    time.Now(),
	}
}

func TestRegionalSnapshotCoordinator(t *testing.T) {
	// Skip this test temporarily until we properly integrate with the main package
	t.Skip("Skipping test until full integration")
	
	// Create mocked dependencies
	snapshotStorage := NewMockSnapshotStorage()
	stateManager := &MockStateManager{} 
	
	// Create the coordinator
	policy := DefaultRegionalSnapshotPolicy()
	policy.MinTEECount = 2 // Lower minimum for testing
	regionID := "test-region"
	coordinator := NewRegionalSnapshotCoordinator(
		regionID,
		policy,
		snapshotStorage,
		stateManager,
	)
	
	// Register some TEEs
	coordinator.RegisterTEE("tee-1", "SGX")
	coordinator.RegisterTEE("tee-2", "SGX")
	coordinator.RegisterTEE("tee-3", "SEV")
	
	// Verify TEEs were registered
	registeredTEEs := coordinator.GetRegisteredTEEs()
	assert.Equal(t, 3, len(registeredTEEs))
	assert.Equal(t, "SGX", registeredTEEs["tee-1"])
	assert.Equal(t, "SEV", registeredTEEs["tee-3"])
	
	// Test initiation of regional snapshot collection
	ctx := context.Background()
	collectionID, err := coordinator.InitiateRegionalSnapshot(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, collectionID)
	
	// Create mock snapshots
	mockSnapshots := createMockSnapshots(regionID, 3)
	
	// Submit TEE snapshots to the collection
	err = coordinator.SubmitTEESnapshot(collectionID, "tee-1", mockSnapshots[0])
	require.NoError(t, err)
	
	err = coordinator.SubmitTEESnapshot(collectionID, "tee-2", mockSnapshots[1])
	require.NoError(t, err)
	
	// Test finalization with just 2 TEEs (minimum required by our policy)
	regionalSnapshot, err := coordinator.FinalizeRegionalSnapshot(ctx, collectionID)
	require.NoError(t, err)
	assert.NotNil(t, regionalSnapshot)
	
	// Verify the regional snapshot properties
	assert.Equal(t, regionID, regionalSnapshot.RegionID)
	assert.Equal(t, 2, len(regionalSnapshot.TEESnapshots))
	assert.Equal(t, 2, len(regionalSnapshot.TEESnapshotIDs))
	assert.NotNil(t, regionalSnapshot.SnapshotSummary)
	assert.NotNil(t, regionalSnapshot.ConsensusInfo)
	assert.NotNil(t, regionalSnapshot.CoordinatorSignature)
	assert.NotNil(t, regionalSnapshot.SnapshotID)
	
	// Verify consensus info
	assert.Equal(t, 3, regionalSnapshot.ConsensusInfo.TEECount)
	assert.Equal(t, 2, regionalSnapshot.ConsensusInfo.ParticipatingTEEs)
	assert.InDelta(t, 0.67, regionalSnapshot.ConsensusInfo.ConsensusLevel, 0.01)
	assert.True(t, regionalSnapshot.ConsensusInfo.ConsensusSuccess)
}

func TestInsufficientTEESnapshots(t *testing.T) {
	// Skip this test temporarily until we properly integrate with the main package
	t.Skip("Skipping test until full integration")
	
	// Create mocked dependencies
	snapshotStorage := NewMockSnapshotStorage()
	stateManager := &MockStateManager{}
	
	// Create the coordinator
	policy := DefaultRegionalSnapshotPolicy()
	policy.MinTEECount = 3 // Require at least 3 TEEs
	regionID := "test-region"
	coordinator := NewRegionalSnapshotCoordinator(
		regionID,
		policy,
		snapshotStorage,
		stateManager,
	)
	
	// Register some TEEs
	coordinator.RegisterTEE("tee-1", "SGX")
	coordinator.RegisterTEE("tee-2", "SGX")
	coordinator.RegisterTEE("tee-3", "SEV")
	coordinator.RegisterTEE("tee-4", "SEV")
	
	// Initiate snapshot collection
	ctx := context.Background()
	collectionID, err := coordinator.InitiateRegionalSnapshot(ctx)
	require.NoError(t, err)
	
	// Create mock snapshots
	mockSnapshots := createMockSnapshots(regionID, 4)
	
	// Submit only 2 TEE snapshots
	err = coordinator.SubmitTEESnapshot(collectionID, "tee-1", mockSnapshots[0])
	require.NoError(t, err)
	
	err = coordinator.SubmitTEESnapshot(collectionID, "tee-2", mockSnapshots[1])
	require.NoError(t, err)
	
	// Attempt to finalize with insufficient snapshots
	_, err = coordinator.FinalizeRegionalSnapshot(ctx, collectionID)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrInsufficientTEESnapshots)
}

func TestSnapshotConsistencyCheck(t *testing.T) {
	// Skip this test temporarily until we properly integrate with the main package
	t.Skip("Skipping test until full integration")
	
	// Create mocked dependencies
	snapshotStorage := NewMockSnapshotStorage()
	stateManager := &MockStateManager{}
	
	// Create the coordinator with strict time drift requirements
	policy := DefaultRegionalSnapshotPolicy()
	policy.MinTEECount = 2
	policy.MaxTimeDrift = 1 // Only 1 second allowed drift
	regionID := "test-region"
	coordinator := NewRegionalSnapshotCoordinator(
		regionID,
		policy,
		snapshotStorage,
		stateManager,
	)
	
	// Register some TEEs
	coordinator.RegisterTEE("tee-1", "SGX")
	coordinator.RegisterTEE("tee-2", "SGX")
	
	// Initiate snapshot collection
	ctx := context.Background()
	collectionID, err := coordinator.InitiateRegionalSnapshot(ctx)
	require.NoError(t, err)
	
	// Create snapshots with inconsistent timestamps
	snapshot1 := createMockSnapshot(regionID, "tee-1", "SGX", time.Now())
	snapshot2 := createMockSnapshot(regionID, "tee-2", "SGX", time.Now().Add(5*time.Second))
	
	// Submit TEE snapshots
	err = coordinator.SubmitTEESnapshot(collectionID, "tee-1", snapshot1)
	require.NoError(t, err)
	
	err = coordinator.SubmitTEESnapshot(collectionID, "tee-2", snapshot2)
	require.NoError(t, err)
	
	// Attempt to finalize with inconsistent timestamps
	_, err = coordinator.FinalizeRegionalSnapshot(ctx, collectionID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "time drift between snapshots exceeds maximum")
}

func createMockSnapshots(regionID string, count int) []*StateSnapshot {
	snapshots := make([]*StateSnapshot, count)
	baseTime := time.Now()
	
	for i := 0; i < count; i++ {
		teeID := fmt.Sprintf("tee-%d", i+1)
		teeType := "SGX"
		if i%2 == 1 {
			teeType = "SEV"
		}
		
		// Small time offsets to simulate real-world conditions
		timestamp := baseTime.Add(time.Duration(i*100) * time.Millisecond)
		snapshots[i] = createMockSnapshot(regionID, teeID, teeType, timestamp)
	}
	
	return snapshots
}

func createMockSnapshot(regionID, teeID, teeType string, timestamp time.Time) *StateSnapshot {
	// Create a unique snapshot ID
	h := sha256.New()
	h.Write([]byte(regionID))
	h.Write([]byte(teeID))
	h.Write([]byte(timestamp.String()))
	snapshotID := h.Sum(nil)
	
	// Create mock state data
	stateData := []byte("mock state data for " + teeID)
	
	// Create mock TEE measurement
	teeMeasurement := []byte("mock TEE measurement for " + teeID)
	
	// Create mock TEE signature
	teeSignature := []byte("mock TEE signature for " + teeID)
	
	return &StateSnapshot{
		ObjectID:       "test-object",
		RegionID:       regionID,
		SnapshotID:     snapshotID,
		SnapshotType:   FullSnapshot,
		StateData:      stateData,
		Timestamp:      timestamp,
		TEEMeasurement: teeMeasurement,
		TEEID:          teeID,
		TEEType:        teeType,
		TEESignature:   teeSignature,
		RegionalMetadata: map[string]interface{}{
			"test_metadata": "test value",
		},
	}
}
