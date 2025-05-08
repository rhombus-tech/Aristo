// See the file LICENSE for licensing terms.

package market

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/quickfixgo/quickfix"
	"github.com/rhombus-tech/aristo/fix-gateway/pkg/config"
	"github.com/rhombus-tech/aristo/fix-gateway/pkg/order"
)

// NasdaqSession implements the Session interface for NASDAQ connectivity
type NasdaqSession struct {
	// Configuration for the session
	config config.NasdaqConfig
	
	// QuickFIX initiator for FIX connectivity
	initiator *quickfix.Initiator
	
	// QuickFIX settings
	settings *quickfix.Settings
	
	// Current connection status
	status ConnectionStatus
	
	// Market data subscriptions
	subscriptions map[string]bool
	
	// Order callback handler
	orderHandler OrderHandler
	
	// Session ID for this connection
	sessionID quickfix.SessionID
	
	// For thread safety
	mutex sync.RWMutex
}

// NewNasdaqSession creates a new NASDAQ session
func NewNasdaqSession(cfg config.NasdaqConfig) *NasdaqSession {
	return &NasdaqSession{
		config:        cfg,
		status:        StatusDisconnected,
		subscriptions: make(map[string]bool),
	}
}

// SetOrderHandler sets the handler for order responses
func (s *NasdaqSession) SetOrderHandler(handler OrderHandler) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	s.orderHandler = handler
}

// Connect establishes a connection to NASDAQ
func (s *NasdaqSession) Connect() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	if s.status == StatusConnected {
		return nil // Already connected
	}
	
	// Initialize QuickFIX settings
	var err error
	s.settings, err = s.createQuickFixSettings()
	if err != nil {
		return fmt.Errorf("failed to create QuickFIX settings: %w", err)
	}
	
	// Create application callbacks
	application := &nasdaqApplication{
		session: s,
	}
	
	// Create a simple log factory
	logFactory := &simpleLogFactory{}
	
	// Create and start the initiator
	s.initiator, err = quickfix.NewInitiator(
		application, 
		quickfix.NewMemoryStoreFactory(), 
		s.settings, 
		logFactory,
	)
	if err != nil {
		return fmt.Errorf("failed to create QuickFIX initiator: %w", err)
	}
	
	// Start the initiator
	err = s.initiator.Start()
	if err != nil {
		return fmt.Errorf("failed to start QuickFIX initiator: %w", err)
	}
	
	// Update status
	s.status = StatusConnected
	
	return nil
}

// Disconnect closes the connection to NASDAQ
func (s *NasdaqSession) Disconnect() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	if s.status == StatusDisconnected {
		return nil // Already disconnected
	}
	
	if s.initiator != nil {
		// Stop the initiator
		s.initiator.Stop() // Stop() doesn't return a value
	}
	
	// Update status
	s.status = StatusDisconnected
	
	return nil
}

// SendOrder sends an order to NASDAQ
func (s *NasdaqSession) SendOrder(order *order.Order) error {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	
	if s.status != StatusConnected {
		return fmt.Errorf("not connected to NASDAQ")
	}
	
	// Create a new order message
	msg := s.createNewOrderSingleMessage(order)
	
	// Send the message via QuickFIX
	return quickfix.Send(msg)
}

// CancelOrder sends a cancel request for an order to NASDAQ
func (s *NasdaqSession) CancelOrder(orderID, clientOrderID string) error {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	
	if s.status != StatusConnected {
		return fmt.Errorf("not connected to NASDAQ")
	}
	
	// Create a cancel request message
	msg := s.createOrderCancelRequestMessage(orderID, clientOrderID)
	
	// Send the message via QuickFIX
	return quickfix.Send(msg)
}

// SubscribeMarketData subscribes to market data for a symbol
func (s *NasdaqSession) SubscribeMarketData(symbol string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	if s.status != StatusConnected {
		return fmt.Errorf("not connected to NASDAQ")
	}
	
	// Check if already subscribed
	if s.subscriptions[symbol] {
		return nil // Already subscribed
	}
	
	// Create a market data request message
	msg := s.createMarketDataRequestMessage(symbol)
	
	// Send the message via QuickFIX
	err := quickfix.Send(msg)
	if err != nil {
		return fmt.Errorf("failed to send market data request: %w", err)
	}
	
	// Add to subscriptions
	s.subscriptions[symbol] = true
	
	return nil
}

// UnsubscribeMarketData unsubscribes from market data for a symbol
func (s *NasdaqSession) UnsubscribeMarketData(symbol string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	if s.status != StatusConnected {
		return fmt.Errorf("not connected to NASDAQ")
	}
	
	// Check if subscribed
	if !s.subscriptions[symbol] {
		return nil // Not subscribed
	}
	
	// Create a market data cancel message
	msg := s.createMarketDataCancelMessage(symbol)
	
	// Send the message via QuickFIX
	err := quickfix.Send(msg)
	if err != nil {
		return fmt.Errorf("failed to send market data cancel: %w", err)
	}
	
	// Remove from subscriptions
	delete(s.subscriptions, symbol)
	
	return nil
}

// GetStatus returns the current connection status
func (s *NasdaqSession) GetStatus() ConnectionStatus {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	
	return s.status
}

// createQuickFixSettings creates the QuickFIX settings for the session
func (s *NasdaqSession) createQuickFixSettings() (*quickfix.Settings, error) {
	settings := quickfix.NewSettings()
	
	// Session configuration
	sessionSettings := map[string]string{
		"BeginString":       "FIX.4.2", // NASDAQ uses FIX 4.2
		"SenderCompID":      s.config.SenderCompID,
		"TargetCompID":      s.config.TargetCompID,
		"ConnectionType":    "initiator",
		"StartTime":         "00:00:00",
		"EndTime":           "23:59:59",
		"HeartBtInt":        "30", // 30 seconds
		"ReconnectInterval": "30", // 30 seconds
		"SocketConnectPort": fmt.Sprintf("%d", s.config.PrimarySession.Port),
		"SocketConnectHost": s.config.PrimarySession.Host,
		"SocketUseSSL":      fmt.Sprintf("%t", s.config.PrimarySession.UseSSL),
	}
	
	// Create a default session
	defaults := settings.GlobalSettings()
	for k, v := range sessionSettings {
		defaults.Set(k, v)
	}
	
	return settings, nil
}

// constructOrderSubmitMessage creates a FIX New Order Single message
func (s *NasdaqSession) constructOrderSubmitMessage(o *order.Order) (*quickfix.Message, error) {
	// Since we have compilation issues with the tag and field packages,
	// we'll use the simpler createNewOrderSingleMessage method that already works
	return s.createNewOrderSingleMessage(o), nil
}

// createNewOrderSingleMessage creates a FIX New Order Single message
func (s *NasdaqSession) createNewOrderSingleMessage(orderData *order.Order) *quickfix.Message {
	msg := quickfix.NewMessage()

	// Message type: New Order Single (35=D)
	msg.Header.SetField(35, quickfix.FIXString("D"))

	// Client Order ID (required)
	msg.Body.SetField(11, quickfix.FIXString(orderData.ClientOrderID))   // ClOrdID
	msg.Body.SetField(21, quickfix.FIXString("1"))                   // HandlInst - Automated execution
	msg.Body.SetField(55, quickfix.FIXString(orderData.Symbol))          // Symbol

	// Side (54) - 1=Buy, 2=Sell
	var side string
	// Compare the order's Side field with the package-level constant
	if orderData.Side == order.SideBuy {
		side = "1"
	} else {
		side = "2"
	}
	msg.Body.SetField(54, quickfix.FIXString(side))

	// Order type (40) - 1=Market, 2=Limit, 3=Stop, 4=StopLimit
	var orderType string
	switch orderData.OrderType {
	case order.TypeMarket:
		orderType = "1" // Market
	case order.TypeLimit:
		orderType = "2" // Limit
		// For limit orders, price is required
		msg.Body.SetField(44, quickfix.FIXString(fmt.Sprintf("%f", orderData.Price)))
	case order.TypeStop:
		orderType = "3" // Stop
		// For stop orders, stop price is required
		msg.Body.SetField(99, quickfix.FIXString(fmt.Sprintf("%f", orderData.StopPrice)))
	case order.TypeStopLimit:
		orderType = "4" // Stop-limit
		// For stop-limit orders, both prices are required
		msg.Body.SetField(44, quickfix.FIXString(fmt.Sprintf("%f", orderData.Price)))
		msg.Body.SetField(99, quickfix.FIXString(fmt.Sprintf("%f", orderData.StopPrice)))
	default:
		orderType = "1" // Default to Market
	}
	msg.Body.SetField(40, quickfix.FIXString(orderType))

	// TimeInForce (59) - 0=Day, 1=GTC, 3=IOC, 4=FOK, etc.
	var tif string
	switch orderData.TimeInForce {
	case order.TimeInForceDay:
		tif = "0" // Day
	case order.TimeInForceGTC:
		tif = "1" // GTC
	case order.TimeInForceIOC:
		tif = "3" // IOC
	case order.TimeInForceFOK:
		tif = "4" // FOK
	default:
		tif = "0" // Default to Day
	}
	msg.Body.SetField(59, quickfix.FIXString(tif))

	// OrderQty (38) - Order quantity
	msg.Body.SetField(38, quickfix.FIXString(fmt.Sprintf("%f", orderData.Quantity)))

	return msg
}

// createMarketDataRequestMessage creates a FIX Market Data Request message
func (s *NasdaqSession) createMarketDataRequestMessage(symbol string) *quickfix.Message {
	msg := quickfix.NewMessage()

	// Message type: Market Data Request (35=V)
	msg.Header.SetField(35, quickfix.FIXString("V"))

	// Required fields
	// MDReqID (262) - Unique identifier for the request
	msg.Body.SetField(262, quickfix.FIXString("MD-"+symbol+"-"+time.Now().Format("20060102-150405")))
	
	// SubscriptionRequestType (263) - 1=Subscribe, 2=Unsubscribe, 0=Snapshot
	msg.Body.SetField(263, quickfix.FIXString("1")) // Subscribe
	
	// MarketDepth (264) - 0=Full book, 1=Top of book
	msg.Body.SetField(264, quickfix.FIXString("1")) // Top of book
	
	// NoRelatedSym (146) - Number of symbols
	msg.Body.SetField(146, quickfix.FIXString("1")) // One symbol
	
	// Symbol group
	msg.Body.SetField(55, quickfix.FIXString(symbol)) // Symbol
	
	return msg
}

// createMarketDataCancelMessage creates a FIX Market Data Request message to cancel a subscription
func (s *NasdaqSession) createMarketDataCancelMessage(symbol string) *quickfix.Message {
	msg := quickfix.NewMessage()
	
	// Message type: Market Data Request (35=V)
	msg.Header.SetField(35, quickfix.FIXString("V"))
	
	// Required fields
	// MDReqID (262) - Unique identifier for the request
	msg.Body.SetField(262, quickfix.FIXString("MD-"+symbol+"-"+time.Now().Format("20060102-150405")))
	
	// SubscriptionRequestType (263) - 1=Subscribe, 2=Unsubscribe, 0=Snapshot
	msg.Body.SetField(263, quickfix.FIXString("2")) // Unsubscribe
	
	// NoRelatedSym (146) - Number of symbols
	msg.Body.SetField(146, quickfix.FIXString("1")) // One symbol
	
	// Symbol group
	msg.Body.SetField(55, quickfix.FIXString(symbol)) // Symbol
	
	return msg
}

// createOrderCancelRequestMessage creates a FIX Order Cancel Request message
func (s *NasdaqSession) createOrderCancelRequestMessage(orderID, clientOrderID string) *quickfix.Message {
	msg := quickfix.NewMessage()
	
	// Message type: Order Cancel Request (35=F)
	msg.Header.SetField(35, quickfix.FIXString("F"))
	
	// Required fields
	msg.Body.SetField(41, quickfix.FIXString(clientOrderID))         // OrigClOrdID
	msg.Body.SetField(11, quickfix.FIXString("CXL-"+clientOrderID))  // ClOrdID for the cancel
	
	// TransactTime (60) - Time of request
	msg.Body.SetField(60, quickfix.FIXString(time.Now().UTC().Format("20060102-15:04:05")))
	
	return msg
}

// nasdaqApplication implements the QuickFIX Application interface
// simpleLogFactory is a minimal implementation of quickfix.LogFactory
type simpleLogFactory struct{}

// Create returns a logger for QuickFIX
func (f *simpleLogFactory) Create() (quickfix.Log, error) {
	return &simpleLog{}, nil
}

// CreateSessionLog returns a session-specific logger for QuickFIX
func (f *simpleLogFactory) CreateSessionLog(sessionID quickfix.SessionID) (quickfix.Log, error) {
	return &simpleLog{}, nil
}

// simpleLog is a minimal implementation of quickfix.Log
type simpleLog struct{}

func (l *simpleLog) OnIncoming(msg []byte)                                      { log.Printf("FIX Incoming: %s", string(msg)) }
func (l *simpleLog) OnOutgoing(msg []byte)                                      { log.Printf("FIX Outgoing: %s", string(msg)) }
func (l *simpleLog) OnEvent(msg string)                                         { log.Printf("FIX Event: %s", msg) }
func (l *simpleLog) OnEventf(format string, a ...interface{})                    { log.Printf("FIX Event: "+format, a...) }
func (l *simpleLog) OnReject(msg string)                                        { log.Printf("FIX Reject: %s", msg) }
func (l *simpleLog) OnRejectf(format string, a ...interface{})                   { log.Printf("FIX Reject: "+format, a...) }
func (l *simpleLog) OnConfigError(err error)                                   { log.Printf("FIX Config Error: %v", err) }
func (l *simpleLog) OnError(err string)                                         { log.Printf("FIX Error: %s", err) }
func (l *simpleLog) OnErrorf(format string, a ...interface{})                    { log.Printf("FIX Error: "+format, a...) }

type nasdaqApplication struct {
	session *NasdaqSession
}

// OnCreate is called when the QuickFIX session is created
func (app *nasdaqApplication) OnCreate(sessionID quickfix.SessionID) {
	app.session.mutex.Lock()
	defer app.session.mutex.Unlock()
	
	app.session.sessionID = sessionID
}

// OnLogon is called when the session logs on
func (app *nasdaqApplication) OnLogon(sessionID quickfix.SessionID) {
	app.session.mutex.Lock()
	defer app.session.mutex.Unlock()
	
	app.session.status = StatusConnected
}

// OnLogout is called when the session logs out
func (app *nasdaqApplication) OnLogout(sessionID quickfix.SessionID) {
	app.session.mutex.Lock()
	defer app.session.mutex.Unlock()
	
	app.session.status = StatusDisconnected
}

// ToAdmin is called before admin messages are sent
func (app *nasdaqApplication) ToAdmin(msg *quickfix.Message, sessionID quickfix.SessionID) {
	// Implement if needed
}

// ToApp is called before application messages are sent
func (app *nasdaqApplication) ToApp(msg *quickfix.Message, sessionID quickfix.SessionID) error {
	// Implement if needed
	return nil
}

// FromAdmin is called when admin messages are received
func (app *nasdaqApplication) FromAdmin(msg *quickfix.Message, sessionID quickfix.SessionID) quickfix.MessageRejectError {
	// Implement if needed
	return nil
}

// FromApp is called when application messages are received
func (app *nasdaqApplication) FromApp(msg *quickfix.Message, sessionID quickfix.SessionID) quickfix.MessageRejectError {
	app.session.mutex.Lock()
	defer app.session.mutex.Unlock()
	
	// Get the message type
	var msgType quickfix.FIXString
	if err := msg.Header.GetField(35, &msgType); err != nil {
		return quickfix.NewMessageRejectError("Missing message type", 0, nil)
	}
	
	// Process based on message type
	switch string(msgType) {
	case "8": // Execution Report
		app.processExecutionReport(msg)
	case "9": // Order Cancel Reject
		app.processOrderCancelReject(msg)
	case "W": // Market Data Snapshot Full Refresh
		app.processMarketDataSnapshot(msg)
	case "X": // Market Data Incremental Refresh
		app.processMarketDataIncremental(msg)
	}
	
	return nil
}

// processExecutionReport processes execution reports from NASDAQ
func (app *nasdaqApplication) processExecutionReport(msg *quickfix.Message) {
	// Extract information from the execution report
	var clientOrderID quickfix.FIXString
	var orderID quickfix.FIXString
	var execType quickfix.FIXString
	var ordStatus quickfix.FIXString
	
	// Extract fields
	msg.Body.GetField(11, &clientOrderID) // ClOrdID
	msg.Body.GetField(37, &orderID)       // OrderID
	msg.Body.GetField(150, &execType)     // ExecType
	msg.Body.GetField(39, &ordStatus)     // OrdStatus
	
	// Create an order response
	response := &OrderResponse{
		OrderID:       string(clientOrderID),
		MarketOrderID: string(orderID),
		Message:       msg,
	}
	
	// Map the order status
	switch string(ordStatus) {
	case "0": // New
		response.Status = order.StatusNew
	case "1": // Partially filled
		response.Status = order.StatusPartial
	case "2": // Filled
		response.Status = order.StatusFilled
	case "4": // Canceled
		response.Status = order.StatusCanceled
	case "8": // Rejected
		response.Status = order.StatusRejected
		// Extract reason text
		var text quickfix.FIXString
		if err := msg.Body.GetField(58, &text); err == nil {
			response.Error = string(text)
		}
	}
	
	// Call the order handler if available
	if app.session.orderHandler != nil {
		app.session.orderHandler.HandleOrderResponse(response)
	}
}

// processOrderCancelReject processes order cancel reject messages
func (app *nasdaqApplication) processOrderCancelReject(msg *quickfix.Message) {
	// Implementation would extract information and create an OrderResponse
	// Similar to processExecutionReport
}

// processMarketDataSnapshot processes market data snapshots
func (app *nasdaqApplication) processMarketDataSnapshot(msg *quickfix.Message) {
	// Implementation would extract market data and call the handler
	// This would involve parsing the message structure, extracting prices, etc.
}

// processMarketDataIncremental processes incremental market data updates
func (app *nasdaqApplication) processMarketDataIncremental(msg *quickfix.Message) {
	// Implementation would extract market data updates and call the handler
	// This would involve parsing the message structure, extracting prices, etc.
}
