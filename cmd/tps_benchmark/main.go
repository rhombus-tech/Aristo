// TPS Benchmark Tool for Parameter Validator
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

var (
	endpoint        = flag.String("endpoint", "http://localhost:7301", "Validator endpoint to benchmark")
	concurrency     = flag.Int("concurrency", 100, "Number of concurrent requests")
	duration        = flag.Int("duration", 10, "Test duration in seconds")
	paramSize       = flag.Int("size", 32, "Parameter size in bytes")
	format          = flag.String("format", "mixed", "Parameter format: direct, length-prefixed, or mixed")
	withCrossVal    = flag.Bool("cross-validate", false, "Use cross_validate endpoint instead of validate")
	warmup          = flag.Int("warmup", 2, "Warmup period in seconds")
	printParams     = flag.Bool("print-params", false, "Print parameter samples")
)

func main() {
	flag.Parse()
	
	fmt.Printf("Parameter Validator TPS Benchmark\n")
	fmt.Printf("=================================\n")
	fmt.Printf("Endpoint:      %s\n", *endpoint)
	fmt.Printf("Concurrency:   %d\n", *concurrency)
	fmt.Printf("Duration:      %d seconds\n", *duration)
	fmt.Printf("Parameter:     %d bytes, %s format\n", *paramSize, *format)
	fmt.Printf("Cross-validate: %v\n", *withCrossVal)
	fmt.Printf("=================================\n\n")
	
	// Generate parameter pools
	directParams := generateParameterPool(100, *paramSize, "direct")
	lengthPrefixedParams := generateParameterPool(100, *paramSize, "length-prefixed")
	
	if *printParams {
		fmt.Println("Direct format parameter example:")
		fmt.Printf("  Size: %d bytes\n", len(directParams[0]))
		fmt.Println("  Hex: ", formatHex(directParams[0][:min(16, len(directParams[0]))]) + "...")
		
		fmt.Println("Length-prefixed format parameter example:")
		fmt.Printf("  Size: %d bytes\n", len(lengthPrefixedParams[0]))
		fmt.Printf("  Prefix: %d\n", binary.LittleEndian.Uint32(lengthPrefixedParams[0][:4]))
		fmt.Println("  Hex: ", formatHex(lengthPrefixedParams[0][:min(20, len(lengthPrefixedParams[0]))]) + "...")
		fmt.Println("")
	}
	
	// Warmup
	fmt.Printf("Warming up for %d seconds...\n", *warmup)
	warmupParams := directParams
	if *format == "length-prefixed" {
		warmupParams = lengthPrefixedParams
	}
	
	start := time.Now()
	for time.Since(start) < time.Duration(*warmup)*time.Second {
		for i := 0; i < 10; i++ {
			sendRequest(*endpoint, warmupParams[rand.Intn(len(warmupParams))], *withCrossVal)
		}
	}
	
	// Run the benchmark
	fmt.Println("Starting benchmark...")
	
	var counter uint64
	var wg sync.WaitGroup
	var counterMutex sync.Mutex
	
	startTime := time.Now()
	endTime := startTime.Add(time.Duration(*duration) * time.Second)
	
	// Start worker goroutines
	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			
			// Each worker gets their own client for best performance
			client := &http.Client{
				Timeout: 5 * time.Second,
				Transport: &http.Transport{
					MaxIdleConnsPerHost: 100,
					MaxConnsPerHost:     100,
				},
			}
			
			for time.Now().Before(endTime) {
				// Choose parameter format
				var param []byte
				if *format == "direct" {
					param = directParams[rand.Intn(len(directParams))]
				} else if *format == "length-prefixed" {
					param = lengthPrefixedParams[rand.Intn(len(lengthPrefixedParams))]
				} else {
					// Mixed format
					if rand.Intn(2) == 0 {
						param = directParams[rand.Intn(len(directParams))]
					} else {
						param = lengthPrefixedParams[rand.Intn(len(lengthPrefixedParams))]
					}
				}
				
				// Send request
				err := sendRequestWithClient(client, *endpoint, param, *withCrossVal)
				if err == nil {
					counterMutex.Lock()
					counter++
					counterMutex.Unlock()
				}
			}
		}(i)
	}
	
	// Print progress
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	
	go func() {
		prevCount := uint64(0)
		for {
			<-ticker.C
			if time.Now().After(endTime) {
				break
			}
			
			counterMutex.Lock()
			current := counter
			currentRate := current - prevCount
			prevCount = current
			elapsed := time.Since(startTime).Seconds()
			counterMutex.Unlock()
			
			fmt.Printf("Progress: %.1f sec, Current TPS: %d, Avg TPS: %.1f\n", 
				elapsed, currentRate, float64(current)/elapsed)
		}
	}()
	
	// Wait for all workers to finish
	wg.Wait()
	
	// Calculate and print results
	totalTime := time.Since(startTime).Seconds()
	finalTPS := float64(counter) / totalTime
	
	fmt.Printf("\nBenchmark Results:\n")
	fmt.Printf("Total Requests:  %d\n", counter)
	fmt.Printf("Total Time:      %.2f seconds\n", totalTime)
	fmt.Printf("Average TPS:     %.2f\n", finalTPS)
	
	// Check NASDAQ requirement
	if finalTPS >= 1100 {
		fmt.Printf("\n✅ NASDAQ TPS requirement met (%.2f TPS >= 1100 TPS)\n", finalTPS)
	} else {
		fmt.Printf("\n❌ NASDAQ TPS requirement not met (%.2f TPS < 1100 TPS)\n", finalTPS)
	}
}

// generateParameterPool creates a pool of test parameters
func generateParameterPool(count, size int, format string) [][]byte {
	params := make([][]byte, count)
	
	for i := 0; i < count; i++ {
		data := make([]byte, size)
		rand.Read(data)
		
		if format == "length-prefixed" {
			// Add length prefix (4-byte little-endian u32)
			prefix := make([]byte, 4)
			binary.LittleEndian.PutUint32(prefix, uint32(size))
			params[i] = append(prefix, data...)
		} else {
			params[i] = data
		}
	}
	
	return params
}

// sendRequest sends a validation request to the endpoint
func sendRequest(endpoint string, param []byte, crossValidate bool) error {
	client := &http.Client{Timeout: 5 * time.Second}
	return sendRequestWithClient(client, endpoint, param, crossValidate)
}

// sendRequestWithClient sends a validation request using the provided client
func sendRequestWithClient(client *http.Client, endpoint string, param []byte, crossValidate bool) error {
	// Determine endpoint URL
	url := endpoint + "/validate"
	if crossValidate {
		url = endpoint + "/cross_validate"
	}
	
	// Create request
	req, err := http.NewRequest("POST", url, bytes.NewReader(param))
	if err != nil {
		return err
	}
	
	// Send request
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	
	// Consume response body to properly reuse the connection
	_, err = io.ReadAll(resp.Body)
	return err
}

// formatHex formats a byte slice as hex string
func formatHex(data []byte) string {
	result := ""
	for _, b := range data {
		result += fmt.Sprintf("%02x", b)
	}
	return result
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
