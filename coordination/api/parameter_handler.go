// coordination/api/parameter_handler.go
package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rhombus-tech/vm/coordination"
)

// ParameterValidationRequest represents a request to validate parameters
type ParameterValidationRequest struct {
	Data             string                 `json:"data"`
	Format           string                 `json:"format,omitempty"`
	ExpectedSize     int                    `json:"expected_size,omitempty"`
	CrossValidate    bool                   `json:"cross_validate"`
	TimeoutMs        int                    `json:"timeout_ms,omitempty"`
	RegionID         string                 `json:"region_id,omitempty"`
	AdditionalParams map[string]interface{} `json:"additional_params,omitempty"`
}

// ParameterValidationResponse represents a response from parameter validation
type ParameterValidationResponse struct {
	Success       bool    `json:"success"`
	Format        string  `json:"format,omitempty"`
	ValidatedData string  `json:"validated_data,omitempty"`
	Error         string  `json:"error,omitempty"`
	TimeNs        int64   `json:"time_ns,omitempty"`
	TaskID        string  `json:"task_id,omitempty"`
	ThroughputTPS float64 `json:"throughput_tps,omitempty"`
}

// ValidationBatchRequest represents a batch of validation requests
type ValidationBatchRequest struct {
	Requests []ParameterValidationRequest `json:"requests"`
	BatchID  string                       `json:"batch_id,omitempty"`
}

// ValidationBatchResponse represents a batch of validation responses
type ValidationBatchResponse struct {
	Responses   []ParameterValidationResponse `json:"responses"`
	BatchID     string                        `json:"batch_id,omitempty"`
	TimeNs      int64                         `json:"time_ns,omitempty"`
	SuccessRate float64                       `json:"success_rate,omitempty"`
}

// ParameterValidationHandler handles parameter validation API requests
type ParameterValidationHandler struct {
	coordinator *coordination.Coordinator
}

// NewParameterValidationHandler creates a new parameter validation handler
func NewParameterValidationHandler(c *coordination.Coordinator) *ParameterValidationHandler {
	return &ParameterValidationHandler{
		coordinator: c,
	}
}

// RegisterRoutes registers all routes for the parameter validation API
func (h *ParameterValidationHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/validate", h.ValidateParameter)
	mux.HandleFunc("/api/validate/batch", h.ValidateBatch)
	mux.HandleFunc("/api/validate/stats", h.GetValidationStats)
}

// ValidateParameter handles a single parameter validation request
func (h *ParameterValidationHandler) ValidateParameter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read and parse request body
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req ParameterValidationRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid request format", http.StatusBadRequest)
		return
	}

	// Decode base64 data
	data, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil {
		http.Error(w, "Invalid base64 data", http.StatusBadRequest)
		return
	}

	// Set default timeout if not specified
	timeoutMs := 5000 // 5 seconds default
	if req.TimeoutMs > 0 {
		timeoutMs = req.TimeoutMs
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	// Prepare options
	options := map[string]interface{}{
		"cross_validate": req.CrossValidate,
	}
	
	if req.Format != "" {
		options["format"] = req.Format
	}
	
	if req.ExpectedSize > 0 {
		options["expected_size"] = req.ExpectedSize
	}
	
	// Include any additional parameters
	for k, v := range req.AdditionalParams {
		options[k] = v
	}

	// Perform validation
	startTime := time.Now()
	result, err := h.coordinator.HandleParameterValidation(ctx, data, options)
	elapsedNs := time.Since(startTime).Nanoseconds()

	// Prepare response
	resp := ParameterValidationResponse{
		TimeNs: elapsedNs,
	}

	if err != nil {
		resp.Success = false
		resp.Error = err.Error()
	} else {
		resp.Success = result.Success
		resp.Format = result.Format
		resp.TaskID = result.ID
		
		// Calculate throughput: parameters per second
		resp.ThroughputTPS = float64(time.Second) / float64(elapsedNs)
		
		// Base64 encode validated data for response
		if result.ValidatedData != nil {
			resp.ValidatedData = base64.StdEncoding.EncodeToString(result.ValidatedData)
		}
		
		if result.Error != nil {
			resp.Error = result.Error.Error()
		}
	}

	// Send response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ValidateBatch handles batch parameter validation requests
func (h *ParameterValidationHandler) ValidateBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read and parse request body
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var batchReq ValidationBatchRequest
	if err := json.Unmarshal(body, &batchReq); err != nil {
		http.Error(w, "Invalid request format", http.StatusBadRequest)
		return
	}

	// Set default batch ID if not provided
	if batchReq.BatchID == "" {
		batchReq.BatchID = coordination.GenerateBatchID()
	}

	// Process all requests in parallel
	startTime := time.Now()
	responses := make([]ParameterValidationResponse, len(batchReq.Requests))
	var successCount int32 = 0

	// Use a wait group to wait for all validations to complete
	var wg sync.WaitGroup
	wg.Add(len(batchReq.Requests))

	for i, req := range batchReq.Requests {
		go func(index int, request ParameterValidationRequest) {
			defer wg.Done()

			// Create a mock request and response for the single validator
			mockReq, _ := http.NewRequest("POST", "/api/validate", nil)
			mockResp := httptest.NewRecorder()

			// Convert request back to JSON
			reqJSON, _ := json.Marshal(request)
			mockReq.Body = ioutil.NopCloser(bytes.NewReader(reqJSON))

			// Process the request
			h.ValidateParameter(mockResp, mockReq)

			// Parse the response
			var resp ParameterValidationResponse
			json.Unmarshal(mockResp.Body.Bytes(), &resp)

			// Store in response array
			responses[index] = resp

			if resp.Success {
				atomic.AddInt32(&successCount, 1)
			}
		}(i, req)
	}

	// Wait for all validations to complete
	wg.Wait()

	// Calculate batch metrics
	elapsedNs := time.Since(startTime).Nanoseconds()
	successRate := float64(atomic.LoadInt32(&successCount)) / float64(len(batchReq.Requests))

	// Prepare batch response
	batchResp := ValidationBatchResponse{
		Responses:   responses,
		BatchID:     batchReq.BatchID,
		TimeNs:      elapsedNs,
		SuccessRate: successRate,
	}

	// Send response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(batchResp)
}

// GetValidationStats returns statistics about parameter validation
func (h *ParameterValidationHandler) GetValidationStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get the parameter validator from the coordinator
	validator, err := h.coordinator.GetParameterValidator()
	if err != nil {
		http.Error(w, "Failed to get validator: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Get validation stats
	stats := validator.GetValidationStats()

	// Convert to map for JSON response
	statsMap := map[string]interface{}{
		"total_processed":       stats.TotalProcessed,
		"success_count":         stats.SuccessCount,
		"failure_count":         stats.FailureCount,
		"length_prefixed_count": stats.LengthPrefixedCount,
		"direct_format_count":   stats.DirectFormatCount,
		"cross_validated_count": stats.CrossValidatedCount,
		"avg_processing_time_ns": stats.AvgProcessingTimeNs,
		"batches_processed":     stats.BatchesProcessed,
	}

	// Calculate success rate
	if stats.TotalProcessed > 0 {
		statsMap["success_rate"] = float64(stats.SuccessCount) / float64(stats.TotalProcessed)
	} else {
		statsMap["success_rate"] = 0.0
	}

	// Send response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(statsMap)
}
