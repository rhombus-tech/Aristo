// Package mesh provides a mesh network for TEE-to-TEE communication
package mesh

import (
	"context"
	"fmt"
	"time"

	"github.com/rhombus-tech/vm/tee/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// FederationServer implements the gRPC service for cross-region federation

type FederationServer struct {
	proto.UnimplementedRegionFederationServer
	coordinator *FederationCoordinator
	regionID    string
}

type RegionDiscoveryResponse struct {
	Success      bool
	ErrorMessage string
	Regions      []*RegionInfo
	Signature    []byte
}

// Using proto.RegionInfo directly instead of redefining it

type RegionSyncRequest struct {
	FederationId   string
	SourceRegionId string
	ObjectId       string
	CurrentState   []byte
	StateVersion   int64
	SyncMode       proto.SyncMode
	DeltaUpdate    []byte
	Signature      []byte
}

type RegionSyncResponse struct {
	Success      bool
	ErrorMessage string
	UpdatedState []byte
	StateVersion int64
	HasConflict  bool
	Signature    []byte
}

type StateChangeProposal struct {
	FederationId   string
	SourceRegionId string
	ObjectId       string
	NewState       []byte
	StateVersion   int64
	ProposalId     int64
	ExpirationTime int64
	Signature      []byte
}

type StateChangeResponse struct {
	Accepted     bool
	ErrorMessage string
	StateVersion int64
	Signature    []byte
}

type FederatedSnapshotRequest struct {
	FederationId   string
	SourceRegionId string
	TargetRegions  []string
	RequestId      int64
	TimeoutMs      int64
	Signature      []byte
}

// Using proto.SnapshotSummary directly instead of redefining it

// Using proto.RegionalSnapshot directly instead of redefining it

// Using proto.FederatedSnapshot directly instead of redefining it

type FederatedSnapshotResponse struct {
	Success      bool
	ErrorMessage string
	Snapshot     *FederatedSnapshot
	Signature    []byte
}

type VerifyFederatedSnapshotRequest struct {
	FederationId   string
	SourceRegionId string
	SnapshotId     []byte
	Signature      []byte
}

type VerifyFederatedSnapshotResponse struct {
	Valid           bool
	ErrorMessage    string
	RegionsVerified int32
	Signature       []byte
}

type RegionStateQuery struct {
	FederationId   string
	SourceRegionId string
	ObjectId       string
	IncludeState   bool
	Signature      []byte
}

type RegionStateResponse struct {
	Exists        bool
	ErrorMessage  string
	StateVersion  int64
	LastModified  int64
	StateData     []byte
	Signature     []byte
}

type ConsensusRequest struct {
	FederationId   string
	SourceRegionId string
	ObjectId       string
	ProposedValue  []byte
	ProposalId     int64
	Timeout        int64
	Retries        int32
	ConsensusMode  string
	Signature      []byte
}

type ConsensusResponse struct {
	Agreement       bool
	ErrorMessage    string
	Reason          string
	CounterProposal []byte
	Signature       []byte
}

type RegionHeartbeatRequest struct {
	FederationId   string
	SourceRegionId string
	Timestamp      int64
	Signature      []byte
}

type RegionHeartbeatResponse struct {
	Available  bool
	Status     string
	Timestamp  int64
	Signature  []byte
}

// NewFederationServer creates a new federation server
func NewFederationServer(coordinator *FederationCoordinator, regionID string) *FederationServer {
	return &FederationServer{
		coordinator: coordinator,
		regionID:    regionID,
	}
}

// DiscoverRegions discovers other regions in the federation
func (fs *FederationServer) DiscoverRegions(
	ctx context.Context,
	req *proto.RegionDiscoveryRequest,
) (*proto.RegionDiscoveryResponse, error) {
	// Verify request
	if req.FederationId != fs.coordinator.federationID {
		return nil, status.Errorf(codes.InvalidArgument, "invalid federation ID")
	}

	// Get regions from coordinator
	regions := fs.coordinator.GetRegions()
	regionInfos := make([]*proto.RegionInfo, 0, len(regions))

	for id, r := range regions {
		// Skip the source region
		if id == req.SourceRegionId {
			continue
		}

		regionInfos = append(regionInfos, &proto.RegionInfo{
			RegionId:           id,
			Endpoint:           r.Endpoint,
			Status:             r.Status,
			AdminCapabilities:  r.AdminCapabilities,
			LastContactTime:    r.LastContactTime.Unix(),
			TeeCount:           int32(r.TEECount),
			Capabilities:       []string{}, // Add capabilities as needed
		})
	}

	// Create and sign response
	response := &proto.RegionDiscoveryResponse{
		Success:      true,
		ErrorMessage: "",
		Regions:      regionInfos,
		Signature:    []byte("simulated-signature"), // TODO: Implement real signature
	}

	return response, nil
}

// SynchronizeState synchronizes state for an object across regions
func (fs *FederationServer) SynchronizeState(
	ctx context.Context,
	req *proto.RegionSyncRequest,
) (*proto.RegionSyncResponse, error) {
	// Verify request
	if req.FederationId != fs.coordinator.federationID {
		return nil, status.Errorf(codes.InvalidArgument, "invalid federation ID")
	}

	// Check sync mode
	var updatedState []byte
	var stateVersion int64
	var err error
	var hasConflict bool

	switch req.SyncMode {
	case proto.SyncMode_PUSH:
		// Source region is pushing state to us
		err = fs.coordinator.stateManager.SetState(req.ObjectId, req.CurrentState)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to set state: %v", err)
		}
		stateVersion = req.StateVersion

	case proto.SyncMode_PULL:
		// Source region is pulling state from us
		updatedState, err = fs.coordinator.stateManager.GetState(req.ObjectId)
		if err != nil {
			return nil, status.Errorf(codes.NotFound, "object state not found: %v", err)
		}
		stateVersion = req.StateVersion + 1 // Increment version for now

	case proto.SyncMode_BIDIRECTIONAL:
		// Bidirectional sync - need to merge or detect conflicts
		localState, err := fs.coordinator.stateManager.GetState(req.ObjectId)
		if err == nil {
			// We have local state, check for conflict
			if string(localState) != string(req.CurrentState) {
				// State conflict detected - resolve based on policy
				conflictMap := make(map[string][]byte)
				conflictMap[fs.regionID] = localState
				conflictMap[req.SourceRegionId] = req.CurrentState

				// Resolve conflict
				resolvedState, err := fs.coordinator.ResolveStateConflict(req.ObjectId, conflictMap)
				if err != nil {
					return nil, status.Errorf(codes.FailedPrecondition, "conflict resolution failed: %v", err)
				}

				updatedState = resolvedState
				hasConflict = true
				stateVersion = req.StateVersion + 1 // Increment version after conflict resolution

				// If our state is different from resolved state, update local state
				if string(localState) != string(resolvedState) {
					err = fs.coordinator.stateManager.SetState(req.ObjectId, resolvedState)
					if err != nil {
						return nil, status.Errorf(codes.Internal, "failed to update local state: %v", err)
					}
				}
			} else {
				// States match, no conflict
				updatedState = localState
				stateVersion = req.StateVersion
			}
		} else {
			// We don't have local state, accept incoming state
			err = fs.coordinator.stateManager.SetState(req.ObjectId, req.CurrentState)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to set state: %v", err)
			}
			updatedState = req.CurrentState
			stateVersion = req.StateVersion
		}

	default:
		return nil, status.Errorf(codes.InvalidArgument, "invalid sync mode")
	}

	// Create response
	response := &proto.RegionSyncResponse{
		Success:      true,
		ErrorMessage: "",
		UpdatedState: updatedState,
		StateVersion: stateVersion,
		HasConflict:  hasConflict,
		Signature:    []byte("simulated-signature"), // TODO: Implement real signature
	}

	return response, nil
}

// ProposeStateChange proposes a state change to this region
func (fs *FederationServer) ProposeStateChange(
	ctx context.Context,
	req *proto.StateChangeProposal,
) (*proto.StateChangeResponse, error) {
	// Verify request
	if req.FederationId != fs.coordinator.federationID {
		return nil, status.Errorf(codes.InvalidArgument, "invalid federation ID")
	}

	// Check if proposal has expired
	currentTime := time.Now().Unix()
	if req.ExpirationTime < currentTime {
		return nil, status.Errorf(codes.DeadlineExceeded, "proposal expired")
	}

	// Check if we have callback handlers for state changes
	var accepted bool
	var err error

	handler, exists := fs.coordinator.callbackHandlers["stateChange"]
	if exists {
		// Let the handler decide whether to accept
		accepted, err = handler.OnConsensusRequest(req.ObjectId, req.NewState)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "handler error: %v", err)
		}
	} else {
		// Default behavior - accept the change
		accepted = true

		// Apply the state change
		err = fs.coordinator.stateManager.SetState(req.ObjectId, req.NewState)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to set state: %v", err)
		}
	}

	// Create response
	response := &proto.StateChangeResponse{
		Accepted:     accepted,
		ErrorMessage: "",
		StateVersion: req.StateVersion + 1, // Increment version
		Signature:    []byte("simulated-signature"), // TODO: Implement real signature
	}

	return response, nil
}

// CreateFederatedSnapshot initiates creation of a federated snapshot
func (fs *FederationServer) CreateFederatedSnapshot(
	ctx context.Context,
	req *proto.FederatedSnapshotRequest,
) (*proto.FederatedSnapshotResponse, error) {
	// Verify request
	if req.FederationId != fs.coordinator.federationID {
		return nil, status.Errorf(codes.InvalidArgument, "invalid federation ID")
	}

	// Create snapshot through coordinator
	snapshot, err := fs.coordinator.CreateFederatedSnapshot(ctx, req.TargetRegions)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create federated snapshot: %v", err)
	}

	// Convert to proto format
	protoSnapshot := &proto.FederatedSnapshot{
		FederationId:         snapshot.FederationID,
		SnapshotId:           snapshot.SnapshotID,
		Timestamp:            snapshot.Timestamp.Unix(),
		RegionalSnapshots:    make(map[string]*proto.RegionalSnapshot),
		GlobalStateRoot:      snapshot.GlobalStateRoot,
		CoordinatorSignature: snapshot.CoordinatorSignature,
		ConsensusMetadata:    make(map[string][]byte),
	}

	// Add regional snapshots
	for regionID, regionalSnapshot := range snapshot.RegionalSnapshots {
		// Create proto regional snapshot using the adapter function
		protoRegionalSnapshot := InternalToProtoSnapshot(regionalSnapshot)

		// If for some reason the adapter fails, create a basic one manually
		if protoRegionalSnapshot == nil {
			protoRegionalSnapshot = &proto.RegionalSnapshot{
				RegionId:   regionalSnapshot.RegionID,
				SnapshotId: regionalSnapshot.SnapshotID,
				Timestamp:  regionalSnapshot.Timestamp.UnixNano(),
				TeeSnapshotIds: regionalSnapshot.TEESnapshotIDs,
			}

			// Initialize summary
			protoRegionalSnapshot.Summary = &proto.SnapshotSummary{
				MerkleRoot:      regionalSnapshot.SnapshotSummary.MerkleRoot,
				ObjectCount:     int32(regionalSnapshot.SnapshotSummary.ObjectCount),
				TotalStateSize:  regionalSnapshot.SnapshotSummary.TotalStateSize,
				StateRootHashes: make(map[string][]byte),
				Metrics:         make(map[string]float64),
			}

			// Copy state root hashes
			for teeID, hash := range regionalSnapshot.SnapshotSummary.StateRootHashes {
				protoRegionalSnapshot.Summary.StateRootHashes[teeID] = hash
			}

			// Copy metrics
			for metric, value := range regionalSnapshot.SnapshotSummary.RegionalMetrics {
				protoRegionalSnapshot.Summary.Metrics[metric] = value
			}
		}
		protoSnapshot.RegionalSnapshots[regionID] = protoRegionalSnapshot
	}

	// Convert consensus metadata
	for key, value := range snapshot.ConsensusMetadata {
		if strVal, ok := value.(string); ok {
			protoSnapshot.ConsensusMetadata[key] = []byte(strVal)
		} else if byteVal, ok := value.([]byte); ok {
			protoSnapshot.ConsensusMetadata[key] = byteVal
		}
	}

	// Create response
	response := &proto.FederatedSnapshotResponse{
		Success:      true,
		ErrorMessage: "",
		Snapshot:     protoSnapshot,
		Signature:    []byte("simulated-signature"), // TODO: Implement real signature
	}

	return response, nil
}

// VerifyFederatedSnapshot verifies a federated snapshot
func (fs *FederationServer) VerifyFederatedSnapshot(
	ctx context.Context,
	req *proto.VerifyFederatedSnapshotRequest,
) (*proto.VerifyFederatedSnapshotResponse, error) {
	// Verify request
	if req.FederationId != fs.coordinator.federationID {
		return nil, status.Errorf(codes.InvalidArgument, "invalid federation ID")
	}

	// TODO: Implement actual verification
	// For now, we simulate verification success

	response := &proto.VerifyFederatedSnapshotResponse{
		Valid:           true,
		ErrorMessage:    "",
		RegionsVerified: 1, // Just this region for now
		Signature:       []byte("simulated-signature"), // TODO: Implement real signature
	}

	return response, nil
}

// QueryRegionState queries the state of an object in this region
func (fs *FederationServer) QueryRegionState(
	ctx context.Context,
	req *proto.RegionStateQuery,
) (*proto.RegionStateResponse, error) {
	// Verify request
	if req.FederationId != fs.coordinator.federationID {
		return nil, status.Errorf(codes.InvalidArgument, "invalid federation ID")
	}

	// Get state from state manager
	stateData, err := fs.coordinator.stateManager.GetState(req.ObjectId)
	if err != nil {
		return &proto.RegionStateResponse{
			Exists:       false,
			ErrorMessage: fmt.Sprintf("object state not found: %v", err),
			StateVersion: 0,
			LastModified: 0,
			StateData:    nil,
			Signature:    []byte("simulated-signature"), // TODO: Implement real signature
		}, nil
	}

	// Create response
	response := &proto.RegionStateResponse{
		Exists:       true,
		ErrorMessage: "",
		StateVersion: 1, // For now, no versioning is tracked
		LastModified: time.Now().Unix(), // For now, no modification time is tracked
		StateData:    nil,
		Signature:    []byte("simulated-signature"), // TODO: Implement real signature
	}

	// Include state data if requested
	if req.IncludeState {
		response.StateData = stateData
	}

	return response, nil
}

// RequestConsensus requests consensus from this region on a proposed value
func (fs *FederationServer) RequestConsensus(
	ctx context.Context,
	req *proto.ConsensusRequest,
) (*proto.ConsensusResponse, error) {
	// Verify request
	if req.FederationId != fs.coordinator.federationID {
		return nil, status.Errorf(codes.InvalidArgument, "invalid federation ID")
	}

	// Check if proposal has expired
	currentTime := time.Now().Unix()
	if req.ExpirationTime < currentTime {
		return nil, status.Errorf(codes.DeadlineExceeded, "consensus request expired")
	}

	// Check if we have callback handlers for consensus
	var agreement bool
	var reason string
	var err error

	handler, exists := fs.coordinator.callbackHandlers["consensus"]
	if exists {
		// Let the handler decide whether to agree
		agreement, err = handler.OnConsensusRequest(req.ObjectId, req.ProposedValue)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "handler error: %v", err)
		}
		reason = "Handler decision"
	} else {
		// Default behavior - agree with the proposal
		agreement = true
		reason = "Default agreement"
	}

	// Create response
	response := &proto.ConsensusResponse{
		Agreement:       agreement,
		ErrorMessage:    "",
		Reason:          reason,
		CounterProposal: nil, // No counter-proposal
		Signature:       []byte("simulated-signature"), // TODO: Implement real signature
	}

	return response, nil
}

// HeartbeatCheck verifies that this region is still available
func (fs *FederationServer) HeartbeatCheck(
	ctx context.Context,
	req *proto.RegionHeartbeatRequest,
) (*proto.RegionHeartbeatResponse, error) {
	// Verify request
	if req.FederationId != fs.coordinator.federationID {
		return nil, status.Errorf(codes.InvalidArgument, "invalid federation ID")
	}

	// Update last contact time for the source region
	fs.coordinator.UpdateRegionStatus(req.SourceRegionId, "active")

	// Create response
	response := &proto.RegionHeartbeatResponse{
		Available:  true,
		Status:     "healthy", // Assume we're healthy
		Timestamp:  time.Now().Unix(),
		Signature:  []byte("simulated-signature"), // TODO: Implement real signature
	}

	return response, nil
}
