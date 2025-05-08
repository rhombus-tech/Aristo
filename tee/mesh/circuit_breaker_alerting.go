package mesh

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// StandardAlertHandler is a basic implementation of the AlertHandler interface
// that logs alerts at the appropriate level
type StandardAlertHandler struct {
	LogPrefix string
}

// HandleAlert logs the alert with appropriate severity level
func (h *StandardAlertHandler) HandleAlert(alert Alert) {
	prefix := h.LogPrefix
	if prefix == "" {
		prefix = "CircuitBreaker"
	}

	switch alert.Level {
	case InfoLevel:
		log.Printf("[%s:INFO] %s", prefix, alert.Message)
	case WarningLevel:
		log.Printf("[%s:WARNING] %s", prefix, alert.Message)
	case ErrorLevel:
		log.Printf("[%s:ERROR] %s", prefix, alert.Message)
	case CriticalLevel:
		log.Printf("[%s:CRITICAL] %s", prefix, alert.Message)
	}
}

// MetricsPublisher is a handler that collects circuit breaker metrics
// and provides them through a metrics registry
type MetricsPublisher struct {
	mu              sync.RWMutex
	circuitMetrics  map[string]*EnhancedCircuitBreakerMetrics
	alertHistory    map[string][]Alert
	maxAlertHistory int
}

// NewMetricsPublisher creates a new metrics publisher
func NewMetricsPublisher(maxAlertHistory int) *MetricsPublisher {
	if maxAlertHistory <= 0 {
		maxAlertHistory = 100
	}
	
	return &MetricsPublisher{
		circuitMetrics:  make(map[string]*EnhancedCircuitBreakerMetrics),
		alertHistory:    make(map[string][]Alert),
		maxAlertHistory: maxAlertHistory,
	}
}

// HandleAlert processes an alert and updates metrics
func (mp *MetricsPublisher) HandleAlert(alert Alert) {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	
	// Store the circuit metrics
	mp.circuitMetrics[alert.CircuitName] = alert.Metrics
	
	// Add to alert history
	alerts := mp.alertHistory[alert.CircuitName]
	if len(alerts) >= mp.maxAlertHistory {
		// Remove oldest alert
		alerts = alerts[1:]
	}
	
	// Add new alert
	alerts = append(alerts, alert)
	mp.alertHistory[alert.CircuitName] = alerts
}

// GetCircuitMetrics returns metrics for all circuits
func (mp *MetricsPublisher) GetCircuitMetrics() map[string]*EnhancedCircuitBreakerMetrics {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	
	// Create a copy to avoid concurrent access issues
	result := make(map[string]*EnhancedCircuitBreakerMetrics, len(mp.circuitMetrics))
	for name, metrics := range mp.circuitMetrics {
		result[name] = metrics
	}
	
	return result
}

// GetAlertHistory returns alert history for a specific circuit
func (mp *MetricsPublisher) GetAlertHistory(circuitName string) []Alert {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	
	alerts, ok := mp.alertHistory[circuitName]
	if !ok {
		return nil
	}
	
	// Create a copy to avoid concurrent access issues
	result := make([]Alert, len(alerts))
	copy(result, alerts)
	
	return result
}

// WebhookAlertHandler sends alerts to a webhook endpoint
type WebhookAlertHandler struct {
	WebhookURL     string
	MinimumLevel   AlertLevel
	Client         *http.Client
	AdditionalTags map[string]string
}

// NewWebhookAlertHandler creates a new webhook alert handler
func NewWebhookAlertHandler(webhookURL string, minimumLevel AlertLevel) *WebhookAlertHandler {
	return &WebhookAlertHandler{
		WebhookURL:     webhookURL,
		MinimumLevel:   minimumLevel,
		Client:         &http.Client{Timeout: 5 * time.Second},
		AdditionalTags: make(map[string]string),
	}
}

// HandleAlert sends the alert to the webhook endpoint
func (wh *WebhookAlertHandler) HandleAlert(alert Alert) {
	// Only send alerts at or above the minimum level
	if alert.Level < wh.MinimumLevel {
		return
	}
	
	// Create webhook payload
	payload := map[string]interface{}{
		"circuit":     alert.CircuitName,
		"level":       alertLevelToString(alert.Level),
		"message":     alert.Message,
		"timestamp":   alert.Timestamp.Format(time.RFC3339),
		"environment": "production", // Default environment
	}
	
	// Add failure rate if available
	if alert.Metrics != nil {
		payload["metrics"] = map[string]interface{}{
			"total_attempts":     alert.Metrics.TotalAttempts,
			"successful":         alert.Metrics.SuccessfulAttempts,
			"failed":             alert.Metrics.FailedAttempts,
			"consecutive_fails":  alert.Metrics.ConsecutiveFailures,
			"open_count":         alert.Metrics.OpenCount,
		}
	}
	
	// Add additional tags
	for k, v := range wh.AdditionalTags {
		payload[k] = v
	}
	
	// Add context if available
	if alert.Context != nil {
		payload["context"] = alert.Context
	}
	
	// Convert to JSON
	jsonData, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshaling webhook alert: %v", err)
		return
	}
	
	// Send the request
	resp, err := wh.Client.Post(wh.WebhookURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("Error sending webhook alert: %v", err)
		return
	}
	defer resp.Body.Close()
	
	if resp.StatusCode >= 300 {
		log.Printf("Webhook returned non-success status: %d", resp.StatusCode)
	}
}

// CircuitBreakerRegistry manages multiple circuit breakers and their metrics
type CircuitBreakerRegistry struct {
	breakers map[string]*EnhancedCircuitBreaker
	mu       sync.RWMutex
	// Common alert handlers for all circuit breakers
	alertHandlers []AlertHandler
}

// NewCircuitBreakerRegistry creates a new circuit breaker registry
func NewCircuitBreakerRegistry() *CircuitBreakerRegistry {
	return &CircuitBreakerRegistry{
		breakers:      make(map[string]*EnhancedCircuitBreaker),
		alertHandlers: make([]AlertHandler, 0),
	}
}

// Register adds a new circuit breaker to the registry
func (r *CircuitBreakerRegistry) Register(name string, config *EnhancedCircuitBreakerConfig) *EnhancedCircuitBreaker {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	// Check if already exists
	if cb, exists := r.breakers[name]; exists {
		return cb
	}
	
	// Apply common alert handlers
	if config == nil {
		config = DefaultEnhancedCircuitBreakerConfig()
	}
	
	// Add registry's alert handlers to the circuit breaker config
	config.AlertHandlers = append(config.AlertHandlers, r.alertHandlers...)
	
	// Create and register the circuit breaker
	cb := NewEnhancedCircuitBreaker(name, config)
	r.breakers[name] = cb
	
	return cb
}

// Get retrieves a circuit breaker by name
func (r *CircuitBreakerRegistry) Get(name string) (*EnhancedCircuitBreaker, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	cb, exists := r.breakers[name]
	return cb, exists
}

// GetAll returns all registered circuit breakers
func (r *CircuitBreakerRegistry) GetAll() map[string]*EnhancedCircuitBreaker {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	result := make(map[string]*EnhancedCircuitBreaker, len(r.breakers))
	for name, cb := range r.breakers {
		result[name] = cb
	}
	
	return result
}

// GetHealthStatus returns health status for all circuit breakers
func (r *CircuitBreakerRegistry) GetHealthStatus() map[string]HealthStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	result := make(map[string]HealthStatus, len(r.breakers))
	for name, cb := range r.breakers {
		result[name] = cb.GetHealthStatus()
	}
	
	return result
}

// AddAlertHandler adds an alert handler to all current and future circuit breakers
func (r *CircuitBreakerRegistry) AddAlertHandler(handler AlertHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	// Add to registry's handlers
	r.alertHandlers = append(r.alertHandlers, handler)
	
	// Add to all existing circuit breakers
	for _, cb := range r.breakers {
		cb.AddAlertHandler(handler)
	}
}

// PrometheusMetricsCollector collects circuit breaker metrics for Prometheus
type PrometheusMetricsCollector struct {
	registry *CircuitBreakerRegistry
}

// NewPrometheusMetricsCollector creates a new Prometheus metrics collector
func NewPrometheusMetricsCollector(registry *CircuitBreakerRegistry) *PrometheusMetricsCollector {
	return &PrometheusMetricsCollector{
		registry: registry,
	}
}

// GetMetrics generates Prometheus metrics strings for all circuit breakers
func (pmc *PrometheusMetricsCollector) GetMetrics() string {
	var result string
	
	// Get all circuit breakers
	breakers := pmc.registry.GetAll()
	
	// Define metrics
	result += "# HELP circuit_breaker_state Current state of the circuit breaker (0=closed, 1=half-open, 2=open)\n"
	result += "# TYPE circuit_breaker_state gauge\n"
	
	result += "# HELP circuit_breaker_total_attempts Total number of execution attempts\n"
	result += "# TYPE circuit_breaker_total_attempts counter\n"
	
	result += "# HELP circuit_breaker_successful_attempts Total number of successful execution attempts\n"
	result += "# TYPE circuit_breaker_successful_attempts counter\n"
	
	result += "# HELP circuit_breaker_failed_attempts Total number of failed execution attempts\n"
	result += "# TYPE circuit_breaker_failed_attempts counter\n"
	
	result += "# HELP circuit_breaker_consecutive_failures Current number of consecutive failures\n"
	result += "# TYPE circuit_breaker_consecutive_failures gauge\n"
	
	result += "# HELP circuit_breaker_consecutive_successes Current number of consecutive successes\n"
	result += "# TYPE circuit_breaker_consecutive_successes gauge\n"
	
	result += "# HELP circuit_breaker_open_count Total number of times the circuit has opened\n"
	result += "# TYPE circuit_breaker_open_count counter\n"
	
	result += "# HELP circuit_breaker_failure_rate Current failure rate (0.0-1.0)\n"
	result += "# TYPE circuit_breaker_failure_rate gauge\n"
	
	result += "# HELP circuit_breaker_latency_p95_ms 95th percentile latency in milliseconds\n"
	result += "# TYPE circuit_breaker_latency_p95_ms gauge\n"
	
	result += "# HELP circuit_breaker_latency_p99_ms 99th percentile latency in milliseconds\n"
	result += "# TYPE circuit_breaker_latency_p99_ms gauge\n"
	
	result += "# HELP circuit_breaker_current_load Current number of concurrent requests\n"
	result += "# TYPE circuit_breaker_current_load gauge\n"
	
	// Generate metrics for each circuit breaker
	for name, cb := range breakers {
		// Escape name for Prometheus labels
		escapedName := escapePrometheusLabel(name)
		
		// Get health status
		health := cb.GetHealthStatus()
		
		// Get enhanced metrics
		metrics := cb.GetEnhancedMetrics()
		
		// Generate metrics
		result += fmt.Sprintf("circuit_breaker_state{name=\"%s\"} %d\n", escapedName, health.State)
		result += fmt.Sprintf("circuit_breaker_total_attempts{name=\"%s\"} %d\n", escapedName, metrics.TotalAttempts)
		result += fmt.Sprintf("circuit_breaker_successful_attempts{name=\"%s\"} %d\n", escapedName, metrics.SuccessfulAttempts)
		result += fmt.Sprintf("circuit_breaker_failed_attempts{name=\"%s\"} %d\n", escapedName, metrics.FailedAttempts)
		result += fmt.Sprintf("circuit_breaker_consecutive_failures{name=\"%s\"} %d\n", escapedName, metrics.ConsecutiveFailures)
		result += fmt.Sprintf("circuit_breaker_consecutive_successes{name=\"%s\"} %d\n", escapedName, metrics.ConsecutiveSuccesses)
		result += fmt.Sprintf("circuit_breaker_open_count{name=\"%s\"} %d\n", escapedName, metrics.OpenCount)
		result += fmt.Sprintf("circuit_breaker_failure_rate{name=\"%s\"} %f\n", escapedName, health.FailureRate)
		result += fmt.Sprintf("circuit_breaker_latency_p95_ms{name=\"%s\"} %f\n", escapedName, health.ResponseTimes.P95ms)
		result += fmt.Sprintf("circuit_breaker_latency_p99_ms{name=\"%s\"} %f\n", escapedName, health.ResponseTimes.P99ms)
		result += fmt.Sprintf("circuit_breaker_current_load{name=\"%s\"} %d\n", escapedName, health.CurrentLoad)
		
		// Add error category metrics
		for category, count := range health.ErrorDistribution {
			result += fmt.Sprintf("circuit_breaker_errors{name=\"%s\",category=\"%s\"} %d\n", 
				escapedName, errorCategoryToString(category), count)
		}
	}
	
	return result
}

// errorCategoryToString converts ErrorCategory to a string
func errorCategoryToString(category ErrorCategory) string {
	switch category {
	case UnknownError:
		return "unknown"
	case TimeoutError:
		return "timeout"
	case ConnectionError:
		return "connection"
	case ResourceError:
		return "resource"
	case AuthError:
		return "auth"
	case StateError:
		return "state"
	default:
		return fmt.Sprintf("category_%d", category)
	}
}

// escapePrometheusLabel escapes a label value for Prometheus
func escapePrometheusLabel(label string) string {
	// Simple implementation - replace quotes with underscores
	// In a real implementation, this would be more comprehensive
	result := ""
	for _, r := range label {
		if r == '"' || r == '\\' || r == '\n' {
			result += "_"
		} else {
			result += string(r)
		}
	}
	return result
}

// HTTPMetricsHandler provides an HTTP handler for exposing circuit breaker metrics
type HTTPMetricsHandler struct {
	Registry       *CircuitBreakerRegistry
	PrometheusPath string
	JSONPath       string
}

// NewHTTPMetricsHandler creates a new HTTP metrics handler
func NewHTTPMetricsHandler(registry *CircuitBreakerRegistry) *HTTPMetricsHandler {
	return &HTTPMetricsHandler{
		Registry:       registry,
		PrometheusPath: "/metrics/circuit-breakers",
		JSONPath:       "/api/circuit-breakers",
	}
}

// RegisterHandlers registers the HTTP handlers with the provided mux
func (h *HTTPMetricsHandler) RegisterHandlers(mux *http.ServeMux) {
	// Prometheus metrics endpoint
	mux.HandleFunc(h.PrometheusPath, h.handlePrometheusMetrics)
	
	// JSON metrics endpoint
	mux.HandleFunc(h.JSONPath, h.handleJSONMetrics)
	
	// Individual circuit breaker endpoint
	mux.HandleFunc(h.JSONPath+"/", h.handleCircuitBreakerDetails)
}

// handlePrometheusMetrics handles Prometheus metrics requests
func (h *HTTPMetricsHandler) handlePrometheusMetrics(w http.ResponseWriter, r *http.Request) {
	collector := NewPrometheusMetricsCollector(h.Registry)
	metrics := collector.GetMetrics()
	
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(metrics))
}

// handleJSONMetrics handles JSON metrics requests
func (h *HTTPMetricsHandler) handleJSONMetrics(w http.ResponseWriter, r *http.Request) {
	// Get health status for all circuit breakers
	status := h.Registry.GetHealthStatus()
	
	// Convert to JSON
	jsonData, err := json.Marshal(status)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonData)
}

// handleCircuitBreakerDetails handles detailed circuit breaker requests
func (h *HTTPMetricsHandler) handleCircuitBreakerDetails(w http.ResponseWriter, r *http.Request) {
	// Extract circuit breaker name from URL
	name := r.URL.Path[len(h.JSONPath)+1:]
	if name == "" {
		http.Error(w, "Circuit breaker name required", http.StatusBadRequest)
		return
	}
	
	// Get circuit breaker
	cb, exists := h.Registry.Get(name)
	if !exists {
		http.Error(w, "Circuit breaker not found", http.StatusNotFound)
		return
	}
	
	// Get detailed metrics
	metrics := cb.GetEnhancedMetrics()
	health := cb.GetHealthStatus()
	
	// Combine into result
	result := map[string]interface{}{
		"name":      name,
		"health":    health,
		"metrics":   metrics,
		"state":     cb.GetState(),
		"stateChanges": metrics.StateChangeLog,
	}
	
	// Convert to JSON
	jsonData, err := json.Marshal(result)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonData)
}
