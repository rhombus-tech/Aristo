package accumulator

import (
	"context"
	"crypto/rand"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	pb "github.com/rhombus-tech/vm/tee/proto"
)

// createTestElement creates a random element for testing
func createTestElement(teeType string, index int) *pb.AccumulatorElement {
	measurement := make([]byte, 32)
	rand.Read(measurement)
	
	return &pb.AccumulatorElement{
		Executor:    fmt.Sprintf("tee-%s-%d", teeType, index),
		Measurement: measurement,
		EnclaveType: teeType,
		Timestamp:   uint64(time.Now().Unix()),
	}
}

// TestPerformanceComparison compares the original vs high-performance implementation
func TestPerformanceComparison(t *testing.T) {
	// Skip in short mode as this is a comprehensive benchmark
	if testing.Short() {
		t.Skip("Skipping performance comparison in short mode")
	}
	
	// Test parameters
	batchSizes := []int{100, 500, 1000}
	nodePairs := []int{1, 2, 4, 8}
	totalElements := 10000 // Total elements to process in each test
	
	t.Logf("Starting performance comparison (target: 50,000+ TPS)")
	t.Logf("System: %d CPU cores available", runtime.NumCPU())
	
	type Result struct {
		Implementation string
		NodePairs      int
		BatchSize      int
		Operations     int
		ElapsedTime    time.Duration
		TPS            float64
	}
	
	var results []Result
	
	// Test the original RSA implementation
	for _, nodePair := range nodePairs {
		for _, batchSize := range batchSizes {
			elementsPerNode := totalElements / nodePair
			
			t.Logf("\n==== Testing original RSA client with %d node pairs, batch size %d ====", nodePair, batchSize)
			
			// Create RSA clients
			clients := make([]*RsaClient, nodePair)
			for i := 0; i < nodePair; i++ {
				client, err := NewRsaClient(
					fmt.Sprintf("node-%d", i),
					func() string {
						if i%2 == 0 {
							return "SGX"
						}
						return "SEV"
					}(),
					RsaOptions{
						BatchSize:    batchSize,
						EnableAsync:  true,
						BatchTimeout: 100 * time.Millisecond,
						ModulusBits:  1024, // Smaller modulus for testing
					},
				)
				if err != nil {
					t.Fatalf("Failed to create RSA client: %v", err)
				}
				defer client.Close()
				clients[i] = client
			}
			
			// Measure performance
			startTime := time.Now()
			
			var wg sync.WaitGroup
			wg.Add(nodePair)
			
			for i := 0; i < nodePair; i++ {
				go func(clientIndex int) {
					defer wg.Done()
					
					client := clients[clientIndex]
					
					// Add elements
					for j := 0; j < elementsPerNode; j++ {
						element := createTestElement(
							func() string {
								if clientIndex%2 == 0 {
									return "SGX"
								}
								return "SEV"
							}(),
							clientIndex*elementsPerNode+j,
						)
						client.AddToBatch(element)
						
						// Process batch if it's full
						if j%batchSize == batchSize-1 {
							_ = client.ProcessBatch(context.Background())
						}
					}
					
					// Process any remaining elements
					_ = client.ProcessBatch(context.Background())
				}(i)
			}
			
			wg.Wait()
			
			// Calculate performance
			elapsedTime := time.Since(startTime)
			tps := float64(totalElements) / elapsedTime.Seconds()
			
			t.Logf("Original RSA: %d operations in %v", totalElements, elapsedTime)
			t.Logf("Original TPS: %.2f", tps)
			
			results = append(results, Result{
				Implementation: "Original",
				NodePairs:      nodePair,
				BatchSize:      batchSize,
				Operations:     totalElements,
				ElapsedTime:    elapsedTime,
				TPS:            tps,
			})
			
			// Extrapolate to 40 nodes
			projectedTPS := tps * float64(40) / float64(nodePair)
			t.Logf("Original projected TPS with 40 nodes: %.2f", projectedTPS)
			
			// Brief pause to let system resources recover
			time.Sleep(500 * time.Millisecond)
		}
	}
	
	// Test the high-performance implementation
	for _, nodePair := range nodePairs {
		for _, batchSize := range batchSizes {
			elementsPerNode := totalElements / nodePair
			
			t.Logf("\n==== Testing high-perf RSA client with %d node pairs, batch size %d ====", nodePair, batchSize)
			
			// Create high-perf RSA clients
			clients := make([]*HighPerfRsaClient, nodePair)
			for i := 0; i < nodePair; i++ {
				client, err := NewHighPerfRsaClient(
					fmt.Sprintf("node-%d", i),
					func() string {
						if i%2 == 0 {
							return "SGX"
						}
						return "SEV"
					}(),
					HighPerfRsaOptions{
						BatchSize:    batchSize,
						AsyncEnabled: true,
						BatchTimeout: 100 * time.Millisecond,
						ModulusBits:  1024, // Smaller modulus for testing
						Parallelism:  runtime.NumCPU(),
						Region:       fmt.Sprintf("region-%d", i%3),
					},
				)
				if err != nil {
					t.Fatalf("Failed to create high-perf RSA client: %v", err)
				}
				defer client.Close()
				clients[i] = client
			}
			
			// Measure performance
			startTime := time.Now()
			
			var wg sync.WaitGroup
			wg.Add(nodePair)
			
			for i := 0; i < nodePair; i++ {
				go func(clientIndex int) {
					defer wg.Done()
					
					client := clients[clientIndex]
					
					// Create batch of elements
					batch := make([]*pb.AccumulatorElement, elementsPerNode)
					for j := 0; j < elementsPerNode; j++ {
						batch[j] = createTestElement(
							func() string {
								if clientIndex%2 == 0 {
									return "SGX"
								}
								return "SEV"
							}(),
							clientIndex*elementsPerNode+j,
						)
					}
					
					// Add elements in efficient batches
					for j := 0; j < elementsPerNode; j += batchSize {
						end := j + batchSize
						if end > elementsPerNode {
							end = elementsPerNode
						}
						client.AddBatch(batch[j:end])
					}
					
					// Process any remaining elements
					_ = client.ProcessBatch(context.Background())
				}(i)
			}
			
			wg.Wait()
			
			// Calculate performance
			elapsedTime := time.Since(startTime)
			tps := float64(totalElements) / elapsedTime.Seconds()
			
			t.Logf("High-perf RSA: %d operations in %v", totalElements, elapsedTime)
			t.Logf("High-perf TPS: %.2f", tps)
			
			results = append(results, Result{
				Implementation: "High-Perf",
				NodePairs:      nodePair,
				BatchSize:      batchSize,
				Operations:     totalElements,
				ElapsedTime:    elapsedTime,
				TPS:            tps,
			})
			
			// Display performance metrics
			for i, client := range clients {
				t.Logf("Client %d stats: %v", i, client.GetPerformanceStats())
			}
			
			// Extrapolate to 40 nodes
			projectedTPS := tps * float64(40) / float64(nodePair)
			t.Logf("High-perf projected TPS with 40 nodes: %.2f", projectedTPS)
			
			if projectedTPS >= 50000 {
				t.Logf("✅ SUCCESS: Projected TPS (%.2f) meets 50,000+ TPS target!", projectedTPS)
			} else {
				t.Logf("⚠️ WARNING: Projected TPS (%.2f) is below target of 50,000", projectedTPS)
			}
			
			// Brief pause to let system resources recover
			time.Sleep(500 * time.Millisecond)
		}
	}
	
	// Print comparative summary
	t.Logf("\n==== PERFORMANCE COMPARISON SUMMARY ====")
	t.Logf("Implementation\tNodePairs\tBatchSize\tTPS\t\tProjected TPS (40 nodes)")
	t.Logf("-------------------------------------------------------------------")
	
	for _, result := range results {
		projectedTPS := result.TPS * 40 / float64(result.NodePairs)
		t.Logf("%s\t\t%d\t\t%d\t\t%.2f\t\t%.2f", 
			result.Implementation, 
			result.NodePairs, 
			result.BatchSize, 
			result.TPS,
			projectedTPS)
	}
	
	// Find best configurations
	var bestOriginal, bestHighPerf Result
	var bestOriginalTPS, bestHighPerfTPS float64
	
	for _, r := range results {
		projectedTPS := r.TPS * 40 / float64(r.NodePairs)
		
		if r.Implementation == "Original" && projectedTPS > bestOriginalTPS {
			bestOriginalTPS = projectedTPS
			bestOriginal = r
		} else if r.Implementation == "High-Perf" && projectedTPS > bestHighPerfTPS {
			bestHighPerfTPS = projectedTPS
			bestHighPerf = r
		}
	}
	
	// Performance improvement factor
	improvementFactor := bestHighPerfTPS / bestOriginalTPS
	
	t.Logf("\nBest original configuration: %d node pairs with batch size %d", 
		bestOriginal.NodePairs, bestOriginal.BatchSize)
	t.Logf("Best original TPS: %.2f (projected: %.2f with 40 nodes)", 
		bestOriginal.TPS, bestOriginalTPS)
	
	t.Logf("\nBest high-perf configuration: %d node pairs with batch size %d", 
		bestHighPerf.NodePairs, bestHighPerf.BatchSize)
	t.Logf("Best high-perf TPS: %.2f (projected: %.2f with 40 nodes)", 
		bestHighPerf.TPS, bestHighPerfTPS)
	
	t.Logf("\nPerformance improvement factor: %.2fx", improvementFactor)
	
	if bestHighPerfTPS >= 50000 {
		t.Logf("✅ SUCCESS: High-performance implementation can achieve 50,000+ TPS target!")
	} else {
		requiredNodes := int((50000 * float64(bestHighPerf.NodePairs)) / bestHighPerf.TPS)
		t.Logf("⚠️ ESTIMATE: Would need approximately %d node pairs to reach 50,000 TPS", requiredNodes)
	}
	
	// Test cross-platform verification (SGX to SEV)
	t.Logf("\n==== Testing Cross-Platform Verification ====")
	
	sgxClient, err := NewHighPerfRsaClient("sgx-tee", "SGX", HighPerfRsaOptions{
		BatchSize:    100,
		AsyncEnabled: false,
		Region:       "test-region",
	})
	if err != nil {
		t.Fatalf("Failed to create SGX client: %v", err)
	}
	defer sgxClient.Close()
	
	sevClient, err := NewHighPerfRsaClient("sev-tee", "SEV", HighPerfRsaOptions{
		BatchSize:    100,
		AsyncEnabled: false,
		Region:       "test-region",
	})
	if err != nil {
		t.Fatalf("Failed to create SEV client: %v", err)
	}
	defer sevClient.Close()
	
	// Create elements
	sgxElement := createTestElement("SGX", 1)
	sgxClient.AddElement(sgxElement)
	_ = sgxClient.ProcessBatch(context.Background())
	
	// Verify SGX element from SEV client
	valid, err := sevClient.VerifyCrossRegion(sgxClient, sgxElement)
	if err != nil {
		t.Logf("Cross-verification error: %v", err)
	} else if valid {
		t.Logf("✅ Cross-platform verification succeeded")
	} else {
		t.Logf("❌ Cross-platform verification failed")
	}
}

// TestRealWorldScenario simulates a full production workload
func TestRealWorldScenario(t *testing.T) {
	// Skip in short mode as this is a comprehensive benchmark
	if testing.Short() {
		t.Skip("Skipping real world scenario in short mode")
	}
	
	t.Logf("Starting real-world scenario simulation...")
	
	// Production-like parameters
	const (
		regionCount         = 3
		nodesPerRegion      = 4
		operationsPerSecond = 5000
		testDuration        = 5 * time.Second
		batchSize           = 1000
	)
	
	// Create clients for each region
	regions := make(map[string][]*HighPerfRsaClient)
	allClients := make([]*HighPerfRsaClient, 0, regionCount*nodesPerRegion)
	
	regionNames := []string{"us-east-1", "us-west-1", "eu-west-1"}
	
	// Create clients
	for _, region := range regionNames {
		regions[region] = make([]*HighPerfRsaClient, nodesPerRegion)
		
		for i := 0; i < nodesPerRegion; i++ {
			teeType := "SGX"
			if i%2 == 1 {
				teeType = "SEV"
			}
			
			client, err := NewHighPerfRsaClient(
				fmt.Sprintf("%s-node-%d", region, i),
				teeType,
				HighPerfRsaOptions{
					BatchSize:    batchSize,
					AsyncEnabled: true,
					Parallelism:  runtime.NumCPU(),
					Region:       region,
				},
			)
			if err != nil {
				t.Fatalf("Failed to create client: %v", err)
			}
			defer client.Close()
			
			regions[region][i] = client
			allClients = append(allClients, client)
		}
	}
	
	// Simulate continuous stream of operations
	totalOperations := int(operationsPerSecond * testDuration.Seconds())
	operationsPerNode := totalOperations / len(allClients)
	
	t.Logf("Simulating %d operations across %d nodes in %d regions", 
		totalOperations, len(allClients), len(regions))
	
	startTime := time.Now()
	
	var wg sync.WaitGroup
	wg.Add(len(allClients))
	
	for clientIndex, client := range allClients {
		go func(idx int, c *HighPerfRsaClient) {
			defer wg.Done()
			
			// Create elements as a continuous stream
			elementCounter := 0
			
			// Calculate rate to match operations per second
			opsPerNodePerSecond := operationsPerSecond / len(allClients)
			interval := time.Second / time.Duration(opsPerNodePerSecond)
			
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			
			for i := 0; i < operationsPerNode; i++ {
				element := createTestElement(c.teeType, elementCounter)
				elementCounter++
				
				c.AddElement(element)
				
				// Wait for next tick to maintain rate
				<-ticker.C
			}
			
			// Process any remaining elements
			_ = c.ProcessBatch(context.Background())
		}(clientIndex, client)
	}
	
	wg.Wait()
	
	elapsedTime := time.Since(startTime)
	actualTPS := float64(totalOperations) / elapsedTime.Seconds()
	
	t.Logf("Real-world scenario completed in %v", elapsedTime)
	t.Logf("Target TPS: %d, Actual TPS: %.2f", operationsPerSecond, actualTPS)
	
	// Calculate projected TPS for 40 node pairs
	totalNodes := len(allClients)
	projectedTPS := actualTPS * 40 / float64(totalNodes/2) // divide by 2 for node pairs
	
	t.Logf("Projected TPS with 40 node pairs: %.2f", projectedTPS)
	
	if projectedTPS >= 50000 {
		t.Logf("✅ SUCCESS: Real-world scenario projects %.2f TPS with 40 node pairs", projectedTPS)
	} else {
		t.Logf("⚠️ Real-world scenario projects %.2f TPS with 40 node pairs", projectedTPS)
	}
	
	// Test cross-regional verification
	t.Logf("\nTesting cross-regional verification...")
	
	// Create test elements in each region
	regionElements := make(map[string]*pb.AccumulatorElement)
	
	for region, clients := range regions {
		if len(clients) > 0 {
			element := createTestElement(clients[0].teeType, 0)
			clients[0].AddElement(element)
			_ = clients[0].ProcessBatch(context.Background())
			regionElements[region] = element
		}
	}
	
	// Verify elements across regions
	for srcRegion, srcElement := range regionElements {
		for dstRegion, dstClients := range regions {
			if srcRegion != dstRegion && len(dstClients) > 0 {
				srcClient := regions[srcRegion][0]
				dstClient := dstClients[0]
				
				t.Logf("Verifying %s element in %s...", srcRegion, dstRegion)
				
				valid, err := dstClient.VerifyCrossRegion(srcClient, srcElement)
				if err != nil {
					t.Logf("  Error: %v", err)
				} else if valid {
					t.Logf("  ✅ Successful cross-region verification")
				} else {
					t.Logf("  ❌ Failed cross-region verification")
				}
			}
		}
	}
	
	// Get final performance stats from all clients
	t.Logf("\nFinal performance statistics:")
	
	for region, clients := range regions {
		t.Logf("Region: %s", region)
		for i, client := range clients {
			stats := client.GetPerformanceStats()
			t.Logf("  Node %d: %v", i, stats)
		}
	}
}
