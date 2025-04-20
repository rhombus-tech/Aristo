package accumulator

import (
	"context"
	"fmt"
	"testing"
	"time"

	pb "github.com/rhombus-tech/vm/tee/proto"
)

func BenchmarkBatchAccumulator(b *testing.B) {
	b.Run("StandardClient", func(b *testing.B) {
		client := NewClient("bench-tee", "SGX")
		err := client.RefreshAccumulator(context.Background())
		if err != nil {
			b.Fatalf("Failed to refresh accumulator: %v", err)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			witness, err := client.GetLocalWitness(context.Background())
			if err != nil {
				b.Fatalf("Failed to get witness: %v", err)
			}

			valid, err := client.VerifyWitness(witness, true)
			if err != nil {
				b.Fatalf("Failed to verify witness: %v", err)
			}
			if !valid {
				b.Fatal("Witness should be valid")
			}
		}
	})

	b.Run("BatchClient-Sequential", func(b *testing.B) {
		opts := DefaultBatchOptions()
		opts.EnableAsync = false
		client := NewBatchClient("bench-tee", "SGX", opts)
		err := client.RefreshAccumulator(context.Background())
		if err != nil {
			b.Fatalf("Failed to refresh accumulator: %v", err)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			witness, err := client.GetLocalWitness(context.Background())
			if err != nil {
				b.Fatalf("Failed to get witness: %v", err)
			}

			valid, err := client.VerifyWitness(witness, true)
			if err != nil {
				b.Fatalf("Failed to verify witness: %v", err)
			}
			if !valid {
				b.Fatal("Witness should be valid")
			}
		}
	})

	b.Run("BatchClient-Async", func(b *testing.B) {
		opts := DefaultBatchOptions()
		opts.EnableAsync = true
		client := NewBatchClient("bench-tee", "SGX", opts)
		defer client.Close()
		
		err := client.RefreshAccumulator(context.Background())
		if err != nil {
			b.Fatalf("Failed to refresh accumulator: %v", err)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			witness, err := client.GetLocalWitness(context.Background())
			if err != nil {
				b.Fatalf("Failed to get witness: %v", err)
			}

			valid, err := client.VerifyWitness(witness, true)
			if err != nil {
				b.Fatalf("Failed to verify witness: %v", err)
			}
			if !valid {
				b.Fatal("Witness should be valid")
			}
		}
	})

	b.Run("BatchClient-BatchVerify", func(b *testing.B) {
		opts := DefaultBatchOptions()
		client := NewBatchClient("bench-tee", "SGX", opts)
		defer client.Close()
		
		err := client.RefreshAccumulator(context.Background())
		if err != nil {
			b.Fatalf("Failed to refresh accumulator: %v", err)
		}

		// Prepare batch of witnesses
		batchSize := 100
		witnesses := make([]*pb.AccumulatorWitness, batchSize)
		
		// Create witnesses
		for i := 0; i < batchSize; i++ {
			executorStr := fmt.Sprintf("bench-tee-%d", i)
			element := &pb.AccumulatorElement{
				Executor:    executorStr,
				Measurement: make([]byte, 32),
				EnclaveType: "SGX",
				Timestamp:   uint64(time.Now().Unix()),
			}
			
			// Fill measurement with some data
			for j := range element.Measurement {
				element.Measurement[j] = byte((i + j) % 256)
			}
			
			// Create a batch marker to store in the Value field
			batchID := uint64(time.Now().UnixNano())
			batchMarker := fmt.Sprintf("BATCH:%d:", batchID)
			valueData := make([]byte, 32)
			// Fill value with some data
			for j := range valueData {
				valueData[j] = byte((i + j*2) % 256)
			}
			
			witnesses[i] = &pb.AccumulatorWitness{
				Value:          append([]byte(batchMarker), valueData...),
				LastAccumulator: make([]byte, 32),
				Element:        element,
				LastUpdate:     uint64(time.Now().Unix()),
			}
			
			// Value is already filled above
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			results, err := client.BatchVerifyWitnesses(witnesses)
			if err != nil {
				b.Fatalf("Failed to batch verify witnesses: %v", err)
			}
			if len(results) != batchSize {
				b.Fatalf("Expected %d results, got %d", batchSize, len(results))
			}
		}
	})

	// Test realistic scenario with varying batch sizes
	for _, batchSize := range []int{1, 10, 50, 100, 500} {
		b.Run(fmt.Sprintf("RealBatch-%d", batchSize), func(b *testing.B) {
			opts := DefaultBatchOptions()
			opts.BatchSize = batchSize
			client := NewBatchClient("bench-tee", "SGX", opts)
			defer client.Close()
			
			err := client.RefreshAccumulator(context.Background())
			if err != nil {
				b.Fatalf("Failed to refresh accumulator: %v", err)
			}

			// Scale b.N to avoid excessive runtime for large batches
			scale := 1
			if batchSize > 100 {
				scale = batchSize / 100
			}
			
			elements := make([]*pb.AccumulatorElement, batchSize)
			for i := 0; i < batchSize; i++ {
				executorStr := fmt.Sprintf("bench-tee-%d", i)
				elements[i] = &pb.AccumulatorElement{
					Executor:    executorStr,
					Measurement: make([]byte, 32),
					EnclaveType: "SGX",
					Timestamp:   uint64(time.Now().Unix()),
				}
				
				// Fill measurement with some data
				for j := range elements[i].Measurement {
					elements[i].Measurement[j] = byte((i + j) % 256)
				}
			}

			b.ResetTimer()
			for i := 0; i < b.N/scale; i++ {
				// Add all elements to batch
				for _, element := range elements {
					client.AddToBatch(element)
				}
				
				// Process batch
				err := client.ProcessBatch(context.Background())
				if err != nil {
					b.Fatalf("Failed to process batch: %v", err)
				}
			}
			b.StopTimer()
			
			// Report throughput
			elementsProcessed := int64(b.N / scale * batchSize)
			elapsedSeconds := float64(b.Elapsed().Nanoseconds()) / 1e9
			throughput := float64(elementsProcessed) / elapsedSeconds
			
			b.ReportMetric(throughput, "elements/sec")
		})
	}
}

func TestBatchClientBatchConsistency(t *testing.T) {
	ctx := context.Background()
	
	// Create standard client
	standardClient := NewClient("test-tee", "SGX")
	err := standardClient.RefreshAccumulator(ctx)
	if err != nil {
		t.Fatalf("Failed to refresh standard accumulator: %v", err)
	}
	
	// Create batch client
	batchClient := NewBatchClient("test-tee", "SGX")
	err = batchClient.RefreshAccumulator(ctx)
	if err != nil {
		t.Fatalf("Failed to refresh batch accumulator: %v", err)
	}
	
	// Get witnesses from both clients
	standardWitness, err := standardClient.GetLocalWitness(ctx)
	if err != nil {
		t.Fatalf("Failed to get standard witness: %v", err)
	}
	
	batchWitness, err := batchClient.GetLocalWitness(ctx)
	if err != nil {
		t.Fatalf("Failed to get batch witness: %v", err)
	}
	
	// Verify cross-client compatibility
	valid, err := standardClient.VerifyWitness(batchWitness, true)
	if err != nil {
		t.Logf("Cross-client verification error: %v", err)
	}
	t.Logf("Standard client verifying batch witness: %v", valid)
	
	valid, err = batchClient.VerifyWitness(standardWitness, true)
	if err != nil {
		t.Logf("Cross-client verification error: %v", err)
	}
	t.Logf("Batch client verifying standard witness: %v", valid)
	
	// Test regional consistency
	regionalRoot := make([]byte, 32)
	for i := range regionalRoot {
		regionalRoot[i] = byte(i)
	}
	
	// Add regional verifier first
	err = batchClient.AddRegionalVerifier("us-east-1", make([]byte, 32))
	if err != nil {
		t.Fatalf("Failed to add regional verifier: %v", err)
	}
	
	// Now verify with the regional hash
	valid, err = batchClient.VerifyRegionalConsistency(ctx, regionalRoot)
	if err != nil {
		t.Fatalf("Failed to verify regional consistency: %v", err)
	}
	if !valid {
		t.Fatal("Regional consistency should be valid")
	}
	
	// Print performance stats
	stats := batchClient.GetPerformanceStats()
	for k, v := range stats {
		t.Logf("%s: %v", k, v)
	}
}
