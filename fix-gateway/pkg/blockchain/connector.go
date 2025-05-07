// Copyright (C) 2025. All rights reserved.
// See the file LICENSE for licensing terms.

package blockchain

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"sync"
	"time"
)

// Connector serves as the integration layer between the FIX message handler and the blockchain
type Connector struct {
	client    *Client
	config    Config
	cache     map[string]string // Maps order IDs to transaction IDs
	cacheLock sync.RWMutex
}

// NewConnector creates a new blockchain connector
func NewConnector(config Config) (*Connector, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid blockchain configuration: %w", err)
	}

	client := NewClient(config.Endpoint)
	
	return &Connector{
		client:    client,
		config:    config,
		cache:     make(map[string]string),
		cacheLock: sync.RWMutex{},
	}, nil
}

// SubmitOrderSettlement processes an order settlement and submits it to the blockchain
func (c *Connector) SubmitOrderSettlement(
	orderID, symbol, side string,
	price, quantity *big.Int,
	attestationData, signature []byte,
	percentFill float64,
	isRejected bool,
	rejectReason string,
) (string, error) {
	// Skip if in simulation mode
	if c.config.SimulationMode {
		txID := fmt.Sprintf("sim-%s-%d", orderID, time.Now().UnixNano())
		log.Printf("[Simulation] Order %s settlement simulated with transaction ID: %s", orderID, txID)
		
		// Cache the transaction ID
		c.cacheLock.Lock()
		c.cache[orderID] = txID
		c.cacheLock.Unlock()
		
		return txID, nil
	}

	// Prepare settlement data
	data := &OrderSettlementData{
		OrderID:      orderID,
		Symbol:       symbol,
		Side:         side,
		Price:        price,
		Quantity:     quantity,
		Timestamp:    time.Now().Unix(),
		AttestedData: attestationData,
		Signature:    signature,
		PercentFill:  percentFill,
		IsRejected:   isRejected,
		RejectReason: rejectReason,
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), c.config.GetTimeout())
	defer cancel()

	// Submit with retries
	var txID string
	var lastErr error

	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("Retrying blockchain submission for order %s (attempt %d/%d)", 
				orderID, attempt, c.config.MaxRetries)
			time.Sleep(c.config.RetryInterval)
		}

		txID, lastErr = c.client.SubmitOrderSettlement(ctx, data)
		if lastErr == nil {
			break
		}
	}

	if lastErr != nil {
		log.Printf("Failed to submit order %s to blockchain after %d attempts: %v", 
			orderID, c.config.MaxRetries+1, lastErr)
		return "", fmt.Errorf("blockchain submission failed: %w", lastErr)
	}

	log.Printf("Order %s settlement submitted to blockchain with transaction ID: %s", orderID, txID)
	
	// Cache the transaction ID
	c.cacheLock.Lock()
	c.cache[orderID] = txID
	c.cacheLock.Unlock()

	return txID, nil
}

// GetTransactionID retrieves the cached transaction ID for an order
func (c *Connector) GetTransactionID(orderID string) (string, bool) {
	c.cacheLock.RLock()
	defer c.cacheLock.RUnlock()
	
	txID, exists := c.cache[orderID]
	return txID, exists
}

// CheckTransactionStatus checks the status of a transaction on the blockchain
func (c *Connector) CheckTransactionStatus(ctx context.Context, txID string) (string, error) {
	// In a real implementation, this would query the blockchain for the transaction status
	// For now, we'll just return a placeholder status
	
	if c.config.SimulationMode {
		return "CONFIRMED", nil
	}
	
	// TODO: Implement actual status checking by querying the blockchain
	return "PENDING", nil
}
