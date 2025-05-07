// Copyright (C) 2025. All rights reserved.
// See the file LICENSE for licensing terms.

package brokerdealer

import (
	"fmt"
	"log"
	"math/big"
	"strings"
	"sync"

	"github.com/quickfixgo/quickfix"
	"github.com/rhombus-tech/aristo/fix-gateway/pkg/blockchain"
	"github.com/rhombus-tech/aristo/fix-gateway/pkg/config"
)

// Custom logger types for broker-dealer FIX sessions

// brokerLogger logs FIX messages with broker identification
type brokerLogger struct {
	brokerID string
}

func (l brokerLogger) OnIncoming(msg []byte) {
	log.Printf("FIX <<< (Broker %s): %s", l.brokerID, string(msg))
}

func (l brokerLogger) OnOutgoing(msg []byte) {
	log.Printf("FIX >>> (Broker %s): %s", l.brokerID, string(msg))
}

func (l brokerLogger) OnEvent(msg string) {
	// Only log significant events
	if strings.Contains(msg, "error") || strings.Contains(msg, "failed") {
		log.Printf("FIX Event (Broker %s): %s", l.brokerID, msg)
	}
}

func (l brokerLogger) OnEventf(format string, args ...interface{}) {
	// Format and pass to OnEvent
	l.OnEvent(fmt.Sprintf(format, args...))
}

// simpleLogFactory is a basic implementation of quickfix.LogFactory
type simpleLogFactory struct {
	brokerID string
}

func newBrokerLogFactory(brokerID string) quickfix.LogFactory {
	return quickfix.NewNullLogFactory() // Use built-in null logger as fallback
}

// Manager handles connections to multiple broker-dealers
type Manager struct {
	config            ManagerConfig
	brokers           map[string]*Broker
	marketSession     MarketSession
	blockchainConn    *blockchain.Connector
	application       *Application
	initiators        map[string]*quickfix.Initiator
	mu                sync.RWMutex
	dropCopyEnabled   bool
}

// MarketSession is an interface for the market (e.g., NASDAQ) session
type MarketSession interface {
	// SendAndVerify sends a message to the market with attestation verification
	SendAndVerify(msg *quickfix.Message) error
}

// NewManager creates a new broker-dealer manager
func NewManager(
	cfg config.BrokerDealerConfig,
	marketSession MarketSession,
	blockchainConn *blockchain.Connector,
) (*Manager, error) {
	// Convert config format
	managerConfig := convertConfig(cfg)
	
	manager := &Manager{
		config:          managerConfig,
		brokers:         make(map[string]*Broker),
		marketSession:   marketSession,
		blockchainConn:  blockchainConn,
		initiators:      make(map[string]*quickfix.Initiator),
		dropCopyEnabled: managerConfig.DropCopyEnabled,
	}
	
	// Create application to handle incoming FIX messages
	manager.application = NewApplication(manager)
	
	return manager, nil
}

// Start initializes and starts connections to all configured broker-dealers
func (m *Manager) Start() error {
	for _, brokerCfg := range m.config.Brokers {
		if err := m.startBroker(brokerCfg); err != nil {
			log.Printf("Failed to start broker %s: %v", brokerCfg.ID, err)
			// Continue with other brokers even if one fails
		}
	}
	return nil
}

// Stop gracefully terminates all broker connections
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	for id, initiator := range m.initiators {
		log.Printf("Stopping broker connection: %s", id)
		initiator.Stop()
	}
}

// startBroker initializes and starts a connection to a single broker
func (m *Manager) startBroker(cfg BrokerConfig) error {
	// Create QuickFIX settings
	settings := createQuickFixSettings(cfg)
	
	// Use our predefined logger
	logFactory := newBrokerLogFactory(cfg.ID)
	
	// Initialize the FIX initiator with our custom logging
	initiator, err := quickfix.NewInitiator(
		m.application,
		quickfix.NewMemoryStoreFactory(),
		settings,
		logFactory,
	)
	if err != nil {
		return fmt.Errorf("failed to create initiator for broker %s: %w", cfg.ID, err)
	}
	
	// Create broker instance
	broker := &Broker{
		config:           cfg,
		manager:          m,
		clientAccounts:   make(map[string]bool),
		blockchainConn:   m.blockchainConn,
		sessionID:        buildSessionID(cfg),
	}
	
	// Add client accounts
	for _, account := range cfg.ClientAccounts {
		broker.clientAccounts[account] = true
	}
	
	// Store broker and initiator
	m.mu.Lock()
	m.brokers[cfg.ID] = broker
	m.initiators[cfg.ID] = initiator
	m.mu.Unlock()
	
	// Start the initiator
	if err := initiator.Start(); err != nil {
		return fmt.Errorf("failed to start initiator for broker %s: %w", cfg.ID, err)
	}
	
	log.Printf("Started connection to broker: %s", cfg.ID)
	return nil
}

// GetBroker returns a broker by ID
func (m *Manager) GetBroker(id string) (*Broker, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	broker, exists := m.brokers[id]
	return broker, exists
}

// ValidateAccount checks if an account is valid for any registered broker
func (m *Manager) ValidateAccount(account string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	for id, broker := range m.brokers {
		if broker.ValidateAccount(account) {
			return id, true
		}
	}
	
	return "", false
}

// ForwardToMarket forwards a message to the market session
func (m *Manager) ForwardToMarket(msg *quickfix.Message) error {
	// Add any necessary processing or validation here
	return m.marketSession.SendAndVerify(msg)
}

// SendExecutionReport sends an execution report to the appropriate broker
func (m *Manager) SendExecutionReport(
	brokerID string,
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
	broker, exists := m.GetBroker(brokerID)
	if !exists {
		return fmt.Errorf("unknown broker: %s", brokerID)
	}
	
	// Record on blockchain for filled or partially filled orders
	if (ordStatus == '2' || ordStatus == '1') && !isRejected {
		priceBig := new(big.Int)
		priceBig.SetInt64(int64(lastPx * 1e8)) // Scale by 10^8 for precision
		
		qtyBig := new(big.Int)
		qtyBig.SetInt64(int64(lastQty * 1e8)) // Scale by 10^8 for precision
		
		// Calculate percent fill
		percentFill := 100.0
		if cumQty > 0 {
			percentFill = (lastQty / cumQty) * 100
			if percentFill > 100 {
				percentFill = 100
			}
		}
		
		// Empty attestation data for now
		attestationData := []byte{}
		signature := []byte{}
		
		// Submit to blockchain
		txID, err := m.blockchainConn.SubmitOrderSettlement(
			orderID,
			symbol,
			string(side),
			priceBig,
			qtyBig,
			attestationData,
			signature,
			percentFill,
			isRejected,
			rejectReason,
		)
		
		if err != nil {
			log.Printf("Error recording settlement to blockchain: %v", err)
			// Continue to send execution report even if blockchain fails
		} else {
			log.Printf("Recorded settlement on blockchain: txID=%s", txID)
		}
	}
	
	// Send execution report to the broker
	return broker.SendExecutionReport(
		orderID,
		execID,
		account,
		symbol,
		side,
		ordStatus,
		execType,
		lastQty,
		lastPx,
		cumQty,
		avgPx,
		isRejected,
		rejectReason,
	)
}

// Helper Functions

// convertConfig converts from the general config format to the broker manager format
func convertConfig(cfg config.BrokerDealerConfig) ManagerConfig {
	managerConfig := ManagerConfig{
		Enabled:         cfg.Enabled,
		RateLimit:       cfg.RateLimitPerBroker,
		DropCopyEnabled: cfg.DropCopyEnabled,
		Brokers:         make([]BrokerConfig, 0, len(cfg.BrokerDealers)),
	}
	
	// Convert each broker config
	for _, broker := range cfg.BrokerDealers {
		managerConfig.Brokers = append(managerConfig.Brokers, BrokerConfig{
			ID:             broker.ID,
			Name:           broker.Name,
			SenderCompID:   broker.SenderCompID,
			TargetCompID:   broker.TargetCompID,
			Session:        broker.SessionConfig,
			ClientAccounts: broker.AllowedAccounts,
			CommissionRate: broker.CommissionRate,
		})
	}
	
	return managerConfig
}

// createQuickFixSettings creates QuickFIX session settings for a broker
func createQuickFixSettings(cfg BrokerConfig) *quickfix.Settings {
	settings := quickfix.NewSettings()
	
	// Default settings
	globalSettings := settings.GlobalSettings()
	globalSettings.Set("SocketConnectHost", cfg.Session.Host)
	globalSettings.Set("SocketConnectPort", fmt.Sprintf("%d", cfg.Session.Port))
	globalSettings.Set("HeartBtInt", "30") // 30 second heartbeat
	globalSettings.Set("ReconnectInterval", "60") // 60 second reconnect
	globalSettings.Set("FileLogPath", "log")
	globalSettings.Set("StartTime", "00:00:00")
	globalSettings.Set("EndTime", "00:00:00") // 24/7 operation
	
	// Session settings
	sessionID := buildSessionID(cfg)
	sessionSettings := settings.SessionSettings()
	
	// Create dictionary for the session
	sessionDict := quickfix.NewSettings().GlobalSettings()
	sessionDict.Set("BeginString", "FIX.4.2") // Default to FIX 4.2
	sessionDict.Set("SenderCompID", cfg.TargetCompID) // Our ID when sending
	sessionDict.Set("TargetCompID", cfg.SenderCompID) // Broker's ID
	
	if cfg.Session.UseSSL {
		sessionDict.Set("SocketUseSSL", "Y")
		sessionDict.Set("SocketPrivateKeyFile", cfg.Session.SSLKey)
		sessionDict.Set("SocketCertificateFile", cfg.Session.SSLCert)
	}
	
	// Store the session dictionary
	sessionSettings[sessionID] = sessionDict
	
	return settings
}

// buildSessionID creates a QuickFIX SessionID for a broker
func buildSessionID(cfg BrokerConfig) quickfix.SessionID {
	fixVersion := "FIX.4.2" // Default to FIX 4.2
	if cfg.Session.FIXVersion != "" {
		fixVersion = cfg.Session.FIXVersion
	}
	
	return quickfix.SessionID{
		BeginString:  fixVersion,
		SenderCompID: cfg.TargetCompID,
		TargetCompID: cfg.SenderCompID,
	}
}
