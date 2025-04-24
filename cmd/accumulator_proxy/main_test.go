package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/rhombus-tech/vm/tee/accumulator"
)

// TestDualFormatLengthPrefixed tests length-prefixed parameter validation
func TestDualFormatLengthPrefixed(t *testing.T) {
	// Create a length-prefixed parameter
	paramSize := uint32(32)
	param := make([]byte, 4+paramSize)
	binary.LittleEndian.PutUint32(param[0:4], paramSize)
	for i := uint32(0); i < paramSize; i++ {
		param[4+i] = byte(i % 256)
	}

	// Mock TEE interface
	mockTEE := &MockTEEInterface{
		executeInTeeFunc: func(ctx context.Context, function string, params []byte, useLengthPrefix bool) ([]byte, error) {
			// Verify the parameters
			if function != "validate_parameter" {
				t.Errorf("Expected function 'validate_parameter', got: %s", function)
			}
			if !useLengthPrefix {
				t.Error("Expected useLengthPrefix to be true")
			}
			// Just return success for this test
			return []byte(`{"status":"success","format":"length_prefixed"}`), nil
		},
	}

	// Create the HTTP server with our handler
	config := &Config{
		Port:               "8080",
		TEEType:            "sgx",
		TEEID:              "test-tee",
		MaxParameterSize:   1024,
		ContractIDSize:     32,
		BatchSize:          10,
		MaxParallelBatches: 4,
		MaxCacheSize:       100,
		EnableLengthPrefix: true,
		EnableDirectFormat: true,
	}
	handler := createHandler(config, mockTEE)
	
	// Create a test HTTP server
	server := httptest.NewServer(handler)
	defer server.Close()

	// Create a request to send to our handler
	req, err := http.NewRequest(http.MethodPost, server.URL+"/accumulate", bytes.NewBuffer(param))
	if err != nil {
		t.Fatal(err)
	}

	// Send the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Check the response
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, resp.StatusCode)
	}

	// Parse the response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}

	// Verify the response
	if result["status"] != "success" {
		t.Errorf("Expected status 'success', got: %v", result["status"])
	}
	if result["format"] != "length_prefixed" {
		t.Errorf("Expected format 'length_prefixed', got: %v", result["format"])
	}
}

// TestDualFormatDirect tests direct parameter validation
func TestDualFormatDirect(t *testing.T) {
	// Create a direct format parameter (typical contract ID)
	paramSize := uint32(32)
	param := make([]byte, paramSize)
	for i := uint32(0); i < paramSize; i++ {
		param[i] = byte(i % 256)
	}

	// Mock TEE interface
	mockTEE := &MockTEEInterface{
		executeInTeeFunc: func(ctx context.Context, function string, params []byte, useLengthPrefix bool) ([]byte, error) {
			// Verify the parameters
			if function != "validate_parameter" {
				t.Errorf("Expected function 'validate_parameter', got: %s", function)
			}
			if useLengthPrefix {
				t.Error("Expected useLengthPrefix to be false for direct format")
			}
			// Just return success for this test
			return []byte(`{"status":"success","format":"direct"}`), nil
		},
	}

	// Create the HTTP server with our handler
	config := &Config{
		Port:               "8080",
		TEEType:            "sgx",
		TEEID:              "test-tee",
		MaxParameterSize:   1024,
		ContractIDSize:     32,
		BatchSize:          10,
		MaxParallelBatches: 4,
		MaxCacheSize:       100,
		EnableLengthPrefix: true,
		EnableDirectFormat: true,
	}
	handler := createHandler(config, mockTEE)
	
	// Create a test HTTP server
	server := httptest.NewServer(handler)
	defer server.Close()

	// Create a request to send to our handler
	req, err := http.NewRequest(http.MethodPost, server.URL+"/accumulate", bytes.NewBuffer(param))
	if err != nil {
		t.Fatal(err)
	}

	// Send the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Check the response
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, resp.StatusCode)
	}

	// Parse the response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}

	// Verify the response
	if result["status"] != "success" {
		t.Errorf("Expected status 'success', got: %v", result["status"])
	}
	if result["format"] != "direct" {
		t.Errorf("Expected format 'direct', got: %v", result["format"])
	}
}

// TestInvalidParameterSize tests validation for parameters that exceed max size
func TestInvalidParameterSize(t *testing.T) {
	// Create a length-prefixed parameter that's too large
	paramSize := uint32(2048) // Greater than max size of 1024
	param := make([]byte, 4+10)
	binary.LittleEndian.PutUint32(param[0:4], paramSize)
	
	// Mock TEE interface
	mockTEE := &MockTEEInterface{
		executeInTeeFunc: func(ctx context.Context, function string, params []byte, useLengthPrefix bool) ([]byte, error) {
			t.Fatal("TEE should not be called for invalid parameter size")
			return nil, nil
		},
	}

	// Create the HTTP server with our handler
	config := &Config{
		Port:               "8080",
		TEEType:            "sgx",
		TEEID:              "test-tee",
		MaxParameterSize:   1024,
		ContractIDSize:     32,
		BatchSize:          10,
		MaxParallelBatches: 4,
		MaxCacheSize:       100,
		EnableLengthPrefix: true,
		EnableDirectFormat: true,
	}
	handler := createHandler(config, mockTEE)
	
	// Create a test HTTP server
	server := httptest.NewServer(handler)
	defer server.Close()

	// Create a request to send to our handler
	req, err := http.NewRequest(http.MethodPost, server.URL+"/accumulate", bytes.NewBuffer(param))
	if err != nil {
		t.Fatal(err)
	}

	// Send the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Parse the response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}

	// Verify the response indicates an error
	if result["status"] != "error" {
		t.Errorf("Expected status 'error', got: %v", result["status"])
	}
}

// TestCrossValidation tests the cross-validation between SGX and SEV
func TestCrossValidation(t *testing.T) {
	// Set up environment variables for testing
	os.Setenv("ENABLE_CROSS_VALIDATE", "true")
	os.Setenv("PEER_ENDPOINTS", "mock:8080")
	defer os.Unsetenv("ENABLE_CROSS_VALIDATE")
	defer os.Unsetenv("PEER_ENDPOINTS")

	// Create test parameter
	paramSize := uint32(32)
	param := make([]byte, 4+paramSize)
	binary.LittleEndian.PutUint32(param[0:4], paramSize)
	for i := uint32(0); i < paramSize; i++ {
		param[4+i] = byte(i % 256)
	}

	// Mock TEE interface
	mockTEE := &MockTEEInterface{
		executeInTeeFunc: func(ctx context.Context, function string, params []byte, useLengthPrefix bool) ([]byte, error) {
			return []byte(`{"status":"success","format":"length_prefixed"}`), nil
		},
	}

	// Create a mock peer server
	peerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"success","format":"length_prefixed"}`))
	}))
	defer peerServer.Close()

	// Create the HTTP server with our handler
	config := &Config{
		Port:               "8080",
		TEEType:            "sgx",
		TEEID:              "test-tee",
		MaxParameterSize:   1024,
		ContractIDSize:     32,
		BatchSize:          10,
		MaxParallelBatches: 4,
		MaxCacheSize:       100,
		EnableLengthPrefix: true,
		EnableDirectFormat: true,
		EnableCrossVal:     true,
		PeerEndpoints:      []string{peerServer.URL},
	}
	handler := createHandler(config, mockTEE)
	
	// Create a test HTTP server
	server := httptest.NewServer(handler)
	defer server.Close()

	// Create a request to send to our handler
	req, err := http.NewRequest(http.MethodPost, server.URL+"/cross_validate", bytes.NewBuffer(param))
	if err != nil {
		t.Fatal(err)
	}

	// Send the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Check the response
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, resp.StatusCode)
	}

	// Parse the response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}

	// Verify the response
	if result["status"] != "success" {
		t.Errorf("Expected status 'success', got: %v", result["status"])
	}
}

// TestBatchProcessing tests batch processing of parameters
func TestBatchProcessing(t *testing.T) {
	// Create batch of parameters
	batchSize := 3
	params := make([][]byte, batchSize)
	for i := 0; i < batchSize; i++ {
		paramSize := uint32(32)
		param := make([]byte, 4+paramSize)
		binary.LittleEndian.PutUint32(param[0:4], paramSize)
		for j := uint32(0); j < paramSize; j++ {
			param[4+j] = byte((i*100 + int(j)) % 256)
		}
		params[i] = param
	}

	// Count number of TEE executions
	executionCount := 0

	// Mock TEE interface
	mockTEE := &MockTEEInterface{
		executeInTeeFunc: func(ctx context.Context, function string, params []byte, useLengthPrefix bool) ([]byte, error) {
			executionCount++
			return []byte(`{"status":"success","format":"length_prefixed"}`), nil
		},
	}

	// Create the HTTP server with our handler
	config := &Config{
		Port:               "8080",
		TEEType:            "sgx",
		TEEID:              "test-tee",
		MaxParameterSize:   1024,
		ContractIDSize:     32,
		BatchSize:          batchSize,
		MaxParallelBatches: 4,
		MaxCacheSize:       100,
		EnableLengthPrefix: true,
		EnableDirectFormat: true,
	}
	handler := createHandler(config, mockTEE)
	
	// Create a test HTTP server
	server := httptest.NewServer(handler)
	defer server.Close()

	// Create batch request
	batchParams := make([]json.RawMessage, batchSize)
	for i := 0; i < batchSize; i++ {
		paramJson, _ := json.Marshal(map[string]interface{}{
			"id":    i,
			"param": params[i],
		})
		batchParams[i] = paramJson
	}
	
	batchRequest, _ := json.Marshal(batchParams)

	// Create a request to send to our handler
	req, err := http.NewRequest(http.MethodPost, server.URL+"/accumulate/batch", bytes.NewBuffer(batchRequest))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Send the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Check the response
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, resp.StatusCode)
	}

	// Parse the response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}

	// Verify we got results array
	results, ok := result["results"].([]interface{})
	if !ok {
		t.Fatal("Expected 'results' array in response")
	}

	// Verify number of results
	if len(results) != batchSize {
		t.Errorf("Expected %d results, got %d", batchSize, len(results))
	}

	// Verify batch processing occurred
	if executionCount != batchSize {
		t.Errorf("Expected %d TEE executions, got %d", batchSize, executionCount)
	}
}

// Mock implementation of TEEInterface for testing
type MockTEEInterface struct {
	executeInTeeFunc func(ctx context.Context, function string, params []byte, useLengthPrefix bool) ([]byte, error)
}

func (m *MockTEEInterface) ExecuteInTee(ctx context.Context, function string, params []byte, useLengthPrefix bool) ([]byte, error) {
	return m.executeInTeeFunc(ctx, function, params, useLengthPrefix)
}

func createHandler(config *Config, teeInterface accumulator.TEEInterface) http.Handler {
	// Define routes
	mux := http.NewServeMux()

	// Route for parameter validation
	mux.HandleFunc("/accumulate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Read parameter data
		data, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}

		// Process the parameter through TEE
		result, err := processSingleParameter(r.Context(), data, config, teeInterface)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Send the response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})

	// Route for batch parameter validation
	mux.HandleFunc("/accumulate/batch", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Read batch request
		var batch []json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
			http.Error(w, "Failed to parse batch request", http.StatusBadRequest)
			return
		}

		// Process each parameter
		results := make([]map[string]interface{}, len(batch))
		for i, item := range batch {
			var paramData struct {
				ID    int    `json:"id"`
				Param []byte `json:"param"`
			}
			if err := json.Unmarshal(item, &paramData); err != nil {
				results[i] = map[string]interface{}{
					"status":  "error",
					"message": "Failed to parse parameter",
				}
				continue
			}

			// Process parameter
			result, err := processSingleParameter(r.Context(), paramData.Param, config, teeInterface)
			if err != nil {
				results[i] = map[string]interface{}{
					"status":  "error",
					"message": err.Error(),
				}
				continue
			}

			results[i] = result
		}

		// Send the response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"results": results,
		})
	})

	// Route for cross-validation
	mux.HandleFunc("/cross_validate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Read parameter data
		data, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}

		// First process locally
		localResult, err := processSingleParameter(r.Context(), data, config, teeInterface)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// If cross-validation enabled and we have peer endpoints
		if config.EnableCrossVal && len(config.PeerEndpoints) > 0 {
			// Send to first peer for cross-validation
			client := &http.Client{Timeout: 5 * time.Second}
			peerURL := config.PeerEndpoints[0] + "/accumulate"
			
			// Just validate the parameter, don't cross-validate again
			peerReq, err := http.NewRequest(http.MethodPost, peerURL, bytes.NewBuffer(data))
			if err != nil {
				// Continue with local result if peer request fails
				w.Header().Set("Content-Type", "application/json")
				localResult["validation"] = "local_only"
				json.NewEncoder(w).Encode(localResult)
				return
			}

			peerResp, err := client.Do(peerReq)
			if err != nil {
				// Continue with local result if peer request fails
				w.Header().Set("Content-Type", "application/json")
				localResult["validation"] = "local_only"
				json.NewEncoder(w).Encode(localResult)
				return
			}
			defer peerResp.Body.Close()

			// Read peer response
			var peerResult map[string]interface{}
			if err := json.NewDecoder(peerResp.Body).Decode(&peerResult); err != nil {
				// Continue with local result if peer response parse fails
				w.Header().Set("Content-Type", "application/json")
				localResult["validation"] = "local_only"
				json.NewEncoder(w).Encode(localResult)
				return
			}

			// Compare results
			if peerResult["status"] == localResult["status"] {
				localResult["validation"] = "cross_validated"
			} else {
				localResult["validation"] = "mismatch"
				localResult["peer_result"] = peerResult
			}
		} else {
			localResult["validation"] = "local_only"
		}

		// Send the response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(localResult)
	})

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
			"tee":    config.TEEType,
		})
	})

	return mux
}

func processSingleParameter(ctx context.Context, data []byte, config *Config, teeInterface accumulator.TEEInterface) (map[string]interface{}, error) {
	// Check parameter size
	if len(data) > config.MaxParameterSize {
		return map[string]interface{}{
			"status":  "error",
			"message": "Parameter exceeds maximum size",
		}, nil
	}

	// Detect parameter format
	useLengthPrefix := false
	formatDetected := "direct"
	
	// Only try to detect length prefix if enabled
	if config.EnableLengthPrefix && len(data) >= 4 {
		// Extract potential length from first 4 bytes
		potentialLength := binary.LittleEndian.Uint32(data[0:4])
		
		// Check if length seems reasonable and matches data
		if potentialLength > 0 && potentialLength <= uint32(config.MaxParameterSize-4) && 
		   len(data) >= 4+int(potentialLength) {
			useLengthPrefix = true
			formatDetected = "length_prefixed"
		}
	}

	// If direct format is not enabled and we didn't detect length prefix
	if !config.EnableDirectFormat && !useLengthPrefix {
		return map[string]interface{}{
			"status":  "error",
			"message": "Direct format parameters not enabled",
		}, nil
	}

	// If we're in direct format but the length doesn't match expected contract ID size
	if !useLengthPrefix && config.ContractIDSize > 0 && len(data) != config.ContractIDSize {
		return map[string]interface{}{
			"status":  "error",
			"message": "Invalid parameter size for direct format",
		}, nil
	}

	// Process through TEE
	result, err := teeInterface.ExecuteInTee(ctx, "validate_parameter", data, useLengthPrefix)
	if err != nil {
		return map[string]interface{}{
			"status":  "error",
			"message": "TEE validation failed: " + err.Error(),
		}, nil
	}

	// Parse TEE result
	var teeResult map[string]interface{}
	if err := json.Unmarshal(result, &teeResult); err != nil {
		return map[string]interface{}{
			"status":  "error",
			"message": "Failed to parse TEE result",
		}, nil
	}

	// Add our detected format
	teeResult["format"] = formatDetected
	
	return teeResult, nil
}
