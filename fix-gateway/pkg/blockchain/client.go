// Copyright (C) 2025. All rights reserved.
// See the file LICENSE for licensing terms.

package blockchain

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"time"
)

const (
	defaultTimeout = 10 * time.Second
)

// Client handles interaction with the blockchain network
type Client struct {
	endpoint   string
	httpClient *http.Client
}

// NewClient creates a new blockchain client
func NewClient(endpoint string) *Client {
	return &Client{
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

// OrderSettlementData represents order data to be stored on the blockchain
type OrderSettlementData struct {
	OrderID      string   `json:"orderId"`
	Symbol       string   `json:"symbol"`
	Side         string   `json:"side"`
	Price        *big.Int `json:"price,omitempty"`
	Quantity     *big.Int `json:"quantity,omitempty"`
	Timestamp    int64    `json:"timestamp"`
	AttestedData []byte   `json:"attestedData"`
	Signature    []byte   `json:"signature"`
	PercentFill  float64  `json:"percentFill,omitempty"`
	IsRejected   bool     `json:"isRejected,omitempty"`
	RejectReason string   `json:"rejectReason,omitempty"`
}

// CreateSettlementPayload creates a blockchain transaction payload for an order settlement
func (c *Client) CreateSettlementPayload(data *OrderSettlementData) ([]byte, error) {
	// Create a simple JSON payload with the order data
	payload := map[string]interface{}{
		"orderID":     data.OrderID,
		"symbol":      data.Symbol,
		"side":        data.Side,
		"timestamp":   data.Timestamp,
		"attestation": hex.EncodeToString(data.Signature),
		"percentFill": data.PercentFill,
		"isRejected":  data.IsRejected,
	}

	// Add price and quantity if present
	if data.Price != nil {
		payload["price"] = data.Price.String()
	}

	if data.Quantity != nil {
		payload["quantity"] = data.Quantity.String()
	}

	if data.RejectReason != "" {
		payload["rejectReason"] = data.RejectReason
	}

	// Marshal to JSON
	return json.Marshal(payload)
}

// SubmitTransaction submits a transaction to the blockchain
func (c *Client) SubmitTransaction(ctx context.Context, payload []byte) (string, error) {
	// 1. Prepare the request body
	reqBody := struct {
		Data string `json:"data"`
	}{
		Data: hex.EncodeToString(payload),
	}

	reqData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	// 2. Create and send the request
	// Adjust the endpoint as needed to match your blockchain's API
	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint+"/submit", bytes.NewReader(reqData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// 3. Process the response
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("unsuccessful status code: %d, body: %s", resp.StatusCode, string(body))
	}

	var result struct {
		TxID string `json:"txId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return result.TxID, nil
}

// SubmitOrderSettlement creates and submits an order settlement transaction
func (c *Client) SubmitOrderSettlement(ctx context.Context, data *OrderSettlementData) (string, error) {
	// 1. Create the transaction payload
	payload, err := c.CreateSettlementPayload(data)
	if err != nil {
		return "", fmt.Errorf("failed to create transaction payload: %w", err)
	}

	// 2. Submit the transaction
	txID, err := c.SubmitTransaction(ctx, payload)
	if err != nil {
		return "", fmt.Errorf("failed to submit transaction: %w", err)
	}

	return txID, nil
}
