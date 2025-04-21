// RSA Accumulator Service with dual-format parameter validation
package main

import (
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
)

var (
	// Configuration variables with defaults
	port               = "7100"
	supportLengthPrefix = true
	supportDirectFormat = true
	batchSize          = 1000
	parallelism        = 8
	pairID             = "nasdaq-poc-1"
	validatorEndpoint  = "http://localhost:7090" // Default will be overridden per node type
)

// ParseDualFormatParameters handles parameter validation for both formats:
// 1. Length-prefixed format: 4-byte little-endian u32 length prefix followed by data
// 2. Direct data format: no length prefix, raw data
func ParseDualFormatParameters(data []byte, supportLengthPrefix, supportDirectFormat bool) ([]byte, string, error) {
	// First try length-prefixed format if supported
	if supportLengthPrefix && len(data) >= 4 {
		length := binary.LittleEndian.Uint32(data[:4])
		// Validate reasonable length (0 < len <= 1024KB)
		if length > 0 && length <= 1024*1024 {
			if int(length+4) <= len(data) {
				// Successfully parsed length-prefixed format
				log.Printf("Detected length-prefixed format: length=%d", length)
				return data[4:4+length], "length-prefixed", nil
			}
		}
	}
	
	// Fall back to direct format if supported
	if supportDirectFormat {
		log.Printf("Using direct data format: length=%d", len(data))
		return data, "direct", nil
	}
	
	return nil, "", fmt.Errorf("invalid parameter format or unsupported format")
}

// AccumulateHandler processes parameter accumulation requests
func AccumulateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Only POST method is supported", http.StatusMethodNotAllowed)
		return
	}
	
	// Read request body
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading request body: %v", err)
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	
	// Parse parameters with dual-format support
	params, format, err := ParseDualFormatParameters(data, supportLengthPrefix, supportDirectFormat)
	if err != nil {
		log.Printf("Parameter validation error: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	
	log.Printf("Successfully validated %d bytes using %s format", len(params), format)
	
	// In a real implementation, we would accumulate the parameters
	// For now, we just confirm the successful validation
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Successfully validated %d bytes using %s format", len(params), format)
}

// HealthHandler provides a health check endpoint
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "RSA Accumulator Service healthy (port=%s, lengthPrefix=%v, directFormat=%v)",
		port, supportLengthPrefix, supportDirectFormat)
}

func main() {
	// Parse environment variables if provided
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}
	
	if slp := os.Getenv("SUPPORT_LENGTH_PREFIX"); slp != "" {
		supportLengthPrefix = slp == "true"
	}
	
	if sdf := os.Getenv("SUPPORT_DIRECT_FORMAT"); sdf != "" {
		supportDirectFormat = sdf == "true"
	}
	
	if bs := os.Getenv("BATCH_SIZE"); bs != "" {
		if val, err := strconv.Atoi(bs); err == nil && val > 0 {
			batchSize = val
		}
	}
	
	if p := os.Getenv("PARALLELISM"); p != "" {
		if val, err := strconv.Atoi(p); err == nil && val > 0 {
			parallelism = val
		}
	}
	
	if ve := os.Getenv("VALIDATOR_ENDPOINT"); ve != "" {
		validatorEndpoint = ve
	}
	
	if pid := os.Getenv("PAIR_ID"); pid != "" {
		pairID = pid
	}
	
	// Register handlers
	http.HandleFunc("/accumulate", AccumulateHandler)
	http.HandleFunc("/health", HealthHandler)
	
	// Start server
	log.Printf("Starting RSA Accumulator Service with dual-format parameter validation")
	log.Printf("Configuration: port=%s, lengthPrefix=%v, directFormat=%v", port, supportLengthPrefix, supportDirectFormat)
	log.Printf("Performance: batchSize=%d, parallelism=%d", batchSize, parallelism)
	log.Printf("Integration: validatorEndpoint=%s, pairID=%s", validatorEndpoint, pairID)
	
	log.Fatal(http.ListenAndServe(":" + port, nil))
}
