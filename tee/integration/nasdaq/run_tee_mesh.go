package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"sync"
	"time"
)

// Simplified versions of the required interfaces

type MockAccumulatorUpdater struct{}

func (m *MockAccumulatorUpdater) UpdateAccumulator(data []byte, regionID string) ([]byte, error) {
	log.Printf("[TEE Mesh] Mock accumulator updated with %d bytes from region %s", len(data), regionID)
	return []byte("mock-accumulator-value"), nil
}

type MockVerificationEngine struct{}

func (m *MockVerificationEngine) VerifyRemoteData(data []byte, sourceRegion, targetRegion string) (bool, error) {
	log.Printf("[TEE Mesh] Mock verification of data from %s to %s", sourceRegion, targetRegion)
	return true, nil
}

type MockPolicyEnforcer struct{}

func (m *MockPolicyEnforcer) EnforcePolicy(data []byte, sourceRegion, targetRegion, policyType string) (bool, error) {
	log.Printf("[TEE Mesh] Mock policy enforcement: %s for %s->%s", policyType, sourceRegion, targetRegion)
	return true, nil
}

// NasdaqMarketData represents data received from NASDAQ Cloud Data Service
type NasdaqMarketData struct {
	NasdaqTrade  json.RawMessage `json:"nasdaq_trade,omitempty"`
	NasdaqIndex  json.RawMessage `json:"nasdaq_index,omitempty"`
	TeeMetadata  struct {
		SourceRegion  string    `json:"source_region"`
		Timestamp     time.Time `json:"timestamp"`
		DataType      string    `json:"data_type"`
		SecurityLevel string    `json:"security_level"`
	} `json:"tee_metadata"`
	AccumulatorValue []byte    `json:"accumulator_value,omitempty"`
	ProcessedAt      time.Time `json:"processed_at,omitempty"`
}

// Simplified TEE mesh handler
type SimpleTEEMeshHandler struct {
	regionID      string
	accumulator   *MockAccumulatorUpdater
	verifier      *MockVerificationEngine
	policyEnforcer *MockPolicyEnforcer
	dataStore     map[string][]NasdaqMarketData
	mu            sync.RWMutex
	receivedCount int
}

func NewSimpleTEEMeshHandler(regionID string) *SimpleTEEMeshHandler {
	return &SimpleTEEMeshHandler{
		regionID:      regionID,
		accumulator:   &MockAccumulatorUpdater{},
		verifier:      &MockVerificationEngine{},
		policyEnforcer: &MockPolicyEnforcer{},
		dataStore:     make(map[string][]NasdaqMarketData),
		receivedCount: 0,
	}
}

// Handle NASDAQ data endpoint
func (h *SimpleTEEMeshHandler) handleNasdaqData(w http.ResponseWriter, r *http.Request) {
	// Only accept POST requests
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read request body
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Parse the market data
	var data NasdaqMarketData
	if err := json.Unmarshal(body, &data); err != nil {
		// Try to log the body for debugging
		log.Printf("[TEE Mesh] Error parsing market data: %v", err)
		log.Printf("[TEE Mesh] Raw body received (%d bytes): %s", len(body), truncateString(string(body), 500))
		http.Error(w, "Error parsing market data", http.StatusBadRequest)
		return
	}

	// Store the data
	h.mu.Lock()
	dataType := data.TeeMetadata.DataType
	if dataType == "" {
		dataType = "unknown"
	}
	h.dataStore[dataType] = append(h.dataStore[dataType], data)
	h.receivedCount++
	count := h.receivedCount
	h.mu.Unlock()

	// Process the data through our mock TEE security pipeline
	accValue, _ := h.accumulator.UpdateAccumulator(body, h.regionID)
	h.verifier.VerifyRemoteData(body, data.TeeMetadata.SourceRegion, h.regionID)
	h.policyEnforcer.EnforcePolicy(body, data.TeeMetadata.SourceRegion, h.regionID, "market_data")

	// Add processing metadata
	data.AccumulatorValue = accValue
	data.ProcessedAt = time.Now()

	// Log successful processing
	log.Printf("[TEE Mesh] Processed NASDAQ data #%d, type: %s, source: %s", 
		count, dataType, data.TeeMetadata.SourceRegion)

	// Respond with success
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	
	response := map[string]interface{}{
		"status": "success",
		"regionId": h.regionID,
		"processedAt": data.ProcessedAt,
		"accumulator": fmt.Sprintf("%x", accValue),
	}
	
	json.NewEncoder(w).Encode(response)
}

// Status endpoint
func (h *SimpleTEEMeshHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	stats := map[string]interface{}{
		"status":      "healthy",
		"regionId":    h.regionID,
		"uptime":      time.Since(startTime).String(),
		"dataReceived": h.receivedCount,
		"dataTypes":   make(map[string]int),
	}

	for dataType, data := range h.dataStore {
		stats["dataTypes"].(map[string]int)[dataType] = len(data)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

var startTime time.Time

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func main() {
	startTime = time.Now()
	port := 8081
	regionID := "us-east"

	// Create our handler
	handler := NewSimpleTEEMeshHandler(regionID)

	// Register endpoints
	http.HandleFunc("/api/v1/nasdaq-data", handler.handleNasdaqData)
	http.HandleFunc("/api/v1/status", handler.handleStatus)

	// Start the HTTP server
	addr := fmt.Sprintf(":%d", port)
	log.Printf("[TEE Mesh] Starting simplified TEE mesh network on %s", addr)
	log.Printf("[TEE Mesh] Ready to receive NASDAQ data at http://localhost%s/api/v1/nasdaq-data", addr)
	log.Printf("[TEE Mesh] Check status at http://localhost%s/api/v1/status", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
