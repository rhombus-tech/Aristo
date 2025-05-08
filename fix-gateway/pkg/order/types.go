// See the file LICENSE for licensing terms.

package order

import (
	"time"

	"github.com/quickfixgo/quickfix"
)

// OrderStatus represents the current status of an order
type OrderStatus string

// Order statuses
const (
	StatusNew        OrderStatus = "NEW"
	StatusPending    OrderStatus = "PENDING"
	StatusPartial    OrderStatus = "PARTIALLY_FILLED"
	StatusFilled     OrderStatus = "FILLED"
	StatusCanceled   OrderStatus = "CANCELED"
	StatusRejected   OrderStatus = "REJECTED"
	StatusReplaced   OrderStatus = "REPLACED"
	StatusSuspended  OrderStatus = "SUSPENDED"
	StatusExpired    OrderStatus = "EXPIRED"
)

// Side represents the side of an order (buy/sell)
type Side string

// Order sides
const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

// Type represents the type of an order
type Type string

// Order types
const (
	TypeMarket     Type = "MARKET"
	TypeLimit      Type = "LIMIT"
	TypeStop       Type = "STOP"
	TypeStopLimit  Type = "STOP_LIMIT"
)

// TimeInForce represents how long an order remains active
type TimeInForce string

// Time in force options
const (
	TimeInForceDay           TimeInForce = "DAY"
	TimeInForceGTC           TimeInForce = "GTC"   // Good till cancel
	TimeInForceIOC           TimeInForce = "IOC"   // Immediate or cancel
	TimeInForceFOK           TimeInForce = "FOK"   // Fill or kill
	TimeInForceGTD           TimeInForce = "GTD"   // Good till date
	TimeInForceAtTheOpening  TimeInForce = "OPG"   // At the opening
	TimeInForceAtTheClose    TimeInForce = "CLS"   // At the close
)

// Order represents a trading order
type Order struct {
	// Order identification
	OrderID       string          // Unique system-generated order ID
	ClientOrderID string          // Client-provided order ID
	BrokerID      string          // ID of the broker that submitted the order
	Account       string          // Account for the order
	
	// Order details
	Symbol        string          // Instrument symbol
	Side          Side            // Buy or sell
	OrderType     Type            // Market, limit, etc.
	Quantity      float64         // Original order quantity
	Price         float64         // Limit price, if applicable
	StopPrice     float64         // Stop price, if applicable
	TimeInForce   TimeInForce     // Day, GTC, etc.
	
	// Order state
	Status        OrderStatus     // Current order status
	FilledQty     float64         // Quantity that has been filled
	AvgFillPrice  float64         // Average price of fills
	
	// Timestamps
	CreatedAt     time.Time       // When the order was created
	UpdatedAt     time.Time       // When the order was last updated
	
	// FIX-specific fields
	OrigMessage   *quickfix.Message // Original FIX message
	SessionID     quickfix.SessionID // SessionID for the connection
}

// Execution represents a single execution (fill or partial fill)
type Execution struct {
	ExecID        string          // Unique execution ID
	OrderID       string          // Associated order ID
	Symbol        string          // Instrument symbol
	Side          Side            // Buy or sell
	Quantity      float64         // Executed quantity
	Price         float64         // Execution price
	Timestamp     time.Time       // Time of execution
	BrokerID      string          // ID of the broker
	Account       string          // Account for the execution
}

// NewOrderFromFIX creates a new order from a FIX message
func NewOrderFromFIX(msg *quickfix.Message, brokerID string, sessionID quickfix.SessionID) (*Order, error) {
	// Implementation will extract fields from the FIX message
	// and construct an Order object
	
	// This is a placeholder implementation - the actual code would
	// extract all the relevant fields from the FIX message
	
	order := &Order{
		OrderID:      generateOrderID(),
		BrokerID:     brokerID,
		Status:       StatusNew,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		OrigMessage:  msg,
		SessionID:    sessionID,
	}
	
	return order, nil
}

// generateOrderID creates a unique order ID
func generateOrderID() string {
	// In a real implementation, this would generate a truly unique ID
	// For now, we'll use a timestamp-based approach
	return "ORD-" + time.Now().Format("20060102-150405-000")
}
