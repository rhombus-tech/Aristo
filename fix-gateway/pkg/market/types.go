// See the file LICENSE for licensing terms.

package market

import (
	"github.com/quickfixgo/quickfix"
	"github.com/rhombus-tech/aristo/fix-gateway/pkg/order"
)

// Venue represents a market venue (e.g., NASDAQ)
type Venue string

// Market venues
const (
	VenueNASDAQ  Venue = "NASDAQ"
	VenueNYSE    Venue = "NYSE"
)

// ConnectionStatus represents the status of a market connection
type ConnectionStatus string

// Connection statuses
const (
	StatusConnected    ConnectionStatus = "CONNECTED"
	StatusDisconnected ConnectionStatus = "DISCONNECTED"
	StatusReconnecting ConnectionStatus = "RECONNECTING"
	StatusError        ConnectionStatus = "ERROR"
)

// OrderResponse represents the response from the market for an order
type OrderResponse struct {
	// Original order ID
	OrderID string
	
	// Market-assigned order ID
	MarketOrderID string
	
	// Status of the order at the market
	Status order.OrderStatus
	
	// Error message if any
	Error string
	
	// Original FIX message from the market
	Message *quickfix.Message
}

// MarketData represents market data for an instrument
type MarketData struct {
	// Symbol of the instrument
	Symbol string
	
	// Market venue
	Venue Venue
	
	// Best bid price
	BidPrice float64
	
	// Best ask price
	AskPrice float64
	
	// Best bid size
	BidSize float64
	
	// Best ask size
	AskSize float64
	
	// Last trade price
	LastPrice float64
	
	// Last trade size
	LastSize float64
	
	// Timestamp of the market data
	Timestamp int64
}

// Session interface for interacting with a market venue
type Session interface {
	// Connect establishes a connection to the market venue
	Connect() error
	
	// Disconnect closes the connection to the market venue
	Disconnect() error
	
	// SendOrder sends an order to the market venue
	SendOrder(order *order.Order) error
	
	// CancelOrder sends a cancel request for an order
	CancelOrder(orderID, clientOrderID string) error
	
	// SubscribeMarketData subscribes to market data for a symbol
	SubscribeMarketData(symbol string) error
	
	// UnsubscribeMarketData unsubscribes from market data for a symbol
	UnsubscribeMarketData(symbol string) error
	
	// GetStatus returns the current connection status
	GetStatus() ConnectionStatus
}
