package tee

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rhombus-tech/vm/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRegulatoryAuditCompliance tests that AI model operations are properly audit-logged
// for regulatory compliance purposes
func TestRegulatoryAuditCompliance(t *testing.T) {
	// Create a temporary directory for audit testing
	tempDir, err := os.MkdirTemp("", "regulatory_audit_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Set environment variable for whitelist/audit store
	originalDir := os.Getenv("AI_MODEL_WHITELIST_DIR")
	defer os.Setenv("AI_MODEL_WHITELIST_DIR", originalDir)
	os.Setenv("AI_MODEL_WHITELIST_DIR", tempDir)

	t.Run("Audit Trail Completeness", func(t *testing.T) {
		// Force singleton recreation
		whitelistStore = nil
		whitelistStoreOnce = sync.Once{}

		store, err := GetModelWhitelistStore()
		require.NoError(t, err, "Should get whitelist store")

		// Create test measurements
		var measurements []([48]byte)
		numModels := 10
		
		// Generate and add multiple models to test audit logging
		for i := 0; i < numModels; i++ {
			var measurement [48]byte
			copy(measurement[:], fmt.Sprintf("regulatory-test-model-%02d-padding-to-make-48b", i))
			measurements = append(measurements, measurement)
			
			policy := &AIModelPolicy{
				ID:             fmt.Sprintf("reg-model-%d", i),
				Measurement:    measurement,
				Approved:       true,
				MaxOrderSize:   1000 * uint64(i+1),
				MaxTradesPerMin: 10 * uint32(i+1),
				AllowedAssets:  []string{"REG1", "REG2"},
				RiskLevel:      uint8(i % 5),
				Description:    fmt.Sprintf("Regulatory test model %d", i),
				ApprovedBy:     fmt.Sprintf("regulator-%d", i),
				ApprovedAt:     time.Now(),
				ExpiresAt:      time.Now().Add(24 * time.Hour),
			}
			
			userID := fmt.Sprintf("regulator-%d", i)
			err = store.AddModelToWhitelist(policy, userID)
			require.NoError(t, err, "Adding model %d should succeed", i)
		}
		
		// Verify audit log exists
		auditLogPath := filepath.Join(tempDir, testAuditLogFilename)
		require.FileExists(t, auditLogPath, "Audit log file should exist")
		
		// Read and parse audit log
		content, err := os.ReadFile(auditLogPath)
		require.NoError(t, err, "Should be able to read audit log")
		
		var auditData AuditLogData
		err = json.Unmarshal(content, &auditData)
		require.NoError(t, err, "Should be able to parse audit log JSON")
		
		// Verify all model additions were logged
		assert.GreaterOrEqual(t, len(auditData.Entries), numModels, 
			"Should have at least %d audit entries", numModels)
			
		// Verify required regulatory fields are present in all entries
		for _, entry := range auditData.Entries {
			assert.NotEmpty(t, entry.ModelID, "Model ID should be logged")
			assert.NotEmpty(t, entry.UserID, "User ID should be logged")
			assert.NotEmpty(t, entry.Action, "Action should be logged")
			assert.False(t, entry.Timestamp.IsZero(), "Timestamp should be logged")
			assert.NotZero(t, entry.ID, "Entry ID should be non-zero")
			
			// Verify timestamp is reasonable (within last hour)
			assert.True(t, time.Since(entry.Timestamp) < time.Hour, 
				"Timestamp should be recent")
		}
	})
}

// TestPerformanceBenchmarks tests the performance characteristics of our TDX
// and WASI implementations, focusing on sub-millisecond verification
func TestPerformanceBenchmarks(t *testing.T) {
	// Skip in CI without proper hardware
	if testing.Short() {
		t.Skip("Skipping performance benchmarks in short mode")
	}

	t.Run("Verification Performance", func(t *testing.T) {
		// Number of iterations for averaging
		iterations := 100
		
		// Create mock attestation data
		var attestationData [1024]byte
		// In real test would use realistic mock data
		
		// Create attestation
		attestation := &core.TEEAttestation{
			EnclaveID:   []byte("tdx-enclave-id"),
			Measurement: attestationData[:48], // Use first 48 bytes as measurement
			Timestamp:   time.Now(),
			Data:        attestationData[:],
		}
		
		executor := NewTestExecutor(t)
		
		// Warm up
		for i := 0; i < 5; i++ {
			executor.VerifyTDXAttestation(attestation)
		}
		
		// Measure verification time
		var totalTime time.Duration
		var maxTime time.Duration
		
		for i := 0; i < iterations; i++ {
			start := time.Now()
			_, err := executor.VerifyTDXAttestation(attestation)
			duration := time.Since(start)
			
			if err != nil {
				t.Fatalf("Verification failed: %v", err)
			}
			
			totalTime += duration
			if duration > maxTime {
				maxTime = duration
			}
		}
		
		avgTime := totalTime / time.Duration(iterations)
		
		// Log results
		t.Logf("TDX Verification Performance:")
		t.Logf("  Average: %v", avgTime)
		t.Logf("  Max: %v", maxTime)
		t.Logf("  Iterations: %d", iterations)
		
		// Assert performance requirements
		assert.Less(t, avgTime, time.Millisecond, 
			"Average verification time should be sub-millisecond")
		assert.Less(t, float64(maxTime)/float64(time.Millisecond), 2.0, 
			"Max verification time should be < 2ms")
	})

	t.Run("Concurrent Verification", func(t *testing.T) {
		numConcurrent := 10
		iterations := 10
		
		// Create mock attestation data
		var attestationData [1024]byte
		attestation := &core.TEEAttestation{
			EnclaveID:   []byte("tdx-enclave-id"),
			Measurement: attestationData[:48], // Use first 48 bytes as measurement
			Timestamp:   time.Now(),
			Data:        attestationData[:],
		}
		
		executor := NewTestExecutor(t)
		
		// Run verification concurrently
		results := make(chan time.Duration, numConcurrent*iterations)
		
		for i := 0; i < numConcurrent; i++ {
			go func(id int) {
				for j := 0; j < iterations; j++ {
					start := time.Now()
					_, err := executor.VerifyTDXAttestation(attestation)
					duration := time.Since(start)
					
					// If verification fails, report a very long duration
					if err != nil {
						t.Logf("Verification failed in goroutine %d, iteration %d: %v", 
							id, j, err)
						results <- 1000 * time.Millisecond
					} else {
						results <- duration
					}
				}
			}(i)
		}
		
		// Collect and analyze results
		var durations []time.Duration
		for i := 0; i < numConcurrent*iterations; i++ {
			durations = append(durations, <-results)
		}
		
		// Calculate statistics
		var totalTime time.Duration
		var maxTime time.Duration
		
		for _, duration := range durations {
			totalTime += duration
			if duration > maxTime {
				maxTime = duration
			}
		}
		
		avgTime := totalTime / time.Duration(len(durations))
		
		// Log results
		t.Logf("Concurrent TDX Verification Performance:")
		t.Logf("  Average: %v", avgTime)
		t.Logf("  Max: %v", maxTime)
		t.Logf("  Concurrent routines: %d", numConcurrent)
		t.Logf("  Iterations per routine: %d", iterations)
		
		// Assert performance for concurrent execution
		assert.Less(t, avgTime, 2*time.Millisecond, 
			"Average concurrent verification time should be < 2ms")
	})
}

// Use the shared test helpers from test_helpers.go

// Test verification is implemented in test_helpers.go
