// Package coordination provides integration with the RSA accumulator proxy
// This client specifically supports dual-format parameter validation

package coordination

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// AccumulatorClient provides a client for the RSA accumulator proxy
// with support for dual-format parameter validation
type AccumulatorClient struct {
	// Endpoints
	sgxEndpoint string
	sevEndpoint string
	
	// Configuration
	enableCrossVal bool
	maxParamSize   int
	contractIdSize int
	
	// HTTP client
	httpClient *http.Client
	
	// Statistics
	stats struct {
		requests        uint64
		successful      uint64
		failed          uint64
		lengthPrefixed  uint64
		directFormat    uint64
		crossValidation uint64
		avgLatencyMs    float64
		mu              sync.RWMutex
	}
}

// ValidationRequest represents a parameter validation request
type ValidationRequest struct {
	Data      []byte                 `json:"data"`
	Format    string                 `json:"format,omitempty"`
	Timestamp int64                  `json:"timestamp,omitempty"`
	Params    map[string]interface{} `json:"parameters,omitempty"`
}

// ValidationResponse represents a validation response from the accumulator
type ValidationResponse struct {
	Success      bool   `json:"success"`
	AccumHash    string `json:"accum_hash,omitempty"`
	BatchSize    int    `json:"batch_size,omitempty"`
	CrossMatched bool   `json:"cross_matched,omitempty"`
	Error        string `json:"error,omitempty"`
}

// NewAccumulatorClient creates a new client for the RSA accumulator proxy
func NewAccumulatorClient(sgxEndpoint, sevEndpoint string, enableCrossVal bool) *AccumulatorClient {
	return &AccumulatorClient{
		sgxEndpoint:    sgxEndpoint,
		sevEndpoint:    sevEndpoint,
		enableCrossVal: enableCrossVal,
		maxParamSize:   1024,
		contractIdSize: 32,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				MaxIdleConnsPerHost: 100,
				MaxConnsPerHost:     100,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// ValidateParameter validates a parameter with the RSA accumulator proxy
// It supports both direct and length-prefixed parameter formats
func (c *AccumulatorClient) ValidateParameter(data []byte, useLengthPrefix bool) (bool, error) {
	// Determine format based on the flag
	format := "direct"
	if useLengthPrefix {
		format = "length-prefixed"
	}
	
	// Create validation request
	request := []ValidationRequest{
		{
			Data:      data,
			Format:    format,
			Timestamp: time.Now().UnixNano(),
		},
	}
	
	// Marshal the request
	requestBody, err := json.Marshal(request)
	if err != nil {
		return false, fmt.Errorf("failed to marshal validation request: %w", err)
	}
	
	// Send request to the accumulator proxy
	endpoint := c.sgxEndpoint
	if c.enableCrossVal {
		// Alternate between SGX and SEV for better distribution
		if time.Now().UnixNano()%2 == 0 {
			endpoint = c.sevEndpoint
		}
	}
	
	// Update statistics
	c.stats.mu.Lock()
	c.stats.requests++
	if useLengthPrefix {
		c.stats.lengthPrefixed++
	} else {
		c.stats.directFormat++
	}
	c.stats.mu.Unlock()
	
	// Record timing for latency calculation
	startTime := time.Now()
	
	// Send request
	resp, err := c.httpClient.Post(
		fmt.Sprintf("http://%s/add_optimized", endpoint),
		"application/json",
		bytes.NewReader(requestBody),
	)
	if err != nil {
		// If primary endpoint fails and cross-validation is enabled, try the other endpoint
		if c.enableCrossVal {
			altEndpoint := c.sevEndpoint
			if endpoint == c.sevEndpoint {
				altEndpoint = c.sgxEndpoint
			}
			
			resp, err = c.httpClient.Post(
				fmt.Sprintf("http://%s/add_optimized", altEndpoint),
				"application/json",
				bytes.NewReader(requestBody),
			)
			if err != nil {
				return false, fmt.Errorf("both validation endpoints failed: %w", err)
			}
		} else {
			return false, fmt.Errorf("validation request failed: %w", err)
		}
	}
	defer resp.Body.Close()
	
	// Calculate latency
	latencyMs := float64(time.Since(startTime).Microseconds()) / 1000.0
	
	// Parse response
	var response ValidationResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return false, fmt.Errorf("failed to decode validation response: %w", err)
	}
	
	// Update statistics
	c.stats.mu.Lock()
	c.stats.avgLatencyMs = (c.stats.avgLatencyMs*0.95 + latencyMs*0.05) // Weighted average
	if response.Success {
		c.stats.successful++
	} else {
		c.stats.failed++
	}
	if response.CrossMatched {
		c.stats.crossValidation++
	}
	c.stats.mu.Unlock()
	
	return response.Success, nil
}

// ValidateDualFormatParameter auto-detects and validates a parameter in either format
// This is the recommended method for most use cases to handle both parameter formats
func (c *AccumulatorClient) ValidateDualFormatParameter(data []byte) (bool, string, error) {
	// Auto-detect format
	format := "direct"
	useLengthPrefix := false
	
	// Check for length prefix (first 4 bytes represent a little-endian u32 length)
	if len(data) >= 4 {
		length := binary.LittleEndian.Uint32(data[:4])
		
		// If length is reasonable (non-zero, not too large, and not exceeding data size)
		if length > 0 && length <= uint32(c.maxParamSize) && length+4 <= uint32(len(data)) {
			format = "length-prefixed"
			useLengthPrefix = true
		}
	}
	
	// If direct format, verify expected size
	if format == "direct" && len(data) != c.contractIdSize {
		return false, format, fmt.Errorf(
			"invalid direct format size: got %d bytes, expected %d bytes",
			len(data), c.contractIdSize,
		)
	}
	
	// Validate with the detected format
	valid, err := c.ValidateParameter(data, useLengthPrefix)
	return valid, format, err
}

// GetStats returns statistics about parameter validation
func (c *AccumulatorClient) GetStats() map[string]interface{} {
	c.stats.mu.RLock()
	defer c.stats.mu.RUnlock()
	
	return map[string]interface{}{
		"requests":         c.stats.requests,
		"successful":       c.stats.successful,
		"failed":           c.stats.failed,
		"length_prefixed":  c.stats.lengthPrefixed,
		"direct_format":    c.stats.directFormat,
		"cross_validation": c.stats.crossValidation,
		"avg_latency_ms":   c.stats.avgLatencyMs,
	}
}

// HealthCheck checks if the accumulator proxy is healthy
func (c *AccumulatorClient) HealthCheck() (bool, error) {
	// Check primary endpoint
	primaryEndpoint := c.sgxEndpoint
	resp, err := c.httpClient.Get(fmt.Sprintf("http://%s/health", primaryEndpoint))
	if err == nil && resp.StatusCode == http.StatusOK {
		resp.Body.Close()
		return true, nil
	}
	
	// If cross-validation is enabled, try the other endpoint
	if c.enableCrossVal {
		secondaryEndpoint := c.sevEndpoint
		resp, err := c.httpClient.Get(fmt.Sprintf("http://%s/health", secondaryEndpoint))
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return true, nil
		}
	}
	
	return false, fmt.Errorf("all accumulator endpoints are unhealthy")
}
