// coordination/parameter_task.go
package coordination

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

// ParameterTask represents a task specifically for parameter validation
type ParameterTask struct {
	// Common task fields
	ID       string
	Data     []byte
	Format   string
	Regional bool
	RegionID string

	// Parameter validation fields
	MaxSize           int
	ContractID        []byte
	CrossValidate     bool
	CrossValidationID string
}

// ParameterTaskResult contains the result of a parameter validation task
type ParameterTaskResult struct {
	ID            string
	Success       bool
	ValidatedData []byte
	Format        string
	Error         error
	TimeNs        int64
	Hash          []byte
	WorkerID      WorkerID
}

// HandleParameterValidation adds parameter validation capabilities to the coordinator
func (c *Coordinator) HandleParameterValidation(ctx context.Context, data []byte, options map[string]interface{}) (*ParameterTaskResult, error) {
	validator, err := c.getOrCreateValidator()
	if err != nil {
		return nil, fmt.Errorf("failed to get parameter validator: %w", err)
	}

	// Create a validation result channel
	resultChan := make(chan *ValidationResult, 1)
	errChan := make(chan error, 1)

	// Perform validation in a goroutine to support timeouts
	go func() {
		result, err := validator.ValidateParameter(ctx, data)
		if err != nil {
			errChan <- err
			return
		}
		resultChan <- result
	}()

	// Wait for result with timeout
	var result *ValidationResult
	select {
	case result = <-resultChan:
		// Got result
	case err := <-errChan:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(2 * time.Second):
		return nil, errors.New("parameter validation timed out")
	}

	// Convert to task result
	taskResult := &ParameterTaskResult{
		ID:            generateTaskID(),
		Success:       result.Success,
		ValidatedData: result.Data,
		Format:        result.Format,
		Error:         result.Error,
		TimeNs:        result.ExecutionTime.Nanoseconds(),
	}

	// Add SHA-256 hash of validated data if available
	if result.Success && result.Data != nil {
		taskResult.Hash = hashData(result.Data)
	}

	// Get cross-validation if required and not already done
	if getBoolOption(options, "cross_validate", true) && !result.CrossValidated && result.Success {
		crossResult, err := c.crossValidateParameter(ctx, result)
		if err != nil {
			// Log but don't fail the whole operation
			c.logf("Cross-validation failed: %v", err)
		} else if crossResult != nil {
			// Check if results match
			if !compareResults(result, crossResult) {
				c.logf("Warning: Cross-validation results don't match")
				taskResult.Success = false
				taskResult.Error = ErrCrossValidationMismatch
			}
		}
	}

	return taskResult, nil
}

// crossValidateParameter performs validation on another TEE type
func (c *Coordinator) crossValidateParameter(ctx context.Context, result *ValidationResult) (*ValidationResult, error) {
	// Get a TEE pair for validation
	regions := c.getRegionIDs()
	if len(regions) == 0 {
		return nil, errors.New("no regions available for cross-validation")
	}

	// Use first region
	regionID := regions[0]
	pair, err := c.selectTEEPair(ctx, regionID)
	if err != nil {
		return nil, fmt.Errorf("failed to select TEE pair: %w", err)
	}

	// Create dedicated task for cross-validation
	task := &Task{
		ID:       generateTaskID(),
		RegionID: regionID,
		WorkerIDs: []WorkerID{
			// Use worker of different type than the one that already validated
			// This ensures cross-validation between different TEE types
			pair.SEVWorker,
		},
		Data: result.Data,
		Timeout: 5 * time.Second, // Add reasonable timeout
	}

	// Submit and wait for the result
	err = c.SubmitTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("failed to submit cross-validation task: %w", err)
	}

	// Wait for task completion with timeout
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	for {
		select {
		case <-waitCtx.Done():
			return nil, waitCtx.Err()
		case <-time.After(100 * time.Millisecond):
			// Check if task is complete
			taskInfo, err := c.GetTaskStatus(task.ID)
			if err != nil {
				return nil, fmt.Errorf("failed to get task status: %w", err)
			}

			if taskInfo.Status == TaskStatusComplete {
				// Task completed successfully
				if len(taskInfo.Results) == 0 {
					return nil, errors.New("no results from cross-validation task")
				}

				// Convert result to ValidationResult
				crossResult := &ValidationResult{
					Success: true,
					Format:  result.Format,
					Data:    taskInfo.Results[0],
					CrossValidated: true,
				}

				return crossResult, nil
			} else if taskInfo.Status == TaskStatusFailed {
				return nil, fmt.Errorf("cross-validation task failed: %v", taskInfo.Error)
			}
			// Task still running, continue waiting
		}
	}
}

// getOrCreateValidator gets or creates a parameter validator
func (c *Coordinator) getOrCreateValidator() (*ParameterValidator, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if coordinator has validator in its context
	validator, ok := c.ctx.Value("parameter_validator").(*ParameterValidator)
	if ok && validator != nil {
		return validator, nil
	}

	// Create new validator
	config := &ParameterValidatorConfig{
		MaxBatchSize:          100,
		BatchInterval:         50 * time.Millisecond,
		EnableCrossValidation: true,
	}

	validator = NewParameterValidator(c, config)

	// Create new context with validator
	c.ctx = context.WithValue(c.ctx, "parameter_validator", validator)

	return validator, nil
}

// ValidateWasmParameters validates parameters specifically for WebAssembly contracts
// Handling both length-prefixed and direct formats
func (c *Coordinator) ValidateWasmParameters(ctx context.Context, data []byte, expectedDirectSize int) ([]byte, string, error) {
	// Quick check for maximum size
	if len(data) > MaxParameterSize {
		return nil, "", ErrParameterTooLarge
	}

	// Try length-prefixed format first
	if len(data) >= 4 {
		length := binary.LittleEndian.Uint32(data[:4])
		
		// Check if length is reasonable
		if length > 0 && length <= MaxParameterSize && len(data) >= int(4+length) {
			// Length prefix seems valid, use length-prefixed format
			return data[4 : 4+length], FormatLengthPrefixed, nil
		}
	}

	// Try direct format
	if expectedDirectSize > 0 {
		if len(data) != expectedDirectSize {
			return nil, "", fmt.Errorf("direct format data size mismatch: expected %d, got %d", 
				expectedDirectSize, len(data))
		}
	}

	// Use direct format
	return data, FormatDirect, nil
}

// RegisterParameterHandler registers handlers for parameter validation
func (c *Coordinator) RegisterParameterHandler() error {
	validator, err := c.getOrCreateValidator()
	if err != nil {
		return err
	}

	// Log validator initialization
	c.logf("Parameter validator initialized with batch size %d and interval %v", 
		validator.maxBatchSize, validator.batchInterval)

	return nil
}

// Helper functions
func getBoolOption(options map[string]interface{}, key string, defaultValue bool) bool {
	if options == nil {
		return defaultValue
	}
	
	value, ok := options[key]
	if !ok {
		return defaultValue
	}
	
	boolValue, ok := value.(bool)
	if !ok {
		return defaultValue
	}
	
	return boolValue
}

func compareResults(r1, r2 *ValidationResult) bool {
	if r1 == nil || r2 == nil {
		return false
	}
	
	if r1.Format != r2.Format {
		return false
	}
	
	if len(r1.Data) != len(r2.Data) {
		return false
	}
	
	return bytes.Equal(r1.Data, r2.Data)
}

func hashData(data []byte) []byte {
	h := sha256.New()
	h.Write(data)
	return h.Sum(nil)
}

func (c *Coordinator) logf(format string, args ...interface{}) {
	fmt.Printf("[Coordinator] "+format+"\n", args...)
}
