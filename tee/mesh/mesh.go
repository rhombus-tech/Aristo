// Package mesh provides a mesh network for TEE-to-TEE communication
package mesh

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"sort"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/rhombus-tech/vm/tee/proto"
)

// MeshService implements the TeeMesh service for direct TEE-to-TEE communication
type MeshService struct {
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
}

// Peer represents a remote TEE in the mesh
type Peer struct {
	TEEID     string
	TEEType   string
	Endpoint  string
	RegionID  string
	Status    string
	LastSeen  time.Time
	MeshClient proto.TeeMeshClient
	Conn      *grpc.ClientConn
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

// NewMeshService creates a new mesh service
func NewMeshService(config *MeshConfig) (*MeshService, error) {
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
	
	return &MeshService{
		teeID:           config.TEEID,
		teeType:         config.TEEType,
		regionID:        config.RegionID,
		endpoint:        config.Endpoint,
		tlsCert:         config.TLSCert,
		tlsKey:          config.TLSKey,
		tlsConfig:       tlsConfig,
		peers:           make(map[string]*Peer),
		executionHandler: config.Handler,
	}, nil
}

// Start starts the mesh service
func (m *MeshService) Start() error {
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
	
	return nil
}

// Stop stops the mesh service
func (m *MeshService) Stop() {
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
func (m *MeshService) Discover(ctx context.Context, req *proto.DiscoveryRequest) (*proto.DiscoveryResponse, error) {
	// Register the requesting TEE as a peer
	peer := &Peer{
		TEEID:    req.TeeId,
		TEEType:  req.TeeType,
		Endpoint: req.Endpoint,
		RegionID: req.RegionId,
		Status:   "active",
		LastSeen: time.Now(),
	}
	
	m.addOrUpdatePeer(peer)
	
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
func (m *MeshService) DirectExecute(ctx context.Context, req *proto.DirectExecutionRequest) (*proto.DirectExecutionResponse, error) {
	// Make sure we have a handler
	if m.executionHandler == nil {
		return nil, errors.New("no execution handler configured")
	}
	
	// Execute the request
	start := time.Now()
	resp, err := m.executionHandler.Execute(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("execution failed: %w", err)
	}
	
	// Add network latency
	resp.NetworkLatencyNs = uint64(time.Since(start).Nanoseconds())
	
	return resp, nil
}

// Ping handles ping requests
func (m *MeshService) Ping(ctx context.Context, req *proto.PingRequest) (*proto.PingResponse, error) {
	return &proto.PingResponse{
		ResponderId: m.teeID,
		TimestampNs: time.Now().UnixNano(),
	}, nil
}

// Sync handles state synchronization requests
func (m *MeshService) Sync(ctx context.Context, req *proto.SyncRequest) (*proto.SyncResponse, error) {
	// For now, just return a simple response
	// In a real implementation, this would sync state
	return &proto.SyncResponse{
		Success:     true,
		StateHash:   req.StateHash,
		TimestampNs: time.Now().UnixNano(),
	}, nil
}

// ConnectToPeer establishes a connection to a peer
func (m *MeshService) ConnectToPeer(peerID, endpoint string) (*Peer, error) {
	// Check if we already have a connection
	m.peerMutex.RLock()
	if peer, exists := m.peers[peerID]; exists && peer.Conn != nil {
		m.peerMutex.RUnlock()
		return peer, nil
	}
	m.peerMutex.RUnlock()
	
	// Create a new connection
	var opts []grpc.DialOption
	
	// If TLS is configured, use it
	if m.tlsConfig != nil {
		creds := credentials.NewTLS(m.tlsConfig)
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		opts = append(opts, grpc.WithInsecure())
	}
	
	conn, err := grpc.Dial(endpoint, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to dial peer: %w", err)
	}
	
	// Create a client
	client := proto.NewTeeMeshClient(conn)
	
	// Create a peer
	peer := &Peer{
		TEEID:      peerID,
		Endpoint:   endpoint,
		LastSeen:   time.Now(),
		MeshClient: client,
		Conn:       conn,
	}
	
	// Add the peer
	m.addOrUpdatePeer(peer)
	
	return peer, nil
}

// addOrUpdatePeer adds or updates a peer
func (m *MeshService) addOrUpdatePeer(peer *Peer) {
	m.peerMutex.Lock()
	defer m.peerMutex.Unlock()
	
	// If the peer already exists, update it
	if existing, exists := m.peers[peer.TEEID]; exists {
		existing.LastSeen = time.Now()
		existing.Status = peer.Status
		
		// Only update other fields if they are set
		if peer.TEEType != "" {
			existing.TEEType = peer.TEEType
		}
		
		if peer.RegionID != "" {
			existing.RegionID = peer.RegionID
		}
		
		if peer.Endpoint != "" {
			existing.Endpoint = peer.Endpoint
		}
		
		if peer.MeshClient != nil {
			existing.MeshClient = peer.MeshClient
		}
		
		if peer.Conn != nil {
			existing.Conn = peer.Conn
		}
	} else {
		// Otherwise, add a new peer
		m.peers[peer.TEEID] = peer
	}
}

// GetPeer gets a peer by ID
func (m *MeshService) GetPeer(peerID string) (*Peer, bool) {
	m.peerMutex.RLock()
	defer m.peerMutex.RUnlock()
	
	peer, exists := m.peers[peerID]
	return peer, exists
}

// GetTEEID returns the ID of this TEE in the mesh
func (m *MeshService) GetTEEID() string {
	return m.teeID
}

// GetTEEType returns the TEE type for this service
func (s *MeshService) GetTEEType() string {
    return s.teeType
}

// createTLSConfig creates a TLS configuration from cert and key files
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
	
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    certPool,
	}, nil
}

// ProxyExecute handles execution requests with automatic failover
func (m *MeshService) ProxyExecute(ctx context.Context, req *proto.ProxyExecutionRequest) (*proto.DirectExecutionResponse, error) {
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
				return resp, nil
			}
			
			// If we reach here, there was an error executing on the target peer
			fmt.Printf("Failed to execute on target peer %s: %v\n", req.IdTo, err)
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
	
	// Sort peers by preferred TEE type
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
			
			return false
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
			return resp, nil
		}
		
		fmt.Printf("Failed to execute on failover peer %s: %v\n", peer.TEEID, err)
		
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

// min returns the smaller of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
