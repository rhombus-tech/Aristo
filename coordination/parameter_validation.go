// coordination/parameter_validation.go
package coordination

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	// MaxParameterSize sets maximum size limit for parameters (1KB)
	MaxParameterSize = 1024

	// Default parameter sizes
	DefaultContractIDSize = 32

	// Parameter format types
	FormatLengthPrefixed = "length_prefixed"
	FormatDirect         = "direct"
)

var (
	ErrParameterTooLarge        = errors.New("parameter exceeds maximum size limit")
	ErrInvalidLengthPrefix      = errors.New("invalid length prefix")
	ErrParameterValidationFailed = errors.New("parameter validation failed in both formats")
	ErrCrossValidationMismatch  = errors.New("cross-validation results do not match")
)

// DualFormatParameter represents a parameter that supports both formats
type DualFormatParameter struct {
	Data         []byte
	Format       string
	ValidatedLen int
}

// ParameterBatch represents a batch of parameters for efficient processing
type ParameterBatch struct {
	Parameters []*DualFormatParameter
	BatchID    string
	Timestamp  time.Time
}

// ValidationResult represents the result of parameter validation
type ValidationResult struct {
	Success        bool
	Format         string
	Data           []byte
	ExecutionTime  time.Duration
	Error          error
	CrossValidated bool
}

// ParameterValidator adds dual-format parameter validation to the coordinator
type ParameterValidator struct {
	// Configuration
	maxBatchSize       int
	batchInterval      time.Duration
	enableCrossValidation bool
	
	// Batch processing
	currentBatch       *ParameterBatch
	batchMu            sync.Mutex
	batchProcessChan   chan *ParameterBatch
	resultChan         chan *ValidationResult
	
	// Validation stats
	stats              *ValidationStats
	coordinator        *Coordinator
}

// ValidationStats tracks performance metrics for parameter validation
type ValidationStats struct {
	TotalProcessed        uint64
	SuccessCount          uint64
	FailureCount          uint64
	LengthPrefixedCount   uint64
	DirectFormatCount     uint64
	CrossValidatedCount   uint64
	AvgProcessingTimeNs   uint64
	BatchesProcessed      uint64
	mu                    sync.RWMutex
}

// NewParameterValidator creates a new parameter validator
func NewParameterValidator(c *Coordinator, config *ParameterValidatorConfig) *ParameterValidator {
	if config == nil {
		config = &ParameterValidatorConfig{
			MaxBatchSize:          100,
			BatchInterval:         50 * time.Millisecond,
			EnableCrossValidation: true,
		}
	}
	
	validator := &ParameterValidator{
		maxBatchSize:         config.MaxBatchSize,
		batchInterval:        config.BatchInterval,
		enableCrossValidation: config.EnableCrossValidation,
		currentBatch:         &ParameterBatch{
			Parameters: make([]*DualFormatParameter, 0, config.MaxBatchSize),
			BatchID:    generateBatchID(),
			Timestamp:  time.Now(),
		},
		batchProcessChan:    make(chan *ParameterBatch, 10),
		resultChan:          make(chan *ValidationResult, 1000),
		stats:               &ValidationStats{},
		coordinator:         c,
	}
	
	// Start batch processor
	go validator.batchProcessorLoop()
	go validator.resultCollectorLoop()
	go validator.batchScheduler()
	
	return validator
}

// ParameterValidatorConfig configures the parameter validator
type ParameterValidatorConfig struct {
	MaxBatchSize          int
	BatchInterval         time.Duration
	EnableCrossValidation bool
}

// ValidateParameter validates a parameter in either format
func (pv *ParameterValidator) ValidateParameter(ctx context.Context, data []byte) (*ValidationResult, error) {
	// Quick size check before any processing
	if len(data) > MaxParameterSize {
		return &ValidationResult{
			Success: false,
			Error:   ErrParameterTooLarge,
		}, ErrParameterTooLarge
	}
	
	// Try to validate synchronously for small batches or when batching is disabled
	if pv.maxBatchSize <= 1 || len(data) < 32 {
		return pv.validateParameterImmediate(ctx, data)
	}
	
	// Use batch processing for larger parameters
	resultChan := make(chan *ValidationResult, 1)
	param := &DualFormatParameter{
		Data: data,
	}
	
	pv.batchMu.Lock()
	pv.currentBatch.Parameters = append(pv.currentBatch.Parameters, param)
	
	// If batch is full, process it immediately
	if len(pv.currentBatch.Parameters) >= pv.maxBatchSize {
		batch := pv.currentBatch
		pv.currentBatch = &ParameterBatch{
			Parameters: make([]*DualFormatParameter, 0, pv.maxBatchSize),
			BatchID:    generateBatchID(),
			Timestamp:  time.Now(),
		}
		pv.batchMu.Unlock()
		
		select {
		case pv.batchProcessChan <- batch:
			// Batch submitted successfully
		default:
			// Channel full, process immediately
			go pv.processBatch(batch)
		}
	} else {
		pv.batchMu.Unlock()
	}
	
	// Wait for result with timeout
	select {
	case result := <-resultChan:
		return result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Second):
		// Timeout, validate immediately as fallback
		return pv.validateParameterImmediate(ctx, data)
	}
}

// validateParameterImmediate performs immediate validation without batching
func (pv *ParameterValidator) validateParameterImmediate(ctx context.Context, data []byte) (*ValidationResult, error) {
	startTime := time.Now()
	
	// First attempt length-prefixed format validation
	lpResult, lpErr := pv.validateLengthPrefixed(data)
	if lpErr == nil {
		// Success with length-prefixed format
		duration := time.Since(startTime)
		result := &ValidationResult{
			Success:       true,
			Format:        FormatLengthPrefixed,
			Data:          lpResult,
			ExecutionTime: duration,
		}
		
		// Update stats
		pv.updateStats(result)
		
		return result, nil
	}
	
	// If length-prefixed validation failed, try direct format
	dfResult, dfErr := pv.validateDirectFormat(data, DefaultContractIDSize)
	if dfErr == nil {
		// Success with direct format
		duration := time.Since(startTime)
		result := &ValidationResult{
			Success:       true,
			Format:        FormatDirect,
			Data:          dfResult,
			ExecutionTime: duration,
		}
		
		// Update stats
		pv.updateStats(result)
		
		return result, nil
	}
	
	// Both validation methods failed
	duration := time.Since(startTime)
	result := &ValidationResult{
		Success:       false,
		ExecutionTime: duration,
		Error:         ErrParameterValidationFailed,
	}
	
	// Update stats
	pv.updateStats(result)
	
	return result, ErrParameterValidationFailed
}

// validateLengthPrefixed validates a parameter in length-prefixed format
func (pv *ParameterValidator) validateLengthPrefixed(data []byte) ([]byte, error) {
	// Check minimum length for prefix
	if len(data) < 4 {
		return nil, fmt.Errorf("%w: data too short for length prefix", ErrInvalidLengthPrefix)
	}
	
	// Extract length from first 4 bytes (little-endian u32)
	length := binary.LittleEndian.Uint32(data[:4])
	
	// Validate reasonable length
	if length == 0 || length > MaxParameterSize {
		return nil, fmt.Errorf("%w: invalid length value %d", ErrInvalidLengthPrefix, length)
	}
	
	// Validate actual data length matches the prefix
	if uint32(len(data)) < 4+length {
		return nil, fmt.Errorf("%w: data length %d less than specified length %d", 
			ErrInvalidLengthPrefix, len(data)-4, length)
	}
	
	// Extract the actual data
	return data[4:4+length], nil
}

// validateDirectFormat validates a parameter in direct format
func (pv *ParameterValidator) validateDirectFormat(data []byte, expectedSize int) ([]byte, error) {
	// For direct format, verify against expected size if provided
	if expectedSize > 0 && len(data) != expectedSize {
		return nil, fmt.Errorf("direct format data size mismatch: expected %d, got %d", 
			expectedSize, len(data))
	}
	
	// For direct format, just check against max size
	if len(data) > MaxParameterSize {
		return nil, ErrParameterTooLarge
	}
	
	return data, nil
}

// processBatch processes a batch of parameters
func (pv *ParameterValidator) processBatch(batch *ParameterBatch) {
	var wg sync.WaitGroup
	results := make([]*ValidationResult, len(batch.Parameters))
	
	// Process parameters in parallel
	for i, param := range batch.Parameters {
		wg.Add(1)
		go func(index int, p *DualFormatParameter) {
			defer wg.Done()
			
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()
			
			result, _ := pv.validateParameterImmediate(ctx, p.Data)
			results[index] = result
			
			// Queue for cross-validation if enabled and successful
			if pv.enableCrossValidation && result.Success {
				select {
				case pv.resultChan <- result:
					// Result queued for cross-validation
				default:
					// Channel full, skip cross-validation
				}
			}
		}(i, param)
	}
	
	wg.Wait()
	
	// Update batch stats
	pv.stats.mu.Lock()
	pv.stats.BatchesProcessed++
	pv.stats.mu.Unlock()
}

// batchProcessorLoop processes batches from the channel
func (pv *ParameterValidator) batchProcessorLoop() {
	for batch := range pv.batchProcessChan {
		pv.processBatch(batch)
	}
}

// resultCollectorLoop handles cross-validation of results
func (pv *ParameterValidator) resultCollectorLoop() {
	for result := range pv.resultChan {
		// Skip already cross-validated results
		if result.CrossValidated {
			continue
		}
		
		// Perform cross-validation asynchronously
		go pv.crossValidateParameter(result)
	}
}

// batchScheduler periodically processes accumulated parameters
func (pv *ParameterValidator) batchScheduler() {
	ticker := time.NewTicker(pv.batchInterval)
	defer ticker.Stop()
	
	for range ticker.C {
		pv.batchMu.Lock()
		if len(pv.currentBatch.Parameters) > 0 {
			batch := pv.currentBatch
			pv.currentBatch = &ParameterBatch{
				Parameters: make([]*DualFormatParameter, 0, pv.maxBatchSize),
				BatchID:    generateBatchID(),
				Timestamp:  time.Now(),
			}
			pv.batchMu.Unlock()
			
			select {
			case pv.batchProcessChan <- batch:
				// Batch submitted successfully
			default:
				// Channel full, process in new goroutine
				go pv.processBatch(batch)
			}
		} else {
			pv.batchMu.Unlock()
		}
	}
}

// crossValidateParameter performs cross-validation between TEE types
func (pv *ParameterValidator) crossValidateParameter(result *ValidationResult) {
	// Get a TEE pair for cross-validation
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	
	// Select a random region for cross-validation
	regions := pv.coordinator.getRegionIDs()
	if len(regions) == 0 {
		return
	}
	
	// Pick first region for simplicity
	regionID := regions[0]
	
	// Get a TEE pair from the region
	pair, err := pv.coordinator.selectTEEPair(ctx, regionID)
	if err != nil {
		return
	}
	
	// Create a task for cross-validation
	task := &Task{
		ID:        generateTaskID(),
		RegionID:  regionID,
		WorkerIDs: []WorkerID{pair.SGXWorker, pair.SEVWorker},
		Data:      result.Data,
	}
	
	// Submit task to coordinator
	err = pv.coordinator.SubmitTask(ctx, task)
	if err != nil {
		return
	}
	
	// Mark as cross-validated
	result.CrossValidated = true
	
	// Update stats
	pv.stats.mu.Lock()
	pv.stats.CrossValidatedCount++
	pv.stats.mu.Unlock()
}

// GetValidationStats returns statistics about parameter validation
func (pv *ParameterValidator) GetValidationStats() *ValidationStats {
	pv.stats.mu.RLock()
	defer pv.stats.mu.RUnlock()
	
	// Create a new stats object without copying the mutex
	statsCopy := &ValidationStats{
		TotalProcessed:      pv.stats.TotalProcessed,
		SuccessCount:        pv.stats.SuccessCount,
		FailureCount:        pv.stats.FailureCount,
		LengthPrefixedCount: pv.stats.LengthPrefixedCount,
		DirectFormatCount:   pv.stats.DirectFormatCount,
		CrossValidatedCount: pv.stats.CrossValidatedCount,
		AvgProcessingTimeNs: pv.stats.AvgProcessingTimeNs,
		BatchesProcessed:    pv.stats.BatchesProcessed,
	}
	return statsCopy
}

// updateStats updates the validation statistics
func (pv *ParameterValidator) updateStats(result *ValidationResult) {
	pv.stats.mu.Lock()
	defer pv.stats.mu.Unlock()
	
	pv.stats.TotalProcessed++
	
	if result.Success {
		pv.stats.SuccessCount++
		
		if result.Format == FormatLengthPrefixed {
			pv.stats.LengthPrefixedCount++
		} else if result.Format == FormatDirect {
			pv.stats.DirectFormatCount++
		}
		
		// Update average processing time (weighted moving average)
		if pv.stats.AvgProcessingTimeNs == 0 {
			pv.stats.AvgProcessingTimeNs = uint64(result.ExecutionTime.Nanoseconds())
		} else {
			current := pv.stats.AvgProcessingTimeNs
			new := uint64(result.ExecutionTime.Nanoseconds())
			pv.stats.AvgProcessingTimeNs = (current*9 + new) / 10 // 10% weight to new value
		}
	} else {
		pv.stats.FailureCount++
	}
	
	if result.CrossValidated {
		pv.stats.CrossValidatedCount++
	}
}

// generateBatchID generates a unique batch ID
func generateBatchID() string {
	return fmt.Sprintf("batch-%d", time.Now().UnixNano())
}
