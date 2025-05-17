package mesh

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

// FailureDetectionStrategy defines how failures are detected and weighted
type FailureDetectionStrategy int

const (
	// SimpleCountStrategy counts raw failures without weighting
	SimpleCountStrategy FailureDetectionStrategy = iota
	
	// TimeWeightedStrategy weights recent failures more heavily
	TimeWeightedStrategy
	
	// ErrorCategoryStrategy differentiates between error types
	ErrorCategoryStrategy
	
	// AdaptiveStrategy adjusts thresholds based on traffic patterns
	AdaptiveStrategy
)

// ErrorCategory classifies errors for different handling
type ErrorCategory int

const (
	// UnknownError is the default category
	UnknownError ErrorCategory = iota
	
	// TimeoutError indicates a request timed out
	TimeoutError
	
	// ConnectionError indicates a network/connection issue
	ConnectionError
	
	// ResourceError indicates resource exhaustion
	ResourceError
	
	// AuthError indicates authentication/authorization issues
	AuthError
	
	// StateError indicates invalid state transitions
	StateError
)

// HealthStatus represents detailed health information
type HealthStatus struct {
	IsHealthy        bool
	State            CircuitBreakerState
	FailureRate      float64
	ErrorDistribution map[ErrorCategory]int64
	ResponseTimes    LatencyMetrics
	LastStateChange  time.Time
	CurrentLoad      int // Current concurrent requests
	MaxLoad          int // Maximum concurrent requests allowed
}

// LatencyMetrics contains detailed latency statistics
type LatencyMetrics struct {
	P50ms float64 // 50th percentile in ms
	P90ms float64 // 90th percentile in ms
	P95ms float64 // 95th percentile in ms
	P99ms float64 // 99th percentile in ms
	MaxMs float64 // Maximum observed latency in ms
	MinMs float64 // Minimum observed latency in ms
	AvgMs float64 // Average latency in ms
}

// AlertLevel defines the severity of circuit breaker alerts
type AlertLevel int

const (
	// InfoLevel is for informational alerts
	InfoLevel AlertLevel = iota
	
	// WarningLevel indicates potential issues
	WarningLevel
	
	// ErrorLevel indicates serious issues
	ErrorLevel
	
	// CriticalLevel indicates critical failures
	CriticalLevel
)

// Alert represents a circuit breaker alert
type Alert struct {
	Level       AlertLevel
	CircuitName string
	Message     string
	Timestamp   time.Time
	Metrics     *EnhancedCircuitBreakerMetrics
	Context     map[string]interface{}
}

// AlertHandler defines the interface for handling circuit breaker alerts
type AlertHandler interface {
	HandleAlert(alert Alert)
}

// EnhancedCircuitBreakerConfig extends the basic configuration
type EnhancedCircuitBreakerConfig struct {
	*CircuitBreakerConfig
	
	// Advanced failure detection
	FailureDetectionStrategy FailureDetectionStrategy
	ErrorCategoryWeights     map[ErrorCategory]float64
	TimeWindowSeconds        int64  // Time window for failure rate calculation
	SlidingWindowBuckets     int    // Number of buckets in sliding window
	
	// Adaptive thresholds
	EnableAdaptiveThresholds bool
	MinFailureThreshold      int64
	MaxFailureThreshold      int64
	MinResetTimeout          time.Duration
	MaxResetTimeout          time.Duration
	
	// Health check
	HealthCheckInterval      time.Duration
	HealthCheckTimeout       time.Duration
	
	// Latency metrics
	TrackLatencyPercentiles  bool
	LatencyBuckets           int
	LatencyThresholdP95      time.Duration
	LatencyThresholdP99      time.Duration
	
	// Concurrency control
	GradualRecovery          bool
	RecoveryStepPercent      int
	
	// Alerting
	AlertThresholds          map[AlertLevel]float64
	AlertHandlers            []AlertHandler
}

// DefaultEnhancedCircuitBreakerConfig returns default enhanced config
func DefaultEnhancedCircuitBreakerConfig() *EnhancedCircuitBreakerConfig {
	baseConfig := DefaultCircuitBreakerConfig()
	
	return &EnhancedCircuitBreakerConfig{
		CircuitBreakerConfig:    baseConfig,
		FailureDetectionStrategy: TimeWeightedStrategy,
		ErrorCategoryWeights: map[ErrorCategory]float64{
			UnknownError:    1.0,
			TimeoutError:    1.5,
			ConnectionError: 2.0,
			ResourceError:   1.0,
			AuthError:       0.5,
			StateError:      1.0,
		},
		TimeWindowSeconds:       60,
		SlidingWindowBuckets:    10,
		EnableAdaptiveThresholds: true,
		MinFailureThreshold:     3,
		MaxFailureThreshold:     50,
		MinResetTimeout:         time.Second * 5,
		MaxResetTimeout:         time.Minute * 5,
		HealthCheckInterval:     time.Second * 5,
		HealthCheckTimeout:      time.Second * 2,
		TrackLatencyPercentiles: true,
		LatencyBuckets:          50,
		LatencyThresholdP95:     time.Millisecond * 200,
		LatencyThresholdP99:     time.Millisecond * 500,
		GradualRecovery:         true,
		RecoveryStepPercent:     10,
		AlertThresholds: map[AlertLevel]float64{
			InfoLevel:      0.1,  // 10% failure rate
			WarningLevel:   0.25, // 25% failure rate
			ErrorLevel:     0.5,  // 50% failure rate 
			CriticalLevel:  0.75, // 75% failure rate
		},
		AlertHandlers:           []AlertHandler{},
	}
}

// ErrorCategoryMapper is a function that maps errors to categories
type ErrorCategoryMapper func(error) ErrorCategory

// DefaultErrorCategoryMapper provides default error categorization
func DefaultErrorCategoryMapper(err error) ErrorCategory {
	if err == nil {
		return UnknownError
	}
	
	if errors.Is(err, context.DeadlineExceeded) {
		return TimeoutError
	}
	
	errStr := err.Error()
	
	// Check for connection errors
	if containsAny(errStr, []string{
		"connection", "connect", "dial", "handshake", "reset", "closed",
		"EOF", "broken pipe", "refused", "network", "unreachable",
	}) {
		return ConnectionError
	}
	
	// Check for resource errors
	if containsAny(errStr, []string{
		"limit", "capacity", "exhausted", "overload", "memory", "cpu", "timeout",
		"queue", "backlog", "throttl", "overcommit", "congestion",
	}) {
		return ResourceError
	}
	
	// Check for auth errors
	if containsAny(errStr, []string{
		"auth", "unauthoriz", "permission", "access", "forbidden", "deny",
		"token", "credential", "certificate",
	}) {
		return AuthError
	}
	
	// Check for state errors
	if containsAny(errStr, []string{
		"state", "transition", "invalid", "illegal", "corrupt", "condition",
		"invariant", "incompatible", "inconsistent",
	}) {
		return StateError
	}
	
	// Default to unknown
	return UnknownError
}

// TimeBucket represents a time bucket for the sliding window
type TimeBucket struct {
	StartTime      time.Time
	EndTime        time.Time
	Requests       int64
	Failures       int64
	SuccessLatency []time.Duration
	FailureLatency []time.Duration
	ErrorCounts    map[ErrorCategory]int64
}

// EnhancedCircuitBreakerMetrics provides detailed metrics
type EnhancedCircuitBreakerMetrics struct {
	CircuitBreakerMetrics
	
	// Sliding window of time buckets
	SlidingWindow      []*TimeBucket
	WindowMutex        sync.RWMutex
	BucketSizeSeconds  int64
	
	// Error categorization
	ErrorCounts        map[ErrorCategory]int64
	ErrorMutex         sync.RWMutex
	
	// Latency statistics
	LatencyPercentiles LatencyMetrics
	LatencyMutex       sync.RWMutex
	
	// Circuit state changes
	StateChanges       int64
	LastStateChange    time.Time
	TimeInStates       map[CircuitBreakerState]time.Duration
	StateChangeLog     []StateChange
	
	// Load tracking
	CurrentLoad        int64
	PeakLoad           int64
	LoadMutex          sync.RWMutex
	
	// Alerting
	AlertsTriggered    int64
	LastAlertTime      time.Time
	AlertCounts        map[AlertLevel]int64
	AlertMutex         sync.RWMutex
}

// StateChange records a change in circuit breaker state
type StateChange struct {
	FromState  CircuitBreakerState
	ToState    CircuitBreakerState
	Timestamp  time.Time
	Reason     string
	Metrics    map[string]interface{}
}

// containsAny checks if a string contains any of the substrings
func containsAny(s string, substrs []string) bool {
	for _, substr := range substrs {
		if contains(s, substr) {
			return true
		}
	}
	return false
}

// contains checks if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
