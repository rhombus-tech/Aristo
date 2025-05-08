package mesh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/rhombus-tech/vm/tee/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// BatchProcessor handles batch operations for the mesh network
type BatchProcessor struct {
	meshService      *MeshService
	metrics          *BatchProcessorMetrics
	sharedParameters map[string][]byte
	mu               sync.RWMutex
}

// BatchProcessorMetrics tracks performance metrics for batch operations
type BatchProcessorMetrics struct {
	TotalBatchesProcessed      int64
	TotalBatchesSucceeded      int64
	TotalBatchesFailed         int64
	TotalOperationsProcessed   int64
	TotalOperationsSucceeded   int64
	TotalOperationsFailed      int64
	TotalExecutionTimeNs       int64
	TotalOverheadTimeNs        int64
	AverageOperationsPerBatch  float64
	AverageBatchLatencyNs      int64
	MaxBatchLatencyNs          int64
	CompressionRatioSum        float64
	CompressionRatioCount      int64
	AverageCompressionRatio    float64
	MaxConcurrentBatches       int64
	CurrentConcurrentBatches   int64
	DependencyWaitCountTotal   int64
	OperationsFallbackTotal    int64
	mu                         sync.RWMutex
}

// NewBatchProcessor creates a new batch processor
func NewBatchProcessor(meshService *MeshService) *BatchProcessor {
	return &BatchProcessor{
		meshService:      meshService,
		metrics:          &BatchProcessorMetrics{},
		sharedParameters: make(map[string][]byte),
	}
}

// BatchDirectExecute processes a batch of execution requests
func (b *BatchProcessor) BatchDirectExecute(ctx context.Context, req *proto.BatchDirectExecutionRequest) (*proto.BatchDirectExecutionResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "cannot execute nil batch request")
	}

	// Validate the request
	if len(req.Operations) == 0 {
		return &proto.BatchDirectExecutionResponse{
			OverallSuccess: false,
			Results:        []*proto.BatchOperationResult{},
		}, nil
	}

	// Record batch start time for metrics
	batchStart := time.Now()
	
	// Create a response object to track results
	resp := &proto.BatchDirectExecutionResponse{
		OverallSuccess:      true,
		Results:             make([]*proto.BatchOperationResult, 0, len(req.Operations)),
		OperationsSucceeded: 0,
		OperationsFailed:    0,
		Statistics:          &proto.BatchStatistics{},
	}

	// Update metrics
	b.updateMetricsForBatchStart(len(req.Operations))

	// Store shared parameters (if any) for this batch
	if len(req.SharedParameters) > 0 {
		b.storeSharedParameters(req.SenderId, req.SharedParameters)
	}

	// Process according to batch execution mode
	switch {
	case req.Options == nil || req.Options.ExecutionMode == proto.BatchExecutionMode_BATCH_MODE_PARALLEL:
		resp = b.executeParallel(ctx, req)
	case req.Options.ExecutionMode == proto.BatchExecutionMode_BATCH_MODE_SEQUENTIAL:
		resp = b.executeSequential(ctx, req)
	case req.Options.ExecutionMode == proto.BatchExecutionMode_BATCH_MODE_OPTIMIZED:
		resp = b.executeOptimized(ctx, req)
	default:
		// Default to parallel execution
		resp = b.executeParallel(ctx, req)
	}

	// Calculate batch metrics
	batchEndTime := time.Now()
	totalBatchTime := batchEndTime.Sub(batchStart)
	
	// Set batch level metrics
	resp.TotalExecutionTimeNs = uint64(totalBatchTime.Nanoseconds())
	
	// Accumulate execution times for statistics
	var executionTimes []uint64
	for _, result := range resp.Results {
		executionTimes = append(executionTimes, result.ExecutionTimeNs)
	}

	// Update batch statistics
	if len(executionTimes) > 0 {
		resp.Statistics = calculateBatchStatistics(executionTimes, resp.TotalExecutionTimeNs)
		
		// Calculate batch processing overhead by subtracting max execution time from total batch time
		// This gives us a rough estimate of the overhead introduced by batch processing
		resp.BatchProcessingOverheadNs = resp.TotalExecutionTimeNs - resp.Statistics.MaxExecutionTimeNs
	}

	// Calculate compression ratio if more than one operation
	if len(req.Operations) > 1 {
		estimatedIndividualSize := estimateTotalIndividualSize(req)
		batchSize := estimateBatchSize(req)
		
		if estimatedIndividualSize > 0 && batchSize > 0 {
			resp.CompressionRatio = float32(estimatedIndividualSize) / float32(batchSize)
		}
	}

	// Update metrics with batch completion information
	b.updateMetricsForBatchCompletion(resp)

	// Clean up shared parameters
	b.clearSharedParameters(req.SenderId)

	return resp, nil
}

// BatchProxyExecute handles batch execution with automatic failover
func (b *BatchProcessor) BatchProxyExecute(ctx context.Context, req *proto.BatchProxyExecutionRequest) (*proto.BatchDirectExecutionResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "cannot execute nil batch proxy request")
	}

	// Generate direct batch execution request from proxy request
	directReq := &proto.BatchDirectExecutionRequest{
		SenderId:         req.SenderId,
		Operations:       req.Operations,
		RegionId:         req.RegionId,
		SharedParameters: req.SharedParameters,
		Options:          req.Options,
	}

	// First try to execute the batch locally
	resp, err := b.BatchDirectExecute(ctx, directReq)
	if err == nil && resp.OverallSuccess {
		return resp, nil
	}

	// If local execution failed and we're allowed to retry, try executing on peers
	if req.MaxRetries > 0 {
		// Get a list of suitable peers for failover
		peers := b.meshService.GetSuitablePeers(req.RegionId, req.PreferredTeeType, req.ExcludedPeers)
		
		// Try each peer until success or we run out of peers/retries
		for i, peer := range peers {
			if i >= int(req.MaxRetries) {
				break
			}
			
			// Skip offline or degraded peers
			if peer.Status != "active" {
				continue
			}
			
			// Execute batch on the peer
			peerResp, err := peer.MeshClient.BatchDirectExecute(ctx, directReq)
			if err == nil && peerResp.OverallSuccess {
				// Add proxy information to the response
				peerResp.Statistics.TotalNetworkLatencyNs += uint64(peer.AverageLatencyNs)
				return peerResp, nil
			}
			
			// Record failure for this peer
			peer.recordFailure()
		}
	}

	// If cross-region execution is allowed, try executing in other regions
	if req.CrossRegionAllowed {
		// This would need coordination with federation service to execute in other regions
		// For now, we'll just return the best partial results we have
	}

	// If we reach here, all attempts failed
	if resp == nil {
		// Create a failure response if we don't have one
		resp = &proto.BatchDirectExecutionResponse{
			OverallSuccess:   false,
			Results:          []*proto.BatchOperationResult{},
			OperationsFailed: uint32(len(req.Operations)),
		}
	}

	return resp, errors.New("all batch execution attempts failed")
}

// executeParallel executes operations in parallel
func (b *BatchProcessor) executeParallel(ctx context.Context, req *proto.BatchDirectExecutionRequest) *proto.BatchDirectExecutionResponse {
	resp := &proto.BatchDirectExecutionResponse{
		OverallSuccess: true,
		Results:        make([]*proto.BatchOperationResult, len(req.Operations)),
	}

	// Create a wait group for parallel execution
	var wg sync.WaitGroup
	var resultsMu sync.Mutex // Mutex to protect concurrent writes to the results slice

	// Determine the maximum concurrent operations
	maxConcurrent := 10 // Default max concurrent operations
	if req.Options != nil && req.Options.MaxConcurrentOperations > 0 {
		maxConcurrent = int(req.Options.MaxConcurrentOperations)
	}

	// Create a semaphore channel to limit concurrency
	semaphore := make(chan struct{}, maxConcurrent)

	// Execute each operation in parallel
	for i, op := range req.Operations {
		wg.Add(1)

		// Acquire semaphore
		semaphore <- struct{}{}

		go func(index int, operation *proto.BatchOperation) {
			defer wg.Done()
			defer func() { <-semaphore }() // Release semaphore when done

			// Process a single operation
			result := b.processSingleOperation(ctx, req.SenderId, operation, req.RegionId)

			// Update results under mutex protection
			resultsMu.Lock()
			resp.Results[index] = result
			if result.Success {
				resp.OperationsSucceeded++
			} else {
				resp.OperationsFailed++
				// Check error handling strategy
				if req.Options != nil && req.Options.ErrorHandling == proto.BatchErrorHandling_BATCH_ERROR_FAIL_FAST {
					resp.OverallSuccess = false
					// Cancel context to signal other goroutines to stop
					// Note: In a real implementation, you'd need a custom cancellable context
				}
			}
			resultsMu.Unlock()
		}(i, op)
	}

	// Wait for all operations to complete
	wg.Wait()

	// Final success check
	if resp.OperationsFailed > 0 {
		resp.OverallSuccess = false
	}

	return resp
}

// executeSequential executes operations sequentially
func (b *BatchProcessor) executeSequential(ctx context.Context, req *proto.BatchDirectExecutionRequest) *proto.BatchDirectExecutionResponse {
	resp := &proto.BatchDirectExecutionResponse{
		OverallSuccess: true,
		Results:        make([]*proto.BatchOperationResult, 0, len(req.Operations)),
	}

	// Process operations in sequence
	for _, op := range req.Operations {
		// Check if context is cancelled
		select {
		case <-ctx.Done():
			return resp
		default:
			// Continue processing
		}

		// Process a single operation
		result := b.processSingleOperation(ctx, req.SenderId, op, req.RegionId)
		resp.Results = append(resp.Results, result)

		// Update success/failure counts
		if result.Success {
			resp.OperationsSucceeded++
		} else {
			resp.OperationsFailed++
			resp.OverallSuccess = false

			// Check error handling strategy
			if req.Options != nil && req.Options.ErrorHandling == proto.BatchErrorHandling_BATCH_ERROR_FAIL_FAST {
				break
			}
		}
	}

	return resp
}

// executeOptimized executes operations in an optimized order
func (b *BatchProcessor) executeOptimized(ctx context.Context, req *proto.BatchDirectExecutionRequest) *proto.BatchDirectExecutionResponse {
	// Build a dependency graph
	dependencies := buildDependencyGraph(req.Operations)
	executionOrder := topologicalSort(dependencies)
	
	resp := &proto.BatchDirectExecutionResponse{
		OverallSuccess: true,
		Results:        make([]*proto.BatchOperationResult, len(req.Operations)),
	}

	// Map operation IDs to their results for dependency tracking
	resultsByID := make(map[string]*proto.BatchOperationResult)
	var resultsMu sync.Mutex

	// Determine the maximum concurrent operations
	maxConcurrent := 10 // Default max concurrent operations
	if req.Options != nil && req.Options.MaxConcurrentOperations > 0 {
		maxConcurrent = int(req.Options.MaxConcurrentOperations)
	}

	// Create a semaphore channel to limit concurrency
	semaphore := make(chan struct{}, maxConcurrent)

	// Execute operations in the optimized order
	for _, executionWave := range executionOrder {
		// Each wave can be executed in parallel
		var waveWg sync.WaitGroup
		for _, opIndex := range executionWave {
			waveWg.Add(1)
			
			// Acquire semaphore
			semaphore <- struct{}{}

			go func(index int, operation *proto.BatchOperation) {
				defer waveWg.Done()
				defer func() { <-semaphore }() // Release semaphore when done

				// Process a single operation
				result := b.processSingleOperation(ctx, req.SenderId, operation, req.RegionId)

				// Update results under mutex protection
				resultsMu.Lock()
				resp.Results[index] = result
				resultsByID[operation.OperationId] = result
				if result.Success {
					resp.OperationsSucceeded++
				} else {
					resp.OperationsFailed++
					// Check error handling strategy
					if req.Options != nil && req.Options.ErrorHandling == proto.BatchErrorHandling_BATCH_ERROR_FAIL_FAST {
						resp.OverallSuccess = false
					}
				}
				resultsMu.Unlock()
			}(opIndex, req.Operations[opIndex])
		}

		// Wait for the entire wave to complete before proceeding
		waveWg.Wait()

		// Check if we should stop due to errors
		if req.Options != nil && req.Options.ErrorHandling == proto.BatchErrorHandling_BATCH_ERROR_FAIL_FAST && resp.OperationsFailed > 0 {
			break
		}
	}

	// Final success check
	if resp.OperationsFailed > 0 {
		resp.OverallSuccess = false
	}

	return resp
}

// processSingleOperation processes a single batch operation
func (b *BatchProcessor) processSingleOperation(ctx context.Context, senderID string, op *proto.BatchOperation, regionID string) *proto.BatchOperationResult {
	startTime := time.Now()

	// Create a result with the operation ID
	result := &proto.BatchOperationResult{
		OperationId: op.OperationId,
		Timestamp:   fmt.Sprintf("%d", time.Now().UnixNano()),
		Success:     false, // Assume failure until proven otherwise
	}

	// Resolve parameters if needed
	parameters := op.Parameters
	if len(op.ParameterReferences) > 0 {
		expandedParams, err := b.expandParameterReferences(senderID, op)
		if err != nil {
			result.ErrorMessage = fmt.Sprintf("Failed to expand parameter references: %v", err)
			result.ExecutionTimeNs = uint64(time.Since(startTime).Nanoseconds())
			return result
		}
		parameters = expandedParams
	}

	// Create a DirectExecutionRequest from the batch operation
	execReq := &proto.DirectExecutionRequest{
		SenderId:         senderID,
		IdTo:             op.IdTo,
		FunctionCall:     op.FunctionCall,
		Parameters:       parameters,
		RegionId:         regionID,
		DetailedProof:    op.DetailedProof,
		ExpectedHash:     op.ExpectedHash,
		BypassCoordinator: true, // Always bypass for batch processing
	}

	// Execute the operation
	execResp, err := b.meshService.DirectExecute(ctx, execReq)
	execTime := time.Since(startTime)

	// Fill in the result based on execution outcome
	if err != nil {
		result.Success = false
		result.ErrorMessage = err.Error()
	} else if execResp != nil {
		result.Success = execResp.Success
		result.Result = execResp.Result
		result.StateHash = execResp.StateHash
		result.ExecutorId = execResp.SenderId
		
		// Copy metrics from the individual execution response
		result.ExecutionTimeNs = execResp.ExecutionTime * 1000000 // Convert ms to ns
		result.MemoryUsed = execResp.MemoryUsed
		result.SyscallCount = execResp.SyscallCount
		
		if !execResp.Success && len(execResp.Result) > 0 {
			result.ErrorMessage = string(execResp.Result)
		}
	} else {
		result.ErrorMessage = "No execution response received"
	}

	// Ensure execution time is set even if the execution failed
	if result.ExecutionTimeNs == 0 {
		result.ExecutionTimeNs = uint64(execTime.Nanoseconds())
	}

	return result
}

// storeSharedParameters stores shared parameters for a batch
func (b *BatchProcessor) storeSharedParameters(senderID string, params map[string][]byte) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Create a key prefix for this sender to avoid conflicts
	prefix := fmt.Sprintf("%s:", senderID)

	// Store each parameter with the sender's prefix
	for key, value := range params {
		b.sharedParameters[prefix+key] = value
	}
}

// clearSharedParameters removes shared parameters for a sender
func (b *BatchProcessor) clearSharedParameters(senderID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Create a key prefix for this sender
	prefix := fmt.Sprintf("%s:", senderID)

	// Remove all parameters with this prefix
	for key := range b.sharedParameters {
		if bytes.HasPrefix([]byte(key), []byte(prefix)) {
			delete(b.sharedParameters, key)
		}
	}
}

// expandParameterReferences expands parameter references in a batch operation
// using JSON for parameter merging
func (b *BatchProcessor) expandParameterReferences(senderID string, op *proto.BatchOperation) ([]byte, error) {
	// If no references, return the original parameters
	if len(op.ParameterReferences) == 0 {
		return op.Parameters, nil
	}

	// Create a key prefix for this sender
	prefix := fmt.Sprintf("%s:", senderID)

	// Check if all referenced parameters exist
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, refKey := range op.ParameterReferences {
		fullKey := prefix + refKey
		if _, exists := b.sharedParameters[fullKey]; !exists {
			return nil, fmt.Errorf("referenced parameter not found: %s", refKey)
		}
	}

	// Create a map to hold merged parameters
	mergedParams := make(map[string]interface{})

	// Add the operation's own parameters first if they exist
	if len(op.Parameters) > 0 {
		var baseParams map[string]interface{}
		err := json.Unmarshal(op.Parameters, &baseParams)
		if err == nil {
			// Successfully parsed as JSON object
			for k, v := range baseParams {
				mergedParams[k] = v
			}
		} else {
			// If not valid JSON, store as raw bytes under default key
			mergedParams["_data"] = op.Parameters
		}
	}

	// Merge each referenced parameter
	for _, refKey := range op.ParameterReferences {
		fullKey := prefix + refKey
		refData := b.sharedParameters[fullKey]

		// Try to parse as JSON object
		var refParams map[string]interface{}
		if err := json.Unmarshal(refData, &refParams); err == nil {
			// Successfully parsed, merge keys
			for k, v := range refParams {
				// Only add if key doesn't already exist (prioritize operation's own parameters)
				if _, exists := mergedParams[k]; !exists {
					mergedParams[k] = v
				}
			}
		} else {
			// Not valid JSON, store under reference key
			if _, exists := mergedParams[refKey]; !exists {
				mergedParams[refKey] = refData
			}
		}
	}

	// Marshal the merged parameters back to JSON
	result, err := json.Marshal(mergedParams)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal merged parameters: %w", err)
	}

	return result, nil
}

// estimateTotalIndividualSize estimates the size of individual requests if sent separately
func estimateTotalIndividualSize(req *proto.BatchDirectExecutionRequest) int64 {
	totalSize := int64(0)
	
	// Baseline size for a DirectExecutionRequest message
	baselineSize := int64(50) // Rough estimate
	
	for _, op := range req.Operations {
		// Add size for this operation
		opSize := baselineSize
		opSize += int64(len(op.IdTo))
		opSize += int64(len(op.FunctionCall))
		opSize += int64(len(op.Parameters))
		
		totalSize += opSize
	}
	
	return totalSize
}

// Helper function to estimate the size of the batch request
func estimateBatchSize(req *proto.BatchDirectExecutionRequest) int64 {
	totalSize := int64(0)
	
	// Baseline size for a BatchDirectExecutionRequest message
	totalSize += int64(50) // Rough estimate
	
	// Add size of sender ID and region ID
	totalSize += int64(len(req.SenderId))
	totalSize += int64(len(req.RegionId))
	
	// Add size for all operations
	for _, op := range req.Operations {
		totalSize += int64(len(op.OperationId))
		totalSize += int64(len(op.IdTo))
		totalSize += int64(len(op.FunctionCall))
		totalSize += int64(len(op.Parameters))
	}
	
	// Add size for shared parameters
	for key, value := range req.SharedParameters {
		totalSize += int64(len(key))
		totalSize += int64(len(value))
	}
	
	return totalSize
}

// calculateBatchStatistics calculates statistics for a batch execution
func calculateBatchStatistics(executionTimes []uint64, totalBatchTimeNs uint64) *proto.BatchStatistics {
	stats := &proto.BatchStatistics{
		TotalNetworkLatencyNs: 0, // Will be populated with actual network latency
	}
	
	if len(executionTimes) == 0 {
		return stats
	}
	
	// Create a sorted copy of execution times for percentile calculations
	sortedTimes := make([]uint64, len(executionTimes))
	copy(sortedTimes, executionTimes)
	sort.Slice(sortedTimes, func(i, j int) bool {
		return sortedTimes[i] < sortedTimes[j]
	})
	
	// Find min, max, and calculate mean
	var sum uint64
	var min uint64 = sortedTimes[0]                    // First element after sorting
	var max uint64 = sortedTimes[len(sortedTimes)-1]    // Last element after sorting
	
	for _, time := range executionTimes {
		sum += time
	}
	
	mean := sum / uint64(len(executionTimes))
	
	// Calculate proper percentiles
	medianIndex := len(sortedTimes) / 2
	var median uint64
	if len(sortedTimes)%2 == 0 && len(sortedTimes) > 1 {
		// Even number of elements, average the middle two
		median = (sortedTimes[medianIndex-1] + sortedTimes[medianIndex]) / 2
	} else {
		// Odd number of elements, take the middle one
		median = sortedTimes[medianIndex]
	}
	
	// Calculate p95 and p99 properly
	p95Index := int(float64(len(sortedTimes)-1) * 0.95)
	p99Index := int(float64(len(sortedTimes)-1) * 0.99)
	
	// Protect against small sample sizes
	if p95Index >= len(sortedTimes) {
		p95Index = len(sortedTimes) - 1
	}
	if p99Index >= len(sortedTimes) {
		p99Index = len(sortedTimes) - 1
	}
	
	p95 := sortedTimes[p95Index]
	p99 := sortedTimes[p99Index]
	
	// Calculate standard deviation
	var sumSquaredDiff float64
	for _, time := range executionTimes {
		diff := float64(time) - float64(mean)
		sumSquaredDiff += diff * diff
	}
	
	// Avoid division by zero for single-element arrays
	var stdev uint64
	if len(executionTimes) > 1 {
		variance := sumSquaredDiff / float64(len(executionTimes))
		stdev = uint64(math.Sqrt(variance))
	}
	
	// Populate all statistics
	stats.MinExecutionTimeNs = min
	stats.MaxExecutionTimeNs = max
	stats.MeanExecutionTimeNs = mean
	stats.MedianExecutionTimeNs = median
	stats.P95ExecutionTimeNs = p95
	stats.P99ExecutionTimeNs = p99
	stats.StdevExecutionTimeNs = stdev
	
	return stats
}

// updateMetricsForBatchStart updates metrics when a batch starts processing
func (b *BatchProcessor) updateMetricsForBatchStart(operationCount int) {
	b.metrics.mu.Lock()
	defer b.metrics.mu.Unlock()
	
	b.metrics.TotalBatchesProcessed++
	b.metrics.TotalOperationsProcessed += int64(operationCount)
	b.metrics.CurrentConcurrentBatches++
	
	if b.metrics.CurrentConcurrentBatches > b.metrics.MaxConcurrentBatches {
		b.metrics.MaxConcurrentBatches = b.metrics.CurrentConcurrentBatches
	}
}

// updateMetricsForBatchCompletion updates metrics when a batch completes
func (b *BatchProcessor) updateMetricsForBatchCompletion(resp *proto.BatchDirectExecutionResponse) {
	b.metrics.mu.Lock()
	defer b.metrics.mu.Unlock()
	
	if resp.OverallSuccess {
		b.metrics.TotalBatchesSucceeded++
	} else {
		b.metrics.TotalBatchesFailed++
	}
	
	b.metrics.TotalOperationsSucceeded += int64(resp.OperationsSucceeded)
	b.metrics.TotalOperationsFailed += int64(resp.OperationsFailed)
	b.metrics.TotalExecutionTimeNs += int64(resp.TotalExecutionTimeNs)
	b.metrics.TotalOverheadTimeNs += int64(resp.BatchProcessingOverheadNs)
	
	if int64(resp.TotalExecutionTimeNs) > b.metrics.MaxBatchLatencyNs {
		b.metrics.MaxBatchLatencyNs = int64(resp.TotalExecutionTimeNs)
	}
	
	// Update average operations per batch
	if b.metrics.TotalBatchesProcessed > 0 {
		b.metrics.AverageOperationsPerBatch = float64(b.metrics.TotalOperationsProcessed) / float64(b.metrics.TotalBatchesProcessed)
	}
	
	// Update average batch latency
	if b.metrics.TotalBatchesProcessed > 0 {
		b.metrics.AverageBatchLatencyNs = b.metrics.TotalExecutionTimeNs / b.metrics.TotalBatchesProcessed
	}
	
	// Update compression ratio metrics
	if resp.CompressionRatio > 0 {
		b.metrics.CompressionRatioSum += float64(resp.CompressionRatio)
		b.metrics.CompressionRatioCount++
		
		if b.metrics.CompressionRatioCount > 0 {
			b.metrics.AverageCompressionRatio = b.metrics.CompressionRatioSum / float64(b.metrics.CompressionRatioCount)
		}
	}
	
	// Update dependency wait count
	if resp.Statistics != nil {
		b.metrics.DependencyWaitCountTotal += int64(resp.Statistics.DependencyWaitCount)
	}
	
	// Decrement current concurrent batches
	b.metrics.CurrentConcurrentBatches--
}

// GetMetrics returns a copy of the current metrics
func (b *BatchProcessor) GetMetrics() *BatchProcessorMetrics {
	b.metrics.mu.RLock()
	defer b.metrics.mu.RUnlock()
	
	// Create a copy of the metrics
	metricsCopy := *b.metrics
	
	return &metricsCopy
}

// buildDependencyGraph builds a dependency graph from batch operations
func buildDependencyGraph(operations []*proto.BatchOperation) map[int][]int {
	// Create a map of operation IDs to their indices
	opIndexMap := make(map[string]int)
	for i, op := range operations {
		opIndexMap[op.OperationId] = i
	}
	
	// Build the dependency graph
	dependencies := make(map[int][]int)
	
	for i, op := range operations {
		// Initialize an empty dependency list for this operation
		dependencies[i] = []int{}
		
		// Add dependencies based on explicit dependencies
		for _, depID := range op.Dependencies {
			if depIndex, exists := opIndexMap[depID]; exists {
				dependencies[i] = append(dependencies[i], depIndex)
			}
		}
	}
	
	return dependencies
}

// topologicalSort performs a topological sort on the dependency graph
func topologicalSort(graph map[int][]int) [][]int {
	// Count the number of operations
	n := len(graph)
	
	// Initialize result as a series of execution waves
	var result [][]int
	
	// Initialize in-degree count for each node
	inDegree := make(map[int]int)
	for node := range graph {
		inDegree[node] = 0
	}
	
	// Calculate in-degree for each node
	for _, edges := range graph {
		for _, target := range edges {
			inDegree[target]++
		}
	}
	
	// Queue of nodes with zero in-degree
	var queue []int
	for node := range graph {
		if inDegree[node] == 0 {
			queue = append(queue, node)
		}
	}
	
	// Process nodes level by level to create execution waves
	for len(queue) > 0 {
		// Create a new wave with all ready nodes
		wave := make([]int, len(queue))
		copy(wave, queue)
		result = append(result, wave)
		
		// Prepare the next queue
		var nextQueue []int
		
		// Process all nodes in the current queue
		for _, node := range queue {
			// Reduce in-degree of all neighbors
			for _, neighbor := range graph[node] {
				inDegree[neighbor]--
				
				// If in-degree becomes zero, add to the next queue
				if inDegree[neighbor] == 0 {
					nextQueue = append(nextQueue, neighbor)
				}
			}
		}
		
		// Update queue for the next wave
		queue = nextQueue
	}
	
	// Check if we processed all nodes
	processed := 0
	for _, wave := range result {
		processed += len(wave)
	}
	
	// Handle cycles by adding remaining nodes
	if processed < n {
		var remaining []int
		for node := range graph {
			if inDegree[node] > 0 {
				remaining = append(remaining, node)
			}
		}
		
		// Add remaining nodes as a final wave
		if len(remaining) > 0 {
			result = append(result, remaining)
		}
	}
	
	return result
}
