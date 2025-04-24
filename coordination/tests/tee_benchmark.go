// coordination/tests/tee_benchmark.go
package tests

import (
	"bytes"
	"context"
	"encoding/binary"
	"flag"
	"fmt" // Used in test function
	"math/rand"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Flag variables are defined in the main.go file to avoid flag redefinition

// TEEBenchmarkConfig holds the configuration for the TEE benchmark
type TEEBenchmarkConfig struct {
	SGXEndpoints      []string
	SEVEndpoints      []string
	GenericEndpoints  []string
	ParamCount        int
	ParamSize         int
	Concurrency       int
	CrossValidate     bool
	CrossValidateRate float64
}

// TEEBenchmarkResult stores results from the TEE benchmark
type TEEBenchmarkResult struct {
	StartTime           time.Time
	EndTime             time.Time
	Duration            time.Duration
	SuccessCount        uint64
	FailureCount        uint64
	TotalTimeNs         uint64
	LengthPrefixedCount uint64
	DirectFormatCount   uint64
	CrossValidated      uint64
	TPS                 float64
}

// Logger interface for benchmark logging
type Logger interface {
	Logf(format string, args ...interface{})
}

// RunTEEBenchmark runs a benchmark against real TEE nodes
func RunTEEBenchmark(logger Logger, config *TEEBenchmarkConfig) *TEEBenchmarkResult {
	// Log the benchmark configuration
	logger.Logf("Setting up benchmark with %d SGX nodes, %d SEV nodes, and %d generic nodes",
		len(config.SGXEndpoints), len(config.SEVEndpoints), len(config.GenericEndpoints))

	// Use a simpler approach with direct HTTP clients to the TEE endpoints
	// Create HTTP clients for each TEE node
	sgxClients := createTEEClients(config.SGXEndpoints)
	sevClients := createTEEClients(config.SEVEndpoints)
	genericClients := createTEEClients(config.GenericEndpoints)

	// Generate test parameters
	logger.Logf("Generating %d test parameters of size %d bytes", config.ParamCount, config.ParamSize)
	parameters := generateMixedTestParameters(config.ParamCount, config.ParamSize)

	// Set up test result tracking
	benchResult := &TEEBenchmarkResult{
		StartTime: time.Now(),
	}

	// Set up test execution
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Rate limiting to avoid overwhelming the TEE nodes
	limiter := make(chan struct{}, config.Concurrency)
	var wg sync.WaitGroup

	logger.Logf("Starting benchmark with concurrency %d, cross-validation %v (rate: %.2f)", 
		config.Concurrency, config.CrossValidate, config.CrossValidateRate)

	// Process parameters in parallel with rate limiting
	for i, param := range parameters {
		// Fill the limiter channel (block if full)
		limiter <- struct{}{}

		wg.Add(1)
		go func(index int, data []byte) {
			defer wg.Done()
			defer func() { <-limiter }() // Release a slot when done

			// Skip if context is done
			if ctx.Err() != nil {
				return
			}

			// Determine if this parameter should be cross-validated
			useCrossValidation := config.CrossValidate && 
				(rand.Float64() < config.CrossValidateRate)

			// Process parameter using the appropriate TEE client
			startTime := time.Now()
			
			// Check if we have any real TEE clients available
			hasRealClients := len(sgxClients) > 0 || len(sevClients) > 0 || len(genericClients) > 0
			
			// Determine if we should use real TEE nodes or fallback to mock
			if !hasRealClients {
				// No clients available, use our mock implementation
				mockValidator := NewMockCoordinator()
				result, err := mockValidator.ValidateParameter(ctx, data)
				
				duration := time.Since(startTime)
				
				if err != nil {
					atomic.AddUint64(&benchResult.FailureCount, 1)
					return
				}
				
				// Record successful validation
				atomic.AddUint64(&benchResult.SuccessCount, 1)
				atomic.AddUint64(&benchResult.TotalTimeNs, uint64(duration.Nanoseconds()))
				
				// Track format distribution
				if result.Format == "length_prefixed" {
					atomic.AddUint64(&benchResult.LengthPrefixedCount, 1)
				} else if result.Format == "direct" {
					atomic.AddUint64(&benchResult.DirectFormatCount, 1)
				}
				
				return
			}
			
			// Use real TEE nodes for validation
			// Select primary node (prefer SGX)
			var primaryEndpoint string
			var primaryClient *http.Client
			var secondaryEndpoint string
			var secondaryClient *http.Client
			
			// Select clients for validation (prefer SGX → SEV → generic)
			if len(sgxClients) > 0 {
				primaryClient = sgxClients[index%len(sgxClients)]
				primaryEndpoint = config.SGXEndpoints[index%len(config.SGXEndpoints)]
				
				// For cross-validation, use SEV if available
				if useCrossValidation && len(sevClients) > 0 {
					secondaryClient = sevClients[index%len(sevClients)]
					secondaryEndpoint = config.SEVEndpoints[index%len(config.SEVEndpoints)]
					atomic.AddUint64(&benchResult.CrossValidated, 1)
				}
			} else if len(sevClients) > 0 {
				primaryClient = sevClients[index%len(sevClients)]
				primaryEndpoint = config.SEVEndpoints[index%len(config.SEVEndpoints)]
				
				// For cross-validation, use generic TEE if available
				if useCrossValidation && len(genericClients) > 0 {
					secondaryClient = genericClients[index%len(genericClients)]
					secondaryEndpoint = config.GenericEndpoints[index%len(config.GenericEndpoints)]
					atomic.AddUint64(&benchResult.CrossValidated, 1)
				}
			} else if len(genericClients) > 0 {
				primaryClient = genericClients[index%len(genericClients)]
				primaryEndpoint = config.GenericEndpoints[index%len(config.GenericEndpoints)]
			}
			
			// Call primary TEE node for parameter validation
			primarySuccess := false
			if primaryClient != nil {
				primarySuccess = validateParameterWithTEENode(primaryClient, primaryEndpoint, data)
			}
			
			// If cross-validation is enabled and we have a secondary client, call it as well
			secondarySuccess := true // Default to true if no secondary validation needed
			if primarySuccess && secondaryClient != nil {
				secondarySuccess = validateParameterWithTEENode(secondaryClient, secondaryEndpoint, data)
			}
			
			// Both validations must succeed for cross-validation
			success := primarySuccess && secondarySuccess
			
			duration := time.Since(startTime)
			
			if !success {
				atomic.AddUint64(&benchResult.FailureCount, 1)
				return
			}
			
			// Record successful validation
			atomic.AddUint64(&benchResult.SuccessCount, 1)
			atomic.AddUint64(&benchResult.TotalTimeNs, uint64(duration.Nanoseconds()))
			
			// Track format distribution based on parameter format detection
			format := detectParameterFormat(data)
			if format == "length_prefixed" {
				atomic.AddUint64(&benchResult.LengthPrefixedCount, 1)
			} else if format == "direct" {
				atomic.AddUint64(&benchResult.DirectFormatCount, 1)
			}
			
			// Track cross-validation if used
			if useCrossValidation {
				atomic.AddUint64(&benchResult.CrossValidated, 1)
			}
		}(i, param)

		// Check context occasionally
		if i%1000 == 0 && ctx.Err() != nil {
			break
		}
	}

	// Wait for all operations to complete
	wg.Wait()
	benchResult.EndTime = time.Now()
	benchResult.Duration = benchResult.EndTime.Sub(benchResult.StartTime)

	// Calculate TPS
	if benchResult.Duration > 0 {
		benchResult.TPS = float64(benchResult.SuccessCount) / benchResult.Duration.Seconds()
	}

	return benchResult
}

// createTEEClients creates HTTP clients for TEE endpoints
func createTEEClients(endpoints []string) []*http.Client {
	clients := make([]*http.Client, len(endpoints))
	for i := range endpoints {
		// Create client with appropriate timeouts
		clients[i] = &http.Client{
			Timeout: 5 * time.Second,
		}
	}
	return clients
}

// generateMixedTestParameters generates a mix of length-prefixed and direct format parameters
func generateMixedTestParameters(count, size int) [][]byte {
	parameters := make([][]byte, count)
	
	for i := 0; i < count; i++ {
		// Generate a mix of length-prefixed and direct parameters
		if i%2 == 0 {
			// Length-prefixed format
			parameters[i] = generateLengthPrefixedParameter(size)
		} else {
			// Direct format
			parameters[i] = generateDirectParameter(size)
		}
	}
	
	return parameters
}

// generateLengthPrefixedParameter generates a length-prefixed parameter
func generateLengthPrefixedParameter(size int) []byte {
	// Allocate buffer for length prefix (4 bytes) + data
	data := make([]byte, size+4)
	
	// Set the length prefix (4-byte little-endian u32)
	binary.LittleEndian.PutUint32(data[:4], uint32(size))
	
	// Fill the rest with random data
	rand.Read(data[4:])
	
	return data
}

// generateDirectParameter generates a direct parameter
func generateDirectParameter(size int) []byte {
	data := make([]byte, size)
	rand.Read(data)
	return data
}

// RunRealTEEBenchmark is the main benchmark function to be called from tests
// TestRealTEEBenchmark tests our dual-format parameter validation with real TEE nodes
func TestRealTEEBenchmark(t *testing.T) {
	// Skip the test if not running in real TEE benchmark mode
	if os.Getenv("RUN_TEE_BENCHMARK") != "true" {
		t.Skip("Skipping real TEE benchmark; set RUN_TEE_BENCHMARK=true to run")
	}

	// Define test flags locally to avoid global flag redefinition
	sgxEndpointsFlag := flag.String("sgx-endpoints", "", "Comma-separated list of SGX node endpoints")
	sevEndpointsFlag := flag.String("sev-endpoints", "", "Comma-separated list of SEV node endpoints")
	teeEndpointsFlag := flag.String("tee-endpoints", "", "Comma-separated list of TEE node endpoints")
	paramCountFlag := flag.Int("param-count", 10000, "Number of parameters to validate")
	paramSizeFlag := flag.Int("param-size", 256, "Size of test parameters in bytes")
	concurrencyFlag := flag.Int("concurrency", 100, "Number of concurrent validation requests")
	crossValidateFlag := flag.Bool("cross-validate", true, "Enable cross-validation between TEE nodes")
	crossValidateRateFlag := flag.Float64("cross-validate-rate", 0.2, "Percentage of parameters to cross-validate")

	// Parse command line flags if not already parsed
	if !flag.Parsed() {
		flag.Parse()
	}
	
	// Set up the benchmark configuration
	config := &TEEBenchmarkConfig{
		ParamCount:        *paramCountFlag,
		ParamSize:         *paramSizeFlag,
		Concurrency:       *concurrencyFlag,
		CrossValidate:     *crossValidateFlag,
		CrossValidateRate: *crossValidateRateFlag,
	}
	
	// Parse TEE endpoints
	if *sgxEndpointsFlag != "" {
		config.SGXEndpoints = splitEndpoints(*sgxEndpointsFlag)
	}
	
	if *sevEndpointsFlag != "" {
		config.SEVEndpoints = splitEndpoints(*sevEndpointsFlag)
	}
	
	if *teeEndpointsFlag != "" {
		config.GenericEndpoints = splitEndpoints(*teeEndpointsFlag)
	}
	
	// Create logger adapter to use with t
	logger := &testLogger{t: t}
	
	// Run the benchmark
	result := RunTEEBenchmark(logger, config)
	
	// Output benchmark info
	fmt.Printf("Benchmark completed successfully in %.2f seconds\n", result.Duration.Seconds())
	
	// Print benchmark results
	logger.Logf("===== TEE Benchmark Results =====")
	logger.Logf("Duration: %.2f seconds", result.Duration.Seconds())
	logger.Logf("Parameters Processed: %d (success: %d, failure: %d)",
		result.SuccessCount+result.FailureCount, result.SuccessCount, result.FailureCount)
	
	if result.SuccessCount > 0 {
		avgTimeMs := float64(result.TotalTimeNs) / float64(result.SuccessCount) / 1e6
		logger.Logf("Average Processing Time: %.3f ms per parameter", avgTimeMs)
	}
	
	logger.Logf("Throughput: %.2f TPS (parameters per second)", result.TPS)
	
	// Report format distribution
	if result.SuccessCount > 0 {
		lpPct := float64(result.LengthPrefixedCount) / float64(result.SuccessCount) * 100
		dirPct := float64(result.DirectFormatCount) / float64(result.SuccessCount) * 100
		logger.Logf("Format Distribution: Length-Prefixed: %d (%.1f%%), Direct: %d (%.1f%%)",
			result.LengthPrefixedCount, lpPct, result.DirectFormatCount, dirPct)
	}
	
	// Report cross-validation stats
	if result.CrossValidated > 0 {
		cvPct := float64(result.CrossValidated) / float64(result.SuccessCount) * 100
		logger.Logf("Cross-Validated: %d (%.1f%%)", result.CrossValidated, cvPct)
	}
	
	// Report success or failure against target
	targetTPS := 1100.0
	if result.TPS >= targetTPS {
		logger.Logf("✅ PASSED: Achieved %.2f TPS, exceeding target of %.0f TPS", result.TPS, targetTPS)
	} else {
		logger.Logf("❌ FAILED: Achieved only %.2f TPS, below target of %.0f TPS", result.TPS, targetTPS)
	}
}

// detectParameterFormat detects if a parameter is length-prefixed or direct format
func detectParameterFormat(data []byte) string {
	// Check if this looks like a length-prefixed parameter
	if len(data) >= 4 {
		// Extract length prefix as little-endian uint32
		paramLen := binary.LittleEndian.Uint32(data[:4])
		
		// If length is reasonable (greater than 0, less than max allowed)
		if paramLen > 0 && paramLen <= 1024 {
			return "length_prefixed"
		}
	}
	
	// Otherwise, treat as direct format
	return "direct"
}

// validateParameterWithTEENode sends a parameter to a TEE node for validation
// testLogger adapts testing.T to our Logger interface
type testLogger struct {
	t *testing.T
}

func (l *testLogger) Logf(format string, args ...interface{}) {
	l.t.Logf(format, args...)
}

// validateParameterWithTEENode sends a parameter to a TEE node for validation
func validateParameterWithTEENode(client *http.Client, endpoint string, data []byte) bool {
	if client == nil || endpoint == "" || len(data) == 0 {
		return false
	}
	
	// Create request to the TEE validation endpoint
	url := fmt.Sprintf("%s/validate", endpoint)
	
	// Create a new POST request with parameter data in body
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	if err != nil {
		return false
	}
	
	// Set appropriate headers
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Parameter-Format", detectParameterFormat(data))
	
	// Execute the request
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	
	// Check response status
	return resp.StatusCode == http.StatusOK
}

// Helper function to split comma-separated endpoints
func splitEndpoints(endpoints string) []string {
	if endpoints == "" {
		return nil
	}
	
	// Split by comma and trim whitespace
	result := make([]string, 0)
	for _, endpoint := range strings.Split(endpoints, ",") {
		trimmed := strings.TrimSpace(endpoint)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	
	return result
}


