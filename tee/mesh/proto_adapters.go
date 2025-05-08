// Package mesh provides a mesh network for TEE-to-TEE communication
package mesh

import (
	"fmt"
	"time"

	"github.com/rhombus-tech/vm/tee/proto"
)

// Type aliases to avoid conflict with protobuf-generated types
// Use these type definitions in the codebase to avoid redeclaration errors
type (
	// ProtoRegionInfo is an alias for the protobuf-generated RegionInfo
	ProtoRegionInfo = proto.RegionInfo
	
	// ProtoFederatedSnapshot is an alias for the protobuf-generated FederatedSnapshot
	ProtoFederatedSnapshot = proto.FederatedSnapshot
	
	// ProtoRegionalSnapshot is an alias for the protobuf-generated RegionalSnapshot
	ProtoRegionalSnapshot = proto.RegionalSnapshot
	
	// ProtoSnapshotSummary is an alias for the protobuf-generated SnapshotSummary
	ProtoSnapshotSummary = proto.SnapshotSummary
	
	// ProtoConsensusInfo is an alias for the protobuf-generated ConsensusInfo, if it exists
	// If ConsensusInfo is not defined in proto, this will need to be commented out
	// ProtoConsensusInfo = proto.ConsensusInfo
)

// ProtoAdapters provides compatibility functions between our mesh types and the generated protobuf types.
// This avoids having to refactor the entire codebase when protobuf definitions change.

// ToProtoRegionalSnapshot converts our internal RegionalSnapshot to the proto-generated type
func ToProtoRegionalSnapshot(snapshot *RegionalSnapshot) *proto.RegionalSnapshot {
	if snapshot == nil {
		return nil
	}

	protoSnapshot := &proto.RegionalSnapshot{
		RegionId:             snapshot.RegionID,
		SnapshotId:           snapshot.SnapshotID,
		Timestamp:            snapshot.Timestamp.Unix(),
		TeeSnapshotIds:       snapshot.TEESnapshotIDs,
		CoordinatorSignature: snapshot.CoordinatorSignature,
		VerifierSignatures:   snapshot.VerifierSignatures,
		Metadata:             snapshot.Metadata,
	}

	// Handle optional fields
	if snapshot.SnapshotSummary != nil {
		protoSnapshot.Summary = &proto.SnapshotSummary{
			MerkleRoot:      snapshot.SnapshotSummary.MerkleRoot,
			ObjectCount:     int32(snapshot.SnapshotSummary.ObjectCount),
			TotalStateSize:  snapshot.SnapshotSummary.TotalStateSize,
		}
		
		// Handle state root hashes if present
		if snapshot.SnapshotSummary.StateRootHashes != nil {
			protoSnapshot.Summary.StateRootHashes = snapshot.SnapshotSummary.StateRootHashes
		}
		
		// Handle metrics if present
		if snapshot.SnapshotSummary.RegionalMetrics != nil {
			metrics := make(map[string]float64)
			for k, v := range snapshot.SnapshotSummary.RegionalMetrics {
				metrics[k] = float64(v)
			}
			protoSnapshot.Summary.Metrics = metrics
		}
	}

	return protoSnapshot
}

// FromProtoRegionalSnapshot converts a proto-generated RegionalSnapshot to our internal type
func FromProtoRegionalSnapshot(protoSnapshot *proto.RegionalSnapshot) *RegionalSnapshot {
	if protoSnapshot == nil {
		return nil
	}

	snapshot := &RegionalSnapshot{
		RegionID:             protoSnapshot.RegionId,
		SnapshotID:           protoSnapshot.SnapshotId,
		Timestamp:            time.Unix(protoSnapshot.Timestamp, 0),
		TEESnapshotIDs:       protoSnapshot.TeeSnapshotIds,
		CoordinatorSignature: protoSnapshot.CoordinatorSignature,
		VerifierSignatures:   protoSnapshot.VerifierSignatures,
		Metadata:             protoSnapshot.Metadata,
	}

	// Handle optional fields
	if protoSnapshot.Summary != nil {
		snapshot.SnapshotSummary = &SnapshotSummary{
			MerkleRoot:       protoSnapshot.Summary.MerkleRoot,
			ObjectCount:      int(protoSnapshot.Summary.ObjectCount),
			TotalStateSize:   protoSnapshot.Summary.TotalStateSize,
			StateRootHashes:  protoSnapshot.Summary.StateRootHashes,
		}
		
		// Handle metrics if present
		if protoSnapshot.Summary.Metrics != nil {
			regionalMetrics := make(map[string]string)
			for k, v := range protoSnapshot.Summary.Metrics {
				regionalMetrics[k] = formatMetric(v)
			}
			snapshot.SnapshotSummary.RegionalMetrics = regionalMetrics
		}
	}

	return snapshot
}

// ToProtoRegionInfo converts our internal RegionInfo to the proto-generated type
func ToProtoRegionInfo(info *RegionInfo) *proto.RegionInfo {
	if info == nil {
		return nil
	}

	return &proto.RegionInfo{
		RegionId:           info.RegionID,
		Endpoint:           info.Endpoint,
		Status:             info.Status,
		AdminCapabilities:  info.AdminCapabilities,
		LastContactTime:    info.LastContactTime.Unix(),
		TeeCount:           int32(info.TEECount),
		Capabilities:       info.Capabilities,
	}
}

// FromProtoRegionInfo converts a proto-generated RegionInfo to our internal type
func FromProtoRegionInfo(protoInfo *proto.RegionInfo) *RegionInfo {
	if protoInfo == nil {
		return nil
	}

	return &RegionInfo{
		RegionID:          protoInfo.RegionId,
		Endpoint:          protoInfo.Endpoint,
		Status:            protoInfo.Status,
		AdminCapabilities: protoInfo.AdminCapabilities,
		LastContactTime:   time.Unix(protoInfo.LastContactTime, 0),
		TEECount:          int(protoInfo.TeeCount),
		Capabilities:      protoInfo.Capabilities,
	}
}

// ToProtoFederatedSnapshot converts our internal FederatedSnapshot to the proto-generated type
func ToProtoFederatedSnapshot(snapshot *FederatedSnapshot) *proto.FederatedSnapshot {
	if snapshot == nil {
		return nil
	}

	protoSnapshot := &proto.FederatedSnapshot{
		FederationId:        snapshot.FederationID,
		SnapshotId:          snapshot.SnapshotID,
		Timestamp:           snapshot.Timestamp.Unix(),
		GlobalStateRoot:     snapshot.GlobalStateRoot,
		CoordinatorSignature: snapshot.CoordinatorSignature,
	}

	// Convert the regional snapshots
	if snapshot.RegionalSnapshots != nil {
		regionalSnapshots := make(map[string]*proto.RegionalSnapshot)
		for k, v := range snapshot.RegionalSnapshots {
			regionalSnapshots[k] = ToProtoRegionalSnapshot(v)
		}
		protoSnapshot.RegionalSnapshots = regionalSnapshots
	}

	// Handle consensus metadata if present
	if snapshot.ConsensusMetadata != nil {
		consensusMetadata := make(map[string][]byte)
		for k, v := range snapshot.ConsensusMetadata {
			consensusMetadata[k] = v
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

	snapshot := &FederatedSnapshot{
		FederationID:         protoSnapshot.FederationId,
		SnapshotID:           protoSnapshot.SnapshotId,
		Timestamp:            time.Unix(protoSnapshot.Timestamp, 0),
		GlobalStateRoot:      protoSnapshot.GlobalStateRoot,
		CoordinatorSignature: protoSnapshot.CoordinatorSignature,
	}

	// Convert the regional snapshots
	if protoSnapshot.RegionalSnapshots != nil {
		regionalSnapshots := make(map[string]*RegionalSnapshot)
		for k, v := range protoSnapshot.RegionalSnapshots {
			regionalSnapshots[k] = FromProtoRegionalSnapshot(v)
		}
		snapshot.RegionalSnapshots = regionalSnapshots
	}

	// Handle consensus metadata if present
	if protoSnapshot.ConsensusMetadata != nil {
		consensusMetadata := make(map[string][]byte)
		for k, v := range protoSnapshot.ConsensusMetadata {
			consensusMetadata[k] = v
		}
		snapshot.ConsensusMetadata = consensusMetadata
	}

	return snapshot
}

// Helper function to format metrics as strings
func formatMetric(value float64) string {
	return formatFloat(value)
}

// Helper function to format float values with appropriate precision
func formatFloat(value float64) string {
	return fmt.Sprintf("%.6f", value)
}
