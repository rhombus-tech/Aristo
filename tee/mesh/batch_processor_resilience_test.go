package mesh

import (
	"context"
	"testing"

	"github.com/rhombus-tech/vm/tee/proto"
	"github.com/stretchr/testify/assert"
)

// TestBatchResilienceBasic tests the basic functionality of batch processor resilience
func TestBatchResilienceBasic(t *testing.T) {
	// Skip if running in short mode
	if testing.Short() {
		t.Skip("Skipping batch resilience test in short mode")
	}
	
	// Create a real batch processor (with nil service) just for testing
	bp := &BatchProcessor{
		meshService: nil,
		metrics: &BatchProcessorMetrics{},
	}
	
	// Create resilience with simple configuration
	config := &BatchResilienceConfig{
		MaxRetries: 2,
		RetryBackoffMs: 10,
		EnableLocalFallback: false,
	}
	
	// Create the resilience layer
	resilience := NewBatchProcessorResilience(bp, config)
	
	// Verify the resilience layer has the correct configuration
	assert.Equal(t, 2, resilience.config.MaxRetries)
	
	// Test basic operations
	ctx := context.Background()
	req := &proto.BatchDirectExecutionRequest{
		RegionId: "test-region",
		Operations: []*proto.BatchOperation{
			{
				OperationId: "op1",
			},
		},
	}
	
	// Since the mesh service is nil, we expect an error, but this shows that the
	// resilience layer at least attempts to execute the request
	_, err := resilience.BatchDirectExecute(ctx, req)
	assert.Error(t, err, "Expected error with nil service")
	
	// Test circuit breaker reset functionality
	cbName := "direct:test-region"
	resilience.serviceCircuitBreakers.Register(cbName, nil)
	err = resilience.ResetCircuitBreaker(cbName)
	assert.NoError(t, err, "Circuit breaker reset should succeed")
}

// TestBatchProxyHandling tests that the proxy execution path works
func TestBatchProxyHandling(t *testing.T) {
	// Skip if running in short mode
	if testing.Short() {
		t.Skip("Skipping batch proxy test in short mode")
	}
	
	// Create a real batch processor (with nil service) just for testing
	bp := &BatchProcessor{
		meshService: nil,
		metrics: &BatchProcessorMetrics{},
	}
	
	// Create resilience with simple configuration
	config := &BatchResilienceConfig{
		MaxRetries: 2,
		RetryBackoffMs: 10,
		EnableLocalFallback: false,
	}
	
	// Create the resilience layer
	resilience := NewBatchProcessorResilience(bp, config)
	
	// Test basic operations
	ctx := context.Background()
	req := &proto.BatchProxyExecutionRequest{
		RegionId: "test-region",
	}
	
	// Since the mesh service is nil, we expect an error, but this shows that the
	// resilience layer at least attempts to execute the request
	_, err := resilience.BatchProxyExecute(ctx, req)
	assert.Error(t, err, "Expected error with nil service")
}

// TestCircuitBreakerReset tests the reset functionality
func TestCircuitBreakerReset(t *testing.T) {
	// Skip if running in short mode
	if testing.Short() {
		t.Skip("Skipping circuit breaker test in short mode")
	}
	
	// Create resilience with simple configuration and real batch processor
	bp := &BatchProcessor{
		meshService: nil,
		metrics: &BatchProcessorMetrics{},
	}

	config := &BatchResilienceConfig{
		MaxRetries: 2,
		RetryBackoffMs: 10,
		EnableLocalFallback: false,
	}
	
	resilience := NewBatchProcessorResilience(bp, config)
	
	// Register a circuit breaker
	cbName := "test:region"
	circuitBreaker := resilience.serviceCircuitBreakers.Register(cbName, nil)
	
	// Test that we can reset a circuit breaker
	err := resilience.ResetCircuitBreaker(cbName)
	assert.NoError(t, err, "Resetting circuit breaker should not error")
	assert.Equal(t, CircuitClosed, circuitBreaker.GetState(), "Circuit should be closed after reset")
}

// TestBatchProcessorGetter tests the getter for the batch processor
func TestBatchProcessorGetter(t *testing.T) {
	// Create resilience with real batch processor
	bp := &BatchProcessor{
		meshService: nil,
		metrics: &BatchProcessorMetrics{},
	}
	
	config := DefaultBatchResilienceConfig()
	resilience := NewBatchProcessorResilience(bp, config)
	
	// Test that we can get the batch processor
	gotBp := resilience.GetBatchProcessor()
	assert.Equal(t, bp, gotBp, "GetBatchProcessor should return the original batch processor")
}
