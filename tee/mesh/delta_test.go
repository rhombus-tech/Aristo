package mesh

import (
	"testing"
	"time"
	
	"github.com/gabstv/go-bsdiff/pkg/bsdiff"
	"github.com/rhombus-tech/vm/tee/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeltaUpdates(t *testing.T) {
	// Create a mesh service for testing
	stateManager := NewDefaultStateManager()
	service := &TeeMeshService{
		stateManager:    stateManager,
		stateCache:      make(map[string]stateInfo),
	}
	
	// Test scenario: small change in large state
	originalState := make([]byte, 1024)
	for i := 0; i < len(originalState); i++ {
		originalState[i] = byte(i % 256)
	}
	
	// Store the original state
	objectID := "test-object-1"
	require.NoError(t, stateManager.SetState(objectID, originalState))
	
	// Create an updated state with a small change
	updatedState := make([]byte, len(originalState))
	copy(updatedState, originalState)
	updatedState[100] = 99 // Change just one byte
	
	// Test generating delta updates
	t.Run("GenerateDelta", func(t *testing.T) {
		// Modify the current state to have a small change
		// This is needed because the test assumes generateDeltaUpdates compares 
		// the provided previousState with a current state from stateManager
		currentState := make([]byte, len(originalState))
		copy(currentState, originalState)
		currentState[100] = 99 // Change byte at position 100 to value 99
		require.NoError(t, stateManager.SetState(objectID, currentState))
		
		// Now generate the delta
		delta, err := service.generateDeltaUpdates(objectID, originalState)
		require.NoError(t, err)
		
		// The delta should be much smaller than the full state
		assert.Less(t, len(delta), len(updatedState)/2, "Delta should be smaller than half the state size")
		
		// Apply the delta to the original state
		newState, err := service.applyDeltaUpdates(originalState, delta)
		require.NoError(t, err)
		
		// The result should match the updated state with the change at position 100
		assert.Equal(t, currentState, newState)
	})
	
	// Test applying delta updates
	t.Run("ApplyDelta", func(t *testing.T) {
		// Generate a delta using bsdiff directly
		delta, err := bsdiff.Bytes(originalState, updatedState)
		require.NoError(t, err)
		
		// Apply the delta
		newState, err := service.applyDeltaUpdates(originalState, delta)
		require.NoError(t, err)
		
		// The result should match the updated state
		assert.Equal(t, updatedState, newState)
	})
	
	// Test shouldUseDelta function
	t.Run("ShouldUseDelta", func(t *testing.T) {
		// Small change in large state - should use delta
		assert.True(t, service.shouldUseDelta(originalState, updatedState, 0.5))
		
		// Create a state with major changes
		majorChangeState := make([]byte, len(originalState))
		for i := 0; i < len(majorChangeState); i++ {
			majorChangeState[i] = byte(255 - (i % 256))
		}
		
		// Major change - should not use delta
		assert.False(t, service.shouldUseDelta(originalState, majorChangeState, 0.5))
		
		// Very small state - should not use delta regardless of changes
		tinyState1 := []byte{1, 2, 3}
		tinyState2 := []byte{1, 2, 4}
		assert.False(t, service.shouldUseDelta(tinyState1, tinyState2, 0.5))
	})
	
	// Test the sync process with delta updates
	t.Run("SyncWithDelta", func(t *testing.T) {
		// Set up object state for testing
		objectID := "test-object-2"
		senderID := "test-sender"
		require.NoError(t, stateManager.SetState(objectID, originalState))
		
		// Create a delta
		delta, err := bsdiff.Bytes(originalState, updatedState)
		require.NoError(t, err)
		
		// For very small changes, sometimes the delta can be larger than the state
		// In a real implementation, shouldUseDelta would determine this
		if len(delta) > len(updatedState) {
			delta = updatedState
		}
		
		// Create a sync response with delta updates
		syncResp := &proto.SyncResponse{
			Success:      true,
			StateHash:    updatedState,
			TimestampNs:  time.Now().UnixNano(),
			DeltaUpdates: delta,
		}
		
		// We need to update the test implementation since our handleReceivedSync 
		// has changed to not take objectID and senderID parameters
		
		// Set the global context for the test
		// In a real implementation, this would be properly tracked
		service.teeID = senderID 
		require.NoError(t, service.stateManager.SetState("current-sync-object", originalState))
		
		// Process the sync response
		err = service.handleReceivedSync(syncResp)
		require.NoError(t, err)
		
		// Check that the object state was updated correctly
		actualState, err := service.stateManager.GetState("current-sync-object") 
		require.NoError(t, err)
		
		// The state should be updated to the new state
		assert.Equal(t, updatedState, actualState)
	})
}
