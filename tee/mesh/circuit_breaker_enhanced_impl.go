package mesh

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync/atomic"
	"time"
)

// EnhancedCircuitBreaker extends the basic CircuitBreaker with sophisticated
// failure detection and production-ready monitoring capabilities
type EnhancedCircuitBreaker struct {
	*CircuitBreaker
	config                *EnhancedCircuitBreakerConfig
	enhancedMetrics       *EnhancedCircuitBreakerMetrics
	errorCategoryMapper   ErrorCategoryMapper
	healthCheckTicker     *time.Ticker
	healthCheckCancelFunc context.CancelFunc
	alertHandlers         []AlertHandler
}

// NewEnhancedCircuitBreaker creates a new enhanced circuit breaker
func NewEnhancedCircuitBreaker(name string, config *EnhancedCircuitBreakerConfig) *EnhancedCircuitBreaker {
	if config == nil {
		config = DefaultEnhancedCircuitBreakerConfig()
	}
	
	// Create base circuit breaker
	baseBreaker := NewCircuitBreaker(name, config.CircuitBreakerConfig)
	
	// Initialize enhanced metrics
	enhancedMetrics := &EnhancedCircuitBreakerMetrics{
		CircuitBreakerMetrics: baseBreaker.metrics,
		BucketSizeSeconds:     config.TimeWindowSeconds / int64(config.SlidingWindowBuckets),
		SlidingWindow:         make([]*TimeBucket, config.SlidingWindowBuckets),
		ErrorCounts:           make(map[ErrorCategory]int64),
		TimeInStates:          make(map[CircuitBreakerState]time.Duration),
		StateChangeLog:        make([]StateChange, 0, 100), // Preserve last 100 state changes
		AlertCounts:           make(map[AlertLevel]int64),
	}
	
	// Initialize sliding window buckets
	now := time.Now()
	bucketDuration := time.Duration(enhancedMetrics.BucketSizeSeconds) * time.Second
	for i := 0; i < config.SlidingWindowBuckets; i++ {
		startTime := now.Add(-bucketDuration * time.Duration(config.SlidingWindowBuckets-i))
		enhancedMetrics.SlidingWindow[i] = &TimeBucket{
			StartTime:      startTime,
			EndTime:        startTime.Add(bucketDuration),
			Requests:       0,
			Failures:       0,
			SuccessLatency: make([]time.Duration, 0),
			FailureLatency: make([]time.Duration, 0),
			ErrorCounts:    make(map[ErrorCategory]int64),
		}
	}
	
	// Create enhanced circuit breaker
	ecb := &EnhancedCircuitBreaker{
		CircuitBreaker:      baseBreaker,
		config:              config,
		enhancedMetrics:     enhancedMetrics,
		errorCategoryMapper: DefaultErrorCategoryMapper,
		alertHandlers:       config.AlertHandlers,
	}
	
	// Add state change callback to track state transitions
	baseBreaker.AddStateChangeCallback(ecb.onStateChange)
	
	// Start health check if interval is positive
	if config.HealthCheckInterval > 0 {
		ecb.startHealthChecks()
	}
	
	return ecb
}

// Execute runs the provided function with enhanced circuit breaker protection
func (ecb *EnhancedCircuitBreaker) Execute(ctx context.Context, operation func() error) error {
	// First, check the current state to make decisions
	ecb.stateMutex.RLock()
	currentState := ecb.state
	tripTime := ecb.tripTime
	ecb.stateMutex.RUnlock()
	
	// If circuit is open and reset timeout hasn't elapsed, immediately reject
	if currentState == CircuitOpen && time.Since(tripTime) <= ecb.config.ResetTimeout {
		atomic.AddInt64(&ecb.metrics.TotalAttempts, 1)
		ecb.recordRequestInWindow(false, nil, 0, nil)
		return ErrCircuitBreakerOpen
	}
	
	// In half-open state, check if we should allow this test request
	if currentState == CircuitHalfOpen {
		atomicCons := atomic.LoadInt64(&ecb.metrics.ConsecutiveSuccesses)
		if atomicCons >= ecb.config.SuccessThreshold {
			atomic.AddInt64(&ecb.metrics.TotalAttempts, 1)
			ecb.recordRequestInWindow(false, nil, 0, nil)
			return ErrCircuitBreakerOpen
		}
	}
	
	// Track concurrent load
	ecb.enhancedMetrics.LoadMutex.Lock()
	atomic.AddInt64(&ecb.enhancedMetrics.CurrentLoad, 1)
	currentLoad := atomic.LoadInt64(&ecb.enhancedMetrics.CurrentLoad)
	if currentLoad > atomic.LoadInt64(&ecb.enhancedMetrics.PeakLoad) {
		atomic.StoreInt64(&ecb.enhancedMetrics.PeakLoad, currentLoad)
	}
	ecb.enhancedMetrics.LoadMutex.Unlock()
	
	// Ensure we decrement the load counter when done
	defer func() {
		atomic.AddInt64(&ecb.enhancedMetrics.CurrentLoad, -1)
	}()
	
	// Increment total attempts
	atomic.AddInt64(&ecb.metrics.TotalAttempts, 1)
	
	// Create timeout context for the operation
	var cancel context.CancelFunc
	var opCtx context.Context
	
	if ecb.config.Timeout > 0 {
		opCtx, cancel = context.WithTimeout(ctx, ecb.config.Timeout)
		defer cancel()
	} else {
		opCtx = ctx
	}
	
	// Track operation timing
	startTime := time.Now()
	
	// Execute the operation
	var operationErr error
	
	// For very fast operations, just run directly
	if ecb.config.Timeout <= time.Millisecond*50 {
		operationErr = operation()
	} else {
		// Otherwise use a channel to handle possible timeout
		done := make(chan error, 1)
		
		// Run the operation in a goroutine
		go func() {
			done <- operation()
		}()
		
		// Wait for operation completion or timeout
		select {
		case operationErr = <-done:
			// Operation completed
		case <-opCtx.Done():
			// Operation timed out
			operationErr = fmt.Errorf("operation timed out after %v: %w", 
				ecb.config.Timeout, opCtx.Err())
		}
	}
	
	// Measure operation duration
	duration := time.Since(startTime)
	
	// Apply latency thresholds
	if duration > ecb.config.MaxLatencyThreshold && operationErr == nil {
		operationErr = fmt.Errorf("operation exceeded latency threshold: %v > %v",
			duration, ecb.config.MaxLatencyThreshold)
	}
	
	// Record success or failure with detailed metrics
	if operationErr != nil {
		var category ErrorCategory
		if ecb.errorCategoryMapper != nil {
			category = ecb.errorCategoryMapper(operationErr)
		} else {
			category = UnknownError
		}
		
		// Check if we're in half-open state, if so, immediately trip back to open
		ecb.stateMutex.RLock()
		isHalfOpen := ecb.state == CircuitHalfOpen
		ecb.stateMutex.RUnlock()
		
		if isHalfOpen {
			// Force back to open state immediately
			ecb.tripBreaker()
		}
		
		// Record in sliding window
		ecb.recordRequestInWindow(false, &category, duration, operationErr)
		
		// Update base metrics
		ecb.recordFailure()
		
		// Check if we need to trigger alerts
		failureRate := ecb.CalculateFailureRate()
		ecb.checkAndTriggerAlerts(failureRate, category, operationErr)
		
		return operationErr
	}
	
	// Record successful operation
	ecb.recordRequestInWindow(true, nil, duration, nil)
	ecb.recordSuccess()
	
	return nil
}

// recordRequestInWindow records a request in the sliding window
func (ecb *EnhancedCircuitBreaker) recordRequestInWindow(success bool, category *ErrorCategory, duration time.Duration, err error) {
	ecb.enhancedMetrics.WindowMutex.Lock()
	defer ecb.enhancedMetrics.WindowMutex.Unlock()
	
	now := time.Now()
	bucketDuration := time.Duration(ecb.enhancedMetrics.BucketSizeSeconds) * time.Second
	
	// Find or create the appropriate time bucket
	var currentBucket *TimeBucket
	for _, bucket := range ecb.enhancedMetrics.SlidingWindow {
		if now.After(bucket.StartTime) && now.Before(bucket.EndTime) || now.Equal(bucket.StartTime) {
			currentBucket = bucket
			break
		}
	}
	
	// If no current bucket found or it's expired, rotate the window
	if currentBucket == nil {
		// Shift buckets
		numBuckets := len(ecb.enhancedMetrics.SlidingWindow)
		for i := 0; i < numBuckets-1; i++ {
			ecb.enhancedMetrics.SlidingWindow[i] = ecb.enhancedMetrics.SlidingWindow[i+1]
		}
		
		// Create new bucket
		newBucketStart := ecb.enhancedMetrics.SlidingWindow[numBuckets-2].EndTime
		ecb.enhancedMetrics.SlidingWindow[numBuckets-1] = &TimeBucket{
			StartTime:      newBucketStart,
			EndTime:        newBucketStart.Add(bucketDuration),
			Requests:       0,
			Failures:       0,
			SuccessLatency: make([]time.Duration, 0),
			FailureLatency: make([]time.Duration, 0),
			ErrorCounts:    make(map[ErrorCategory]int64),
		}
		currentBucket = ecb.enhancedMetrics.SlidingWindow[numBuckets-1]
	}
	
	// Record the request
	currentBucket.Requests++
	if !success {
		currentBucket.Failures++
		currentBucket.FailureLatency = append(currentBucket.FailureLatency, duration)
		
		// Categorize error if available
		if category != nil {
			ecb.enhancedMetrics.ErrorMutex.Lock()
			ecb.enhancedMetrics.ErrorCounts[*category]++
			currentBucket.ErrorCounts[*category]++
			ecb.enhancedMetrics.ErrorMutex.Unlock()
		}
	} else {
		currentBucket.SuccessLatency = append(currentBucket.SuccessLatency, duration)
	}
	
	// Update latency statistics if tracking percentiles
	if ecb.config.TrackLatencyPercentiles {
		ecb.updateLatencyStats()
	}
}

// CalculateFailureRate returns the current failure rate based on the configured strategy
func (ecb *EnhancedCircuitBreaker) CalculateFailureRate() float64 {
	ecb.enhancedMetrics.WindowMutex.RLock()
	defer ecb.enhancedMetrics.WindowMutex.RUnlock()
	
	switch ecb.config.FailureDetectionStrategy {
	case SimpleCountStrategy:
		return ecb.calculateSimpleFailureRate()
	case TimeWeightedStrategy:
		return ecb.calculateTimeWeightedFailureRate()
	case ErrorCategoryStrategy:
		return ecb.calculateCategoryWeightedFailureRate()
	case AdaptiveStrategy:
		return ecb.calculateAdaptiveFailureRate()
	default:
		return ecb.calculateSimpleFailureRate()
	}
}

// calculateSimpleFailureRate calculates a simple failure rate
func (ecb *EnhancedCircuitBreaker) calculateSimpleFailureRate() float64 {
	var totalRequests, totalFailures int64
	
	// Sum up all requests and failures from all buckets
	for _, bucket := range ecb.enhancedMetrics.SlidingWindow {
		totalRequests += bucket.Requests
		totalFailures += bucket.Failures
	}
	
	if totalRequests == 0 {
		return 0.0
	}
	
	return float64(totalFailures) / float64(totalRequests)
}

// calculateTimeWeightedFailureRate weights recent failures more heavily
func (ecb *EnhancedCircuitBreaker) calculateTimeWeightedFailureRate() float64 {
	var weightedRequests, weightedFailures float64
	bucketCount := len(ecb.enhancedMetrics.SlidingWindow)
	
	// Apply higher weights to more recent buckets
	for i, bucket := range ecb.enhancedMetrics.SlidingWindow {
		// Calculate weight (more recent buckets have higher weight)
		weight := float64(i+1) / float64(bucketCount)
		weightedRequests += float64(bucket.Requests) * weight
		weightedFailures += float64(bucket.Failures) * weight
	}
	
	if weightedRequests == 0 {
		return 0.0
	}
	
	return weightedFailures / weightedRequests
}

// calculateCategoryWeightedFailureRate weights errors by category
func (ecb *EnhancedCircuitBreaker) calculateCategoryWeightedFailureRate() float64 {
	var totalRequests int64
	var weightedFailures float64
	
	// Sum up total requests
	for _, bucket := range ecb.enhancedMetrics.SlidingWindow {
		totalRequests += bucket.Requests
	}
	
	if totalRequests == 0 {
		return 0.0
	}
	
	// Calculate weighted failures
	for _, bucket := range ecb.enhancedMetrics.SlidingWindow {
		for category, count := range bucket.ErrorCounts {
			weight := ecb.config.ErrorCategoryWeights[category]
			if weight == 0 {
				weight = 1.0 // Default weight if not specified
			}
			weightedFailures += float64(count) * weight
		}
	}
	
	return weightedFailures / float64(totalRequests)
}

// calculateAdaptiveFailureRate adjusts thresholds based on traffic patterns
func (ecb *EnhancedCircuitBreaker) calculateAdaptiveFailureRate() float64 {
	// Start with a time-weighted approach
	baseRate := ecb.calculateTimeWeightedFailureRate()
	
	// Get the traffic volume to adjust sensitivity
	var totalRequests int64
	for _, bucket := range ecb.enhancedMetrics.SlidingWindow {
		totalRequests += bucket.Requests
	}
	
	// For very low traffic, be more conservative (lower the effective rate)
	// For high traffic, be more aggressive (increase the effective rate)
	if totalRequests < 5 {
		return baseRate * 0.5 // More conservative
	} else if totalRequests > 100 {
		return math.Min(baseRate * 1.5, 1.0) // More aggressive, but cap at 1.0
	}
	
	return baseRate
}

// updateLatencyStats calculates and updates latency percentiles
func (ecb *EnhancedCircuitBreaker) updateLatencyStats() {
	ecb.enhancedMetrics.LatencyMutex.Lock()
	defer ecb.enhancedMetrics.LatencyMutex.Unlock()
	
	// Collect all latency samples
	var allLatencies []float64
	
	for _, bucket := range ecb.enhancedMetrics.SlidingWindow {
		for _, d := range bucket.SuccessLatency {
			allLatencies = append(allLatencies, float64(d.Milliseconds()))
		}
		for _, d := range bucket.FailureLatency {
			allLatencies = append(allLatencies, float64(d.Milliseconds()))
		}
	}
	
	if len(allLatencies) == 0 {
		return
	}
	
	// Sort latencies for percentile calculation
	sort.Float64s(allLatencies)
	
	// Calculate percentiles
	count := len(allLatencies)
	latencyMetrics := &ecb.enhancedMetrics.LatencyPercentiles
	
	latencyMetrics.MinMs = allLatencies[0]
	latencyMetrics.MaxMs = allLatencies[count-1]
	
	// Average
	var sum float64
	for _, v := range allLatencies {
		sum += v
	}
	latencyMetrics.AvgMs = sum / float64(count)
	
	// Percentiles
	latencyMetrics.P50ms = percentile(allLatencies, 0.5)
	latencyMetrics.P90ms = percentile(allLatencies, 0.9)
	latencyMetrics.P95ms = percentile(allLatencies, 0.95)
	latencyMetrics.P99ms = percentile(allLatencies, 0.99)
}

// percentile calculates the p-th percentile of sorted data
func percentile(sortedData []float64, p float64) float64 {
	if len(sortedData) == 0 {
		return 0
	}
	
	index := p * float64(len(sortedData)-1)
	i := int(index)
	
	if i+1 >= len(sortedData) {
		return sortedData[i]
	}
	
	fraction := index - float64(i)
	return sortedData[i] + fraction*(sortedData[i+1]-sortedData[i])
}

// onStateChange is called when the circuit breaker changes state
func (ecb *EnhancedCircuitBreaker) onStateChange(name string, newState CircuitBreakerState) {
	// Record state change
	now := time.Now()
	
	ecb.stateMutex.Lock()
	oldState := ecb.state
	
	// Avoid recording duplicate state changes
	if oldState == newState {
		ecb.stateMutex.Unlock()
		return
	}
	
	// Calculate time spent in the previous state
	if ecb.enhancedMetrics.LastStateChange.IsZero() {
		ecb.enhancedMetrics.LastStateChange = now
	} else {
		timeInState := now.Sub(ecb.enhancedMetrics.LastStateChange)
		ecb.enhancedMetrics.TimeInStates[oldState] += timeInState
	}
	
	// Update state change metrics
	atomic.AddInt64(&ecb.enhancedMetrics.StateChanges, 1)
	ecb.enhancedMetrics.LastStateChange = now
	
	// Log state change
	stateChange := StateChange{
		FromState: oldState,
		ToState:   newState,
		Timestamp: now,
		Reason:    fmt.Sprintf("Changed from %v to %v", oldState, newState),
		Metrics: map[string]interface{}{
			"consecutiveFailures":  atomic.LoadInt64(&ecb.metrics.ConsecutiveFailures),
			"consecutiveSuccesses": atomic.LoadInt64(&ecb.metrics.ConsecutiveSuccesses),
			"failureRate":          ecb.CalculateFailureRate(),
		},
	}
	
	// Append to log (limited size)
	if len(ecb.enhancedMetrics.StateChangeLog) >= 100 {
		// Remove oldest entry
		ecb.enhancedMetrics.StateChangeLog = ecb.enhancedMetrics.StateChangeLog[1:]
	}
	ecb.enhancedMetrics.StateChangeLog = append(ecb.enhancedMetrics.StateChangeLog, stateChange)
	ecb.stateMutex.Unlock()
	
	// Trigger alerts based on state change
	failureRate := ecb.CalculateFailureRate()
	if newState == CircuitOpen {
		// Circuit opened alert
		ecb.triggerAlert(ErrorLevel, fmt.Sprintf(
			"Circuit '%s' opened with failure rate %.2f%%", 
			name, failureRate*100), nil)
	} else if oldState == CircuitOpen && newState == CircuitHalfOpen {
		// Circuit half-open alert
		ecb.triggerAlert(WarningLevel, fmt.Sprintf(
			"Circuit '%s' entering half-open state after %.2f seconds",
			name, ecb.config.ResetTimeout.Seconds()), nil)
	} else if oldState != CircuitClosed && newState == CircuitClosed {
		// Circuit closed alert
		ecb.triggerAlert(InfoLevel, fmt.Sprintf(
			"Circuit '%s' closed after recovery",
			name), nil)
	}
}

// checkAndTriggerAlerts checks failure rates against thresholds and triggers alerts
func (ecb *EnhancedCircuitBreaker) checkAndTriggerAlerts(failureRate float64, category ErrorCategory, err error) {
	// Check each alert level
	for level, threshold := range ecb.config.AlertThresholds {
		if failureRate >= threshold {
			message := fmt.Sprintf(
				"Circuit '%s' failure rate %.2f%% exceeds %s threshold %.2f%%",
				ecb.name, failureRate*100, alertLevelToString(level), threshold*100)
			
			context := map[string]interface{}{
				"errorCategory": category,
				"errorMessage":  err.Error(),
				"state":         ecb.GetState(),
			}
			
			ecb.triggerAlert(level, message, context)
			break // Only trigger highest severity alert
		}
	}
}

// triggerAlert sends an alert to all registered handlers
func (ecb *EnhancedCircuitBreaker) triggerAlert(level AlertLevel, message string, context map[string]interface{}) {
	alert := Alert{
		Level:       level,
		CircuitName: ecb.name,
		Message:     message,
		Timestamp:   time.Now(),
		Metrics:     ecb.enhancedMetrics,
		Context:     context,
	}
	
	// Update alert metrics
	ecb.enhancedMetrics.AlertMutex.Lock()
	ecb.enhancedMetrics.AlertCounts[level]++
	ecb.enhancedMetrics.LastAlertTime = alert.Timestamp
	atomic.AddInt64(&ecb.enhancedMetrics.AlertsTriggered, 1)
	ecb.enhancedMetrics.AlertMutex.Unlock()
	
	// Send to alert handlers
	for _, handler := range ecb.alertHandlers {
		go handler.HandleAlert(alert)
	}
}

// alertLevelToString converts AlertLevel to a string
func alertLevelToString(level AlertLevel) string {
	switch level {
	case InfoLevel:
		return "INFO"
	case WarningLevel:
		return "WARNING"
	case ErrorLevel:
		return "ERROR"
	case CriticalLevel:
		return "CRITICAL"
	default:
		return "UNKNOWN"
	}
}

// startHealthChecks starts periodic health checks
func (ecb *EnhancedCircuitBreaker) startHealthChecks() {
	healthCtx, cancel := context.WithCancel(context.Background())
	ecb.healthCheckCancelFunc = cancel
	
	ecb.healthCheckTicker = time.NewTicker(ecb.config.HealthCheckInterval)
	
	go func() {
		for {
			select {
			case <-healthCtx.Done():
				if ecb.healthCheckTicker != nil {
					ecb.healthCheckTicker.Stop()
				}
				return
			case <-ecb.healthCheckTicker.C:
				ecb.performHealthCheck(healthCtx)
			}
		}
	}()
}

// performHealthCheck evaluates circuit health and responds accordingly
func (ecb *EnhancedCircuitBreaker) performHealthCheck(ctx context.Context) {
	health := ecb.GetHealthStatus()
	
	// If in half-open state with high failure rate, trip back to open
	if ecb.GetState() == CircuitHalfOpen && health.FailureRate > 0.5 {
		ecb.stateMutex.Lock()
		ecb.tripBreaker()
		ecb.stateMutex.Unlock()
	}
	
	// Check for latency degradation
	if ecb.config.TrackLatencyPercentiles && 
		health.ResponseTimes.P95ms > float64(ecb.config.LatencyThresholdP95.Milliseconds()) {
		
		// Trigger warning for latency degradation
		ecb.triggerAlert(WarningLevel, fmt.Sprintf(
			"Circuit '%s' experiencing latency degradation. P95: %.2fms exceeds threshold %.2fms",
			ecb.name, 
			health.ResponseTimes.P95ms,
			float64(ecb.config.LatencyThresholdP95.Milliseconds())),
			nil)
	}
}

// GetHealthStatus returns detailed health information about the circuit breaker
func (ecb *EnhancedCircuitBreaker) GetHealthStatus() HealthStatus {
	currentState := ecb.GetState()
	isHealthy := currentState == CircuitClosed
	
	// Collect error distribution
	ecb.enhancedMetrics.ErrorMutex.RLock()
	errorDistribution := make(map[ErrorCategory]int64)
	for category, count := range ecb.enhancedMetrics.ErrorCounts {
		errorDistribution[category] = count
	}
	ecb.enhancedMetrics.ErrorMutex.RUnlock()
	
	// Get latency metrics
	ecb.enhancedMetrics.LatencyMutex.RLock()
	latencyMetrics := ecb.enhancedMetrics.LatencyPercentiles
	ecb.enhancedMetrics.LatencyMutex.RUnlock()
	
	// Get load metrics
	ecb.enhancedMetrics.LoadMutex.RLock()
	currentLoad := int(atomic.LoadInt64(&ecb.enhancedMetrics.CurrentLoad))
	ecb.enhancedMetrics.LoadMutex.RUnlock()
	
	return HealthStatus{
		IsHealthy:        isHealthy,
		State:            currentState,
		FailureRate:      ecb.CalculateFailureRate(),
		ErrorDistribution: errorDistribution,
		ResponseTimes:    latencyMetrics,
		LastStateChange:  ecb.enhancedMetrics.LastStateChange,
		CurrentLoad:      currentLoad,
		MaxLoad:          ecb.config.MaxConcurrent,
	}
}

// AddAlertHandler adds a handler for circuit breaker alerts
func (ecb *EnhancedCircuitBreaker) AddAlertHandler(handler AlertHandler) {
	ecb.alertHandlers = append(ecb.alertHandlers, handler)
}

// GetEnhancedMetrics returns detailed metrics for the circuit breaker
func (ecb *EnhancedCircuitBreaker) GetEnhancedMetrics() *EnhancedCircuitBreakerMetrics {
	return ecb.enhancedMetrics
}

// ForceOpen forces the circuit breaker to open regardless of metrics
func (ecb *EnhancedCircuitBreaker) ForceOpen(reason string) {
	ecb.stateMutex.Lock()
	defer ecb.stateMutex.Unlock()
	
	if ecb.state != CircuitOpen {
		// Log the manual state change
		stateChange := StateChange{
			FromState: ecb.state,
			ToState:   CircuitOpen,
			Timestamp: time.Now(),
			Reason:    fmt.Sprintf("Manually forced open: %s", reason),
			Metrics:   map[string]interface{}{},
		}
		
		ecb.enhancedMetrics.StateChangeLog = append(ecb.enhancedMetrics.StateChangeLog, stateChange)
		
		// Trip the breaker
		ecb.tripBreaker()
		
		// Trigger alert
		ecb.triggerAlert(WarningLevel, fmt.Sprintf(
			"Circuit '%s' manually forced open: %s",
			ecb.name, reason), nil)
	}
}

// Stop stops all background processes
func (ecb *EnhancedCircuitBreaker) Stop() {
	// Cancel the context to signal the health check goroutine to exit
	if ecb.healthCheckCancelFunc != nil {
		ecb.healthCheckCancelFunc()
		ecb.healthCheckCancelFunc = nil
	}
	
	// Stop the ticker to prevent resource leaks
	if ecb.healthCheckTicker != nil {
		ecb.healthCheckTicker.Stop()
		ecb.healthCheckTicker = nil
	}
	
	// The base CircuitBreaker doesn't have a Stop method to call
}
