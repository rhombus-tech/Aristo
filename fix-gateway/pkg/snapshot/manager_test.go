// Copyright (C) 2025. All rights reserved.
// See the file LICENSE for licensing terms.

package snapshot

import (
	"fmt"
	"testing"
	"time"
)

// Simple tests for the snapshot functionality

func TestStateSerializationAndSnapshot(t *testing.T) {
	// Create a test state
	state := &GatewayState{
		Version:    "1.0",
		Timestamp:  time.Now(),
		GatewayID:  "test-gateway",
		ConfigHash: "test-config-hash",
		Sessions:   []SessionState{},
		Orders:     []OrderState{},
	}

	// Test serialization
	data, err := state.Serialize()
	if err != nil {
		t.Fatalf("Failed to serialize state: %v", err)
	}

	// Test deserialization
	var deserializedState GatewayState
	if err := deserializedState.Deserialize(data); err != nil {
		t.Fatalf("Failed to deserialize state: %v", err)
	}

	// Verify key fields match
	if deserializedState.Version != state.Version {
		t.Errorf("Version mismatch: got %s, want %s", deserializedState.Version, state.Version)
	}
	if deserializedState.GatewayID != state.GatewayID {
		t.Errorf("GatewayID mismatch: got %s, want %s", deserializedState.GatewayID, state.GatewayID)
	}

	// Create a snapshot from the state
	snapshot, err := state.CreateSnapshot("test-region", "test-tee", "SGX")
	if err != nil {
		t.Fatalf("Failed to create snapshot: %v", err)
	}

	// Verify snapshot fields
	if snapshot.ObjectID != state.GatewayID {
		t.Errorf("ObjectID mismatch: got %s, want %s", snapshot.ObjectID, state.GatewayID)
	}
	if snapshot.RegionID != "test-region" {
		t.Errorf("RegionID mismatch: got %s, want %s", snapshot.RegionID, "test-region")
	}
	if snapshot.SnapshotType != FullSnapshot {
		t.Errorf("SnapshotType mismatch: got %d, want %d", snapshot.SnapshotType, FullSnapshot)
	}
	if len(snapshot.StateData) == 0 {
		t.Error("StateData should not be empty")
	}
}

func TestSnapshotStorage(t *testing.T) {
	// Initialize storage
	storage := NewSnapshotStorage()

	// Create and store multiple snapshots
	for i := 0; i < 3; i++ {
		snapshot := &StateSnapshot{
			ObjectID:     "test-object",
			RegionID:     "test-region",
			SnapshotType: FullSnapshot,
			StateData:    []byte(fmt.Sprintf("test state data %d", i)),
			TEEID:        "test-tee",
			TEEType:      "SGX",
			CreatedAt:    time.Now(),
		}

		// Generate ID and store
		snapshot.SnapshotID = ComputeSnapshotID(snapshot)
		if err := storage.StoreSnapshot(snapshot); err != nil {
			t.Fatalf("Failed to store snapshot %d: %v", i, err)
		}

		// Small delay to ensure different timestamps
		time.Sleep(5 * time.Millisecond)
	}

	// Retrieve the latest snapshot
	latestSnapshot, err := storage.GetLatestObjectSnapshot("test-object")
	if err != nil {
		t.Fatalf("Failed to get latest snapshot: %v", err)
	}

	// Verify it contains the latest data
	expectedData := "test state data 2" // The last snapshot we created
	if string(latestSnapshot.StateData) != expectedData {
		t.Errorf("StateData mismatch: got %s, want %s", string(latestSnapshot.StateData), expectedData)
	}
}
