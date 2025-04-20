package accumulator

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDualAccumulatorIntegration tests the integrated Go and Rust accumulator approach
func TestDualAccumulatorIntegration(t *testing.T) {
	// Skip in short mode
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}
	
	// Load the WebAssembly binary
	wasmPath := getWasmPath(t)
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("Failed to read WebAssembly binary: %v", err)
	}
	
	// Create WebAssembly interface for SGX
	sgxInterface, err := NewWasmTeeInterface("sgx-node-1", "SGX", "us-east-1", wasmBytes)
	if err != nil {
		t.Fatalf("Failed to create SGX WebAssembly interface: %v", err)
	}
	defer sgxInterface.Close()
	
	// Create WebAssembly interface for SEV
	sevInterface, err := NewWasmTeeInterface("sev-node-1", "SEV", "us-east-1", wasmBytes)
	if err != nil {
		t.Fatalf("Failed to create SEV WebAssembly interface: %v", err)
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
	
	// Test attestation registration and verification
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	// Generate test attestations
	sgxAttestation := generateTestAttestation(t, "SGX")
	sevAttestation := generateTestAttestation(t, "SEV")
	
	// Register attestations
	t.Log("Registering SGX attestation...")
	sgxSuccess, err := sgxConnector.RegisterAttestation(ctx, sgxAttestation)
	if err != nil {
		t.Fatalf("Failed to register SGX attestation: %v", err)
	}
	if !sgxSuccess {
		t.Fatal("SGX attestation registration failed")
	}
	
	t.Log("Registering SEV attestation...")
	sevSuccess, err := sevConnector.RegisterAttestation(ctx, sevAttestation)
	if err != nil {
		t.Fatalf("Failed to register SEV attestation: %v", err)
	}
	if !sevSuccess {
		t.Fatal("SEV attestation registration failed")
	}
	
	// Verify cross-attestation
	t.Log("Verifying cross-attestation...")
	valid, err := sgxConnector.VerifyAttestationWithCrossCheck(ctx, sgxAttestation, sevAttestation)
	if err != nil {
		t.Fatalf("Cross-attestation verification failed: %v", err)
	}
	if !valid {
		t.Fatal("Cross-attestation verification returned invalid")
	}
	
	// Get performance stats
	sgxStats := sgxConnector.GetPerformanceStats()
	t.Logf("SGX Performance Stats: %+v", sgxStats)
	
	sevStats := sevConnector.GetPerformanceStats()
	t.Logf("SEV Performance Stats: %+v", sevStats)
	
	// Batch test with multiple attestations
	batchSize := 5
	t.Logf("Testing batch verification with %d attestations...", batchSize)
	
	// Generate batch attestations
	attestations := make([]struct {
		SGXTEE []byte
		SEVTEE []byte
	}, batchSize)
	
	for i := 0; i < batchSize; i++ {
		attestations[i].SGXTEE = generateTestAttestation(t, fmt.Sprintf("SGX-Batch-%d", i))
		attestations[i].SEVTEE = generateTestAttestation(t, fmt.Sprintf("SEV-Batch-%d", i))
		
		// Register each attestation
		_, err := sgxConnector.RegisterAttestation(ctx, attestations[i].SGXTEE)
		if err != nil {
			t.Fatalf("Failed to register batch SGX attestation %d: %v", i, err)
		}
		
		_, err = sevConnector.RegisterAttestation(ctx, attestations[i].SEVTEE)
		if err != nil {
			t.Fatalf("Failed to register batch SEV attestation %d: %v", i, err)
		}
	}
	
	// Batch verify
	results, err := sgxConnector.BatchVerifyAttestations(ctx, attestations)
	if err != nil {
		t.Fatalf("Batch verification failed: %v", err)
	}
	
	// Check results
	for id, valid := range results {
		if !valid {
			t.Errorf("Attestation %s was not valid", id)
		}
	}
	
	// Print final performance stats
	finalStats := sgxConnector.GetPerformanceStats()
	t.Logf("Final Performance Stats: %+v", finalStats)
	
	// Extrapolate to 50,000 TPS
	if tps, ok := finalStats["overall_tps"].(float64); ok {
		extrapolatedNodes := int(50000 / tps)
		t.Logf("To achieve 50,000+ TPS, approximately %d nodes would be needed", extrapolatedNodes)
		t.Logf("Our current implementation achieves %.2f TPS per node", tps)
	}
}

// getWasmPath retrieves the path to the WebAssembly binary
func getWasmPath(t *testing.T) string {
	// Try common locations
	paths := []string{
		"../execution/accumulator/build/accumulator.wasm",
		"../../execution/accumulator/build/accumulator.wasm",
		"../execution/build/accumulator.wasm",
		"/tmp/accumulator.wasm", // For CI environments
	}
	
	// Check if any path exists
	for _, path := range paths {
		absPath, err := filepath.Abs(path)
		if err == nil {
			if _, err := os.Stat(absPath); err == nil {
				return absPath
			}
		}
	}
	
	// For testing purposes, if not found, skip the test
	t.Skip("WebAssembly binary not found. Skipping integration test.")
	return ""
}

// generateTestAttestation generates a test attestation
func generateTestAttestation(t *testing.T, prefix string) []byte {
	// Create a realistic-sized attestation (approx 1KB)
	attestation := make([]byte, 1024)
	
	// Fill with random data
	if _, err := rand.Read(attestation); err != nil {
		t.Fatalf("Failed to generate random data: %v", err)
	}
	
	// Set prefix bytes for identification
	prefixBytes := []byte(prefix)
	copy(attestation[:len(prefixBytes)], prefixBytes)
	
	return attestation
}

// TestHighThroughputCrossRegional tests the system with a high throughput cross-regional workload
func TestHighThroughputCrossRegional(t *testing.T) {
	// Skip in short mode
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}
	
	// Load the WebAssembly binary
	wasmPath := getWasmPath(t)
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("Failed to read WebAssembly binary: %v", err)
	}
	
	// Create WebAssembly interfaces for multiple regions
	regions := []string{"us-east-1", "us-west-2", "eu-west-1", "ap-southeast-1"}
	connectors := make([]*TeeConnector, 0, len(regions)*2) // 2 for SGX and SEV per region
	
	// Create a connector for each TEE type in each region
	for _, region := range regions {
		for _, teeType := range []string{"SGX", "SEV"} {
			teeID := fmt.Sprintf("%s-%s-node", teeType, region)
			
			// Create interface
			teeInterface, err := NewWasmTeeInterface(teeID, teeType, region, wasmBytes)
			if err != nil {
				t.Fatalf("Failed to create %s interface for %s: %v", teeType, region, err)
			}
			
			// Create connector with optimized options
			opts := DefaultTeeConnectorOptions()
			opts.RegionID = region
			opts.BatchSize = 1000 // Larger batch size for high throughput
			
			connector, err := NewTeeConnector(teeInterface, opts)
			if err != nil {
				t.Fatalf("Failed to create connector: %v", err)
			}
			
			connectors = append(connectors, connector)
			// Close the interface when finished
			defer teeInterface.Close()
			defer connector.Close()
		}
	}
	
	// Test high-throughput cross-regional verification
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	
	// Generate a set of test attestations
	attestationCount := 100 // Number of attestations for testing
	t.Logf("Generating %d test attestations...", attestationCount)
	
	attestations := make([]struct {
		SGXTEE []byte
		SEVTEE []byte
	}, attestationCount)
	
	for i := 0; i < attestationCount; i++ {
		attestations[i].SGXTEE = generateTestAttestation(t, fmt.Sprintf("SGX-HT-%d", i))
		attestations[i].SEVTEE = generateTestAttestation(t, fmt.Sprintf("SEV-HT-%d", i))
	}
	
	// Register attestations in all regions (cross-regional registration)
	t.Log("Registering attestations across all regions...")
	for _, connector := range connectors {
		for i := 0; i < attestationCount; i++ {
			teeType := connector.wasmInterface.GetTeeType()
			
			var attestation []byte
			if teeType == "SGX" {
				attestation = attestations[i].SGXTEE
			} else {
				attestation = attestations[i].SEVTEE
			}
			
			_, err := connector.RegisterAttestation(ctx, attestation)
			if err != nil {
				t.Fatalf("Failed to register attestation in %s: %v", connector.region, err)
			}
		}
	}
	
	// Perform cross-regional verification
	t.Log("Performing cross-regional verification...")
	
	// Take the first connector for each TEE type for verification
	var sgxConnector, sevConnector *TeeConnector
	for _, conn := range connectors {
		if conn.wasmInterface.GetTeeType() == "SGX" && sgxConnector == nil {
			sgxConnector = conn
		} else if conn.wasmInterface.GetTeeType() == "SEV" && sevConnector == nil {
			sevConnector = conn
		}
		
		if sgxConnector != nil && sevConnector != nil {
			break
		}
	}
	
	if sgxConnector == nil || sevConnector == nil {
		t.Fatal("Failed to get connectors for both TEE types")
	}
	
	// Perform batch verification on each connector
	sgxResults, err := sgxConnector.BatchVerifyAttestations(ctx, attestations)
	if err != nil {
		t.Fatalf("SGX batch verification failed: %v", err)
	}
	
	sevResults, err := sevConnector.BatchVerifyAttestations(ctx, attestations)
	if err != nil {
		t.Fatalf("SEV batch verification failed: %v", err)
	}
	
	// Analyze results
	sgxValid, sevValid := 0, 0
	for _, valid := range sgxResults {
		if valid {
			sgxValid++
		}
	}
	
	for _, valid := range sevResults {
		if valid {
			sevValid++
		}
	}
	
	t.Logf("SGX verification results: %d/%d valid", sgxValid, attestationCount)
	t.Logf("SEV verification results: %d/%d valid", sevValid, attestationCount)
	
	// Get performance stats from all connectors
	t.Log("Performance statistics across regions:")
	
	var totalTPS float64
	for _, connector := range connectors {
		stats := connector.GetPerformanceStats()
		teeType := connector.wasmInterface.GetTeeType()
		region := connector.region
		
		if tps, ok := stats["overall_tps"].(float64); ok {
			totalTPS += tps
			t.Logf("%s in %s: %.2f TPS", teeType, region, tps)
		}
	}
	
	t.Logf("Total system throughput: %.2f TPS", totalTPS)
	t.Logf("Estimated throughput with 40 node pairs: %.2f TPS", totalTPS*40/float64(len(connectors)))
}
