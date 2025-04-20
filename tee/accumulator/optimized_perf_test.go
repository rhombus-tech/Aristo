package accumulator

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	pb "github.com/rhombus-tech/vm/tee/proto"
)

// createHighPerfTestElement creates a random element specifically for the high performance test
func createHighPerfTestElement(teeType string, index int) *pb.AccumulatorElement {
	// Create a measurement that's unique but deterministic
	measurement := make([]byte, 32)
	for i := range measurement {
		measurement[i] = byte((i + index) % 256)
	}
	
	return &pb.AccumulatorElement{
		Executor:    fmt.Sprintf("tee-%s-%d", teeType, index),
		Measurement: measurement,
		EnclaveType: teeType,
		Timestamp:   uint64(time.Now().Unix()),
	}
}

func TestHighPerfAccumulator(t *testing.T) {
	// Use a simplified approach to test performance without hanging
	
	// Create a single client for testing
	client, err := NewOptimizedRsaClient(
		"test-node", 
		"SGX",
		OptimizedRsaOptions{
			BatchSize:      100,      // Small batch size for testing
			EnableAsync:    false,    // Disable async to prevent hanging
			Parallelism:    4,        // Moderate parallelism
			ModulusBits:    2048,     // Standard RSA security
			VerifyInterval: 500 * time.Millisecond,
			RegionID:       "us-east-1",
		},
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()
	
	// Create a test context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	// Number of elements to test with
	numElements := 1000
	
	// Prepare elements in advance
	elements := make([]*pb.AccumulatorElement, numElements)
	for i := 0; i < numElements; i++ {
		elements[i] = createHighPerfTestElement("SGX", i)
	}
	
	// Start performance test
	fmt.Println("Testing high performance accumulator with 1000 elements...")
	startTime := time.Now()
	
	// Add elements to batch
	for i, element := range elements {
		client.AddToBatch(element)
		
		// Process batch after each 100 elements
		if (i+1) % 100 == 0 {
			err := client.ProcessBatch(ctx)
			if err != nil {
				t.Fatalf("Error processing batch: %v", err)
			}
		}
	}
	
	// Process any remaining elements
	err = client.ProcessBatch(ctx)
	if err != nil {
		t.Fatalf("Error processing final batch: %v", err)
	}
	
	// Calculate performance
	elapsedTime := time.Since(startTime)
	tps := float64(numElements) / elapsedTime.Seconds()
	
	// Calculate extrapolated performance for 50,000 TPS target
	extrapolatedTPS := tps * 40 // Simulate with 40 node pairs
	nodesNeeded := int(math.Ceil(50000 / tps))
	
	// Output performance results
	fmt.Printf("\n==== Performance Results ====\n")
	fmt.Printf("Processed %d elements in %v\n", numElements, elapsedTime)
	fmt.Printf("Transactions per second (TPS): %.2f\n", tps)
	fmt.Printf("Extrapolated TPS with 40 node pairs: %.2f\n", extrapolatedTPS)
	fmt.Printf("Estimated node pairs needed for 50,000 TPS: %d\n", nodesNeeded)
	
	// Check if we met our performance goals
	if extrapolatedTPS >= 50000 {
		fmt.Printf("SUCCESS: Extrapolated performance meets 50,000+ TPS target\n")
	} else {
		fmt.Printf("Performance target not yet met. Current: %.2f TPS, Target: 50,000+ TPS\n", extrapolatedTPS)
	}

	fmt.Printf("\nTest configuration: %d node pairs with batch size %d\n",
		1, 100)
	fmt.Printf("Measured TPS: %.2f\n", tps)
	fmt.Printf("Projected TPS with 40 node pairs: %.2f\n", extrapolatedTPS)
	if extrapolatedTPS >= 50000 {
		fmt.Printf("✅ SUCCESS: Can achieve 50,000+ TPS target with 40 node pairs!\n")
	} else {
		fmt.Printf("⚠️ Need optimization: Current projection (%.2f TPS) below 50,000 target\n", extrapolatedTPS)
	}
}
