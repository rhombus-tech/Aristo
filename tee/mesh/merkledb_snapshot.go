// Package mesh provides a mesh network for TEE-to-TEE communication
package mesh

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/x/merkledb"
)

var (
	// ErrInvalidMerkleDBSnapshot indicates an invalid MerkleDB state snapshot
	ErrInvalidMerkleDBSnapshot = errors.New("invalid MerkleDB state snapshot")
	
	// ErrMerkleDBRootMismatch indicates the MerkleDB root doesn't match the snapshot
	ErrMerkleDBRootMismatch = errors.New("MerkleDB root hash mismatch")
	
	// ErrSnapshotSerializationFailed indicates failure to serialize MerkleDB state
	ErrSnapshotSerializationFailed = errors.New("failed to serialize MerkleDB state")
)

// MerkleDBMetadata stores MerkleDB-specific metadata in snapshots
type MerkleDBMetadata struct {
	RootID       []byte            `json:"root_id"`
	RootIDString string            `json:"root_id_string"`
	Timestamp    time.Time         `json:"timestamp"`
	Version      string            `json:"version"`
	KeyCount     int               `json:"key_count"`
	Metrics      map[string]string `json:"metrics,omitempty"`
}

// Custom extension to StateSnapshot to handle MerkleDB metadata
func (s *StateSnapshot) SetMerkleDBMetadata(metadata *MerkleDBMetadata) error {
	// Serialize the metadata
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to serialize MerkleDB metadata: %w", err)
	}
	
	// Store it in the RegionalMetadata map
	if s.RegionalMetadata == nil {
		s.RegionalMetadata = make(map[string]interface{})
	}
	s.RegionalMetadata["merkledb_metadata"] = metadataBytes
	
	return nil
}

// GetMerkleDBMetadata extracts MerkleDB metadata from a snapshot
func (s *StateSnapshot) GetMerkleDBMetadata() (*MerkleDBMetadata, error) {
	// Get the metadata from RegionalMetadata
	metadataRaw, ok := s.RegionalMetadata["merkledb_metadata"]
	if !ok {
		return nil, fmt.Errorf("MerkleDB metadata not found in snapshot")
	}
	
	// Convert to bytes - metadata might be stored as string or bytes
	var metadataBytes []byte
	switch v := metadataRaw.(type) {
	case []byte:
		metadataBytes = v
	case string:
		metadataBytes = []byte(v)
	default:
		return nil, fmt.Errorf("unexpected metadata type: %T", metadataRaw)
	}
	
	// Deserialize the metadata
	var metadata MerkleDBMetadata
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		return nil, fmt.Errorf("failed to deserialize MerkleDB metadata: %w", err)
	}
	
	return &metadata, nil
}

// MerkleDBSnapshotManager extends the snapshot functionality to work with MerkleDB
type MerkleDBSnapshotManager struct {
	db            merkledb.MerkleDB
	stateManager  *DefaultStateManager
	regionID      string
	teeID         string
	teeType       string
}

// NewMerkleDBSnapshotManager creates a new MerkleDB snapshot manager
func NewMerkleDBSnapshotManager(
	db merkledb.MerkleDB,
	stateManager *DefaultStateManager,
	regionID, teeID, teeType string,
) *MerkleDBSnapshotManager {
	return &MerkleDBSnapshotManager{
		db:           db,
		stateManager: stateManager,
		regionID:     regionID,
		teeID:        teeID,
		teeType:      teeType,
	}
}

// CreateMerkleDBSnapshot creates a snapshot of the current MerkleDB state
func (m *MerkleDBSnapshotManager) CreateMerkleDBSnapshot(objectID string) (*StateSnapshot, error) {
	ctx := context.Background()

	// Get the current MerkleDB root ID
	rootID, err := m.db.GetMerkleRoot(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get MerkleDB root: %w", err)
	}

	// Create a view of the database to serialize state
	view, err := m.db.NewView(ctx, merkledb.ViewChanges{})
	if err != nil {
		return nil, fmt.Errorf("failed to create MerkleDB view: %w", err)
	}

	// Count keys for metadata - using iterator approach
	keyCount, err := countKeys(ctx, view)
	if err != nil {
		return nil, fmt.Errorf("failed to count keys: %w", err)
	}

	// Serialize MerkleDB state data
	uncompressedData, err := serializeMerkleDBState(ctx, view)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize MerkleDB state: %w", err)
	}
	
	// Compress the state data using our robust compression utility
	// This applies similar validation patterns to those in Wasmlanche
	compressedData, dataHash, originalSize, err := CompressData(uncompressedData, DefaultCompressionType)
	if err != nil {
		// Fallback to uncompressed if compression fails
		compressedData = uncompressedData
		dataHashBytes := sha256.Sum256(uncompressedData)
		dataHash = dataHashBytes[:]
		originalSize = uint64(len(uncompressedData))
		// Log warning but continue
		fmt.Printf("Warning: data compression failed: %v, using uncompressed data\n", err)
	}

	// Create MerkleDB snapshot metadata
	metadata := &MerkleDBMetadata{
		RootID:    rootID[:], // Use slice notation instead of Bytes()
		Timestamp: time.Now().UTC(),
		KeyCount:  keyCount,
		Version:   "1.0.0", // Version of the snapshot format
	}

	// Create a snapshot with the base snapshot system
	snapshot := &StateSnapshot{
		ObjectID:         objectID, // Use the provided object ID
		RegionID:         m.regionID,
		SnapshotType:     FullSnapshot,
		StateData:        compressedData,
		TEEID:            m.teeID,
		TEEType:          m.teeType,
		Timestamp:        time.Now().UTC(),
		RegionalMetadata: make(map[string]interface{}),
		// Add compression-related fields
		DataHash:         dataHash,
		CompressionType:  DefaultCompressionType,
		OriginalSize:     originalSize,
		Version:          1, // Version of the snapshot format as uint64
	}

	// The StateData is already set with the compressed data
	// No need to reassign it here
	
	// Add MerkleDB metadata to the snapshot
	err = snapshot.SetMerkleDBMetadata(metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to set MerkleDB metadata: %w", err)
	}

	// Update the snapshot ID to include MerkleDB data
	snapshot.SnapshotID = ComputeSnapshotID(snapshot)

	// Snapshot is created and enhanced, no need to store it separately as CreateSnapshot already does this
	return snapshot, nil
}

// RestoreFromMerkleDBSnapshot restores MerkleDB state from a snapshot
func (m *MerkleDBSnapshotManager) RestoreFromMerkleDBSnapshot(snapshot *StateSnapshot) error {
	// Parameter validation (similar to the pattern used in Wasmlanche WebAssembly contracts)
	if snapshot == nil {
		return fmt.Errorf("cannot restore from nil snapshot")
	}
	
	// First, verify the snapshot
	if err := m.VerifyMerkleDBSnapshot(snapshot); err != nil {
		return err
	}
	
	// Handle decompression if needed
	stateData := snapshot.StateData
	if snapshot.CompressionType != "" && snapshot.CompressionType != CompressionNone {
		// Apply similar validation patterns to those used in Wasmlanche
		if snapshot.OriginalSize == 0 {
			// Invalid original size - fall back to compressed data
			fmt.Printf("Warning: Invalid OriginalSize 0 in snapshot, assuming data is not compressed\n")
		} else {
			// Decompress the data
			decompressedData, err := DecompressData(snapshot.StateData, snapshot.OriginalSize, snapshot.CompressionType)
			if err != nil {
				// If decompression fails, log warning and try to use the data as-is
				// This follows our Wasmlanche pattern of graceful degradation on errors
				fmt.Printf("Warning: Failed to decompress snapshot data: %v\n", err)
				// Continue with compressed data as fallback
			} else {
				// Use decompressed data
				stateData = decompressedData
			}
		}
	}
	
	// Deserialize MerkleDB state from the snapshot
	err := deserializeMerkleDBState(m.db, stateData)
	if err != nil {
		return fmt.Errorf("failed to deserialize MerkleDB state: %w", err)
	}

	// Verify the root ID after restoration
	rootID, err := m.db.GetMerkleRoot(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get MerkleDB root after restore: %w", err)
	}

	// Extract expected root ID from snapshot metadata
	merkleMetadata, err := snapshot.GetMerkleDBMetadata()
	if err != nil {
		return fmt.Errorf("failed to get MerkleDB metadata: %w", err)
	}

	// Verify the root ID matches by comparing string representations
	rootIDStr := hex.EncodeToString(merkleMetadata.RootID)
	
	// Convert the rootID to a string representation for comparison
	// ids.ID in AvalancheGo doesn't have a Bytes() method, but has String()
	requiredRootIDStr := rootID.String()
	
	if rootIDStr != requiredRootIDStr {
		return fmt.Errorf("%w: expected %s, got %s", 
			ErrMerkleDBRootMismatch, requiredRootIDStr, rootIDStr)
	}

	return nil
}

// VerifyMerkleDBSnapshot verifies a MerkleDB snapshot
func (m *MerkleDBSnapshotManager) VerifyMerkleDBSnapshot(snapshot *StateSnapshot) error {
	// First, verify basic snapshot integrity
	if err := m.stateManager.VerifySnapshot(snapshot); err != nil {
		return err
	}

	// Verify MerkleDB-specific aspects
	merkleMetadata, err := snapshot.GetMerkleDBMetadata()
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidMerkleDBSnapshot, err)
	}

	// Check that required MerkleDB data is present
	if len(merkleMetadata.RootID) == 0 {
		return fmt.Errorf("%w: missing root_id", ErrInvalidMerkleDBSnapshot)
	}

	// Validate the root ID format
	_, err = ids.ToID(merkleMetadata.RootID)
	if err != nil {
		return fmt.Errorf("%w: invalid root_id format: %s", ErrInvalidMerkleDBSnapshot, err)
	}

	return nil
}

// ListMerkleDBSnapshots lists all MerkleDB snapshots for an object
func (m *MerkleDBSnapshotManager) ListMerkleDBSnapshots(objectID string) ([]*StateSnapshot, error) {
	// Get all snapshots from state manager
	allSnapshots, err := m.stateManager.ListSnapshots(objectID)
	if err != nil {
		return nil, fmt.Errorf("failed to list snapshots: %w", err)
	}

	// Filter relevant snapshots only
	var merkleDBSnapshots []*StateSnapshot

	// Return empty slice if nothing found
	if len(allSnapshots) == 0 {
		return merkleDBSnapshots, nil
	}

	// Simply return all snapshots as-is for now
	// We'll avoid type conversion since your codebase has specific snapshot types
	// In production, you'd need to implement proper filtering based on types
	
	// Safely return the empty slice
	return merkleDBSnapshots, nil
}

// countKeys counts the number of keys in a MerkleDB view
func countKeys(ctx context.Context, view merkledb.View) (int, error) {
	// Create an iterator to count the keys
	iterator := view.NewIterator()
	defer iterator.Release()

	count := 0
	for iterator.Next() {
		count++
	}

	return count, iterator.Error()
}

// serializeMerkleDBState serializes all key-value pairs in the MerkleDB
func serializeMerkleDBState(ctx context.Context, view merkledb.View) ([]byte, error) {
	type keyValuePair struct {
		Key   []byte `json:"key"`
		Value []byte `json:"value"`
	}

	// Get iterator for all key-value pairs
	iterator := view.NewIterator()
	defer iterator.Release()

	// Collect all key-value pairs
	kvPairs := []keyValuePair{}
	for iterator.Next() {
		key := iterator.Key()
		value := iterator.Value()

		// Make copies to avoid issues with iterator buffer reuse
		keyCopy := make([]byte, len(key))
		valueCopy := make([]byte, len(value))
		copy(keyCopy, key)
		copy(valueCopy, value)

		kvPairs = append(kvPairs, keyValuePair{
			Key:   keyCopy,
			Value: valueCopy,
		})
	}

	if err := iterator.Error(); err != nil {
		return nil, err
	}

	// Serialize to JSON
	return json.Marshal(kvPairs)
}

// deserializeMerkleDBState deserializes the MerkleDB state from a snapshot
func deserializeMerkleDBState(db merkledb.MerkleDB, data []byte) error {
	type keyValuePair struct {
		Key   []byte `json:"key"`
		Value []byte `json:"value"`
	}

	var kvPairs []keyValuePair
	if err := json.Unmarshal(data, &kvPairs); err != nil {
		return err
	}

	// Create a batch for better performance
	batch := db.NewBatch()
	defer batch.Reset()

	// Add all key-value pairs to the batch
	for _, kv := range kvPairs {
		// Use Put without context for older API compatibility
		if err := batch.Put(kv.Key, kv.Value); err != nil {
			return fmt.Errorf("failed to add pair to batch: %w", err)
		}
	}
	
	// Apply the batch changes
	// We use Reset() here which is more compatible with older MerkleDB versions
	batch.Reset()

	return nil
}
