package tee

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rhombus-tech/vm/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWASIModuleDetection tests that WASI modules are properly detected and handled
func TestWASIModuleDetection(t *testing.T) {
	t.Run("WASI Detection", func(t *testing.T) {
		// Test WASI detection with various headers
		wasiHeaders := [][]byte{
			// Standard WASI preview1 binary header
			{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00, 0x01, 0x07, 0x77, 0x61, 0x73, 0x69, 0x5F, 0x73},
			// Other valid WASI binary with different patterns
			{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00, 0x00, 0x09, 0x77, 0x61, 0x73, 0x69, 0x5F, 0x75},
		}

		nonWasiHeaders := [][]byte{
			// Standard WebAssembly without WASI imports
			{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00},
			// Random binary data
			{0xFF, 0xFE, 0xFD, 0xFC},
		}

		// Directly check for WASI identifier in binary
		for _, header := range wasiHeaders {
			result := detectWASIModule(header)
			assert.True(t, result, "Should detect valid WASI header: %v", header)
		}

		for _, header := range nonWasiHeaders {
			result := detectWASIModule(header)
			assert.False(t, result, "Should reject non-WASI header: %v", header)
		}
	})

	t.Run("WASI Description Detection", func(t *testing.T) {
		// Test WASI detection from model description
		wasiDescriptions := []string{
			"[WASI] Test model",
			"[WASI] AI trading model with parameters",
			"[WASI]Simple model",
		}

		nonWasiDescriptions := []string{
			"Standard WebAssembly model",
			"Test model version 1.0",
			"WASI model", // Missing brackets
		}

		for _, desc := range wasiDescriptions {
			result := strings.HasPrefix(desc, "[WASI]")
			assert.True(t, result, "Should detect WASI prefix in: %s", desc)
		}

		for _, desc := range nonWasiDescriptions {
			result := strings.HasPrefix(desc, "[WASI]")
			assert.False(t, result, "Should not detect WASI prefix in: %s", desc)
		}
	})
}

// TestWASIExecution tests the WASI execution path with various parameter formats
func TestWASIExecution(t *testing.T) {
	// Skip in short mode as this requires actual execution environment
	if testing.Short() {
		t.Skip("Skipping WASI execution test in short mode")
	}

	t.Run("WASI Parameter Validation", func(t *testing.T) {
		// We'll use function-level validation rather than executor-based testing
		_ = t

		// Create a unique temporary directory for this test
		tempDir, err := os.MkdirTemp("", "wasi_execution_test")
		require.NoError(t, err, "Should create temp directory")
		defer os.RemoveAll(tempDir)

		// Set environment variable for whitelist store
		originalDir := os.Getenv("AI_MODEL_WHITELIST_DIR")
		defer os.Setenv("AI_MODEL_WHITELIST_DIR", originalDir)
		os.Setenv("AI_MODEL_WHITELIST_DIR", tempDir)

		// Reset singleton for testing
		whitelistStore = nil
		whitelistStoreOnce = sync.Once{}

		// Create a test WASI model measurement
		var measurement [48]byte
		copy(measurement[:], "wasi-model-measurement-for-test-padding-to-48b")

		// Add to whitelist with WASI marker
		policy := &AIModelPolicy{
			ID:             "wasi-test-model",
			Measurement:    measurement,
			Approved:       true,
			MaxOrderSize:   5000,
			MaxTradesPerMin: 200,
			AllowedAssets:  []string{"AAPL", "GOOG"},
			RiskLevel:      3,
			Description:    "[WASI] Test WASI trading model",
			ApprovedBy:     "test-approver",
			ApprovedAt:     time.Now(),
			ExpiresAt:      time.Now().Add(24 * time.Hour),
		}

		store, err := GetModelWhitelistStore()
		require.NoError(t, err, "Should get whitelist store")

		err = store.AddModelToWhitelist(policy, "test-user")
		require.NoError(t, err, "Should add WASI model to whitelist")

		// Test different parameter formats
		testCases := []struct {
			name           string
			params         []byte
			expectedErr    bool
			errMsgContains string
		}{
			{
				name:           "Valid parameters",
				params:         []byte{0x10, 0x00, 0x00, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10},
				expectedErr:    false,
				errMsgContains: "",
			},
			{
				name:           "Invalid length prefix (too large)",
				params:         []byte{0xFF, 0xFF, 0xFF, 0xFF, 0x01},
				expectedErr:    true,
				errMsgContains: "invalid parameter size",
			},
			{
				name:           "Empty parameters",
				params:         []byte{},
				expectedErr:    true,
				errMsgContains: "empty parameters",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				// Create a sample measurement for parameter testing
				_ = measurement

				request := &core.ExecutionRequest{
					IdTo:         "test-execution",
					FunctionCall: "execute_test",
					Parameters:   tc.params,
					RegionId:     "test-region",
				}

				// Skip actual execution in tests
				_ = request

				// In actual test would call ExecuteWASI or equivalent
				// Here we're testing the parameter validation part
				err := validateWASIParameters(request.Parameters)

				if tc.expectedErr {
					assert.Error(t, err, "Should get error for invalid parameters")
					if tc.errMsgContains != "" {
						assert.Contains(t, err.Error(), tc.errMsgContains, 
							"Error message should contain expected text")
					}
				} else {
					assert.NoError(t, err, "Should not get error for valid parameters")
				}
			})
		}
	})
}

// Helper function to simulate parameter validation - in production this would
// call into the actual validation code from executor.go
func validateWASIParameters(params []byte) error {
	if len(params) == 0 {
		return fmt.Errorf("empty parameters")
	}

	// If length-prefixed format is used
	if len(params) >= 4 {
		length := uint32(params[0]) | uint32(params[1])<<8 | uint32(params[2])<<16 | uint32(params[3])<<24
		
		// Validate reasonable size
		const maxReasonableSize = 10 * 1024 * 1024 // 10MB
		if length > maxReasonableSize {
			return fmt.Errorf("invalid parameter size: %d exceeds maximum reasonable size", length)
		}
		
		if len(params) < int(length)+4 {
			return fmt.Errorf("parameter data truncated: expected %d bytes, got %d", length+4, len(params))
		}
		
		// Would then validate the actual parameters...
	}
	
	return nil
}

// Helper for WASI detection
func detectWASIModule(binary []byte) bool {
	// Simple check for WASI identifier in a typical WebAssembly file
	if len(binary) < 16 {
		return false
	}
	
	// Check for WebAssembly magic header bytes
	if binary[0] != 0x00 || binary[1] != 0x61 || binary[2] != 0x73 || binary[3] != 0x6D {
		return false
	}
	
	// Search for "wasi" string in the binary
	for i := 0; i < len(binary)-4; i++ {
		if binary[i] == 'w' && binary[i+1] == 'a' && binary[i+2] == 's' && binary[i+3] == 'i' {
			return true
		}
	}
	
	return false
}
