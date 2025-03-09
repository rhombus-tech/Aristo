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
