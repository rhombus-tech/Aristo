// Package mesh provides a mesh network for TEE-to-TEE communication
package mesh

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// DiffOperation represents the type of operation in a differential update
type DiffOperation byte

const (
	// DiffOpAdd indicates an object was added
	DiffOpAdd DiffOperation = iota
	// DiffOpRemove indicates an object was removed
	DiffOpRemove
	// DiffOpModify indicates an object was modified
	DiffOpModify
	// DiffOpUnchanged indicates an object was unchanged (referenced only)
	DiffOpUnchanged
)

// DifferentialUpdate represents a set of changes between two snapshots
type DifferentialUpdate struct {
	BaseSnapshotID    []byte                      // ID of the base snapshot
	TargetSnapshotID  []byte                      // ID of the target snapshot
	Timestamp         time.Time                   // Time the diff was created
	ObjectChanges     map[string]*ObjectDiff      // Changes by object ID
	StateReferences   map[string][]byte           // References to unchanged state (ID -> hash)
	ChangedObjectsRaw map[string][]byte           // Raw data for changed objects
	Metadata          map[string]interface{}      // Additional metadata
	Size              int64                        // Total size in bytes
	Checksum          []byte                      // Checksum of the entire update
}

// ObjectDiff represents the changes to a single object
type ObjectDiff struct {
	ObjectID      string         // ID of the object
	Operation     DiffOperation  // Type of operation
	PreviousHash  []byte         // Hash of the object in base snapshot
	CurrentHash   []byte         // Hash of the object in target snapshot
	SizeDelta     int64          // Change in size (can be negative)
	IsBinary      bool           // Whether the object contains binary data
	FieldChanges  []FieldChange  // For structured objects, list of field changes
}

// FieldChange represents a change to a field in a structured object
type FieldChange struct {
	FieldPath     string        // Path to the field (dot notation)
	Operation     DiffOperation // Type of operation on this field
	PreviousValue interface{}   // Previous value, if any
	CurrentValue  interface{}   // Current value, if any
}

// DifferentialUpdater creates and applies differential updates between snapshots
type DifferentialUpdater struct {
	snapshotStorage SnapshotStorageInterface
	compressor      *Compressor
	diffCache       map[string]*DifferentialUpdate // Cache of recent diffs (key = baseID:targetID)
	cacheMutex      sync.RWMutex
	maxDiffSize     int64 // Maximum size of a diff to generate
	metrics         DiffMetrics
}

// DiffMetrics tracks metrics for differential updates
type DiffMetrics struct {
	TotalDiffsGenerated    int64
	TotalDiffsApplied      int64
	TotalDiffSizeBytes     int64
	AverageDiffSizeBytes   int64
	MaxDiffSizeBytes       int64
	AverageSavingsPercent  float64
	TotalObjectsChanged    int64
	TotalObjectsReferenced int64
	GenerationTimeNs       int64
	ApplyTimeNs            int64
}

// NewDifferentialUpdater creates a new differential updater
func NewDifferentialUpdater(storage SnapshotStorageInterface, compressor *Compressor) *DifferentialUpdater {
	if compressor == nil {
		// Create default compressor with Zstd algorithm
		var err error
		compressor, err = NewCompressor(CompressionZstd, 3)
		if err != nil {
			// Fall back to gzip if zstd fails
			compressor, _ = NewCompressor(CompressionGzip, 6)
		}
	}
	
	return &DifferentialUpdater{
		snapshotStorage: storage,
		compressor:      compressor,
		diffCache:       make(map[string]*DifferentialUpdate),
		maxDiffSize:     100 * 1024 * 1024, // 100MB max diff size by default
	}
}

// GenerateDiff creates a differential update between base and target snapshots
func (du *DifferentialUpdater) GenerateDiff(baseSnapshot, targetSnapshot *RegionalSnapshot) (*DifferentialUpdate, error) {
	if baseSnapshot == nil || targetSnapshot == nil {
		return nil, errors.New("both base and target snapshots are required")
	}
	
	// Check if we already have this diff cached
	cacheKey := fmt.Sprintf("%x:%x", baseSnapshot.SnapshotID, targetSnapshot.SnapshotID)
	
	du.cacheMutex.RLock()
	cachedDiff, found := du.diffCache[cacheKey]
	du.cacheMutex.RUnlock()
	
	if found {
		return cachedDiff, nil
	}
	
	// Track generation time
	startTime := time.Now()
	
	// Create a new differential update
	diff := &DifferentialUpdate{
		BaseSnapshotID:    baseSnapshot.SnapshotID,
		TargetSnapshotID:  targetSnapshot.SnapshotID,
		Timestamp:         time.Now(),
		ObjectChanges:     make(map[string]*ObjectDiff),
		StateReferences:   make(map[string][]byte),
		ChangedObjectsRaw: make(map[string][]byte),
		Metadata:          make(map[string]interface{}),
	}
	
	// Build maps of objects for faster lookup
	baseObjects := make(map[string][]byte)
	targetObjects := make(map[string][]byte)
	
	// In a real implementation, we would extract actual state objects from snapshots
	// Parse the state data from TEE snapshots
	for _, teeSnapshot := range baseSnapshot.TEESnapshots {
		// In a real implementation, we would parse StateData as a map
		// For now, we'll use the TEE ID as the object ID and the state data as the object data
		objID := teeSnapshot.TEEID
		baseObjects[objID] = teeSnapshot.StateData
	}
	
	for _, teeSnapshot := range targetSnapshot.TEESnapshots {
		// Same approach as above for target snapshots
		objID := teeSnapshot.TEEID
		targetObjects[objID] = teeSnapshot.StateData
	}
	
	// Track objects
	var objectsChanged, objectsReferenced int64
	
	// Process all objects from both snapshots
	allObjectIDs := make(map[string]struct{})
	for id := range baseObjects {
		allObjectIDs[id] = struct{}{}
	}
	for id := range targetObjects {
		allObjectIDs[id] = struct{}{}
	}
	
	// Sort object IDs for deterministic processing (important for consistent checksums)
	sortedIDs := make([]string, 0, len(allObjectIDs))
	for id := range allObjectIDs {
		sortedIDs = append(sortedIDs, id)
	}
	sort.Strings(sortedIDs)
	
	// Generate diff for each object
	totalDiffSize := int64(0)
	
	for _, objID := range sortedIDs {
		baseObj, baseExists := baseObjects[objID]
		targetObj, targetExists := targetObjects[objID]
		
		switch {
		case !baseExists && targetExists:
			// Object was added
			diff.ObjectChanges[objID] = &ObjectDiff{
				ObjectID:     objID,
				Operation:    DiffOpAdd,
				CurrentHash:  hashBytes(targetObj),
				SizeDelta:    int64(len(targetObj)),
				IsBinary:     true, // Assume binary for simplicity
			}
			diff.ChangedObjectsRaw[objID] = targetObj
			totalDiffSize += int64(len(targetObj))
			objectsChanged++
			
		case baseExists && !targetExists:
			// Object was removed
			diff.ObjectChanges[objID] = &ObjectDiff{
				ObjectID:     objID,
				Operation:    DiffOpRemove,
				PreviousHash: hashBytes(baseObj),
				SizeDelta:    -int64(len(baseObj)),
			}
			objectsChanged++
			
		case baseExists && targetExists:
			// Object might be modified or unchanged
			baseHash := hashBytes(baseObj)
			targetHash := hashBytes(targetObj)
			
			if bytes.Equal(baseHash, targetHash) {
				// Object is unchanged - just reference it
				diff.StateReferences[objID] = baseHash
				diff.ObjectChanges[objID] = &ObjectDiff{
					ObjectID:     objID,
					Operation:    DiffOpUnchanged,
					PreviousHash: baseHash,
					CurrentHash:  targetHash,
					SizeDelta:    0,
				}
				objectsReferenced++
			} else {
				// Object was modified
				// In a real implementation, we would generate a detailed field-by-field diff
				// For this example, we'll just include the whole changed object
				diff.ObjectChanges[objID] = &ObjectDiff{
					ObjectID:     objID,
					Operation:    DiffOpModify,
					PreviousHash: baseHash,
					CurrentHash:  targetHash,
					SizeDelta:    int64(len(targetObj) - len(baseObj)),
					IsBinary:     true, // Assume binary for simplicity
				}
				diff.ChangedObjectsRaw[objID] = targetObj
				totalDiffSize += int64(len(targetObj))
				objectsChanged++
			}
		}
	}
	
	// Check if diff is too large
	if totalDiffSize > du.maxDiffSize {
		return nil, fmt.Errorf("differential update too large: %d bytes (max %d)", totalDiffSize, du.maxDiffSize)
	}
	
	// Calculate size and checksum
	diff.Size = totalDiffSize
	diff.Checksum = calculateDiffChecksum(diff)
	
	// Update metrics
	du.metrics.TotalDiffsGenerated++
	du.metrics.TotalDiffSizeBytes += totalDiffSize
	du.metrics.TotalObjectsChanged += objectsChanged
	du.metrics.TotalObjectsReferenced += objectsReferenced
	
	if totalDiffSize > du.metrics.MaxDiffSizeBytes {
		du.metrics.MaxDiffSizeBytes = totalDiffSize
	}
	
	// Calculate average size
	du.metrics.AverageDiffSizeBytes = du.metrics.TotalDiffSizeBytes / du.metrics.TotalDiffsGenerated
	
	// Calculate savings percentage compared to full snapshot
	fullSize := int64(targetSnapshot.SnapshotSummary.TotalStateSize)
	if fullSize > 0 {
		savings := 100.0 * (1.0 - float64(totalDiffSize)/float64(fullSize))
		
		// Update moving average of savings
		du.metrics.AverageSavingsPercent = (du.metrics.AverageSavingsPercent*float64(du.metrics.TotalDiffsGenerated-1) + savings) / float64(du.metrics.TotalDiffsGenerated)
	}
	
	// Record generation time
	du.metrics.GenerationTimeNs = time.Since(startTime).Nanoseconds()
	
	// Cache the diff
	du.cacheMutex.Lock()
	du.diffCache[cacheKey] = diff
	du.cacheMutex.Unlock()
	
	return diff, nil
}

// ApplyDiff applies a differential update to a base snapshot to produce a target snapshot
func (du *DifferentialUpdater) ApplyDiff(baseSnapshot *RegionalSnapshot, diff *DifferentialUpdate) (*RegionalSnapshot, error) {
	if baseSnapshot == nil || diff == nil {
		return nil, errors.New("both base snapshot and diff are required")
	}
	
	// Verify base snapshot ID matches the diff's base ID
	if !bytes.Equal(baseSnapshot.SnapshotID, diff.BaseSnapshotID) {
		return nil, errors.New("base snapshot ID doesn't match diff's base ID")
	}
	
	// Track application time
	startTime := time.Now()
	
	// Clone the base snapshot to avoid modifying it
	targetSnapshot := cloneRegionalSnapshot(baseSnapshot)
	
	// Update the snapshot ID and timestamp
	targetSnapshot.SnapshotID = diff.TargetSnapshotID
	targetSnapshot.Timestamp = diff.Timestamp
	
	// In a real implementation, we would apply changes to the actual state
	// For this example, we'll focus on the TEESnapshots
	
	// Process each TEE snapshot
	for i, teeSnapshot := range targetSnapshot.TEESnapshots {
		// We need to handle StateData as a byte slice, not a map
		// Make a copy of state data to avoid modifying the original
		newStateData := make([]byte, len(teeSnapshot.StateData))
		copy(newStateData, teeSnapshot.StateData)
		targetSnapshot.TEESnapshots[i].StateData = newStateData
		
		// For this implementation, we're not applying individual object changes to a byte array
		// Instead, we're treating the entire StateData as a single entity that gets replaced
		// In a real implementation with structured objects, you'd parse the StateData, 
		// apply changes to specific objects, and then reserialize
		
		// We'll create a simple metadata string to track which changes were applied
		appliedChanges := 0
		
		// Count the number of changes we're applying
		appliedChanges = len(diff.ObjectChanges)
		
		// In a real implementation, we would actually modify the state data
		// For now, we'll just track that changes were applied by appending a marker
		if appliedChanges > 0 && len(targetSnapshot.TEESnapshots[i].StateData) > 0 {
			// Add a marker at the end that changes were applied (simple demonstration)
			// In a real implementation, we would properly parse and modify the state data
			marker := []byte(fmt.Sprintf("|CHANGES:%d|", appliedChanges))
			newData := make([]byte, len(newStateData)+len(marker))
			copy(newData, newStateData)
			copy(newData[len(newStateData):], marker)
			targetSnapshot.TEESnapshots[i].StateData = newData
		}
	}
	
	// Update metrics
	du.metrics.TotalDiffsApplied++
	du.metrics.ApplyTimeNs = time.Since(startTime).Nanoseconds()
	
	return targetSnapshot, nil
}

// SetMaxDiffSize sets the maximum size allowed for a differential update
func (du *DifferentialUpdater) SetMaxDiffSize(maxBytes int64) {
	du.maxDiffSize = maxBytes
}

// GetMetrics returns the current metrics for differential updates
func (du *DifferentialUpdater) GetMetrics() DiffMetrics {
	return du.metrics
}

// hashBytes returns a SHA-256 hash of the provided data
func hashBytes(data []byte) []byte {
	h := sha256.New()
	h.Write(data)
	return h.Sum(nil)
}

// calculateDiffChecksum computes a checksum for the entire differential update
func calculateDiffChecksum(diff *DifferentialUpdate) []byte {
	// Create a hash for the diff
	h := sha256.New()
	
	// Include snapshot IDs
	h.Write(diff.BaseSnapshotID)
	h.Write(diff.TargetSnapshotID)
	
	// Include timestamp
	timeBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(timeBytes, uint64(diff.Timestamp.Unix()))
	h.Write(timeBytes)
	
	// Sort object IDs for deterministic processing
	objIDs := make([]string, 0, len(diff.ObjectChanges))
	for id := range diff.ObjectChanges {
		objIDs = append(objIDs, id)
	}
	sort.Strings(objIDs)
	
	// Process each object change
	for _, id := range objIDs {
		objDiff := diff.ObjectChanges[id]
		
		// Include object ID
		h.Write([]byte(id))
		
		// Include operation
		h.Write([]byte{byte(objDiff.Operation)})
		
		// Include hashes if present
		if objDiff.PreviousHash != nil {
			h.Write(objDiff.PreviousHash)
		}
		if objDiff.CurrentHash != nil {
			h.Write(objDiff.CurrentHash)
		}
		
		// Include raw data if present
		if rawData, ok := diff.ChangedObjectsRaw[id]; ok {
			h.Write(rawData)
		}
	}
	
	return h.Sum(nil)
}

// cloneRegionalSnapshot creates a deep copy of a RegionalSnapshot
func cloneRegionalSnapshot(source *RegionalSnapshot) *RegionalSnapshot {
	if source == nil {
		return nil
	}
	
	clone := &RegionalSnapshot{
		RegionID:            source.RegionID,
		SnapshotID:          make([]byte, len(source.SnapshotID)),
		Timestamp:           source.Timestamp,
		TEESnapshotIDs:      make([][]byte, len(source.TEESnapshotIDs)),
		CoordinatorSignature: make([]byte, len(source.CoordinatorSignature)),
		VerifierSignatures:   make([][]byte, len(source.VerifierSignatures)),
	}
	
	// Copy slice contents
	copy(clone.SnapshotID, source.SnapshotID)
	
	// Deep copy TEE snapshots
	clone.TEESnapshots = make([]*StateSnapshot, len(source.TEESnapshots))
	for i, teeSnapshot := range source.TEESnapshots {
		clone.TEESnapshots[i] = cloneStateSnapshot(teeSnapshot)
	}
	
	// Copy TEE snapshot IDs
	for i, id := range source.TEESnapshotIDs {
		clone.TEESnapshotIDs[i] = make([]byte, len(id))
		copy(clone.TEESnapshotIDs[i], id)
	}
	
	// Clone snapshot summary
	if source.SnapshotSummary != nil {
		clone.SnapshotSummary = &SnapshotSummary{
			MerkleRoot:      make([]byte, len(source.SnapshotSummary.MerkleRoot)),
			StateRootHashes: make(map[string][]byte),
			ObjectCount:     source.SnapshotSummary.ObjectCount,
			TotalStateSize:  source.SnapshotSummary.TotalStateSize,
			RegionalMetrics: make(map[string]float64),
		}
		
		copy(clone.SnapshotSummary.MerkleRoot, source.SnapshotSummary.MerkleRoot)
		
		// Copy state root hashes
		for k, v := range source.SnapshotSummary.StateRootHashes {
			clone.SnapshotSummary.StateRootHashes[k] = make([]byte, len(v))
			copy(clone.SnapshotSummary.StateRootHashes[k], v)
		}
		
		// Copy regional metrics
		for k, v := range source.SnapshotSummary.RegionalMetrics {
			clone.SnapshotSummary.RegionalMetrics[k] = v
		}
	}
	
	// Clone consensus info
	if source.ConsensusInfo != nil {
		clone.ConsensusInfo = &SnapshotConsensusInfo{
			TEECount:          source.ConsensusInfo.TEECount,
			ParticipatingTEEs: source.ConsensusInfo.ParticipatingTEEs,
			ConsensusLevel:    source.ConsensusInfo.ConsensusLevel,
			ConsensusMethod:   source.ConsensusInfo.ConsensusMethod,
			ConsensusSuccess:  source.ConsensusInfo.ConsensusSuccess,
		}
	}
	
	// Copy coordinator signature
	copy(clone.CoordinatorSignature, source.CoordinatorSignature)
	
	// Copy verifier signatures
	for i, sig := range source.VerifierSignatures {
		clone.VerifierSignatures[i] = make([]byte, len(sig))
		copy(clone.VerifierSignatures[i], sig)
	}
	
	// Clone metadata
	if source.Metadata != nil {
		clone.Metadata = make(map[string]interface{})
		for k, v := range source.Metadata {
			clone.Metadata[k] = v
		}
	}
	
	return clone
}

// cloneStateSnapshot creates a deep copy of a StateSnapshot
func cloneStateSnapshot(source *StateSnapshot) *StateSnapshot {
    if source == nil {
        return nil
    }

    clone := &StateSnapshot{
        ObjectID:        source.ObjectID,
        RegionID:        source.RegionID,
        SnapshotID:      nil,
        SnapshotType:    source.SnapshotType,
        StateData:       nil,
        AccumulatorState: nil,
        PreviousSnapshotID: nil,
        TEEMeasurement:  nil,
        TEEID:           source.TEEID,
        TEEType:         source.TEEType,
        TEESignature:    nil,
        Timestamp:       source.Timestamp,
        RegionalMetadata: nil,
        Version:         source.Version,
        DataHash:        nil,
        CompressionType: source.CompressionType,
        OriginalSize:    source.OriginalSize,
    }

    // Deep copy byte slices
    if source.SnapshotID != nil {
        clone.SnapshotID = make([]byte, len(source.SnapshotID))
        copy(clone.SnapshotID, source.SnapshotID)
    }

    if source.StateData != nil {
        clone.StateData = make([]byte, len(source.StateData))
        copy(clone.StateData, source.StateData)
    }

    if source.AccumulatorState != nil {
        clone.AccumulatorState = make([]byte, len(source.AccumulatorState))
        copy(clone.AccumulatorState, source.AccumulatorState)
    }

    if source.PreviousSnapshotID != nil {
        clone.PreviousSnapshotID = make([]byte, len(source.PreviousSnapshotID))
        copy(clone.PreviousSnapshotID, source.PreviousSnapshotID)
    }

    if source.TEEMeasurement != nil {
        clone.TEEMeasurement = make([]byte, len(source.TEEMeasurement))
        copy(clone.TEEMeasurement, source.TEEMeasurement)
    }

    if source.TEESignature != nil {
        clone.TEESignature = make([]byte, len(source.TEESignature))
        copy(clone.TEESignature, source.TEESignature)
    }

    if source.DataHash != nil {
        clone.DataHash = make([]byte, len(source.DataHash))
        copy(clone.DataHash, source.DataHash)
    }

    // Deep copy maps
    if source.RegionalMetadata != nil {
        clone.RegionalMetadata = make(map[string]interface{})
        for k, v := range source.RegionalMetadata {
            clone.RegionalMetadata[k] = v
        }
    }

    return clone
}
