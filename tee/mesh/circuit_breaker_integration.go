package mesh

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"
)

// CircuitBreakerFactory provides circuit breakers for different services
type CircuitBreakerFactory struct {
	registry *CircuitBreakerRegistry
	metrics  *MetricsPublisher
}

// NewCircuitBreakerFactory creates a new factory with default handlers
func NewCircuitBreakerFactory() *CircuitBreakerFactory {
	registry := NewCircuitBreakerRegistry()
	metrics := NewMetricsPublisher(100)
	
	// Add default alert handlers
	registry.AddAlertHandler(&StandardAlertHandler{LogPrefix: "TEEMesh"})
	registry.AddAlertHandler(metrics)
	
	return &CircuitBreakerFactory{
		registry: registry,
		metrics:  metrics,
	}
}

// GetOrCreate returns an existing circuit breaker or creates a new one
func (f *CircuitBreakerFactory) GetOrCreate(name string, customConfig *EnhancedCircuitBreakerConfig) *EnhancedCircuitBreaker {
	cb, exists := f.registry.Get(name)
	if exists {
		return cb
	}
	
	return f.registry.Register(name, customConfig)
}

// StartMetricsServer starts an HTTP server for exposing circuit breaker metrics
func (f *CircuitBreakerFactory) StartMetricsServer(address string) *http.Server {
	mux := http.NewServeMux()
	
	// Register metrics handlers
	handler := NewHTTPMetricsHandler(f.registry)
	handler.RegisterHandlers(mux)
	
	// Create server
	server := &http.Server{
		Addr:    address,
		Handler: mux,
	}
	
	// Start server in a goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Metrics server error: %v", err)
		}
	}()
	
	return server
}

// AddWebhookAlertHandler adds a webhook alert handler for the specified destination
func (f *CircuitBreakerFactory) AddWebhookAlertHandler(webhookURL string, minLevel AlertLevel) {
	webhook := NewWebhookAlertHandler(webhookURL, minLevel)
	f.registry.AddAlertHandler(webhook)
}

// CircuitBreakerIntegration demonstrates how to integrate the enhanced circuit breaker
// with the HyperTeeController execute method for dual-path execution
type CircuitBreakerIntegration struct {
	// Circuit breakers for different execution paths
	meshCircuitBreaker      *EnhancedCircuitBreaker
	coordinatorCircuitBreaker *EnhancedCircuitBreaker
	
	// Factory for creating circuit breakers
	factory *CircuitBreakerFactory
	
	// Metrics server
	metricsServer *http.Server
}

// NewCircuitBreakerIntegration creates a new integration with default settings
func NewCircuitBreakerIntegration() *CircuitBreakerIntegration {
	factory := NewCircuitBreakerFactory()
	
	// Create circuit breakers for different execution paths
	meshConfig := DefaultEnhancedCircuitBreakerConfig()
	meshConfig.FailureThreshold = 5
	meshConfig.ResetTimeout = 30 * time.Second
	meshConfig.LatencyThresholdP95 = 100 * time.Millisecond
	meshConfig.MaxLatencyThreshold = 200 * time.Millisecond
	
	coordinatorConfig := DefaultEnhancedCircuitBreakerConfig()
	coordinatorConfig.FailureThreshold = 10      // More tolerant
	coordinatorConfig.ResetTimeout = 60 * time.Second
	coordinatorConfig.LatencyThresholdP95 = 300 * time.Millisecond
	coordinatorConfig.MaxLatencyThreshold = 500 * time.Millisecond
	
	return &CircuitBreakerIntegration{
		meshCircuitBreaker:      factory.GetOrCreate("mesh-execution", meshConfig),
		coordinatorCircuitBreaker: factory.GetOrCreate("coordinator-execution", coordinatorConfig),
		factory:                  factory,
	}
}

// StartMetricsServer starts the metrics server on the given address
func (i *CircuitBreakerIntegration) StartMetricsServer(address string) {
	i.metricsServer = i.factory.StartMetricsServer(address)
}

// ExecuteWithCircuitBreaker demonstrates how to use circuit breakers in a dual-path
// execution system similar to the HyperTeeController
func (i *CircuitBreakerIntegration) ExecuteWithCircuitBreaker(
	ctx context.Context,
	payload []byte,
	target string,
	region string,
) ([]byte, error) {
	// 1. First attempt direct mesh execution
	meshResult, meshErr := i.tryMeshExecution(ctx, payload, target, region)
	if meshErr == nil {
		return meshResult, nil
	}
	
	// 2. If mesh execution fails, use coordinator as fallback
	log.Printf("Mesh execution failed, falling back to coordinator: %v", meshErr)
	return i.tryCoordinatorExecution(ctx, payload, target, region)
}

// tryMeshExecution attempts to execute via the mesh network with circuit breaker protection
func (i *CircuitBreakerIntegration) tryMeshExecution(
	ctx context.Context,
	payload []byte,
	target string,
	region string,
) ([]byte, error) {
	var result []byte
	
	// Use the mesh circuit breaker to protect this execution path
	err := i.meshCircuitBreaker.Execute(ctx, func() error {
		// This is where the actual mesh execution would happen
		// For example:
		// result, err = meshService.DirectExecute(ctx, target, payload)
		// if err != nil {
		//     return err
		// }
		
		// Simulate mesh execution for demonstration
		if len(payload) == 0 {
			return fmt.Errorf("invalid payload")
		}
		
		// Simulate successful execution
		result = append([]byte("MESH:"), payload...)
		return nil
	})
	
	return result, err
}

// tryCoordinatorExecution attempts to execute via the coordinator with circuit breaker protection
func (i *CircuitBreakerIntegration) tryCoordinatorExecution(
	ctx context.Context,
	payload []byte,
	target string,
	region string,
) ([]byte, error) {
	var result []byte
	
	// Use the coordinator circuit breaker to protect this execution path
	err := i.coordinatorCircuitBreaker.Execute(ctx, func() error {
		// This is where the actual coordinator execution would happen
		// For example:
		// result, err = coordinatorService.Execute(ctx, target, region, payload)
		// if err != nil {
		//     return err
		// }
		
		// Simulate coordinator execution for demonstration
		if len(payload) == 0 {
			return fmt.Errorf("invalid payload")
		}
		
		// Simulate successful execution
		result = append([]byte("COORDINATOR:"), payload...)
		return nil
	})
	
	return result, err
}

// GetHealthStatus returns the health status of all circuit breakers
func (i *CircuitBreakerIntegration) GetHealthStatus() map[string]HealthStatus {
	return i.factory.registry.GetHealthStatus()
}

// StopMetricsServer gracefully stops the metrics server
func (i *CircuitBreakerIntegration) StopMetricsServer(ctx context.Context) error {
	if i.metricsServer != nil {
		return i.metricsServer.Shutdown(ctx)
	}
	return nil
}

// Example usage in main:
/*
func main() {
	// Create the integration
	integration := NewCircuitBreakerIntegration()
	
	// Start metrics server on port 8080
	integration.StartMetricsServer(":8080")
	
	// Add Slack webhook for critical alerts
	integration.factory.AddWebhookAlertHandler(
		"https://hooks.slack.com/services/XXXXX/YYYYY/ZZZZZ",
		ErrorLevel,
	)
	
	// Example execution
	ctx := context.Background()
	result, err := integration.ExecuteWithCircuitBreaker(
		ctx,
		[]byte("hello world"),
		"worker1",
		"us-west",
	)
	
	if err != nil {
		log.Fatalf("Execution failed: %v", err)
	}
	
	log.Printf("Execution result: %s", string(result))
	
	// Graceful shutdown on program exit
	integration.StopMetricsServer(ctx)
}
*/
