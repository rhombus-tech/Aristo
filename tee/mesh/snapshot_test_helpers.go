//go:build testing
// +build testing

package mesh

import (
	"context"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/x/merkledb"
)

// DefaultStateManager for testing
type DefaultStateManager struct {
	objectStates     map[string][]byte
	objectStatesMutex sync.RWMutex
}

// NewDefaultStateManager creates a new DefaultStateManager for testing
func NewDefaultStateManager() *DefaultStateManager {
	return &DefaultStateManager{
		objectStates: make(map[string][]byte),
	}
}

// VerifySnapshot is a simple verification for testing
func (d *DefaultStateManager) VerifySnapshot(snapshot *StateSnapshot) error {
	return nil
}

// ListSnapshots returns an empty list for testing
func (d *DefaultStateManager) ListSnapshots(objectID, regionID string) ([]*StateSnapshot, error) {
	return []*StateSnapshot{}, nil
}

// MerkleDBSnapshotManager implementation for testing
type MerkleDBSnapshotManager struct {
	db           merkledb.MerkleDB
	stateManager *DefaultStateManager
	regionID     string
	teeID        string
	teeType      string
}

// NewMerkleDBSnapshotManager creates a new MerkleDBSnapshotManager for testing
func NewMerkleDBSnapshotManager(db merkledb.MerkleDB, stateManager *DefaultStateManager, regionID, teeID, teeType string) *MerkleDBSnapshotManager {
	return &MerkleDBSnapshotManager{
		db:           db,
		stateManager: stateManager,
		regionID:     regionID,
		teeID:        teeID,
		teeType:      teeType,
	}
}

// GetMerkleDBMetadata is a helper function for tests
func GetMerkleDBMetadata(ctx context.Context, db merkledb.MerkleDB) (map[string]interface{}, error) {
	rootID, err := db.GetMerkleRoot(ctx)
	if err != nil {
		return nil, err
	}
	
	// Create a simplified metadata map for testing
	metadata := map[string]interface{}{
		"version": uint64(1),
		"rootID":  rootID.String(),
	}
	return metadata, nil
}
