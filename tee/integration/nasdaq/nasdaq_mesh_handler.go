package nasdaq

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// NasdaqDataHandler integrates NASDAQ data into the TEE mesh network
type NasdaqDataHandler struct {
	RegionID           string
	AccumulatorUpdater AccumulatorUpdater
	VerificationEngine CrossRegionalVerifier
	PolicyEnforcer     RegionalPolicyEnforcer
	dataStore          map[string][]NasdaqMarketData // map of data type to data
	mu                 sync.RWMutex
	metrics            *nasdaqIntegrationMetrics
}

// AccumulatorUpdater represents a service that can update the cryptographic accumulator
type AccumulatorUpdater interface {
	UpdateAccumulator(data []byte, regionID string) ([]byte, error)
}

// CrossRegionalVerifier represents the verification engine for cross-regional operations
type CrossRegionalVerifier interface {
	VerifyRemoteData(data []byte, sourceRegion, targetRegion string) (bool, error)
}

// RegionalPolicyEnforcer enforces regional data policies
type RegionalPolicyEnforcer interface {
	EnforcePolicy(data []byte, sourceRegion, targetRegion, policyType string) (bool, error)
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

// nasdaqIntegrationMetrics contains Prometheus metrics for NASDAQ data integration
type nasdaqIntegrationMetrics struct {
	nasdaqDataReceived *prometheus.CounterVec
	processingLatency  *prometheus.HistogramVec
	attestationSuccess *prometheus.CounterVec
	policyEnforcement  *prometheus.CounterVec
}

// NewNasdaqDataHandler creates a new NASDAQ data handler
func NewNasdaqDataHandler(
	regionID string,
	accumulator AccumulatorUpdater,
	verifier CrossRegionalVerifier,
	policyEnforcer RegionalPolicyEnforcer,
) *NasdaqDataHandler {
	metrics := &nasdaqIntegrationMetrics{
		nasdaqDataReceived: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tee_nasdaq_data_received_total",
				Help: "Total count of NASDAQ data received by data type",
			},
			[]string{"region_id", "data_type", "security_level"},
		),
		processingLatency: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "tee_nasdaq_processing_latency_ms",
				Help:    "Latency of processing NASDAQ data in milliseconds",
				Buckets: prometheus.ExponentialBuckets(1, 2, 10), // 1ms to 512ms
			},
			[]string{"region_id", "data_type"},
		),
		attestationSuccess: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tee_nasdaq_attestation_total",
				Help: "Total count of attestations for NASDAQ data",
			},
			[]string{"region_id", "result"},
		),
		policyEnforcement: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tee_nasdaq_policy_enforcement_total",
				Help: "Total count of policy enforcements for NASDAQ data",
			},
			[]string{"region_id", "policy_type", "result"},
		),
	}

	return &NasdaqDataHandler{
		RegionID:           regionID,
		AccumulatorUpdater: accumulator,
		VerificationEngine: verifier,
		PolicyEnforcer:     policyEnforcer,
		dataStore:          make(map[string][]NasdaqMarketData),
		metrics:            metrics,
	}
}

// Start launches the NASDAQ data handler HTTP server
func (h *NasdaqDataHandler) Start(port int) error {
	http.HandleFunc("/api/v1/nasdaq-data", h.handleNasdaqData)
	addr := fmt.Sprintf(":%d", port)
	log.Printf("Starting NASDAQ data handler on %s", addr)
	return http.ListenAndServe(addr, nil)
}

// handleNasdaqData processes incoming NASDAQ data
func (h *NasdaqDataHandler) handleNasdaqData(w http.ResponseWriter, r *http.Request) {
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
		http.Error(w, "Error parsing market data", http.StatusBadRequest)
		return
	}

	// Start measuring processing time
	start := time.Now()

	// Record metrics
	h.metrics.nasdaqDataReceived.WithLabelValues(
		h.RegionID,
		data.TeeMetadata.DataType,
		data.TeeMetadata.SecurityLevel,
	).Inc()

	// Process the data through the TEE
	if err := h.processMarketData(&data); err != nil {
		http.Error(w, fmt.Sprintf("Error processing market data: %v", err), http.StatusInternalServerError)
		return
	}

	// Update metrics for processing latency
	latency := float64(time.Since(start).Milliseconds())
	h.metrics.processingLatency.WithLabelValues(
		h.RegionID,
		data.TeeMetadata.DataType,
	).Observe(latency)

	// Store the data
	h.storeMarketData(data)

	// Return success
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success","message":"Data processed successfully"}`))
}

// processMarketData processes NASDAQ market data through the TEE architecture
func (h *NasdaqDataHandler) processMarketData(data *NasdaqMarketData) error {
	// Convert data to bytes for processing
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}

	// 1. Update the cryptographic accumulator with the new data
	accumulatorValue, err := h.AccumulatorUpdater.UpdateAccumulator(dataBytes, h.RegionID)
	if err != nil {
		return fmt.Errorf("failed to update accumulator: %v", err)
	}
	data.AccumulatorValue = accumulatorValue

	// 2. For cross-regional data, verify through the verification engine
	if data.TeeMetadata.SourceRegion != h.RegionID {
		verified, err := h.VerificationEngine.VerifyRemoteData(
			dataBytes,
			data.TeeMetadata.SourceRegion,
			h.RegionID,
		)
		if err != nil {
			h.metrics.attestationSuccess.WithLabelValues(h.RegionID, "failure").Inc()
			return fmt.Errorf("cross-regional verification failed: %v", err)
		}
		if !verified {
			h.metrics.attestationSuccess.WithLabelValues(h.RegionID, "failure").Inc()
			return fmt.Errorf("data verification failed for source region %s", data.TeeMetadata.SourceRegion)
		}
		h.metrics.attestationSuccess.WithLabelValues(h.RegionID, "success").Inc()
	}

	// 3. Enforce regional policy for the data
	policyType := "data_sharing"
	if data.TeeMetadata.SecurityLevel == "confidential" {
		policyType = "confidential_data"
	}

	policyAllowed, err := h.PolicyEnforcer.EnforcePolicy(
		dataBytes,
		data.TeeMetadata.SourceRegion,
		h.RegionID,
		policyType,
	)
	if err != nil {
		h.metrics.policyEnforcement.WithLabelValues(h.RegionID, policyType, "failure").Inc()
		return fmt.Errorf("policy enforcement failed: %v", err)
	}
	if !policyAllowed {
		h.metrics.policyEnforcement.WithLabelValues(h.RegionID, policyType, "failure").Inc()
		return fmt.Errorf("policy enforcement denied data processing for policy type %s", policyType)
	}
	h.metrics.policyEnforcement.WithLabelValues(h.RegionID, policyType, "success").Inc()

	// 4. Mark the data as processed
	data.ProcessedAt = time.Now()

	return nil
}

// storeMarketData stores the processed market data
func (h *NasdaqDataHandler) storeMarketData(data NasdaqMarketData) {
	h.mu.Lock()
	defer h.mu.Unlock()

	dataType := data.TeeMetadata.DataType
	if _, exists := h.dataStore[dataType]; !exists {
		h.dataStore[dataType] = make([]NasdaqMarketData, 0)
	}

	// Add to the store, keeping only the latest 1000 items per type
	h.dataStore[dataType] = append(h.dataStore[dataType], data)
	if len(h.dataStore[dataType]) > 1000 {
		h.dataStore[dataType] = h.dataStore[dataType][len(h.dataStore[dataType])-1000:]
	}
}

// GetLatestMarketData retrieves the latest market data of a given type
func (h *NasdaqDataHandler) GetLatestMarketData(dataType string, limit int) []NasdaqMarketData {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if data, exists := h.dataStore[dataType]; exists {
		if limit > 0 && limit < len(data) {
			return data[len(data)-limit:]
		}
		return data
	}
	return nil
}
