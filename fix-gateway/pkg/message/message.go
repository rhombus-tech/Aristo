// Copyright (C) 2025. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"
	"log"
	"math/big"
	"strconv"
	"time"

	"github.com/quickfixgo/quickfix"
	"github.com/rhombus-tech/aristo/fix-gateway/pkg/attestation"
	"github.com/rhombus-tech/aristo/fix-gateway/pkg/blockchain"
)

// MessageType represents FIX message types
type MessageType string

// FIX message types
const (
	MessageTypeNewOrderSingle        MessageType = "D"
	MessageTypeExecutionReport       MessageType = "8"
	MessageTypeOrderCancelRequest    MessageType = "F"
	MessageTypeOrderCancelReject     MessageType = "9"
	MessageTypeMarketDataRequest     MessageType = "V"
	MessageTypeMarketDataSnapshotFull MessageType = "W"
	MessageTypeMarketDataIncrementalRefresh MessageType = "X"
)



// FIX tag constants
const (
	TagBeginString   = 8
	TagBodyLength    = 9
	TagMsgType       = 35
	TagSenderCompID  = 49
	TagTargetCompID  = 56
	TagMsgSeqNum     = 34
	TagSendingTime   = 52
	TagCheckSum      = 10
	TagSignature     = 89 // Custom field for TEE attestation
	
	// Common fields
	TagSymbol        = 55
	TagSide          = 54
	TagOrderQty      = 38
	TagOrdType       = 40
	TagPrice         = 44
	TagTimeInForce   = 59
	
	// Order fields
	TagClOrdID       = 11
	TagOrigClOrdID   = 41
	TagOrderID       = 37
	TagExecID        = 17
	TagOrdStatus     = 39
	TagExecType      = 150
	TagCumQty        = 14  // Cumulative quantity
	TagAvgPx         = 6   // Average price
	TagLastPx        = 31  // Last price
	TagLastQty       = 32  // Last quantity
	TagText          = 58  // Text message/reason
	
	// Market data fields
	TagMDReqID      = 262
)

// Order status values
const (
	OrdStatusNew             = "0"
	OrdStatusPartiallyFilled = "1"
	OrdStatusFilled          = "2"
	OrdStatusDoneForDay      = "3"
	OrdStatusCanceled        = "4"
	OrdStatusReplaced        = "5"
	OrdStatusPendingCancel   = "6"
	OrdStatusStopped         = "7"
	OrdStatusRejected        = "8"
	OrdStatusSuspended       = "9"
	OrdStatusPendingNew      = "A"
	OrdStatusCalculated      = "B"
	OrdStatusExpired         = "C"
	OrdStatusAcceptedForBidding = "D"
	OrdStatusPendingReplace  = "E"
)

// Global blockchain connector instance
var (
	blockchainConnector *blockchain.Connector
)

// InitBlockchainConnector initializes the blockchain connector
func InitBlockchainConnector(config blockchain.Config) error {
	var err error
	blockchainConnector, err = blockchain.NewConnector(config)
	if err != nil {
		return fmt.Errorf("failed to initialize blockchain connector: %w", err)
	}
	return nil
}

// InitBlockchainConnectorWithDefaults initializes the blockchain connector with default config
func InitBlockchainConnectorWithDefaults() error {
	config := blockchain.DefaultConfig()
	return InitBlockchainConnector(config)
}

// MessageHandler handles FIX messages with TEE attestation
type MessageHandler struct {
	// attestationProvider provides TEE attestation services
	attestationProvider *attestation.TEEProvider
	
	// msgTypeHandlers contains handlers for each message type
	msgTypeHandlers map[MessageType]MessageTypeHandler
}

// MessageTypeHandler handles a specific message type
type MessageTypeHandler interface {
	// Handle handles a FIX message
	Handle(msg *quickfix.Message) error
}

// NewMessageHandler creates a new message handler
func NewMessageHandler(attestationProvider *attestation.TEEProvider) *MessageHandler {
	h := &MessageHandler{
		attestationProvider: attestationProvider,
		msgTypeHandlers:     make(map[MessageType]MessageTypeHandler),
	}
	
	// Register handlers
	h.registerHandlers()
	
	return h
}

// registerHandlers registers handlers for each message type
func (h *MessageHandler) registerHandlers() {
	// Register order handlers
	h.msgTypeHandlers[MessageTypeNewOrderSingle] = NewOrderSingleHandler(h.attestationProvider)
	h.msgTypeHandlers[MessageTypeExecutionReport] = ExecutionReportHandler(h.attestationProvider)
	h.msgTypeHandlers[MessageTypeOrderCancelRequest] = OrderCancelRequestHandler(h.attestationProvider)
	
	// Register market data handlers
	h.msgTypeHandlers[MessageTypeMarketDataRequest] = MarketDataRequestHandler(h.attestationProvider)
	h.msgTypeHandlers[MessageTypeMarketDataSnapshotFull] = MarketDataSnapshotHandler(h.attestationProvider)
	h.msgTypeHandlers[MessageTypeMarketDataIncrementalRefresh] = MarketDataIncrementalHandler(h.attestationProvider)
}

// HandleMessage handles a FIX message
func (h *MessageHandler) HandleMessage(msg *quickfix.Message) error {
	// Extract message type
	msgTypeStr, err := msg.MsgType()
	if err != nil {
		return fmt.Errorf("failed to get message type: %w", err)
	}
	
	msgType := MessageType(msgTypeStr)
	
	// Get handler for message type
	handler, ok := h.msgTypeHandlers[msgType]
	if !ok {
		// Use default handler if no specific handler
		return h.handleDefault(msg, msgType)
	}
	
	// Handle message
	return handler.Handle(msg)
}

// handleDefault handles messages with no specific handler
func (h *MessageHandler) handleDefault(msg *quickfix.Message, msgType MessageType) error {
	// Log the message type
	fmt.Printf("Received message of type %s with no specific handler\n", msgType)
	
	// Just acknowledge receipt for now
	return nil
}

// AddAttestation adds attestation to a message
func (h *MessageHandler) AddAttestation(msg *quickfix.Message) error {
	// Convert message to bytes
	msgBytes := msg.Bytes()
	
	// Get attestation
	attestation, err := h.attestationProvider.AttestMessage(msgBytes)
	if err != nil {
		return fmt.Errorf("failed to attest message: %w", err)
	}
	
	// Add attestation to message
	if err := msg.Body.SetString(TagSignature, string(attestation)); err != nil {
		return fmt.Errorf("failed to set attestation: %v", err)
	}
	
	return nil
}

// VerifyAttestation verifies the attestation in a message
func (h *MessageHandler) VerifyAttestation(msg *quickfix.Message) error {
	// Get attestation from message
	signatureStr, err := msg.Body.GetString(TagSignature)
	if err != nil {
		return fmt.Errorf("failed to get attestation: %v", err)
	}
	
	// Create a copy of the message for verification
	msgCopy := quickfix.NewMessage()
	msg.CopyInto(msgCopy)
	
	// In v0.9.7, we need to create a new message without the signature
	// Unfortunately we need to handle signature removal manually
	newMsg := quickfix.NewMessage()
	msg.CopyInto(newMsg)
	
	// Remove the signature field from the message copy
	// In v0.9.7, there's no direct RemoveField, so we'll recreate it without the field
	// We're just creating a clean message without copying the signature field
	// Not using the msgCopy parameter since we're taking another approach
	newMsg.Body.Clear()
	
	// Convert message to bytes
	msgBytes := newMsg.Bytes()
	
	// Verify attestation
	if err := h.attestationProvider.VerifyMessageAttestation(
		msgBytes,
		[]byte(signatureStr),
	); err != nil {
		return fmt.Errorf("failed to verify attestation: %v", err)
	}
	
	return nil
}

// ExecutionReportHandler handles execution report messages
func ExecutionReportHandler(provider *attestation.TEEProvider) MessageTypeHandler {
	return &executionReportHandler{
		attestationProvider: provider,
	}
}

type executionReportHandler struct {
	attestationProvider *attestation.TEEProvider
}

func (h *executionReportHandler) Handle(msg *quickfix.Message) error {
	// Get order status
	statusField, err := msg.Body.GetString(TagOrdStatus)
	if err != nil {
		return fmt.Errorf("failed to get order status: %v", err)
	}
	
	// Handle execution report based on order status
	switch statusField {
	case OrdStatusFilled:
		return h.handleFilled(msg)
	case OrdStatusPartiallyFilled:
		return h.handlePartiallyFilled(msg)
	case OrdStatusRejected:
		return h.handleRejected(msg)
	default:
		return h.handleDefault(msg, statusField)
	}
}

func (h *executionReportHandler) handleFilled(msg *quickfix.Message) error {
	// Get client order ID
	clOrdID, err := msg.Body.GetString(TagClOrdID)
	if err != nil {
		return fmt.Errorf("failed to get client order ID: %v", err)
	}
	
	// Get symbol
	symbol, err := msg.Body.GetString(TagSymbol)
	if err != nil {
		return fmt.Errorf("failed to get symbol: %v", err)
	}
	
	// Get side
	side, err := msg.Body.GetString(TagSide)
	if err != nil {
		return fmt.Errorf("failed to get side: %v", err)
	}
	
	// Get order type
	ordType, err := msg.Body.GetString(TagOrdType)
	if err != nil {
		return fmt.Errorf("failed to get order type: %v", err)
	}
	
	// Get price if it exists (not for market orders)
	var priceStr string
	if ordType != "1" { // Market orders don't have price
		priceStr, err = msg.Body.GetString(TagPrice)
		if err != nil {
			return fmt.Errorf("failed to get price: %v", err)
		}
	}
	
	// Get quantity
	qtyStr, err := msg.Body.GetString(TagOrderQty)
	if err != nil {
		return fmt.Errorf("failed to get quantity: %v", err)
	}
	qtyFloat, parseErr := strconv.ParseFloat(qtyStr, 64)
	if parseErr != nil {
		return fmt.Errorf("failed to parse quantity: %v", parseErr)
	}
	quantity := big.NewInt(int64(qtyFloat * 1e8)) // Convert to smallest unit
	
	// Parse the price to a big.Int for blockchain usage (if not a market order)
	var price *big.Int
	if ordType != "1" { // Not a market order
		priceFloat, parseErr := strconv.ParseFloat(priceStr, 64)
		if parseErr != nil {
			return fmt.Errorf("failed to parse price: %v", parseErr)
		}
		// Convert to blockchain's smallest unit (assuming 8 decimal places)
		price = big.NewInt(int64(priceFloat * 1e8))
	}
	
	// Create attestation data for the filled order
	attestationData := fmt.Sprintf(
		"FIX.FILLED.%s.%s.%s.%s.%s",
		clOrdID,
		symbol,
		side,
		priceStr,
		qtyStr,
	)
	
	log.Printf("Processing filled order: %s", attestationData)
	
	// Generate message signature using TEE attestation
	attSignature, attErr := h.attestationProvider.AttestMessage([]byte(attestationData))
	if attErr != nil {
		return fmt.Errorf("failed to create attestation signature: %v", attErr)
	}
	
	// Submit order settlement to blockchain
	// First, ensure blockchain connector is initialized
	if blockchainConnector == nil {
		if err := InitBlockchainConnectorWithDefaults(); err != nil {
			log.Printf("Failed to initialize blockchain connector: %v", err)
			return fmt.Errorf("blockchain connector not initialized: %v", err)
		}
	}
	
	// Submit order settlement to blockchain
	txID, submitErr := blockchainConnector.SubmitOrderSettlement(
		clOrdID,
		symbol,
		side,
		price,
		quantity,
		[]byte(attestationData),
		attSignature,
		100.0, // 100% filled
		false, // not rejected
		"",   // no rejection reason
	)
	if submitErr != nil {
		return fmt.Errorf("failed to submit order settlement to blockchain: %v", submitErr)
	}
	
	log.Printf("Order %s settlement processed with blockchain transaction ID: %s", clOrdID, txID)
	return nil
}

func (h *executionReportHandler) handlePartiallyFilled(msg *quickfix.Message) error {
	// Get client order ID
	clOrdID, err := msg.Body.GetString(TagClOrdID)
	if err != nil {
		return fmt.Errorf("failed to get client order ID: %v", err)
	}
	
	// Get symbol
	symbol, err := msg.Body.GetString(TagSymbol)
	if err != nil {
		return fmt.Errorf("failed to get symbol: %v", err)
	}
	
	// Get side
	side, err := msg.Body.GetString(TagSide)
	if err != nil {
		return fmt.Errorf("failed to get side: %v", err)
	}
	
	// Get order type
	ordType, err := msg.Body.GetString(TagOrdType)
	if err != nil {
		return fmt.Errorf("failed to get order type: %v", err)
	}
	
	// Get price if exists (not for market orders)
	var priceStr string
	if ordType != "1" { // Market orders don't have price
		priceStr, err = msg.Body.GetString(TagPrice)
		if err != nil {
			return fmt.Errorf("failed to get price: %v", err)
		}
	}
	
	// Get filled quantity
	cumQtyStr, err := msg.Body.GetString(TagCumQty)
	if err != nil {
		return fmt.Errorf("failed to get cumulative quantity: %v", err)
	}
	cumQtyFloat, parseErr := strconv.ParseFloat(cumQtyStr, 64)
	if parseErr != nil {
		return fmt.Errorf("failed to parse cumulative quantity: %v", parseErr)
	}
	
	// Get total quantity
	orderQtyStr, err := msg.Body.GetString(TagOrderQty)
	if err != nil {
		return fmt.Errorf("failed to get order quantity: %v", err)
	}
	orderQtyFloat, parseErr := strconv.ParseFloat(orderQtyStr, 64)
	if parseErr != nil {
		return fmt.Errorf("failed to parse order quantity: %v", parseErr)
	}
	
	// Calculate percentage filled
	percentFilled := (cumQtyFloat / orderQtyFloat) * 100
	
	// Parse the price to a big.Int for blockchain usage
	var price *big.Int
	if ordType != "1" { // Not a market order
		priceFloat, parseErr := strconv.ParseFloat(priceStr, 64)
		if parseErr != nil {
			return fmt.Errorf("failed to parse price: %v", parseErr)
		}
		// Convert to blockchain's smallest unit
		price = big.NewInt(int64(priceFloat * 1e8))
	}
	
	// Convert filled quantity to blockchain unit
	filledQuantity := big.NewInt(int64(cumQtyFloat * 1e8))
	
	// Create attestation data
	attestationData := fmt.Sprintf(
		"FIX.PARTIALLY_FILLED.%s.%s.%s.%s.%s.%.2f%%",
		clOrdID,
		symbol,
		side,
		priceStr,
		cumQtyStr,
		percentFilled,
	)
	
	log.Printf("Processing partially filled order: %s", attestationData)
	
	// Generate message signature using TEE attestation
	attSignature, signErr := h.attestationProvider.AttestMessage([]byte(attestationData))
	if signErr != nil {
		return fmt.Errorf("failed to create attestation signature: %v", signErr)
	}
	
	// Submit partial order settlement to blockchain
	// First, ensure blockchain connector is initialized
	if blockchainConnector == nil {
		if err := InitBlockchainConnectorWithDefaults(); err != nil {
			log.Printf("Failed to initialize blockchain connector: %v", err)
			return fmt.Errorf("blockchain connector not initialized: %v", err)
		}
	}
	
	// Submit partial order settlement to blockchain
	txID, submitErr := blockchainConnector.SubmitOrderSettlement(
		clOrdID,
		symbol,
		side,
		price,
		filledQuantity,
		[]byte(attestationData),
		attSignature,
		percentFilled, // percentage filled
		false,        // not rejected
		"",          // no rejection reason
	)
	if submitErr != nil {
		return fmt.Errorf("failed to submit partial order settlement to blockchain: %v", submitErr)
	}
	
	log.Printf("Partial order %s settlement processed with blockchain transaction ID: %s (%.2f%%)", 
		clOrdID, txID, percentFilled)
	return nil
}

func (h *executionReportHandler) handleRejected(msg *quickfix.Message) error {
	// Get client order ID
	clOrdID, err := msg.Body.GetString(TagClOrdID)
	if err != nil {
		return fmt.Errorf("failed to get client order ID: %v", err)
	}
	
	// Get symbol
	symbol, err := msg.Body.GetString(TagSymbol)
	if err != nil {
		return fmt.Errorf("failed to get symbol: %v", err)
	}
	
	// Get side (optional for rejected orders)
	side := "unknown"
	sideField, err := msg.Body.GetString(TagSide)
	if err == nil {
		side = sideField
	}
	
	// Get order type (optional for rejected orders)
	var ordType string
	ordTypeField, err := msg.Body.GetString(TagOrdType)
	if err == nil {
		ordType = ordTypeField
	}
	
	// Get price (optional for rejected orders)
	var price *big.Int
	priceField, err := msg.Body.GetString(TagPrice)
	if err == nil && ordType != "1" { // Not a market order
		priceFloat, parseErr := strconv.ParseFloat(priceField, 64)
		if parseErr == nil {
			// Convert to blockchain's smallest unit
			price = big.NewInt(int64(priceFloat * 1e8))
		}
	}
	
	// Get quantity (optional for rejected orders)
	var quantity *big.Int
	qtyField, err := msg.Body.GetString(TagOrderQty)
	if err == nil {
		qtyFloat, parseErr := strconv.ParseFloat(qtyField, 64)
		if parseErr == nil {
			// Convert to blockchain's smallest unit
			quantity = big.NewInt(int64(qtyFloat * 1e8))
		}
	}
	
	// Get rejection reason if available
	var reason string
	reasonField, err := msg.Body.GetString(TagText)
	if err == nil {
		reason = reasonField
	} else {
		reason = "Unknown reason"
	}
	
	// Create attestation data for the rejection
	attestationData := fmt.Sprintf(
		"FIX.REJECTED.%s.%s.%s",
		clOrdID,
		symbol,
		reason,
	)
	
	log.Printf("Processing rejected order: %s", attestationData)
	
	// Generate message signature using the TEE attestation provider
	attSignature, signErr := h.attestationProvider.AttestMessage([]byte(attestationData))
	if signErr != nil {
		return fmt.Errorf("failed to create attestation signature: %v", signErr)
	}
	
	// Submit order rejection to blockchain
	// First, ensure blockchain connector is initialized
	if blockchainConnector == nil {
		if err := InitBlockchainConnectorWithDefaults(); err != nil {
			log.Printf("Failed to initialize blockchain connector: %v", err)
			return fmt.Errorf("blockchain connector not initialized: %v", err)
		}
	}
	
	// Submit order rejection to blockchain
	txID, submitErr := blockchainConnector.SubmitOrderSettlement(
		clOrdID,
		symbol,
		side,
		price,
		quantity,
		[]byte(attestationData),
		attSignature,
		0.0,         // 0% filled for rejected orders
		true,        // is rejected
		reason,      // rejection reason
	)
	if submitErr != nil {
		return fmt.Errorf("failed to submit order rejection to blockchain: %v", submitErr)
	}
	
	log.Printf("Order %s rejection processed with blockchain transaction ID: %s", clOrdID, txID)
	return nil
}

func (h *executionReportHandler) handleDefault(msg *quickfix.Message, status string) error {
	// Handle other order statuses
	orderID, _ := msg.Body.GetString(TagOrderID)
	
	// Log the status change
	fmt.Printf("Order %s status changed to %s\n", orderID, status)
	return nil
}

// NewOrderSingleHandler handles new order single messages
func NewOrderSingleHandler(provider *attestation.TEEProvider) MessageTypeHandler {
	return &newOrderSingleHandler{
		attestationProvider: provider,
	}
}

type newOrderSingleHandler struct {
	attestationProvider *attestation.TEEProvider
}

func (h *newOrderSingleHandler) Handle(msg *quickfix.Message) error {
	// Get client order ID
	clOrdID, err := msg.Body.GetString(TagClOrdID)
	if err != nil {
		return fmt.Errorf("failed to get client order ID: %v", err)
	}

	// Get symbol
	symbol, err := msg.Body.GetString(TagSymbol)
	if err != nil {
		return fmt.Errorf("failed to get symbol: %v", err)
	}

	// Process new order with attestation
	// Record the new order intent on the blockchain
	log.Printf("Processing new order %s for %s", clOrdID, symbol)

	return nil
}

// OrderCancelRequestHandler handles order cancel request messages
func OrderCancelRequestHandler(provider *attestation.TEEProvider) MessageTypeHandler {
	return &orderCancelRequestHandler{
		attestationProvider: provider,
	}
}

type orderCancelRequestHandler struct {
	attestationProvider *attestation.TEEProvider
}

func (h *orderCancelRequestHandler) Handle(msg *quickfix.Message) error {
	// Get client order ID
	clOrdID, err := msg.Body.GetString(TagClOrdID)
	if err != nil {
		return fmt.Errorf("failed to get client order ID: %v", err)
	}
	
	// Get original client order ID
	origClOrdID, err := msg.Body.GetString(TagOrigClOrdID)
	if err != nil {
		return fmt.Errorf("failed to get original client order ID: %v", err)
	}
	
	// Process cancel request with attestation
	// This would integrate with your blockchain for cancel integrity
	
	fmt.Printf("Processing cancel request %s for order %s\n", clOrdID, origClOrdID)
	
	return nil
}

// MarketDataRequestHandler handles market data request messages
func MarketDataRequestHandler(provider *attestation.TEEProvider) MessageTypeHandler {
	return &marketDataRequestHandler{
		attestationProvider: provider,
	}
}

type marketDataRequestHandler struct {
	attestationProvider *attestation.TEEProvider
}

func (h *marketDataRequestHandler) Handle(msg *quickfix.Message) error {
	// Get market data request ID
	reqID, err := msg.Body.GetString(TagMDReqID)
	if err != nil {
		return fmt.Errorf("failed to get market data request ID: %v", err)
	}

	// Process market data request with attestation
	fmt.Printf("Processing market data request %s\n", reqID)

	return nil
}

// MarketDataSnapshotHandler handles market data snapshot messages
func MarketDataSnapshotHandler(provider *attestation.TEEProvider) MessageTypeHandler {
	return &marketDataSnapshotHandler{
		attestationProvider: provider,
	}
}

type marketDataSnapshotHandler struct {
	attestationProvider *attestation.TEEProvider
}

func (h *marketDataSnapshotHandler) Handle(msg *quickfix.Message) error {
	// Get market data request ID
	reqID, err := msg.Body.GetString(TagMDReqID)
	if err != nil {
		return fmt.Errorf("failed to get market data request ID: %v", err)
	}
	
	// Process market data snapshot with attestation
	// This would integrate with your blockchain for market data timestamping
	
	fmt.Printf("Processing market data snapshot for request %s\n", reqID)
	
	return nil
}

// MarketDataIncrementalHandler handles market data incremental refresh messages
func MarketDataIncrementalHandler(provider *attestation.TEEProvider) MessageTypeHandler {
	return &marketDataIncrementalHandler{
		attestationProvider: provider,
	}
}

type marketDataIncrementalHandler struct {
	attestationProvider *attestation.TEEProvider
}

func (h *marketDataIncrementalHandler) Handle(msg *quickfix.Message) error {
	// Process market data incremental refresh with attestation
	// This would integrate with your blockchain for market data timestamping
	
	// Set sending time
	timeNow := time.Now().Format("20060102-15:04:05.000")
	err := msg.Header.SetString(TagSendingTime, timeNow)
	if err != nil {
		return fmt.Errorf("failed to set sending time: %v", err)
	}
	
	// In a real implementation, you would:
	// 1. Verify attestation
	// 2. Create a blockchain transaction with market data timestamp
	// 3. Use your TEE attestation to secure the timestamp
	
	return nil
}
