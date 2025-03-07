// Package mocks provides mock implementations of interfaces for testing
package mocks

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rhombus-tech/vm/tee/proto"
	"github.com/stretchr/testify/mock"
)

// MockComputeNode is a mock implementation of the TeeExecutionServer interface
type MockComputeNode struct {
	mock.Mock
	proto.UnimplementedTeeExecutionServer
	
	// Store events and test objects
	events      map[string]*proto.Event
	testObjects map[string]bool
	mu          sync.Mutex
}

// NewMockComputeNode creates a new MockComputeNode
func NewMockComputeNode() *MockComputeNode {
	return &MockComputeNode{
		events:      make(map[string]*proto.Event),
		testObjects: make(map[string]bool),
	}
}

// DeployContract implements proto.TeeExecutionServer
func (m *MockComputeNode) DeployContract(ctx context.Context, req *proto.DeployContractRequest) (*proto.DeployContractResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) != nil {
		return args.Get(0).(*proto.DeployContractResponse), args.Error(1)
	}
	return nil, args.Error(1)
}

// CallContract implements proto.TeeExecutionServer
func (m *MockComputeNode) CallContract(ctx context.Context, req *proto.CallContractRequest) (*proto.CallContractResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) != nil {
		return args.Get(0).(*proto.CallContractResponse), args.Error(1)
	}
	return nil, args.Error(1)
}

// GetEvent implements the GetEvent RPC method
func (m *MockComputeNode) GetEvent(ctx context.Context, req *proto.GetEventRequest) (*proto.GetEventResponse, error) {
	args := m.Called(ctx, req)
	
	// If there's a mocked response, return it
	if args.Get(0) != nil {
		return args.Get(0).(*proto.GetEventResponse), args.Error(1)
	}

	// Otherwise, handle with our default implementation
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// Create event key that combines event ID and region
	key := fmt.Sprintf("%s:%s", req.EventId, req.RegionId)
	
	event, exists := m.events[key]
	if !exists {
		// Check if we have a direct match without region
		event, exists = m.events[req.EventId]
		if !exists {
			return &proto.GetEventResponse{}, nil
		}
	}
	
	return &proto.GetEventResponse{
		Event: event,
	}, nil
}

// SetupTestObject implements the SetupTestObject RPC method
func (m *MockComputeNode) SetupTestObject(ctx context.Context, req *proto.SetupTestObjectRequest) (*proto.SetupTestObjectResponse, error) {
	args := m.Called(ctx, req)
	
	// If there's a mocked response, return it
	if args.Get(0) != nil {
		return args.Get(0).(*proto.SetupTestObjectResponse), args.Error(1)
	}

	// Otherwise, handle with our default implementation
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// Create object key that combines object ID and region
	key := fmt.Sprintf("%s:%s", req.ObjectId, req.RegionId)
	
	// Store the test object
	m.testObjects[key] = true
	
	return &proto.SetupTestObjectResponse{
		Success: true,
	}, nil
}

// Execute handles calls to Execute and creates events
func (m *MockComputeNode) Execute(ctx context.Context, req *proto.ExecutionRequest) (*proto.ExecutionResult, error) {
    // First call the original mock implementation
    args := m.Called(ctx, req)
    
    // Create a timestamp for consistency
    timestamp := time.Now().UTC().Format(time.RFC3339)
    
    // If this is a function call that should emit an event, create and store it
    if strings.HasPrefix(req.FunctionCall, "test_function") {
        m.mu.Lock()
        defer m.mu.Unlock()
        
        // Create a unique event ID
        eventID := fmt.Sprintf("%s:%s", req.IdTo, timestamp)
        
        // Create default attestations
        attestations := []*proto.TEEAttestation{
            {
                EnclaveId:   []byte("mock-enclave-id-1"),
                Measurement: []byte("mock-measurement-1"),
                Timestamp:   timestamp,
            },
            {
                EnclaveId:   []byte("mock-enclave-id-2"),
                Measurement: []byte("mock-measurement-2"),
                Timestamp:   timestamp,
            },
        }
        
        // Create and store the event
        event := &proto.Event{
            Id:           eventID,
            FunctionCall: req.FunctionCall,
            Parameters:   req.Parameters,
            RegionId:     req.RegionId,
            Timestamp:    timestamp,
            Attestations: attestations,
        }
        
        m.events[eventID] = event
    }
    
    // If the original Execute implementation returned a result, use it
    if args.Get(0) != nil {
        return args.Get(0).(*proto.ExecutionResult), args.Error(1)
    }
    
    // Otherwise return a default result with proper fields
    return &proto.ExecutionResult{
        Timestamp: timestamp,
        Attestations: []*proto.TEEAttestation{
            {
                EnclaveId:   []byte("mock-enclave-1"),
                Measurement: []byte("mock-measurement-1"),
                Timestamp:   timestamp,
                Signature:   []byte("mock-signature-1"),
                RegionProof: []byte("mock-region-proof-1"),
            },
        },
        StateHash: []byte("mock-state-hash"),
    }, nil
}
