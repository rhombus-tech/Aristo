package policy

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPolicyEngine(t *testing.T) {
	// Create temporary policy directory
	tempDir := filepath.Join(os.TempDir(), "tdx-policy-test")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)
	
	// Create default policies
	if err := CreateDefaultPolicies(tempDir); err != nil {
		t.Fatalf("Failed to create default policies: %v", err)
	}
	
	// Create policy engine
	engine, err := NewPolicyEngine(tempDir)
	if err != nil {
		t.Fatalf("Failed to create policy engine: %v", err)
	}
	
	// Verify the default policies were loaded
	if len(engine.policies) != 2 {
		t.Errorf("Expected 2 policies, got %d", len(engine.policies))
	}
	
	// Create a policy verifier
	verifier := NewPolicyVerifierWithEngine(engine)
	
	// Create test data
	fakeAttestation := []byte("fake-tdx-attestation-data")
	fakeMeasurement := []byte("fake-tdx-measurement-data")
	
	// Test TDX attestation verification
	result, err := verifier.VerifyTDXAttestation(context.Background(), fakeAttestation, fakeMeasurement, nil)
	if err != nil {
		t.Fatalf("Failed to verify TDX attestation: %v", err)
	}
	
	// Since we're using the special "ALLOWALL" value, it should succeed
	if !result.Valid {
		t.Errorf("Expected attestation to be valid, but it was invalid: %+v", result)
	}
	
	// Verify batch processing
	attestations := map[string][]byte{
		"test-1": fakeAttestation,
		"test-2": fakeAttestation,
	}
	
	measurements := map[string][]byte{
		"test-1": fakeMeasurement,
		"test-2": fakeMeasurement,
	}
	
	batchResult, err := verifier.BatchVerifyTDXAttestations(context.Background(), attestations, measurements, nil)
	if err != nil {
		t.Fatalf("Failed to batch verify TDX attestations: %v", err)
	}
	
	if batchResult.SuccessCount != 2 {
		t.Errorf("Expected 2 successful verifications, got %d", batchResult.SuccessCount)
	}
	
	if batchResult.FailureCount != 0 {
		t.Errorf("Expected 0 failed verifications, got %d", batchResult.FailureCount)
	}
}

// TestPolicyIntegrationWithBatchVerification tests integration with the batch verification system
func TestPolicyIntegrationWithBatchVerification(t *testing.T) {
	t.Skip("Integration test - requires TDX batch verification implementation")
	
	// This is a skeleton for how to integrate with the batch verification system
	// The actual implementation would call into the TDX batch verification
	/*
	// Set up policy engine
	policyDir := filepath.Join(os.TempDir(), "tdx-policy-integration-test")
	if err := os.MkdirAll(policyDir, 0755); err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(policyDir)
	
	if err := CreateDefaultPolicies(policyDir); err != nil {
		t.Fatalf("Failed to create default policies: %v", err)
	}
	
	// Create policy verifier
	verifier, err := NewPolicyVerifier()
	if err != nil {
		t.Fatalf("Failed to create policy verifier: %v", err)
	}
	
	// Create a batch of TDX attestations (would come from batch_verification.go)
	batchSize := 10
	attestations := make([]TDXQuote, batchSize)
	for i := 0; i < batchSize; i++ {
		attestations[i] = createTestTDXQuote()
	}
	
	// Extract measurements and prepare for verification
	attestationMap := make(map[string][]byte)
	measurementMap := make(map[string][]byte)
	
	for i, quote := range attestations {
		id := fmt.Sprintf("tdx-quote-%d", i)
		attestationMap[id] = quote.RawQuote
		measurementMap[id] = extractMeasurementFromTDXQuote(quote)
	}
	
	// Verify using policy engine
	options := &VerificationOptions{
		EnforcePolicy: true,
		TEEType:       "TDX",
		Timeout:       1 * time.Second,
	}
	
	batchResult, err := verifier.BatchVerifyTDXAttestations(context.Background(), attestationMap, measurementMap, options)
	if err != nil {
		t.Fatalf("Batch verification failed: %v", err)
	}
	
	// Check results
	if batchResult.SuccessCount != batchSize {
		t.Errorf("Expected %d successful verifications, got %d", batchSize, batchResult.SuccessCount)
	}
	
	// In integration with the batch verification system, you would:
	// 1. Extract measurements from quotes in batch_verification.go
	// 2. Call policy verifier as part of the verification process
	// 3. Use the policy results to determine final verification status
	*/
}

// Example of a WebAssembly constraint handler function in Go
func verifyTradingCapacity(ctx context.Context, measurement []byte, maxTradesPerMinute int) (bool, string) {
	// In a real implementation, this would:
	// 1. Parse the measurement to extract the trading capacity metadata
	// 2. Verify it against the specified max trades per minute
	// 3. Return validation result and any error message
	
	// For testing, always succeed
	return true, ""
}

// Example of a wasm export function - in actual implementation this would be in the WebAssembly module
//export verify_trading_capacity
func verify_trading_capacity(measurementPtr int32, measurementLen int32, maxTradesPtr int32, maxTradesLen int32) int32 {
	// This is just a placeholder for documentation
	// The actual implementation would be in WebAssembly
	
	// Process would be:
	// 1. Extract memory from WebAssembly instance
	// 2. Read measurement bytes from memory at measurementPtr
	// 3. Read max trades configuration from memory
	// 4. Perform validation
	// 5. Return 1 for success, 0 for failure
	
	return 1 // Success
}
