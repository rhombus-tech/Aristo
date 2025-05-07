// Copyright (C) 2025. All rights reserved.
// See the file LICENSE for licensing terms.

package blockchain

import (
	"fmt"
	"math/big"
)

// OrderPayload defines the structure for an order settlement on the blockchain
type OrderPayload struct {
	OrderID      string   `json:"orderId"`
	Symbol       string   `json:"symbol"`
	Side         string   `json:"side"`
	Price        *big.Int `json:"price,omitempty"`
	Quantity     *big.Int `json:"quantity,omitempty"`
	Timestamp    int64    `json:"timestamp"`
	PercentFill  float64  `json:"percentFill,omitempty"`
	IsRejected   bool     `json:"isRejected,omitempty"`
	RejectReason string   `json:"rejectReason,omitempty"`
	Attestation  []byte   `json:"attestation,omitempty"`
}

// FormatOrderStatus returns a string representation of the order status
func FormatOrderStatus(orderID, symbol, side string, price, quantity *big.Int, percentFill float64, isRejected bool, rejectReason string) string {
	var priceStr, qtyStr string
	if price != nil {
		priceStr = price.String()
	} else {
		priceStr = "N/A"
	}
	
	if quantity != nil {
		qtyStr = quantity.String()
	} else {
		qtyStr = "N/A"
	}
	
	var status string
	if isRejected {
		status = fmt.Sprintf("REJECTED (%s)", rejectReason)
	} else if percentFill < 100 {
		status = fmt.Sprintf("PARTIAL (%.2f%%)", percentFill)
	} else {
		status = "FILLED"
	}
	
	return fmt.Sprintf("OrderSettlement[Order=%s, Symbol=%s, Side=%s, Price=%s, Qty=%s, Status=%s]",
		orderID, symbol, side, priceStr, qtyStr, status)
}
