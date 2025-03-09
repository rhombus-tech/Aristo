package mesh

import (
	"context"
	"testing"
	"time"

	"github.com/rhombus-tech/vm/tee/proto"
)

func TestDiscoveryService(t *testing.T) {
	// Create a discovery service
	config := &DiscoveryConfig{
		TEEID:            "test-tee",
		TEEType:          "SGX",
		RegionID:         "test-region",
		Endpoint:         "localhost:50051",
		HeartbeatInterval: time.Millisecond * 100,
		RefreshInterval:   time.Millisecond * 200,
		DeadThreshold:     2,
	}
	
	discovery, err := NewDiscoveryService(config)
	if err != nil {
		t.Fatalf("Failed to create discovery service: %v", err)
	}
	
	// Start the discovery service
	err = discovery.Start()
	if err != nil {
		t.Fatalf("Failed to start discovery service: %v", err)
	}
	
	// Cleanup at the end
	defer discovery.Stop()
	
	// Test that the local witness was created
	localWitness := discovery.GetLocalWitness()
	if localWitness == nil {
		t.Fatal("Local witness is nil")
	}
	
	// Test registering a peer
	peerInfo := &PeerInfo{
		TEEID:    "peer1",
		TEEType:  "SEV",
		RegionID: "test-region",
		Endpoint: "localhost:50052",
		Status:   "active",
	}
	
	err = discovery.RegisterPeer(peerInfo)
	if err != nil {
		t.Fatalf("Failed to register peer: %v", err)
	}
	
	// Override the existing peer's status to active for testing
	peer1, found := discovery.GetPeer("peer1") 
	if !found {
		t.Fatal("Failed to find peer1 after registration")
	}
	peer1.Status = "active"
	
	// Register a new SGX peer directly
	sgxPeer := &PeerInfo{
		TEEID:    "peer2-sgx",
		TEEType:  "SGX",
		RegionID: "test-region",
		Endpoint: "localhost:50053",
		Status:   "active", // Set as active for testing
	}
	err = discovery.RegisterPeer(sgxPeer)
	if err != nil {
		t.Fatalf("Failed to register SGX peer: %v", err)
	}
	
	// Verify the peers were added
	peers := discovery.GetPeers()
	if len(peers) != 2 {
		t.Fatalf("Expected 2 peers, got %d", len(peers))
	}
	
	// Test getting peers by type
	sgxPeers := discovery.GetPeersByType("SGX")
	if len(sgxPeers) != 1 {
		t.Fatalf("Expected 1 active SGX peer, got %d", len(sgxPeers))
	}
	
	sevPeers := discovery.GetPeersByType("SEV")
	if len(sevPeers) != 1 {
		t.Fatalf("Expected 1 active SEV peer, got %d", len(sevPeers))
	}
	
	// Test getting peers via gRPC
	getReq := &proto.GetPeersRequest{
		TeeType: "",
	}
	
	getResp, err := discovery.HandleGetPeers(context.Background(), getReq)
	if err != nil {
		t.Fatalf("Failed to handle get peers: %v", err)
	}
	
	if len(getResp.Peers) != 2 {
		t.Fatalf("Expected 2 active peers in response, got %d", len(getResp.Peers))
	}
}

func TestDiscoveryConfig(t *testing.T) {
	// Test default config
	config := DefaultDiscoveryConfig()
	
	if config.HeartbeatInterval != 30*time.Second {
		t.Fatalf("Expected heartbeat interval 30s, got %v", config.HeartbeatInterval)
	}
	
	if config.RefreshInterval != 5*time.Minute {
		t.Fatalf("Expected refresh interval 5m, got %v", config.RefreshInterval)
	}
	
	if config.DeadThreshold != 3 {
		t.Fatalf("Expected dead threshold 3, got %d", config.DeadThreshold)
	}
}

func TestDiscoveryServiceValidation(t *testing.T) {
	// Test with missing TEEID
	config := &DiscoveryConfig{
		TEEType:  "SGX",
		RegionID: "test-region",
		Endpoint: "localhost:50051",
	}
	
	_, err := NewDiscoveryService(config)
	if err == nil {
		t.Fatal("Expected error with missing TEEID")
	}
	
	// Test with missing TEEType
	config = &DiscoveryConfig{
		TEEID:    "test-tee",
		RegionID: "test-region",
		Endpoint: "localhost:50051",
	}
	
	_, err = NewDiscoveryService(config)
	if err == nil {
		t.Fatal("Expected error with missing TEEType")
	}
	
	// Test with missing RegionID
	config = &DiscoveryConfig{
		TEEID:    "test-tee",
		TEEType:  "SGX",
		Endpoint: "localhost:50051",
	}
	
	_, err = NewDiscoveryService(config)
	if err == nil {
		t.Fatal("Expected error with missing RegionID")
	}
	
	// Test with missing Endpoint
	config = &DiscoveryConfig{
		TEEID:    "test-tee",
		TEEType:  "SGX",
		RegionID: "test-region",
	}
	
	_, err = NewDiscoveryService(config)
	if err == nil {
		t.Fatal("Expected error with missing Endpoint")
	}
}
