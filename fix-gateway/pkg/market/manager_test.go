// See the file LICENSE for licensing terms.

package market

import (
	"testing"
	
	"github.com/rhombus-tech/aristo/fix-gateway/pkg/config"
	"github.com/rhombus-tech/aristo/fix-gateway/pkg/order"
)

// mockOrderHandler implements the OrderHandler interface for testing
type mockOrderHandler struct {
	responses []*OrderResponse
	marketData []*MarketData
}

func (h *mockOrderHandler) HandleOrderResponse(response *OrderResponse) error {
	h.responses = append(h.responses, response)
	return nil
}

func (h *mockOrderHandler) HandleMarketData(data *MarketData) error {
	h.marketData = append(h.marketData, data)
	return nil
}

// mockSession implements the Session interface for testing
type mockSession struct {
	connected      bool
	orders         []*order.Order
	cancelRequests []struct{ orderID, clientOrderID string }
	subscriptions  []string
	status         ConnectionStatus
}

func (s *mockSession) Connect() error {
	s.connected = true
	s.status = StatusConnected
	return nil
}

func (s *mockSession) Disconnect() error {
	s.connected = false
	s.status = StatusDisconnected
	return nil
}

func (s *mockSession) SendOrder(order *order.Order) error {
	s.orders = append(s.orders, order)
	return nil
}

func (s *mockSession) CancelOrder(orderID, clientOrderID string) error {
	s.cancelRequests = append(s.cancelRequests, struct{ orderID, clientOrderID string }{orderID, clientOrderID})
	return nil
}

func (s *mockSession) SubscribeMarketData(symbol string) error {
	s.subscriptions = append(s.subscriptions, symbol)
	return nil
}

func (s *mockSession) UnsubscribeMarketData(symbol string) error {
	// Find and remove the subscription
	for i, sym := range s.subscriptions {
		if sym == symbol {
			s.subscriptions = append(s.subscriptions[:i], s.subscriptions[i+1:]...)
			break
		}
	}
	return nil
}

func (s *mockSession) GetStatus() ConnectionStatus {
	return s.status
}

func TestMarketManager(t *testing.T) {
	// Create a mock order handler
	handler := &mockOrderHandler{
		responses:  []*OrderResponse{},
		marketData: []*MarketData{},
	}
	
	// Create test configuration
	cfg := config.FIXGatewayConfig{
		NASDAQ: config.NasdaqConfig{
			SenderCompID: "ARISTO",
			TargetCompID: "NASDAQ",
			PrimarySession: config.SessionConfig{
				Host: "localhost",
				Port: 9000,
			},
		},
	}
	
	// Create a market manager with the mock handler
	manager, err := NewManager(cfg, handler)
	if err != nil {
		t.Fatalf("Failed to create market manager: %v", err)
	}
	
	// Replace the NASDAQ session with a mock
	nasdaqMock := &mockSession{
		status: StatusDisconnected,
	}
	manager.sessions[VenueNASDAQ] = nasdaqMock
	
	// Test starting the manager
	if err := manager.Start(); err != nil {
		t.Errorf("Failed to start market manager: %v", err)
	}
	
	// Verify the mock session was connected
	if !nasdaqMock.connected {
		t.Errorf("Expected NASDAQ session to be connected")
	}
	
	// Create a test order
	testOrder := &order.Order{
		OrderID:       "test-order-1",
		ClientOrderID: "client-1",
		BrokerID:      "broker-1",
		Symbol:        "AAPL",
		Side:          order.SideBuy,
		OrderType:     order.TypeLimit,
		Quantity:      100,
		Price:         150.50,
	}
	
	// Test routing an order
	if err := manager.RouteOrder(testOrder); err != nil {
		t.Errorf("Failed to route order: %v", err)
	}
	
	// Verify the order was sent to the mock session
	if len(nasdaqMock.orders) != 1 {
		t.Errorf("Expected 1 order to be sent, got %d", len(nasdaqMock.orders))
	}
	
	if len(nasdaqMock.orders) > 0 && nasdaqMock.orders[0].OrderID != testOrder.OrderID {
		t.Errorf("Expected order ID to match, got %s", nasdaqMock.orders[0].OrderID)
	}
	
	// Test subscribing to market data
	if err := manager.SubscribeMarketData("AAPL"); err != nil {
		t.Errorf("Failed to subscribe to market data: %v", err)
	}
	
	// Verify the subscription was made
	if len(nasdaqMock.subscriptions) != 1 {
		t.Errorf("Expected 1 market data subscription, got %d", len(nasdaqMock.subscriptions))
	}
	
	if len(nasdaqMock.subscriptions) > 0 && nasdaqMock.subscriptions[0] != "AAPL" {
		t.Errorf("Expected subscription for AAPL, got %s", nasdaqMock.subscriptions[0])
	}
	
	// Test stopping the manager
	if err := manager.Stop(); err != nil {
		t.Errorf("Failed to stop market manager: %v", err)
	}
	
	// Verify the mock session was disconnected
	if nasdaqMock.connected {
		t.Errorf("Expected NASDAQ session to be disconnected")
	}
}
