// cmd/verify_parity/main.go
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

var (
	pythonEndpoint = flag.String("python", "http://localhost:7300", "Python validator endpoint")
	goEndpoint     = flag.String("go", "http://localhost:7301", "Go validator endpoint")
	testCount      = flag.Int("count", 100, "Number of test cases to run")
	verbose        = flag.Bool("verbose", false, "Verbose output")
	paramSizes     = []int{32, 64, 128, 256, 512, 1024, 1025} // Various parameter sizes
	formats        = []string{"length_prefixed", "direct"}     // Test both formats
)

type validationResult struct {
	Valid     bool   `json:"valid"`
	Format    string `json:"format"`
	Size      int    `json:"size"`
	Latency   int64  `json:"latency_ms"`
	Timestamp int64  `json:"timestamp"`
}

func main() {
	flag.Parse()
	
	fmt.Println("Parameter Validator Parity Verification")
	fmt.Println("======================================")
	fmt.Printf("Python endpoint: %s\n", *pythonEndpoint)
	fmt.Printf("Go endpoint: %s\n", *goEndpoint)
	fmt.Printf("Test count: %d\n", *testCount)
	fmt.Println("======================================")
	
	rand.Seed(time.Now().UnixNano())
	
	// Track results
	matches := 0
	mismatches := 0
	
	// Run test cases
	for i := 0; i < *testCount; i++ {
		// Generate test parameter
		paramSize := paramSizes[rand.Intn(len(paramSizes))]
		format := formats[rand.Intn(len(formats))]
		param := generateParameter(paramSize, format)
		
		// Test both implementations
		pyResult, err := testEndpoint(*pythonEndpoint, param)
		if err != nil {
			log.Printf("Error with Python endpoint: %v\n", err)
			continue
		}
		
		goResult, err := testEndpoint(*goEndpoint, param)
		if err != nil {
			log.Printf("Error with Go endpoint: %v\n", err)
			continue
		}
		
		// Compare results
		if pyResult.Valid == goResult.Valid && pyResult.Format == goResult.Format {
			matches++
			if *verbose {
				fmt.Printf("[✓] Test %d: Both implementations agree (size=%d, format=%s)\n", 
					i+1, paramSize, format)
			}
		} else {
			mismatches++
			fmt.Printf("[✗] Test %d: MISMATCH detected!\n", i+1)
			fmt.Printf("    Parameter: size=%d, intended format=%s\n", paramSize, format)
			fmt.Printf("    Python result: valid=%v, format=%s\n", pyResult.Valid, pyResult.Format)
			fmt.Printf("    Go result: valid=%v, format=%s\n", goResult.Valid, goResult.Format)
		}
	}
	
	// Report final results
	fmt.Println("======================================")
	fmt.Printf("Tests completed: %d\n", *testCount)
	fmt.Printf("Matches: %d (%.1f%%)\n", matches, float64(matches)*100/float64(*testCount))
	fmt.Printf("Mismatches: %d (%.1f%%)\n", mismatches, float64(mismatches)*100/float64(*testCount))
	
	// Performance comparison
	fmt.Println("\nPerformance Comparison:")
	benchmarkEndpoints(*pythonEndpoint, *goEndpoint)
}

// generateParameter creates a test parameter with the specified size and format
func generateParameter(size int, format string) []byte {
	data := make([]byte, size)
	rand.Read(data)
	
	if format == "length_prefixed" {
		// Add length prefix (4-byte little-endian u32)
		prefix := make([]byte, 4)
		// Adjust size if it exceeds limit
		if size > 1024 {
			size = 1024 // Truncate to max limit
		}
		prefix[0] = byte(size)
		prefix[1] = byte(size >> 8)
		prefix[2] = byte(size >> 16)
		prefix[3] = byte(size >> 24)
		
		return append(prefix, data[:size]...)
	} else {
		// Return direct format
		return data
	}
}

// testEndpoint sends a parameter to an endpoint and returns the validation result
func testEndpoint(endpoint string, param []byte) (*validationResult, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	
	// Create request
	url := strings.TrimSuffix(endpoint, "/") + "/validate"
	req, err := http.NewRequest("POST", url, bytes.NewReader(param))
	if err != nil {
		return nil, err
	}
	
	// Send request
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	
	// Parse response
	var result validationResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("error parsing response: %v (response: %s)", err, string(body))
	}
	
	return &result, nil
}

// benchmarkEndpoints runs a performance comparison between Python and Go endpoints
func benchmarkEndpoints(pythonEndpoint, goEndpoint string) {
	// Generate test parameters for each format
	testParams := []struct {
		name   string
		param  []byte
		format string
	}{
		{"Small Direct (32B)", generateParameter(32, "direct"), "direct"},
		{"Medium Direct (256B)", generateParameter(256, "direct"), "direct"},
		{"Small Length-Prefixed (32B)", generateParameter(32, "length_prefixed"), "length_prefixed"},
		{"Medium Length-Prefixed (256B)", generateParameter(256, "length_prefixed"), "length_prefixed"},
		{"Large Length-Prefixed (1KB)", generateParameter(1024, "length_prefixed"), "length_prefixed"},
	}
	
	// Run benchmarks
	for _, test := range testParams {
		fmt.Printf("\nBenchmark: %s\n", test.name)
		
		// Warm-up
		for i := 0; i < 10; i++ {
			testEndpoint(pythonEndpoint, test.param)
			testEndpoint(goEndpoint, test.param)
		}
		
		// Python benchmark
		pyTimes := make([]int64, 0, 50)
		pyStart := time.Now()
		for i := 0; i < 50; i++ {
			result, err := testEndpoint(pythonEndpoint, test.param)
			if err == nil {
				pyTimes = append(pyTimes, result.Latency)
			}
		}
		pyElapsed := time.Since(pyStart)
		
		// Go benchmark
		goTimes := make([]int64, 0, 50)
		goStart := time.Now()
		for i := 0; i < 50; i++ {
			result, err := testEndpoint(goEndpoint, test.param)
			if err == nil {
				goTimes = append(goTimes, result.Latency)
			}
		}
		goElapsed := time.Since(goStart)
		
		// Calculate averages
		pyAvg := calculateAverage(pyTimes)
		goAvg := calculateAverage(goTimes)
		speedup := float64(pyAvg) / float64(goAvg)
		
		// Report results
		fmt.Printf("  Python: %.2f ms (%.2f ms per validation)\n", 
			float64(pyElapsed.Milliseconds())/50, pyAvg)
		fmt.Printf("  Go:     %.2f ms (%.2f ms per validation)\n", 
			float64(goElapsed.Milliseconds())/50, goAvg)
		fmt.Printf("  Speedup: %.1fx\n", speedup)
	}
}

// calculateAverage calculates the average of a slice of int64 values
func calculateAverage(values []int64) float64 {
	if len(values) == 0 {
		return 0
	}
	
	var sum int64
	for _, v := range values {
		sum += v
	}
	
	return float64(sum) / float64(len(values))
}
