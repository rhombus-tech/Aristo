// See the file LICENSE for licensing terms.

package order

import (
	"testing"
	"time"
)

func TestOrderManager(t *testing.T) {
	// Create a new order manager
	manager := NewManager(nil)
	
	// Test finding a non-existent order
	_, exists := manager.FindOrderByID("non-existent")
	if exists {
		t.Errorf("Expected non-existent order to not be found")
	}
	
	// Create a test order
	order := &Order{
		OrderID:       "test-order-1",
		ClientOrderID: "client-1",
		BrokerID:      "broker-1",
		Account:       "account-1",
		Symbol:        "AAPL",
		Side:          SideBuy,
		OrderType:     TypeLimit,
		Quantity:      100,
		Price:         150.50,
		Status:        StatusNew,
		FilledQty:     0,
		AvgFillPrice:  0,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	
	// Manually add the order to simulate processing
	manager.orders[order.OrderID] = order
	manager.clientOrderMap[order.ClientOrderID] = order.OrderID
	
	// Test finding the order by ID
	foundOrder, exists := manager.FindOrderByID(order.OrderID)
	if !exists {
		t.Errorf("Expected order to be found by ID")
	}
	if foundOrder.OrderID != order.OrderID {
		t.Errorf("Expected found order ID to match")
	}
	
	// Test finding the order by client order ID
	foundOrder, exists = manager.FindOrderByClientOrderID(order.ClientOrderID)
	if !exists {
		t.Errorf("Expected order to be found by client order ID")
	}
	if foundOrder.OrderID != order.OrderID {
		t.Errorf("Expected found order ID to match")
	}
	
	// Test processing an execution
	execution, err := manager.ProcessExecution(order.OrderID, 50, 151.0)
	if err != nil {
		t.Errorf("Failed to process execution: %v", err)
	}
	
	// Verify execution details
	if execution.OrderID != order.OrderID {
		t.Errorf("Expected execution order ID to match")
	}
	if execution.Quantity != 50 {
		t.Errorf("Expected execution quantity to be 50, got %f", execution.Quantity)
	}
	if execution.Price != 151.0 {
		t.Errorf("Expected execution price to be 151.0, got %f", execution.Price)
	}
	
	// Verify order was updated correctly
	if order.Status != StatusPartial {
		t.Errorf("Expected order status to be PARTIALLY_FILLED, got %s", order.Status)
	}
	if order.FilledQty != 50 {
		t.Errorf("Expected filled quantity to be 50, got %f", order.FilledQty)
	}
	if order.AvgFillPrice != 151.0 {
		t.Errorf("Expected average fill price to be 151.0, got %f", order.AvgFillPrice)
	}
	
	// Process another execution to fully fill the order
	execution, err = manager.ProcessExecution(order.OrderID, 50, 152.0)
	if err != nil {
		t.Errorf("Failed to process execution: %v", err)
	}
	
	// Verify order is now fully filled
	if order.Status != StatusFilled {
		t.Errorf("Expected order status to be FILLED, got %s", order.Status)
	}
	if order.FilledQty != 100 {
		t.Errorf("Expected filled quantity to be 100, got %f", order.FilledQty)
	}
	
	// Verify average price calculation: (151.0 * 50 + 152.0 * 50) / 100 = 151.5
	if order.AvgFillPrice != 151.5 {
		t.Errorf("Expected average fill price to be 151.5, got %f", order.AvgFillPrice)
	}
	
	// Test processing a cancel order
	canceledOrder, err := manager.ProcessCancelOrder(order.ClientOrderID, order.BrokerID)
	if err == nil {
		t.Errorf("Expected error when canceling a filled order")
	}
	
	// Create another order for cancel testing
	newOrder := &Order{
		OrderID:       "test-order-2",
		ClientOrderID: "client-2",
		BrokerID:      "broker-1",
		Account:       "account-1",
		Symbol:        "MSFT",
		Side:          SideSell,
		OrderType:     TypeLimit,
		Quantity:      100,
		Price:         250.50,
		Status:        StatusNew,
		FilledQty:     0,
		AvgFillPrice:  0,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	
	// Manually add the order
	manager.orders[newOrder.OrderID] = newOrder
	manager.clientOrderMap[newOrder.ClientOrderID] = newOrder.OrderID
	
	// Test successful cancel
	canceledOrder, err = manager.ProcessCancelOrder(newOrder.ClientOrderID, newOrder.BrokerID)
	if err != nil {
		t.Errorf("Failed to cancel order: %v", err)
	}
	
	// Verify canceled order status
	if canceledOrder.Status != StatusCanceled {
		t.Errorf("Expected order status to be CANCELED, got %s", canceledOrder.Status)
	}
}
