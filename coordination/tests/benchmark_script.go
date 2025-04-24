// coordination/tests/benchmark_script.go
package tests

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rhombus-tech/vm/coordination"
	"github.com/rhombus-tech/vm/coordination/api"
)

const (
	// Benchmark configuration
	NasdaqTransactionRate = 1100 // Target TPS
	BenchmarkDurationSeconds = 30 // Duration to run each benchmark
	ReportIntervalSeconds = 5    // How often to report stats
	
	// Parameter distribution configuration
	LengthPrefixPct      = 70    // 70% length-prefixed format
	DirectFormatPct      = 30    // 30% direct format
	ContractIDSize       = 32    // Standard contract ID size in bytes
	
	// Batch sizes for testing
	TestBatchSizes       = "1,10,50,100,200,500"
	
	// Output file for benchmark results
	ResultsFile          = "tee_mesh_benchmark_results.json"
)

// BenchmarkResults stores the results of a benchmark run
type BenchmarkResults struct {
	TestName           string    `json:"test_name"`
	StartTime          time.Time `json:"start_time"`
	EndTime            time.Time `json:"end_time"`
	Duration           float64   `json:"duration_seconds"`
	TotalRequests      int       `json:"total_requests"`
	SuccessfulRequests int       `json:"successful_requests"`
	FailedRequests     int       `json:"failed_requests"`
	AverageLatencyMs   float64   `json:"average_latency_ms"`
	P95LatencyMs       float64   `json:"p95_latency_ms"`
	P99LatencyMs       float64   `json:"p99_latency_ms"`
	MaxLatencyMs       float64   `json:"max_latency_ms"`
	ThroughputTPS      float64   `json:"throughput_tps"`
	CrossValidated     int       `json:"cross_validated"`
	BatchSize          int       `json:"batch_size"`
	Notes              string    `json:"notes"`
}

// RunBenchmarks runs a series of benchmarks for the parameter validation system
func RunBenchmarks() {
	// Parse batch sizes to test
	var batchSizes []int
	for _, size := range parseBatchSizes(TestBatchSizes) {
		batchSizes = append(batchSizes, size)
	}
	
	// Results collection
	var allResults []BenchmarkResults
	
	// Run benchmark with different configurations
	benchmarkConfigs := []struct {
		name           string
		batchSize      int
		crossValidate  bool
		notes          string
	}{
		{"Baseline-Single", 1, false, "Base performance without batching or cross-validation"},
	}
	
	// Add batch size tests
	for _, size := range batchSizes {
		if size > 1 {
			benchmarkConfigs = append(benchmarkConfigs, struct {
				name           string
				batchSize      int
				crossValidate  bool
				notes          string
			}{
				fmt.Sprintf("Batch-%d", size),
				size,
				false,
				fmt.Sprintf("Batched processing with size %d", size),
			})
		}
	}
	
	// Add cross-validation tests
	benchmarkConfigs = append(benchmarkConfigs, 
		struct {
			name           string
			batchSize      int
			crossValidate  bool
			notes          string
		}{
			"Cross-Validation-10%", 
			100, // Use a good batch size
			true,
			"10% of parameters cross-validated between SGX and SEV",
		})
	
	// Create coordinator and API for testing
	coordinator, handler := setupTestEnvironment()
	
	// Run each configured benchmark
	for _, config := range benchmarkConfigs {
		fmt.Printf("\n==== Running Benchmark: %s ====\n", config.name)
		results := runSingleBenchmark(coordinator, handler, config.name, config.batchSize, config.crossValidate)
		results.Notes = config.notes
		allResults = append(allResults, results)
		
		// Report results
		reportBenchmarkResults(results)
	}
	
	// Save all results to file
	saveResultsToFile(allResults, ResultsFile)
	
	fmt.Printf("\n==== All Benchmarks Complete ====\n")
	fmt.Printf("Results saved to %s\n", ResultsFile)
	
	// Determine if we've met the NASDAQ requirements
	bestTPS := 0.0
	for _, result := range allResults {
		if result.ThroughputTPS > bestTPS {
			bestTPS = result.ThroughputTPS
		}
	}
	
	if bestTPS >= NasdaqTransactionRate {
		fmt.Printf("\n✅ NASDAQ REQUIREMENT MET: Best throughput %.2f TPS exceeds target of %d TPS\n", 
			bestTPS, NasdaqTransactionRate)
	} else {
		fmt.Printf("\n❌ NASDAQ REQUIREMENT NOT MET: Best throughput %.2f TPS below target of %d TPS\n", 
			bestTPS, NasdaqTransactionRate)
		fmt.Printf("Optimization recommendations:\n")
		fmt.Printf("1. Increase batch size for parameter validation\n")
		fmt.Printf("2. Reduce cross-validation percentage\n")
		fmt.Printf("3. Optimize parameter format detection logic\n")
		fmt.Printf("4. Add more TEE nodes to the mesh\n")
	}
}

// setupTestEnvironment sets up a test environment with coordinator and API handler
func setupTestEnvironment() (*coordination.Coordinator, *api.ParameterValidationHandler) {
	// Create coordinator
	config := &coordination.Config{
		MaxTasks:            1000,
		TaskTimeout:         time.Second * 30,
		TaskCleanupInterval: time.Minute,
	}
	
	coordinator, _ := coordination.NewCoordinator(config, nil, nil)
	coordinator.Start()
	
	// Initialize parameter validator
	validatorConfig := &coordination.ParameterValidatorConfig{
		MaxBatchSize:          500, // Large batch size for testing
		BatchInterval:         5 * time.Millisecond,
		EnableCrossValidation: true,
	}
	coordinator.InitializeParameterValidation(validatorConfig)
	
	// Set up mock TEE environment
	setupMockTEEEnvironmentForBenchmark(coordinator)
	
	// Create API handler
	handler := api.NewParameterValidationHandler(coordinator)
	
	return coordinator, handler
}

// setupMockTEEEnvironmentForBenchmark sets up mock TEE nodes for benchmark
func setupMockTEEEnvironmentForBenchmark(coordinator *coordination.Coordinator) {
	ctx := context.Background()
	
	// Create SGX and SEV workers in multiple regions for realistic testing
	regions := []string{"us-east", "us-west", "eu-central"}
	
	for i, region := range regions {
		sgxID := coordination.WorkerID(fmt.Sprintf("sgx-node-%d", i+1))
		sevID := coordination.WorkerID(fmt.Sprintf("sev-node-%d", i+1))
		
		// Register workers
		coordinator.RegisterWorker(ctx, sgxID, []byte(fmt.Sprintf("mock-sgx-enclave-%d", i+1)))
		coordinator.RegisterWorker(ctx, sevID, []byte(fmt.Sprintf("mock-sev-enclave-%d", i+1)))
		
		// Register region with TEE pair
		coordinator.RegisterRegion(ctx, region, [2]coordination.WorkerID{sgxID, sevID})
	}
}

// runSingleBenchmark runs a single benchmark with the given configuration
func runSingleBenchmark(
	coordinator *coordination.Coordinator,
	handler *api.ParameterValidationHandler,
	testName string,
	batchSize int,
	crossValidate bool,
) BenchmarkResults {
	// Set up result tracking
	results := BenchmarkResults{
		TestName:  testName,
		StartTime: time.Now(),
		BatchSize: batchSize,
	}
	
	// Track request latencies for percentile calculations
	var latencies []float64
	var latencyMutex sync.Mutex
	
	// Counters for results
	var successCount, failCount, crossValidateCount int32
	var totalLatencyMs float64
	
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ValidateParameter(w, r)
	}))
	defer server.Close()
	
	// Calculate number of requests to achieve target TPS over test duration
	numRequests := NasdaqTransactionRate * BenchmarkDurationSeconds
	
	// Use batch requests if batch size > 1
	if batchSize > 1 {
		batchCount := (numRequests + batchSize - 1) / batchSize // ceiling division
		numRequests = batchCount * batchSize // adjust to exact multiple of batch size
		
		// Set up batch server
		batchServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handler.ValidateBatch(w, r)
		}))
		defer batchServer.Close()
		
		// Run batch testing
		var wg sync.WaitGroup
		startTime := time.Now()
		
		// Create and submit batches
		for b := 0; b < batchCount; b++ {
			wg.Add(1)
			go func(batchNum int) {
				defer wg.Done()
				
				// Create a batch request
				batch := createBatchRequest(batchSize, crossValidate)
				batchJSON, _ := json.Marshal(batch)
				
				// Submit batch and time it
				batchStart := time.Now()
				resp, err := http.Post(batchServer.URL, "application/json", bytes.NewBuffer(batchJSON))
				batchLatency := time.Since(batchStart).Milliseconds()
				
				// Record batch latency
				latencyMutex.Lock()
				latencies = append(latencies, float64(batchLatency)/float64(batchSize)) // per-parameter latency
				latencyMutex.Unlock()
				
				if err != nil || resp.StatusCode != http.StatusOK {
					atomic.AddInt32(&failCount, int32(batchSize))
					return
				}
				
				// Process batch response
				var batchResp api.ValidationBatchResponse
				json.NewDecoder(resp.Body).Decode(&batchResp)
				resp.Body.Close()
				
				// Count successes and cross-validations in batch
				successInBatch := 0
				crossValidateInBatch := 0
				
				for i, paramResp := range batchResp.Responses {
					if paramResp.Success {
						successInBatch++
					}
					
					// If it was cross-validated, count it
					if batch.Requests[i].CrossValidate && paramResp.Success {
						crossValidateInBatch++
					}
				}
				
				atomic.AddInt32(&successCount, int32(successInBatch))
				atomic.AddInt32(&failCount, int32(batchSize-successInBatch))
				atomic.AddInt32(&crossValidateCount, int32(crossValidateInBatch))
				
				// Report progress periodically
				elapsed := time.Since(startTime).Seconds()
				if int(elapsed)%ReportIntervalSeconds == 0 {
					completedBatches := batchNum + 1
					pctComplete := float64(completedBatches) / float64(batchCount) * 100
					currentTPS := float64(completedBatches*batchSize) / elapsed
					
					fmt.Printf("Progress: %.1f%% complete, Current TPS: %.2f\n", pctComplete, currentTPS)
				}
			}(b)
		}
		
		// Wait for all batches to complete
		wg.Wait()
	} else {
		// Single parameter testing (no batching)
		var wg sync.WaitGroup
		startTime := time.Now()
		
		// Submit individual parameters
		for i := 0; i < numRequests; i++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				
				// Create parameter request
				paramReq := createParameterRequest(index, crossValidate)
				reqJSON, _ := json.Marshal(paramReq)
				
				// Submit request and time it
				reqStart := time.Now()
				resp, err := http.Post(server.URL, "application/json", bytes.NewBuffer(reqJSON))
				reqLatency := time.Since(reqStart).Milliseconds()
				
				// Record latency
				latencyMutex.Lock()
				latencies = append(latencies, float64(reqLatency))
				totalLatencyMs += float64(reqLatency)
				latencyMutex.Unlock()
				
				if err != nil || resp.StatusCode != http.StatusOK {
					atomic.AddInt32(&failCount, 1)
					return
				}
				
				// Process response
				var paramResp api.ParameterValidationResponse
				json.NewDecoder(resp.Body).Decode(&paramResp)
				resp.Body.Close()
				
				if paramResp.Success {
					atomic.AddInt32(&successCount, 1)
					
					// If it was cross-validated, count it
					if paramReq.CrossValidate {
						atomic.AddInt32(&crossValidateCount, 1)
					}
				} else {
					atomic.AddInt32(&failCount, 1)
				}
				
				// Report progress periodically
				elapsed := time.Since(startTime).Seconds()
				if int(elapsed)%ReportIntervalSeconds == 0 && index%(numRequests/20) == 0 {
					pctComplete := float64(index) / float64(numRequests) * 100
					currentTPS := float64(index) / elapsed
					
					fmt.Printf("Progress: %.1f%% complete, Current TPS: %.2f\n", pctComplete, currentTPS)
				}
			}(i)
		}
		
		// Wait for all requests to complete
		wg.Wait()
	}
	
	// Calculate final results
	results.EndTime = time.Now()
	results.Duration = results.EndTime.Sub(results.StartTime).Seconds()
	results.TotalRequests = numRequests
	results.SuccessfulRequests = int(successCount)
	results.FailedRequests = int(failCount)
	results.CrossValidated = int(crossValidateCount)
	
	// Calculate latency statistics
	if len(latencies) > 0 {
		results.AverageLatencyMs = totalLatencyMs / float64(len(latencies))
		
		// Sort latencies for percentile calculations
		sort.Float64s(latencies)
		
		// Calculate percentiles
		if len(latencies) > 0 {
			results.P95LatencyMs = latencies[int(float64(len(latencies))*0.95)]
			results.P99LatencyMs = latencies[int(float64(len(latencies))*0.99)]
			results.MaxLatencyMs = latencies[len(latencies)-1]
		}
	}
	
	// Calculate TPS
	results.ThroughputTPS = float64(results.SuccessfulRequests) / results.Duration
	
	return results
}

// createParameterRequest creates a single parameter validation request
func createParameterRequest(index int, crossValidate bool) api.ParameterValidationRequest {
	// Decide parameter format (70% length-prefixed, 30% direct)
	isLengthPrefixed := (index % 100) < LengthPrefixPct
	
	var data []byte
	if isLengthPrefixed {
		// Create length-prefixed parameter
		paramSize := 64 + (index % 256) // Vary sizes between 64-320 bytes
		data = createBenchmarkLengthPrefixedParam(paramSize)
	} else {
		// Create direct format parameter
		data = createBenchmarkDirectParam(ContractIDSize)
	}
	
	// Encode for JSON transport
	base64Data := base64.StdEncoding.EncodeToString(data)
	
	// Determine if this parameter should be cross-validated
	// Only cross-validate a percentage of parameters
	shouldCrossValidate := crossValidate && (index%10 == 0) // 10% cross-validation
	
	return api.ParameterValidationRequest{
		Data:          base64Data,
		CrossValidate: shouldCrossValidate,
		TimeoutMs:     1000, // 1 second timeout
	}
}

// createBatchRequest creates a batch validation request
func createBatchRequest(batchSize int, crossValidate bool) api.ValidationBatchRequest {
	batch := api.ValidationBatchRequest{
		BatchID:  fmt.Sprintf("bench-%d", time.Now().UnixNano()),
		Requests: make([]api.ParameterValidationRequest, batchSize),
	}
	
	for i := 0; i < batchSize; i++ {
		batch.Requests[i] = createParameterRequest(i, crossValidate)
	}
	
	return batch
}

// reportBenchmarkResults prints benchmark results to console
func reportBenchmarkResults(results BenchmarkResults) {
	fmt.Printf("\n----- Benchmark Results: %s -----\n", results.TestName)
	fmt.Printf("Duration: %.2f seconds\n", results.Duration)
	fmt.Printf("Total Requests: %d\n", results.TotalRequests)
	fmt.Printf("Successful: %d (%.1f%%)\n", 
		results.SuccessfulRequests, 
		float64(results.SuccessfulRequests)/float64(results.TotalRequests)*100)
	fmt.Printf("Failed: %d (%.1f%%)\n", 
		results.FailedRequests, 
		float64(results.FailedRequests)/float64(results.TotalRequests)*100)
	fmt.Printf("Cross-Validated: %d\n", results.CrossValidated)
	
	fmt.Printf("\nLatency:\n")
	fmt.Printf("  Average: %.2f ms\n", results.AverageLatencyMs)
	fmt.Printf("  P95: %.2f ms\n", results.P95LatencyMs)
	fmt.Printf("  P99: %.2f ms\n", results.P99LatencyMs)
	fmt.Printf("  Max: %.2f ms\n", results.MaxLatencyMs)
	
	fmt.Printf("\nThroughput: %.2f TPS\n", results.ThroughputTPS)
	
	// Compare with target
	if results.ThroughputTPS >= NasdaqTransactionRate {
		fmt.Printf("✅ PASSED: Exceeds NASDAQ target of %d TPS\n", NasdaqTransactionRate)
	} else {
		fmt.Printf("❌ FAILED: Below NASDAQ target of %d TPS\n", NasdaqTransactionRate)
	}
}

// saveResultsToFile saves benchmark results to a JSON file
func saveResultsToFile(results []BenchmarkResults, filename string) {
	resultsJSON, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		fmt.Printf("Error encoding results to JSON: %v\n", err)
		return
	}
	
	err = os.WriteFile(filename, resultsJSON, 0644)
	if err != nil {
		fmt.Printf("Error writing results to file: %v\n", err)
	}
}

// parseBatchSizes parses comma-separated batch sizes
func parseBatchSizes(sizeStr string) []int {
	var sizes []int
	parts := strings.Split(sizeStr, ",")
	
	for _, part := range parts {
		size, err := strconv.Atoi(strings.TrimSpace(part))
		if err == nil && size > 0 {
			sizes = append(sizes, size)
		}
	}
	
	return sizes
}

// Helper functions for parameter creation

// createBenchmarkLengthPrefixedParam creates a parameter with length prefix
func createBenchmarkLengthPrefixedParam(dataSize int) []byte {
	// Create random data
	data := make([]byte, dataSize)
	rand.Read(data)
	
	// Create parameter with length prefix
	param := make([]byte, 4+dataSize)
	binary.LittleEndian.PutUint32(param, uint32(dataSize))
	copy(param[4:], data)
	
	return param
}

// createBenchmarkDirectParam creates a direct format parameter
func createBenchmarkDirectParam(size int) []byte {
	// Create direct data
	param := make([]byte, size)
	rand.Read(param)
	
	return param
}

// Main function to run benchmarks directly
func main() {
	RunBenchmarks()
}
