// Copyright (C) 2025. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"math/big"
	"testing"
	"time"

	"github.com/rhombus-tech/aristo/fix-gateway/pkg/blockchain"
)

// TestBlockchainConnector tests direct interaction with the blockchain connector
func TestBlockchainConnector(t *testing.T) {
	// This test is just a placeholder for now, as we need to run it in an environment
	// with the actual dependencies available
	
	// Initialize blockchain connector
	connector, err := blockchain.NewConnector(blockchain.Config{
		Endpoint:       "http://localhost:9650",
		TimeoutSeconds: 5,
		MaxRetries:     2,
		RetryInterval:  time.Second,
		SimulationMode: true, // Use simulation mode for testing
	})
	
	if err != nil {
		t.Fatalf("Failed to initialize blockchain connector: %v", err)
	}
	
	// Use big integers for price and quantity
	price := big.NewInt(15025)    // $150.25
	quantity := big.NewInt(10000) // 100 shares
	
	// Test order settlement submission
	txID, err := connector.SubmitOrderSettlement(
		"order-123456",            // orderID
		"AAPL",                    // symbol
		"BUY",                     // side
		price,                     // price
		quantity,                  // quantity
		[]byte("attestation-data"), // attestationData
		[]byte("signature-data"),   // signature
		100.0,                     // percentFill (100% filled)
		false,                     // isRejected
		"",                       // rejectReason
	)
	
	// In simulation mode, we should get a transaction ID and no error
	if err != nil {
		t.Fatalf("Failed to submit order settlement: %v", err)
	}
	
	// Check that we got a transaction ID (even if it's simulated)
	if txID == "" {
		t.Fatal("Expected a transaction ID, got empty string")
	}
	
	t.Logf("Successfully submitted order settlement, transaction ID: %s", txID)
}

// TestRejectedOrder tests order rejection settlement
func TestRejectedOrder(t *testing.T) {
	// Initialize blockchain connector
	connector, err := blockchain.NewConnector(blockchain.Config{
		Endpoint:       "http://localhost:9650",
		TimeoutSeconds: 5,
		MaxRetries:     2,
		RetryInterval:  time.Second,
		SimulationMode: true,
	})
	
	if err != nil {
		t.Fatalf("Failed to initialize blockchain connector: %v", err)
	}
	
	// Use big integers for price and quantity
	price := big.NewInt(90000)    // $900.00
	quantity := big.NewInt(20000) // 200 shares
	
	// Test rejected order settlement submission
	txID, err := connector.SubmitOrderSettlement(
		"order-567890",            // orderID
		"TSLA",                    // symbol
		"BUY",                     // side
		price,                     // price
		quantity,                  // quantity
		[]byte("attestation-data"), // attestationData
		[]byte("signature-data"),   // signature
		0.0,                       // percentFill (0% filled - rejected)
		true,                      // isRejected
		"insufficient buying power", // rejectReason
	)
	
	// In simulation mode, we should get a transaction ID and no error
	if err != nil {
		t.Fatalf("Failed to submit rejected order settlement: %v", err)
	}
	
	// Check that we got a transaction ID (even if it's simulated)
	if txID == "" {
		t.Fatal("Expected a transaction ID, got empty string")
	}
	
	t.Logf("Successfully submitted rejected order settlement, transaction ID: %s", txID)
}

// TestPartiallyFilledOrder tests partially filled order settlement
func TestPartiallyFilledOrder(t *testing.T) {
	// Initialize blockchain connector
	connector, err := blockchain.NewConnector(blockchain.Config{
		Endpoint:       "http://localhost:9650",
		TimeoutSeconds: 5,
		MaxRetries:     2,
		RetryInterval:  time.Second,
		SimulationMode: true,
	})
	
	if err != nil {
		t.Fatalf("Failed to initialize blockchain connector: %v", err)
	}
	
	// Use big integers for price and quantity
	price := big.NewInt(28050)    // $280.50
	quantity := big.NewInt(50000) // 500 shares
	
	// Test partially filled order settlement submission
	txID, err := connector.SubmitOrderSettlement(
		"order-345678",            // orderID
		"MSFT",                    // symbol
		"SELL",                    // side
		price,                     // price
		quantity,                  // quantity
		[]byte("attestation-data"), // attestationData
		[]byte("signature-data"),   // signature
		50.0,                      // percentFill (50% filled)
		false,                     // isRejected
		"",                       // rejectReason
	)
	
	// In simulation mode, we should get a transaction ID and no error
	if err != nil {
		t.Fatalf("Failed to submit partially filled order settlement: %v", err)
	}
	
	// Check that we got a transaction ID (even if it's simulated)
	if txID == "" {
		t.Fatal("Expected a transaction ID, got empty string")
	}
	
	t.Logf("Successfully submitted partially filled order settlement, transaction ID: %s", txID)
}
