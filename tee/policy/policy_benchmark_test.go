package policy

import (
	"context"
	"crypto/rand"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// BenchmarkPolicyEngine benchmarks the policy engine to ensure it meets throughput requirements
func BenchmarkPolicyEngine(b *testing.B) {
	// Skip the benchmark if in short mode
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	// Ensure we use multiple cores for testing throughput
	runtime.GOMAXPROCS(runtime.NumCPU())
	
	// Create a test policy directory
	policyDir := filepath.Join(b.TempDir(), "policy-benchmark")
	engine, err := NewPolicyEngine(policyDir)
	if err != nil {
		b.Fatalf("Failed to create policy engine: %v", err)
	}

	// Create a simple policy with measurement validation
	policy := &VerificationPolicy{
		ID:          "benchmark-policy",
		Name:        "Benchmark Policy",
		Version:     "1.0.0",
		Description: "Policy for benchmark testing",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		TargetTEEs:  []string{"TDX"},
		Constraints: []Constraint{
			{
				ID:           "measurement-validation",
				Type:         "measurement",
				Operator:     "equals",
				ExpectedValue: make([]byte, 32), // Zero hash for simplicity
				ErrorMessage: "Measurement validation failed",
				Severity:     "error",
			},
		},
	}

	// Add policy to engine
	err = engine.AddPolicy(policy)
	if err != nil {
		b.Fatalf("Failed to add policy: %v", err)
	}

	// Generate sample attestations and measurements for benchmark
	const sampleCount = 10000 // Use a large number of samples to reduce initialization impact
	samples := generateBenchmarkSamples(sampleCount)

	// Prepare for benchmark
	ctx := context.Background()
	b.ResetTimer()

	// Run the benchmark
	b.Run("SingleThread", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			sampleIdx := i % len(samples)
			sample := samples[sampleIdx]
			
			_, err := engine.EvaluateTDXAttestation(ctx, sample.Attestation, sample.Measurement)
			if err != nil {
				b.Fatalf("Error evaluating attestation: %v", err)
			}
		}
	})

	// Run multi-threaded benchmark to simulate high throughput scenarios
	b.Run("MultiThread", func(b *testing.B) {
		// Determine batch size based on b.N
		batchSize := 1000
		if b.N < batchSize {
			batchSize = b.N
		}

		// Create worker pool
		numWorkers := runtime.NumCPU()
		wg := sync.WaitGroup{}
		jobs := make(chan int, b.N)

		// Start workers
		for w := 0; w < numWorkers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := range jobs {
					sampleIdx := j % len(samples)
					sample := samples[sampleIdx]
					
					_, err := engine.EvaluateTDXAttestation(ctx, sample.Attestation, sample.Measurement)
					if err != nil {
						b.Errorf("Error evaluating attestation: %v", err)
					}
				}
			}()
		}

		// Submit jobs
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			jobs <- i
		}
		close(jobs)

		// Wait for completion
		wg.Wait()
	})

	// Run benchmark with batch verification integration
	b.Run("BatchPolicyIntegration", func(b *testing.B) {
		batchSize := 1000
		numBatches := (b.N + batchSize - 1) / batchSize // Ceiling division
		
		// Create batches of attestations
		batches := make([][][]byte, numBatches)
		for i := 0; i < numBatches; i++ {
			start := i * batchSize
			end := start + batchSize
			if end > b.N {
				end = b.N
			}
			
			batch := make([][]byte, end-start)
			for j := 0; j < len(batch); j++ {
				sampleIdx := (start + j) % len(samples)
				batch[j] = samples[sampleIdx].Attestation
			}
			
			batches[i] = batch
		}
		
		// Process batches in parallel to simulate high throughput
		var wg sync.WaitGroup
		var mu sync.Mutex
		validCount := 0
		
		b.ResetTimer()
		
		// Process each batch
		for _, batch := range batches {
			wg.Add(1)
			go func(attestations [][]byte) {
				defer wg.Done()
				
				// For each attestation in batch
				for _, attestation := range attestations {
					// Generate a measurement (in real usage, this would be extracted from the attestation)
					measurement := make([]byte, 32)
					rand.Read(measurement)
					
					// Evaluate policy
					result, err := engine.EvaluateTDXAttestation(ctx, attestation, measurement)
					if err == nil && result.Valid {
						mu.Lock()
						validCount++
						mu.Unlock()
					}
				}
			}(batch)
		}
		
		wg.Wait()
		
		// Record result to prevent compiler optimization
		b.ReportMetric(float64(validCount)/float64(b.N), "valid-ratio")
	})
}

// generateBenchmarkSamples creates random attestation/measurement pairs for benchmarking
func generateBenchmarkSamples(count int) []TestSample {
	samples := make([]TestSample, count)
	
	for i := 0; i < count; i++ {
		// Create random attestation (sized for TDX attestation)
		attestation := make([]byte, 4096)
		rand.Read(attestation)
		
		// Create random measurement
		measurement := make([]byte, 32)
		rand.Read(measurement)
		
		samples[i] = TestSample{
			Attestation: attestation,
			Measurement: measurement,
		}
	}
	
	return samples
}
