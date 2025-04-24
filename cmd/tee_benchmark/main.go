// cmd/tee_benchmark/main.go
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/rhombus-tech/vm/coordination/tests"
)

var (
	// Command-line flags for TEE benchmark configuration
	teeEndpoints      = flag.String("tee-endpoints", "", "Comma-separated list of TEE node endpoints")
	sgxEndpoints      = flag.String("sgx-endpoints", "", "Comma-separated list of SGX node endpoints")
	sevEndpoints      = flag.String("sev-endpoints", "", "Comma-separated list of SEV node endpoints")
	region            = flag.String("region", "us-east-1", "AWS region where TEE nodes are deployed")
	concurrency       = flag.Int("concurrency", 100, "Number of concurrent validation requests")
	paramCount        = flag.Int("param-count", 10000, "Number of parameters to validate")
	paramSize         = flag.Int("param-size", 256, "Size of test parameters in bytes")
	crossValidate     = flag.Bool("cross-validate", true, "Enable cross-validation between TEE nodes")
	crossValidateRate = flag.Float64("cross-validate-rate", 0.2, "Percentage of parameters to cross-validate")
	format            = flag.String("format", "mixed", "Parameter format: 'length-prefixed', 'direct', or 'mixed'")
)

// splitEndpoints splits a comma-separated list of endpoints
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

// printResults prints the benchmark results
func printResults(result *tests.TEEBenchmarkResult) {
	fmt.Println("\n===== TEE Benchmark Results =====")
	fmt.Printf("Duration: %.2f seconds\n", result.Duration.Seconds())
	fmt.Printf("Parameters Processed: %d (success: %d, failure: %d)\n",
		result.SuccessCount+result.FailureCount, result.SuccessCount, result.FailureCount)
	
	if result.SuccessCount > 0 {
		avgTimeMs := float64(result.TotalTimeNs) / float64(result.SuccessCount) / 1e6
		fmt.Printf("Average Processing Time: %.3f ms per parameter\n", avgTimeMs)
	}
	
	fmt.Printf("Throughput: %.2f TPS (parameters per second)\n", result.TPS)
	
	// Report format distribution
	if result.SuccessCount > 0 {
		lpPct := float64(result.LengthPrefixedCount) / float64(result.SuccessCount) * 100
		dirPct := float64(result.DirectFormatCount) / float64(result.SuccessCount) * 100
		fmt.Printf("Format Distribution: Length-Prefixed: %d (%.1f%%), Direct: %d (%.1f%%)\n",
			result.LengthPrefixedCount, lpPct, result.DirectFormatCount, dirPct)
	}
	
	// Report cross-validation stats
	if result.CrossValidated > 0 {
		cvPct := float64(result.CrossValidated) / float64(result.SuccessCount) * 100
		fmt.Printf("Cross-Validated: %d (%.1f%%)\n", result.CrossValidated, cvPct)
	}
	
	// Report success or failure against target
	targetTPS := 1100.0
	if result.TPS >= targetTPS {
		fmt.Printf("✅ PASSED: Achieved %.2f TPS, exceeding target of %.0f TPS\n", result.TPS, targetTPS)
	} else {
		fmt.Printf("❌ FAILED: Achieved only %.2f TPS, below target of %.0f TPS\n", result.TPS, targetTPS)
	}
}

// benchmarkLogger implements the tests.Logger interface for command line output
type benchmarkLogger struct{}

func (l *benchmarkLogger) Logf(format string, args ...interface{}) {
	log.Printf(format, args...)
}

func main() {
	log.Println("TEE Benchmark Tool - Testing Dual-Format Parameter Validation")
	log.Println("===========================================================")
	
	// Parse command line flags
	flag.Parse()
	
	// Configure benchmark
	config := &tests.TEEBenchmarkConfig{
		ParamCount:        *paramCount,
		ParamSize:         *paramSize,
		Concurrency:       *concurrency,
		CrossValidate:     *crossValidate,
		CrossValidateRate: *crossValidateRate,
	}
	
	// Parse TEE endpoints
	config.SGXEndpoints = splitEndpoints(*sgxEndpoints)
	config.SEVEndpoints = splitEndpoints(*sevEndpoints)
	config.GenericEndpoints = splitEndpoints(*teeEndpoints)
	
	// Check if we have any endpoints to test with
	totalEndpoints := len(config.SGXEndpoints) + len(config.SEVEndpoints) + len(config.GenericEndpoints)
	if totalEndpoints == 0 {
		log.Fatal("No TEE endpoints provided. Please specify at least one endpoint using --sgx-endpoints, --sev-endpoints, or --tee-endpoints")
	}
	
	// Display benchmark configuration
	log.Printf("Parameter Format: %s", *format)
	log.Printf("Parameter Size: %d bytes", *paramSize)
	log.Printf("Parameter Count: %d", *paramCount)
	log.Printf("Concurrency: %d", *concurrency)
	log.Printf("Cross-Validation: %v (Rate: %.1f%%)", *crossValidate, *crossValidateRate*100)
	log.Printf("SGX Endpoints: %v", config.SGXEndpoints)
	log.Printf("SEV Endpoints: %v", config.SEVEndpoints)
	log.Printf("Generic TEE Endpoints: %v", config.GenericEndpoints)
	
	// Create a simple logger for the benchmark
	logger := &benchmarkLogger{}
	
	// Start the benchmark
	log.Println("Starting benchmark...")
	startTime := time.Now()
	
	// Run the benchmark
	result := tests.RunTEEBenchmark(logger, config)
	
	duration := time.Since(startTime)
	log.Printf("Benchmark completed in %.2f seconds", duration.Seconds())
	
	// Print results
	printResults(result)
	
	// Exit with appropriate status code
	if result.TPS < 1100.0 {
		os.Exit(1)
	}
}
