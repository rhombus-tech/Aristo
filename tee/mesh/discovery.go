// Package mesh provides TEE mesh network functionality
package mesh

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rhombus-tech/vm/tee/accumulator"
	pb "github.com/rhombus-tech/vm/tee/proto"
	"google.golang.org/grpc"
)

// DiscoveryConfig holds configuration for the discovery service
type DiscoveryConfig struct {
	// TEEID is the ID of the local TEE
	TEEID string
	
	// TEEType is the type of the local TEE (SGX, SEV)
	TEEType string
	
	// RegionID is the region this TEE belongs to
	RegionID string
	
	// Endpoint is the network endpoint of this TEE
	Endpoint string
	
	// HeartbeatInterval is how often to send heartbeats
	HeartbeatInterval time.Duration
	
	// RefreshInterval is how often to refresh the peer list
	RefreshInterval time.Duration
	
	// DeadThreshold is how many missed heartbeats before considering a TEE dead
	DeadThreshold int
}

// DefaultDiscoveryConfig returns a default configuration
func DefaultDiscoveryConfig() *DiscoveryConfig {
	return &DiscoveryConfig{
		HeartbeatInterval: 30 * time.Second,
		RefreshInterval:   5 * time.Minute,
		DeadThreshold:     3,
	}
}

// PeerInfo stores information about a peer TEE
type PeerInfo struct {
	// TEEID is the ID of the peer
	TEEID string
	
	// TEEType is the type of the peer
	TEEType string
	
	// RegionID is the region of the peer
	RegionID string
	
	// Endpoint is the network endpoint of the peer
	Endpoint string
	
	// Status is the current status of the peer
	Status string
	
	// LastSeen is when we last received a heartbeat from this peer
	LastSeen time.Time
	
	// Client is the gRPC client for this peer
	Client pb.TeeMeshClient
	
	// Connection is the gRPC connection to this peer
	Connection *grpc.ClientConn
	
	// Attestation is the most recent attestation witness from this peer
	Attestation *pb.AccumulatorWitness
}

// DiscoveryService manages the discovery of peers in the mesh
type DiscoveryService struct {
	// config is the configuration for this service
	config *DiscoveryConfig
	
	// peers is a map of peer ID to peer information
	peers map[string]*PeerInfo
	
	// accClient is the client for the accumulator
	accClient *accumulator.Client
	
	// localWitness is the attestation witness for the local TEE
	localWitness *pb.AccumulatorWitness
	
	// mutex protects access to the peers map
	mutex sync.RWMutex
	
	// ctx is the context for background operations
	ctx context.Context
	
	// cancel is the cancel function for the context
	cancel context.CancelFunc
}

// NewDiscoveryService creates a new discovery service
func NewDiscoveryService(config *DiscoveryConfig) (*DiscoveryService, error) {
	if config == nil {
		config = DefaultDiscoveryConfig()
	}
	
	// Validate config
	if config.TEEID == "" {
		return nil, fmt.Errorf("TEEID is required")
	}
	
	if config.TEEType == "" {
		return nil, fmt.Errorf("TEEType is required")
	}
	
	if config.RegionID == "" {
		return nil, fmt.Errorf("RegionID is required")
	}
	
	if config.Endpoint == "" {
		return nil, fmt.Errorf("Endpoint is required")
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	// Create the accumulator client
	accClient := accumulator.NewClient(config.TEEID, config.TEEType)
	
	return &DiscoveryService{
		config:     config,
		peers:      make(map[string]*PeerInfo),
		accClient:  accClient,
		ctx:        ctx,
		cancel:     cancel,
	}, nil
}

// Start starts the discovery service
func (d *DiscoveryService) Start() error {
	// Refresh the accumulator
	err := d.accClient.RefreshAccumulator(d.ctx)
	if err != nil {
		return fmt.Errorf("failed to refresh accumulator: %w", err)
	}
	
	// Get the local witness
	localWitness, err := d.accClient.GetLocalWitness(d.ctx)
	if err != nil {
		return fmt.Errorf("failed to get local witness: %w", err)
	}
	d.localWitness = localWitness
	
	// Start the heartbeat goroutine
	go d.heartbeatLoop()
	
	// Start the refresh goroutine
	go d.refreshLoop()
	
	return nil
}

// Stop stops the discovery service and closes all connections
func (d *DiscoveryService) Stop() {
	d.cancel()
	
	// Close all connections
	d.mutex.Lock()
	defer d.mutex.Unlock()
	
	for id, peer := range d.peers {
		if peer.Connection != nil {
			peer.Connection.Close()
		}
		delete(d.peers, id)
	}
}

// heartbeatLoop periodically sends heartbeats to all peers
func (d *DiscoveryService) heartbeatLoop() {
	ticker := time.NewTicker(d.config.HeartbeatInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.sendHeartbeats()
		}
	}
}

// refreshLoop periodically refreshes the peer list
func (d *DiscoveryService) refreshLoop() {
	ticker := time.NewTicker(d.config.RefreshInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.refreshPeers()
		}
	}
}

// sendHeartbeats sends heartbeats to all peers
func (d *DiscoveryService) sendHeartbeats() {
	d.mutex.RLock()
	peersCopy := make([]*PeerInfo, 0, len(d.peers))
	for _, peer := range d.peers {
		peersCopy = append(peersCopy, peer)
	}
	d.mutex.RUnlock()
	
	// Refresh our local witness periodically
	if time.Since(time.Unix(int64(d.localWitness.LastUpdate), 0)) > d.config.RefreshInterval {
		localWitness, err := d.accClient.GetLocalWitness(d.ctx)
		if err == nil {
			d.localWitness = localWitness
		}
	}
	
	// Send heartbeats to all peers
	for _, peer := range peersCopy {
		if peer.Client == nil {
			continue
		}
		
		req := &pb.HeartbeatRequest{
			TeeId:      d.config.TEEID,
			TeeType:    d.config.TEEType,
			RegionId:   d.config.RegionID,
			Endpoint:   d.config.Endpoint,
			Timestamp:  time.Now().Format(time.RFC3339Nano),
			Attestation: d.localWitness,
		}
		
		// Use a timeout context for the heartbeat
		ctx, cancel := context.WithTimeout(d.ctx, 5*time.Second)
		resp, err := peer.Client.Heartbeat(ctx, req)
		cancel()
		
		if err != nil {
			// If we failed to send a heartbeat, mark as missed
			d.recordMissedHeartbeat(peer.TEEID)
			continue
		}
		
		// Update the peer's information from the response
		d.updatePeerFromHeartbeat(resp)
	}
}

// refreshPeers refreshes the peer list by checking for dead peers
func (d *DiscoveryService) refreshPeers() {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	
	now := time.Now()
	maxAge := time.Duration(d.config.DeadThreshold) * d.config.HeartbeatInterval
	
	// Check for dead peers
	for id, peer := range d.peers {
		if now.Sub(peer.LastSeen) > maxAge {
			// Close the connection
			if peer.Connection != nil {
				peer.Connection.Close()
			}
			
			// Remove the peer
			delete(d.peers, id)
		}
	}
	
	// Refresh our accumulator
	d.accClient.RefreshAccumulator(d.ctx)
}

// recordMissedHeartbeat records a missed heartbeat for a peer
func (d *DiscoveryService) recordMissedHeartbeat(peerID string) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	
	if peer, ok := d.peers[peerID]; ok {
		// Don't update the LastSeen time, so it will eventually time out
		// if we keep missing heartbeats
		
		// Optionally, we could implement a counter for missed heartbeats
		// and mark as dead after a certain number
		if peer.Status == "active" {
			peer.Status = "degraded"
		}
	}
}

// updatePeerFromHeartbeat updates a peer's information from a heartbeat response
func (d *DiscoveryService) updatePeerFromHeartbeat(resp *pb.HeartbeatResponse) {
	if resp == nil {
		return
	}
	
	d.mutex.Lock()
	defer d.mutex.Unlock()
	
	// If we don't have a record for this peer yet, we need to create a new connection
	peer, ok := d.peers[resp.TeeId]
	if !ok {
		// This is a new peer, we'll add it but won't create a connection yet
		// since we don't have enough information
		peer = &PeerInfo{
			TEEID:    resp.TeeId,
			TEEType:  resp.TeeType,
			RegionID: resp.RegionId,
			Endpoint: resp.Endpoint,
			Status:   "pending", // We'll verify the attestation before marking as active
			LastSeen: time.Now(),
		}
		d.peers[resp.TeeId] = peer
	} else {
		// Update the existing peer
		peer.TEEType = resp.TeeType
		peer.RegionID = resp.RegionId
		peer.Endpoint = resp.Endpoint
		peer.LastSeen = time.Now()
	}
	
	// If we have attestation information, verify it
	if resp.Attestation != nil {
		valid, err := d.accClient.VerifyWitness(resp.Attestation, true)
		if err != nil || !valid {
			// Attestation failed, mark as untrusted
			peer.Status = "untrusted"
			peer.Attestation = nil
		} else {
			// Attestation passed, store it and mark as active
			peer.Status = "active"
			peer.Attestation = resp.Attestation
		}
	}
}

// RegisterPeer adds a new peer to the discovery service
func (d *DiscoveryService) RegisterPeer(peerInfo *PeerInfo) error {
	if peerInfo == nil {
		return fmt.Errorf("peer info is nil")
	}
	
	if peerInfo.TEEID == "" {
		return fmt.Errorf("peer ID is required")
	}
	
	if peerInfo.Endpoint == "" {
		return fmt.Errorf("peer endpoint is required")
	}
	
	// Verify attestation if provided
	if peerInfo.Attestation != nil {
		valid, err := d.accClient.VerifyWitness(peerInfo.Attestation, true)
		if err != nil {
			return fmt.Errorf("failed to verify attestation: %w", err)
		}
		
		if !valid {
			return fmt.Errorf("attestation verification failed")
		}
	}
	
	d.mutex.Lock()
	defer d.mutex.Unlock()
	
	// If we already have this peer, update it
	if peer, ok := d.peers[peerInfo.TEEID]; ok {
		// Update the existing peer
		peer.TEEType = peerInfo.TEEType
		peer.RegionID = peerInfo.RegionID
		peer.Endpoint = peerInfo.Endpoint
		peer.Status = peerInfo.Status
		peer.LastSeen = time.Now()
		peer.Attestation = peerInfo.Attestation
		
		// If the existing peer has a connection, keep it
		// Otherwise, we'll create a new one if the client is provided
		if peer.Connection == nil && peerInfo.Connection != nil {
			peer.Connection = peerInfo.Connection
			peer.Client = peerInfo.Client
		}
	} else {
		// This is a new peer, add it
		peerInfo.LastSeen = time.Now()
		d.peers[peerInfo.TEEID] = peerInfo
	}
	
	return nil
}

// GetPeers returns a list of all peers
func (d *DiscoveryService) GetPeers() []*PeerInfo {
	d.mutex.RLock()
	defer d.mutex.RUnlock()
	
	peers := make([]*PeerInfo, 0, len(d.peers))
	for _, peer := range d.peers {
		peers = append(peers, peer)
	}
	
	return peers
}

// GetPeersByType returns a list of peers of a specific type
func (d *DiscoveryService) GetPeersByType(teeType string) []*PeerInfo {
	d.mutex.RLock()
	defer d.mutex.RUnlock()
	
	peers := make([]*PeerInfo, 0)
	for _, peer := range d.peers {
		if peer.TEEType == teeType && peer.Status == "active" {
			peers = append(peers, peer)
		}
	}
	
	return peers
}

// GetPeer returns information about a specific peer
func (d *DiscoveryService) GetPeer(peerID string) (*PeerInfo, bool) {
	d.mutex.RLock()
	defer d.mutex.RUnlock()
	
	peer, ok := d.peers[peerID]
	return peer, ok
}

// HandleHeartbeat processes a heartbeat request from a peer
func (d *DiscoveryService) HandleHeartbeat(ctx context.Context, req *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("request is nil")
	}
	
	// Create a peer info object from the request
	peerInfo := &PeerInfo{
		TEEID:    req.TeeId,
		TEEType:  req.TeeType,
		RegionID: req.RegionId,
		Endpoint: req.Endpoint,
		Status:   "pending", // Will be updated after attestation verification
		LastSeen: time.Now(),
	}
	
	// If we have attestation information, store it
	if req.Attestation != nil {
		peerInfo.Attestation = req.Attestation
		
		// Verify the attestation
		valid, err := d.accClient.VerifyWitness(req.Attestation, true)
		if err == nil && valid {
			peerInfo.Status = "active"
		} else {
			peerInfo.Status = "untrusted"
		}
	}
	
	// Register the peer
	err := d.RegisterPeer(peerInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to register peer: %w", err)
	}
	
	// Create a response with our information
	resp := &pb.HeartbeatResponse{
		TeeId:      d.config.TEEID,
		TeeType:    d.config.TEEType,
		RegionId:   d.config.RegionID,
		Endpoint:   d.config.Endpoint,
		Timestamp:  time.Now().Format(time.RFC3339Nano),
		Attestation: d.localWitness,
	}
	
	return resp, nil
}

// HandleGetPeers processes a get peers request
func (d *DiscoveryService) HandleGetPeers(ctx context.Context, req *pb.GetPeersRequest) (*pb.GetPeersResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("request is nil")
	}
	
	// Get the list of peers, filtered by type if specified
	var peerList []*PeerInfo
	if req.TeeType != "" {
		peerList = d.GetPeersByType(req.TeeType)
	} else {
		peerList = d.GetPeers()
	}
	
	// Convert to proto format
	peers := make([]*pb.Peer, 0, len(peerList))
	for _, peer := range peerList {
		// Only include active peers
		if peer.Status == "active" {
			peers = append(peers, &pb.Peer{
				TeeId:      peer.TEEID,
				TeeType:    peer.TEEType,
				RegionId:   peer.RegionID,
				Endpoint:   peer.Endpoint,
				Status:     peer.Status,
				Attestation: peer.Attestation,
			})
		}
	}
	
	return &pb.GetPeersResponse{
		Peers: peers,
	}, nil
}

// ConnectToPeer establishes a connection to a peer
func (d *DiscoveryService) ConnectToPeer(peerID string) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	
	peer, ok := d.peers[peerID]
	if !ok {
		return fmt.Errorf("peer not found: %s", peerID)
	}
	
	// If we already have a connection, return
	if peer.Connection != nil && peer.Client != nil {
		return nil
	}
	
	// Create a connection to the peer
	conn, err := grpc.Dial(peer.Endpoint, grpc.WithInsecure())
	if err != nil {
		return fmt.Errorf("failed to connect to peer: %w", err)
	}
	
	// Create a client
	client := pb.NewTeeMeshClient(conn)
	
	// Update the peer
	peer.Connection = conn
	peer.Client = client
	
	return nil
}

// DisconnectFromPeer closes the connection to a peer
func (d *DiscoveryService) DisconnectFromPeer(peerID string) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	
	peer, ok := d.peers[peerID]
	if !ok {
		return fmt.Errorf("peer not found: %s", peerID)
	}
	
	// Close the connection if it exists
	if peer.Connection != nil {
		err := peer.Connection.Close()
		if err != nil {
			return fmt.Errorf("failed to close connection: %w", err)
		}
		
		peer.Connection = nil
		peer.Client = nil
	}
	
	return nil
}

// GetLocalWitness returns the attestation witness for the local TEE
func (d *DiscoveryService) GetLocalWitness() *pb.AccumulatorWitness {
	return d.localWitness
}
