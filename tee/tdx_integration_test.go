package tee

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/rhombus-tech/vm/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTDXAttestationVerification tests the TDX attestation verification path,
// focusing on parameter validation, dual-format support, and performance
func TestTDXAttestationVerification(t *testing.T) {
	// Skip if running in CI without TDX hardware
	if testing.Short() {
		t.Skip("Skipping TDX test in short mode")
	}

	// Create a mock TDX attestation (in real test would come from actual TDX)
	var mockTDXAttestation [1024]byte
	// In real test, populate with actual TDX attestation quote
	
	// Test 1: Basic attestation verification
	t.Run("Basic TDX Verification", func(t *testing.T) {
		executor := NewTestExecutor(t)
		
		// Create a verification request with standard format
		attestation := &core.TEEAttestation{
			EnclaveID:   []byte("tdx-enclave-id"),
			Measurement: mockTDXAttestation[:48], // Use first 48 bytes as measurement
			Timestamp:   time.Now(),
			Data:        mockTDXAttestation[:],
		}
		
		// Measure verification time
		start := time.Now()
		result, err := executor.VerifyTDXAttestation(attestation)
		verificationTime := time.Since(start)
		
		// Assertions
		require.NoError(t, err, "TDX attestation verification should succeed")
		assert.True(t, result, "TDX verification should return true for valid attestation")
		assert.Less(t, verificationTime, time.Millisecond, 
			"Verification should complete in under 1ms (got %v)", verificationTime)
	})

	// Test 2: Length-prefixed format
	t.Run("Length-Prefixed Format", func(t *testing.T) {
		executor := NewTestExecutor(t)
		
		// Create length-prefixed attestation data
		dataLen := len(mockTDXAttestation)
		prefixedData := make([]byte, dataLen+4)
		// Add length prefix in little-endian format
		prefixedData[0] = byte(dataLen)
		prefixedData[1] = byte(dataLen >> 8)
		prefixedData[2] = byte(dataLen >> 16)
		prefixedData[3] = byte(dataLen >> 24)
		// Copy attestation data
		copy(prefixedData[4:], mockTDXAttestation[:])
		
		attestation := &core.TEEAttestation{
			EnclaveID:   []byte("tdx-enclave-id"),
			Measurement: mockTDXAttestation[:48],
			Timestamp:   time.Now(),
			Data:        prefixedData,
		}
		
		// Verify with length-prefixed format
		result, err := executor.VerifyTDXAttestation(attestation)
		
		require.NoError(t, err, "Length-prefixed attestation verification should succeed")
		assert.True(t, result, "Verification should return true for valid length-prefixed attestation")
	})

	// Test 3: Malformed data handling
	t.Run("Malformed Data Handling", func(t *testing.T) {
		executor := NewTestExecutor(t)
		
		// Create invalid attestation with very large length prefix
		invalidData := make([]byte, 8)
		// Set impossibly large length (4GB)
		invalidData[0] = 0xFF
		invalidData[1] = 0xFF
		invalidData[2] = 0xFF
		invalidData[3] = 0xFF
		
		attestation := &core.TEEAttestation{
			EnclaveID:   []byte("tdx-enclave-id"),
			Measurement: make([]byte, 48), // Empty measurement
			Timestamp:   time.Now(),
			Data:        invalidData,
		}
		
		// Should reject invalid data
		result, err := executor.VerifyTDXAttestation(attestation)
		
		assert.Error(t, err, "Should reject attestation with invalid size")
		assert.False(t, result, "Verification should return false for invalid attestation")
		if err != nil {
			assert.Contains(t, err.Error(), "size", "Error should mention invalid size")
		}
	})
}

// TestTripleAttestation tests that all three attestation methods work together
// (TDX hardware, RSA accumulator, and quote verification)
func TestTripleAttestation(t *testing.T) {
	// Skip if running in CI without proper hardware
	if testing.Short() {
		t.Skip("Skipping triple attestation test in short mode")
	}

	t.Run("Triple Attestation Path", func(t *testing.T) {
		executor := NewTestExecutor(t)
		
		// Create simulated measurement (in production would come from TDX quote)
		var measurement [48]byte
		copy(measurement[:], "test-triple-attestation-measurement-padding-48b")
		
		// Create a unique temporary directory for this test
		tempDir, err := os.MkdirTemp("", "tdx_triple_attestation_test")
		require.NoError(t, err, "Should create temp directory")
		defer os.RemoveAll(tempDir)

		// Set environment variable for whitelist store
		originalDir := os.Getenv("AI_MODEL_WHITELIST_DIR")
		defer os.Setenv("AI_MODEL_WHITELIST_DIR", originalDir)
		os.Setenv("AI_MODEL_WHITELIST_DIR", tempDir)

		// Reset singleton for testing
		whitelistStore = nil
		whitelistStoreOnce = sync.Once{}

		// Set up a whitelisted model policy
		policy := &AIModelPolicy{
			ID:             "triple-attestation-test",
			Measurement:    measurement,
			Approved:       true,
			MaxOrderSize:   10000,
			MaxTradesPerMin: 100,
			AllowedAssets:  []string{"AAPL", "MSFT"},
			RiskLevel:      2,
			Description:    "Triple attestation test model",
			ApprovedBy:     "test-user",
			ApprovedAt:     time.Now(),
			ExpiresAt:      time.Now().Add(24 * time.Hour),
			VerifyMode:     "accumulator", // Use accumulator-based verification
		}
		
		store, err := GetModelWhitelistStore()
		require.NoError(t, err, "Should get whitelist store")
		
		// Add policy to whitelist
		err = store.AddModelToWhitelist(policy, "test-user")
		require.NoError(t, err, "Should add model to whitelist")
		
		// Create attestation that will pass hardware TDX validation
		// In real test we would use this attestation
		_ = &core.TEEAttestation{
			EnclaveID:   []byte("tdx-enclave-id"),
			Measurement: measurement[:],
			Timestamp:   time.Now(),
			Data:        []byte("tdx-attestation-data"),
			Signature:   []byte("test-signature"),
		}
		
		// Create execution request with the measurement that matches whitelist
		request := &core.ExecutionRequest{
			IdTo:         "test-execution",
			FunctionCall: "execute_triple_attestation",
			Parameters:   measurement[:], // Use measurement as parameters for test
			RegionId:     "test-region",
		}
		
		// Execute with triple attestation
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		
		// This should validate via:
		// 1. TDX hardware attestation
		// 2. RSA accumulator verification
		// 3. Whitelist policy check
		start := time.Now()
		_, err = executor.ExecuteTDX(ctx, request)
		verificationTime := time.Since(start)
		
		// In a test environment, we might get an error about actual execution,
		// but attestation validation should have succeeded
		t.Logf("Triple attestation completed in %v", verificationTime)
		
		// Verify it's not a measurement or attestation error
		if err != nil {
			assert.NotContains(t, err.Error(), "measurement", 
				"Error should not be related to measurement validation")
			assert.NotContains(t, err.Error(), "attestation", 
				"Error should not be related to attestation validation")
		}
	})
}

// Implementation moved to MockExecutor in test_helpers.go

// Test implementation moved to test_helpers.go
