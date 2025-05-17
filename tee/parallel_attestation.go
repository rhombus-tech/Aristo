package tee

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

// ParallelAttestationResult contains the combined results of parallel attestation
type ParallelAttestationResult struct {
	// Combined result
	Valid bool

	// Individual attestation results
	SGXResult *AttestationResult
	SEVResult *AttestationResult

	// Performance metrics
	TotalLatencyMs        float64 // Total end-to-end latency
	LatencySavedMs        float64 // Time saved compared to sequential processing
	StartTime             time.Time
	CompletionTime        time.Time
	
	// Error information
	Error error
}

// ParallelAttestationOptions provides configuration options for parallel attestation
type ParallelAttestationOptions struct {
	// Timeout for the entire attestation process
	Timeout time.Duration
	
	// Whether to fail fast on first attestation failure
	// When true, cancels the other attestation if one fails
	FailFast bool
	
	// Whether to collect detailed performance metrics
	DetailedMetrics bool
	
	// Debug logging
	Debug bool
}

// DefaultParallelAttestationOptions returns the default options
func DefaultParallelAttestationOptions() *ParallelAttestationOptions {
	return &ParallelAttestationOptions{
		Timeout:         200 * time.Millisecond, // Default timeout
		FailFast:        true,                   // Cancel other attestation on failure
		DetailedMetrics: true,                   // Collect detailed metrics
		Debug:           false,                  // No debug logging by default
	}
}

// ParallelAttestationProcessor handles parallel processing of attestations
type ParallelAttestationProcessor struct {
	options  *ParallelAttestationOptions
	metrics  *ParallelAttestationMetrics
	mu       sync.RWMutex
}

// ParallelAttestationMetrics tracks performance metrics for parallel attestations
type ParallelAttestationMetrics struct {
	TotalAttestations       int64
	SuccessfulAttestations  int64
	FailedAttestations      int64
	TotalLatencyMs          float64
	AverageLatencyMs        float64
	MaxLatencyMs            float64
	MinLatencyMs            float64
	TotalTimeSavedMs        float64
	AverageTimeSavedMs      float64
	SGXFailureCount         int64
	SEVFailureCount         int64
	BothFailureCount        int64
	TimeoutCount            int64
	mu                      sync.RWMutex
}

// NewParallelAttestationProcessor creates a new processor with the given options
func NewParallelAttestationProcessor(options *ParallelAttestationOptions) *ParallelAttestationProcessor {
	if options == nil {
		options = DefaultParallelAttestationOptions()
	}
	
	return &ParallelAttestationProcessor{
		options: options,
		metrics: &ParallelAttestationMetrics{
			MinLatencyMs: 9999999, // Initialize to high value
		},
	}
}

// VerifyAttestationsParallel performs SGX and SEV attestations in parallel
// sgxAttestationFunc and sevAttestationFunc are functions that perform the actual attestation
// The function signatures match the attestation functions in the codebase
func (p *ParallelAttestationProcessor) VerifyAttestationsParallel(
	ctx context.Context,
	sgxAttestationFunc func(context.Context, string, []byte) (AttestationResult, error),
	sevAttestationFunc func(context.Context, string, []byte) (AttestationResult, error),
	operation string,
	data []byte,
) (*ParallelAttestationResult, error) {
	startTime := time.Now()
	
	// Create a context with timeout
	attCtx, cancel := context.WithTimeout(ctx, p.options.Timeout)
	defer cancel()
	
	// Create a result
	result := &ParallelAttestationResult{
		StartTime: startTime,
		Valid:     false,
	}
	
	// Create channels for results
	sgxResultCh := make(chan struct {
		result AttestationResult
		err    error
	}, 1)
	
	sevResultCh := make(chan struct {
		result AttestationResult
		err    error
	}, 1)
	
	// Use a WaitGroup to ensure goroutines complete
	var wg sync.WaitGroup
	wg.Add(2)
	
	// Start SGX attestation
	go func() {
		defer wg.Done()
		
		if p.options.Debug {
			log.Printf("Starting SGX attestation for operation: %s", operation)
		}
		
		r, err := sgxAttestationFunc(attCtx, operation, data)
		
		select {
		case sgxResultCh <- struct {
			result AttestationResult
			err    error
		}{r, err}:
			// Successfully sent the result
			if p.options.Debug {
				log.Printf("SGX attestation completed: valid=%v, quorum=%d", r.Valid, r.QuorumSize)
			}
			
			// If fail fast is enabled and attestation failed, cancel context
			if p.options.FailFast && (!r.Valid || err != nil) {
				if p.options.Debug {
					log.Printf("SGX attestation failed, canceling context")
				}
				cancel()
			}
		case <-attCtx.Done():
			// Context canceled or timed out
			if p.options.Debug {
				log.Printf("SGX attestation context canceled or timed out")
			}
		}
	}()
	
	// Start SEV attestation
	go func() {
		defer wg.Done()
		
		if p.options.Debug {
			log.Printf("Starting SEV attestation for operation: %s", operation)
		}
		
		r, err := sevAttestationFunc(attCtx, operation, data)
		
		select {
		case sevResultCh <- struct {
			result AttestationResult
			err    error
		}{r, err}:
			// Successfully sent the result
			if p.options.Debug {
				log.Printf("SEV attestation completed: valid=%v, quorum=%d", r.Valid, r.QuorumSize)
			}
			
			// If fail fast is enabled and attestation failed, cancel context
			if p.options.FailFast && (!r.Valid || err != nil) {
				if p.options.Debug {
					log.Printf("SEV attestation failed, canceling context")
				}
				cancel()
			}
		case <-attCtx.Done():
			// Context canceled or timed out
			if p.options.Debug {
				log.Printf("SEV attestation context canceled or timed out")
			}
		}
	}()
	
	// Wait for results or timeout
	var sgxResponse, sevResponse struct {
		result AttestationResult
		err    error
	}
	
	var sgxDone, sevDone bool
	
	// Wait for both attestations to complete or timeout
	for !sgxDone || !sevDone {
		select {
		case sgxResponse = <-sgxResultCh:
			sgxDone = true
			if p.options.Debug {
				log.Printf("Received SGX attestation result")
			}
			
		case sevResponse = <-sevResultCh:
			sevDone = true
			if p.options.Debug {
				log.Printf("Received SEV attestation result")
			}
			
		case <-attCtx.Done():
			// Handle timeout
			completionTime := time.Now()
			result.CompletionTime = completionTime
			result.TotalLatencyMs = float64(completionTime.Sub(startTime).Milliseconds())
			result.Error = fmt.Errorf("attestation timeout after %.2fms", result.TotalLatencyMs)
			
			// Update metrics
			p.updateMetricsForTimeout(result)
			
			if p.options.Debug {
				log.Printf("Parallel attestation timed out after %.2fms", result.TotalLatencyMs)
			}
			
			// Wait for goroutines to finish
			go func() {
				wg.Wait()
			}()
			
			return result, result.Error
		}
	}
	
	// Both attestations completed, process results
	completionTime := time.Now()
	result.CompletionTime = completionTime
	result.TotalLatencyMs = float64(completionTime.Sub(startTime).Milliseconds())
	
	// Set individual results
	if sgxDone {
		sgxResult := sgxResponse.result
		result.SGXResult = &sgxResult
	}
	
	if sevDone {
		sevResult := sevResponse.result
		result.SEVResult = &sevResult
	}
	
	// Calculate time saved compared to sequential processing
	// This is an approximation: sum of individual latencies minus parallel latency
	if sgxDone && sevDone {
		sequentialLatency := result.SGXResult.LatencyMs + result.SEVResult.LatencyMs
		result.LatencySavedMs = sequentialLatency - result.TotalLatencyMs
		
		// Ensure we don't report negative time saved due to overhead
		if result.LatencySavedMs < 0 {
			result.LatencySavedMs = 0
		}
	}
	
	// Determine if attestation is valid (both SGX and SEV must be valid)
	valid := true
	var err error
	
	if sgxDone && sevDone {
		// Both attestations completed
		if sgxResponse.err != nil || sevResponse.err != nil {
			// At least one attestation had an error
			err = errors.New("attestation error")
			valid = false
			
			if sgxResponse.err != nil {
				err = fmt.Errorf("SGX attestation error: %w", sgxResponse.err)
			}
			
			if sevResponse.err != nil {
				if err != nil {
					err = fmt.Errorf("%v; SEV attestation error: %w", err, sevResponse.err)
				} else {
					err = fmt.Errorf("SEV attestation error: %w", sevResponse.err)
				}
			}
		} else if !sgxResponse.result.Valid || !sevResponse.result.Valid {
			// At least one attestation was invalid
			valid = false
			err = fmt.Errorf("invalid attestation: SGX=%v, SEV=%v", 
				sgxResponse.result.Valid, sevResponse.result.Valid)
		}
	} else {
		// Shouldn't happen with our implementation, but just in case
		valid = false
		err = errors.New("incomplete attestation")
	}
	
	result.Valid = valid
	result.Error = err
	
	// Update metrics
	p.updateMetrics(result)
	
	if p.options.Debug {
		log.Printf("Parallel attestation completed in %.2fms (saved %.2fms): valid=%v", 
			result.TotalLatencyMs, result.LatencySavedMs, result.Valid)
	}
	
	return result, err
}

// updateMetrics updates the performance metrics for a completed attestation
func (p *ParallelAttestationProcessor) updateMetrics(result *ParallelAttestationResult) {
	p.metrics.mu.Lock()
	defer p.metrics.mu.Unlock()
	
	p.metrics.TotalAttestations++
	
	if result.Valid {
		p.metrics.SuccessfulAttestations++
	} else {
		p.metrics.FailedAttestations++
		
		// Determine failure type
		if result.SGXResult != nil && !result.SGXResult.Valid && result.SEVResult != nil && !result.SEVResult.Valid {
			p.metrics.BothFailureCount++
		} else if result.SGXResult != nil && !result.SGXResult.Valid {
			p.metrics.SGXFailureCount++
		} else if result.SEVResult != nil && !result.SEVResult.Valid {
			p.metrics.SEVFailureCount++
		}
	}
	
	// Update latency metrics
	p.metrics.TotalLatencyMs += result.TotalLatencyMs
	p.metrics.AverageLatencyMs = p.metrics.TotalLatencyMs / float64(p.metrics.TotalAttestations)
	
	if result.TotalLatencyMs > p.metrics.MaxLatencyMs {
		p.metrics.MaxLatencyMs = result.TotalLatencyMs
	}
	
	if result.TotalLatencyMs < p.metrics.MinLatencyMs {
		p.metrics.MinLatencyMs = result.TotalLatencyMs
	}
	
	// Update time saved metrics
	p.metrics.TotalTimeSavedMs += result.LatencySavedMs
	p.metrics.AverageTimeSavedMs = p.metrics.TotalTimeSavedMs / float64(p.metrics.TotalAttestations)
}

// updateMetricsForTimeout updates metrics when an attestation times out
func (p *ParallelAttestationProcessor) updateMetricsForTimeout(result *ParallelAttestationResult) {
	p.metrics.mu.Lock()
	defer p.metrics.mu.Unlock()
	
	p.metrics.TotalAttestations++
	p.metrics.FailedAttestations++
	p.metrics.TimeoutCount++
	
	// Update latency metrics
	p.metrics.TotalLatencyMs += result.TotalLatencyMs
	p.metrics.AverageLatencyMs = p.metrics.TotalLatencyMs / float64(p.metrics.TotalAttestations)
	
	if result.TotalLatencyMs > p.metrics.MaxLatencyMs {
		p.metrics.MaxLatencyMs = result.TotalLatencyMs
	}
}

// GetMetrics returns a copy of the current metrics
func (p *ParallelAttestationProcessor) GetMetrics() ParallelAttestationMetrics {
	p.metrics.mu.RLock()
	defer p.metrics.mu.RUnlock()
	
	// Return a copy of the metrics
	return *p.metrics
}

// ResetMetrics resets all metrics to zero
func (p *ParallelAttestationProcessor) ResetMetrics() {
	p.metrics.mu.Lock()
	defer p.metrics.mu.Unlock()
	
	p.metrics.TotalAttestations = 0
	p.metrics.SuccessfulAttestations = 0
	p.metrics.FailedAttestations = 0
	p.metrics.TotalLatencyMs = 0
	p.metrics.AverageLatencyMs = 0
	p.metrics.MaxLatencyMs = 0
	p.metrics.MinLatencyMs = 9999999
	p.metrics.TotalTimeSavedMs = 0
	p.metrics.AverageTimeSavedMs = 0
	p.metrics.SGXFailureCount = 0
	p.metrics.SEVFailureCount = 0
	p.metrics.BothFailureCount = 0
	p.metrics.TimeoutCount = 0
}
