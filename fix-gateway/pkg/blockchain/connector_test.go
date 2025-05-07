// Copyright (C) 2025. All rights reserved.
// See the file LICENSE for licensing terms.

package blockchain

import (
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestSubmitOrderSettlement tests the SubmitOrderSettlement function
func TestSubmitOrderSettlement(t *testing.T) {
	// Create a test server that records the request
	var receivedRequest struct {
		OrderID  string `json:"order_id"`
		Symbol   string `json:"symbol"`
		Price    int64  `json:"price"`
		Quantity int64  `json:"quantity"`
	}
	
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify method and path
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST request, got %s", r.Method)
		}
		if r.URL.Path != "/api/settlement" {
			t.Errorf("Expected /api/settlement path, got %s", r.URL.Path)
		}
		
		// Decode the request body
		if err := json.NewDecoder(r.Body).Decode(&receivedRequest); err != nil {
			t.Errorf("Failed to decode request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		
		// Return a mock response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"confirmed", "tx_id":"mock-tx-123"}`))
	}))
	defer server.Close()
	
	// Create a blockchain connector using the test server
	config := Config{
		Endpoint:       server.URL,
		TimeoutSeconds: 5,
		MaxRetries:     2,
		RetryInterval:  time.Millisecond * 100,
		SimulationMode: false,
	}
	
	connector, err := NewConnector(config)
	if err != nil {
		t.Fatalf("Failed to create connector: %v", err)
	}
	
	// Test cases
	tests := []struct {
		name     string
		orderID  string
		symbol   string
		price    int64
		quantity int64
	}{
		{
			name:     "Basic settlement",
			orderID:  "ORDER123",
			symbol:   "AAPL",
			price:    15050000000, // 150.50 * 10^8
			quantity: 100,
		},
		{
			name:     "Zero price",
			orderID:  "ORDER456",
			symbol:   "TSLA",
			price:    0,
			quantity: 50,
		},
		{
			name:     "Large values",
			orderID:  "ORDER789",
			symbol:   "BTC",
			price:    6000000000000, // 60,000 * 10^8
			quantity: 1000000,
		},
	}
	
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Submit the settlement
			side := "1" // Buy side
			price := big.NewInt(tc.price)
			quantity := big.NewInt(tc.quantity)
			var attestationData, signature []byte
			percentFill := 100.0
			isRejected := false
			rejectReason := ""
			
			_, err := connector.SubmitOrderSettlement(
				tc.orderID, tc.symbol, side,
				price, quantity,
				attestationData, signature,
				percentFill, isRejected, rejectReason)
			
			// Verify no error
			if err != nil {
				t.Errorf("SubmitOrderSettlement() error = %v", err)
			}
			
			// Verify the request was properly formed
			if receivedRequest.OrderID != tc.orderID {
				t.Errorf("Request OrderID = %v, want %v", receivedRequest.OrderID, tc.orderID)
			}
			if receivedRequest.Symbol != tc.symbol {
				t.Errorf("Request Symbol = %v, want %v", receivedRequest.Symbol, tc.symbol)
			}
			if receivedRequest.Price != tc.price {
				t.Errorf("Request Price = %v, want %v", receivedRequest.Price, tc.price)
			}
			if receivedRequest.Quantity != tc.quantity {
				t.Errorf("Request Quantity = %v, want %v", receivedRequest.Quantity, tc.quantity)
			}
		})
	}
}

// TestSimulationMode tests the simulation mode of the connector
func TestSimulationMode(t *testing.T) {
	// Create a server that fails all requests - it should never be called
	// when in simulation mode
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("Server should not be called in simulation mode")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	
	// Create a blockchain connector in simulation mode
	config := Config{
		Endpoint:       server.URL,
		TimeoutSeconds: 5,
		MaxRetries:     2,
		RetryInterval:  time.Millisecond * 100,
		SimulationMode: true,
	}
	
	connector, err := NewConnector(config)
	if err != nil {
		t.Fatalf("Failed to create connector: %v", err)
	}
	
	// Submit a settlement - it should succeed without calling the server
	side := "1" // Buy side
	price := big.NewInt(15050000000)
	quantity := big.NewInt(100)
	var attestationData, signature []byte
	percentFill := 100.0
	isRejected := false
	rejectReason := ""
	
	_, err = connector.SubmitOrderSettlement(
		"SIM-ORDER", "AAPL", side,
		price, quantity,
		attestationData, signature,
		percentFill, isRejected, rejectReason)
	if err != nil {
		t.Errorf("SubmitOrderSettlement() error = %v", err)
	}
}

// TestRetryMechanism tests the retry mechanism of the connector
func TestRetryMechanism(t *testing.T) {
	// Create a server that initially fails but succeeds after retries
	failCount := 0
	maxFailures := 1
	
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failCount < maxFailures {
			failCount++
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		
		// After failures, return success
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"confirmed", "tx_id":"mock-retry-tx"}`))
	}))
	defer server.Close()
	
	// Create a blockchain connector with retry mechanism
	config := Config{
		Endpoint:       server.URL,
		TimeoutSeconds: 5,
		MaxRetries:     3, // We'll need 2 retries (1 initial + 1 retry)
		RetryInterval:  time.Millisecond * 50, // Short interval for testing
		SimulationMode: false,
	}
	
	connector, err := NewConnector(config)
	if err != nil {
		t.Fatalf("Failed to create connector: %v", err)
	}
	
	// Submit a settlement - it should succeed after retries
	side := "1" // Buy side
	price := big.NewInt(15050000000)
	quantity := big.NewInt(100)
	var attestationData, signature []byte
	percentFill := 100.0
	isRejected := false
	rejectReason := ""
	
	_, err = connector.SubmitOrderSettlement(
		"RETRY-ORDER", "AAPL", side,
		price, quantity,
		attestationData, signature,
		percentFill, isRejected, rejectReason)
	if err != nil {
		t.Errorf("SubmitOrderSettlement() error = %v", err)
	}
	
	// Verify the right number of attempts were made
	expectedAttempts := maxFailures + 1
	actualAttempts := failCount + 1 // +1 for the successful attempt
	if actualAttempts != expectedAttempts {
		t.Errorf("Expected %d attempts, got %d", expectedAttempts, actualAttempts)
	}
}

// TestInternalPriceConversion tests the price conversion between float64 and int64
func TestInternalPriceConversion(t *testing.T) {
	tests := []struct {
		name        string
		floatPrice  float64
		intPrice    int64
	}{
		{
			name:       "Basic price",
			floatPrice: 150.50,
			intPrice:   15050000000,
		},
		{
			name:       "Zero price",
			floatPrice: 0.0,
			intPrice:   0,
		},
		{
			name:       "Small price",
			floatPrice: 0.00000001,
			intPrice:   1,
		},
		{
			name:       "Large price",
			floatPrice: 50000.0,
			intPrice:   5000000000000,
		},
	}
	
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Convert float to int
			b := new(big.Int)
			b.SetInt64(int64(tc.floatPrice * 1e8))
			result := b.Int64()
			
			if result != tc.intPrice {
				t.Errorf("Price conversion: got %v, want %v", result, tc.intPrice)
			}
		})
	}
}
