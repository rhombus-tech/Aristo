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
	// Test creating a mesh service with valid config for SGX
	handler := NewMockHandler()
	sgxConfig := &MeshConfig{
		TEEID:    "test-tee-sgx",
		TEEType:  "SGX",
		RegionID: "us-west",
		Endpoint: "localhost:50051",
		Handler:  handler,
	}

	sgxService, err := NewMeshService(sgxConfig)
	if err != nil {
		t.Fatalf("Failed to create SGX mesh service: %v", err)
	}

	if sgxService.teeID != sgxConfig.TEEID {
		t.Errorf("Expected TEEID %s, got %s", sgxConfig.TEEID, sgxService.teeID)
	}

	if sgxService.teeType != sgxConfig.TEEType {
		t.Errorf("Expected TEEType %s, got %s", sgxConfig.TEEType, sgxService.teeType)
	}
	
	// Test creating a mesh service with valid config for SEV
	sevConfig := &MeshConfig{
		TEEID:    "test-tee-sev",
		TEEType:  "SEV",
		RegionID: "us-west",
		Endpoint: "localhost:50052",
		Handler:  handler,
	}

	sevService, err := NewMeshService(sevConfig)
	if err != nil {
		t.Fatalf("Failed to create SEV mesh service: %v", err)
	}

	if sevService.teeID != sevConfig.TEEID {
		t.Errorf("Expected TEEID %s, got %s", sevConfig.TEEID, sevService.teeID)
	}

	if sevService.teeType != sevConfig.TEEType {
		t.Errorf("Expected TEEType %s, got %s", sevConfig.TEEType, sevService.teeType)
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
	
	// Add specific responses for test objects for both SGX and SEV
	sgxResp := &proto.DirectExecutionResponse{
		Timestamp:        time.Now().Format(time.RFC3339Nano),
		Result:           []byte("sgx result"),
		StateHash:        []byte("sgx hash"),
		ExecutionTime:    200,
		MemoryUsed:       2048,
		SyscallCount:     10,
		NetworkLatencyNs: 100000,
	}
	handler.AddResponse("test-object-sgx", sgxResp)
	
	sevResp := &proto.DirectExecutionResponse{
		Timestamp:        time.Now().Format(time.RFC3339Nano),
		Result:           []byte("sev result"),
		StateHash:        []byte("sev hash"),
		ExecutionTime:    180,
		MemoryUsed:       1024,
		SyscallCount:     8,
		NetworkLatencyNs: 90000,
	}
	handler.AddResponse("test-object-sev", sevResp)
	
	// Test SGX execution
	t.Run("SGX", func(t *testing.T) {
		// Create an SGX mesh service
		sgxConfig := &MeshConfig{
			TEEID:    "test-tee-sgx",
			TEEType:  "SGX",
			RegionID: "us-west",
			Endpoint: "localhost:50051",
			Handler:  handler,
		}
		
		sgxService, err := NewMeshService(sgxConfig)
		if err != nil {
			t.Fatalf("Failed to create SGX mesh service: %v", err)
		}
		
		// Test direct execution with the test object
		req := &proto.DirectExecutionRequest{
			SenderId:     "test-client",
			IdTo:         "test-object-sgx",
			FunctionCall: "test-function",
			Parameters:   []byte("test-params"),
			RegionId:     "us-west",
		}
		
		resp, err := sgxService.DirectExecute(context.Background(), req)
		if err != nil {
			t.Fatalf("SGX DirectExecute failed: %v", err)
		}
		
		// Check that we got the expected response
		if string(resp.Result) != string(sgxResp.Result) {
			t.Errorf("Expected SGX result %s, got %s", string(sgxResp.Result), string(resp.Result))
		}
	})
	
	// Test SEV execution
	t.Run("SEV", func(t *testing.T) {
		// Create an SEV mesh service
		sevConfig := &MeshConfig{
			TEEID:    "test-tee-sev",
			TEEType:  "SEV",
			RegionID: "us-west",
			Endpoint: "localhost:50052",
			Handler:  handler,
		}
		
		sevService, err := NewMeshService(sevConfig)
		if err != nil {
			t.Fatalf("Failed to create SEV mesh service: %v", err)
		}
		
		// Test direct execution with the test object
		req := &proto.DirectExecutionRequest{
			SenderId:     "test-client",
			IdTo:         "test-object-sev",
			FunctionCall: "test-function",
			Parameters:   []byte("test-params"),
			RegionId:     "us-west",
		}
		
		resp, err := sevService.DirectExecute(context.Background(), req)
		if err != nil {
			t.Fatalf("SEV DirectExecute failed: %v", err)
		}
		
		// Check that we got the expected response
		if string(resp.Result) != string(sevResp.Result) {
			t.Errorf("Expected SEV result %s, got %s", string(sevResp.Result), string(resp.Result))
		}
	})
}

func TestMultiTEETypeSupport(t *testing.T) {
	// Create handlers for SGX and SEV
	sgxHandler := NewMockHandler()
	sevHandler := NewMockHandler()
	
	// Configure responses
	sgxResp := &proto.DirectExecutionResponse{
		Timestamp:     time.Now().Format(time.RFC3339Nano),
		Result:        []byte("sgx executed"),
		StateHash:     []byte("sgx hash"),
		ExecutionTime: 100,
	}
	sgxHandler.AddResponse("test-contract", sgxResp)
	
	sevResp := &proto.DirectExecutionResponse{
		Timestamp:     time.Now().Format(time.RFC3339Nano),
		Result:        []byte("sev executed"),
		StateHash:     []byte("sev hash"),
		ExecutionTime: 120,
	}
	sevHandler.AddResponse("test-contract", sevResp)
	
	// Create mesh services for SGX and SEV
	sgxService, err := NewMeshService(&MeshConfig{
		TEEID:    "sgx-tee",
		TEEType:  "SGX",
		RegionID: "test-region",
		Endpoint: "localhost:50151",
		Handler:  sgxHandler,
	})
	if err != nil {
		t.Fatalf("Failed to create SGX service: %v", err)
	}
	
	sevService, err := NewMeshService(&MeshConfig{
		TEEID:    "sev-tee",
		TEEType:  "SEV",
		RegionID: "test-region",
		Endpoint: "localhost:50152",
		Handler:  sevHandler,
	})
	if err != nil {
		t.Fatalf("Failed to create SEV service: %v", err)
	}
	
	// Test that each service properly identifies its own TEE type
	if sgxService.GetTEEType() != "SGX" {
		t.Errorf("Expected SGX service to have TEE type SGX, got %s", sgxService.GetTEEType())
	}
	
	if sevService.GetTEEType() != "SEV" {
		t.Errorf("Expected SEV service to have TEE type SEV, got %s", sevService.GetTEEType())
	}
	
	// Test DirectExecute for SGX
	sgxReq := &proto.DirectExecutionRequest{
		SenderId:     "test-client",
		IdTo:         "test-contract",
		FunctionCall: "test-function",
		Parameters:   []byte("test-params"),
		RegionId:     "test-region",
	}
	
	sgxResult, err := sgxService.DirectExecute(context.Background(), sgxReq)
	if err != nil {
		t.Fatalf("Failed to execute on SGX: %v", err)
	}
	
	if string(sgxResult.Result) != string(sgxResp.Result) {
		t.Errorf("Expected SGX result %s, got %s", string(sgxResp.Result), string(sgxResult.Result))
	}
	
	// Test DirectExecute for SEV
	sevReq := &proto.DirectExecutionRequest{
		SenderId:     "test-client", 
		IdTo:         "test-contract",
		FunctionCall: "test-function",
		Parameters:   []byte("test-params"),
		RegionId:     "test-region",
	}
	
	sevResult, err := sevService.DirectExecute(context.Background(), sevReq)
	if err != nil {
		t.Fatalf("Failed to execute on SEV: %v", err)
	}
	
	if string(sevResult.Result) != string(sevResp.Result) {
		t.Errorf("Expected SEV result %s, got %s", string(sevResp.Result), string(sevResult.Result))
	}
}
