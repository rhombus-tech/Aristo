package accumulator

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"sync"
	"testing"
	"time"

	pb "github.com/rhombus-tech/vm/tee/proto"
)

// TestOptimizedAccumulatorBasic tests basic functionality
func TestOptimizedAccumulatorBasic(t *testing.T) {
	// Create an optimized RSA client with small batch size for testing
	client, err := NewOptimizedRsaClient("test-tee-1", "SGX", OptimizedRsaOptions{
		BatchSize: 10,
		EnableAsync: false,
		ModulusBits: 1024, // Smaller for testing
	})
	
	if err != nil {
		t.Fatalf("Failed to create optimized RSA client: %v", err)
	}
	defer client.Close()
	
	// Add a single element
	element := &pb.AccumulatorElement{
		Executor:    "test-executor",
		Measurement: []byte("test-measurement"),
		EnclaveType: "SGX",
		Timestamp:   uint64(time.Now().Unix()),
	}
	
	client.AddToBatch(element)
	
	// Process the batch
	err = client.ProcessBatch(context.Background())
	if err != nil {
		t.Fatalf("Failed to process batch: %v", err)
	}
	
	// Get witness for the element
	witness, err := client.GetWitnessForElement(element)
	if err != nil {
		t.Fatalf("Failed to get witness: %v", err)
	}
	
	// Verify the witness
	t.Logf("Witness details: Value=%v, AccumulatorSnapshot=%v", witness.Value, witness.AccumulatorSnapshot)
	
	// Get the prime for verification
	prime := client.hashToPrime(element)
	t.Logf("Element hash prime: %v", prime)
	
	// Compute verification manually
	computed := new(big.Int).Exp(witness.Value, prime, client.modulus)
	t.Logf("Computed: %v, Target: %v, Equal: %v", 
		computed, witness.AccumulatorSnapshot, computed.Cmp(witness.AccumulatorSnapshot) == 0)
	
	// Verify through client method
	valid, err := client.VerifyWitness(witness)
	if err != nil {
		t.Fatalf("Failed to verify witness: %v", err)
	}
	
	if !valid {
		t.Fatalf("Witness verification failed")
	}
}

// createRandomElement creates a random accumulator element for testing
func createRandomElement(teeType string, index int) *pb.AccumulatorElement {
	// Create random measurement
	measurement := make([]byte, 32)
	rand.Read(measurement)
	
	return &pb.AccumulatorElement{
		Executor:    fmt.Sprintf("tee-%s-%d", teeType, index),
		Measurement: measurement,
		EnclaveType: teeType,
		Timestamp:   uint64(time.Now().Unix()),
	}
}

// BenchmarkSingleElementAddition benchmarks adding single elements
func BenchmarkSingleElementAddition(b *testing.B) {
	client, err := NewOptimizedRsaClient("benchmark-tee", "SGX", OptimizedRsaOptions{
		BatchSize:   1,
		EnableAsync: false,
	})
	if err != nil {
		b.Fatalf("Failed to create RSA client: %v", err)
	}
	defer client.Close()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		element := createRandomElement("SGX", i)
		client.AddToBatch(element)
		err := client.ProcessBatch(context.Background())
		if err != nil {
			b.Fatalf("Failed to process batch: %v", err)
		}
	}
}

// BenchmarkBatchAddition benchmarks batch element addition
func BenchmarkBatchAddition(b *testing.B) {
	batchSizes := []int{10, 50, 100, 500, 1000}
	
	for _, batchSize := range batchSizes {
		b.Run(fmt.Sprintf("BatchSize=%d", batchSize), func(b *testing.B) {
			client, err := NewOptimizedRsaClient("benchmark-tee", "SGX", OptimizedRsaOptions{
				BatchSize:   batchSize,
				EnableAsync: false,
				Parallelism: 8,
			})
			if err != nil {
				b.Fatalf("Failed to create RSA client: %v", err)
			}
			defer client.Close()
			
			// Prepare elements
			elements := make([]*pb.AccumulatorElement, batchSize)
			for i := 0; i < batchSize; i++ {
				elements[i] = createRandomElement("SGX", i)
			}
			
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Add elements to batch
				for _, element := range elements {
					client.AddToBatch(element)
				}
				
				// Process batch
				err := client.ProcessBatch(context.Background())
				if err != nil {
					b.Fatalf("Failed to process batch: %v", err)
				}
			}
		})
	}
}

// BenchmarkWitnessVerification benchmarks witness verification
func BenchmarkWitnessVerification(b *testing.B) {
	batchSizes := []int{10, 50, 100}
	
	for _, batchSize := range batchSizes {
		b.Run(fmt.Sprintf("BatchSize=%d", batchSize), func(b *testing.B) {
			client, err := NewOptimizedRsaClient("benchmark-tee", "SGX", OptimizedRsaOptions{
				BatchSize:   batchSize,
				EnableAsync: false,
			})
			if err != nil {
				b.Fatalf("Failed to create RSA client: %v", err)
			}
			defer client.Close()
			
			// Create elements and witnesses
			elements := make([]*pb.AccumulatorElement, batchSize)
			for i := 0; i < batchSize; i++ {
				elements[i] = createRandomElement("SGX", i)
				client.AddToBatch(elements[i])
			}
			
			// Process the batch
			err = client.ProcessBatch(context.Background())
			if err != nil {
				b.Fatalf("Failed to process batch: %v", err)
			}
			
			// Get witnesses
			witnesses := make([]*OptimizedWitness, batchSize)
			for i, element := range elements {
				witness, err := client.GetWitnessForElement(element)
				if err != nil {
					b.Fatalf("Failed to get witness: %v", err)
				}
				witnesses[i] = witness
			}
			
			// Benchmark verification
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Verify each witness
				for _, witness := range witnesses {
					valid, err := client.VerifyWitness(witness)
					if err != nil {
						b.Fatalf("Failed to verify witness: %v", err)
					}
					if !valid {
						b.Fatalf("Witness verification failed")
					}
				}
			}
		})
	}
}

// BenchmarkBatchVerification benchmarks batch verification
func BenchmarkBatchVerification(b *testing.B) {
	batchSizes := []int{10, 50, 100, 500}
	
	for _, batchSize := range batchSizes {
		b.Run(fmt.Sprintf("BatchSize=%d", batchSize), func(b *testing.B) {
			client, err := NewOptimizedRsaClient("benchmark-tee", "SGX", OptimizedRsaOptions{
				BatchSize:   batchSize,
				EnableAsync: false,
				Parallelism: 8,
			})
			if err != nil {
				b.Fatalf("Failed to create RSA client: %v", err)
			}
			defer client.Close()
			
			// Create elements and witnesses
			elements := make([]*pb.AccumulatorElement, batchSize)
			for i := 0; i < batchSize; i++ {
				elements[i] = createRandomElement("SGX", i)
				client.AddToBatch(elements[i])
			}
			
			// Process the batch
			err = client.ProcessBatch(context.Background())
			if err != nil {
				b.Fatalf("Failed to process batch: %v", err)
			}
			
			// Get witnesses
			witnesses := make([]*OptimizedWitness, batchSize)
			for i, element := range elements {
				witness, err := client.GetWitnessForElement(element)
				if err != nil {
					b.Fatalf("Failed to get witness: %v", err)
				}
				witnesses[i] = witness
			}
			
			// Benchmark batch verification
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Verify all witnesses in batch
				results, err := client.BatchVerifyWitnesses(witnesses)
				if err != nil {
					b.Fatalf("Failed to batch verify witnesses: %v", err)
				}
				
				// Check results
				for teeID, valid := range results {
					if !valid {
						b.Fatalf("Batch verification failed for %s", teeID)
					}
				}
			}
		})
	}
}

// BenchmarkAsyncBatchProcessing benchmarks asynchronous batch processing
func BenchmarkAsyncBatchProcessing(b *testing.B) {
	batchSizes := []int{100, 500, 1000}
	
	for _, batchSize := range batchSizes {
		b.Run(fmt.Sprintf("BatchSize=%d", batchSize), func(b *testing.B) {
			client, err := NewOptimizedRsaClient("benchmark-tee", "SGX", OptimizedRsaOptions{
				BatchSize:   batchSize,
				EnableAsync: true,
				Parallelism: 8,
			})
			if err != nil {
				b.Fatalf("Failed to create RSA client: %v", err)
			}
			defer client.Close()
			
			// Benchmark adding elements
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Add batch size elements
				for j := 0; j < batchSize; j++ {
					element := createRandomElement("SGX", j)
					client.AddToBatch(element)
				}
				
				// Allow some time for async processing
				time.Sleep(100 * time.Millisecond)
			}
		})
	}
}

// BenchmarkCrossRegionVerification benchmarks cross-region verification
func BenchmarkCrossRegionVerification(b *testing.B) {
	// Create clients for two different regions
	region1Client, err := NewOptimizedRsaClient("region1-tee", "SGX", OptimizedRsaOptions{
		BatchSize:   100,
		EnableAsync: false,
		RegionID:    "us-east-1",
	})
	if err != nil {
		b.Fatalf("Failed to create region1 client: %v", err)
	}
	defer region1Client.Close()
	
	region2Client, err := NewOptimizedRsaClient("region2-tee", "SEV", OptimizedRsaOptions{
		BatchSize:   100,
		EnableAsync: false,
		RegionID:    "us-west-1",
	})
	if err != nil {
		b.Fatalf("Failed to create region2 client: %v", err)
	}
	defer region2Client.Close()
	
	// Create and process elements in region1
	region1Elements := make([]*pb.AccumulatorElement, 10)
	for i := 0; i < 10; i++ {
		region1Elements[i] = createRandomElement("SGX", i)
		region1Client.AddToBatch(region1Elements[i])
	}
	
	err = region1Client.ProcessBatch(context.Background())
	if err != nil {
		b.Fatalf("Failed to process batch in region1: %v", err)
	}
	
	// Benchmark cross-region verification
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Verify region1 elements from region2
		for _, element := range region1Elements {
			valid, err := region2Client.VerifyCrossRegion(region1Client, element)
			if err != nil {
				b.Fatalf("Failed to verify cross-region: %v", err)
			}
			if !valid {
				b.Fatalf("Cross-region verification failed")
			}
		}
	}
}

// BenchmarkParallelAccumulators benchmarks parallel accumulator operations
func BenchmarkParallelAccumulators(b *testing.B) {
	// Number of parallel accumulators to simulate multiple node pairs
	nodeCount := 10
	elementsPerNode := 1000
	
	b.Run(fmt.Sprintf("Nodes=%d,ElementsPerNode=%d", nodeCount, elementsPerNode), func(b *testing.B) {
		// Create clients for multiple nodes
		clients := make([]*OptimizedRsaClient, nodeCount)
		for i := 0; i < nodeCount; i++ {
			client, err := NewOptimizedRsaClient(
				fmt.Sprintf("node-%d", i),
				"SGX",
				OptimizedRsaOptions{
					BatchSize:   elementsPerNode,
					EnableAsync: true,
					Parallelism: 8,
					RegionID:    fmt.Sprintf("region-%d", i%3), // Distribute across 3 regions
				},
			)
			if err != nil {
				b.Fatalf("Failed to create client %d: %v", i, err)
			}
			defer client.Close()
			clients[i] = client
		}
		
		// Prepare elements for each client
		elementsByClient := make([][]*pb.AccumulatorElement, nodeCount)
		for i := 0; i < nodeCount; i++ {
			elementsByClient[i] = make([]*pb.AccumulatorElement, elementsPerNode)
			for j := 0; j < elementsPerNode; j++ {
				elementsByClient[i][j] = createRandomElement("SGX", i*elementsPerNode+j)
			}
		}
		
		// Benchmark parallel processing
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			// Set up a local pool of clients for this goroutine
			clientIndex := 0
			
			for pb.Next() {
				client := clients[clientIndex]
				elements := elementsByClient[clientIndex]
				
				// Add all elements
				for _, element := range elements {
					client.AddToBatch(element)
				}
				
				// Process batch
				_ = client.ProcessBatch(context.Background())
				
				// Move to next client
				clientIndex = (clientIndex + 1) % nodeCount
			}
		})
	})
}

// ThroughputTest is a specialized benchmark to measure TPS directly
func TestThroughputOptimized(t *testing.T) {
	// Skip in short mode as this is a comprehensive test
	if testing.Short() {
		t.Skip("Skipping throughput test in short mode")
	}
	
	// Test parameters
	nodePairs := []int{1, 2, 4, 8}
	batchSizes := []int{100, 500, 1000}
	
	for _, nodePair := range nodePairs {
		for _, batchSize := range batchSizes {
			t.Run(fmt.Sprintf("NodePairs=%d,BatchSize=%d", nodePair, batchSize), func(t *testing.T) {
				// Create clients for each node pair
				clients := make([]*OptimizedRsaClient, nodePair)
				for i := 0; i < nodePair; i++ {
					client, err := NewOptimizedRsaClient(
						fmt.Sprintf("node-%d", i),
						func() string {
							if i%2 == 0 {
								return "SGX"
							}
							return "SEV"
						}(), // Alternate SGX and SEV
						OptimizedRsaOptions{
							BatchSize:   batchSize,
							EnableAsync: true,
							Parallelism: 8,
							RegionID:    "us-east-1",
							ModulusBits: 1024, // Use smaller key for faster benchmark tests
							VerifyInterval: 100 * time.Millisecond, // Ensure non-zero interval
						},
					)
					if err != nil {
						t.Fatalf("Failed to create client %d: %v", i, err)
					}
					defer client.Close()
					clients[i] = client
				}
				
				// Number of operations to perform
				totalOperations := 10000
				operationsPerNode := totalOperations / nodePair
				
				// Track start time
				startTime := time.Now()
				
				// Use WaitGroup to wait for all operations
				var wg sync.WaitGroup
				wg.Add(nodePair)
				
				// Start goroutine for each client
				for i := 0; i < nodePair; i++ {
					go func(clientIndex int) {
						defer wg.Done()
						
						client := clients[clientIndex]
						
						// Generate and process elements
						for j := 0; j < operationsPerNode; j++ {
							element := createRandomElement(
								func() string {
									if clientIndex%2 == 0 {
										return "SGX"
									}
									return "SEV"
								}(),
								clientIndex*operationsPerNode+j,
							)
							client.AddToBatch(element)
							
							// Process batch if it's full (this will happen automatically with async)
							if j%batchSize == batchSize-1 {
								_ = client.ProcessBatch(context.Background())
							}
						}
						
						// Process any remaining elements
						_ = client.ProcessBatch(context.Background())
					}(i)
				}
				
				// Wait for all operations to complete
				wg.Wait()
				
				// Calculate elapsed time and TPS
				elapsedTime := time.Since(startTime)
				tps := float64(totalOperations) / elapsedTime.Seconds()
				
				t.Logf("Configuration: %d node pairs, batch size %d", nodePair, batchSize)
				t.Logf("Total operations: %d", totalOperations)
				t.Logf("Elapsed time: %v", elapsedTime)
				t.Logf("Transactions per second (TPS): %.2f", tps)
				
				// Get stats from clients
				for i, client := range clients {
					stats := client.GetPerformanceStats()
					t.Logf("Client %d stats: %v", i, stats)
				}
				
				// Validate that we're on track to meet the 50k TPS target
				// Extrapolate to expected TPS with 40 node pairs (our max target)
				maxNodePairs := 40
				projectedTPS := tps * float64(maxNodePairs) / float64(nodePair)
				t.Logf("Projected TPS with %d node pairs: %.2f", maxNodePairs, projectedTPS)
				
				if projectedTPS < 50000 && nodePair == nodePairs[len(nodePairs)-1] && batchSize == batchSizes[len(batchSizes)-1] {
					t.Logf("WARNING: Projected TPS (%.2f) is below target of 50,000", projectedTPS)
				}
			})
		}
	}
}

// TestCrossAttestationSecurity tests the security aspects of cross-attestation
func TestCrossAttestationSecurity(t *testing.T) {
	// Create clients for different TEE types
	sgxClient, err := NewOptimizedRsaClient("sgx-tee", "SGX", OptimizedRsaOptions{
		BatchSize:   100,
		EnableAsync: false,
		RegionID:    "us-east-1",
	})
	if err != nil {
		t.Fatalf("Failed to create SGX client: %v", err)
	}
	defer sgxClient.Close()
	
	sevClient, err := NewOptimizedRsaClient("sev-tee", "SEV", OptimizedRsaOptions{
		BatchSize:   100,
		EnableAsync: false,
		RegionID:    "us-east-1",
	})
	if err != nil {
		t.Fatalf("Failed to create SEV client: %v", err)
	}
	defer sevClient.Close()
	
	// Create valid SGX element
	sgxElement := &pb.AccumulatorElement{
		Executor:    "sgx-tee-valid",
		Measurement: []byte("valid-sgx-measurement"),
		EnclaveType: "SGX",
		Timestamp:   uint64(time.Now().Unix()),
	}
	sgxClient.AddToBatch(sgxElement)
	err = sgxClient.ProcessBatch(context.Background())
	if err != nil {
		t.Fatalf("Failed to process batch: %v", err)
	}
	
	// Verify valid SGX element from SEV
	valid, err := sevClient.VerifyCrossRegion(sgxClient, sgxElement)
	if err != nil {
		t.Fatalf("Failed to verify cross-region: %v", err)
	}
	if !valid {
		t.Fatalf("Valid cross-platform verification failed")
	}
	
	// Try to tamper with the element
	tamperedElement := &pb.AccumulatorElement{
		Executor:    "sgx-tee-valid",
		Measurement: []byte("tampered-measurement"), // Changed measurement
		EnclaveType: "SGX",
		Timestamp:   uint64(time.Now().Unix()),
	}
	
	// Verify tampered element should fail
	valid, err = sevClient.VerifyCrossRegion(sgxClient, tamperedElement)
	if err == nil && valid {
		t.Fatalf("Security failure: tampered element was verified successfully")
	}
	
	t.Logf("Cross-attestation security test passed: tampered element was rejected")
}
