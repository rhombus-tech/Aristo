package mesh

import (
	"testing"
	"time"
	
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnapshotCreationAndVerification(t *testing.T) {
	// Create a state manager
	manager := NewDefaultStateManager()
	
	// Store some test state
	testObjectID := "test-object-1"
	testState := []byte(`{"value":"test-value","version":1}`)
	
	err := manager.SetState(testObjectID, testState)
	require.NoError(t, err)
	
	// Create a snapshot
	snapshot, err := manager.CreateSnapshot(testObjectID, "us-east", "tee-1", "SGX")
	require.NoError(t, err)
	
	// Verify the snapshot was created correctly
	assert.Equal(t, testObjectID, snapshot.ObjectID)
	assert.Equal(t, "us-east", snapshot.RegionID)
	assert.Equal(t, "tee-1", snapshot.TEEID)
	assert.Equal(t, "SGX", snapshot.TEEType)
	assert.Equal(t, FullSnapshot, snapshot.SnapshotType)
	assert.NotNil(t, snapshot.StateData)
	assert.NotNil(t, snapshot.AccumulatorState)
	assert.NotNil(t, snapshot.TEEMeasurement)
	assert.NotNil(t, snapshot.TEESignature)
	assert.NotNil(t, snapshot.SnapshotID)
	
	// Verify the state data matches the original state
	assert.Equal(t, testState, snapshot.StateData)
	
	// Test snapshot verification
	err = manager.VerifySnapshot(snapshot)
	assert.NoError(t, err)
	
	// Test snapshot restoration
	// First, change the state to something else
	err = manager.SetState(testObjectID, []byte(`{"value":"changed","version":2}`))
	require.NoError(t, err)
	
	// Then restore from snapshot
	err = manager.RestoreFromSnapshot(snapshot)
	require.NoError(t, err)
	
	// Verify the state was restored
	restoredState, err := manager.GetState(testObjectID)
	require.NoError(t, err)
	assert.Equal(t, testState, restoredState)
}

func TestSnapshotTampering(t *testing.T) {
	// Create a state manager
	manager := NewDefaultStateManager()
	
	// Store some test state
	testObjectID := "test-object-2"
	testState := []byte(`{"value":"test-value","version":1}`)
	
	err := manager.SetState(testObjectID, testState)
	require.NoError(t, err)
	
	// Create a snapshot
	snapshot, err := manager.CreateSnapshot(testObjectID, "us-east", "tee-2", "SGX")
	require.NoError(t, err)
	
	// Verify the original snapshot
	err = manager.VerifySnapshot(snapshot)
	assert.NoError(t, err)
	
	// Tamper with the state data
	tamperedSnapshot := *snapshot
	tamperedSnapshot.StateData = []byte(`{"value":"tampered","version":999}`)
	
	// Verification should fail
	err = manager.VerifySnapshot(&tamperedSnapshot)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "data hash mismatch")
}

func TestSnapshotChain(t *testing.T) {
	// Create a state manager
	manager := NewDefaultStateManager()
	storage := NewSnapshotStorage()
	
	// Store some test state
	testObjectID := "test-object-3"
	
	// Create a series of snapshots
	for i := 1; i <= 3; i++ {
		// Update state
		testState := []byte(`{"value":"test-value","version":` + string([]byte{byte('0' + i)}) + `}`)
		err := manager.SetState(testObjectID, testState)
		require.NoError(t, err)
		
		// Create snapshot
		snapshot, err := manager.CreateSnapshot(testObjectID, "us-east", "tee-3", "SGX")
		require.NoError(t, err)
		
		// Add small delay to ensure timestamps are different
		time.Sleep(10 * time.Millisecond)
		
		// Store in chain
		err = storage.StoreSnapshot(snapshot)
		require.NoError(t, err)
	}
	
	// Check chain info
	chainInfo, err := storage.GetChainInfo(testObjectID)
	require.NoError(t, err)
	
	// Verify chain properties
	assert.Equal(t, testObjectID, chainInfo.ObjectID)
	assert.Equal(t, uint64(3), chainInfo.SnapshotCount)
	assert.NotNil(t, chainInfo.LatestSnapshotID)
	
	// Get the latest snapshot
	latestSnapshot, err := storage.GetLatestObjectSnapshot(testObjectID)
	require.NoError(t, err)
	
	// Verify it's the version 3 snapshot
	restoredState := latestSnapshot.StateData
	assert.Contains(t, string(restoredState), `"version":3`)
}

func TestCrossRegionalSnapshotPolicy(t *testing.T) {
	// Create state managers for different regions
	usEastManager := NewDefaultStateManager()
	euCentralManager := NewDefaultStateManager()
	
	// Store test state
	testObjectID := "test-object-4"
	testState := []byte(`{"value":"test-value","region":"us-east"}`)
	
	err := usEastManager.SetState(testObjectID, testState)
	require.NoError(t, err)
	
	// Create snapshot in US-East
	usEastSnapshot, err := usEastManager.CreateSnapshot(testObjectID, "us-east", "tee-us-1", "SGX")
	require.NoError(t, err)
	
	// Verify in same region - should succeed
	err = usEastManager.VerifySnapshot(usEastSnapshot)
	assert.NoError(t, err)
	
	// Verify in different region - would depend on policy
	// For this simple test, we'll just ensure the snapshot itself is valid
	err = euCentralManager.VerifySnapshot(usEastSnapshot)
	assert.NoError(t, err)
	
	// In a full implementation, we'd check policy here to determine
	// if cross-regional verification is allowed
}
