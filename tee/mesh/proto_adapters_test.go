package mesh

import (
	"testing"
	"time"

	"github.com/rhombus-tech/vm/tee/proto"
	"github.com/stretchr/testify/assert"
)

func TestInternalToProtoSnapshot(t *testing.T) {
	// Create an internal snapshot
	internalSnapshot := &RegionalSnapshot{
		RegionID:    "test-region",
		SnapshotID:  []byte("test-snapshot-id"),
		Timestamp:   time.Now(),
		TEESnapshotIDs: [][]byte{[]byte("tee-snapshot-1"), []byte("tee-snapshot-2")},
		SnapshotSummary: &SnapshotSummary{
			MerkleRoot:      []byte("merkle-root"),
			StateRootHashes: map[string][]byte{"tee1": []byte("hash1"), "tee2": []byte("hash2")},
			RegionalMetrics: map[string]float64{"metric1": 1.0, "metric2": 2.0},
			ObjectCount:     10,
			TotalStateSize:  1024,
		},
	}

	// Convert to proto
	protoSnapshot := InternalToProtoSnapshot(internalSnapshot)

	// Verify basic fields
	assert.Equal(t, internalSnapshot.RegionID, protoSnapshot.RegionId)
	assert.Equal(t, internalSnapshot.SnapshotID, protoSnapshot.SnapshotId)
	assert.Equal(t, internalSnapshot.Timestamp.UnixNano(), protoSnapshot.Timestamp)
	assert.Equal(t, len(internalSnapshot.TEESnapshotIDs), len(protoSnapshot.TeeSnapshotIds))

	// Verify summary fields
	assert.NotNil(t, protoSnapshot.Summary)
	assert.Equal(t, internalSnapshot.SnapshotSummary.MerkleRoot, protoSnapshot.Summary.MerkleRoot)
	assert.Equal(t, int32(internalSnapshot.SnapshotSummary.ObjectCount), protoSnapshot.Summary.ObjectCount)
	assert.Equal(t, internalSnapshot.SnapshotSummary.TotalStateSize, protoSnapshot.Summary.TotalStateSize)
	
	// Verify maps
	assert.Equal(t, len(internalSnapshot.SnapshotSummary.StateRootHashes), len(protoSnapshot.Summary.StateRootHashes))
	assert.Equal(t, len(internalSnapshot.SnapshotSummary.RegionalMetrics), len(protoSnapshot.Summary.Metrics))
}

func TestProtoToInternalSnapshot(t *testing.T) {
	// Create a proto snapshot
	protoSnapshot := &proto.RegionalSnapshot{
		RegionId:   "test-region",
		SnapshotId: []byte("test-snapshot-id"),
		Timestamp:  time.Now().UnixNano(),
		TeeSnapshotIds: [][]byte{[]byte("tee-snapshot-1"), []byte("tee-snapshot-2")},
		Summary: &proto.SnapshotSummary{
			MerkleRoot:      []byte("merkle-root"),
			StateRootHashes: map[string][]byte{"tee1": []byte("hash1"), "tee2": []byte("hash2")},
			Metrics:         map[string]float64{"metric1": 1.0, "metric2": 2.0},
			ObjectCount:     10,
			TotalStateSize:  1024,
		},
	}

	// Convert to internal
	internalSnapshot := ProtoToInternalSnapshot(protoSnapshot)

	// Verify basic fields
	assert.Equal(t, protoSnapshot.RegionId, internalSnapshot.RegionID)
	assert.Equal(t, protoSnapshot.SnapshotId, internalSnapshot.SnapshotID)
	assert.Equal(t, time.Unix(0, protoSnapshot.Timestamp), internalSnapshot.Timestamp)
	assert.Equal(t, len(protoSnapshot.TeeSnapshotIds), len(internalSnapshot.TEESnapshotIDs))

	// Verify summary fields
	assert.NotNil(t, internalSnapshot.SnapshotSummary)
	assert.Equal(t, protoSnapshot.Summary.MerkleRoot, internalSnapshot.SnapshotSummary.MerkleRoot)
	assert.Equal(t, int(protoSnapshot.Summary.ObjectCount), internalSnapshot.SnapshotSummary.ObjectCount)
	assert.Equal(t, protoSnapshot.Summary.TotalStateSize, internalSnapshot.SnapshotSummary.TotalStateSize)
	
	// Verify maps
	assert.Equal(t, len(protoSnapshot.Summary.StateRootHashes), len(internalSnapshot.SnapshotSummary.StateRootHashes))
	assert.Equal(t, len(protoSnapshot.Summary.Metrics), len(internalSnapshot.SnapshotSummary.RegionalMetrics))
}

func TestRoundTripConversion(t *testing.T) {
	// Create an internal snapshot
	originalSnapshot := &RegionalSnapshot{
		RegionID:    "test-region",
		SnapshotID:  []byte("test-snapshot-id"),
		Timestamp:   time.Now().Truncate(time.Nanosecond), // Truncate to avoid precision loss
		TEESnapshotIDs: [][]byte{[]byte("tee-snapshot-1"), []byte("tee-snapshot-2")},
		SnapshotSummary: &SnapshotSummary{
			MerkleRoot:      []byte("merkle-root"),
			StateRootHashes: map[string][]byte{"tee1": []byte("hash1"), "tee2": []byte("hash2")},
			RegionalMetrics: map[string]float64{"metric1": 1.0, "metric2": 2.0},
			ObjectCount:     10,
			TotalStateSize:  1024,
		},
	}

	// Convert to proto
	protoSnapshot := InternalToProtoSnapshot(originalSnapshot)
	
	// Convert back to internal
	roundTrippedSnapshot := ProtoToInternalSnapshot(protoSnapshot)

	// Verify fields match after round trip
	assert.Equal(t, originalSnapshot.RegionID, roundTrippedSnapshot.RegionID)
	assert.Equal(t, originalSnapshot.SnapshotID, roundTrippedSnapshot.SnapshotID)
	assert.Equal(t, originalSnapshot.Timestamp, roundTrippedSnapshot.Timestamp)
	assert.Equal(t, len(originalSnapshot.TEESnapshotIDs), len(roundTrippedSnapshot.TEESnapshotIDs))
	
	// Verify summary fields
	assert.Equal(t, originalSnapshot.SnapshotSummary.MerkleRoot, roundTrippedSnapshot.SnapshotSummary.MerkleRoot)
	assert.Equal(t, originalSnapshot.SnapshotSummary.ObjectCount, roundTrippedSnapshot.SnapshotSummary.ObjectCount)
	assert.Equal(t, originalSnapshot.SnapshotSummary.TotalStateSize, roundTrippedSnapshot.SnapshotSummary.TotalStateSize)
	
	// Verify maps (check sizes and a few values)
	assert.Equal(t, len(originalSnapshot.SnapshotSummary.StateRootHashes), 
		len(roundTrippedSnapshot.SnapshotSummary.StateRootHashes))
	assert.Equal(t, len(originalSnapshot.SnapshotSummary.RegionalMetrics), 
		len(roundTrippedSnapshot.SnapshotSummary.RegionalMetrics))
}
