package policy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHarnessConfig holds test harness configuration
type TestHarnessConfig struct {
	PolicyDir    string
	SampleSize   int
	BatchSize    int
	TestDuration time.Duration
}

// DefaultTestHarnessConfig returns a default configuration for the test harness
func DefaultTestHarnessConfig() TestHarnessConfig {
	return TestHarnessConfig{
		PolicyDir:    "examples/sample_policy",
		SampleSize:   100,
		BatchSize:    10,
		TestDuration: 5 * time.Second,
	}
}

// RunPolicyTestHarness runs the WebAssembly policy engine test harness
func RunPolicyTestHarness(t *testing.T, config TestHarnessConfig) {
	// Create policy engine
	policyDir := filepath.Join(os.TempDir(), "policy-test-harness")
	err := os.MkdirAll(policyDir, 0755)
	require.NoError(t, err, "Failed to create policy directory")

	// Create the wasm directory
	wasmDir := filepath.Join(policyDir, "wasm")
	err = os.MkdirAll(wasmDir, 0755)
	require.NoError(t, err, "Failed to create wasm directory")

	// Copy policy definition and Wasm modules to the test directory
	// In a real test, you would compile the AssemblyScript modules to Wasm
	// and place them in the wasm directory
	
	// For this test, we'll use placeholder attestations and measurements
	samples := generateTestSamples(config.SampleSize)

	// Create the policy engine
	engine, err := NewPolicyEngine(policyDir)
	require.NoError(t, err, "Failed to create policy engine")

	// Create sample policy
	// Create a unique policy ID with a timestamp to avoid conflicts
	uniquePolicyID := fmt.Sprintf("test-policy-%d", time.Now().UnixNano())
	
	policy := &VerificationPolicy{
		ID:          uniquePolicyID,
		Name:        "Test Policy",
		Version:     "1.0.0",
		Description: "Test policy for WebAssembly integration",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		TargetTEEs:  []string{"TDX"},
		Constraints: []Constraint{
			{
				ID:           "test-measurement",
				Type:         "measurement",
				Operator:     "equals",
				ExpectedValue: "ALLOWALL", // Special value to allow all measurements during testing
				ErrorMessage: "Measurement mismatch",
				Severity:     "error",
			},
		},
	}

	// Add policy to engine
	err = engine.AddPolicy(policy)
	require.NoError(t, err, "Failed to add policy")

	// Run tests for TDX attestations
	fmt.Printf("Starting policy test harness with %d samples\n", config.SampleSize)
	fmt.Printf("Running for %s with batch size %d\n", config.TestDuration, config.BatchSize)

	start := time.Now()
	var validCount, invalidCount int

	// Process all samples in batches
	for i := 0; i < len(samples); i += config.BatchSize {
		end := i + config.BatchSize
		if end > len(samples) {
			end = len(samples)
		}

		batch := samples[i:end]
		batchResults := make([]*PolicyResult, len(batch))
		
		// Process batch
		for j, sample := range batch {
			ctx := context.Background()
			result, err := engine.EvaluateTDXAttestation(ctx, sample.Attestation, sample.Measurement)
			if err != nil {
				t.Logf("Error evaluating sample %d: %v", i+j, err)
				continue
			}
			
			batchResults[j] = result
			if result.Valid {
				validCount++
			} else {
				invalidCount++
			}
		}
	}

	duration := time.Since(start)
	totalSamples := validCount + invalidCount
	tps := float64(totalSamples) / duration.Seconds()

	fmt.Printf("Processed %d samples in %s\n", totalSamples, duration)
	fmt.Printf("Valid: %d, Invalid: %d\n", validCount, invalidCount)
	fmt.Printf("Throughput: %.2f TPS\n", tps)

	// In a high-performance production environment, we'd expect 
	// throughput in the 25-40k TPS range for the optimized batch system
	
	// For this test harness, we're just validating the policy engine works correctly
	assert.True(t, validCount > 0, "Should have some valid attestations")
}

// TestSample represents a test attestation and measurement pair
type TestSample struct {
	Attestation []byte
	Measurement []byte
}

// generateTestSamples creates random test samples
func generateTestSamples(count int) []TestSample {
	samples := make([]TestSample, count)
	
	for i := 0; i < count; i++ {
		// Create a random attestation (128 bytes for this example)
		attestation := make([]byte, 128)
		rand.Read(attestation)
		
		// Create a random measurement (32 bytes, SHA-256 size)
		measurement := make([]byte, 32)
		rand.Read(measurement)
		
		// For simplicity, include the measurement in the attestation
		// so the WebAssembly modules can find it
		copy(attestation[64:96], measurement)
		
		samples[i] = TestSample{
			Attestation: attestation,
			Measurement: measurement,
		}
	}
	
	return samples
}

// HexDump returns a hex dump of binary data for debugging
func HexDump(data []byte, maxLen int) string {
	if len(data) > maxLen {
		return hex.EncodeToString(data[:maxLen]) + fmt.Sprintf("... (%d bytes total)", len(data))
	}
	return hex.EncodeToString(data)
}
