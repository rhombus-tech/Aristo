// Copyright (C) 2025. All rights reserved.
// See the file LICENSE for licensing terms.

package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// This file contains local implementations of the essential mesh package functionality
// to avoid dependency issues. In a production environment, these would be imported
// from the tee/mesh package.

var (
	// ErrInvalidSnapshot indicates an invalid state snapshot
	ErrInvalidSnapshot = errors.New("invalid state snapshot")
	
	// ErrSnapshotVerificationFailed indicates a failure in snapshot verification
	ErrSnapshotVerificationFailed = errors.New("snapshot verification failed")
	
	// ErrSnapshotOutdated indicates a snapshot that is too old to be used
	ErrSnapshotOutdated = errors.New("snapshot is outdated")
)

// SnapshotType defines the type of snapshot
type SnapshotType int

const (
	// FullSnapshot is a complete state snapshot
	FullSnapshot SnapshotType = iota
	
	// DeltaSnapshot is an incremental snapshot based on a previous full snapshot
	DeltaSnapshot
)

// StateSnapshot represents a cryptographically verifiable snapshot of state
type StateSnapshot struct {
	// Core identification
	ObjectID     string            // ID of the object this snapshot represents
	RegionID     string            // Region where this snapshot was created
	SnapshotID   []byte            // Unique identifier for this snapshot (hash)
	SnapshotType SnapshotType      // Type of snapshot (full or delta)
	
	// State data
	StateData        []byte        // The actual state data, possibly compressed
	AccumulatorState []byte        // The 32-byte accumulator state at snapshot time
	PreviousSnapshotID []byte      // Reference to previous snapshot (for deltas or chain)
	
	// TEE attestation
	TEEMeasurement []byte          // Measurement of the TEE that created this snapshot
	TEEID          string          // ID of the TEE that created this snapshot
	TEEType        string          // Type of TEE (SGX or SEV)
	TEESignature   []byte          // Signature from the TEE covering snapshot contents
	
	// Metadata
	CreatedAt     time.Time        // When the snapshot was created
	Description   string           // Human-readable description
}

// SnapshotChainInfo contains information about a chain of snapshots
type SnapshotChainInfo struct {
	ObjectID         string
	LatestSnapshotID []byte
	SnapshotCount    uint64
	OldestTimestamp  time.Time
	LatestTimestamp  time.Time
}

// SnapshotStorage provides storage for state snapshots
type SnapshotStorage struct {
	snapshotsMutex     sync.RWMutex
	snapshots          map[string][]*StateSnapshot          // objectID -> snapshots
	snapshotsByID      map[string]*StateSnapshot            // snapshotID -> snapshot
	snapshotChains     map[string]*SnapshotChainInfo        // objectID -> chain info
	objectLatestSnapshot map[string][]byte                  // objectID -> latest snapshotID
}

// NewSnapshotStorage creates a new snapshot storage
func NewSnapshotStorage() *SnapshotStorage {
	return &SnapshotStorage{
		snapshots:           make(map[string][]*StateSnapshot),
		snapshotsByID:       make(map[string]*StateSnapshot),
		snapshotChains:      make(map[string]*SnapshotChainInfo),
		objectLatestSnapshot: make(map[string][]byte),
	}
}

// ComputeSnapshotID calculates a unique identifier for a snapshot
func ComputeSnapshotID(snapshot *StateSnapshot) []byte {
	if snapshot == nil {
		return nil
	}
	
	// Create a hash of key snapshot fields
	h := sha256.New()
	h.Write([]byte(snapshot.ObjectID))
	h.Write([]byte(snapshot.RegionID))
	h.Write([]byte(fmt.Sprintf("%d", snapshot.SnapshotType)))
	h.Write(snapshot.StateData)
	
	if len(snapshot.PreviousSnapshotID) > 0 {
		h.Write(snapshot.PreviousSnapshotID)
	}
	
	return h.Sum(nil)
}

// StoreSnapshot stores a snapshot
func (s *SnapshotStorage) StoreSnapshot(snapshot *StateSnapshot) error {
	if snapshot == nil {
		return errors.New("cannot store nil snapshot")
	}
	
	snapshotID := snapshot.SnapshotID
	if len(snapshotID) == 0 {
		// Generate snapshot ID if not set
		snapshotID = ComputeSnapshotID(snapshot)
		snapshot.SnapshotID = snapshotID
	}
	
	s.snapshotsMutex.Lock()
	defer s.snapshotsMutex.Unlock()
	
	// Store in maps
	idStr := hex.EncodeToString(snapshotID)
	objectID := snapshot.ObjectID
	
	// Set creation time if not already set
	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = time.Now()
	}
	
	// Update snapshots map
	if _, exists := s.snapshots[objectID]; !exists {
		s.snapshots[objectID] = []*StateSnapshot{}
	}
	s.snapshots[objectID] = append(s.snapshots[objectID], snapshot)
	
	// Update snapshot by ID map
	s.snapshotsByID[idStr] = snapshot
	
	// Update latest snapshot reference
	s.objectLatestSnapshot[objectID] = snapshotID
	
	// Update or create chain info
	chainInfo, exists := s.snapshotChains[objectID]
	if !exists {
		chainInfo = &SnapshotChainInfo{
			ObjectID:        objectID,
			LatestSnapshotID: snapshotID,
			SnapshotCount:   1,
			OldestTimestamp: snapshot.CreatedAt,
			LatestTimestamp: snapshot.CreatedAt,
		}
	} else {
		chainInfo.LatestSnapshotID = snapshotID
		chainInfo.SnapshotCount++
		chainInfo.LatestTimestamp = snapshot.CreatedAt
	}
	s.snapshotChains[objectID] = chainInfo
	
	return nil
}

// GetLatestObjectSnapshot gets the latest snapshot for an object
func (s *SnapshotStorage) GetLatestObjectSnapshot(objectID string) (*StateSnapshot, error) {
	s.snapshotsMutex.RLock()
	defer s.snapshotsMutex.RUnlock()
	
	snapshotID, exists := s.objectLatestSnapshot[objectID]
	if !exists {
		return nil, fmt.Errorf("no snapshots found for object %s", objectID)
	}
	
	idStr := hex.EncodeToString(snapshotID)
	snapshot, exists := s.snapshotsByID[idStr]
	if !exists {
		return nil, fmt.Errorf("inconsistent state: snapshot %s not found", idStr)
	}
	
	return snapshot, nil
}
