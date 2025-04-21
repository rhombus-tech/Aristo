// Simple RSA Accumulator with dual-format parameter support
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

// Configuration variables
var (
	port               = "7100"
	supportLengthPrefix = true
	supportDirectFormat = true
	parallelism        = 8
	batchSize          = 1000
)

// ParseDualFormatParameters handles both length-prefixed and direct data formats
func ParseDualFormatParameters(data []byte, supportLengthPrefix, supportDirectFormat bool) ([]byte, string, error) {
	// Try length-prefixed format first if supported
	if supportLengthPrefix && len(data) >= 4 {
		length := binary.LittleEndian.Uint32(data[:4])
		if length > 0 && length <= 1024*1024 {
			if len(data) >= int(4+length) {
				return data[4:4+length], "length-prefixed", nil
			}
		}
	}
	
	// Fall back to direct format if supported
	if supportDirectFormat {
		return data, "direct", nil
	}
	
	return nil, "", fmt.Errorf("invalid parameter format")
}

// AccumulateHandler processes accumulation requests
func AccumulateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Only POST method is supported", http.StatusMethodNotAllowed)
		return
	}
	
	// Read request body
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	
	// Parse parameters using dual-format support
	params, format, err := ParseDualFormatParameters(data, supportLengthPrefix, supportDirectFormat)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	
	log.Printf("Received %d bytes in %s format", len(params), format)
	
	// For a real implementation, we would accumulate the parameters
	// but for this prototype, we just confirm receipt
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Successfully accumulated %d bytes in %s format", len(params), format)
}

func main() {
	// Parse environment variables if present
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}
	
	if slp := os.Getenv("SUPPORT_LENGTH_PREFIX"); slp != "" {
		if val, err := strconv.ParseBool(slp); err == nil {
			supportLengthPrefix = val
		}
	}
	
	if sdf := os.Getenv("SUPPORT_DIRECT_FORMAT"); sdf != "" {
		if val, err := strconv.ParseBool(sdf); err == nil {
			supportDirectFormat = val
		}
	}
	
	if p := os.Getenv("PARALLELISM"); p != "" {
		if val, err := strconv.Atoi(p); err == nil {
			parallelism = val
		}
	}
	
	if bs := os.Getenv("BATCH_SIZE"); bs != "" {
		if val, err := strconv.Atoi(bs); err == nil {
			batchSize = val
		}
	}
	
	// Register handlers
	http.HandleFunc("/accumulate", AccumulateHandler)
	
	// Start server
	log.Printf("Starting RSA accumulator service on port %s with config: lengthPrefix=%v, directFormat=%v, parallelism=%d, batchSize=%d",
		port, supportLengthPrefix, supportDirectFormat, parallelism, batchSize)
		
	log.Fatal(http.ListenAndServe(":" + port, nil))
}
