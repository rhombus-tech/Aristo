package mesh

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

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
	
	// Add to object snapshots list
	if _, ok := s.snapshots[objectID]; !ok {
		s.snapshots[objectID] = make([]*StateSnapshot, 0)
	}
	s.snapshots[objectID] = append(s.snapshots[objectID], snapshot)
	
	// Store by ID
	s.snapshotsByID[idStr] = snapshot
	
	// Update chain info
	if _, ok := s.snapshotChains[objectID]; !ok {
		s.snapshotChains[objectID] = &SnapshotChainInfo{
			ObjectID:        objectID,
			SnapshotCount:   0,
			OldestTimestamp: snapshot.Timestamp,
			LatestTimestamp: snapshot.Timestamp,
		}
	}
	
	chain := s.snapshotChains[objectID]
	chain.SnapshotCount++
	chain.LatestSnapshotID = snapshotID
	
	if snapshot.Timestamp.Before(chain.OldestTimestamp) {
		chain.OldestTimestamp = snapshot.Timestamp
	}
	if snapshot.Timestamp.After(chain.LatestTimestamp) {
		chain.LatestTimestamp = snapshot.Timestamp
	}
	
	// Update latest
	s.objectLatestSnapshot[objectID] = snapshotID
	
	return nil
}

// GetSnapshot retrieves a snapshot by ID
func (s *SnapshotStorage) GetSnapshot(snapshotID []byte) (*StateSnapshot, error) {
	s.snapshotsMutex.RLock()
	defer s.snapshotsMutex.RUnlock()
	
	idStr := hex.EncodeToString(snapshotID)
	snapshot, ok := s.snapshotsByID[idStr]
	if !ok {
		return nil, fmt.Errorf("snapshot not found: %s", idStr)
	}
	
	return snapshot, nil
}

// GetLatestObjectSnapshot gets the latest snapshot for an object
func (s *SnapshotStorage) GetLatestObjectSnapshot(objectID string) (*StateSnapshot, error) {
	s.snapshotsMutex.RLock()
	defer s.snapshotsMutex.RUnlock()
	
	snapshotID, ok := s.objectLatestSnapshot[objectID]
	if !ok {
		return nil, fmt.Errorf("no snapshots for object: %s", objectID)
	}
	
	idStr := hex.EncodeToString(snapshotID)
	snapshot, ok := s.snapshotsByID[idStr]
	if !ok {
		return nil, fmt.Errorf("latest snapshot not found: %s", idStr)
	}
	
	return snapshot, nil
}

// GetChainInfo gets the chain info for an object
func (s *SnapshotStorage) GetChainInfo(objectID string) (*SnapshotChainInfo, error) {
	s.snapshotsMutex.RLock()
	defer s.snapshotsMutex.RUnlock()
	
	chain, ok := s.snapshotChains[objectID]
	if !ok {
		return nil, fmt.Errorf("no snapshot chain for object: %s", objectID)
	}
	
	return chain, nil
}

// ListObjectSnapshots lists all snapshots for an object
func (s *SnapshotStorage) ListObjectSnapshots(objectID string) ([]*StateSnapshot, error) {
	s.snapshotsMutex.RLock()
	defer s.snapshotsMutex.RUnlock()
	
	snapshots, ok := s.snapshots[objectID]
	if !ok {
		return nil, fmt.Errorf("no snapshots for object: %s", objectID)
	}
	
	// Return a copy to avoid concurrent modification
	result := make([]*StateSnapshot, len(snapshots))
	copy(result, snapshots)
	
	return result, nil
}

// DeleteSnapshot deletes a snapshot
func (s *SnapshotStorage) DeleteSnapshot(snapshotID []byte) error {
	s.snapshotsMutex.Lock()
	defer s.snapshotsMutex.Unlock()
	
	idStr := hex.EncodeToString(snapshotID)
	snapshot, ok := s.snapshotsByID[idStr]
	if !ok {
		return fmt.Errorf("snapshot not found: %s", idStr)
	}
	
	objectID := snapshot.ObjectID
	
	// Remove from snapshots list
	snapshots := s.snapshots[objectID]
	for i, snap := range snapshots {
		if hex.EncodeToString(snap.SnapshotID) == idStr {
			// Remove this snapshot
			s.snapshots[objectID] = append(snapshots[:i], snapshots[i+1:]...)
			break
		}
	}
	
	// Remove from map
	delete(s.snapshotsByID, idStr)
	
	// Update chain info
	if chain, ok := s.snapshotChains[objectID]; ok {
		chain.SnapshotCount--
		
		// If this was the latest, update latest
		if hex.EncodeToString(chain.LatestSnapshotID) == idStr {
			// Find new latest
			var latest *StateSnapshot
			for _, snap := range s.snapshots[objectID] {
				if latest == nil || snap.Timestamp.After(latest.Timestamp) {
					latest = snap
				}
			}
			
			if latest != nil {
				chain.LatestSnapshotID = latest.SnapshotID
				chain.LatestTimestamp = latest.Timestamp
				s.objectLatestSnapshot[objectID] = latest.SnapshotID
			} else {
				// No more snapshots
				delete(s.snapshotChains, objectID)
				delete(s.objectLatestSnapshot, objectID)
			}
		}
	}
	
	return nil
}

// Implement snapshot functionality for DefaultStateManager

// CreateSnapshot implements StateManager.CreateSnapshot
func (m *DefaultStateManager) CreateSnapshot(objectID string, regionID string, teeID string, teeType string) (*StateSnapshot, error) {
	// Get the current state
	state, err := m.GetState(objectID)
	if err != nil {
		return nil, &SnapshotError{
			Operation: "create",
			ObjectID:  objectID,
			Err:       err,
		}
	}
	
	// Create a new snapshot
	snapshot := &StateSnapshot{
		ObjectID:         objectID,
		RegionID:         regionID,
		SnapshotType:     FullSnapshot,
		StateData:        make([]byte, len(state)),
		AccumulatorState: generateMockAccumulatorState(), // In a real system, get from accumulator
		TEEID:            teeID,
		TEEType:          teeType,
		Timestamp:        time.Now(),
		RegionalMetadata: make(map[string]interface{}),
		Version:          1,
		CompressionType:  "none",
		OriginalSize:     uint64(len(state)),
		DataHash:         sha256.New().Sum(state),
	}
	
	// Copy state data
	copy(snapshot.StateData, state)
	
	// Generate TEE measurement and signature
	// In a real implementation, these would come from the actual TEE
	snapshot.TEEMeasurement = generateMockTEEMeasurement(teeID, teeType)
	snapshot.TEESignature = generateMockTEESignature(snapshot)
	
	// Compute snapshot ID
	snapshot.SnapshotID = ComputeSnapshotID(snapshot)
	
	return snapshot, nil
}

// VerifySnapshot implements StateManager.VerifySnapshot
func (m *DefaultStateManager) VerifySnapshot(snapshot *StateSnapshot) error {
	if snapshot == nil {
		return &SnapshotError{
			Operation: "verify",
			ObjectID:  "unknown",
			Err:       ErrInvalidSnapshot,
		}
	}
	
	// Verify data hash
	calculatedHash := sha256.New().Sum(snapshot.StateData)
	if !bytesEqual(calculatedHash, snapshot.DataHash) {
		return &SnapshotError{
			Operation: "verify",
			ObjectID:  snapshot.ObjectID,
			Err:       errors.New("data hash mismatch"),
		}
	}
	
	// Verify snapshot ID
	calculatedID := ComputeSnapshotID(snapshot)
	if !bytesEqual(calculatedID, snapshot.SnapshotID) {
		return &SnapshotError{
			Operation: "verify",
			ObjectID:  snapshot.ObjectID,
			Err:       errors.New("snapshot ID mismatch"),
		}
	}
	
	// In a real implementation, verify TEE measurement and signature
	// This is a mock implementation
	
	return nil
}

// RestoreFromSnapshot implements StateManager.RestoreFromSnapshot
func (m *DefaultStateManager) RestoreFromSnapshot(snapshot *StateSnapshot) error {
	if snapshot == nil {
		return &SnapshotError{
			Operation: "restore",
			ObjectID:  "unknown",
			Err:       ErrInvalidSnapshot,
		}
	}
	
	// Verify snapshot first
	err := m.VerifySnapshot(snapshot)
	if err != nil {
		return &SnapshotError{
			Operation: "restore",
			ObjectID:  snapshot.ObjectID,
			Err:       err,
		}
	}
	
	// Restore state
	err = m.SetState(snapshot.ObjectID, snapshot.StateData)
	if err != nil {
		return &SnapshotError{
			Operation: "restore",
			ObjectID:  snapshot.ObjectID,
			Err:       err,
		}
	}
	
	return nil
}

// ListSnapshots lists available snapshots for an object
func (m *DefaultStateManager) ListSnapshots(objectID string) ([]*SnapshotChainInfo, error) {
	// In a real implementation, this would retrieve snapshots from storage
	// This is a mock implementation that returns an empty list
	return []*SnapshotChainInfo{}, nil
}

// GetLatestSnapshot gets the latest snapshot for an object
func (m *DefaultStateManager) GetLatestSnapshot(objectID string) (*StateSnapshot, error) {
	// In a real implementation, this would retrieve the latest snapshot from storage
	// This is a mock implementation that creates a new snapshot
	return m.CreateSnapshot(objectID, "unknown-region", "unknown-tee", "unknown-type")
}

// Helper functions

// generateMockAccumulatorState generates a mock accumulator state
func generateMockAccumulatorState() []byte {
	// In a real implementation, this would be the actual accumulator state
	// This is a mock implementation that generates a random 32-byte value
	acc := make([]byte, 32)
	rand.Read(acc)
	return acc
}

// generateMockTEEMeasurement generates a mock TEE measurement
func generateMockTEEMeasurement(teeID string, teeType string) []byte {
	// In a real implementation, this would be the actual TEE measurement
	// This is a mock implementation that generates a deterministic value
	hasher := sha256.New()
	hasher.Write([]byte(teeID))
	hasher.Write([]byte(teeType))
	return hasher.Sum(nil)
}

// generateMockTEESignature generates a mock TEE signature
func generateMockTEESignature(snapshot *StateSnapshot) []byte {
	// In a real implementation, this would be an actual signature from the TEE
	// This is a mock implementation that generates a deterministic value
	hasher := sha256.New()
	hasher.Write([]byte(snapshot.ObjectID))
	hasher.Write([]byte(snapshot.RegionID))
	hasher.Write(snapshot.StateData)
	return hasher.Sum(nil)
}

// bytesEqual compares two byte slices for equality
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
