// Package mesh provides a mesh network for TEE-to-TEE communication
package mesh

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/gabstv/go-bsdiff/pkg/bsdiff"
	"github.com/gabstv/go-bsdiff/pkg/bspatch"
	"github.com/rhombus-tech/vm/tee/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// TeeMeshService implements the TeeMesh service for direct TEE-to-TEE communication
type TeeMeshService struct {
	proto.UnimplementedTeeMeshServer // Embed for forward compatibility

	// TEE information
	teeID   string
	teeType string // SGX or SEV
	
	// Region information
	regionID string
	
	// Network information
	endpoint string
	server   *grpc.Server
	
	// TLS configuration
	tlsCert   string
	tlsKey    string
	tlsConfig *tls.Config
	
	// Peer tracking
	peers     map[string]*Peer
	peerMutex sync.RWMutex
	
	// Execution handlers
	executionHandler ExecutionHandler
	
	// State tracking
	stateMutex   sync.RWMutex
	stateCache   map[string]stateInfo
	lastStateHash []byte
	// State management
	stateManager StateManager
	
	// Batch processing
	batchProcessor *BatchProcessor
}

// StateManager defines the interface for managing serialized state
type StateManager interface {
	// SerializeState serializes an object to a byte array
	SerializeState(objectID string, object interface{}) ([]byte, error)
	
	// DeserializeState deserializes a byte array to an object
	DeserializeState(objectID string, data []byte, target interface{}) error
	
	// GetState retrieves the state for an object
	GetState(objectID string) ([]byte, error)
	
	// SetState stores the state for an object
	SetState(objectID string, state []byte) error
	
	// CreateSnapshot creates a verifiable snapshot of an object's state
	CreateSnapshot(objectID string, regionID string, teeID string, teeType string) (*StateSnapshot, error)
	
	// VerifySnapshot verifies a snapshot's integrity and authenticity
	VerifySnapshot(snapshot *StateSnapshot) error
	
	// RestoreFromSnapshot restores an object's state from a snapshot
	RestoreFromSnapshot(snapshot *StateSnapshot) error
	
	// ListSnapshots lists available snapshots for an object
	ListSnapshots(objectID string) ([]*SnapshotChainInfo, error)
	
	// GetLatestSnapshot gets the latest snapshot for an object
	GetLatestSnapshot(objectID string) (*StateSnapshot, error)
}

// DefaultStateManager is the default implementation of the StateManager interface
type DefaultStateManager struct {
	objectStates     map[string][]byte
	objectStatesMutex sync.RWMutex
}

// NewDefaultStateManager creates a new DefaultStateManager
func NewDefaultStateManager() *DefaultStateManager {
	return &DefaultStateManager{
		objectStates: make(map[string][]byte),
	}
}

// SerializeState implements StateManager.SerializeState using JSON
func (m *DefaultStateManager) SerializeState(objectID string, object interface{}) ([]byte, error) {
	if object == nil {
		return nil, fmt.Errorf("cannot serialize nil object")
	}
	
	// Use JSON marshal for serialization
	data, err := json.Marshal(object)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize state: %w", err)
	}
	
	// Store the state
	err = m.SetState(objectID, data)
	if err != nil {
		return nil, err
	}
	
	return data, nil
}

// DeserializeState implements StateManager.DeserializeState using JSON
func (m *DefaultStateManager) DeserializeState(objectID string, data []byte, target interface{}) error {
	if target == nil {
		return fmt.Errorf("cannot deserialize to nil target")
	}
	
	// Use JSON unmarshal for deserialization
	err := json.Unmarshal(data, target)
	if err != nil {
		return fmt.Errorf("failed to deserialize state: %w", err)
	}
	
	return nil
}

// GetState implements StateManager.GetState
func (m *DefaultStateManager) GetState(objectID string) ([]byte, error) {
	m.objectStatesMutex.RLock()
	defer m.objectStatesMutex.RUnlock()
	
	state, exists := m.objectStates[objectID]
	if !exists {
		return nil, fmt.Errorf("no state found for object %s", objectID)
	}
	
	return state, nil
}

// SetState implements StateManager.SetState
func (m *DefaultStateManager) SetState(objectID string, state []byte) error {
	if len(state) == 0 {
		return fmt.Errorf("cannot store empty state")
	}
	
	m.objectStatesMutex.Lock()
	m.objectStates[objectID] = state
	m.objectStatesMutex.Unlock()
	
	return nil
}

// Peer represents a remote TEE in the mesh
type Peer struct {
	TEEID    string // Unique ID
	TEEType  string // SGX or SEV
	RegionID string
	Endpoint string
	Status   string // e.g., "active", "degraded", "offline"
	
	MeshClient proto.TeeMeshClient
	Conn       *grpc.ClientConn
	
	// Performance tracking
	AverageLatencyNs  uint64    // Average latency in nanoseconds
	LatencyHistory    []uint64  // Recent latency measurements (circular buffer)
	LatencyHistoryPos int       // Position in circular buffer
	LastPingTime      time.Time // Last successful ping time
	SuccessCount      int       // Count of successful operations
	FailureCount      int       // Count of failed operations
}

// updateLatency updates the latency tracking for this peer
func (p *Peer) updateLatency(latencyNs uint64) {
	const historySize = 10 // Keep last 10 latency measurements
	
	// Initialize history if needed
	if p.LatencyHistory == nil {
		p.LatencyHistory = make([]uint64, historySize)
	}
	
	// Update history in circular buffer
	p.LatencyHistory[p.LatencyHistoryPos] = latencyNs
	p.LatencyHistoryPos = (p.LatencyHistoryPos + 1) % historySize
	
	// Recalculate average latency
	var sum uint64
	var count int
	for i, lat := range p.LatencyHistory {
		if lat > 0 || i == p.LatencyHistoryPos {
			sum += lat
			count++
		}
	}
	
	if count > 0 {
		p.AverageLatencyNs = sum / uint64(count)
	}
	
	// Update last ping time
	p.LastPingTime = time.Now()
	
	// Reset failure count and increment success
	p.FailureCount = 0
	p.SuccessCount++
}

// recordFailure records a failed interaction with this peer
func (p *Peer) recordFailure() {
	p.FailureCount++
	p.SuccessCount = 0
	
	// Update status based on consecutive failures
	if p.FailureCount >= 3 {
		p.Status = "degraded"
	}
	if p.FailureCount >= 5 {
		p.Status = "offline"
	}
}

// ExecutionHandler defines the interface for handling direct execution requests
type ExecutionHandler interface {
	Execute(ctx context.Context, req *proto.DirectExecutionRequest) (*proto.DirectExecutionResponse, error)
}

// MeshConfig contains configuration for a mesh service
type MeshConfig struct {
	TEEID      string
	TEEType    string
	RegionID   string
	Endpoint   string
	TLSCert    string
	TLSKey     string
	Handler    ExecutionHandler
}

// NewTeeMeshService creates a new mesh service
func NewTeeMeshService(config *MeshConfig) (*TeeMeshService, error) {
	if config.TEEID == "" {
		return nil, errors.New("TEEID is required")
	}
	
	if config.TEEType == "" {
		return nil, errors.New("TEEType is required")
	}
	
	if config.RegionID == "" {
		return nil, errors.New("RegionID is required")
	}
	
	if config.Endpoint == "" {
		return nil, errors.New("Endpoint is required")
	}
	
	var tlsConfig *tls.Config
	var err error
	
	// If TLS is configured, set it up
	if config.TLSCert != "" && config.TLSKey != "" {
		tlsConfig, err = createTLSConfig(config.TLSCert, config.TLSKey)
		if err != nil {
			return nil, fmt.Errorf("failed to create TLS config: %w", err)
		}
	}
	
	stateManager := NewDefaultStateManager()
	
	service := &TeeMeshService{
		teeID:           config.TEEID,
		teeType:         config.TEEType,
		regionID:        config.RegionID,
		endpoint:        config.Endpoint,
		tlsCert:         config.TLSCert,
		tlsKey:          config.TLSKey,
		tlsConfig:       tlsConfig,
		peers:           make(map[string]*Peer),
		executionHandler: config.Handler,
		stateCache:       make(map[string]stateInfo),
		stateManager:     stateManager,
	}
	
	// Initialize the batch processor
	service.batchProcessor = NewBatchProcessor(service)
	
	return service, nil
}

// Start starts the mesh service
func (m *TeeMeshService) Start() error {
	// Create a listener
	lis, err := net.Listen("tcp", m.endpoint)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	
	var opts []grpc.ServerOption
	
	// If TLS is configured, use it
	if m.tlsConfig != nil {
		creds := credentials.NewTLS(m.tlsConfig)
		opts = append(opts, grpc.Creds(creds))
	}
	
	// Create a gRPC server
	m.server = grpc.NewServer(opts...)
	
	// Register the mesh service
	proto.RegisterTeeMeshServer(m.server, m)
	
	// Start the server
	go func() {
		if err := m.server.Serve(lis); err != nil {
			fmt.Printf("Failed to serve: %v\n", err)
		}
	}()
	
	// Start periodic pings
	go m.PerformPeriodicPings(context.Background())
	
	return nil
}

// Stop stops the mesh service
func (m *TeeMeshService) Stop() {
	if m.server != nil {
		m.server.GracefulStop()
	}
	
	// Close all peer connections
	m.peerMutex.Lock()
	defer m.peerMutex.Unlock()
	
	for _, peer := range m.peers {
		if peer.Conn != nil {
			peer.Conn.Close()
		}
	}
}

// Discover handles TEE discovery requests
func (m *TeeMeshService) Discover(ctx context.Context, req *proto.DiscoveryRequest) (*proto.DiscoveryResponse, error) {
	// Record the discovering peer
	if req.TeeId != "" && req.Endpoint != "" {
		// Create a new peer
		peer := &Peer{
			TEEID:       req.TeeId,
			TEEType:     req.TeeType,
			Endpoint:    req.Endpoint,
			RegionID:    req.RegionId,
			Status:      "active",
			LastPingTime: time.Now(),
		}
		
		// Add or update the peer
		m.addOrUpdatePeer(peer)
		
		// Try to establish connection to the new peer if needed
		m.ConnectToPeer(req.TeeId, req.Endpoint)
	}
	
	// Build a response with all known peers
	resp := &proto.DiscoveryResponse{
		Peers: make([]*proto.TEEPeer, 0),
	}
	
	// Add all peers to the response
	m.peerMutex.RLock()
	defer m.peerMutex.RUnlock()
	
	for _, p := range m.peers {
		// Skip the requesting TEE
		if p.TEEID == req.TeeId {
			continue
		}
		
		// Only include peers in the same region
		if p.RegionID != req.RegionId {
			continue
		}
		
		resp.Peers = append(resp.Peers, &proto.TEEPeer{
			TeeId:    p.TEEID,
			TeeType:  p.TEEType,
			Endpoint: p.Endpoint,
			RegionId: p.RegionID,
			Status:   p.Status,
		})
	}
	
	return resp, nil
}

// DirectExecute handles direct execution requests
func (m *TeeMeshService) DirectExecute(ctx context.Context, req *proto.DirectExecutionRequest) (*proto.DirectExecutionResponse, error) {
	if req == nil {
		return nil, errors.New("cannot execute nil request")
	}
	
	// Record start time for latency calculation
	start := time.Now()
	
	// Validate the request
	if req.FunctionCall == "" {
		return &proto.DirectExecutionResponse{
			Success:     false,
			Result:      []byte("Error: No function specified"),
			Timestamp:   fmt.Sprintf("%d", time.Now().UnixNano()),
		}, nil
	}
	
	// Retrieve the required state if needed
	if m.executionHandler != nil && req.IdTo != "" {
		// Get current state for the object, if it exists
		currentState, err := m.stateManager.GetState(req.IdTo)
		if err != nil {
			// State not found, but that's OK for some executions
			// Just log the warning and continue
			fmt.Printf("Warning: State not found for object %s: %v\n", req.IdTo, err)
		} else if len(currentState) > 0 {
			// If we have state, we might want to modify the request to include it
			// In our implementation, we pass it unmodified to the execution handler
			// to handle state in its own way
			fmt.Printf("Found state for object %s, size: %d bytes\n", req.IdTo, len(currentState))
		}
	}
	
	// If we don't have an execution handler, we can't process this request
	if m.executionHandler == nil {
		return &proto.DirectExecutionResponse{
			Success:     false,
			Result:      []byte("Error: No execution handler registered"),
			Timestamp:   fmt.Sprintf("%d", time.Now().UnixNano()),
		}, nil
	}
	
	// Execute request and return response
	resp, execErr := m.executionHandler.Execute(ctx, req)
	if execErr != nil {
		fmt.Printf("Execution error for function %s: %v\n", req.FunctionCall, execErr)
		return &proto.DirectExecutionResponse{
			Success:     false,
			Result:      []byte(fmt.Sprintf("Error: %v", execErr)),
			Timestamp:   fmt.Sprintf("%d", time.Now().UnixNano()),
		}, nil
	}
	
	// If execution was successful and response contains result data, update our state
	// In our implementation, we'll consider the Output field to contain state updates if needed
	if resp.Success && req.IdTo != "" && resp.Result != nil && len(resp.Result) > 0 {
		// Store the updated state after execution
		err := m.stateManager.SetState(req.IdTo, resp.Result)
		if err != nil {
			// Log the error but don't fail the request
			fmt.Printf("Warning: Failed to store output state for object %s: %v\n", req.IdTo, err)
		}
		
		// Update our last state hash for efficient delta updates
		m.stateMutex.Lock()
		m.lastStateHash = resp.Result
		m.stateMutex.Unlock()
	}
	
	// Add network latency
	resp.NetworkLatencyNs = uint64(time.Since(start).Nanoseconds())
	
	return resp, nil
}

// Ping handles ping requests
func (m *TeeMeshService) Ping(ctx context.Context, req *proto.PingRequest) (*proto.PingResponse, error) {
	// Record timestamp for accurate latency measurement
	now := time.Now()
	
	// Update peer information if this ping comes from a known peer
	if req.SenderId != "" {
		if peer, exists := m.GetPeer(req.SenderId); exists {
			// Mark peer as active if it was previously degraded or offline
			if peer.Status == "degraded" || peer.Status == "offline" {
				peer.Status = "active"
				m.addOrUpdatePeer(peer)
			}
		}
	}
	
	// Return ping response with high precision timestamp
	return &proto.PingResponse{
		ResponderId: m.teeID,
		TimestampNs: now.UnixNano(),
	}, nil
}

// Sync handles state synchronization requests
func (m *TeeMeshService) Sync(ctx context.Context, req *proto.SyncRequest) (*proto.SyncResponse, error) {
	// 1. Check if we have the requested object
	state, err := m.getStateForObject(req.ObjectId)
	if err != nil {
		return &proto.SyncResponse{
			Success:     false,
			StateHash:   nil,
			TimestampNs: time.Now().UnixNano(),
		}, nil
	}
	
	// 2. Check state cache to see if we can do a delta update
	key := fmt.Sprintf("%s:%s", req.SenderId, req.ObjectId)
	m.stateMutex.RLock()
	lastState, hasLastState := m.stateCache[key]
	m.stateMutex.RUnlock()
	
	// 3. Prepare response
	resp := &proto.SyncResponse{
		Success:     true,
		StateHash:   nil,
		TimestampNs: time.Now().UnixNano(),
	}
	
	// 4. Determine if we should send full state or delta
	if hasLastState && req.StateHash != nil {
		// We have previous state from this sender and they provided a hash
		// Calculate a delta if it's more efficient
		currentState, err := m.getStateForObject(req.ObjectId)
		if err == nil {
			// Only use delta if it's more efficient
			if m.shouldUseDelta(lastState.state, currentState, 0.5) {
				// Generate delta update (only changed portions)
				deltaUpdates, err := m.generateDeltaUpdates(req.ObjectId, lastState.state)
				if err != nil {
					fmt.Printf("Warning: Failed to generate delta updates: %v\n", err)
				} else {
					// If the delta is significantly smaller than the full state, use it
					if len(deltaUpdates) < len(state)/2 {
						resp.DeltaUpdates = deltaUpdates
						resp.StateHash = currentState
					}
				}
			}
		}
	}
	
	// If we didn't add a delta, use the full state hash
	if resp.DeltaUpdates == nil {
		resp.StateHash = state
	}
	
	// 5. Update our cache of what this sender has
	m.stateMutex.Lock()
	m.stateCache[key] = stateInfo{
		state:     state,
		timestamp: time.Now().UnixNano(),
	}
	m.stateMutex.Unlock()
	
	return resp, nil
}

// handleReceivedSync processes a received sync response
func (m *TeeMeshService) handleReceivedSync(resp *proto.SyncResponse) error {
	if resp == nil {
		return errors.New("received nil sync response")
	}
	
	// We need to determine the object ID from context
	// In a real implementation, you'd track this in a request/response map
	// For now we'll use a placeholder
	objectID := "current-sync-object" // This needs to be passed or tracked
	
	// Check if we received a delta update
	if resp.DeltaUpdates != nil && len(resp.DeltaUpdates) > 0 {
		// We got a delta update, need to apply it to our current state
		
		// Get our current state for the object
		currentState, err := m.stateManager.GetState(objectID)
		if err != nil {
			return &DeltaUpdateError{
				Operation: "sync",
				ObjectID:  objectID,
				Err:       fmt.Errorf("failed to get current state: %w", err),
			}
		}
		
		// Apply the delta update to our current state
		newState, err := m.applyDeltaUpdates(currentState, resp.DeltaUpdates)
		if err != nil {
			// If delta application fails, we should request a full state instead
			// This is a recoverable error so we'll just log it
			fmt.Printf("Failed to apply delta update for object %s: %v\n", objectID, err)
			
			// In real implementation, we'd request a full state
			// This is just a placeholder for now
			return &DeltaUpdateError{
				Operation: "sync",
				ObjectID:  objectID,
				Err:       fmt.Errorf("fallback to full state after delta failure: %w", err),
			}
		}
		
		// Successfully applied delta, store the new state
		err = m.stateManager.SetState(objectID, newState)
		if err != nil {
			return &DeltaUpdateError{
				Operation: "sync",
				ObjectID:  objectID,
				Err:       fmt.Errorf("failed to store updated state: %w", err),
			}
		}
		
		fmt.Printf("Successfully applied delta update for object %s, new state size: %d bytes\n", 
			objectID, len(newState))
	} else if resp.StateHash != nil && len(resp.StateHash) > 0 {
		// We got a state hash, but no actual state
		// In a real implementation, we would request the full state if needed
		fmt.Printf("Received state hash for object %s, hash size: %d bytes\n", 
			objectID, len(resp.StateHash))
		
		// We'd then decide whether to request the full state
		// based on comparing this hash with our current state hash
	} else {
		// No state or delta received
		return fmt.Errorf("sync response contained neither state hash nor delta updates")
	}
	
	return nil
}

// shouldUseDelta determines if we should use delta updates based on state comparison
func (m *TeeMeshService) shouldUseDelta(prevState, currentState []byte, changeThreshold float64) bool {
	// For very small states, the overhead of delta is not worth it
	if len(prevState) < 1024 || len(currentState) < 1024 {
		return false
	}
	
	// If no previous state, we can't do a delta update
	if len(prevState) == 0 {
		return false
	}
	
	// If the state sizes are vastly different, delta is less efficient
	sizeDiff := float64(abs(len(currentState) - len(prevState))) / float64(len(prevState))
	if sizeDiff > changeThreshold {
		return false
	}
	
	// Simple heuristic: sample random bytes to estimate change percentage
	// In a production system, you'd use a more sophisticated algorithm
	const sampleSize = 100
	changedBytes := 0
	for i := 0; i < sampleSize; i++ {
		idx := (i * len(prevState) / sampleSize) % len(prevState)
		if idx < len(currentState) && prevState[idx] != currentState[idx] {
			changedBytes++
		}
	}
	
	changePercentage := float64(changedBytes) / float64(sampleSize)
	
	// Use delta updates if less than threshold (e.g., 50%) of bytes changed
	return changePercentage <= changeThreshold
}

// stateInfo tracks state information for delta updates
type stateInfo struct {
	state     []byte
	timestamp int64
}

// DeltaUpdateError defines errors related to delta update operations
type DeltaUpdateError struct {
	Operation string // The operation that failed (generate, apply, etc.)
	ObjectID  string // The object ID involved
	Err       error  // The underlying error
}

func (e *DeltaUpdateError) Error() string {
	return fmt.Sprintf("delta update %s failed for object %s: %v", e.Operation, e.ObjectID, e.Err)
}

func (e *DeltaUpdateError) Unwrap() error {
	return e.Err
}

// generateDeltaUpdates creates an efficient delta between current and previous state
// Uses binary diffing for small payloads (reduced CPU cost)
func (m *TeeMeshService) generateDeltaUpdates(objectID string, previousState []byte) ([]byte, error) {
	// Get current state for the object
	currentState, err := m.getStateForObject(objectID)
	if err != nil {
		return nil, &DeltaUpdateError{
			Operation: "generate",
			ObjectID:  objectID,
			Err:       fmt.Errorf("failed to get current state: %w", err),
		}
	}
	
	// If no previous state or current state, cannot generate delta
	if len(previousState) == 0 || len(currentState) == 0 {
		return nil, &DeltaUpdateError{
			Operation: "generate",
			ObjectID:  objectID,
			Err:       errors.New("missing state data"),
		}
	}
	
	// Use bsdiff to create an efficient binary diff
	delta, err := bsdiff.Bytes(previousState, currentState)
	if err != nil {
		return nil, &DeltaUpdateError{
			Operation: "generate",
			ObjectID:  objectID,
			Err:       fmt.Errorf("bsdiff failed: %w", err),
		}
	}
	
	// We don't update lastStateHash here anymore as it's managed by updateObjectState
	
	return delta, nil
}

// applyDeltaUpdates applies a delta patch to a previous state to get the new state
func (m *TeeMeshService) applyDeltaUpdates(previousState []byte, deltaUpdates []byte) ([]byte, error) {
	if len(previousState) == 0 {
		return nil, &DeltaUpdateError{
			Operation: "apply",
			ObjectID:  "", // Unknown in this context
			Err:       errors.New("missing previous state"),
		}
	}
	
	if len(deltaUpdates) == 0 {
		return previousState, nil // No changes, return original state
	}
	
	// Apply the bsdiff patch to get the new state
	newState, err := bspatch.Bytes(previousState, deltaUpdates)
	if err != nil {
		return nil, &DeltaUpdateError{
			Operation: "apply",
			ObjectID:  "", // Unknown in this context
			Err:       fmt.Errorf("bspatch failed: %w", err),
		}
	}
	
	return newState, nil
}

// getStateForObject retrieves the current state for a given object ID
// In a real implementation, this would access your state storage system
func (m *TeeMeshService) getStateForObject(objectID string) ([]byte, error) {
	// This is a placeholder - in a real implementation, you would:
	// 1. Access your state storage (database, in-memory store, etc.)
	// 2. Retrieve the current state for the specified object
	// 3. Return the state as a byte array
	
	// For testing/placeholder, we'll simulate state
	state, err := m.stateManager.GetState(objectID)
	if err != nil {
		return nil, fmt.Errorf("no state available for object %s", objectID)
	}
	
	return state, nil
}

// updateObjectState updates the state for a specific object and updates lastStateHash
func (m *TeeMeshService) updateObjectState(objectID string, state []byte) {
	if len(state) == 0 {
		return
	}
	
	err := m.stateManager.SetState(objectID, state)
	if err != nil {
		fmt.Printf("Failed to update state for object %s: %v\n", objectID, err)
		return
	}
	
	// Also update lastStateHash for quick comparisons
	m.stateMutex.Lock()
	m.lastStateHash = state
	m.stateMutex.Unlock()
}

// ProxyExecute handles execution requests with automatic failover
func (m *TeeMeshService) ProxyExecute(ctx context.Context, req *proto.ProxyExecutionRequest) (*proto.DirectExecutionResponse, error) {
	// 1. Try to execute locally if this TEE is the target
	if req.IdTo == m.teeID {
		// Convert ProxyExecutionRequest to DirectExecutionRequest
		directReq := &proto.DirectExecutionRequest{
			SenderId:          req.SenderId,
			IdTo:              req.IdTo,
			FunctionCall:      req.FunctionCall,
			Parameters:        req.Parameters,
			RegionId:          req.RegionId,
			DetailedProof:     req.DetailedProof,
			ExpectedHash:      req.ExpectedHash,
			BypassCoordinator: req.BypassCoordinator,
		}
		
		return m.DirectExecute(ctx, directReq)
	}
	
	// 2. Try to execute on the target peer if it's available
	targetPeer, exists := m.GetPeer(req.IdTo)
	if exists && targetPeer.Status == "active" {
		// Check if the target peer is in excluded peers list
		excluded := false
		for _, excludedID := range req.ExcludedPeers {
			if excludedID == req.IdTo {
				excluded = true
				break
			}
		}
		
		if !excluded {
			// Try direct execution on target
			start := time.Now()
			directReq := &proto.DirectExecutionRequest{
				SenderId:          req.SenderId,
				IdTo:              req.IdTo,
				FunctionCall:      req.FunctionCall,
				Parameters:        req.Parameters,
				RegionId:          req.RegionId,
				DetailedProof:     req.DetailedProof,
				ExpectedHash:      req.ExpectedHash,
				BypassCoordinator: req.BypassCoordinator,
			}
			
			resp, err := targetPeer.MeshClient.DirectExecute(ctx, directReq)
			if err == nil {
				// Successful execution
				resp.NetworkLatencyNs = uint64(time.Since(start).Nanoseconds())
				targetPeer.updateLatency(resp.NetworkLatencyNs)
				return resp, nil
			}
			
			// If we reach here, there was an error executing on the target peer
			fmt.Printf("Failed to execute on target peer %s: %v\n", req.IdTo, err)
			targetPeer.recordFailure()
		}
	}
	
	// 3. Failover to another peer based on preferred TEE type
	eligiblePeers := make([]*Peer, 0)
	
	m.peerMutex.RLock()
	for id, peer := range m.peers {
		// Skip if peer is in excluded list
		excluded := false
		for _, excludedID := range req.ExcludedPeers {
			if excludedID == id {
				excluded = true
				break
			}
		}
		
		if excluded {
			continue
		}
		
		// Skip if peer is not active
		if peer.Status != "active" {
			continue
		}
		
		// Skip if peer is in a different region and cross-region is not allowed
		if peer.RegionID != req.RegionId && !req.CrossRegionAllowed {
			continue
		}
		
		// Add to eligible peers
		eligiblePeers = append(eligiblePeers, peer)
	}
	m.peerMutex.RUnlock()
	
	// Sort peers by preferred TEE type and latency
	if req.PreferredTeeType != "" {
		sort.SliceStable(eligiblePeers, func(i, j int) bool {
			// Preferred TEE type comes first
			if eligiblePeers[i].TEEType == req.PreferredTeeType && eligiblePeers[j].TEEType != req.PreferredTeeType {
				return true
			}
			
			// Then sort by region (prefer same region)
			if eligiblePeers[i].RegionID == req.RegionId && eligiblePeers[j].RegionID != req.RegionId {
				return true
			}
			
			// Finally, sort by latency (prefer lower latency)
			return eligiblePeers[i].AverageLatencyNs < eligiblePeers[j].AverageLatencyNs
		})
	}
	
	// Try eligible peers until one succeeds or we run out of retries
	maxRetries := int(req.MaxRetries)
	if maxRetries <= 0 {
		maxRetries = 3 // Default to 3 retries
	}
	
	// Set a context timeout if specified
	var cancelFunc context.CancelFunc
	if req.TimeoutMs > 0 {
		ctx, cancelFunc = context.WithTimeout(ctx, time.Duration(req.TimeoutMs)*time.Millisecond)
		defer cancelFunc()
	}
	
	// Try each eligible peer
	for i := 0; i < min(len(eligiblePeers), maxRetries); i++ {
		peer := eligiblePeers[i]
		
		// Skip the original target peer since we already tried it
		if peer.TEEID == req.IdTo {
			continue
		}
		
		// Try execution on this peer
		start := time.Now()
		directReq := &proto.DirectExecutionRequest{
			SenderId:          req.SenderId,
			IdTo:              peer.TEEID, // Change target to the failover peer
			FunctionCall:      req.FunctionCall,
			Parameters:        req.Parameters,
			RegionId:          req.RegionId,
			DetailedProof:     req.DetailedProof,
			ExpectedHash:      req.ExpectedHash,
			BypassCoordinator: req.BypassCoordinator,
		}
		
		resp, err := peer.MeshClient.DirectExecute(ctx, directReq)
		if err == nil {
			// Successful execution
			resp.NetworkLatencyNs = uint64(time.Since(start).Nanoseconds())
			peer.updateLatency(resp.NetworkLatencyNs)
			return resp, nil
		}
		
		fmt.Printf("Failed to execute on failover peer %s: %v\n", peer.TEEID, err)
		peer.recordFailure()
		
		// Check if context is done
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			// Continue to next peer
		}
	}
	
	// If we reach here, all attempts failed
	return nil, fmt.Errorf("all execution attempts failed after %d retries", maxRetries)
}

// ConnectToPeer establishes a connection to a peer
func (m *TeeMeshService) ConnectToPeer(peerID, endpoint string) (*Peer, error) {
	// Check if we already have an active connection to this peer
	m.peerMutex.RLock()
	existingPeer, exists := m.peers[peerID]
	m.peerMutex.RUnlock()
	
	if exists && existingPeer.MeshClient != nil && existingPeer.Conn != nil {
		// We already have a connection to this peer
		return existingPeer, nil
	}
	
	// Create TLS credentials
	tlsConfig, err := createTLSConfig(m.tlsCert, m.tlsKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create TLS config: %v", err)
	}
	creds := credentials.NewTLS(tlsConfig)
	
	// Connect to the peer
	conn, err := grpc.Dial(endpoint, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to peer %s at %s: %v", peerID, endpoint, err)
	}
	
	// Create client
	client := proto.NewTeeMeshClient(conn)
	
	// Create peer object
	peer := &Peer{
		TEEID:        peerID,
		Endpoint:     endpoint,
		LastPingTime: time.Now(),
		MeshClient:   client,
		Conn:         conn,
	}
	
	// Store peer connection
	m.addOrUpdatePeer(peer)
	
	return peer, nil
}

// addOrUpdatePeer adds or updates a peer
func (m *TeeMeshService) addOrUpdatePeer(peer *Peer) {
	m.peerMutex.Lock()
	defer m.peerMutex.Unlock()
	
	// Check if peer already exists
	existing, exists := m.peers[peer.TEEID]
	if exists {
		// Update existing peer
		existing.TEEType = peer.TEEType
		existing.Endpoint = peer.Endpoint
		existing.RegionID = peer.RegionID
		
		// Only update status if the new status is active
		if peer.Status == "active" {
			existing.Status = "active"
		}
		
		// Update last seen time
		existing.LastPingTime = time.Now()
		
		// Keep the existing connection
		if peer.MeshClient != nil {
			existing.MeshClient = peer.MeshClient
		}
		if peer.Conn != nil {
			existing.Conn = peer.Conn
		}
	} else {
		// Add new peer
		m.peers[peer.TEEID] = peer
	}
}

// GetPeer gets a peer by ID
func (m *TeeMeshService) GetPeer(peerID string) (*Peer, bool) {
	m.peerMutex.RLock()
	defer m.peerMutex.RUnlock()
	
	peer, exists := m.peers[peerID]
	return peer, exists
}

// GetTEEID returns the ID of this TEE in the mesh
func (m *TeeMeshService) GetTEEID() string {
	return m.teeID
}

// GetTEEType returns the TEE type for this service
func (m *TeeMeshService) GetTEEType() string {
    return m.teeType
}

// GetSuitablePeers returns a list of peers matching the criteria
func (m *TeeMeshService) GetSuitablePeers(regionID string, preferredTEEType string, excludedPeers []string) []*Peer {
	m.peerMutex.RLock()
	defer m.peerMutex.RUnlock()
	
	// Create a map of excluded peers for quick lookup
	excluded := make(map[string]bool)
	for _, peerID := range excludedPeers {
		excluded[peerID] = true
	}
	
	// Collect suitable peers
	suitable := make([]*Peer, 0)
	for _, peer := range m.peers {
		// Skip excluded peers
		if excluded[peer.TEEID] {
			continue
		}
		
		// Match region if specified
		if regionID != "" && peer.RegionID != regionID {
			continue
		}
		
		// Check status (only include active peers)
		if peer.Status != "active" {
			continue
		}
		
		// Add to suitable peers
		suitable = append(suitable, peer)
	}
	
	// Sort suitable peers by preference
	sort.Slice(suitable, func(i, j int) bool {
		// Preferred TEE type comes first if specified
		if preferredTEEType != "" {
			iPreferred := suitable[i].TEEType == preferredTEEType
			jPreferred := suitable[j].TEEType == preferredTEEType
			
			if iPreferred != jPreferred {
				return iPreferred
			}
		}
		
		// Sort by latency (lower is better)
		return suitable[i].AverageLatencyNs < suitable[j].AverageLatencyNs
	})
	
	return suitable
}

// GetBatchProcessorMetrics returns metrics from the batch processor
func (m *TeeMeshService) GetBatchProcessorMetrics() *BatchProcessorMetrics {
	if m.batchProcessor != nil {
		return m.batchProcessor.GetMetrics()
	}
	return nil
}

// PerformPeriodicPings maintains accurate latency information with peers
func (m *TeeMeshService) PerformPeriodicPings(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.pingAllPeers(ctx)
		}
	}
}

// pingAllPeers pings all known peers to update latency information
func (m *TeeMeshService) pingAllPeers(ctx context.Context) {
	m.peerMutex.RLock()
	peers := make([]*Peer, 0, len(m.peers))
	for _, peer := range m.peers {
		peers = append(peers, peer)
	}
	m.peerMutex.RUnlock()
	
	for _, peer := range peers {
		// Skip offline peers (ping them less frequently)
		if peer.Status == "offline" && time.Since(peer.LastPingTime) < 30*time.Second {
			continue
		}
		
		// Create a timeout context for ping
		pingCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		
		// Perform ping with accurate timing
		start := time.Now()
		resp, err := peer.MeshClient.Ping(pingCtx, &proto.PingRequest{
			SenderId: m.teeID,
		})
		latency := time.Since(start)
		
		// Update peer status based on ping result
		if err == nil && resp != nil {
			peer.updateLatency(uint64(latency.Nanoseconds()))
		} else {
			peer.recordFailure()
		}
		
		cancel()
	}
}

// createTLSConfig creates a TLS configuration from cert and key files
// with support for mutual attestation and performance optimization
func createTLSConfig(certFile, keyFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load TLS credentials: %w", err)
	}
	
	// Create a certificate pool and add the client CA
	certPool := x509.NewCertPool()
	ca, err := ioutil.ReadFile(certFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read certificate: %w", err)
	}
	
	if !certPool.AppendCertsFromPEM(ca) {
		return nil, fmt.Errorf("failed to append CA certificate")
	}
	
	// Configure TLS for optimal performance while maintaining security
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    certPool,
		MinVersion:   tls.VersionTLS12,                    // TLS 1.2 minimum for security
		MaxVersion:   tls.VersionTLS13,                    // TLS 1.3 for improved performance
		CipherSuites: optimalCipherSuites(),              // Optimized cipher suites
		
		// Performance optimizations
		SessionTicketsDisabled: false,                    // Enable session resumption
		ClientSessionCache:     tls.NewLRUClientSessionCache(64), // Cache sessions
		
		// Certificate validation function for attestation verification
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			if len(verifiedChains) == 0 || len(verifiedChains[0]) == 0 {
				return fmt.Errorf("no verified chains")
			}
			
			cert := verifiedChains[0][0]
			
			// Check if attestation data is present in certificate extensions
			for _, ext := range cert.Extensions {
				// Look for our custom OID for attestation data
				if ext.Id.Equal(attestationOID) {
					// In production, validate attestation data here
					// For now, just check it exists
					if len(ext.Value) > 0 {
						return nil
					}
				}
			}
			
			// For backward compatibility, don't fail if attestation isn't present yet
			return nil
		},
	}, nil
}

// attestationOID is the OID for attestation data in certificates
var attestationOID = []int{1, 3, 6, 1, 4, 1, 0, 1, 2, 3} // Example OID

// optimalCipherSuites returns the optimal cipher suites for performance and security
func optimalCipherSuites() []uint16 {
	return []uint16{
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
		tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
	}
}

// min returns the smaller of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// abs returns the absolute value of an integer
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// SyncState synchronizes state for an object with another TEE
func (m *TeeMeshService) SyncState(ctx context.Context, teeID string, objectID string) error {
	// Check if the peer exists
	peer, exists := m.GetPeer(teeID)
	if !exists {
		return fmt.Errorf("peer %s does not exist", teeID)
	}
	
	// Make sure we have a client connection to the peer
	if peer.MeshClient == nil {
		return fmt.Errorf("no client connection to peer %s", teeID)
	}
	
	// Get our current state for calculating deltas
	previousState, err := m.stateManager.GetState(objectID)
	if err != nil {
		// State doesn't exist yet, that's okay for initial sync
		fmt.Printf("Warning: No local state found for object %s, will request full state\n", objectID)
		previousState = nil
	}
	
	// Create sync request
	req := &proto.SyncRequest{
		SenderId:     m.teeID,
		ObjectId:     objectID,
		StateHash:    previousState, // Use our current state as hash for comparison
		TimestampNs:  time.Now().UnixNano(),
	}
	
	// Send sync request
	resp, err := peer.MeshClient.Sync(ctx, req)
	if err != nil {
		return fmt.Errorf("sync request failed: %w", err)
	}
	
	// Process sync response
	err = m.handleReceivedSync(resp)
	if err != nil {
		// If handling fails, log detailed error information for debugging
		var deltaErr *DeltaUpdateError
		if errors.As(err, &deltaErr) {
			fmt.Printf("Sync handling failed with delta error: %v\nOperation: %s, ObjectID: %s\n", 
				deltaErr.Err, deltaErr.Operation, deltaErr.ObjectID)
			
			// For certain delta errors, we might want to retry with full state
			if deltaErr.Operation == "apply" {
				fmt.Println("Retrying with full state request...")
				
				// Request full state
				retryReq := &proto.SyncRequest{
					SenderId:     m.teeID,
					ObjectId:     objectID,
					StateHash:    nil,
					TimestampNs:  time.Now().UnixNano(),
				}
				
				retryResp, retryErr := peer.MeshClient.Sync(ctx, retryReq)
				if retryErr != nil {
					return fmt.Errorf("retry sync failed: %w", retryErr)
				}
				
				// Process the full state response
				if retryResp.StateHash != nil {
					err = m.stateManager.SetState(objectID, retryResp.StateHash)
					if err != nil {
						return fmt.Errorf("failed to save full state after retry: %w", err)
					}
				}
			}
		}
		
		return fmt.Errorf("failed to process sync response: %w", err)
	}
	
	// Notify the execution handler that state has been updated
	// This is an optional step that lets the execution handler know that state has changed
	if m.executionHandler != nil {
		// In a real implementation, you'd want to create a notification mechanism
		// Here we'll just simulate it with a direct execution request
		state, _ := m.stateManager.GetState(objectID)
		if state != nil {
			notifyReq := &proto.DirectExecutionRequest{
				SenderId:     m.teeID,
				IdTo:         m.teeID,  // Self-notification
				FunctionCall: "OnStateUpdated",  // Special function name indicating state update
				Parameters:   state,    // Pass state as parameters
			}
			
			// Execute the notification in a non-blocking way
			go func() {
				_, err := m.DirectExecute(context.Background(), notifyReq)
				if err != nil {
					fmt.Printf("Failed to notify execution handler about state update: %v\n", err)
				}
			}()
		}
	}
	
	return nil
}

func (m *TeeMeshService) getPeer(teeID string) (*Peer, error) {
	m.peerMutex.RLock()
	defer m.peerMutex.RUnlock()
	
	peer, exists := m.peers[teeID]
	if !exists {
		return nil, fmt.Errorf("peer %s not found", teeID)
	}
	
	return peer, nil
}

// peerClient returns a mesh client for a given peer ID
func (m *TeeMeshService) peerClient(peerID string) proto.TeeMeshClient {
	m.peerMutex.RLock()
	defer m.peerMutex.RUnlock()
	
	peer, exists := m.peers[peerID]
	if !exists || peer.MeshClient == nil {
		// Log error but don't panic - return nil and let caller handle it
		fmt.Printf("Warning: No client connection available for peer %s\n", peerID)
		return nil
	}
	
	return peer.MeshClient
}
