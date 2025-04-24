// Parameter Validator Service
// Replaces the Python implementation with a high-performance Go version
// Supports dual-format parameter validation and cross-validation between TEE types

package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
	
	"github.com/rhombus-tech/vm/coordination"
)

var (
	// Command line flags
	port           = flag.Int("port", 7300, "Port to run the validator service on")
	enableCrossVal = flag.Bool("cross-validate", true, "Enable cross-validation between TEE nodes")
	teeType        = flag.String("tee-type", "sgx", "TEE type (sgx, sev)")
	peerEndpoint   = flag.String("peer-endpoint", "", "Endpoint for peer TEE node for cross-validation")
	maxParamSize   = flag.Int("max-param-size", 1024, "Maximum parameter size in bytes")
	logLevel       = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	
	// RSA Accumulator flags
	sgxAccumulator = flag.String("sgx-accumulator", "localhost:7101", "Endpoint for SGX accumulator proxy")
	sevAccumulator = flag.String("sev-accumulator", "localhost:7101", "Endpoint for SEV accumulator proxy")
	useAccumProxy  = flag.Bool("use-accumulator-proxy", true, "Use the RSA accumulator proxy for validation")
	
	// WebAssembly accumulator flags
	sgxNodeIP      = flag.String("sgx-node-ip", "localhost", "IP address of SGX node")
	sevNodeIP      = flag.String("sev-node-ip", "localhost", "IP address of SEV node")
	accumulatorPort = flag.Int("accumulator-port", 7300, "Port for WebAssembly accumulator")
	batchSize      = flag.Int("batch-size", 10, "Number of parameters per batch")
	batchInterval  = flag.Int("batch-interval", 50, "Batch collection interval in milliseconds")
	enableAccum    = flag.Bool("enable-accumulator", true, "Enable forwarding to WebAssembly accumulator")
)

func main() {
	flag.Parse()

	// Configure logging
	log.SetOutput(os.Stdout)
	log.Printf("Starting parameter validator service")
	log.Printf("TEE Type: %s", *teeType)
	log.Printf("Port: %d", *port)
	log.Printf("Cross-validation: %v", *enableCrossVal)
	log.Printf("Max parameter size: %d bytes", *maxParamSize)
	log.Printf("WebAssembly accumulator integration: %v", *enableAccum)

	// Create validator
	validator := NewParameterValidator()

	// Set up HTTP server
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/validate", validator.validateHandler)
	http.HandleFunc("/cross_validate", validator.crossValidateHandler)
	http.HandleFunc("/stats", validator.statsHandler)
	// Add accumulator health endpoint if the accumulator is enabled
	if validator.accumulator != nil {
		http.HandleFunc("/accumulator_health", validator.accumulatorHealthHandler)
	}

	// Start server
	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Server listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// ParameterValidator handles validation of parameters in dual formats
type ParameterValidator struct {
	stats struct {
		totalRequests          uint64
		successfulValidations  uint64
		failedValidations      uint64
		lengthPrefixedCount    uint64
		directFormatCount      uint64
		crossValidations       uint64
		crossValidationMatches uint64
		averageLatencyMs       float64
	}
	// WebAssembly accumulator connector
	accumulator *coordination.WasmAccumulator
	// New RSA accumulator proxy client
	accumClient *coordination.AccumulatorClient
	useAccumProxy bool
}

// NewParameterValidator creates a new validator
func NewParameterValidator() *ParameterValidator {
	// Initialize validator
	validator := &ParameterValidator{
		useAccumProxy: *useAccumProxy,
	}

	// Create RSA accumulator proxy client if enabled
	if *useAccumProxy {
		accumClient := coordination.NewAccumulatorClient(*sgxAccumulator, *sevAccumulator, *enableCrossVal)

		// Check if accumulator proxy is healthy
		healthy, err := accumClient.HealthCheck()
		if !healthy || err != nil {
			log.Printf("Warning: RSA accumulator proxy is not healthy: %v", err)

			// Fall back to WebAssembly accumulator
			log.Printf("Falling back to WebAssembly accumulator")
			validator.useAccumProxy = false
		} else {
			validator.accumClient = accumClient
			log.Printf("Successfully connected to RSA accumulator proxy")
		}
	}

	// Create WebAssembly accumulator as fallback or if proxy not enabled
	if !validator.useAccumProxy {
		config := &coordination.WasmAccumulatorConfig{
			SGXNodeIP:       *sgxNodeIP,
			SEVNodeIP:       *sevNodeIP,
			AccumulatorPort: *accumulatorPort,
			BatchSize:       *batchSize,
			BatchInterval:   time.Duration(*batchInterval) * time.Millisecond,
			EnableCrossVal:  *enableCrossVal,
		}

		accumulator := coordination.NewWasmAccumulator(config)

		// Check if accumulator is healthy
		healthy, err := accumulator.CheckAccumulatorHealth()
		if !healthy || err != nil {
			log.Printf("Warning: WebAssembly accumulator is not healthy: %v", err)
		}

		validator.accumulator = accumulator
	}

	return validator
}

// validateHandler handles parameter validation requests
func (v *ParameterValidator) validateHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Only accept POST requests
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read parameter data
	data := make([]byte, *maxParamSize+4) // Extra space for potential length prefix
	n, err := r.Body.Read(data)
	if err != nil && err.Error() != "EOF" {
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}
	data = data[:n]

	// Validate the parameter using our dual-format validation
	format, isValid, err := v.validateParameter(data)

	// Track statistics
	v.stats.totalRequests++
	if isValid {
		v.stats.successfulValidations++
		if format == "length-prefixed" {
			v.stats.lengthPrefixedCount++
		} else if format == "direct" {
			v.stats.directFormatCount++
		}

		// If accumulator is enabled, forward validated parameter
		if v.accumulator != nil {
			// Extract any additional parameters from request
			params := make(map[string]interface{})

			// Add request query parameters if any
			for k, v := range r.URL.Query() {
				if len(v) > 0 {
					params[k] = v[0]
				}
			}

			// Add TEE type
			params["tee_type"] = *teeType

			// Forward to accumulator
			_, err = v.accumulator.AddParameter(r.Context(), data, format, params)
			if err != nil {
				log.Printf("Error forwarding to accumulator: %v", err)
			}
		}
	} else {
		v.stats.failedValidations++
	}

	// Calculate latency
	latency := time.Since(startTime).Milliseconds()
	v.stats.averageLatencyMs = (v.stats.averageLatencyMs*float64(v.stats.totalRequests-1) + float64(latency)) / float64(v.stats.totalRequests)

	// Return response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"valid":%t,"format":"%s","size":%d,"timestamp":%d,"latency_ms":%d}`,
		isValid, format, len(data), time.Now().UnixNano(), latency)
}

// crossValidateHandler handles cross-validation requests between different TEE types
func (v *ParameterValidator) crossValidateHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Only accept POST requests
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read parameter data
	data := make([]byte, *maxParamSize+4)
	n, err := r.Body.Read(data)
	if err != nil && err.Error() != "EOF" {
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}
	data = data[:n]

	// Validate locally first
	localFormat, localValid, _ := v.validateParameter(data)

	// Perform cross-validation if enabled and peer endpoint is specified
	peerValid := false
	peerFormat := ""
	crossValidationPerformed := false

	if *enableCrossVal && *peerEndpoint != "" {
		crossValidationPerformed = true
		v.stats.crossValidations++

		// Call peer for validation
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Post(
			fmt.Sprintf("http://%s/validate", *peerEndpoint),
			"application/octet-stream",
			strings.NewReader(string(data)),
		)

		if err == nil {
			defer resp.Body.Close()

			// Parse peer response
			body, err := io.ReadAll(resp.Body)
			if err == nil {
				var peerResponse struct {
					Valid  bool   `json:"valid"`
					Format string `json:"format"`
				}

				if err := json.Unmarshal(body, &peerResponse); err == nil {
					peerValid = peerResponse.Valid
					peerFormat = peerResponse.Format

					// Check if validation results match
					if localValid == peerValid && localFormat == peerFormat {
						v.stats.crossValidationMatches++
					}
				}
			}
		}
	}

	// Calculate latency
	latency := time.Since(startTime).Milliseconds()

	// Calculate match for response formatting
	match := localValid == peerValid && localFormat == peerFormat

	// Return response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"local_valid":%t,"local_format":"%s","cross_validation":%t,"peer_valid":%t,"peer_format":"%s","cross_validation_match":%t,"size":%d,"latency_ms":%d}`,
		localValid, localFormat, crossValidationPerformed, peerValid, peerFormat,
		match, len(data), latency)
}

// validateParameter validates a parameter in dual formats (length-prefixed or direct)
func (p *ParameterValidator) validateParameter(data []byte) (format string, isValid bool, err error) {
	// Track timing for latency calculation
	startTime := time.Now()

	// Use accumulator proxy if enabled
	if p.useAccumProxy && p.accumClient != nil {
		// Use the dual-format validation that auto-detects format
		isValid, format, err = p.accumClient.ValidateDualFormatParameter(data)
		if err != nil {
			// If proxy fails, try falling back to direct WebAssembly accumulator
			if p.accumulator != nil {
				log.Printf("Falling back to WebAssembly accumulator: %v", err)
				return p.fallbackValidation(data)
			}
			return "", false, fmt.Errorf("parameter validation failed: %w", err)
		}
	} else if p.accumulator != nil {
		// Use existing batch accumulator function
		return p.fallbackValidation(data)
	} else {
		return "", false, fmt.Errorf("no parameter validator available")
	}

	// Calculate latency
	latencyMs := float64(time.Since(startTime).Microseconds()) / 1000.0

	// Update statistics
	p.stats.totalRequests++
	if isValid {
		p.stats.successfulValidations++
	} else {
		p.stats.failedValidations++
	}

	if format == "length-prefixed" {
		p.stats.lengthPrefixedCount++
	} else {
		p.stats.directFormatCount++
	}

	// Update average latency with weighted average
	p.stats.averageLatencyMs = (p.stats.averageLatencyMs*0.95 + latencyMs*0.05)

	return format, isValid, nil
}

// fallbackValidation uses the WebAssembly accumulator for validation
func (p *ParameterValidator) fallbackValidation(data []byte) (format string, isValid bool, err error) {
	// Use existing batch accumulator function
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Execute with batch accumulation
	result, err := p.accumulator.AddParameter(ctx, data, "", nil)
	if err != nil {
		return "", false, fmt.Errorf("parameter validation failed: %w", err)
	}

	// Determine format based on the first 4 bytes
	if len(data) >= 4 {
		length := binary.LittleEndian.Uint32(data[:4])
		if length > 0 && length <= uint32(*maxParamSize) && length+4 <= uint32(len(data)) {
			format = "length-prefixed"
		} else {
			format = "direct"
		}
	} else {
		format = "direct"
	}

	return format, result.Success, nil
}

// statsHandler returns validation statistics
func (p *ParameterValidator) statsHandler(w http.ResponseWriter, r *http.Request) {
	// Create combined stats
	stats := map[string]interface{}{
		"total_requests":           p.stats.totalRequests,
		"successful_validations":   p.stats.successfulValidations,
		"failed_validations":       p.stats.failedValidations,
		"length_prefixed_count":    p.stats.lengthPrefixedCount,
		"direct_format_count":      p.stats.directFormatCount,
		"cross_validations":        p.stats.crossValidations,
		"cross_validation_matches": p.stats.crossValidationMatches,
		"average_latency_ms":       p.stats.averageLatencyMs,
		"tee_type":                 *teeType,
		"using_proxy":              p.useAccumProxy,
	}

	// Add specific stats based on which validator is active
	if p.useAccumProxy && p.accumClient != nil {
		// Get stats from accumulator proxy client
		stats["accumulator_proxy"] = p.accumClient.GetStats()
	} else if p.accumulator != nil {
		// Get stats from WebAssembly accumulator
		stats["accumulator"] = p.accumulator.GetStats()
	}

	// Return as JSON
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		log.Printf("Error encoding stats response: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

// healthHandler provides a health check endpoint
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"ok","tee_type":"%s"}`, *teeType)
}

// accumulatorHealthHandler checks the health of the WebAssembly accumulator
func (v *ParameterValidator) accumulatorHealthHandler(w http.ResponseWriter, r *http.Request) {
	if v.accumulator == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, `{"status":"error","message":"WebAssembly accumulator not enabled"}`)
		return
	}
	
	// Check accumulator health
	healthy, err := v.accumulator.CheckAccumulatorHealth()
	
	w.Header().Set("Content-Type", "application/json")
	if err != nil || !healthy {
		w.WriteHeader(http.StatusServiceUnavailable)
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
		}
		fmt.Fprintf(w, `{"status":"error","message":"%s","tee_type":"%s"}`, errMsg, *teeType)
	} else {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"ok","message":"WebAssembly accumulator is healthy","tee_type":"%s"}`, *teeType)
	}
}
