


// coordination/tests/performance_test.go
package tests

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rhombus-tech/vm/coordination"
)

const (
	// Test configuration
	NumParametersPerTest = 10000
	MaxConcurrency       = 100
	ParameterSizeBytes   = 256
	TestCaseDurationSeconds = 5
	CrossValidateRatio   = 0.25 // 25% of parameters will be cross-validated
	
	// Target TPS for NASDAQ
	TargetTPS = 1100
)

// TestParameterValidationPerformance tests the performance of parameter validation
func TestParameterValidationPerformance(t *testing.T) {
	// Create mock coordinator for testing
	mockCoordinator := NewMockCoordinator()
	
	// Validator functionality is built into the mock coordinator
	
	// No need to register TEE nodes with our simplified mock
	
	// Run performance tests for different parameter types
	runTests := []struct {
		name                string
		parameterFormat     string
		withCrossValidation bool
	}{
		{
			name:           "Length-Prefixed Format",
			parameterFormat: "length_prefixed",
			withCrossValidation: false,
		},
		{
			name:           "Direct Format",
			parameterFormat: "direct",
			withCrossValidation: false,
		},
		{
			name:           "Length-Prefixed With Cross-Validation",
			parameterFormat: "length_prefixed",
			withCrossValidation: true,
		},
		{
			name:           "Direct Format With Cross-Validation",
			parameterFormat: "direct",
			withCrossValidation: true,
		},
		{"Mixed Format Batch Processing", "mixed", false},
		{"Mixed Format With Cross-Validation", "mixed", true},
	}
	
	for _, test := range runTests {
		t.Run(test.name, func(t *testing.T) {
			results := runPerformanceTest(t, mockCoordinator, test.parameterFormat, test.withCrossValidation)
			analyzeResults(t, test.name, results, test.withCrossValidation)
		})
	}
}

// runPerformanceTest runs a single performance test with the given configuration
func runPerformanceTest(t *testing.T, coordinator *MockCoordinator, format string, crossValidate bool) *TestResults {
	// Generate test parameters based on format
	parameters := generateTestParameters(format, NumParametersPerTest)
	
	// Set up test context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), TestCaseDurationSeconds*time.Second)
	defer cancel()
	
	// Set up test result tracking
	results := &TestResults{
		StartTime:      time.Now(),
		Format:         format,
		CrossValidated: crossValidate,
	}
	
	// Rate limiting to avoid overwhelming the system
	limiter := make(chan struct{}, MaxConcurrency)
	var wg sync.WaitGroup
	
	// Process parameters in parallel with rate limiting
	for i, param := range parameters {
		// Fill the limiter channel (block if full)
		limiter <- struct{}{}
		
		wg.Add(1)
		go func(index int, data []byte) {
			defer wg.Done()
			defer func() { <-limiter }() // Release a slot when done
			
			// Skip if context is done
			if ctx.Err() != nil {
				return
			}
			
			// Process parameter using our mock implementation
			startTime := time.Now()
			result, err := coordinator.ValidateParameter(ctx, data)
			duration := time.Since(startTime)
			
			// Record result
			if err != nil {
				atomic.AddUint64(&results.FailureCount, 1)
				return
			}
			
			atomic.AddUint64(&results.SuccessCount, 1)
			atomic.AddUint64(&results.TotalTimeNs, uint64(duration.Nanoseconds()))
			
			if result.Format == "length_prefixed" {
				atomic.AddUint64(&results.LengthPrefixedCount, 1)
			} else if result.Format == "direct" {
				atomic.AddUint64(&results.DirectFormatCount, 1)
			}
		}(i, param)
	}
	
	// Either wait for all tasks to complete or timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	
	select {
	case <-done:
		// All tasks completed
	case <-ctx.Done():
		// Timeout reached - this is expected in time-bounded tests
	}
	
	// Calculate final results
	results.EndTime = time.Now()
	results.Duration = results.EndTime.Sub(results.StartTime)
	results.TPS = float64(results.SuccessCount) / results.Duration.Seconds()
	
	return results
}

// TestResults tracks performance test results
type TestResults struct {
	StartTime          time.Time
	EndTime            time.Time
	Duration           time.Duration
	Format             string
	CrossValidated     bool
	SuccessCount       uint64
	FailureCount       uint64
	TotalTimeNs        uint64
	LengthPrefixedCount uint64
	DirectFormatCount  uint64
	TPS                float64
}

// analyzeResults analyzes and reports test results
func analyzeResults(t *testing.T, testName string, results *TestResults, crossValidated bool) {
	t.Logf("===== Performance Test Results: %s =====", testName)
	t.Logf("Duration: %.2f seconds", results.Duration.Seconds())
	t.Logf("Parameters Processed: %d (success: %d, failure: %d)", 
		results.SuccessCount+results.FailureCount, results.SuccessCount, results.FailureCount)
	
	if results.SuccessCount > 0 {
		avgTimeMs := float64(results.TotalTimeNs) / float64(results.SuccessCount) / 1e6
		t.Logf("Average Processing Time: %.3f ms per parameter", avgTimeMs)
	}
	
	t.Logf("Throughput: %.2f TPS (parameters per second)", results.TPS)
	
	// Report distribution of parameter formats
	if results.Format == "mixed" {
		t.Logf("Format Distribution: Length-Prefixed: %d (%.1f%%), Direct: %d (%.1f%%)",
			results.LengthPrefixedCount, 
			float64(results.LengthPrefixedCount)/float64(results.SuccessCount)*100,
			results.DirectFormatCount, 
			float64(results.DirectFormatCount)/float64(results.SuccessCount)*100)
	}
	
	// Check if we hit the target TPS
	if results.TPS >= TargetTPS {
		t.Logf("✅ PASSED: Achieved %.2f TPS, exceeding target of %d TPS", results.TPS, TargetTPS)
	} else {
		t.Logf("❌ FAILED: Achieved only %.2f TPS, below target of %d TPS", results.TPS, TargetTPS)
		if !crossValidated {
			t.Logf("Recommendation: Optimize parameter validation for higher throughput")
		} else {
			t.Logf("Recommendation: Consider reducing cross-validation ratio or implementing batched cross-validation")
		}
	}
}

// setupMockTEEEnvironment sets up a mock TEE environment for testing
func setupMockTEEEnvironment(t *testing.T, coordinator *coordination.Coordinator) {
	ctx := context.Background()
	
	// Create mock SGX and SEV workers
	sgxID := coordination.WorkerID("sgx-node-1")
	sevID := coordination.WorkerID("sev-node-1")
	
	// Register workers
	if err := coordinator.RegisterWorker(ctx, sgxID, []byte("mock-sgx-enclave")); err != nil {
		t.Fatalf("Failed to register SGX worker: %v", err)
	}
	
	if err := coordinator.RegisterWorker(ctx, sevID, []byte("mock-sev-enclave")); err != nil {
		t.Fatalf("Failed to register SEV worker: %v", err)
	}
	
	// Register region with TEE pair
	if err := coordinator.RegisterRegion(ctx, "test-region", [2]coordination.WorkerID{sgxID, sevID}); err != nil {
		t.Fatalf("Failed to register region: %v", err)
	}
}

// generateTestParameters generates test parameters in the specified format
func generateTestParameters(format string, count int) [][]byte {
	parameters := make([][]byte, count)
	
	for i := 0; i < count; i++ {
		var param []byte
		
		// Determine format for this parameter
		paramFormat := format
		if format == "mixed" {
			// For mixed format, alternate between length-prefixed and direct
			if i%2 == 0 {
				paramFormat = coordination.FormatLengthPrefixed
			} else {
				paramFormat = coordination.FormatDirect
			}
		}
		
		if paramFormat == coordination.FormatLengthPrefixed {
			// Length-prefixed format: 4-byte length + data
			data := make([]byte, ParameterSizeBytes)
			rand.Read(data)
			
			// Create parameter with length prefix
			param = make([]byte, 4+len(data))
			binary.LittleEndian.PutUint32(param, uint32(len(data)))
			copy(param[4:], data)
		} else {
			// Direct format: just the data
			param = make([]byte, 32) // Standard size for contract ID
			rand.Read(param)
		}
		
		parameters[i] = param
	}
	
	return parameters
}

// TestParameterFormatDistribution tests parameter format detection accuracy
func TestParameterFormatDistribution(t *testing.T) {
	t.Log("Starting parameter format distribution test")
	// Create mock coordinator for testing
	mockCoordinator := NewMockCoordinator()
	
	// Test cases for different parameter formats
	testCases := []struct {
		name           string
		data           []byte
		expectedFormat string
		expectError    bool
	}{
		{
			name:           "Valid Length-Prefixed Small",
			data:           createTestLengthPrefixedParam(16),
			expectedFormat: "length_prefixed",
			expectError:    false,
		},
		{
			name:           "Valid Length-Prefixed Medium",
			data:           createTestLengthPrefixedParam(256),
			expectedFormat: "length_prefixed",
			expectError:    false,
		},
		{
			name:           "Valid Length-Prefixed Large",
			data:           createTestLengthPrefixedParam(1000),
			expectedFormat: "length_prefixed",
			expectError:    false,
		},
		{
			name:           "Invalid Length-Prefixed Too Large",
			data:           createTestLengthPrefixedParam(2000), // Exceeds 1KB limit
			expectedFormat: "",
			expectError:    true,
		},
		{
			name:           "Valid Direct Format 32 Bytes",
			data:           createTestDirectParam(32),
			expectedFormat: "direct",
			expectError:    false,
		},
		{
			name:           "Valid Direct Format 64 Bytes",
			data:           createTestDirectParam(64),
			expectedFormat: "direct",
			expectError:    false,
		},
		{
			name:           "Invalid Direct Format Too Large",
			data:           createTestDirectParam(2000), // Exceeds 1KB limit
			expectedFormat: "",
			expectError:    true,
		},
		{
			name:           "Ambiguous Format", // First 4 bytes could be interpreted as length
			data:           createTestAmbiguousParam(),
			expectedFormat: "length_prefixed", // We expect length-prefixed to be tried first
			expectError:    false,
		},
		{
			name:           "Empty Parameter",
			data:           []byte{},
			expectedFormat: "",
			expectError:    true,
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			result, err := mockCoordinator.ValidateParameter(ctx, tc.data)
			
			if tc.expectError {
				if err == nil {
					t.Errorf("Expected error, but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Did not expect error, but got: %v", err)
				} else if result.Format != tc.expectedFormat {
					t.Errorf("Expected format %s, but got %s", tc.expectedFormat, result.Format)
				}
				
				// Verify that the data was correctly extracted
				if !tc.expectError && result.Success {
					if tc.expectedFormat == "length_prefixed" {
						// Should match the data without the length prefix
						expectedData := tc.data[4:]
						if !bytes.Equal(result.ValidatedData, expectedData) {
							t.Errorf("Validated data doesn't match expected data for %s\nExpected: %v\nGot: %v", 
								tc.name, expectedData[:10], result.ValidatedData[:10])
						}
					} else if tc.expectedFormat == "direct" {
						// Should match the direct data
						if !bytes.Equal(result.ValidatedData, tc.data) {
							t.Errorf("Validated data doesn't match expected data")
						}
					}
				}
			}
		})
	}
}

// Helper functions to create test parameters

func createTestLengthPrefixedParam(dataSize int) []byte {
	data := make([]byte, dataSize)
	rand.Read(data)
	
	param := make([]byte, 4+dataSize)
	binary.LittleEndian.PutUint32(param, uint32(dataSize))
	copy(param[4:], data)
	
	return param
}

func createTestDirectParam(size int) []byte {
	param := make([]byte, size)
	rand.Read(param)
	return param
}

func createTestAmbiguousParam() []byte {
	// Create a parameter where the first 4 bytes could be a valid length
	// but also works as direct data
	param := make([]byte, 64)
	rand.Read(param)
	
	// Set the first 4 bytes to represent a reasonable length (32)
	binary.LittleEndian.PutUint32(param, 32)
	
	return param
}
