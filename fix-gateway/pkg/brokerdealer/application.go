// Copyright (C) 2025. All rights reserved.
// See the file LICENSE for licensing terms.

package brokerdealer

import (
	"log"

	"github.com/quickfixgo/quickfix"
)

// Application implements the QuickFIX Application interface for broker-dealer sessions
type Application struct {
	manager *Manager
}

// NewApplication creates a new QuickFIX application for broker-dealer FIX sessions
func NewApplication(manager *Manager) *Application {
	return &Application{
		manager: manager,
	}
}

// OnCreate is called when a session is created
func (a *Application) OnCreate(sessionID quickfix.SessionID) {
	log.Printf("Broker session created: %v", sessionID)
}

// OnLogon is called when a session logs on
func (a *Application) OnLogon(sessionID quickfix.SessionID) {
	log.Printf("Broker session logged on: %v", sessionID)
	
	// Find the broker by session ID
	for _, broker := range a.manager.brokers {
		if sessionsEqual(broker.sessionID, sessionID) {
			broker.SetLoggedOn(true)
			log.Printf("Broker %s (%s) is now logged on", broker.GetID(), broker.GetName())
			break
		}
	}
}

// OnLogout is called when a session logs out
func (a *Application) OnLogout(sessionID quickfix.SessionID) {
	log.Printf("Broker session logged out: %v", sessionID)
	
	// Find the broker by session ID
	for _, broker := range a.manager.brokers {
		if sessionsEqual(broker.sessionID, sessionID) {
			broker.SetLoggedOn(false)
			log.Printf("Broker %s (%s) is now logged out", broker.GetID(), broker.GetName())
			break
		}
	}
}

// ToAdmin is called before sending an admin message
func (a *Application) ToAdmin(msg *quickfix.Message, sessionID quickfix.SessionID) {
	// No special processing needed for admin messages
}

// ToApp is called before sending an application message
func (a *Application) ToApp(msg *quickfix.Message, sessionID quickfix.SessionID) error {
	// No special processing needed for outgoing application messages
	return nil
}

// FromAdmin is called when an admin message is received
func (a *Application) FromAdmin(msg *quickfix.Message, sessionID quickfix.SessionID) quickfix.MessageRejectError {
	// No special processing needed for admin messages
	return nil
}

// FromApp is called when an application message is received
func (a *Application) FromApp(msg *quickfix.Message, sessionID quickfix.SessionID) quickfix.MessageRejectError {
	msgType, err := msg.Header.GetString(35) // Tag 35 is MsgType
	if err != nil {
		return quickfix.NewMessageRejectError("Missing message type", 999, nil)
	}
	
	// Find the broker by session ID
	var broker *Broker
	for _, b := range a.manager.brokers {
		if sessionsEqual(b.sessionID, sessionID) {
			broker = b
			break
		}
	}
	
	if broker == nil {
		return quickfix.NewMessageRejectError("Unknown broker session", 999, nil)
	}
	
	// Route message based on type
	switch msgType {
	case "D": // New Order Single
		return broker.HandleNewOrderSingle(msg, sessionID)
		
	case "F": // Order Cancel Request
		return broker.HandleOrderCancelRequest(msg, sessionID)
		
	default:
		log.Printf("Received unsupported message type from broker %s: %s", broker.GetID(), msgType)
		return quickfix.NewBusinessMessageRejectError("Unsupported message type", 999, nil)
	}
}

// sessionsEqual compares two FIX session IDs
func sessionsEqual(a, b quickfix.SessionID) bool {
	return a.BeginString == b.BeginString &&
		a.SenderCompID == b.SenderCompID &&
		a.TargetCompID == b.TargetCompID &&
		a.Qualifier == b.Qualifier
}
