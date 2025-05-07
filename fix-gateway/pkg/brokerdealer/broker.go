// Copyright (C) 2025. All rights reserved.
// See the file LICENSE for licensing terms.

package brokerdealer

import (
	"fmt"
	"time"

	"github.com/quickfixgo/quickfix"
	"github.com/rhombus-tech/aristo/fix-gateway/pkg/blockchain"
)

// Broker represents a single broker-dealer connection
type Broker struct {
	config         BrokerConfig
	manager        *Manager
	clientAccounts map[string]bool
	blockchainConn *blockchain.Connector
	sessionID      quickfix.SessionID
	isLoggedOn     bool
}

// ValidateAccount checks if an account is valid for this broker
func (b *Broker) ValidateAccount(account string) bool {
	return b.clientAccounts[account]
}

// GetID returns the broker's ID
func (b *Broker) GetID() string {
	return b.config.ID
}

// GetName returns the broker's name
func (b *Broker) GetName() string {
	return b.config.Name
}

// IsLoggedOn returns true if the broker is currently logged on
func (b *Broker) IsLoggedOn() bool {
	return b.isLoggedOn
}

// SetLoggedOn sets the logged on state
func (b *Broker) SetLoggedOn(state bool) {
	b.isLoggedOn = state
}

// HandleNewOrderSingle processes a new order single message from the broker
func (b *Broker) HandleNewOrderSingle(msg *quickfix.Message, sessionID quickfix.SessionID) quickfix.MessageRejectError {
	// Extract account from message
	var account quickfix.FIXString
	if err := msg.Body.GetField(1, &account); err != nil { // Tag 1 is Account
		return quickfix.NewMessageRejectError("Account field missing", 999, nil)
	}
	
	// Validate the account belongs to this broker
	if !b.ValidateAccount(string(account)) {
		return quickfix.NewMessageRejectError(fmt.Sprintf("Unknown account: %s", account), 999, nil)
	}
	
	// Enrich with broker information
	addBrokerInfo(msg, b.config.ID)
	
	// Forward to market
	if err := b.manager.ForwardToMarket(msg); err != nil {
		return quickfix.NewMessageRejectError(fmt.Sprintf("Failed to forward order: %v", err), 999, nil)
	}
	
	return nil
}

// HandleOrderCancelRequest processes a cancel request from the broker
func (b *Broker) HandleOrderCancelRequest(msg *quickfix.Message, sessionID quickfix.SessionID) quickfix.MessageRejectError {
	// Extract account from message
	var account quickfix.FIXString
	if err := msg.Body.GetField(1, &account); err != nil { // Tag 1 is Account
		return quickfix.NewMessageRejectError("Account field missing", 999, nil)
	}
	
	// Validate the account belongs to this broker
	if !b.ValidateAccount(string(account)) {
		return quickfix.NewMessageRejectError(fmt.Sprintf("Unknown account: %s", account), 999, nil)
	}
	
	// Enrich with broker information
	addBrokerInfo(msg, b.config.ID)
	
	// Forward to market
	if err := b.manager.ForwardToMarket(msg); err != nil {
		return quickfix.NewMessageRejectError(fmt.Sprintf("Failed to forward cancel request: %v", err), 999, nil)
	}
	
	return nil
}

// SendExecutionReport sends an execution report to the broker
func (b *Broker) SendExecutionReport(
	orderID string,
	execID string,
	account string,
	symbol string,
	side byte,
	ordStatus byte,
	execType byte,
	lastQty float64,
	lastPx float64,
	cumQty float64,
	avgPx float64,
	isRejected bool,
	rejectReason string,
) error {
	// Create a new execution report message
	msg := quickfix.NewMessage()
	header := msg.Header
	
	// Set message type to Execution Report
	header.SetField(35, quickfix.FIXString("8")) // 35=8 is ExecutionReport
	header.SetField(49, quickfix.FIXString(b.sessionID.SenderCompID)) // SenderCompID
	header.SetField(56, quickfix.FIXString(b.sessionID.TargetCompID)) // TargetCompID
	
	// Set required fields
	msg.Body.SetField(37, quickfix.FIXString(execID))   // ExecutionID
	msg.Body.SetField(11, quickfix.FIXString(orderID))  // ClOrdID
	msg.Body.SetField(17, quickfix.FIXString(execID))   // ExecID
	msg.Body.SetField(150, quickfix.FIXString(string(execType))) // ExecType
	msg.Body.SetField(39, quickfix.FIXString(string(ordStatus))) // OrdStatus
	msg.Body.SetField(55, quickfix.FIXString(symbol))  // Symbol
	msg.Body.SetField(54, quickfix.FIXString(string(side)))  // Side
	msg.Body.SetField(14, quickfix.FIXFloat(cumQty))  // CumQty
	msg.Body.SetField(6, quickfix.FIXFloat(avgPx))    // AvgPx
	
	// Set additional fields
	msg.Body.SetField(1, quickfix.FIXString(account))     // Account
	msg.Body.SetField(32, quickfix.FIXFloat(lastQty))    // LastQty
	msg.Body.SetField(31, quickfix.FIXFloat(lastPx))     // LastPx
	
	// Set timestamp
	timeStr := time.Now().Format("20060102-15:04:05.000")
	msg.Body.SetField(60, quickfix.FIXString(timeStr)) // TransactTime
	
	// Add broker information
	msg.Body.SetField(9000, quickfix.FIXString(b.config.ID)) // Custom broker ID field
	
	// Add rejection details if applicable
	if isRejected {
		msg.Body.SetField(58, quickfix.FIXString(rejectReason)) // Text
	}
	
	// Send message
	return quickfix.SendToTarget(msg, b.sessionID)
}

// addBrokerInfo adds broker identification to a message
func addBrokerInfo(msg *quickfix.Message, brokerID string) {
	// Add broker ID as a custom field
	msg.Body.SetField(9000, quickfix.FIXString(brokerID))
	
	// Add DeliverToCompID for routing
	msg.Header.SetField(128, quickfix.FIXString(brokerID)) // Tag 128 is DeliverToCompID
}
