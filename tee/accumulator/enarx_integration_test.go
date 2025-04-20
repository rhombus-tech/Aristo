package accumulator

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
	"time"

	pb "github.com/rhombus-tech/vm/tee/proto"
)

// TestEnarxIntegration tests the integrated dual accumulator approach using Enarx
func TestEnarxIntegration(t *testing.T) {
	// Skip if not running in integration test mode
	if os.Getenv("RUN_INTEGRATION_TESTS") != "1" {
		t.Skip("Skipping integration test. Set RUN_INTEGRATION_TESTS=1 to run")
	}
	
	// Check if Enarx binaries are available
	enarxPath := os.Getenv("ENARX_PATH")
	if enarxPath == "" {
		enarxPath = "/usr/local/bin/enarx"
	}
	
	if _, err := os.Stat(enarxPath); os.IsNotExist(err) {
		t.Skipf("Enarx not found at %s. Skipping integration test", enarxPath)
	}
	
	// Set up Enarx simulation mode for testing without actual TEE hardware
	os.Setenv("ENARX_SIMULATION", "1")
	defer os.Unsetenv("ENARX_SIMULATION")
	
	// Load the WebAssembly binary
	wasmPath := os.Getenv("ACCUMULATOR_WASM_PATH")
	if wasmPath == "" {
		t.Skip("ACCUMULATOR_WASM_PATH not set. Skipping integration test")
	}
	
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("Failed to read WebAssembly binary: %v", err)
	}
	
	// Create Enarx interfaces for SGX and SEV
	sgxInterface, err := NewEnarxTeeInterface("sgx-test-1", "SGX", "us-east-1", wasmBytes)
	if err != nil {
		t.Fatalf("Failed to create SGX Enarx interface: %v", err)
	}
	defer sgxInterface.Close()
	
	sevInterface, err := NewEnarxTeeInterface("sev-test-1", "SEV", "us-east-1", wasmBytes)
	if err != nil {
		t.Fatalf("Failed to create SEV Enarx interface: %v", err)
	}
	defer sevInterface.Close()
	
	// Create TeeConnector for SGX
	sgxConnector, err := NewTeeConnector(sgxInterface, DefaultTeeConnectorOptions())
	if err != nil {
		t.Fatalf("Failed to create SGX connector: %v", err)
	}
	defer sgxConnector.Close()
	
	// Create TeeConnector for SEV
	sevConnector, err := NewTeeConnector(sevInterface, DefaultTeeConnectorOptions())
	if err != nil {
		t.Fatalf("Failed to create SEV connector: %v", err)
	}
	defer sevConnector.Close()
	
	// Test dual accumulator approach with Enarx
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	
	// Generate test attestations
	sgxAttestation := generateEnarxAttestation(t, "SGX")
	sevAttestation := generateEnarxAttestation(t, "SEV")
	
	// Register attestations
	t.Log("Registering SGX attestation...")
	sgxResult, err := sgxConnector.RegisterAttestation(ctx, sgxAttestation)
	if err != nil {
		t.Fatalf("Failed to register SGX attestation: %v", err)
	}
	if !sgxResult {
		t.Fatal("SGX attestation registration returned false")
	}
	
	t.Log("Registering SEV attestation...")
	sevResult, err := sevConnector.RegisterAttestation(ctx, sevAttestation)
	if err != nil {
		t.Fatalf("Failed to register SEV attestation: %v", err)
	}
	if !sevResult {
		t.Fatal("SEV attestation registration returned false")
	}
	
	// Verify cross-attestation (this uses both accumulators)
	t.Log("Verifying cross-attestation with dual accumulator approach...")
	// Extract attestation data from witnesses for verification
	sgxAttestationData := sgxAttestation
	sevAttestationData := sevAttestation
	valid, err := sgxConnector.VerifyAttestationWithCrossCheck(ctx, sgxAttestationData, sevAttestationData)
	if err != nil {
		t.Fatalf("Cross-attestation verification failed: %v", err)
	}
	if !valid {
		t.Fatal("Cross-attestation verification returned invalid")
	}
	
	// Performance benchmarking
	t.Log("Running performance benchmark...")
	batchSize := 100
	attestations := make([]struct {
		SGXTEE []byte
		SEVTEE []byte
	}, batchSize)
	
	// Generate and register batch attestations
	for i := 0; i < batchSize; i++ {
		attestations[i].SGXTEE = generateEnarxAttestation(t, fmt.Sprintf("SGX-Batch-%d", i))
		attestations[i].SEVTEE = generateEnarxAttestation(t, fmt.Sprintf("SEV-Batch-%d", i))
		
		_, err := sgxConnector.RegisterAttestation(ctx, attestations[i].SGXTEE)
		if err != nil {
			t.Fatalf("Failed to register batch SGX attestation %d: %v", i, err)
		}
		
		_, err = sevConnector.RegisterAttestation(ctx, attestations[i].SEVTEE)
		if err != nil {
			t.Fatalf("Failed to register batch SEV attestation %d: %v", i, err)
		}
	}
	
	// Measure batch verification performance
	startTime := time.Now()
	results, err := sgxConnector.BatchVerifyAttestations(ctx, attestations)
	if err != nil {
		t.Fatalf("Batch verification failed: %v", err)
	}
	elapsedTime := time.Since(startTime)
	
	// Calculate TPS
	tps := float64(batchSize) / elapsedTime.Seconds()
	t.Logf("Batch verification performance: %.2f TPS", tps)
	
	// Validate results
	successCount := 0
	for _, valid := range results {
		if valid {
			successCount++
		}
	}
	t.Logf("Successfully verified %d/%d attestations", successCount, batchSize)
	
	// Compare with our high-performance requirements
	t.Logf("Target TPS: 50,000+")
	t.Logf("Current TPS: %.2f", tps)
	t.Logf("Projected TPS with 40 nodes: %.2f", tps*40)
	
	// Get final performance stats
	sgxStats := sgxConnector.GetPerformanceStats()
	t.Logf("SGX Performance Stats: %+v", sgxStats)
	
	sevStats := sevConnector.GetPerformanceStats()
	t.Logf("SEV Performance Stats: %+v", sevStats)
	
	// Validate Enarx integration
	enarxSgxStats := sgxInterface.GetPerformanceStats()
	t.Logf("Enarx SGX Stats: %+v", enarxSgxStats)
	
	enarxSevStats := sevInterface.GetPerformanceStats()
	t.Logf("Enarx SEV Stats: %+v", enarxSevStats)
}

// For testing purposes only - helper functions for the test

// createTestWitness creates a witness with the given attestation for testing
func createTestWitness(attestation []byte, teeID, teeType string) *pb.AccumulatorWitness {
	// Hash the attestation
	attestationHash := sha256.Sum256(attestation)
	
	// Create an accumulator element
	element := &pb.AccumulatorElement{
		Executor:    teeID,
		Measurement: attestationHash[:],
		EnclaveType: teeType,
		Timestamp:   uint64(time.Now().Unix()),
	}
	
	// Create a witness for this element
	witness := &pb.AccumulatorWitness{
		Element:        element,
		Value:          []byte{1, 2, 3, 4}, // Simplified witness value for testing
		LastAccumulator: []byte{5, 6, 7, 8},
		LastUpdate:     uint64(time.Now().Unix()),
	}
	
	return witness
}

// generateEnarxAttestation generates a test attestation for Enarx integration testing
func generateEnarxAttestation(t *testing.T, prefix string) []byte {
	// Create a realistic-sized attestation (approx 1KB)
	attestation := make([]byte, 1024)
	
	// Fill with random data
	if _, err := rand.Read(attestation); err != nil {
		t.Fatalf("Failed to generate random data: %v", err)
	}
	
	// Set prefix bytes for identification
	prefixBytes := []byte("enarx-" + prefix)
	copy(attestation[:len(prefixBytes)], prefixBytes)
	
	return attestation
}
