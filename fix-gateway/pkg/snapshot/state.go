// Copyright (C) 2025. All rights reserved.
// See the file LICENSE for licensing terms.

package snapshot

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/rhombus-tech/aristo/fix-gateway/pkg/config"
	"github.com/rhombus-tech/aristo/fix-gateway/pkg/session"
)

// GatewayState represents the serializable state of the FIX gateway
type GatewayState struct {
	// Versioning and identification
	Version       string    `json:"version"`
	Timestamp     time.Time `json:"timestamp"`
	GatewayID     string    `json:"gateway_id"`
	ConfigHash    string    `json:"config_hash"`
	
	// Session state
	Sessions      []SessionState     `json:"sessions"`
	
	// Order state
	Orders        []OrderState       `json:"orders"`
	
	// Blockchain state
	BlockchainTxs []TransactionState `json:"blockchain_txs"`
	
	// Message history (limited to recent important messages)
	RecentMsgs    []MessageState     `json:"recent_msgs"`
}

// SessionState represents the state of a FIX session
type SessionState struct {
	SessionID       string    `json:"session_id"`
	ConnectionState string    `json:"connection_state"` // active, disconnected, etc.
	CounterpartyID  string    `json:"counterparty_id"`
	LastHeartbeat   time.Time `json:"last_heartbeat"`
	SequenceNumIn   int       `json:"sequence_num_in"`
	SequenceNumOut  int       `json:"sequence_num_out"`
	SessionType     string    `json:"session_type"` // NASDAQ, broker-dealer, etc.
}

// OrderState represents the state of an order
type OrderState struct {
	OrderID        string    `json:"order_id"`
	Symbol         string    `json:"symbol"`
	Side           string    `json:"side"`
	OrderType      string    `json:"order_type"`
	Price          float64   `json:"price"`
	Quantity       float64   `json:"quantity"`
	FilledQuantity float64   `json:"filled_quantity"`
	OrderStatus    string    `json:"order_status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	BrokerID       string    `json:"broker_id,omitempty"`
	ClientAccount  string    `json:"client_account,omitempty"`
}

// TransactionState represents a blockchain transaction
type TransactionState struct {
	TxID          string    `json:"tx_id"`
	OrderID       string    `json:"order_id"`
	TxType        string    `json:"tx_type"` // settlement, attestation, etc.
	Status        string    `json:"status"`  // pending, confirmed, failed
	Timestamp     time.Time `json:"timestamp"`
	BlockchainRef string    `json:"blockchain_ref,omitempty"` // reference to blockchain location
}

// MessageState represents important FIX messages for historical purposes
type MessageState struct {
	MsgType     string    `json:"msg_type"`
	MsgID       string    `json:"msg_id"`
	OrderID     string    `json:"order_id,omitempty"`
	SessionID   string    `json:"session_id"`
	Direction   string    `json:"direction"` // inbound, outbound
	Timestamp   time.Time `json:"timestamp"`
	Attestation []byte    `json:"attestation,omitempty"` // TEE attestation if available
}

// Serialize converts the gateway state to a byte array
func (gs *GatewayState) Serialize() ([]byte, error) {
	return json.Marshal(gs)
}

// Deserialize populates the gateway state from a byte array
func (gs *GatewayState) Deserialize(data []byte) error {
	return json.Unmarshal(data, gs)
}

// CreateSnapshot creates a mesh state snapshot from the gateway state
func (gs *GatewayState) CreateSnapshot(regionID, teeID, teeType string) (*StateSnapshot, error) {
	// Serialize the gateway state
	stateData, err := gs.Serialize()
	if err != nil {
		return nil, fmt.Errorf("failed to serialize gateway state: %w", err)
	}
	
	// Create a basic snapshot structure
	// In a production system, these would be provided by the TEE environment
	snapshot := &StateSnapshot{
		ObjectID:         gs.GatewayID,
		RegionID:         regionID,
		SnapshotType:     FullSnapshot,
		StateData:        stateData,
		TEEID:            teeID,
		TEEType:          teeType,
		// These fields would be populated by the TEE in a real system
		// TEEMeasurement:   []byte{}, 
		// TEESignature:     []byte{},
		// AccumulatorState: []byte{},
	}
	
	// Generate a snapshot ID
	snapshot.SnapshotID = ComputeSnapshotID(snapshot)
	
	return snapshot, nil
}

// ExtractStateFromSnapshot extracts a gateway state from a mesh state snapshot
func ExtractStateFromSnapshot(snapshot *StateSnapshot) (*GatewayState, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("snapshot is nil")
	}
	
	if len(snapshot.StateData) == 0 {
		return nil, fmt.Errorf("snapshot has no state data")
	}
	
	var state GatewayState
	if err := state.Deserialize(snapshot.StateData); err != nil {
		return nil, fmt.Errorf("failed to deserialize state data: %w", err)
	}
	
	return &state, nil
}

// CollectSessionState collects the current state of sessions from the session manager
func CollectSessionState(sessionManager *session.Manager) []SessionState {
	// In a real implementation, this would extract actual session state
	// For now, we'll return a placeholder
	return []SessionState{}
}

// CollectConfigHash generates a hash of the current configuration
func CollectConfigHash(cfg *config.FIXGatewayConfig) string {
	// In a real implementation, this would hash the config
	// For now, we'll return a placeholder
	return "config-hash-placeholder"
}
