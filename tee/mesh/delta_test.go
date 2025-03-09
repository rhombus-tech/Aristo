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
	service := &MeshService{
		objectStates:     make(map[string][]byte),
		stateCache:       make(map[string]stateInfo),
	}
	
	// Test scenario: small change in large state
	originalState := make([]byte, 1024)
	for i := 0; i < len(originalState); i++ {
		originalState[i] = byte(i % 256)
	}
	
	// Store the original state
	objectID := "test-object-1"
	service.updateObjectState(objectID, originalState)
	
	// Create a modified state with a small change
	modifiedState := make([]byte, 1024)
	copy(modifiedState, originalState)
	
	// Modify just a small portion (10 bytes in the middle)
	for i := 500; i < 510; i++ {
		modifiedState[i] = 0xFF
	}
	
	// Test basic delta generation functionality
	t.Run("GenerateDelta", func(t *testing.T) {
		// Store as a different object to test delta generation
		modifiedObjectID := "test-object-2"
		service.updateObjectState(modifiedObjectID, modifiedState)
		
		// Generate delta from original to modified
		delta, err := service.generateDeltaUpdates(modifiedObjectID, originalState)
		require.NoError(t, err)
		
		// Delta should be significantly smaller than the full state
		assert.Less(t, len(delta), len(modifiedState)/2, 
			"Delta should be much smaller than the full state for small changes")
		
		// Test applying the delta
		reconstructed, err := service.applyDeltaUpdates(originalState, delta)
		require.NoError(t, err)
		
		// The reconstructed state should match the modified state
		assert.Equal(t, modifiedState, reconstructed, 
			"Reconstructed state should match the modified state after applying delta")
	})
	
	// Test the full sync process with delta updates
	t.Run("SyncWithDeltas", func(t *testing.T) {
		senderID := "test-sender"
		objectID := "test-object-3"
		
		// Initial state in cache
		initialState := []byte("Initial state of the object")
		service.stateCache[senderID+":"+objectID] = stateInfo{
			state:     initialState,
			timestamp: time.Now().UnixNano(),
		}
		
		// Create a new state with minor changes
		updatedState := []byte("Initial state of the updated object")
		
		// Generate a delta
		delta, err := bsdiff.Bytes(initialState, updatedState)
		if err != nil {
			// If delta generation fails, just use the full state for testing
			delta = updatedState
		}
		
		// Create a sync response with delta updates
		syncResp := &proto.SyncResponse{
			Success:      true,
			StateHash:    updatedState,
			TimestampNs:  time.Now().UnixNano(),
			DeltaUpdates: delta,
		}
		
		// Process the sync response
		err = service.handleReceivedSync(senderID, objectID, syncResp)
		require.NoError(t, err)
		
		// Check that the object state was updated correctly
		actualState, err := service.getStateForObject(objectID)
		require.NoError(t, err)
		
		// The state should be updated to the new state
		assert.Equal(t, updatedState, actualState)
	})
	
	// Test shouldUseDelta function
	t.Run("ShouldUseDelta", func(t *testing.T) {
		// Small change should use delta
		assert.True(t, shouldUseDelta(originalState, modifiedState))
		
		// Very different states should not use delta
		veryDifferentState := make([]byte, 1024)
		for i := 0; i < len(veryDifferentState); i++ {
			veryDifferentState[i] = byte(255 - (i % 256))
		}
		assert.False(t, shouldUseDelta(originalState, veryDifferentState))
		
		// Small states should not use delta
		smallState1 := []byte{1, 2, 3, 4}
		smallState2 := []byte{1, 2, 3, 5}
		assert.False(t, shouldUseDelta(smallState1, smallState2))
	})
}
