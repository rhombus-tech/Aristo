// Copyright (C) 2025. All rights reserved.
// See the file LICENSE for licensing terms.

package brokerdealer

import (
	"testing"
	"time"

	"github.com/quickfixgo/quickfix"
	"github.com/rhombus-tech/aristo/fix-gateway/pkg/config"
)

// mockMarketSession is a mock implementation of the MarketSession interface
type mockMarketSession struct {
	t            *testing.T
	receivedMsgs []*quickfix.Message
}

func (m *mockMarketSession) SendAndVerify(msg *quickfix.Message) error {
	m.receivedMsgs = append(m.receivedMsgs, msg)
	m.t.Logf("Market received message: %s", msg.String())
	return nil
}

// TestBrokerMessageHandling tests the broker's message handling capabilities
func TestBrokerMessageHandling(t *testing.T) {
	// Create a mock market session
	market := &mockMarketSession{t: t}

	// Create a broker manager
	manager := &Manager{
		marketSession: market,
	}

	// Create a broker config
	brokerConfig := config.BrokerDealer{
		ID:             "BROKER1",
		Name:           "Test Broker",
		Active:         true,
		SenderCompID:   "BROKER1",
		TargetCompID:   "ARISTO",
		SessionConfig: config.SessionConfig{
			Host:       "localhost",
			Port:       9002,
			FIXVersion: "4.2",
		},
		AllowedAccounts: []string{"ACCT1", "ACCT2"},
		CommissionRate:  0.0025,
	}

	// Adapt the config for internal broker use
	brkConfig := BrokerConfig{
		ID:             brokerConfig.ID,
		Name:           brokerConfig.Name,
		SenderCompID:   brokerConfig.SenderCompID,
		TargetCompID:   brokerConfig.TargetCompID,
		ClientAccounts: brokerConfig.AllowedAccounts,
		CommissionRate: brokerConfig.CommissionRate,
	}

	// Create a broker
	broker := &Broker{
		config:         brkConfig,
		clientAccounts: make(map[string]bool),
		manager:        manager,
	}

	// Add client accounts
	for _, account := range brokerConfig.AllowedAccounts {
		broker.clientAccounts[account] = true
	}

	// Create a session ID
	sessionID := quickfix.SessionID{
		BeginString:  "FIX.4.2",
		SenderCompID: "BROKER1",
		TargetCompID: "ARISTO",
	}

	// Test cases
	tests := []struct {
		name          string
		account       string
		expectSuccess bool
	}{
		{
			name:          "Valid account",
			account:       "ACCT1",
			expectSuccess: true,
		},
		{
			name:          "Invalid account",
			account:       "INVALID-ACCT",
			expectSuccess: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Reset received messages
			market.receivedMsgs = []*quickfix.Message{}

			// Create a simple order message for testing
			orderMsg := quickfix.NewMessage()
			orderMsg.Header.SetField(35, quickfix.FIXString("D")) // NewOrderSingle
			orderMsg.Body.SetField(11, quickfix.FIXString("TEST-ORDER-1")) // ClOrdID
			orderMsg.Body.SetField(21, quickfix.FIXString("1")) // HandlInst
			orderMsg.Body.SetField(55, quickfix.FIXString("AAPL")) // Symbol
			orderMsg.Body.SetField(54, quickfix.FIXString("1")) // Side (Buy)
			// Use a string representation of time that QuickFIX can handle
			orderMsg.Body.SetField(60, quickfix.FIXString(time.Now().Format("20060102-15:04:05"))) // TransactTime
			orderMsg.Body.SetField(40, quickfix.FIXString("2")) // OrdType (Limit)
			orderMsg.Body.SetField(38, quickfix.FIXString("100")) // OrderQty
			orderMsg.Body.SetField(59, quickfix.FIXString("0")) // TimeInForce (Day)
			orderMsg.Body.SetField(44, quickfix.FIXString("150.50")) // Price
			orderMsg.Body.SetField(1, quickfix.FIXString(tc.account)) // Account

			// Process the order
			err := broker.HandleNewOrderSingle(orderMsg, sessionID)

			// Check result
			if tc.expectSuccess {
				if err != nil {
					t.Errorf("Expected success but got error: %v", err)
				}
				if len(market.receivedMsgs) != 1 {
					t.Errorf("Expected 1 message to be forwarded to market, got %d", len(market.receivedMsgs))
				}
			} else {
				if err == nil {
					t.Errorf("Expected error but got success")
				}
				if len(market.receivedMsgs) != 0 {
					t.Errorf("Expected no messages to be forwarded to market, got %d", len(market.receivedMsgs))
				}
			}
		})
	}
}

// TestValidateAccount tests the account validation function
func TestValidateAccount(t *testing.T) {
	// Create a broker
	broker := &Broker{
		config: BrokerConfig{
			ID: "BROKER1",
		},
		clientAccounts: make(map[string]bool),
	}

	// Add client accounts
	accounts := []string{"ACCT1", "ACCT2", "ACCT3"}
	for _, account := range accounts {
		broker.clientAccounts[account] = true
	}

	// Test cases
	tests := []struct {
		name    string
		account string
		want    bool
	}{
		{
			name:    "Valid account 1",
			account: "ACCT1",
			want:    true,
		},
		{
			name:    "Valid account 2",
			account: "ACCT2",
			want:    true,
		},
		{
			name:    "Invalid account",
			account: "INVALID-ACCT",
			want:    false,
		},
		{
			name:    "Empty account",
			account: "",
			want:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := broker.ValidateAccount(tc.account); got != tc.want {
				t.Errorf("ValidateAccount() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestAddBrokerInfo tests that broker info is properly added to messages
func TestAddBrokerInfo(t *testing.T) {
	// Create a simple order message for testing
	orderMsg := quickfix.NewMessage()
	orderMsg.Header.SetField(35, quickfix.FIXString("D")) // NewOrderSingle
	orderMsg.Body.SetField(11, quickfix.FIXString("TEST-ORDER-1")) // ClOrdID
	orderMsg.Body.SetField(21, quickfix.FIXString("1")) // HandlInst
	orderMsg.Body.SetField(55, quickfix.FIXString("AAPL")) // Symbol
	orderMsg.Body.SetField(54, quickfix.FIXString("1")) // Side (Buy)
	// Use a string representation of time that QuickFIX can handle
	orderMsg.Body.SetField(60, quickfix.FIXString(time.Now().Format("20060102-15:04:05"))) // TransactTime
	orderMsg.Body.SetField(40, quickfix.FIXString("2")) // OrdType (Limit)

	msg := orderMsg.ToMessage()

	// Add broker info
	brokerID := "BROKER1"
	addBrokerInfo(msg, brokerID)

	// Check if broker ID field was added
	var brokerIDField quickfix.FIXString
	err := msg.Body.GetField(9000, &brokerIDField)
	if err != nil {
		t.Errorf("Expected broker ID field to be added, but got error: %v", err)
	}

	if string(brokerIDField) != brokerID {
		t.Errorf("Broker ID mismatch: got %s, want %s", brokerIDField, brokerID)
	}
}
