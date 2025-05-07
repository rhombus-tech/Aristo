// Copyright (C) 2025. All rights reserved.
// See the file LICENSE for licensing terms.

package snapshot

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/rhombus-tech/aristo/fix-gateway/pkg/blockchain"
	"github.com/rhombus-tech/aristo/fix-gateway/pkg/session"
)

// SnapshotManager manages the creation, storage, and restoration of gateway state snapshots
type SnapshotManager struct {
	gatewayID       string
	regionID        string
	teeID           string
	teeType         string
	sessionManager  *session.Manager
	blockchainConn  *blockchain.Connector
	snapshotStorage *SnapshotStorage
	config          SnapshotConfig
	ticker          *time.Ticker
	stopChan        chan struct{}
	mu              sync.RWMutex
}

// SnapshotConfig contains configuration for the snapshot manager
type SnapshotConfig struct {
	// How often to take periodic snapshots
	SnapshotInterval time.Duration
	
	// Whether to anchor snapshots to the blockchain
	AnchorToBlockchain bool
	
	// How many snapshots to keep in history
	RetentionCount int
	
	// Maximum number of recent messages to include in each snapshot
	MaxRecentMessages int
	
	// Maximum number of orders to include in each snapshot
	MaxOrderHistory int
}

// DefaultSnapshotConfig provides default configuration values
func DefaultSnapshotConfig() SnapshotConfig {
	return SnapshotConfig{
		SnapshotInterval:   10 * time.Minute,
		AnchorToBlockchain: true,
		RetentionCount:     24, // Keep 24 snapshots (4 hours at 10min intervals)
		MaxRecentMessages:  100,
		MaxOrderHistory:    1000,
	}
}

// NewSnapshotManager creates a new snapshot manager
func NewSnapshotManager(
	gatewayID string,
	sessionManager *session.Manager,
	blockchainConn *blockchain.Connector,
	config SnapshotConfig,
) *SnapshotManager {
	// Use the TEE mesh snapshot storage
	snapshotStorage := NewSnapshotStorage()
	
	// In a real TEE environment, these would be obtained from the environment
	teeID := "tee-" + gatewayID
	teeType := "SGX" // or "SEV" or "TDX" based on environment
	
	return &SnapshotManager{
		gatewayID:       gatewayID,
		regionID:        "default-region", // In production, this would be the cloud region
		teeID:           teeID,
		teeType:         teeType,
		sessionManager:  sessionManager,
		blockchainConn:  blockchainConn,
		snapshotStorage: snapshotStorage,
		config:          config,
		stopChan:        make(chan struct{}),
	}
}

// Start begins periodic snapshot creation
func (sm *SnapshotManager) Start() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	
	// Take an initial snapshot
	if err := sm.CreateSnapshot(); err != nil {
		log.Printf("Failed to create initial snapshot: %v", err)
	}
	
	// Start periodic snapshots
	sm.ticker = time.NewTicker(sm.config.SnapshotInterval)
	go func() {
		for {
			select {
			case <-sm.ticker.C:
				if err := sm.CreateSnapshot(); err != nil {
					log.Printf("Failed to create periodic snapshot: %v", err)
				}
			case <-sm.stopChan:
				sm.ticker.Stop()
				return
			}
		}
	}()
	
	log.Printf("Snapshot manager started with interval %v", sm.config.SnapshotInterval)
}

// Stop stops periodic snapshot creation
func (sm *SnapshotManager) Stop() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	
	if sm.ticker != nil {
		close(sm.stopChan)
		sm.ticker = nil
	}
	
	log.Println("Snapshot manager stopped")
}

// CreateSnapshot creates a new snapshot of the current gateway state
func (sm *SnapshotManager) CreateSnapshot() error {
	// Collect current state
	gatewayState := &GatewayState{
		Version:       "1.0",
		Timestamp:     time.Now(),
		GatewayID:     sm.gatewayID,
		ConfigHash:    "config-hash-placeholder", // Would be real hash in production
		Orders:        []OrderState{}, // Would populate from order tracker in production
		BlockchainTxs: []TransactionState{}, // Would populate from blockchain connector
		RecentMsgs:    []MessageState{}, // Would populate from message history
	}
	
	// Collect session state if session manager is available
	if sm.sessionManager != nil {
		gatewayState.Sessions = CollectSessionState(sm.sessionManager)
	} else {
		gatewayState.Sessions = []SessionState{}
	}
	
	// Create a mesh snapshot
	snapshot, err := gatewayState.CreateSnapshot(sm.regionID, sm.teeID, sm.teeType)
	if err != nil {
		return fmt.Errorf("failed to create state snapshot: %w", err)
	}
	
	// Store the snapshot
	if err := sm.snapshotStorage.StoreSnapshot(snapshot); err != nil {
		return fmt.Errorf("failed to store snapshot: %w", err)
	}
	
	// Anchor to blockchain if configured and blockchain connector is available
	if sm.config.AnchorToBlockchain && sm.blockchainConn != nil {
		sm.anchorToBlockchain(snapshot)
	}
	
	// Prune old snapshots to maintain retention policy
	sm.pruneOldSnapshots()
	
	log.Printf("Created gateway state snapshot: %x", snapshot.SnapshotID)
	return nil
}

// GetLatestSnapshot gets the latest snapshot
func (sm *SnapshotManager) GetLatestSnapshot() (*StateSnapshot, error) {
	return sm.snapshotStorage.GetLatestObjectSnapshot(sm.gatewayID)
}

// RestoreFromLatestSnapshot restores the gateway state from the latest snapshot
func (sm *SnapshotManager) RestoreFromLatestSnapshot() error {
	snapshot, err := sm.GetLatestSnapshot()
	if err != nil {
		return fmt.Errorf("failed to get latest snapshot: %w", err)
	}
	
	return sm.RestoreFromSnapshot(snapshot)
}

// RestoreFromSnapshot restores the gateway state from a specific snapshot
func (sm *SnapshotManager) RestoreFromSnapshot(snapshot *StateSnapshot) error {
	if snapshot == nil {
		return errors.New("cannot restore from nil snapshot")
	}
	
	// Extract the gateway state
	gatewayState, err := ExtractStateFromSnapshot(snapshot)
	if err != nil {
		return fmt.Errorf("failed to extract state from snapshot: %w", err)
	}
	
	// Apply state to systems
	// In a real implementation, this would restore session state, etc.
	log.Printf("Restored gateway state from snapshot %x (time: %s)", 
		snapshot.SnapshotID, gatewayState.Timestamp.Format(time.RFC3339))
	
	return nil
}

// anchorToBlockchain anchors a snapshot to the blockchain
func (sm *SnapshotManager) anchorToBlockchain(snapshot *StateSnapshot) {
	// In a real implementation, this would submit a transaction to the blockchain
	// with the snapshot ID as a reference
	log.Printf("Anchoring snapshot %x to blockchain", snapshot.SnapshotID)
	
	// This is just a placeholder for the actual blockchain anchoring
	// In production, we would use the blockchain connector to submit this data
}

// pruneOldSnapshots removes snapshots beyond the retention count
func (sm *SnapshotManager) pruneOldSnapshots() {
	// In a real implementation, this would query for all snapshots
	// and delete the oldest ones beyond the retention count
	log.Printf("Pruning old snapshots to maintain %d snapshot retention", sm.config.RetentionCount)
}
