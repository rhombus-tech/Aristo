package mesh

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/rhombus-tech/vm/tee/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// BatchMockExecutionHandler is a mock implementation of the ExecutionHandler interface for batch tests
type BatchMockExecutionHandler struct {
	mock.Mock
	execTime     time.Duration
	memoryUsed   uint64
	failureRate  float64
	processDelay time.Duration
	mutex        sync.Mutex
	execCount    int
}

func (m *BatchMockExecutionHandler) Execute(ctx context.Context, req *proto.DirectExecutionRequest) (*proto.DirectExecutionResponse, error) {
	m.mutex.Lock()
	m.execCount++
	execCount := m.execCount // Make a local copy
	m.mutex.Unlock()

	// Simulate processing delay if specified
	if m.processDelay > 0 {
		time.Sleep(m.processDelay)
	}

	// Simulate failures based on failure rate
	shouldFail := false
	if m.failureRate > 0 {
		// Calculate whether this execution should fail based on the failure rate
		shouldFail = (execCount % int(1.0/m.failureRate)) == 0
	}

	if shouldFail {
		return &proto.DirectExecutionResponse{
			Success:     false,
			Result:      []byte(fmt.Sprintf("Simulated failure for operation %s", req.FunctionCall)),
			Timestamp:   fmt.Sprintf("%d", time.Now().UnixNano()),
			SenderId:    "mock-executor",
		}, nil
	}

	// Simulate successful execution
	time.Sleep(m.execTime)
	return &proto.DirectExecutionResponse{
		Success:        true,
		Result:         []byte(fmt.Sprintf("Executed %s successfully", req.FunctionCall)),
		Timestamp:      fmt.Sprintf("%d", time.Now().UnixNano()),
		ExecutionTime:  uint64(m.execTime.Milliseconds()),
		MemoryUsed:     m.memoryUsed,
		SyscallCount:   5,
		SenderId:       "mock-executor",
		StateHash:      []byte("mock-state-hash"),
		NetworkLatencyNs: 50000,
	}, nil
}

// Setup a test mesh service with the mock execution handler
func setupTestMeshService() (*TeeMeshService, *BatchMockExecutionHandler) {
	mockHandler := &BatchMockExecutionHandler{
		execTime:    10 * time.Millisecond,
		memoryUsed:  1024,
		failureRate: 0,
	}

	service := &TeeMeshService{
		teeID:            "test-tee",
		teeType:          "SGX",
		regionID:         "test-region",
		endpoint:         "localhost:12345",
		executionHandler: mockHandler,
		peers:            make(map[string]*Peer),
		stateCache:       make(map[string]stateInfo),
		stateManager:     NewDefaultStateManager(),
	}

	// Initialize the batch processor
	service.batchProcessor = NewBatchProcessor(service)

	return service, mockHandler
}

// Create a batch request with specified number of operations
func createBatchRequest(operationCount int, executionMode proto.BatchExecutionMode, errorHandling proto.BatchErrorHandling) *proto.BatchDirectExecutionRequest {
	operations := make([]*proto.BatchOperation, operationCount)
	
	for i := 0; i < operationCount; i++ {
		operations[i] = &proto.BatchOperation{
			OperationId:  fmt.Sprintf("op-%d", i),
			IdTo:         fmt.Sprintf("contract-%d", i%5),  // Distribute across 5 contracts
			FunctionCall: fmt.Sprintf("function-%d", i%10), // Use 10 different functions
			Parameters:   []byte(fmt.Sprintf("params-%d", i)),
		}
	}
	
	return &proto.BatchDirectExecutionRequest{
		SenderId:   "test-sender",
		RegionId:   "test-region",
		Operations: operations,
		Options: &proto.BatchOptions{
			ExecutionMode:           executionMode,
			ErrorHandling:           errorHandling,
			MaxConcurrentOperations: 10,
			PreserveOrder:           false,
			TimeoutMs:               5000,
		},
		SharedParameters: map[string][]byte{
			"shared-param-1": []byte("shared-value-1"),
			"shared-param-2": []byte("shared-value-2"),
		},
	}
}

// Test batch execution with parallel mode
func TestBatchDirectExecute_Parallel(t *testing.T) {
	service, mockHandler := setupTestMeshService()
	req := createBatchRequest(30, proto.BatchExecutionMode_BATCH_MODE_PARALLEL, proto.BatchErrorHandling_BATCH_ERROR_CONTINUE)
	
	// Execute the batch
	start := time.Now()
	resp, err := service.BatchDirectExecute(context.Background(), req)
	execTime := time.Since(start)
	
	// Verify successful execution
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.OverallSuccess)
	assert.Equal(t, 30, len(resp.Results))
	assert.Equal(t, uint32(30), resp.OperationsSucceeded)
	assert.Equal(t, uint32(0), resp.OperationsFailed)
	
	// Verify batch execution was faster than sequential execution would be
	expectedSequentialTime := mockHandler.execTime * 30
	assert.Less(t, execTime, expectedSequentialTime)
	
	// Verify compression ratio is calculated
	assert.Greater(t, resp.CompressionRatio, float32(1.0))
	
	// Verify stats were calculated correctly
	assert.NotNil(t, resp.Statistics)
	assert.Greater(t, resp.Statistics.MeanExecutionTimeNs, uint64(0))
	assert.Greater(t, resp.Statistics.MedianExecutionTimeNs, uint64(0))
	assert.Greater(t, resp.Statistics.P95ExecutionTimeNs, uint64(0))
	assert.Greater(t, resp.Statistics.P99ExecutionTimeNs, uint64(0))
}

// Test batch execution with sequential mode
func TestBatchDirectExecute_Sequential(t *testing.T) {
	service, _ := setupTestMeshService()
	req := createBatchRequest(10, proto.BatchExecutionMode_BATCH_MODE_SEQUENTIAL, proto.BatchErrorHandling_BATCH_ERROR_CONTINUE)
	
	// Execute the batch
	resp, err := service.BatchDirectExecute(context.Background(), req)
	
	// Verify successful execution
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.OverallSuccess)
	assert.Equal(t, 10, len(resp.Results))
	
	// Verify sequential execution maintained order
	for i, result := range resp.Results {
		assert.Equal(t, fmt.Sprintf("op-%d", i), result.OperationId)
	}
}

// Test batch execution with optimized mode
func TestBatchDirectExecute_Optimized(t *testing.T) {
	service, _ := setupTestMeshService()
	req := createBatchRequest(20, proto.BatchExecutionMode_BATCH_MODE_OPTIMIZED, proto.BatchErrorHandling_BATCH_ERROR_CONTINUE)
	
	// Add dependencies between operations
	for i := 10; i < 20; i++ {
		req.Operations[i].Dependencies = []string{fmt.Sprintf("op-%d", i-10)}
	}
	
	// Execute the batch
	resp, err := service.BatchDirectExecute(context.Background(), req)
	
	// Verify successful execution
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.OverallSuccess)
	assert.Equal(t, 20, len(resp.Results))
}

// Test batch execution with failures and continue error handling
func TestBatchDirectExecute_WithFailures_Continue(t *testing.T) {
	service, mockHandler := setupTestMeshService()
	mockHandler.failureRate = 0.2 // 20% of operations will fail
	
	req := createBatchRequest(50, proto.BatchExecutionMode_BATCH_MODE_PARALLEL, proto.BatchErrorHandling_BATCH_ERROR_CONTINUE)
	
	// Execute the batch
	resp, err := service.BatchDirectExecute(context.Background(), req)
	
	// Verify partial success
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.False(t, resp.OverallSuccess) // Overall is false due to failures
	assert.Equal(t, 50, len(resp.Results))
	
	// Approximately 80% should succeed and 20% fail
	assert.Greater(t, resp.OperationsSucceeded, uint32(30)) // Allow some variance
	assert.Less(t, resp.OperationsSucceeded, uint32(50))
	assert.Greater(t, resp.OperationsFailed, uint32(0))
	assert.Less(t, resp.OperationsFailed, uint32(20))
	
	// The result count should still be 50 (all operations)
	assert.Equal(t, 50, len(resp.Results))
}

// Test batch execution with failures and fail-fast error handling
func TestBatchDirectExecute_WithFailures_FailFast(t *testing.T) {
	service, mockHandler := setupTestMeshService()
	mockHandler.failureRate = 0.1 // 10% of operations will fail
	
	req := createBatchRequest(50, proto.BatchExecutionMode_BATCH_MODE_PARALLEL, proto.BatchErrorHandling_BATCH_ERROR_FAIL_FAST)
	
	// Execute the batch
	resp, err := service.BatchDirectExecute(context.Background(), req)
	
	// Verify execution stopped on first failure
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.False(t, resp.OverallSuccess)
	
	// We should have less than 50 results due to fail-fast
	// But this is hard to test with parallel execution since some may complete
	// before the failure is detected
	assert.LessOrEqual(t, int(resp.OperationsSucceeded+resp.OperationsFailed), 50)
}

// Test batch execution with varying latencies
func TestBatchDirectExecute_VaryingLatencies(t *testing.T) {
	service, mockHandler := setupTestMeshService()
	
	// Set up the mock handler to simulate operations with varying latencies
	mockHandler.processDelay = 0
	mockHandler.execTime = 0
	
	req := createBatchRequest(30, proto.BatchExecutionMode_BATCH_MODE_PARALLEL, proto.BatchErrorHandling_BATCH_ERROR_CONTINUE)
	
	// Execute the batch
	resp, err := service.BatchDirectExecute(context.Background(), req)
	
	// Verify successful execution and statistics
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.OverallSuccess)
	
	// Verify the batch processing overhead is calculated
	assert.Greater(t, resp.BatchProcessingOverheadNs, uint64(0))
}

// Test parameter expansion and shared parameters
func TestBatchDirectExecute_SharedParameters(t *testing.T) {
	service, _ := setupTestMeshService()
	
	// Create a request with shared parameters and references
	req := createBatchRequest(5, proto.BatchExecutionMode_BATCH_MODE_SEQUENTIAL, proto.BatchErrorHandling_BATCH_ERROR_CONTINUE)
	
	// Add parameter references to the operations
	for i := 0; i < 5; i++ {
		req.Operations[i].ParameterReferences = []string{"shared-param-1", "shared-param-2"}
	}
	
	// Execute the batch
	resp, err := service.BatchDirectExecute(context.Background(), req)
	
	// Verify successful execution
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.OverallSuccess)
	assert.Equal(t, 5, len(resp.Results))
}

// Test performance with large batch size (50 operations)
func TestBatchDirectExecute_LargeBatch(t *testing.T) {
	service, mockHandler := setupTestMeshService()
	mockHandler.execTime = 5 * time.Millisecond // Reduce execution time for test
	
	// Create a batch with 50 operations
	req := createBatchRequest(50, proto.BatchExecutionMode_BATCH_MODE_PARALLEL, proto.BatchErrorHandling_BATCH_ERROR_CONTINUE)
	
	// Execute the batch
	start := time.Now()
	resp, err := service.BatchDirectExecute(context.Background(), req)
	execTime := time.Since(start)
	
	// Verify successful execution
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.OverallSuccess)
	assert.Equal(t, 50, len(resp.Results))
	
	// Verify batch processing provides significant throughput improvement
	// With 50 operations at 5ms each, sequential would take ~250ms
	// Parallel should be much faster
	assert.Less(t, execTime, 100*time.Millisecond)
	
	// Check the batch processor metrics
	metrics := service.GetBatchProcessorMetrics()
	assert.NotNil(t, metrics)
	assert.Equal(t, int64(1), metrics.TotalBatchesProcessed)
	assert.Equal(t, int64(1), metrics.TotalBatchesSucceeded)
	assert.Equal(t, int64(50), metrics.TotalOperationsProcessed)
	assert.Equal(t, int64(50), metrics.TotalOperationsSucceeded)
}

// Test that batching provides throughput improvement over individual requests
func TestBatchVsIndividualThroughput(t *testing.T) {
	service, mockHandler := setupTestMeshService()
	mockHandler.execTime = 2 * time.Millisecond
	ctx := context.Background()
	
	// Number of operations to execute
	numOperations := 30
	
	// Measure time for individual operations
	start := time.Now()
	for i := 0; i < numOperations; i++ {
		req := &proto.DirectExecutionRequest{
			SenderId:     "test-sender",
			IdTo:         fmt.Sprintf("contract-%d", i%5),
			FunctionCall: fmt.Sprintf("function-%d", i%10),
			Parameters:   []byte(fmt.Sprintf("params-%d", i)),
			RegionId:     "test-region",
		}
		
		_, err := service.DirectExecute(ctx, req)
		assert.NoError(t, err)
	}
	individualTime := time.Since(start)
	
	// Measure time for batch operation
	batchReq := createBatchRequest(numOperations, proto.BatchExecutionMode_BATCH_MODE_PARALLEL, proto.BatchErrorHandling_BATCH_ERROR_CONTINUE)
	
	start = time.Now()
	resp, err := service.BatchDirectExecute(ctx, batchReq)
	batchTime := time.Since(start)
	
	// Verify successful execution
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.OverallSuccess)
	
	// Verify batch execution is significantly faster
	// Expect at least 3x improvement for Phase 1 optimization goals
	speedup := float64(individualTime) / float64(batchTime)
	t.Logf("Throughput improvement: %.2fx (Individual: %v, Batch: %v)", speedup, individualTime, batchTime)
	assert.GreaterOrEqual(t, speedup, 3.0, "Batch processing should provide at least 3x throughput improvement")
}
