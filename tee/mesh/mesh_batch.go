package mesh

import (
	"context"

	"github.com/rhombus-tech/vm/tee/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// BatchDirectExecute processes multiple operations in a single batch
func (m *TeeMeshService) BatchDirectExecute(ctx context.Context, req *proto.BatchDirectExecutionRequest) (*proto.BatchDirectExecutionResponse, error) {
	if m.batchProcessor == nil {
		return nil, status.Error(codes.Internal, "batch processor not initialized")
	}
	return m.batchProcessor.BatchDirectExecute(ctx, req)
}

// BatchProxyExecute handles batch execution with automatic failover
func (m *TeeMeshService) BatchProxyExecute(ctx context.Context, req *proto.BatchProxyExecutionRequest) (*proto.BatchDirectExecutionResponse, error) {
	if m.batchProcessor == nil {
		return nil, status.Error(codes.Internal, "batch processor not initialized")
	}
	return m.batchProcessor.BatchProxyExecute(ctx, req)
}



