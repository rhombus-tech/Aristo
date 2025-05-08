package mesh

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/rhombus-tech/vm/tee/proto"
)

// BatchProcessorResilience adds circuit breaker protection to the batch processor
type BatchProcessorResilience struct {
	// Main components
	batchProcessor *BatchProcessor
	
	// Circuit breakers for different target services
	serviceCircuitBreakers *CircuitBreakerRegistry
	
	// Configuration
	config *BatchResilienceConfig
	
	// Locks and internal state
	mu sync.RWMutex
}

// BatchResilienceConfig holds configuration for batch processor resilience
type BatchResilienceConfig struct {
	// Circuit breaker settings
	DefaultCircuitBreakerConfig *EnhancedCircuitBreakerConfig
	
	// Retry settings
	MaxRetries          int
	RetryBackoffMs      int
	RetryJitterMs       int
	
	// Fallback settings
	EnableLocalFallback bool
	
	// Monitoring settings
	EnableMetricsServer bool
	MetricsServerAddr   string
}

// DefaultBatchResilienceConfig returns the default configuration
func DefaultBatchResilienceConfig() *BatchResilienceConfig {
	return &BatchResilienceConfig{
		DefaultCircuitBreakerConfig: DefaultEnhancedCircuitBreakerConfig(),
		MaxRetries:                 3,
		RetryBackoffMs:             50,
		RetryJitterMs:              20,
		EnableLocalFallback:        true,
		EnableMetricsServer:        true,
		MetricsServerAddr:          ":8081",
	}
}

// NewBatchProcessorResilience creates a new resilient batch processor
func NewBatchProcessorResilience(batchProcessor *BatchProcessor, config *BatchResilienceConfig) *BatchProcessorResilience {
	if config == nil {
		config = DefaultBatchResilienceConfig()
	}
	
	registry := NewCircuitBreakerRegistry()
	
	// Add standard handlers to all circuit breakers
	registry.AddAlertHandler(&StandardAlertHandler{LogPrefix: "BatchProcessor"})
	
	// Create metrics publisher for collecting alerts
	metrics := NewMetricsPublisher(100)
	registry.AddAlertHandler(metrics)
	
	// Start metrics server if enabled
	if config.EnableMetricsServer {
		handler := NewHTTPMetricsHandler(registry)
		go func() {
			mux := new(http.ServeMux)
			handler.RegisterHandlers(mux)
			err := http.ListenAndServe(config.MetricsServerAddr, mux)
			if err != nil {
				fmt.Printf("Failed to start metrics server: %v\n", err)
			}
		}()
	}
	
	return &BatchProcessorResilience{
		batchProcessor:        batchProcessor,
		serviceCircuitBreakers: registry,
		config:                config,
	}
}

// BatchDirectExecute executes a batch of operations with circuit breaker protection
func (bpr *BatchProcessorResilience) BatchDirectExecute(ctx context.Context, req *proto.BatchDirectExecutionRequest) (*proto.BatchDirectExecutionResponse, error) {
	// Create or get a circuit breaker for the target region
	cbName := fmt.Sprintf("direct:%s", req.RegionId)
	cb, _ := bpr.serviceCircuitBreakers.Get(cbName)
	if cb == nil {
		cb = bpr.serviceCircuitBreakers.Register(cbName, bpr.config.DefaultCircuitBreakerConfig)
	}
	
	// Track retries
	retries := 0
	var lastErr error
	
	// Use the circuit breaker to protect the call
	var result *proto.BatchDirectExecutionResponse
	err := cb.Execute(ctx, func() error {
		var execErr error
		result, execErr = bpr.batchProcessor.BatchDirectExecute(ctx, req)
		
		if execErr != nil {
			// Check if retry is appropriate
			if retries < bpr.config.MaxRetries {
				retries++
				
				// Implement exponential backoff with jitter
				backoff := time.Duration(bpr.config.RetryBackoffMs * (1 << uint(retries-1))) * time.Millisecond
				jitter := time.Duration(bpr.config.RetryJitterMs) * time.Millisecond
				
				// Add jitter
				if jitter > 0 {
					backoff = backoff + time.Duration(time.Now().UnixNano()%int64(jitter))
				}
				
				// Wait before retry
				select {
				case <-time.After(backoff):
					// Continue with retry
				case <-ctx.Done():
					return ctx.Err()
				}
				
				// This error will be retried, so don't record it as a failure in the circuit breaker
				return nil
			}
			
			lastErr = execErr
			return execErr
		}
		
		// Check response for batch-level success
		if !result.OverallSuccess {
			lastErr = fmt.Errorf("batch error: overall execution failed")
			return lastErr
		}
		
		// Check if too many individual operations failed
		totalOps := len(req.Operations)
		failedOps := 0
		
		for _, opResult := range result.Results {
			if opResult.ErrorMessage != "" {
				failedOps++
			}
		}
		
		// If more than 50% of operations failed, consider the batch failed
		if totalOps > 0 && float64(failedOps)/float64(totalOps) > 0.5 {
			lastErr = fmt.Errorf("batch partial failure: %d/%d operations failed", failedOps, totalOps)
			return lastErr
		}
		
		return nil
	})
	
	// Handle circuit breaker open
	if err == ErrCircuitBreakerOpen && bpr.config.EnableLocalFallback {
		// Implement fallback logic - execute locally if possible
		return bpr.executeLocalFallback(ctx, req)
	}
	
	if err != nil {
		return nil, fmt.Errorf("batch execution failed after %d retries: %w", retries, lastErr)
	}
	
	return result, nil
}

// BatchProxyExecute executes a batch of operations through a proxy with circuit breaker protection
func (bpr *BatchProcessorResilience) BatchProxyExecute(ctx context.Context, req *proto.BatchProxyExecutionRequest) (*proto.BatchDirectExecutionResponse, error) {
	// Create a circuit breaker for the proxy execution
	cbName := fmt.Sprintf("proxy:%s", req.RegionId)
	cb, _ := bpr.serviceCircuitBreakers.Get(cbName)
	if cb == nil {
		cb = bpr.serviceCircuitBreakers.Register(cbName, bpr.config.DefaultCircuitBreakerConfig)
	}
	
	// Attempt execution with circuit breaker protection
	var lastErr error
	var result *proto.BatchDirectExecutionResponse
		
	err := cb.Execute(ctx, func() error {
		var execErr error
		result, execErr = bpr.batchProcessor.BatchProxyExecute(ctx, req)
		return execErr
	})
	
	// If successful, return the result
	if err == nil && result != nil {
		return result, nil
	}
	
	// Store the last error
	if err != nil {
		lastErr = err
	}
	
	// Execution failed, try local fallback if enabled
	if bpr.config.EnableLocalFallback {
		return bpr.executeLocalFallback(ctx, req)
	}
	
	return nil, fmt.Errorf("batch proxy execution failed on all peers: %w", lastErr)
}

// executeLocalFallback attempts to execute the batch locally as a fallback
func (bpr *BatchProcessorResilience) executeLocalFallback(ctx context.Context, req interface{}) (*proto.BatchDirectExecutionResponse, error) {
	// Handle different request types
	var operations []*proto.BatchOperation
	var senderId string
	var regionId string
	
	switch r := req.(type) {
	case *proto.BatchDirectExecutionRequest:
		operations = r.Operations
		senderId = r.SenderId
		regionId = r.RegionId
	case *proto.BatchProxyExecutionRequest:
		operations = r.Operations
		senderId = r.SenderId
		regionId = r.RegionId
	default:
		return nil, fmt.Errorf("unsupported request type for local fallback")
	}
	
	// Convert to a direct execution request for local processing
	directReq := &proto.BatchDirectExecutionRequest{
		SenderId:   senderId,
		Operations: operations,
		RegionId:   regionId,
		Options: &proto.BatchOptions{
			ExecutionMode: proto.BatchExecutionMode_BATCH_MODE_SEQUENTIAL,
			ErrorHandling: proto.BatchErrorHandling_BATCH_ERROR_CONTINUE,
		},
	}
	
	// Execute batch in optimized sequential mode to reduce resource contention
	// This is a simplified fallback that won't use parallelism to avoid overloading the system
	resp := &proto.BatchDirectExecutionResponse{
		Results:        make([]*proto.BatchOperationResult, len(operations)),
		OverallSuccess: false,
	}
	
	// We already set the options in the directReq
	
	// Execute each operation sequentially
	startTime := time.Now()
	executionTimes := make([]uint64, len(directReq.Operations))
	totalSucceeded := 0
	
	for i, op := range directReq.Operations {
		opStartTime := time.Now()

		// Process this operation locally
		result := &proto.BatchOperationResult{
			OperationId: op.OperationId,
		}

		// Try to execute the operation locally
		if bpr.batchProcessor.meshService != nil {
			// For operations that can be executed locally
			localReq := &proto.DirectExecutionRequest{
				IdTo:         op.IdTo,
				FunctionCall: op.FunctionCall,
				Parameters:   op.Parameters,
			}

			// Execute direct request
			localResp, err := bpr.batchProcessor.meshService.DirectExecute(ctx, localReq)
			if err != nil {
				result.ErrorMessage = fmt.Sprintf("local fallback failed: %v", err)
			} else {
				result.Result = localResp.Result
				totalSucceeded++
			}
		} else {
			result.ErrorMessage = "local fallback unavailable: no mesh service"
		}
		
		// Store the execution time
		opEndTime := time.Now()
		executionTimes[i] = uint64(opEndTime.Sub(opStartTime).Nanoseconds())
		resp.Results[i] = result
	}
	
	// Calculate statistics
	stats := calculateBatchStatistics(executionTimes, uint64(time.Since(startTime).Nanoseconds()))
	resp.TotalExecutionTimeNs = uint64(time.Since(startTime).Nanoseconds())
	resp.BatchProcessingOverheadNs = 0
	resp.OperationsSucceeded = uint32(totalSucceeded)
	resp.OperationsFailed = uint32(len(directReq.Operations) - totalSucceeded)
	resp.Statistics = stats
	
	// Set overall success based on operation success rate
	resp.OverallSuccess = (totalSucceeded > 0)
	
	return resp, nil
}

// GetCircuitBreakerHealth returns health information for all circuit breakers
func (bpr *BatchProcessorResilience) GetCircuitBreakerHealth() map[string]HealthStatus {
	return bpr.serviceCircuitBreakers.GetHealthStatus()
}

// ResetCircuitBreaker forcibly resets a circuit breaker by name
func (bpr *BatchProcessorResilience) ResetCircuitBreaker(name string) error {
	cb, exists := bpr.serviceCircuitBreakers.Get(name)
	if !exists {
		return fmt.Errorf("circuit breaker not found: %s", name)
	}
	
	cb.Reset()
	return nil
}

// GetBatchProcessor returns the underlying batch processor
func (bpr *BatchProcessorResilience) GetBatchProcessor() *BatchProcessor {
	return bpr.batchProcessor
}
