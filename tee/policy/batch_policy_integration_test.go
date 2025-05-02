package policy

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// generateUniqueID creates a unique identifier for test attestations
func generateUniqueID() string {
	return fmt.Sprintf("test-id-%d", time.Now().UnixNano())
}

// TestBatchPolicyIntegration validates the integration between policy engine
// and batch verification system for high-throughput TDX attestation verification

func TestBatchPolicyIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create policy engine with test policies
	policyDir := t.TempDir()
	err := CreateDefaultPolicies(policyDir)
	assert.NoError(t, err, "Failed to create default policies")

	engine, err := NewPolicyEngine(policyDir)
	assert.NoError(t, err, "Failed to create policy engine")
	
	// Create a policy verifier using the existing implementation
	verifier := NewPolicyVerifierWithEngine(engine)
	assert.NotNil(t, verifier, "Failed to create policy verifier")

	// Create test attestations
	attestations := make(map[string][]byte)
	measurements := make(map[string][]byte)
	
	// Generate some test samples
	for i := 0; i < 5; i++ {
		id := generateUniqueID()
		attestations[id] = []byte("test-attestation-data")
		measurements[id] = []byte("test-measurement-data")
	}

	// Configure verification options
	options := &VerificationOptions{
		EnforcePolicy: true,
		PolicyID:      "default-tdx-policy",
		TEEType:       "TDX",
		Timeout:       5 * time.Second,
	}

	// Verify the batch using the existing batch verification method
	ctx := context.Background()
	result, err := verifier.BatchVerifyTDXAttestations(ctx, attestations, measurements, options)
	require.NoError(t, err, "Batch verification should not error with ALLOWALL policy")
	
	// We expect this to succeed because our "ALLOWALL" policy should accept all measurements
	assert.Equal(t, len(attestations), result.SuccessCount, "All attestations should be valid")
	assert.Equal(t, 0, result.FailureCount, "No attestations should fail")
	assert.Equal(t, len(attestations), result.TotalCount, "Total count should match input attestations")

	// Calculate and report throughput from the result
	tps := float64(result.TotalCount) / result.Duration.Seconds()
	t.Logf("Batch verification throughput: %.2f TPS", tps)
	
	// Log a message if throughput is lower than target
	// The target TPS range mentioned in the planning docs is 25-40k TPS
	if tps < 1000 {
		t.Logf("Note: Production target is 25-40k TPS, test environment throughput is lower")
	}
}
