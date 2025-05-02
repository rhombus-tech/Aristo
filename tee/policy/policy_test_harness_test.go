package policy

import (
	"testing"
)

// TestRunPolicyTestHarness tests the WebAssembly policy engine with the test harness
func TestRunPolicyTestHarness(t *testing.T) {
	// Skip if running in short mode
	if testing.Short() {
		t.Skip("Skipping policy test harness in short mode")
	}

	// Create a test harness configuration
	config := DefaultTestHarnessConfig()
	
	// Use a smaller batch size and test duration for CI environments
	config.SampleSize = 20
	config.BatchSize = 5
	config.TestDuration = 0 // No time limit, just run the samples

	// Run the policy test harness
	RunPolicyTestHarness(t, config)
}
