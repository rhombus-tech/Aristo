package mesh

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// CheckpointStorage handles persistence of checkpoints
type CheckpointStorage struct {
	baseStorage    Storage
	checkpointPath string
	meshID         string
	mu             sync.RWMutex
}

// Storage defines the interface for checkpoint storage
type Storage interface {
	Put(path string, data []byte) error
	Get(path string) ([]byte, error)
	Delete(path string) error
	List(path string) ([]string, error)
}

// BasicStorage is a minimal implementation of Storage used for the checkpoint system
type BasicStorage struct{}

// Put stores data at the given path
func (bs *BasicStorage) Put(path string, data []byte) error {
	// In a real implementation, this would store to disk/cloud/etc.
	// For this demo, we'll just log what would have happened
	log.Printf("Would store %d bytes at path: %s", len(data), path)
	return nil
}

// Get retrieves data from the given path
func (bs *BasicStorage) Get(path string) ([]byte, error) {
	// In a real implementation, this would retrieve from disk/cloud/etc.
	// For this demo, we'll just return simulated data
	log.Printf("Would retrieve data from path: %s", path)
	return []byte(fmt.Sprintf("simulated-data-%s", path)), nil
}

// Delete removes data at the given path
func (bs *BasicStorage) Delete(path string) error {
	// In a real implementation, this would delete from disk/cloud/etc.
	log.Printf("Would delete data at path: %s", path)
	return nil
}

// List returns all entries at the given path
func (bs *BasicStorage) List(path string) ([]string, error) {
	// In a real implementation, this would list entries from disk/cloud/etc.
	log.Printf("Would list entries at path: %s", path)
	return []string{"simulated-entry-1", "simulated-entry-2"}, nil
}

// NewCheckpointStorage creates a new checkpoint storage manager
func NewCheckpointStorage(meshID string) *CheckpointStorage {
	// Create a basic storage implementation
	return &CheckpointStorage{
		baseStorage:    &BasicStorage{},
		checkpointPath: "checkpoints",
		meshID:         meshID,
	}
}

// StoreCheckpoint persists a checkpoint to storage
func (cs *CheckpointStorage) StoreCheckpoint(ctx context.Context, checkpoint *PairSnapshot) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	
	// Create path for checkpoint
	checkpointPath := cs.buildCheckpointPath(checkpoint.PairID, checkpoint.CheckpointID)
	
	// Serialize checkpoint to JSON
	data, err := json.Marshal(checkpoint)
	if err != nil {
		return fmt.Errorf("failed to serialize checkpoint %s: %v", checkpoint.CheckpointID, err)
	}
	
	// Store checkpoint data
	err = cs.baseStorage.Put(checkpointPath, data)
	if err != nil {
		return fmt.Errorf("failed to store checkpoint %s: %v", checkpoint.CheckpointID, err)
	}
	
	// Update index of checkpoints
	return cs.updateCheckpointIndex(ctx, checkpoint.PairID, checkpoint.CheckpointID, checkpoint.Type, checkpoint.CreatedAt)
}

// LoadCheckpoint loads a checkpoint from storage
func (cs *CheckpointStorage) LoadCheckpoint(ctx context.Context, pairID, checkpointID string) (*PairSnapshot, error) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	
	// Build path to checkpoint
	checkpointPath := cs.buildCheckpointPath(pairID, checkpointID)
	
	// Retrieve data
	data, err := cs.baseStorage.Get(checkpointPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load checkpoint %s: %v", checkpointID, err)
	}
	
	// Deserialize
	var checkpoint PairSnapshot
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return nil, fmt.Errorf("failed to deserialize checkpoint %s: %v", checkpointID, err)
	}
	
	return &checkpoint, nil
}

// ListCheckpoints returns all checkpoints for a pair
func (cs *CheckpointStorage) ListCheckpoints(ctx context.Context, pairID string) ([]CheckpointIndex, error) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	
	// Load index for this pair
	indexPath := cs.buildIndexPath(pairID)
	data, err := cs.baseStorage.Get(indexPath)
	if err != nil {
		// If no index exists, return empty list
		return []CheckpointIndex{}, nil
	}
	
	// Deserialize index
	var index CheckpointIndexList
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("failed to deserialize checkpoint index: %v", err)
	}
	
	return index.Checkpoints, nil
}

// DeleteCheckpoint removes a checkpoint from storage
func (cs *CheckpointStorage) DeleteCheckpoint(ctx context.Context, pairID, checkpointID string) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	
	// Build path to checkpoint
	checkpointPath := cs.buildCheckpointPath(pairID, checkpointID)
	
	// Delete the checkpoint
	if err := cs.baseStorage.Delete(checkpointPath); err != nil {
		return fmt.Errorf("failed to delete checkpoint %s: %v", checkpointID, err)
	}
	
	// Update index
	return cs.removeFromIndex(ctx, pairID, checkpointID)
}

// StoreMeshCheckpoint stores a mesh-wide checkpoint
func (cs *CheckpointStorage) StoreMeshCheckpoint(ctx context.Context, checkpoint *MeshCheckpoint) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	
	// Create path for mesh checkpoint
	meshPath := cs.buildMeshCheckpointPath(checkpoint.CheckpointID)
	
	// Serialize checkpoint
	data, err := json.Marshal(checkpoint)
	if err != nil {
		return fmt.Errorf("failed to serialize mesh checkpoint %s: %v", checkpoint.CheckpointID, err)
	}
	
	// Store checkpoint data
	return cs.baseStorage.Put(meshPath, data)
}

// LoadMeshCheckpoint loads a mesh-wide checkpoint
func (cs *CheckpointStorage) LoadMeshCheckpoint(ctx context.Context, checkpointID string) (*MeshCheckpoint, error) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	
	// Build path to mesh checkpoint
	meshPath := cs.buildMeshCheckpointPath(checkpointID)
	
	// Retrieve data
	data, err := cs.baseStorage.Get(meshPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load mesh checkpoint %s: %v", checkpointID, err)
	}
	
	// Deserialize
	var checkpoint MeshCheckpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return nil, fmt.Errorf("failed to deserialize mesh checkpoint %s: %v", checkpointID, err)
	}
	
	return &checkpoint, nil
}

// PerformRetention applies retention policy to stored checkpoints
func (cs *CheckpointStorage) PerformRetention(ctx context.Context, retentionPeriod time.Duration, maxCheckpoints int) (int, error) {
	// List all pairs
	pairs, err := cs.listPairsWithCheckpoints(ctx)
	if err != nil {
		return 0, err
	}
	
	deletedCount := 0
	cutoffTime := time.Now().Add(-retentionPeriod)
	
	// Apply retention to each pair
	for _, pairID := range pairs {
		deleted, err := cs.applyRetentionToPair(ctx, pairID, cutoffTime, maxCheckpoints)
		if err != nil {
			log.Printf("Error applying retention to pair %s: %v", pairID, err)
			continue
		}
		deletedCount += deleted
	}
	
	return deletedCount, nil
}

// Helper functions

// CheckpointIndex represents a single checkpoint entry in the index
type CheckpointIndex struct {
	CheckpointID string         `json:"checkpoint_id"`
	Type         CheckpointType `json:"type"`
	CreatedAt    time.Time      `json:"created_at"`
}

// CheckpointIndexList represents the list of checkpoints for a pair
type CheckpointIndexList struct {
	PairID      string           `json:"pair_id"`
	Checkpoints []CheckpointIndex `json:"checkpoints"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

// buildCheckpointPath builds the storage path for a checkpoint
func (cs *CheckpointStorage) buildCheckpointPath(pairID, checkpointID string) string {
	return filepath.Join(cs.checkpointPath, cs.meshID, "pairs", pairID, "checkpoints", checkpointID)
}

// buildIndexPath builds the path to the checkpoint index for a pair
func (cs *CheckpointStorage) buildIndexPath(pairID string) string {
	return filepath.Join(cs.checkpointPath, cs.meshID, "pairs", pairID, "index.json")
}

// buildMeshCheckpointPath builds the path to a mesh checkpoint
func (cs *CheckpointStorage) buildMeshCheckpointPath(checkpointID string) string {
	return filepath.Join(cs.checkpointPath, cs.meshID, "mesh", checkpointID)
}

// updateCheckpointIndex adds a checkpoint to the index
func (cs *CheckpointStorage) updateCheckpointIndex(ctx context.Context, pairID, checkpointID string, cpType CheckpointType, createdAt time.Time) error {
	// Load existing index or create new one
	indexPath := cs.buildIndexPath(pairID)
	var index CheckpointIndexList
	
	data, err := cs.baseStorage.Get(indexPath)
	if err == nil {
		// Index exists, unmarshal it
		if err := json.Unmarshal(data, &index); err != nil {
			return fmt.Errorf("failed to deserialize checkpoint index: %v", err)
		}
	} else {
		// Create new index
		index = CheckpointIndexList{
			PairID:      pairID,
			Checkpoints: []CheckpointIndex{},
		}
	}
	
	// Add new checkpoint to index
	index.Checkpoints = append(index.Checkpoints, CheckpointIndex{
		CheckpointID: checkpointID,
		Type:         cpType,
		CreatedAt:    createdAt,
	})
	index.UpdatedAt = time.Now()
	
	// Serialize and store updated index
	updatedData, err := json.Marshal(&index)
	if err != nil {
		return fmt.Errorf("failed to serialize checkpoint index: %v", err)
	}
	
	return cs.baseStorage.Put(indexPath, updatedData)
}

// removeFromIndex removes a checkpoint from the index
func (cs *CheckpointStorage) removeFromIndex(ctx context.Context, pairID, checkpointID string) error {
	// Load existing index
	indexPath := cs.buildIndexPath(pairID)
	data, err := cs.baseStorage.Get(indexPath)
	if err != nil {
		// No index exists, nothing to do
		return nil
	}
	
	// Unmarshal index
	var index CheckpointIndexList
	if err := json.Unmarshal(data, &index); err != nil {
		return fmt.Errorf("failed to deserialize checkpoint index: %v", err)
	}
	
	// Filter out the checkpoint to remove
	newCheckpoints := make([]CheckpointIndex, 0, len(index.Checkpoints))
	for _, cp := range index.Checkpoints {
		if cp.CheckpointID != checkpointID {
			newCheckpoints = append(newCheckpoints, cp)
		}
	}
	
	// Update index
	index.Checkpoints = newCheckpoints
	index.UpdatedAt = time.Now()
	
	// Serialize and store updated index
	updatedData, err := json.Marshal(&index)
	if err != nil {
		return fmt.Errorf("failed to serialize checkpoint index: %v", err)
	}
	
	return cs.baseStorage.Put(indexPath, updatedData)
}

// listPairsWithCheckpoints returns all pairs that have checkpoints
func (cs *CheckpointStorage) listPairsWithCheckpoints(ctx context.Context) ([]string, error) {
	// Get directory listing for pairs
	pairsPath := filepath.Join(cs.checkpointPath, cs.meshID, "pairs")
	entries, err := cs.baseStorage.List(pairsPath)
	if err != nil {
		// If directory doesn't exist, return empty list
		return []string{}, nil
	}
	
	// Extract pair IDs
	pairs := make([]string, 0, len(entries))
	for _, entry := range entries {
		// Add entry name (which is the pair ID)
		pairs = append(pairs, filepath.Base(entry))
	}
	
	return pairs, nil
}

// applyRetentionToPair applies retention policy to a specific pair
func (cs *CheckpointStorage) applyRetentionToPair(ctx context.Context, pairID string, cutoffTime time.Time, maxCheckpoints int) (int, error) {
	// Get all checkpoints for this pair
	checkpoints, err := cs.ListCheckpoints(ctx, pairID)
	if err != nil {
		return 0, err
	}
	
	// Sort checkpoints by creation time (newest first)
	sort.Slice(checkpoints, func(i, j int) bool {
		return checkpoints[i].CreatedAt.After(checkpoints[j].CreatedAt)
	})
	
	deletedCount := 0
	checkpointsToKeep := make([]CheckpointIndex, 0, maxCheckpoints)
	
	// First pass: keep the newest maxCheckpoints
	for i, cp := range checkpoints {
		if i < maxCheckpoints {
			checkpointsToKeep = append(checkpointsToKeep, cp)
		} else if cp.CreatedAt.Before(cutoffTime) {
			// Delete older checkpoints that are beyond retention period
			if err := cs.DeleteCheckpoint(ctx, pairID, cp.CheckpointID); err != nil {
				log.Printf("Error deleting checkpoint %s: %v", cp.CheckpointID, err)
			} else {
				deletedCount++
			}
		} else {
			// Keep any checkpoint within retention period
			checkpointsToKeep = append(checkpointsToKeep, cp)
		}
	}
	
	return deletedCount, nil
}
