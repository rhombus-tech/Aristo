// Package mocks provides mock implementations of interfaces for testing
package mocks

import (
	"context"

	"github.com/rhombus-tech/vm/tee/proto"
	"github.com/stretchr/testify/mock"
)

// MockComputeNode is a mock implementation of the TeeExecutionServer interface
type MockComputeNode struct {
	mock.Mock
	proto.UnimplementedTeeExecutionServer
}

// NewMockComputeNode creates a new MockComputeNode
func NewMockComputeNode() *MockComputeNode {
	return &MockComputeNode{}
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
