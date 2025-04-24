// coordination/wasm_connector.go
package coordination

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	// AccumulatorPort is the port for the WebAssembly accumulator
	AccumulatorPort = 7300
	
	// BatchSize is the number of attestations per batch
	DefaultBatchSize = 10
	
	// BatchInterval is the collection interval for batching
	DefaultBatchInterval = 50 * time.Millisecond
)

// WasmAccumulatorConfig configures the WebAssembly accumulator connector
type WasmAccumulatorConfig struct {
	SGXNodeIP       string
	SEVNodeIP       string
	AccumulatorPort int
	BatchSize       int
	BatchInterval   time.Duration
	EnableCrossVal  bool
}

// BatchItem represents a single item in a batch for accumulation
type BatchItem struct {
	Data       []byte
	Format     string
	Timestamp  time.Time
	Parameters map[string]interface{}
	Result     chan *AccumulationResult
}

// AccumulationResult represents the result of an accumulation operation
type AccumulationResult struct {
	Success      bool
	AccumHash    string
	Error        error
	Latency      time.Duration
	CrossMatched bool
}

// WasmAccumulator provides integration with the WebAssembly-based RSA accumulator
type WasmAccumulator struct {
	// Configuration
	config *WasmAccumulatorConfig
	
	// BatchProcessor
	currentBatch     []*BatchItem
	batchLock        sync.Mutex
	processing       bool
	lastBatchTime    time.Time
	batchProcessChan chan []*BatchItem
	
	// Statistics
	totalProcessed   uint64
	successCount     uint64
	crossValCount    uint64
	crossValMatches  uint64
	avgLatencyMs     float64
	statsMu          sync.RWMutex
	
	// HTTP client for accumulator communication
	client *http.Client
}

// NewWasmAccumulator creates a new WebAssembly accumulator connector
func NewWasmAccumulator(config *WasmAccumulatorConfig) *WasmAccumulator {
	if config == nil {
		config = &WasmAccumulatorConfig{
			SGXNodeIP:       "localhost",
			SEVNodeIP:       "localhost",
			AccumulatorPort: AccumulatorPort,
			BatchSize:       DefaultBatchSize,
			BatchInterval:   DefaultBatchInterval,
			EnableCrossVal:  true,
		}
	}
	
	acc := &WasmAccumulator{
		config:           config,
		currentBatch:     make([]*BatchItem, 0, config.BatchSize),
		batchProcessChan: make(chan []*BatchItem, 10),
		lastBatchTime:    time.Now(),
		client: &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				MaxIdleConnsPerHost: 100,
				MaxConnsPerHost:     100,
			},
		},
	}
	
	// Start batch processor goroutine
	go acc.batchProcessor()
	
	// Start batch scheduler goroutine
	go acc.batchScheduler()
	
	return acc
}

// AddParameter adds a parameter to the accumulator
func (acc *WasmAccumulator) AddParameter(ctx context.Context, data []byte, format string, params map[string]interface{}) (*AccumulationResult, error) {
	resultChan := make(chan *AccumulationResult, 1)
	
	item := &BatchItem{
		Data:       data,
		Format:     format,
		Timestamp:  time.Now(),
		Parameters: params,
		Result:     resultChan,
	}
	
	// Add to batch
	acc.addToBatch(item)
	
	// Wait for result or context cancellation
	select {
	case result := <-resultChan:
		return result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// addToBatch adds an item to the current batch
func (acc *WasmAccumulator) addToBatch(item *BatchItem) {
	acc.batchLock.Lock()
	defer acc.batchLock.Unlock()
	
	// Add to current batch
	acc.currentBatch = append(acc.currentBatch, item)
	
	// Process immediately if batch is full
	if len(acc.currentBatch) >= acc.config.BatchSize && !acc.processing {
		batch := acc.currentBatch
		acc.currentBatch = make([]*BatchItem, 0, acc.config.BatchSize)
		acc.processing = true
		acc.batchProcessChan <- batch
	}
}

// batchScheduler periodically processes accumulated parameters
func (acc *WasmAccumulator) batchScheduler() {
	ticker := time.NewTicker(acc.config.BatchInterval)
	defer ticker.Stop()
	
	for range ticker.C {
		acc.batchLock.Lock()
		if len(acc.currentBatch) > 0 && !acc.processing && 
		   time.Since(acc.lastBatchTime) >= acc.config.BatchInterval {
			batch := acc.currentBatch
			acc.currentBatch = make([]*BatchItem, 0, acc.config.BatchSize)
			acc.processing = true
			acc.lastBatchTime = time.Now()
			acc.batchProcessChan <- batch
		}
		acc.batchLock.Unlock()
	}
}

// batchProcessor processes batches from the channel
func (acc *WasmAccumulator) batchProcessor() {
	for batch := range acc.batchProcessChan {
		// Process the batch
		acc.processBatch(batch)
		
		// Reset processing flag
		acc.batchLock.Lock()
		acc.processing = false
		acc.batchLock.Unlock()
	}
}

// processBatch processes a batch of parameters
func (acc *WasmAccumulator) processBatch(batch []*BatchItem) {
	if len(batch) == 0 {
		return
	}
	
	// Prepare batch for accumulator
	batchData := make([]map[string]interface{}, len(batch))
	for i, item := range batch {
		batchData[i] = map[string]interface{}{
			"data":       item.Data,
			"format":     item.Format,
			"timestamp":  item.Timestamp.UnixNano(),
			"parameters": item.Parameters,
		}
	}
	
	// Marshal batch data
	batchJSON, err := json.Marshal(batchData)
	if err != nil {
		// Handle error
		for _, item := range batch {
			item.Result <- &AccumulationResult{
				Success: false,
				Error:   fmt.Errorf("batch marshal error: %w", err),
			}
		}
		return
	}
	
	// Send to accumulator (SGX node)
	sgxURL := fmt.Sprintf("http://%s:%d/add_optimized", acc.config.SGXNodeIP, acc.config.AccumulatorPort)
	startTime := time.Now()
	
	resp, err := acc.client.Post(sgxURL, "application/json", bytes.NewReader(batchJSON))
	if err != nil {
		// Handle error
		for _, item := range batch {
			item.Result <- &AccumulationResult{
				Success: false,
				Error:   fmt.Errorf("accumulator request error: %w", err),
			}
		}
		return
	}
	defer resp.Body.Close()
	
	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		// Handle error
		for _, item := range batch {
			item.Result <- &AccumulationResult{
				Success: false,
				Error:   fmt.Errorf("accumulator response error: %w", err),
			}
		}
		return
	}
	
	// Parse response
	var response struct {
		Success      bool   `json:"success"`
		AccumHash    string `json:"accum_hash"`
		BatchSize    int    `json:"batch_size"`
		CrossMatched bool   `json:"cross_matched"`
		Latency      int64  `json:"latency_ms"`
	}
	
	if err := json.Unmarshal(respBody, &response); err != nil {
		// Handle error
		for _, item := range batch {
			item.Result <- &AccumulationResult{
				Success: false,
				Error:   fmt.Errorf("accumulator response parse error: %w", err),
			}
		}
		return
	}
	
	// Update statistics
	acc.updateStats(len(batch), response.Success, response.CrossMatched, time.Since(startTime))
	
	// Return results to callers
	result := &AccumulationResult{
		Success:      response.Success,
		AccumHash:    response.AccumHash,
		Latency:      time.Duration(response.Latency) * time.Millisecond,
		CrossMatched: response.CrossMatched,
	}
	
	for _, item := range batch {
		item.Result <- result
	}
}

// updateStats updates the accumulator statistics
func (acc *WasmAccumulator) updateStats(count int, success bool, crossMatched bool, latency time.Duration) {
	acc.statsMu.Lock()
	defer acc.statsMu.Unlock()
	
	acc.totalProcessed += uint64(count)
	if success {
		acc.successCount += uint64(count)
	}
	
	if acc.config.EnableCrossVal {
		acc.crossValCount += uint64(count)
		if crossMatched {
			acc.crossValMatches += uint64(count)
		}
	}
	
	// Update average latency
	latencyMs := float64(latency.Milliseconds())
	acc.avgLatencyMs = (acc.avgLatencyMs*float64(acc.totalProcessed-uint64(count)) + latencyMs*float64(count)) / float64(acc.totalProcessed)
}

// GetStats returns accumulator statistics
func (acc *WasmAccumulator) GetStats() map[string]interface{} {
	acc.statsMu.RLock()
	defer acc.statsMu.RUnlock()
	
	crossValRate := float64(0)
	if acc.crossValCount > 0 {
		crossValRate = float64(acc.crossValMatches) / float64(acc.crossValCount)
	}
	
	return map[string]interface{}{
		"total_processed":      acc.totalProcessed,
		"success_count":        acc.successCount,
		"cross_val_count":      acc.crossValCount,
		"cross_val_matches":    acc.crossValMatches,
		"cross_val_rate":       crossValRate,
		"avg_latency_ms":       acc.avgLatencyMs,
		"batch_size":           acc.config.BatchSize,
		"batch_interval_ms":    acc.config.BatchInterval.Milliseconds(),
		"enable_cross_val":     acc.config.EnableCrossVal,
	}
}

// CheckAccumulatorHealth checks the health of the WebAssembly accumulator
func (acc *WasmAccumulator) CheckAccumulatorHealth() (bool, error) {
	// Check SGX node
	sgxURL := fmt.Sprintf("http://%s:%d/health", acc.config.SGXNodeIP, acc.config.AccumulatorPort)
	resp, err := acc.client.Get(sgxURL)
	if err != nil {
		return false, fmt.Errorf("SGX accumulator health check failed: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("SGX accumulator returned non-OK status: %d", resp.StatusCode)
	}
	
	// If cross-validation is enabled, also check SEV node
	if acc.config.EnableCrossVal {
		sevURL := fmt.Sprintf("http://%s:%d/health", acc.config.SEVNodeIP, acc.config.AccumulatorPort)
		resp, err := acc.client.Get(sevURL)
		if err != nil {
			return false, fmt.Errorf("SEV accumulator health check failed: %w", err)
		}
		defer resp.Body.Close()
		
		if resp.StatusCode != http.StatusOK {
			return false, fmt.Errorf("SEV accumulator returned non-OK status: %d", resp.StatusCode)
		}
	}
	
	return true, nil
}
