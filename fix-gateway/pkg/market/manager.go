// See the file LICENSE for licensing terms.

package market

import (
	"fmt"
	"sync"

	"github.com/quickfixgo/quickfix"
	"github.com/rhombus-tech/aristo/fix-gateway/pkg/config"
	"github.com/rhombus-tech/aristo/fix-gateway/pkg/order"
)

// OrderHandler interface for components that process market responses
type OrderHandler interface {
	// HandleOrderResponse processes a response from the market
	HandleOrderResponse(response *OrderResponse) error
	
	// HandleMarketData processes market data updates
	HandleMarketData(data *MarketData) error
}

// Manager manages connections to different market venues
type Manager struct {
	// Sessions for different market venues
	sessions map[Venue]Session
	
	// Order handler for processing market responses
	orderHandler OrderHandler
	
	// Symbol to market venue map for routing
	symbolRouting map[string]Venue
	
	// For thread safety
	mutex sync.RWMutex
}

// NewManager creates a new market manager
func NewManager(cfg config.FIXGatewayConfig, orderHandler OrderHandler) (*Manager, error) {
	manager := &Manager{
		sessions:      make(map[Venue]Session),
		orderHandler:  orderHandler,
		symbolRouting: make(map[string]Venue),
	}
	
	// Initialize market sessions based on configuration
	// For now, we'll just set up NASDAQ
	if err := manager.setupNasdaqSession(cfg.NASDAQ); err != nil {
		return nil, fmt.Errorf("failed to set up NASDAQ session: %w", err)
	}
	
	return manager, nil
}

// setupNasdaqSession initializes the NASDAQ market session
func (m *Manager) setupNasdaqSession(cfg config.NasdaqConfig) error {
	// Create a new NASDAQ session (implementation provided in nasdaq.go)
	session := NewNasdaqSession(cfg)
	
	m.sessions[VenueNASDAQ] = session
	
	// Set up default symbol routing to NASDAQ
	// In a real implementation, this would be more sophisticated
	// and possibly loaded from a configuration or database
	// For now, we'll just route all symbols to NASDAQ
	
	return nil
}

// Start initializes and connects all market sessions
func (m *Manager) Start() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	for venue, session := range m.sessions {
		if err := session.Connect(); err != nil {
			return fmt.Errorf("failed to connect to %s: %w", venue, err)
		}
	}
	
	return nil
}

// Stop disconnects all market sessions
func (m *Manager) Stop() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	for venue, session := range m.sessions {
		if err := session.Disconnect(); err != nil {
			return fmt.Errorf("failed to disconnect from %s: %w", venue, err)
		}
	}
	
	return nil
}

// RouteOrder routes an order to the appropriate market venue
func (m *Manager) RouteOrder(order *order.Order) error {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	// Determine which venue to route to
	venue, exists := m.symbolRouting[order.Symbol]
	if !exists {
		// Default to NASDAQ if no specific routing is defined
		venue = VenueNASDAQ
	}
	
	// Get the session for the venue
	session, exists := m.sessions[venue]
	if !exists {
		return fmt.Errorf("no session available for venue %s", venue)
	}
	
	// Send the order to the market
	if err := session.SendOrder(order); err != nil {
		return fmt.Errorf("failed to send order to %s: %w", venue, err)
	}
	
	return nil
}

// CancelOrder sends a cancellation request to the market
func (m *Manager) CancelOrder(orderID, clientOrderID string, venue Venue) error {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	// Get the session for the venue
	session, exists := m.sessions[venue]
	if !exists {
		return fmt.Errorf("no session available for venue %s", venue)
	}
	
	// Send the cancel request
	if err := session.CancelOrder(orderID, clientOrderID); err != nil {
		return fmt.Errorf("failed to cancel order %s at %s: %w", orderID, venue, err)
	}
	
	return nil
}

// SubscribeMarketData subscribes to market data for a symbol
func (m *Manager) SubscribeMarketData(symbol string) error {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	// Determine which venue to subscribe to
	venue, exists := m.symbolRouting[symbol]
	if !exists {
		// Default to NASDAQ if no specific routing is defined
		venue = VenueNASDAQ
	}
	
	// Get the session for the venue
	session, exists := m.sessions[venue]
	if !exists {
		return fmt.Errorf("no session available for venue %s", venue)
	}
	
	// Subscribe to market data
	if err := session.SubscribeMarketData(symbol); err != nil {
		return fmt.Errorf("failed to subscribe to market data for %s at %s: %w", symbol, venue, err)
	}
	
	return nil
}

// ProcessFromApp handles messages from FIX application
func (m *Manager) ProcessFromApp(msg *quickfix.Message, sessionID quickfix.SessionID) error {
	// Implementation will handle messages from the market
	// and route them to the appropriate handlers
	
	// For example, for execution reports:
	// 1. Extract relevant information from the FIX message
	// 2. Create an OrderResponse
	// 3. Call the order handler
	
	return nil
}

// ProcessToApp processes outgoing messages to the market
func (m *Manager) ProcessToApp(msg *quickfix.Message, sessionID quickfix.SessionID) error {
	// Implementation will handle messages being sent to the market
	// This could include logging, validation, etc.
	
	return nil
}
