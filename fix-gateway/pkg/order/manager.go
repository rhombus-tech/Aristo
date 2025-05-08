// See the file LICENSE for licensing terms.

package order

import (
	"fmt"
	"sync"
	"time"

	"github.com/quickfixgo/quickfix"
	"github.com/rhombus-tech/aristo/fix-gateway/pkg/blockchain"
)

// Manager handles order lifecycle management
type Manager struct {
	// All active orders mapped by order ID
	orders      map[string]*Order
	
	// Client order ID to system order ID map for lookups
	clientOrderMap map[string]string
	
	// Blockchain connector for processing settlements
	blockchainConn *blockchain.Connector
	
	// For thread safety
	mutex       sync.RWMutex
}

// NewManager creates a new order manager
func NewManager(blockchainConn *blockchain.Connector) *Manager {
	return &Manager{
		orders:        make(map[string]*Order),
		clientOrderMap: make(map[string]string),
		blockchainConn: blockchainConn,
	}
}

// ProcessNewOrder handles a new order submission
func (m *Manager) ProcessNewOrder(msg *quickfix.Message, brokerID string, sessionID quickfix.SessionID) (*Order, error) {
	order, err := NewOrderFromFIX(msg, brokerID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to create order from FIX message: %w", err)
	}
	
	// Store the order
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	m.orders[order.OrderID] = order
	m.clientOrderMap[order.ClientOrderID] = order.OrderID
	
	return order, nil
}

// FindOrderByID retrieves an order by system order ID
func (m *Manager) FindOrderByID(orderID string) (*Order, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	order, exists := m.orders[orderID]
	return order, exists
}

// FindOrderByClientOrderID retrieves an order by client order ID
func (m *Manager) FindOrderByClientOrderID(clientOrderID string) (*Order, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	orderID, exists := m.clientOrderMap[clientOrderID]
	if !exists {
		return nil, false
	}
	
	order, exists := m.orders[orderID]
	return order, exists
}

// ProcessExecution handles an execution report for an order
func (m *Manager) ProcessExecution(orderID string, execQty, execPrice float64) (*Execution, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	order, exists := m.orders[orderID]
	if !exists {
		return nil, fmt.Errorf("order not found: %s", orderID)
	}
	
	// Create execution record
	execution := &Execution{
		ExecID:    "EXEC-" + time.Now().Format("20060102-150405-000"),
		OrderID:   orderID,
		Symbol:    order.Symbol,
		Side:      order.Side,
		Quantity:  execQty,
		Price:     execPrice,
		Timestamp: time.Now(),
		BrokerID:  order.BrokerID,
		Account:   order.Account,
	}
	
	// Update order
	order.FilledQty += execQty
	if order.FilledQty >= order.Quantity {
		order.Status = StatusFilled
	} else {
		order.Status = StatusPartial
	}
	
	// Calculate the new average price
	totalValue := (order.AvgFillPrice * (order.FilledQty - execQty)) + (execPrice * execQty)
	order.AvgFillPrice = totalValue / order.FilledQty
	
	order.UpdatedAt = time.Now()
	
	// Process settlement via blockchain if order is fully filled
	if order.Status == StatusFilled {
		go m.settleTrade(order)
	}
	
	return execution, nil
}

// ProcessCancelOrder handles an order cancellation request
func (m *Manager) ProcessCancelOrder(clientOrderID, brokerID string) (*Order, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	// Find the order by client order ID
	orderID, exists := m.clientOrderMap[clientOrderID]
	if !exists {
		return nil, fmt.Errorf("order not found: %s", clientOrderID)
	}
	
	order, exists := m.orders[orderID]
	if !exists {
		return nil, fmt.Errorf("order not found: %s", orderID)
	}
	
	// Verify broker authorization
	if order.BrokerID != brokerID {
		return nil, fmt.Errorf("broker %s not authorized for order %s", brokerID, orderID)
	}
	
	// Check if the order can be canceled
	if order.Status == StatusFilled || order.Status == StatusCanceled || order.Status == StatusRejected {
		return nil, fmt.Errorf("order in %s state cannot be canceled", order.Status)
	}
	
	// Cancel the order
	order.Status = StatusCanceled
	order.UpdatedAt = time.Now()
	
	return order, nil
}

// settleTrade processes the settlement via blockchain
func (m *Manager) settleTrade(order *Order) error {
	// Implementation would call blockchain connector to record the trade
	// This is a placeholder - real implementation would convert order details
	// to blockchain-compatible format and submit transaction
	
	if m.blockchainConn == nil {
		return fmt.Errorf("blockchain connector not available")
	}
	
	// In a real implementation, we would:
	// 1. Create a settlement transaction with order details
	// 2. Submit to blockchain
	// 3. Update order with settlement status
	
	return nil
}
