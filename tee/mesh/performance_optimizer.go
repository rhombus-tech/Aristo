// Package mesh provides a mesh network for TEE-to-TEE communication
package mesh

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/klauspost/compress/zstd"
)

// Compression algorithm constants
const (
	CompressionZstd string = "zstd"
	CompressionLz4 string = "lz4"   // For future implementation
	CompressionBrotli string = "brotli" // For future implementation
)

// CircuitBreakerState represents the current state of a circuit breaker
type CircuitBreakerState int

const (
	// CircuitClosed represents normal operation (allowing operations)
	CircuitClosed CircuitBreakerState = iota
	// CircuitHalfOpen represents testing if operations can resume
	CircuitHalfOpen
	// CircuitOpen represents a tripped circuit (blocking operations)
	CircuitOpen
)

// ErrCircuitBreakerOpen indicates the circuit breaker is open and operations are being blocked
var ErrCircuitBreakerOpen = errors.New("circuit breaker is open")

// CircuitBreakerMetrics tracks performance metrics for the circuit breaker
type CircuitBreakerMetrics struct {
	TotalAttempts        int64
	SuccessfulAttempts   int64
	FailedAttempts       int64
	ConsecutiveFailures  int64
	ConsecutiveSuccesses int64
	LastFailure          time.Time
	LastSuccess          time.Time
	OpenCount            int64
}

// CircuitBreakerConfig holds configuration options for the circuit breaker
type CircuitBreakerConfig struct {
	FailureThreshold    int64         // Number of consecutive failures to trip circuit
	ResetTimeout        time.Duration // Time to wait before trying again
	SuccessThreshold    int64         // Number of consecutive successes to close circuit
	Timeout             time.Duration // Timeout for operations
	MaxConcurrent       int           // Maximum concurrent operations
	MaxLatencyThreshold time.Duration // Maximum latency before considering an operation failed
}

// DefaultCircuitBreakerConfig returns default config for the circuit breaker
func DefaultCircuitBreakerConfig() *CircuitBreakerConfig {
	return &CircuitBreakerConfig{
		FailureThreshold:    5,
		ResetTimeout:        30 * time.Second,
		SuccessThreshold:    2,
		Timeout:             10 * time.Second,
		MaxConcurrent:       100,
		MaxLatencyThreshold: 5 * time.Second,
	}
}

// CircuitBreaker implements the circuit breaker pattern to prevent cascade failures
type CircuitBreaker struct {
	name            string
	config          *CircuitBreakerConfig
	state           CircuitBreakerState
	metrics         CircuitBreakerMetrics
	stateMutex      sync.RWMutex
	tripTime        time.Time
	semaphore       chan struct{} // Semaphore for limiting concurrency
	callbacks       []func(string, CircuitBreakerState)
}

// NewCircuitBreaker creates a new circuit breaker with the specified configuration
func NewCircuitBreaker(name string, config *CircuitBreakerConfig) *CircuitBreaker {
	if config == nil {
		config = DefaultCircuitBreakerConfig()
	}
	
	return &CircuitBreaker{
		name:       name,
		config:     config,
		state:      CircuitClosed,
		semaphore:  make(chan struct{}, config.MaxConcurrent),
		callbacks:  make([]func(string, CircuitBreakerState), 0),
	}
}

// Execute runs the provided function with circuit breaker protection
func (cb *CircuitBreaker) Execute(ctx context.Context, operation func() error) error {
	// Check if breaker is open
	if !cb.allowRequest() {
		atomic.AddInt64(&cb.metrics.TotalAttempts, 1)
		return ErrCircuitBreakerOpen
	}
	
	// Acquire semaphore to enforce concurrency limits
	select {
	case cb.semaphore <- struct{}{}:
		// Successfully acquired semaphore
		defer func() { <-cb.semaphore }()
	case <-ctx.Done():
		// Context canceled while waiting for semaphore
		return ctx.Err()
	default:
		// No semaphore available
		return fmt.Errorf("too many concurrent operations (%d max)", cb.config.MaxConcurrent)
	}
	
	// Increment total attempts
	atomic.AddInt64(&cb.metrics.TotalAttempts, 1)
	
	// Execute with timeout
	var operationErr error
	
	// Create timeout context for the operation
	opCtx, cancel := context.WithTimeout(ctx, cb.config.Timeout)
	defer cancel()
	
	// Track operation timing
	startTime := time.Now()
	
	// Channel for operation completion
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
		operationErr = fmt.Errorf("operation timed out after %v: %w", cb.config.Timeout, opCtx.Err())
	}
	
	// Check if operation exceeded the latency threshold
	elapsed := time.Since(startTime)
	if elapsed > cb.config.MaxLatencyThreshold {
		// Auto-convert slow operations to failures
		if operationErr == nil {
			operationErr = fmt.Errorf("operation took too long: %v > %v", elapsed, cb.config.MaxLatencyThreshold)
		}
	}
	
	// Record success or failure
	if operationErr != nil {
		cb.recordFailure()
		return operationErr
	}
	
	cb.recordSuccess()
	return nil
}

// allowRequest determines if the current request should be allowed based on circuit state
func (cb *CircuitBreaker) allowRequest() bool {
	cb.stateMutex.RLock()
	defer cb.stateMutex.RUnlock()
	
	switch cb.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		// Check if reset timeout has elapsed to transition to half-open
		if time.Since(cb.tripTime) > cb.config.ResetTimeout {
			// Don't update state here, just allow a test request through
			// The state will be updated by recordSuccess/recordFailure
			return true
		}
		return false
	case CircuitHalfOpen:
		// In half-open state, we allow limited traffic
		// We're testing if the system has recovered
		atomicCons := atomic.LoadInt64(&cb.metrics.ConsecutiveSuccesses)
		return atomicCons < cb.config.SuccessThreshold
	default:
		return true
	}
}

// recordSuccess records a successful operation
func (cb *CircuitBreaker) recordSuccess() {
	cb.stateMutex.Lock()
	defer cb.stateMutex.Unlock()
	
	atomic.AddInt64(&cb.metrics.SuccessfulAttempts, 1)
	atomic.StoreInt64(&cb.metrics.ConsecutiveFailures, 0)
	atomic.AddInt64(&cb.metrics.ConsecutiveSuccesses, 1)
	cb.metrics.LastSuccess = time.Now()
	
	// If we're half-open and have reached the success threshold, close the circuit
	if cb.state == CircuitHalfOpen {
		if atomic.LoadInt64(&cb.metrics.ConsecutiveSuccesses) >= cb.config.SuccessThreshold {
			cb.setState(CircuitClosed)
		}
	}
}

// recordFailure records a failed operation
func (cb *CircuitBreaker) recordFailure() {
	cb.stateMutex.Lock()
	defer cb.stateMutex.Unlock()
	
	atomic.AddInt64(&cb.metrics.FailedAttempts, 1)
	atomic.StoreInt64(&cb.metrics.ConsecutiveSuccesses, 0)
	atomic.AddInt64(&cb.metrics.ConsecutiveFailures, 1)
	cb.metrics.LastFailure = time.Now()
	
	// Trip the circuit if we've reached the failure threshold
	if cb.state == CircuitClosed {
		if atomic.LoadInt64(&cb.metrics.ConsecutiveFailures) >= cb.config.FailureThreshold {
			cb.tripBreaker()
		}
	} else if cb.state == CircuitHalfOpen {
		// If we fail during half-open, immediately trip the circuit
		cb.tripBreaker()
	}
}

// tripBreaker transitions the circuit to the open state
func (cb *CircuitBreaker) tripBreaker() {
	cb.setState(CircuitOpen)
	cb.tripTime = time.Now()
	atomic.AddInt64(&cb.metrics.OpenCount, 1)
}

// setState changes the circuit breaker state and notifies callbacks
func (cb *CircuitBreaker) setState(state CircuitBreakerState) {
	// Don't fire callbacks if state isn't changing
	if cb.state != state {
		cb.state = state
		// Notify all callbacks
		for _, callback := range cb.callbacks {
			go callback(cb.name, state)
		}
	}
}

// GetState returns the current state of the circuit breaker
func (cb *CircuitBreaker) GetState() CircuitBreakerState {
	cb.stateMutex.RLock()
	defer cb.stateMutex.RUnlock()
	return cb.state
}

// GetMetrics returns the current metrics for the circuit breaker
func (cb *CircuitBreaker) GetMetrics() CircuitBreakerMetrics {
	return cb.metrics
}

// Reset resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.stateMutex.Lock()
	defer cb.stateMutex.Unlock()
	
	cb.state = CircuitClosed
	atomic.StoreInt64(&cb.metrics.ConsecutiveFailures, 0)
	atomic.StoreInt64(&cb.metrics.ConsecutiveSuccesses, 0)
	
	// Notify callbacks
	for _, callback := range cb.callbacks {
		go callback(cb.name, CircuitClosed)
	}
}

// AddStateChangeCallback adds a callback to be notified of state changes
func (cb *CircuitBreaker) AddStateChangeCallback(callback func(string, CircuitBreakerState)) {
	cb.stateMutex.Lock()
	defer cb.stateMutex.Unlock()
	cb.callbacks = append(cb.callbacks, callback)
}

// Compressor provides efficient data compression capabilities
type Compressor struct {
	algorithm       string
	level           int
	zstdEncoder     *zstd.Encoder
	zstdEncoderOnce sync.Once
	stats           struct {
		totalBytesIn       int64
		totalBytesOut      int64
		compressionRatio   float64
		totalOperations    int64
		averageTimeNs      int64
		lastCompressionNs  int64
	}
}

// NewCompressor creates a new compressor with the specified algorithm and level
func NewCompressor(algorithm string, level int) (*Compressor, error) {
	if level < 0 || level > 9 {
		return nil, fmt.Errorf("compression level must be between 0 and 9")
	}
	
	switch algorithm {
	case CompressionNone, CompressionGzip, CompressionZstd:
		// Supported algorithms
	default:
		return nil, fmt.Errorf("unsupported compression algorithm: %s", algorithm)
	}
	
	return &Compressor{
		algorithm: algorithm,
		level:     level,
	}, nil
}

// Compress compresses data using the configured algorithm
func (c *Compressor) Compress(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return data, nil
	}
	
	// Track metrics
	startTime := time.Now()
	defer func() {
		// Update statistics
		compressionTime := time.Since(startTime).Nanoseconds()
		atomic.StoreInt64(&c.stats.lastCompressionNs, compressionTime)
		
		// Update average time using exponential moving average
		total := atomic.LoadInt64(&c.stats.totalOperations)
		if total == 0 {
			atomic.StoreInt64(&c.stats.averageTimeNs, compressionTime)
		} else {
			avg := atomic.LoadInt64(&c.stats.averageTimeNs)
			// Weight recent operations more heavily
			newAvg := int64(float64(avg)*0.7 + float64(compressionTime)*0.3)
			atomic.StoreInt64(&c.stats.averageTimeNs, newAvg)
		}
		
		atomic.AddInt64(&c.stats.totalOperations, 1)
	}()
	
	switch c.algorithm {
	case CompressionNone:
		// No compression
		return data, nil
		
	case CompressionGzip:
		return c.compressGzip(data)
		
	case CompressionZstd:
		return c.compressZstd(data)
		
	default:
		return nil, fmt.Errorf("unsupported compression algorithm: %s", c.algorithm)
	}
}

// compressGzip compresses data using gzip
func (c *Compressor) compressGzip(data []byte) ([]byte, error) {
	var b []byte
	buf := new(bytes.Buffer)
	w, err := gzip.NewWriterLevel(buf, c.level)
	if err != nil {
		return nil, err
	}
	
	_, err = w.Write(data)
	if err != nil {
		return nil, err
	}
	
	err = w.Close()
	if err != nil {
		return nil, err
	}
	
	b = buf.Bytes()
	
	// Update metrics
	atomic.AddInt64(&c.stats.totalBytesIn, int64(len(data)))
	atomic.AddInt64(&c.stats.totalBytesOut, int64(len(b)))
	
	// Update compression ratio
	if len(data) > 0 {
		ratio := float64(len(b)) / float64(len(data))
		// Use exponential moving average for ratio
		oldRatio := atomic.LoadInt64((*int64)(unsafe.Pointer(&c.stats.compressionRatio)))
		newRatio := int64(float64(oldRatio)*0.7 + ratio*0.3*math.Pow10(9))
		atomic.StoreInt64((*int64)(unsafe.Pointer(&c.stats.compressionRatio)), newRatio)
	}
	
	return b, nil
}

// compressZstd compresses data using zstd
func (c *Compressor) compressZstd(data []byte) ([]byte, error) {
	// Initialize the encoder once and re-use
	c.zstdEncoderOnce.Do(func() {
		// Set up encoder with the desired compression level
		// Low = 1, Default = 3, Better = 5, Best = 9
		encoderLevel := zstd.EncoderLevelFromZstd(c.level)
		encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(encoderLevel))
		if err == nil {
			c.zstdEncoder = encoder
		}
	})
	
	if c.zstdEncoder == nil {
		return nil, fmt.Errorf("failed to initialize zstd encoder")
	}
	
	b := c.zstdEncoder.EncodeAll(data, nil)
	
	// Update metrics
	atomic.AddInt64(&c.stats.totalBytesIn, int64(len(data)))
	atomic.AddInt64(&c.stats.totalBytesOut, int64(len(b)))
	
	// Update compression ratio
	if len(data) > 0 {
		ratio := float64(len(b)) / float64(len(data))
		// Use exponential moving average for ratio
		oldRatio := atomic.LoadInt64((*int64)(unsafe.Pointer(&c.stats.compressionRatio)))
		newRatio := int64(float64(oldRatio)*0.7 + ratio*0.3*math.Pow10(9))
		atomic.StoreInt64((*int64)(unsafe.Pointer(&c.stats.compressionRatio)), newRatio)
	}
	
	return b, nil
}

// Decompress decompresses data using the configured algorithm
func (c *Compressor) Decompress(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return data, nil
	}
	
	switch c.algorithm {
	case CompressionNone:
		// No compression
		return data, nil
		
	case CompressionGzip:
		return c.decompressGzip(data)
		
	case CompressionZstd:
		return c.decompressZstd(data)
		
	default:
		return nil, fmt.Errorf("unsupported compression algorithm: %s", c.algorithm)
	}
}

// decompressGzip decompresses gzip data
func (c *Compressor) decompressGzip(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	
	return io.ReadAll(r)
}

// decompressZstd decompresses zstd data
func (c *Compressor) decompressZstd(data []byte) ([]byte, error) {
	// Create a decoder
	decoder, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer decoder.Close()
	
	// Decode the data
	return io.ReadAll(decoder)
}

// GetCompressionStats returns statistics about compression operations
func (c *Compressor) GetCompressionStats() map[string]interface{} {
	return map[string]interface{}{
		"algorithm":        c.algorithm,
		"level":            c.level,
		"totalBytesIn":     atomic.LoadInt64(&c.stats.totalBytesIn),
		"totalBytesOut":    atomic.LoadInt64(&c.stats.totalBytesOut),
		"compressionRatio": float64(atomic.LoadInt64((*int64)(unsafe.Pointer(&c.stats.compressionRatio)))) / math.Pow10(9),
		"totalOperations":  atomic.LoadInt64(&c.stats.totalOperations),
		"averageTimeMs":    float64(atomic.LoadInt64(&c.stats.averageTimeNs)) / 1e6,
		"lastTimeMs":       float64(atomic.LoadInt64(&c.stats.lastCompressionNs)) / 1e6,
	}
}
