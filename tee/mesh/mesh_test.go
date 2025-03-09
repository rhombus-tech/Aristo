package mesh

import (
	"context"
	"testing"
	"time"

	"github.com/rhombus-tech/vm/tee/proto"
)

// MockExecutionHandler is a mock implementation of ExecutionHandler for testing
type MockExecutionHandler struct {
	responses map[string]*proto.DirectExecutionResponse
}

// Execute implements the ExecutionHandler interface for testing
func (m *MockExecutionHandler) Execute(ctx context.Context, req *proto.DirectExecutionRequest) (*proto.DirectExecutionResponse, error) {
	// If we have a predefined response for this ID, return it
	if resp, ok := m.responses[req.IdTo]; ok {
		return resp, nil
	}

	// Otherwise, return a default response
	return &proto.DirectExecutionResponse{
		Timestamp:        time.Now().Format(time.RFC3339Nano),
		Result:           []byte("test result"),
		StateHash:        []byte("test hash"),
		ExecutionTime:    100, // milliseconds
		MemoryUsed:       1024, // bytes
		SyscallCount:     5,
		NetworkLatencyNs: 50000, // 50 microseconds
	}, nil
}

// NewMockHandler creates a new mock execution handler for testing
func NewMockHandler() *MockExecutionHandler {
	return &MockExecutionHandler{
		responses: make(map[string]*proto.DirectExecutionResponse),
	}
}

// AddResponse adds a predefined response for a specific object ID
func (m *MockExecutionHandler) AddResponse(objectID string, resp *proto.DirectExecutionResponse) {
	m.responses[objectID] = resp
}

func TestNewMeshService(t *testing.T) {
	// Test creating a mesh service with valid config
	handler := NewMockHandler()
	config := &MeshConfig{
		TEEID:    "test-tee",
		TEEType:  "SGX",
		RegionID: "us-west",
		Endpoint: "localhost:50051",
		Handler:  handler,
	}

	service, err := NewMeshService(config)
	if err != nil {
		t.Fatalf("Failed to create mesh service: %v", err)
	}

	if service.teeID != config.TEEID {
		t.Errorf("Expected TEEID %s, got %s", config.TEEID, service.teeID)
	}

	if service.teeType != config.TEEType {
		t.Errorf("Expected TEEType %s, got %s", config.TEEType, service.teeType)
	}

	if service.regionID != config.RegionID {
		t.Errorf("Expected RegionID %s, got %s", config.RegionID, service.regionID)
	}

	if service.endpoint != config.Endpoint {
		t.Errorf("Expected Endpoint %s, got %s", config.Endpoint, service.endpoint)
	}

	// Test with missing required fields
	invalidConfigs := []*MeshConfig{
		{TEEType: "SGX", RegionID: "us-west", Endpoint: "localhost:50051", Handler: handler}, // Missing TEEID
		{TEEID: "test-tee", RegionID: "us-west", Endpoint: "localhost:50051", Handler: handler}, // Missing TEEType
		{TEEID: "test-tee", TEEType: "SGX", Endpoint: "localhost:50051", Handler: handler}, // Missing RegionID
		{TEEID: "test-tee", TEEType: "SGX", RegionID: "us-west", Handler: handler}, // Missing Endpoint
	}

	for i, config := range invalidConfigs {
		_, err := NewMeshService(config)
		if err == nil {
			t.Errorf("Test %d: Expected error for invalid config, got nil", i)
		}
	}
}

func TestDirectExecute(t *testing.T) {
	// Create a mock handler
	handler := NewMockHandler()
	
	// Add a specific response for a test object
	expectedResp := &proto.DirectExecutionResponse{
		Timestamp:        time.Now().Format(time.RFC3339Nano),
		Result:           []byte("custom result"),
		StateHash:        []byte("custom hash"),
		ExecutionTime:    200,
		MemoryUsed:       2048,
		SyscallCount:     10,
		NetworkLatencyNs: 100000,
	}
	handler.AddResponse("test-object", expectedResp)
	
	// Create a mesh service
	config := &MeshConfig{
		TEEID:    "test-tee",
		TEEType:  "SGX",
		RegionID: "us-west",
		Endpoint: "localhost:50051",
		Handler:  handler,
	}
	
	service, err := NewMeshService(config)
	if err != nil {
		t.Fatalf("Failed to create mesh service: %v", err)
	}
	
	// Test direct execution with the test object
	req := &proto.DirectExecutionRequest{
		SenderId:     "test-client",
		IdTo:         "test-object",
		FunctionCall: "test-function",
		Parameters:   []byte("test-params"),
		RegionId:     "us-west",
	}
	
	resp, err := service.DirectExecute(context.Background(), req)
	if err != nil {
		t.Fatalf("DirectExecute failed: %v", err)
	}
	
	// Check that we got the expected response
	if string(resp.Result) != string(expectedResp.Result) {
		t.Errorf("Expected result %s, got %s", string(expectedResp.Result), string(resp.Result))
	}
	
	if string(resp.StateHash) != string(expectedResp.StateHash) {
		t.Errorf("Expected state hash %s, got %s", string(expectedResp.StateHash), string(resp.StateHash))
	}
	
	// Test with a different object ID
	req.IdTo = "different-object"
	resp, err = service.DirectExecute(context.Background(), req)
	if err != nil {
		t.Fatalf("DirectExecute failed: %v", err)
	}
	
	// Check that we got the default response
	if string(resp.Result) != "test result" {
		t.Errorf("Expected result 'test result', got %s", string(resp.Result))
	}
	
	if string(resp.StateHash) != "test hash" {
		t.Errorf("Expected state hash 'test hash', got %s", string(resp.StateHash))
	}
}
