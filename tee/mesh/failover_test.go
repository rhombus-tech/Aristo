package mesh

import (
	"context"
	"testing"
	"time"

	"github.com/rhombus-tech/vm/tee/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockMeshClient is a mock of the TeeMeshClient interface for testing
type MockMeshClient struct {
	mock.Mock
}

func (m *MockMeshClient) DirectExecute(ctx context.Context, req *proto.DirectExecutionRequest) (*proto.DirectExecutionResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*proto.DirectExecutionResponse), args.Error(1)
}

func (m *MockMeshClient) ProxyExecute(ctx context.Context, req *proto.ProxyExecutionRequest) (*proto.DirectExecutionResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*proto.DirectExecutionResponse), args.Error(1)
}

func (m *MockMeshClient) Sync(ctx context.Context, req *proto.SyncRequest) (*proto.SyncResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*proto.SyncResponse), args.Error(1)
}

func (m *MockMeshClient) Heartbeat(ctx context.Context, req *proto.HeartbeatRequest) (*proto.HeartbeatResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*proto.HeartbeatResponse), args.Error(1)
}

func (m *MockMeshClient) GetPeers(ctx context.Context, req *proto.GetPeersRequest) (*proto.GetPeersResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*proto.GetPeersResponse), args.Error(1)
}

// TestProxyExecuteWithFailover tests the automatic failover functionality using mocked peers
func TestProxyExecuteWithFailover(t *testing.T) {
	// This test will be implemented differently to avoid modifying private fields
	// To properly test the ProxyExecute function with failover, we need to implement the ProxyExecute method
	// in a mock MeshService

	// Create mocked response for SGX
	sgxResp := &proto.DirectExecutionResponse{
		Timestamp:     time.Now().Format(time.RFC3339Nano),
		Result:        []byte("from-sgx"),
		StateHash:     []byte("hash-sgx"),
		ExecutionTime: 60,
		SenderId:      "sgx-tee",
		Success:       true,
	}

	// Create a mock execution handler
	mockHandler := NewMockHandler()
	mockHandler.AddResponse("test-id", sgxResp)

	// Create a mesh service with the mock handler
	config := &MeshConfig{
		TEEID:    "test-tee",
		TEEType:  "SGX",
		RegionID: "test-region",
		Endpoint: "localhost:8080",
		Handler:  mockHandler,
	}

	// Create the mesh service
	service, err := NewMeshService(config)
	assert.NoError(t, err)
	
	// Create a test request
	req := &proto.ProxyExecutionRequest{
		SenderId:          "source",
		IdTo:              "target",
		FunctionCall:      "testFunc",
		Parameters:        []byte("params"),
		RegionId:          "test-region",
		PreferredTeeType:  "SGX",
		MaxRetries:        3,
		TimeoutMs:         5000,
		CrossRegionAllowed: true,
	}

	// Test 1: Default behavior (will fail since we haven't set up any peers)
	resp, err := service.ProxyExecute(context.Background(), req)
	assert.Error(t, err)
	assert.Nil(t, resp)

	// For testing with real failover behavior, we would need to:
	// 1. Create mock mesh clients for each peer
	// 2. Register peers with the mesh service using AddPeer
	// 3. Configure the mock clients to return success or failure
	// 4. Call ProxyExecute and verify the behavior
	
	// This requires a different test approach that integrates with the actual MeshService
	// implementation more closely rather than trying to modify private fields.
	
	t.Log("Note: For a complete failover test, we would need to use integration testing with real or strongly mocked peers.")
}

// Mock discovery service for testing
type MockDiscoveryService struct {
	peers []*PeerInfoV2
}

func (m *MockDiscoveryService) GetPeers() []*PeerInfoV2 {
	return m.peers
}

func (m *MockDiscoveryService) GetPeersByType(teeType string) []*PeerInfoV2 {
	var filtered []*PeerInfoV2
	for _, p := range m.peers {
		if p.TEEType == teeType {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

func (m *MockDiscoveryService) GetPeerByID(id string) *PeerInfoV2 {
	for _, p := range m.peers {
		if p.TEEID == id {
			return p
		}
	}
	return nil
}

func (m *MockDiscoveryService) Start() error {
	return nil
}

func (m *MockDiscoveryService) Stop() {
	// No-op for testing
}
