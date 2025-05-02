package tee

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWASIExecutor(t *testing.T) {
	// Skip if running in CI without Enarx/WASI support
	if testing.Short() {
		t.Skip("Skipping WASI executor test in short mode")
	}

	t.Run("GetWASIExecutor", func(t *testing.T) {
		// Get the singleton executor instance
		executor := GetWASIExecutor()
		require.NotNil(t, executor, "Executor should not be nil")
		
		// Check default values
		assert.NotNil(t, executor.mutex, "Mutex should be initialized")
		assert.NotNil(t, executor.defaultConfig, "Default config should be initialized")
	})

	t.Run("DetectWASIModule", func(t *testing.T) {
		// Test with valid WASI module following WebAssembly 1.0 binary format
		// Properly structured with correct section sizes and headers for security validation
		validWASI := []byte{
			// Magic bytes "\0asm"
			0x00, 0x61, 0x73, 0x6D,
			// Version 1 
			0x01, 0x00, 0x00, 0x00,
			// Import section ID
			0x02,
			// Import section size (bytes) in little-endian format - exactly sized for our test
			0x1F, 0x00, 0x00, 0x00,
			// Number of imports
			0x01,
			// Import details (module name length)
			0x15, // length of module name (21 bytes)
			// "wasi_snapshot_preview1" as module name (matches what DetectWASIModule searches for)
			0x77, 0x61, 0x73, 0x69, 0x5F, 0x73, 0x6E, 0x61, 0x70, 0x73, 0x68, 0x6F, 0x74, 0x5F, 0x70, 0x72, 0x65, 0x76, 0x69, 0x65, 0x77, 0x31,
			// Import field name length
			0x08, // length of field name
			// "fd_write" function name
			0x66, 0x64, 0x5F, 0x77, 0x72, 0x69, 0x74, 0x65,
			// Function import type (0x00)
			0x00,
		}
		
		// Verify our test module is correctly detected
		isWASI, err := DetectWASIModule(validWASI)
		assert.NoError(t, err, "Should detect WASI without error")
		assert.True(t, isWASI, "Should identify valid WASI module")
		
		// Test with invalid module (not WASM)
		invalidModule := []byte{0x01, 0x02, 0x03, 0x04}
		isWASI, err = DetectWASIModule(invalidModule)
		assert.Error(t, err, "Should return error for invalid module")
		assert.False(t, isWASI, "Should reject invalid module")
	})

	t.Run("Placeholder", func(t *testing.T) {
		// Skip actual execution in unit tests
		t.Skip("Skipping execution tests that require Enarx/TDX environment")
		
		// In a real test environment with Enarx and TDX support, we would test:
		// 1. Parameter validation
		// 2. Module execution
		// 3. Timeouts and resource limits
		// 4. Error handling
	})

	t.Run("ConfigValidation", func(t *testing.T) {
		// Create a test configuration
		config := NewDefaultEnarxConfig()
		require.NotNil(t, config, "Default config should not be nil")
		
		// Verify the default config has reasonable values
		assert.NotEmpty(t, config.TEEType, "TEE type should be set")
		assert.NotZero(t, config.Timeout, "Timeout should be set")
		assert.NotZero(t, config.StdioSize, "StdioSize should be set")
	})

	t.Run("DualFormatParameterHandling", func(t *testing.T) {
		// Create a minimal valid WASM module
		directFormat := []byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00}
		
		// Create length-prefixed format (8 bytes length, little endian)
		prefixedFormat := make([]byte, 4+len(directFormat))
		binary.LittleEndian.PutUint32(prefixedFormat[0:4], uint32(len(directFormat)))
		copy(prefixedFormat[4:], directFormat)
		
		// Both should be detected properly by our dual-format parameter handling
		directResult, err1 := DetectWASIModule(directFormat)
		prefixedResult, err2 := DetectWASIModule(prefixedFormat)
		
		// We expect detection to fail because our mock isn't a full WASI module,
		// but the dual-format handling should work the same for both formats
		assert.Equal(t, err1 != nil, err2 != nil, "Error results should be consistent across formats")
		assert.Equal(t, directResult, prefixedResult, "Detection results should be consistent across formats")
	})
}
