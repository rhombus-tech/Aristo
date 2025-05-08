// Copyright (C) 2025, Rhombus Technologies. All rights reserved.
// See the file LICENSE for licensing terms.

package archive

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"
	"time"
)

const (
	// Default constants for mesh network integration
	DefaultConnectionTimeout  = 30 * time.Second
	DefaultMaxRetries         = 3
	DefaultRegionalPreference = true
	DefaultCacheTTL           = 10 * time.Minute
)

// Supported TEE types
type TEEType string

const (
	TEETypeIntelSGX TEEType = "IntelSGX"
	TEETypeSEV      TEEType = "SEV"
	TEETypeTDX      TEEType = "TDX"
)

// MeshClient connects to the TEE mesh network and provides cross-attestation capabilities
type MeshClient struct {
	// Endpoint for the mesh coordinator
	meshEndpoint string
	
	// Default region to use
	defaultRegion string
	
	// Client configuration
	config MeshClientConfig
	
	// HTTP client with timeouts
	client *http.Client
	
	// Cache for attestation results
	attestationCache     map[string]AttestationCacheEntry
	attestationCacheMu   sync.RWMutex
	attestationCacheTTL  time.Duration
	
	// Health tracking
	healthTracker map[string]HealthStatus
	healthMu      sync.RWMutex
}

// OperationType represents different types of operations that can be executed
type OperationType string

const (
	OperationDefault      OperationType = "Default"
	OperationAIInference  OperationType = "AIInference"
	OperationAITraining   OperationType = "AITraining"
	OperationBatchProcess OperationType = "BatchProcess"
)

// MeshClientConfig provides configuration options for the mesh client
type MeshClientConfig struct {
	// Connection timeout for requests
	ConnectionTimeout       time.Duration
	
	// Number of retries for failed operations
	MaxRetries              int
	
	// Whether to prefer TEEs in the same region
	RegionalPreference      bool
	
	// Circuit breaker threshold (consecutive failures)
	CircuitBreakerThreshold int
	
	// Cache TTL for attestation results
	AttestationCacheTTL     time.Duration
	
	// Preferred TEE types in order of preference
	PreferredTEETypes       []TEEType
	
	// AI-specific configuration
	AIEnabled               bool
	MaxModelSize            int64
	BatchSize               int
}

// AttestationCacheEntry stores cached attestation verification results
type AttestationCacheEntry struct {
	// Verification result
	Valid bool
	
	// When the entry was created
	Timestamp time.Time
	
	// Source TEE that performed verification
	SourceTEE string
	
	// TEE type of the source
	SourceTEEType TEEType
}

// HealthStatus tracks the health of TEE connections
type HealthStatus struct {
	// Consecutive failures
	ConsecutiveFailures int
	
	// Last successful use
	LastSuccess time.Time
	
	// Average latency
	AverageLatencyMs float64
	
	// Circuit open/closed status
	CircuitOpen bool
}

// MeshExecuteRequest represents a request to execute an operation on the mesh
type MeshExecuteRequest struct {
	// Operation to perform (e.g., OpSecureCommit, OpSecureOpenAtPoint)
	Operation             string `json:"operation"`
	
	// Operation type (e.g., AIInference, AITraining)
	OperationType         OperationType `json:"operation_type,omitempty"`
	
	// Input data for the operation
	Input                 []byte `json:"input"`
	
	// Target TEE for the operation
	TargetTEE             string `json:"target_tee,omitempty"`
	
	// Region ID for the operation
	RegionID              string `json:"region_id,omitempty"`
	
	// Primary TEE type to use (e.g., "IntelSGX")
	PrimaryTEEType        string `json:"primary_tee_type,omitempty"`
	
	// Secondary TEE type for cross-verification (e.g., "SEV")
	SecondaryTEEType      string `json:"secondary_tee_type,omitempty"`
	
	// Execution mode ("parallel", "primary-only", "verification-only")
	ExecutionMode         string `json:"execution_mode,omitempty"`
	
	// Whether to allow fallback to single-TEE mode if dual execution fails
	AllowSingleTEEFallback bool `json:"allow_single_tee_fallback"`
	
	// Cross-verification strategy ("consensus", "primary-authoritative", "require-both")
	VerificationStrategy   string `json:"verification_strategy,omitempty"`
	
	// Whether to use cache 
	UseCache              bool `json:"use_cache"`
	
	// Cache TTL if using cache
	CacheTTL              int64 `json:"cache_ttl,omitempty"`
	
	// AI-specific parameters
	BatchSize             int    `json:"batch_size,omitempty"`
	ModelID               string `json:"model_id,omitempty"`
}

// MeshExecuteResponse is the response from a mesh execution
type MeshExecuteResponse struct {
	// Result data
	Result []byte `json:"result"`
	
	// Error message, if any
	Error string `json:"error,omitempty"`
	
	// Performance metrics
	Metrics ExecutionMetrics `json:"metrics,omitempty"`
	
	// Whether the result was from cache
	FromCache bool `json:"from_cache"`
	
	// Attestation data from the TEE
	Attestation []byte `json:"attestation,omitempty"`
	
	// TEE that executed the operation
	ExecutorTEE string `json:"executor_tee"`
	
	// TEE type that executed the operation
	ExecutorTEEType string `json:"executor_tee_type"`
	
	// Verification TEE (if dual verification was used)
	VerifierTEE string `json:"verifier_tee,omitempty"`
	
	// TEE type of the verifier
	VerifierTEEType string `json:"verifier_tee_type,omitempty"`
}

// ExecutionMetrics contains performance metrics for an execution
type ExecutionMetrics struct {
	// End-to-end latency in milliseconds
	LatencyMs float64 `json:"latency_ms"`
	
	// TEE execution time in milliseconds
	ExecutionTimeMs float64 `json:"execution_time_ms"`
	
	// Network time in milliseconds
	NetworkTimeMs float64 `json:"network_time_ms"`
	
	// Whether cross-region execution was performed
	CrossRegion bool `json:"cross_region"`
	
	// Whether cross-TEE verification was performed
	CrossTEEVerification bool `json:"cross_tee_verification"`
}

// NewMeshClient creates a new client for the TEE mesh network
func NewMeshClient(meshEndpoint, defaultRegion string, config MeshClientConfig) *MeshClient {
	// Apply defaults for zero values
	if config.ConnectionTimeout == 0 {
		config.ConnectionTimeout = DefaultConnectionTimeout
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = DefaultMaxRetries
	}
	if config.AttestationCacheTTL == 0 {
		config.AttestationCacheTTL = DefaultCacheTTL
	}
	if len(config.PreferredTEETypes) == 0 {
		// Default to SGX first, then SEV as fallback
		config.PreferredTEETypes = []TEEType{TEETypeIntelSGX, TEETypeSEV}
	}
	
	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: config.ConnectionTimeout,
	}
	
	return &MeshClient{
		meshEndpoint:        meshEndpoint,
		defaultRegion:       defaultRegion,
		config:              config,
		client:              client,
		attestationCache:    make(map[string]AttestationCacheEntry),
		attestationCacheTTL: config.AttestationCacheTTL,
		healthTracker:       make(map[string]HealthStatus),
	}
}

// SecureCommit calls the TEE controller to securely commit to a polynomial
// using parallel execution on both SGX and SEV for cross-verification
func (c *MeshClient) SecureCommit(ctx context.Context, stateRoots [][]byte, degree uint32) ([]byte, []byte, error) {
	// Prepare input data as expected by the TEE
	// Format: [num_roots(u32)][root1][root2]...[rootN][degree(u32)]
	
	// Calculate total size
	totalSize := 4 + (len(stateRoots) * 32) + 4
	input := make([]byte, totalSize)
	
	// Write number of roots
	numRoots := uint32(len(stateRoots))
	writeUint32(input, 0, numRoots)
	
	// Write roots
	offset := 4
	for _, root := range stateRoots {
		copy(input[offset:offset+32], root)
		offset += 32
	}
	
	// Write degree
	writeUint32(input, offset, degree)
	
	// Create request for mesh execution with parallel SGX and SEV execution
	request := MeshExecuteRequest{
		Operation:           "SecureCommit",
		Input:               input,
		RegionID:            c.defaultRegion,
		PrimaryTEEType:      string(TEETypeIntelSGX),
		SecondaryTEEType:    string(TEETypeSEV),
		ExecutionMode:       "parallel",
		AllowSingleTEEFallback: true,
		VerificationStrategy: "consensus", 
		UseCache:            false, // Don't cache commits
	}
	
	// Execute via mesh
	response, err := c.executeMesh(ctx, request)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute secure commit: %w", err)
	}
	
	// Parse response: [commitment_size(u32)][commitment][attestation_size(u32)][attestation]
	if len(response.Result) < 8 {
		return nil, nil, fmt.Errorf("invalid response size: %d", len(response.Result))
	}
	
	// Extract commitment
	commitmentSize := readUint32(response.Result, 0)
	if commitmentSize == 0 || int(commitmentSize+8) > len(response.Result) {
		return nil, nil, fmt.Errorf("invalid commitment size: %d", commitmentSize)
	}
	commitment := response.Result[4 : 4+commitmentSize]
	
	// Extract attestation
	responseSize := uint32(len(response.Result))
	attestationSizeOffset := responseSize - 4
	attestationSize := readUint32(response.Result, int(attestationSizeOffset))
	if attestationSizeOffset+4+attestationSize > responseSize {
		return nil, nil, fmt.Errorf("invalid attestation size")
	}
	
	attestation := response.Result[attestationSizeOffset+4 : attestationSizeOffset+4+attestationSize]
	
	// Log metrics for monitoring
	fmt.Printf("[MeshClient] SecureCommit executed in %.2fms using %s/%s\n", 
		response.Metrics.LatencyMs, response.ExecutorTEEType, response.VerifierTEEType)
	
	return commitment, attestation, nil
}

// SecureOpenAtPoint calls the TEE controller to verify a commitment at a specific point
// using parallel execution on both SGX and SEV for cross-verification
func (c *MeshClient) SecureOpenAtPoint(ctx context.Context, commitment, point []byte) (bool, error) {
	// Prepare input data: [commitment_size(u32)][commitment][point_size(u32)][point]
	inputSize := 8 + len(commitment) + len(point)
	input := make([]byte, inputSize)
	
	// Write commitment with length prefix
	writeUint32(input, 0, uint32(len(commitment)))
	copy(input[4:4+len(commitment)], commitment)
	
	// Write point with length prefix
	pointOffset := 4 + len(commitment)
	writeUint32(input, pointOffset, uint32(len(point)))
	copy(input[pointOffset+4:], point)
	
	// Create request for mesh execution with caching and parallel SGX/SEV execution
	// This operation is good to cache since verification is deterministic
	request := MeshExecuteRequest{
		Operation:            "SecureOpenAtPoint",
		Input:                input,
		RegionID:             c.defaultRegion,
		PrimaryTEEType:       string(TEETypeIntelSGX),
		SecondaryTEEType:     string(TEETypeSEV),
		ExecutionMode:        "parallel",
		AllowSingleTEEFallback: true,
		VerificationStrategy: "require-both", // More stringent for openings - require both TEEs to agree
		UseCache:             true,
		CacheTTL:             int64(c.config.AttestationCacheTTL.Seconds()),
	}
	
	// Execute via mesh
	response, err := c.executeMesh(ctx, request)
	if err != nil {
		return false, fmt.Errorf("failed to execute secure open at point: %w", err)
	}
	
	// Parse response - a single byte indicating success (1) or failure (0)
	if len(response.Result) < 1 {
		return false, fmt.Errorf("invalid response size: %d", len(response.Result))
	}
	
	// Log metrics for monitoring
	fmt.Printf("[MeshClient] SecureOpenAtPoint executed in %.2fms using %s/%s (cached: %v)\n", 
		response.Metrics.LatencyMs, response.ExecutorTEEType, response.VerifierTEEType, response.FromCache)
	
	return response.Result[0] == 1, nil
}

// VerifyAttestation verifies a TEE attestation using cross-attestation
// from the opposite TEE type (SGX verifies SEV and vice versa)
func (c *MeshClient) VerifyAttestation(ctx context.Context, attestation []byte, teeType TEEType) (bool, error) {
	// Check cache first
	cacheKey := fmt.Sprintf("%x:%s", attestation, teeType)
	
	c.attestationCacheMu.RLock()
	entry, found := c.attestationCache[cacheKey]
	c.attestationCacheMu.RUnlock()
	
	if found && time.Since(entry.Timestamp) < c.attestationCacheTTL {
		fmt.Printf("[MeshClient] Using cached attestation verification for type %s\n", teeType)
		return entry.Valid, nil
	}
	
	// Select a different TEE type for verification (cross-attestation)
	verifierTEEType := c.getAlternateTEEType(teeType)
	fmt.Printf("[MeshClient] Cross-attestation: %s attestation being verified by %s\n", 
		teeType, verifierTEEType)
	
	// Prepare input data for attestation verification
	input := make([]byte, 4+len(attestation)+4+len(teeType))
	
	// Write attestation with length prefix
	writeUint32(input, 0, uint32(len(attestation)))
	copy(input[4:4+len(attestation)], attestation)
	
	// Write TEE type with length prefix
	typeOffset := 4 + len(attestation)
	writeUint32(input, typeOffset, uint32(len(teeType)))
	copy(input[typeOffset+4:], []byte(teeType))
	
	// Create request for mesh execution
	// For attestation verification, we explicitly use the opposite TEE type
	// SGX verifies SEV attestations and vice versa
	request := MeshExecuteRequest{
		Operation:           "VerifyAttestation",
		Input:               input,
		RegionID:            c.defaultRegion,
		PrimaryTEEType:      string(verifierTEEType), // Use opposite TEE type as primary
		ExecutionMode:       "primary-only", // Single TEE for attestation verification
		AllowSingleTEEFallback: true,
		UseCache:            false,
	}
	
	// Execute via mesh
	response, err := c.executeMesh(ctx, request)
	if err != nil {
		return false, fmt.Errorf("failed to verify attestation: %w", err)
	}
	
	// Parse response - a single byte indicating success (1) or failure (0)
	if len(response.Result) < 1 {
		return false, fmt.Errorf("invalid response size: %d", len(response.Result))
	}
	
	valid := response.Result[0] == 1
	
	// Cache the result
	c.attestationCacheMu.Lock()
	c.attestationCache[cacheKey] = AttestationCacheEntry{
		Valid:        valid,
		Timestamp:    time.Now(),
		SourceTEE:    response.ExecutorTEE,
		SourceTEEType: TEEType(response.ExecutorTEEType),
	}
	c.attestationCacheMu.Unlock()
	
	// Log metrics for monitoring
	fmt.Printf("[MeshClient] Attestation verification executed in %.2fms by %s\n", 
		response.Metrics.LatencyMs, response.ExecutorTEEType)
	
	return valid, nil
}

// executeMesh sends a request to the mesh coordinator and returns the response,
// executing the operation in parallel on both SGX and SEV TEEs for cross-validation
func (c *MeshClient) executeMesh(ctx context.Context, request MeshExecuteRequest) (*MeshExecuteResponse, error) {
	// Optimize TEE selection based on operation type
	if request.OperationType == OperationAIInference || request.OperationType == OperationAITraining {
		// For AI workloads, use TDX as primary due to its larger memory and better performance
		// for computationally intensive workloads
		request.PrimaryTEEType = string(TEETypeTDX)
		// Use SGX for verification due to its strong security guarantees
		request.SecondaryTEEType = string(TEETypeIntelSGX)
		// For large AI models, we might need to use a different verification strategy
		if request.OperationType == OperationAITraining || request.BatchSize > 100 {
			// For training or large batch inference, use a sampling verification strategy
			// to avoid performance bottlenecks
			request.VerificationStrategy = "sampling"
		}
	}
	// Set default execution parameters if not specified
	if request.ExecutionMode == "" {
		request.ExecutionMode = "parallel" // Default to parallel execution on both TEE types
	}
	
	// Set default TEE types if not specified 
	if request.PrimaryTEEType == "" {
		request.PrimaryTEEType = string(TEETypeIntelSGX) // Default primary to SGX
	}
	
	if request.SecondaryTEEType == "" {
		request.SecondaryTEEType = string(TEETypeSEV) // Default secondary to SEV
	}
	
	// Set default verification strategy if not specified
	if request.VerificationStrategy == "" {
		request.VerificationStrategy = "consensus" // Default to consensus between SGX and SEV
	}
	
	// Log the dual-TEE execution
	fmt.Printf("[MeshClient] Executing operation '%s' in %s mode with %s and %s verification\n", 
		request.Operation, request.ExecutionMode, request.PrimaryTEEType, request.SecondaryTEEType)
	
	// Marshal payload to JSON
	payloadBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}
	
	// Create request with context
	req, err := http.NewRequestWithContext(ctx, "POST", c.meshEndpoint, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	// Set headers
	req.Header.Set("Content-Type", "application/json")
	
	// Implement retries with backoff
	var resp *http.Response
	var respErr error
	
	for attempt := 0; attempt < c.config.MaxRetries; attempt++ {
		resp, respErr = c.client.Do(req)
		if respErr == nil && resp.StatusCode == http.StatusOK {
			break
		}
		
		if resp != nil {
			resp.Body.Close()
		}
		
		// Backoff before retry
		if attempt < c.config.MaxRetries-1 {
			time.Sleep(time.Duration(1<<attempt) * 100 * time.Millisecond)
		}
	}
	
	if respErr != nil {
		return nil, fmt.Errorf("failed to send request after %d retries: %w", c.config.MaxRetries, respErr)
	}
	
	defer resp.Body.Close()
	
	// Check response status
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mesh coordinator returned status: %d", resp.StatusCode)
	}
	
	// Read response body
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	
	// Parse response
	var response MeshExecuteResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	
	// Check for error
	if response.Error != "" {
		return nil, fmt.Errorf("mesh execution error: %s", response.Error)
	}
	
	// Update health metrics for both primary and secondary TEEs
	c.updateHealthMetrics(request.PrimaryTEEType, request.RegionID, response.Metrics.LatencyMs, true)
	c.updateHealthMetrics(request.SecondaryTEEType, request.RegionID, response.Metrics.LatencyMs, true)
	
	// Log successful cross-attestation
	if response.VerifierTEEType != "" {
		fmt.Printf("[MeshClient] Cross-attestation successful: %s operation verified between %s (executor) and %s (verifier)\n",
			request.Operation, response.ExecutorTEEType, response.VerifierTEEType)
	}
	
	return &response, nil
}

// getAlternateTEEType returns an alternate TEE type for cross-attestation
func (c *MeshClient) getAlternateTEEType(primaryType TEEType) TEEType {
	// Choose appropriate alternate TEE type based on the primary type
	switch primaryType {
	case TEETypeIntelSGX:
		return TEETypeSEV
	case TEETypeSEV:
		return TEETypeIntelSGX
	case TEETypeTDX:
		// For TDX, prefer SGX for verification due to its strong security properties
		return TEETypeIntelSGX
	default:
		return TEETypeIntelSGX
	}
}

// updateHealthMetrics updates the health tracking for a specific TEE
func (c *MeshClient) updateHealthMetrics(teeType, regionID string, latencyMs float64, success bool) {
	key := fmt.Sprintf("%s:%s", teeType, regionID)
	
	c.healthMu.Lock()
	defer c.healthMu.Unlock()
	
	status, exists := c.healthTracker[key]
	if !exists {
		status = HealthStatus{
			LastSuccess: time.Time{},
		}
	}
	
	if success {
		// Reset failures on success
		status.ConsecutiveFailures = 0
		status.LastSuccess = time.Now()
		status.CircuitOpen = false
		
		// Update average latency with exponential moving average
		if status.AverageLatencyMs == 0 {
			status.AverageLatencyMs = latencyMs
		} else {
			status.AverageLatencyMs = 0.9*status.AverageLatencyMs + 0.1*latencyMs
		}
	} else {
		// Increment failures
		status.ConsecutiveFailures++
		
		// Open circuit if threshold reached
		if status.ConsecutiveFailures >= c.config.CircuitBreakerThreshold {
			status.CircuitOpen = true
		}
	}
	
	c.healthTracker[key] = status
}

// Helper functions for binary encoding

func writeUint32(buffer []byte, offset int, value uint32) {
	binary.LittleEndian.PutUint32(buffer[offset:], value)
}

func readUint32(data []byte, offset int) uint32 {
	return binary.LittleEndian.Uint32(data[offset : offset+4])
}
