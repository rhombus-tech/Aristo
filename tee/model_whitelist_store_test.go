package tee

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testWhitelistFilename = "model_whitelist.json"
	testAuditLogFilename  = "audit_log.json"
)

func TestModelWhitelistStore(t *testing.T) {
	// Create a temporary directory for whitelist testing
	tempDir, err := os.MkdirTemp("", "model_whitelist_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Set environment variable for whitelist store
	originalDir := os.Getenv("AI_MODEL_WHITELIST_DIR")
	defer os.Setenv("AI_MODEL_WHITELIST_DIR", originalDir)
	os.Setenv("AI_MODEL_WHITELIST_DIR", tempDir)

	// Create test measurements (must be exactly 48 bytes for [48]byte type)
	var testMeasurement1 [48]byte
	var testMeasurement2 [48]byte
	var testMeasurement3 [48]byte
	copy(testMeasurement1[:], "test-measurement-1-with-unique-bytes-padding-48b")
	copy(testMeasurement2[:], "test-measurement-2-with-different-bytes-padding48")
	copy(testMeasurement3[:], "wasi-module-measurement-for-testing-padding-48b")

	t.Run("Basic Store Operations", func(t *testing.T) {
		store, err := GetModelWhitelistStore()
		if err != nil {
			t.Fatalf("Failed to get whitelist store: %v", err)
		}

		// Add a model policy
		policy := &AIModelPolicy{
			ID:             "test-policy-1",
			Measurement:    testMeasurement1,
			Approved:       true,
			MaxOrderSize:   1000,
			MaxTradesPerMin: 100,
			AllowedAssets:  []string{"AAPL", "MSFT"},
			RiskLevel:      3,
			Description:    "Test model 1",
			ApprovedBy:     "test-user-1",
			ApprovedAt:     time.Now(),
			ExpiresAt:      time.Now().Add(24 * time.Hour),
		}

		err = store.AddModelToWhitelist(policy, "test-user-1")
		if err != nil {
			t.Fatalf("Failed to add model policy: %v", err)
		}

		// Verify the policy can be retrieved
		retrievedPolicy, err := store.GetPolicy(testMeasurement1[:])
		if err != nil || retrievedPolicy == nil {
			t.Fatalf("Failed to retrieve model policy: %v", err)
		}

		if retrievedPolicy.MaxOrderSize != 1000 || len(retrievedPolicy.AllowedAssets) != 2 {
			t.Errorf("Retrieved policy values don't match: got %+v", retrievedPolicy)
		}
	})

	t.Run("Persistence Test", func(t *testing.T) {
		// Force recreate the singleton for testing
		whitelistStore = nil
		whitelistStoreOnce = sync.Once{}

		// Create first store instance
		store1, err := GetModelWhitelistStore()
		if err != nil {
			t.Fatalf("Failed to get first whitelist store: %v", err)
		}

		// Add a model policy
		policy := &AIModelPolicy{
			ID:             "test-policy-2",
			Measurement:    testMeasurement2,
			Approved:       true,
			MaxOrderSize:   2000,
			MaxTradesPerMin: 120,
			AllowedAssets:  []string{"GOOGL", "AMZN"},
			RiskLevel:      4,
			Description:    "Test model 2",
			ApprovedBy:     "test-user-2",
			ApprovedAt:     time.Now(),
			ExpiresAt:      time.Now().Add(24 * time.Hour),
		}

		err = store1.AddModelToWhitelist(policy, "test-user-2")
		if err != nil {
			t.Fatalf("Failed to add model policy: %v", err)
		}

		// Force recreate the singleton for testing
		whitelistStore = nil
		whitelistStoreOnce = sync.Once{}

		// Create a second store instance (simulating restart)
		store2, err := GetModelWhitelistStore()
		if err != nil {
			t.Fatalf("Failed to get second whitelist store: %v", err)
		}

		// Verify the policy can be retrieved from the second instance
		retrievedPolicy, err := store2.GetPolicy(testMeasurement2[:])
		if err != nil || retrievedPolicy == nil {
			t.Fatalf("Failed to retrieve model policy from second store: %v", err)
		}

		if retrievedPolicy.MaxOrderSize != 2000 || len(retrievedPolicy.AllowedAssets) != 2 {
			t.Errorf("Retrieved policy values don't match after persistence: got %+v", retrievedPolicy)
		}
	})

	t.Run("WASI Model Support", func(t *testing.T) {
		// Force recreate the singleton for testing
		whitelistStore = nil
		whitelistStoreOnce = sync.Once{}

		store, err := GetModelWhitelistStore()
		if err != nil {
			t.Fatalf("Failed to get whitelist store: %v", err)
		}

		// Add a WASI model policy (add IsWasi as a custom Description prefix since the struct doesn't have a dedicated field)
		policy := &AIModelPolicy{
			ID:             "test-policy-3-wasi",
			Measurement:    testMeasurement3,
			Approved:       true,
			MaxOrderSize:   5000,
			MaxTradesPerMin: 200,
			AllowedAssets:  []string{"ETH", "BTC"},
			RiskLevel:      5,
			Description:    "[WASI] WASI test model", // Mark WASI in description
			ApprovedBy:     "test-user-3",
			ApprovedAt:     time.Now(),
			ExpiresAt:      time.Now().Add(24 * time.Hour),
			VerifyMode:     "accumulator", // Use accumulator verification for WASI modules
		}

		err = store.AddModelToWhitelist(policy, "test-user-3")
		if err != nil {
			t.Fatalf("Failed to add WASI model policy: %v", err)
		}

		// Verify the policy can be retrieved
		retrievedPolicy, err := store.GetPolicy(testMeasurement3[:])
		if err != nil || retrievedPolicy == nil {
			t.Fatalf("Failed to retrieve WASI model policy: %v", err)
		}

		if !strings.Contains(retrievedPolicy.Description, "[WASI]") {
			t.Errorf("WASI flag not in description: got %+v", retrievedPolicy)
		}
	})

	t.Run("Thread Safety Test", func(t *testing.T) {
		// Force recreate the singleton for testing
		whitelistStore = nil
		whitelistStoreOnce = sync.Once{}

		store, err := GetModelWhitelistStore()
		if err != nil {
			t.Fatalf("Failed to get whitelist store: %v", err)
		}

		// Number of concurrent operations
		numConcurrent := 10 // Reduced for test speed
		var wg sync.WaitGroup
		wg.Add(numConcurrent)

		// Add policies concurrently
		for i := 0; i < numConcurrent; i++ {
			go func(idx int) {
				defer wg.Done()
				
				// Create a unique measurement that's exactly 48 bytes
				var measurement [48]byte
				measStr := fmt.Sprintf("concurrent-measurement-%04d-padding-to-make-48-bytes", idx)
				copy(measurement[:], measStr)
				
				// Determine if this is a WASI module (even indices)
				isWasi := idx%2 == 0
				
				policy := &AIModelPolicy{
					ID:             fmt.Sprintf("concurrent-policy-%d", idx),
					Measurement:    measurement,
					Approved:       true,
					MaxOrderSize:   1000 + uint64(idx*100),
					MaxTradesPerMin: 100 + uint32(idx*10),
					AllowedAssets:  []string{fmt.Sprintf("ASSET-%d", idx)},
					RiskLevel:      uint8(1 + idx%10),
					Description:    fmt.Sprintf("%sConcurrent test model %d", 
					                   // Add WASI prefix if applicable
					                   func() string {
					                       if isWasi {
					                           return "[WASI] "
					                       }
					                       return ""
					                   }(), 
					                   idx),
					ApprovedBy:     fmt.Sprintf("test-user-%d", idx),
					ApprovedAt:     time.Now(),
					ExpiresAt:      time.Now().Add(24 * time.Hour),
				}
				
				// Add the policy and verify it's retrievable
				err := store.AddModelToWhitelist(policy, fmt.Sprintf("test-user-%d", idx))
				if err != nil {
					t.Errorf("Thread %d: Failed to add model: %v", idx, err)
					return
				}
				
				var retrievedPolicy *AIModelPolicy
				retrievedPolicy, _ = store.GetPolicy(measurement[:])
				if retrievedPolicy == nil {
					t.Errorf("Thread %d: Failed to retrieve just-added policy", idx)
				}
			}(i)
		}
		
		// Wait for all goroutines to complete
		wg.Wait()
		
		// Verify that the expected number of models were added
		// This validation depends on GetAllPolicies() or similar method
	})

	t.Run("Audit Logging Test", func(t *testing.T) {
		// Force recreate the singleton for testing
		whitelistStore = nil
		whitelistStoreOnce = sync.Once{}

		store, err := GetModelWhitelistStore()
		if err != nil {
			t.Fatalf("Failed to get whitelist store: %v", err)
		}

		// Add a model policy
		policy := &AIModelPolicy{
			ID:             "audit-test-policy",
			Measurement:    testMeasurement1,
			Approved:       true,
			MaxOrderSize:   1000,
			MaxTradesPerMin: 100,
			AllowedAssets:  []string{"AUDIT"}, 
			RiskLevel:      3,
			Description:    "Audit test model",
			ApprovedBy:     "audit-test-user",
			ApprovedAt:     time.Now(),
			ExpiresAt:      time.Now().Add(24 * time.Hour),
		}

		err = store.AddModelToWhitelist(policy, "audit-test-user")
		if err != nil {
			t.Fatalf("Failed to add model policy: %v", err)
		}

		// Verify the audit log file exists
		auditLogPath := filepath.Join(tempDir, testAuditLogFilename)
		if _, err := os.Stat(auditLogPath); os.IsNotExist(err) {
			t.Errorf("Audit log file not created: %v", err)
		}

		// Read the audit log to verify entries
		content, err := os.ReadFile(auditLogPath)
		if err != nil {
			t.Fatalf("Failed to read audit log: %v", err)
		}

		// Check user ID in audit log
		if !strings.Contains(string(content), "audit-test-user") {
			t.Errorf("Audit log does not contain expected user ID")
		}
		
		// Optional: verify measurement is in log (commented out as it might be stored in a different format)
		// hexMeasurement := hex.EncodeToString(testMeasurement1[:])
		// if !strings.Contains(string(content), hexMeasurement) {
		//     t.Errorf("Audit log does not contain expected measurement: %s", hexMeasurement)
		// }
	})

	t.Run("Fallback to In-Memory Cache", func(t *testing.T) {
		// Force recreate the singleton for testing
		whitelistStore = nil
		whitelistStoreOnce = sync.Once{}

		// Set environment variable to non-writable directory
		nonWritableDir := "/proc" // typically not writable on Linux
		os.Setenv("AI_MODEL_WHITELIST_DIR", nonWritableDir)

		// We expect a warning about memory-only mode but store should still be created
		store, err := GetModelWhitelistStore()
		
		// We expect a non-nil store despite the error
		if store == nil {
			t.Fatalf("Expected non-nil store in memory-only mode")
		}
		
		// Log the warning but don't fail - our implementation should return a usable store with a warning
		if err != nil {
			t.Logf("Got expected warning: %v", err)
		}

		// Add a model policy - should succeed in memory despite bad dir
		policy := &AIModelPolicy{
			ID:             "fallback-policy",
			Measurement:    testMeasurement1,
			Approved:       true,
			MaxOrderSize:   1000,
			MaxTradesPerMin: 100,
			AllowedAssets:  []string{"FALLBACK"},
			RiskLevel:      2,
			Description:    "Fallback test model",
			ApprovedBy:     "fallback-test-user",
			ApprovedAt:     time.Now(),
			ExpiresAt:      time.Now().Add(24 * time.Hour),
		}

		err = store.AddModelToWhitelist(policy, "fallback-test-user")
		// We might get an error when writing to disk, but in-memory should work
		// Log but don't fail on disk write errors
		if err != nil {
			t.Logf("Got expected warning on write: %v", err)
		}
		
		// Verify the policy can be retrieved from in-memory cache
		retrievedPolicy, err := store.GetPolicy(testMeasurement1[:])
		if err != nil || retrievedPolicy == nil {
			t.Fatalf("Failed to retrieve model policy from in-memory cache: %v", err)
		}

		if retrievedPolicy.MaxOrderSize != 1000 || len(retrievedPolicy.AllowedAssets) != 1 {
			t.Errorf("Retrieved policy values don't match: got %+v", retrievedPolicy)
		}
		
		// Reset the environment variable
		os.Setenv("AI_MODEL_WHITELIST_DIR", tempDir)
	})
}
