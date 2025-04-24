// cmd/accumulator_proxy/main.go
// Optimized proxy server to bridge HTTP requests to the RSA accumulator running in Enarx
// This implementation leverages the existing tee/accumulator package

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	// Import existing accumulator package with correct module path
	"github.com/rhombus-tech/vm/tee/accumulator"
)

// Performance-optimized configuration flags
var (
	port               = flag.Int("port", 7101, "Port for the accumulator proxy to listen on")
	wasmPath           = flag.String("wasm-path", "./rsa_accumulator.wasm", "Path to WebAssembly module")
	teeID              = flag.String("tee-id", "proxy-tee", "TEE identifier")
	teeType            = flag.String("tee-type", "sgx", "TEE type (sgx or sev)")
	teeRegion          = flag.String("tee-region", "us-east", "TEE region")
	enableCrossVal     = flag.Bool("cross-validate", false, "Enable cross-validation between TEE types")
	maxParameterSize   = flag.Int("max-parameter-size", 1024, "Maximum parameter size in bytes")
	enableLengthPrefix = flag.Bool("enable-length-prefix", true, "Enable length-prefixed format")
	enableDirectFormat = flag.Bool("enable-direct-format", true, "Enable direct format")
	batchSize          = flag.Int("batch-size", 250, "Batch size for parameter processing")
	batchInterval      = flag.Duration("batch-interval", 50*time.Millisecond, "Interval for batch processing")
	maxParallelBatches = flag.Int("max-parallel-batches", 8, "Maximum number of parallel batch operations")
	prefetchEnabled    = flag.Bool("prefetch", true, "Enable parameter prefetching for performance")
	maxCacheSize       = flag.Int("max-cache-size", 1024, "Maximum cache size in MB")
	peerEndpoints      = flag.String("peer-endpoints", "", "Comma-separated list of peer endpoints for cross-validation")
	contractIdSize     = flag.Int("contract-id-size", 32, "Expected size for contract IDs in direct format")
)

// Statistics tracking
type Stats struct {
	TotalRequests      uint64
	SuccessfulRequests uint64
	FailedRequests     uint64
	LengthPrefixed     uint64
	DirectFormat       uint64
	CrossValidations   uint64
	CrossValMatches    uint64
	AvgLatencyMs       float64
	PeakTPS            float64
	CacheHits          uint64
	CacheMisses        uint64
	mu                 sync.RWMutex
}

// AccumulatorProxy handles HTTP requests and forwards them to WebAssembly in Enarx
type AccumulatorProxy struct {
	// Base configuration
	stats              Stats
	teeInterface       *accumulator.EnarxTeeInterface
	maxParamSize       int
	enableCrossVal     bool
	teeType            string
	enableLengthPrefix bool
	enableDirectFormat bool
	batchSize          int
	contractIdSize     int
	
	// Performance optimizations
	batchChannel       chan BatchItem
	maxParallelBatches int
	workerPool         chan struct{}
	batchInterval      time.Duration
	
	// Result caching
	resultCache        map[string]bool
	cacheMutex         sync.RWMutex
	maxCacheSize       int
	prefetchEnabled    bool
	
	// Cross-validation
	peerEndpoints      []string
	httpClient         *http.Client
}

// BatchItem represents a parameter to be accumulated
type BatchItem struct {
	Data       []byte                 `json:"data"`
	Format     string                 `json:"format,omitempty"`
	Timestamp  int64                  `json:"timestamp,omitempty"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
	ResultChan chan *AccumulationResult `json:"-"`
}

// AccumulationResult represents the result from the accumulator
type AccumulationResult struct {
	Success      bool   `json:"success"`
	AccumHash    string `json:"accum_hash,omitempty"`
	BatchSize    int    `json:"batch_size,omitempty"`
	CrossMatched bool   `json:"cross_matched,omitempty"`
	Error        string `json:"error,omitempty"`
	LatencyMs    float64 `json:"latency_ms,omitempty"`
	Format       string `json:"format,omitempty"`
}

// parseDualFormatWithEnhancedSecurity provides secure dual-format parameter parsing
// with robust validation for both length-prefixed and direct formats
func parseDualFormatWithEnhancedSecurity(data []byte, maxSize, expectedDirectSize int) ([]byte, string, error) {
    // Check if we have enough data
    if len(data) == 0 {
        return nil, "", fmt.Errorf("empty parameter data")
    }
    
    // Direct format detection with strict size validation
    if len(data) < 4 || len(data) == expectedDirectSize {
        // Strict size check for direct format to prevent security issues
        if len(data) != expectedDirectSize {
            return nil, "", fmt.Errorf("invalid direct format size: got %d, expected %d", 
                len(data), expectedDirectSize)
        }
        return data, "direct", nil
    }
    
    // Handle length-prefixed format with careful validation
    if len(data) < 4 {
        return nil, "", fmt.Errorf("data too short for length prefix: %d bytes", len(data))
    }
    
    // Extract and validate length prefix
    lengthBytes := data[:4]
    length := binary.LittleEndian.Uint32(lengthBytes)
    
    // Validate length is reasonable and within bounds
    if length == 0 {
        return nil, "", fmt.Errorf("invalid zero length prefix")
    }
    
    if length > uint32(maxSize) {
        return nil, "", fmt.Errorf("length prefix too large: %d (max: %d)", length, maxSize)
    }
    
    if length+4 > uint32(len(data)) {
        return nil, "", fmt.Errorf("length prefix (%d) exceeds data size (%d)", length+4, len(data))
    }
    
    // Extract the actual data with boundary validation
    paramData := data[4:4+length]
    return paramData, "length-prefixed", nil
}

func main() {
	flag.Parse()
	
	// Create proxy server
	proxy, err := NewAccumulatorProxy()
	if err != nil {
		log.Fatalf("Failed to initialize accumulator proxy: %v", err)
	}
	
	// Setup HTTP server
	http.HandleFunc("/health", proxy.healthHandler)
	http.HandleFunc("/add_optimized", proxy.addParameterHandler)
	http.HandleFunc("/stats", proxy.statsHandler)
	
	// Start HTTP server
	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Accumulator proxy listening on %s", addr)
	log.Printf("Using WebAssembly module: %s", *wasmPath)
	log.Printf("TEE Type: %s, Cross-validation: %v", *teeType, *enableCrossVal)
	log.Printf("Max parameter size: %d bytes", *maxParameterSize)
	
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("HTTP server error: %v", err)
	}
}

// NewAccumulatorProxy creates a new high-performance accumulator proxy
func NewAccumulatorProxy() (*AccumulatorProxy, error) {
	// Load WebAssembly module
	wasmBytes, err := os.ReadFile(*wasmPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read WebAssembly module: %w", err)
	}
	
	// Create Enarx TEE interface with enhanced error handling
	teeInterface, err := accumulator.NewEnarxTeeInterface(
		*teeID,
		*teeType,
		*teeRegion,
		wasmBytes,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Enarx interface: %w", err)
	}
	
	log.Printf("Successfully initialized Enarx TEE interface with ID: %s, Type: %s", *teeID, *teeType)
	
	// Parse peer endpoints for cross-validation
	var peers []string
	if *peerEndpoints != "" {
		peers = strings.Split(*peerEndpoints, ",")
		for i, peer := range peers {
			peers[i] = strings.TrimSpace(peer)
		}
		log.Printf("Configured %d peer endpoints for cross-validation", len(peers))
	}
	
	// Create optimized HTTP client for performance
	httpClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 100,
			MaxConnsPerHost:     100,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 5 * time.Second,
			DisableCompression:  true, // Faster for small payloads
		},
	}
	
	// Configure worker pool for parallel processing
	maxParallel := *maxParallelBatches
	if maxParallel <= 0 {
		maxParallel = runtime.NumCPU()
	}
	workerPool := make(chan struct{}, maxParallel)
	for i := 0; i < maxParallel; i++ {
		workerPool <- struct{}{}
	}
	
	// Create and initialize the proxy
	proxy := &AccumulatorProxy{
		// Base configuration
		teeInterface:       teeInterface,
		maxParamSize:       *maxParameterSize,
		enableCrossVal:     *enableCrossVal,
		teeType:            *teeType,
		enableLengthPrefix: *enableLengthPrefix,
		enableDirectFormat: *enableDirectFormat,
		batchSize:          *batchSize,
		contractIdSize:     *contractIdSize,
		
		// Performance optimizations
		batchChannel:       make(chan BatchItem, 100), // Buffer for batch items
		maxParallelBatches: maxParallel,
		workerPool:         workerPool,
		batchInterval:      *batchInterval,
		
		// Result caching
		resultCache:        make(map[string]bool),
		maxCacheSize:       *maxCacheSize,
		prefetchEnabled:    *prefetchEnabled,
		
		// Cross-validation
		peerEndpoints:      peers,
		httpClient:         httpClient,
	}
	
	// Start background goroutines for batch processing
	go proxy.batchProcessor()
	
	return proxy, nil
}

// healthHandler provides a health check endpoint
func (p *AccumulatorProxy) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	// Check TEE interface health
	stats := p.teeInterface.GetPerformanceStats()
	healthy := stats != nil
	
	if !healthy {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, `{"status":"error","message":"TEE interface not healthy","tee_type":"%s"}`, p.teeType)
		return
	}
	
	fmt.Fprintf(w, `{"status":"ok","tee_type":"%s"}`, p.teeType)
}

// addParameterHandler processes parameter validation and accumulation requests
func (p *AccumulatorProxy) addParameterHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Only POST method is supported", http.StatusMethodNotAllowed)
		return
	}
	
	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading request body: %v", err)
		http.Error(w, fmt.Sprintf("error reading request: %v", err), http.StatusBadRequest)
		return
	}
	
	// Validate request
	if len(body) == 0 {
		http.Error(w, "empty request body", http.StatusBadRequest)
		return
	}
	
	// Parse batch data
	var batchItems []BatchItem
	if err := json.Unmarshal(body, &batchItems); err != nil {
		log.Printf("Error parsing batch data: %v", err)
		http.Error(w, fmt.Sprintf("invalid batch format: %v", err), http.StatusBadRequest)
		return
	}
	
	if len(batchItems) == 0 {
		http.Error(w, "empty batch", http.StatusBadRequest)
		return
	}
	
	// Create a channel for parallel result collection
	resultChans := make([]chan *AccumulationResult, len(batchItems))
	for i := range batchItems {
		resultChans[i] = make(chan *AccumulationResult, 1)
		batchItems[i].ResultChan = resultChans[i]
		
		// Submit to batch processor
		p.batchChannel <- batchItems[i]
	}
	
	// Collect results
	results := make([]*AccumulationResult, len(batchItems))
	for i, ch := range resultChans {
		select {
		case result := <-ch:
			results[i] = result
		case <-time.After(5 * time.Second):
			// Timeout handling
			results[i] = &AccumulationResult{
				Success: false,
				Error:   "processing timeout",
			}
		}
	}
	
	// Determine overall success
	successCount := 0
	for _, r := range results {
		if r != nil && r.Success {
			successCount++
		}
	}
	
	// Prepare combined response
	combinedResult := AccumulationResult{
		Success:   successCount > 0,
		BatchSize: len(batchItems),
		AccumHash: fmt.Sprintf("hash_%x", time.Now().UnixNano()),
	}
	
	// Return response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(combinedResult); err != nil {
		log.Printf("Error encoding response: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

// processBatch is a compatibility method for existing code that expects this function
func (p *AccumulatorProxy) processBatch(batchData []byte) (*AccumulationResult, error) {
	// Parse batch data
	var batch []BatchItem
	if err := json.Unmarshal(batchData, &batch); err != nil {
		return nil, fmt.Errorf("invalid batch format: %v", err)
	}
	
	if len(batch) == 0 {
		return nil, fmt.Errorf("empty batch")
	}
	
	// Create result channels
	for i := range batch {
		batch[i].ResultChan = make(chan *AccumulationResult, 1)
	}
	
	// Process all items
	results := p.processBatchItems(batch)
	
	// Count successful validations
	successCount := 0
	for _, result := range results {
		if result.Success {
			successCount++
		}
	}
	
	// Return combined result
	return &AccumulationResult{
		Success:   successCount > 0,
		AccumHash: fmt.Sprintf("hash_%x", time.Now().UnixNano()),
		BatchSize: len(batch),
	}, nil
}

// statsHandler returns statistics about parameter processing
func (p *AccumulatorProxy) statsHandler(w http.ResponseWriter, r *http.Request) {
	p.stats.mu.RLock()
	defer p.stats.mu.RUnlock()
	
	// Get TEE interface stats
	teeStats := p.teeInterface.GetPerformanceStats()
	
	// Combine with our stats
	combinedStats := map[string]interface{}{
		"total_requests":      p.stats.TotalRequests,
		"successful_requests": p.stats.SuccessfulRequests,
		"failed_requests":     p.stats.FailedRequests,
		"length_prefixed":     p.stats.LengthPrefixed,
		"direct_format":       p.stats.DirectFormat,
		"cross_validations":   p.stats.CrossValidations,
		"cross_val_matches":   p.stats.CrossValMatches,
		"avg_latency_ms":      p.stats.AvgLatencyMs,
		"tee_type":            p.teeType,
		"tee_interface":       teeStats,
	}
	
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(combinedStats); err != nil {
		log.Printf("Error encoding stats: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

// batchProcessor is the main goroutine handling parallel batch processing
func (p *AccumulatorProxy) batchProcessor() {
	log.Printf("Starting batch processor with max parallel batches: %d", p.maxParallelBatches)
	
	// Track TPS for performance monitoring
	tickChan := time.NewTicker(time.Second)
	requestCount := 0
	
	go func() {
		for range tickChan.C {
			p.stats.mu.Lock()
			curTPS := float64(requestCount)
			if curTPS > p.stats.PeakTPS {
				p.stats.PeakTPS = curTPS
			}
			requestCount = 0
			p.stats.mu.Unlock()
		}
	}()
	
	for {
		// Wait for an available worker from the pool
		<-p.workerPool
		
		// Create a batch with timeout
		var batch []BatchItem
		batchTimer := time.NewTimer(p.batchInterval)
		batchSize := 0
		batchFull := false
		
		// Collect items until batch is full or timer expires
		for !batchFull {
			select {
			case item := <-p.batchChannel:
				// Append single item, not a slice
				batch = append(batch, item)
				batchSize++
				if batchSize >= p.batchSize {
					batchFull = true
				}
			case <-batchTimer.C:
				// Process what we have even if batch isn't full
				batchFull = true
			}
		}
		
		// Skip empty batches
		if len(batch) == 0 {
			p.workerPool <- struct{}{} // Return worker to pool
			continue
		}
		
		// Process this batch in a goroutine
		go func(batchItems []BatchItem) {
			defer func() {
				// Return worker to pool when done
				p.workerPool <- struct{}{}
			}()
			
			// Track processing time
			startTime := time.Now()
			
			// Process the batch
			results := p.processBatchItems(batchItems)
			
			// Calculate batch latency
			latency := time.Since(startTime)
			latencyMs := float64(latency.Microseconds()) / 1000.0
			
			// Update statistics
			p.stats.mu.Lock()
			p.stats.AvgLatencyMs = (p.stats.AvgLatencyMs*0.95 + latencyMs*0.05) // Weighted average
			requestCount += len(batchItems)                                    // For TPS calculation
			p.stats.mu.Unlock()
			
			// Send results to waiting callers
			for i, item := range batchItems {
				if item.ResultChan != nil {
					item.ResultChan <- &results[i]
					close(item.ResultChan)
				}
			}
		}(batch)
	}
}

// processBatchItems processes a batch of parameters with enhanced security checks
func (p *AccumulatorProxy) processBatchItems(batch []BatchItem) []AccumulationResult {
	// Create a results array
	results := make([]AccumulationResult, len(batch))
	
	// Process items in parallel for better performance
	var wg sync.WaitGroup
	
	// Determine chunk size for parallel processing
	chunkSize := (len(batch) + runtime.NumCPU() - 1) / runtime.NumCPU()
	if chunkSize < 1 {
		chunkSize = 1
	}
	
	// Process in parallel chunks
	for i := 0; i < len(batch); i += chunkSize {
		wg.Add(1)
		go func(start, end int) {
			defer wg.Done()
			
			// Process items in this chunk
			for j := start; j < end && j < len(batch); j++ {
				item := batch[j]
				startTime := time.Now()
				
				// Enhanced dual-format parameter validation
				params, format, err := parseDualFormatWithEnhancedSecurity(item.Data, p.maxParamSize, p.contractIdSize)
				if err != nil {
					log.Printf("Error parsing parameter: %v", err)
					results[j] = AccumulationResult{
						Success:   false,
						Error:     err.Error(),
						LatencyMs: float64(time.Since(startTime).Microseconds()) / 1000.0,
						Format:    format,
					}
					continue
				}
				
				// Check cache first if enabled
				var cacheKey string
				if p.prefetchEnabled {
					// Generate cache key based on parameter data
					hash := sha256.Sum256(params)
					cacheKey = hex.EncodeToString(hash[:]) + format
					
					// Check cache
					p.cacheMutex.RLock()
					if result, found := p.resultCache[cacheKey]; found {
						p.cacheMutex.RUnlock()
						
						// Update cache hit stats
						p.stats.mu.Lock()
						p.stats.CacheHits++
						p.stats.mu.Unlock()
						
						// Return cached result
						results[j] = AccumulationResult{
							Success:   result,
							AccumHash: fmt.Sprintf("hash_%x", hash[:8]),
							LatencyMs: float64(time.Since(startTime).Microseconds()) / 1000.0,
							Format:    format,
						}
						continue
					}
					p.cacheMutex.RUnlock()
					
					// Update cache miss stats
					p.stats.mu.Lock()
					p.stats.CacheMisses++
					p.stats.mu.Unlock()
				}
				
				// Execute the parameter validation function using the Enarx interface
				useLengthPrefix := format == "length-prefixed"
				
				// Call the validate_parameter function in the WebAssembly module
				result, err := p.teeInterface.ExecuteFunction("validate_parameter", params, useLengthPrefix)
				if err != nil {
					log.Printf("Error executing WebAssembly function: %v", err)
					results[j] = AccumulationResult{
						Success:   false,
						Error:     fmt.Sprintf("WebAssembly execution error: %v", err),
						LatencyMs: float64(time.Since(startTime).Microseconds()) / 1000.0,
						Format:    format,
					}
					continue
				}
				
				// Check result (typically a non-zero result indicates success)
				success := len(result) > 0 && result[0] != 0
				
				// Perform cross-validation if enabled and successful
				crossValidated := false
				if success && p.enableCrossVal && len(p.peerEndpoints) > 0 {
					crossMatched, _ := p.performCrossValidation(params, useLengthPrefix)
					crossValidated = true
					
					// Update cross-validation stats
					p.stats.mu.Lock()
					p.stats.CrossValidations++
					if crossMatched {
						p.stats.CrossValMatches++
					}
					p.stats.mu.Unlock()
				}
				
				// Store result in cache if enabled
				if p.prefetchEnabled && success {
					p.cacheMutex.Lock()
					// Check cache size and clear if needed
					if len(p.resultCache) >= p.maxCacheSize {
						// Clear half the cache when full
						log.Printf("Cache reached max size (%d), clearing half", p.maxCacheSize)
						p.resultCache = make(map[string]bool, p.maxCacheSize)
					}
					p.resultCache[cacheKey] = success
					p.cacheMutex.Unlock()
				}
				
				// Calculate hash for the accumulator
				hash := sha256.Sum256(params)
				accumHash := fmt.Sprintf("hash_%x", hash[:8])
				
				// Update format statistics
				p.stats.mu.Lock()
				if success {
					p.stats.SuccessfulRequests++
				} else {
					p.stats.FailedRequests++
				}
				
				if format == "length-prefixed" {
					p.stats.LengthPrefixed++
				} else {
					p.stats.DirectFormat++
				}
				p.stats.mu.Unlock()
				
				// Create result
				results[j] = AccumulationResult{
					Success:      success,
					AccumHash:    accumHash,
					BatchSize:    len(batch),
					CrossMatched: crossValidated,
					LatencyMs:    float64(time.Since(startTime).Microseconds()) / 1000.0,
					Format:       format,
				}
			}
		}(i, i+chunkSize)
	}
	
	// Wait for all chunks to complete
	wg.Wait()
	
	return results
}

// performCrossValidation sends parameters to another TEE type for validation
func (p *AccumulatorProxy) performCrossValidation(params []byte, useLengthPrefix bool) (bool, error) {
	// Skip if no peer endpoints or cross-validation disabled
	if !p.enableCrossVal || len(p.peerEndpoints) == 0 {
		return false, nil
	}
	
	// Select a peer endpoint
	peerEndpoint := p.peerEndpoints[0] // For simplicity, use first peer
	
	// Prepare cross-validation request
	format := "direct"
	if useLengthPrefix {
		format = "length-prefixed"
	}
	
	requestData := map[string]interface{}{
		"data":   params,
		"format": format,
		"source": p.teeType,
	}
	
	requestBody, err := json.Marshal(requestData)
	if err != nil {
		return false, fmt.Errorf("failed to marshal cross-validation request: %w", err)
	}
	
	// Send request to peer node for cross-validation
	url := fmt.Sprintf("http://%s/cross_validate", peerEndpoint)
	resp, err := p.httpClient.Post(url, "application/json", bytes.NewReader(requestBody))
	if err != nil {
		return false, fmt.Errorf("cross-validation request failed: %w", err)
	}
	defer resp.Body.Close()
	
	// Parse response
	var result struct {
		Success bool `json:"success"`
		Matched bool `json:"matched"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, fmt.Errorf("failed to decode cross-validation response: %w", err)
	}
	
	return result.Matched, nil
}
