// Package mesh provides a mesh network for TEE-to-TEE communication
package mesh

import (
	"fmt" // Add fmt import for formatting
	"time"

	"github.com/rhombus-tech/vm/tee/proto"
)

// InternalToProtoSnapshot converts our internal RegionalSnapshot to a protobuf-generated RegionalSnapshot
func InternalToProtoSnapshot(snapshot *RegionalSnapshot) *proto.RegionalSnapshot {
	if snapshot == nil {
		return nil
	}

	// Create proto snapshot with basic fields
	protoSnapshot := &proto.RegionalSnapshot{
		RegionId:   snapshot.RegionID,          // Map RegionID to RegionId
		SnapshotId: snapshot.SnapshotID,        // Map SnapshotID to SnapshotId
		Timestamp:  snapshot.Timestamp.UnixNano(), // Convert time.Time to int64 nanoseconds
	}

	// Convert TEE snapshot IDs if present
	if len(snapshot.TEESnapshotIDs) > 0 {
		protoSnapshot.TeeSnapshotIds = make([][]byte, len(snapshot.TEESnapshotIDs))
		copy(protoSnapshot.TeeSnapshotIds, snapshot.TEESnapshotIDs)
	}

	// Convert snapshot summary if present
	if snapshot.SnapshotSummary != nil {
		summary := &proto.SnapshotSummary{
			MerkleRoot:     snapshot.SnapshotSummary.MerkleRoot,
			ObjectCount:    int32(snapshot.SnapshotSummary.ObjectCount), // int in Go, int32 in proto
			TotalStateSize: snapshot.SnapshotSummary.TotalStateSize,
		}

		// Convert state root hashes if present
		if snapshot.SnapshotSummary.StateRootHashes != nil {
			summary.StateRootHashes = make(map[string][]byte)
			for k, v := range snapshot.SnapshotSummary.StateRootHashes {
				summary.StateRootHashes[k] = v
			}
		}

		// Convert metrics if present
		if snapshot.SnapshotSummary.RegionalMetrics != nil {
			summary.Metrics = make(map[string]float64)
			for k, v := range snapshot.SnapshotSummary.RegionalMetrics {
				summary.Metrics[k] = v
			}
		}

		protoSnapshot.Summary = summary
	}

	return protoSnapshot
}

// ProtoToInternalSnapshot converts a protobuf-generated RegionalSnapshot to an internal RegionalSnapshot
func ProtoToInternalSnapshot(protoSnapshot *proto.RegionalSnapshot) *RegionalSnapshot {
	if protoSnapshot == nil {
		return nil
	}

	// Create the internal snapshot with basic fields
	snapshot := &RegionalSnapshot{
		RegionID:       protoSnapshot.RegionId,     // Map RegionId to RegionID
		SnapshotID:     protoSnapshot.SnapshotId,   // Map SnapshotId to SnapshotID
		Timestamp:      time.Unix(0, protoSnapshot.Timestamp), // Convert int64 nanoseconds to time.Time
	}

	// Convert TEE snapshot IDs if present
	if len(protoSnapshot.TeeSnapshotIds) > 0 {
		snapshot.TEESnapshotIDs = protoSnapshot.TeeSnapshotIds
	}

	// Convert snapshot summary if present
	if protoSnapshot.Summary != nil {
		// Map state root hashes if present
		stateRootHashes := make(map[string][]byte)
		for k, v := range protoSnapshot.Summary.StateRootHashes {
			stateRootHashes[k] = v
		}

		// Map metrics if present
		regionalMetrics := make(map[string]float64)
		for k, v := range protoSnapshot.Summary.Metrics {
			regionalMetrics[k] = v
		}

		// Set the summary fields
		snapshot.SnapshotSummary = &SnapshotSummary{
			MerkleRoot:      protoSnapshot.Summary.MerkleRoot,
			StateRootHashes: stateRootHashes,
			RegionalMetrics: regionalMetrics,
			ObjectCount:     int(protoSnapshot.Summary.ObjectCount), // Convert int32 to int
			TotalStateSize:  protoSnapshot.Summary.TotalStateSize,
		}
	}

	return snapshot
}

// FromProtoRegionalSnapshot is an alias for ProtoToInternalSnapshot for backward compatibility
func FromProtoRegionalSnapshot(protoSnapshot *proto.RegionalSnapshot) *RegionalSnapshot {
	return ProtoToInternalSnapshot(protoSnapshot)
}

// ToProtoRegionInfo converts our internal RegionInfo to the proto-generated type
func ToProtoRegionInfo(info *RegionInfo) *proto.RegionInfo {
	if info == nil {
		return nil
	}
	
	return &proto.RegionInfo{
		RegionId:           info.RegionID,           // RegionID in Go, RegionId in proto 
		Endpoint:           info.Endpoint,
		Status:             info.Status,
		AdminCapabilities:  info.AdminCapabilities,
		LastContactTime:    info.LastContactTime.Unix(), // time.Time in Go, int64 in proto
		TeeCount:           int32(info.TEECount),       // TEECount in Go, TeeCount in proto
	}
}

// FromProtoRegionInfo converts a proto-generated RegionInfo to our internal type
func FromProtoRegionInfo(protoInfo *proto.RegionInfo) *RegionInfo {
	if protoInfo == nil {
		return nil
	}
	
	return &RegionInfo{
		RegionID:          protoInfo.RegionId,          // RegionId in proto, RegionID in Go
		Endpoint:          protoInfo.Endpoint,
		Status:            protoInfo.Status,
		AdminCapabilities: protoInfo.AdminCapabilities,
		LastContactTime:   time.Unix(protoInfo.LastContactTime, 0), // int64 in proto, time.Time in Go
		TEECount:          int(protoInfo.TeeCount),                // TeeCount in proto, TEECount in Go
	}
}

// ToProtoFederatedSnapshot converts our internal FederatedSnapshot to the proto-generated type
func ToProtoFederatedSnapshot(snapshot *FederatedSnapshot) *proto.FederatedSnapshot {
	if snapshot == nil {
		return nil
	}
	
	// Convert timestamp to Unix timestamp
	var timestamp int64
	if !snapshot.Timestamp.IsZero() {
		timestamp = snapshot.Timestamp.Unix()
	}
	
	// Create base proto federated snapshot with correct field mappings
	protoSnapshot := &proto.FederatedSnapshot{
		FederationId:        snapshot.FederationID,         // FederationID in Go, FederationId in proto
		SnapshotId:          snapshot.SnapshotID,           // SnapshotID in Go, SnapshotId in proto
		Timestamp:           timestamp,                     // time.Time in Go, int64 in proto
		GlobalStateRoot:     snapshot.GlobalStateRoot,
		CoordinatorSignature: snapshot.CoordinatorSignature,
	}
	
	// Convert regional snapshots map
	if snapshot.RegionalSnapshots != nil && len(snapshot.RegionalSnapshots) > 0 {
		regionalSnapshotsMap := make(map[string]*proto.RegionalSnapshot)
		for id, rs := range snapshot.RegionalSnapshots {
			regionalSnapshotsMap[id] = InternalToProtoSnapshot(rs)
		}
		protoSnapshot.RegionalSnapshots = regionalSnapshotsMap
	}
	
	// Convert consensus metadata if available
	if snapshot.ConsensusMetadata != nil && len(snapshot.ConsensusMetadata) > 0 {
		consensusMetadata := make(map[string][]byte)
		for k, v := range snapshot.ConsensusMetadata {
			// Handle possible interface{} to []byte conversion
			if byteVal, ok := v.([]byte); ok {
				consensusMetadata[k] = byteVal
			} else {
				// Log warning about type mismatch
				fmt.Printf("Warning: consensusMetadata value for key %s is not []byte\n", k)
			}
		}
		protoSnapshot.ConsensusMetadata = consensusMetadata
	}
	
	return protoSnapshot
}

// FromProtoFederatedSnapshot converts a proto-generated FederatedSnapshot to our internal type
func FromProtoFederatedSnapshot(protoSnapshot *proto.FederatedSnapshot) *FederatedSnapshot {
	if protoSnapshot == nil {
		return nil
	}
	
	// Convert timestamp to time.Time
	timestamp := time.Unix(protoSnapshot.Timestamp, 0)
	
	// Create base federated snapshot with correct field mappings
	snapshot := &FederatedSnapshot{
		FederationID:        protoSnapshot.FederationId,         // FederationId in proto, FederationID in Go
		SnapshotID:          protoSnapshot.SnapshotId,           // SnapshotId in proto, SnapshotID in Go
		Timestamp:           timestamp,                          // int64 in proto, time.Time in Go
		GlobalStateRoot:     protoSnapshot.GlobalStateRoot,
		CoordinatorSignature: protoSnapshot.CoordinatorSignature,
	}
	
	// Convert regional snapshots map
	if protoSnapshot.RegionalSnapshots != nil && len(protoSnapshot.RegionalSnapshots) > 0 {
		regionalSnapshots := make(map[string]*RegionalSnapshot)
		for id, rs := range protoSnapshot.RegionalSnapshots {
			regionalSnapshots[id] = FromProtoRegionalSnapshot(rs)
		}
		snapshot.RegionalSnapshots = regionalSnapshots
	}
	
	// Convert consensus metadata
	if protoSnapshot.ConsensusMetadata != nil && len(protoSnapshot.ConsensusMetadata) > 0 {
		// Need to convert from map[string][]byte to map[string]interface{}
		consensusMetadata := make(map[string]interface{})
		for k, v := range protoSnapshot.ConsensusMetadata {
			consensusMetadata[k] = v
		}
		snapshot.ConsensusMetadata = consensusMetadata
	}

	// Note: Metadata field is not present in the proto structure
	
	return snapshot
}

// formatMetric formats a metric value as a string with appropriate precision
func formatMetric(value float64) string {
	return fmt.Sprintf("%.2f", value)
}

// formatFloat formats a float value with appropriate precision based on magnitude
func formatFloat(value float64) string {
	return fmt.Sprintf("%.3f", value)
}
