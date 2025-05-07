// Copyright (C) 2025. All rights reserved.
// See the file LICENSE for licensing terms.

package snapshot

import (
	"testing"
	"time"
)

func TestGatewayStateSerialization(t *testing.T) {
	// Create a test state
	state := &GatewayState{
		Version:    "1.0",
		Timestamp:  time.Now(),
		GatewayID:  "test-gateway",
		ConfigHash: "test-config-hash",
		Sessions: []SessionState{
			{
				SessionID:       "test-session",
				ConnectionState: "active",
				CounterpartyID:  "NASDAQ",
				LastHeartbeat:   time.Now(),
				SequenceNumIn:   100,
				SequenceNumOut:  101,
				SessionType:     "NASDAQ",
			},
		},
		Orders: []OrderState{
			{
				OrderID:        "order-123",
				Symbol:         "AAPL",
				Side:           "buy",
				OrderType:      "limit",
				Price:          150.50,
				Quantity:       100,
				FilledQuantity: 50,
				OrderStatus:    "partially_filled",
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
				BrokerID:       "broker-abc",
				ClientAccount:  "client-1",
			},
		},
		BlockchainTxs: []TransactionState{
			{
				TxID:          "tx-456",
				OrderID:       "order-123",
				TxType:        "settlement",
				Status:        "confirmed",
				Timestamp:     time.Now(),
				BlockchainRef: "0x123456",
			},
		},
		RecentMsgs: []MessageState{
			{
				MsgType:   "NewOrderSingle",
				MsgID:     "msg-789",
				OrderID:   "order-123",
				SessionID: "test-session",
				Direction: "inbound",
				Timestamp: time.Now(),
			},
		},
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
	if deserializedState.ConfigHash != state.ConfigHash {
		t.Errorf("ConfigHash mismatch: got %s, want %s", deserializedState.ConfigHash, state.ConfigHash)
	}

	// Verify collections have expected sizes
	if len(deserializedState.Sessions) != len(state.Sessions) {
		t.Errorf("Sessions count mismatch: got %d, want %d", len(deserializedState.Sessions), len(state.Sessions))
	}
	if len(deserializedState.Orders) != len(state.Orders) {
		t.Errorf("Orders count mismatch: got %d, want %d", len(deserializedState.Orders), len(state.Orders))
	}
	if len(deserializedState.BlockchainTxs) != len(state.BlockchainTxs) {
		t.Errorf("BlockchainTxs count mismatch: got %d, want %d", len(deserializedState.BlockchainTxs), len(state.BlockchainTxs))
	}
	if len(deserializedState.RecentMsgs) != len(state.RecentMsgs) {
		t.Errorf("RecentMsgs count mismatch: got %d, want %d", len(deserializedState.RecentMsgs), len(state.RecentMsgs))
	}

	// Verify one specific field from each collection
	if len(deserializedState.Sessions) > 0 && deserializedState.Sessions[0].SessionID != state.Sessions[0].SessionID {
		t.Errorf("Session ID mismatch: got %s, want %s", deserializedState.Sessions[0].SessionID, state.Sessions[0].SessionID)
	}
	if len(deserializedState.Orders) > 0 && deserializedState.Orders[0].OrderID != state.Orders[0].OrderID {
		t.Errorf("Order ID mismatch: got %s, want %s", deserializedState.Orders[0].OrderID, state.Orders[0].OrderID)
	}
}

func TestCreateSnapshot(t *testing.T) {
	// Create a test state
	state := &GatewayState{
		Version:    "1.0",
		Timestamp:  time.Now(),
		GatewayID:  "test-gateway",
		ConfigHash: "test-config-hash",
	}

	// Create a snapshot
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
	if snapshot.TEEID != "test-tee" {
		t.Errorf("TEEID mismatch: got %s, want %s", snapshot.TEEID, "test-tee")
	}
	if snapshot.TEEType != "SGX" {
		t.Errorf("TEEType mismatch: got %s, want %s", snapshot.TEEType, "SGX")
	}
	if snapshot.SnapshotType != FullSnapshot {
		t.Errorf("SnapshotType mismatch: got %d, want %d", snapshot.SnapshotType, FullSnapshot)
	}
	if len(snapshot.SnapshotID) == 0 {
		t.Error("SnapshotID should not be empty")
	}
	if len(snapshot.StateData) == 0 {
		t.Error("StateData should not be empty")
	}

	// Test round-trip extraction
	extractedState, err := ExtractStateFromSnapshot(snapshot)
	if err != nil {
		t.Fatalf("Failed to extract state from snapshot: %v", err)
	}

	// Verify key fields match
	if extractedState.Version != state.Version {
		t.Errorf("Version mismatch: got %s, want %s", extractedState.Version, state.Version)
	}
	if extractedState.GatewayID != state.GatewayID {
		t.Errorf("GatewayID mismatch: got %s, want %s", extractedState.GatewayID, state.GatewayID)
	}
	if extractedState.ConfigHash != state.ConfigHash {
		t.Errorf("ConfigHash mismatch: got %s, want %s", extractedState.ConfigHash, state.ConfigHash)
	}
}
