// Copyright (C) 2025. All rights reserved.
// See the file LICENSE for licensing terms.

package session

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/quickfixgo/quickfix"
	"github.com/rhombus-tech/aristo/fix-gateway/pkg/config"
)

// Manager handles FIX session management with TEE attestation
type Manager struct {
	// config contains the session configuration
	config config.FIXGatewayConfig
	
	// initiator is the QuickFIX/Go initiator
	initiator *quickfix.Initiator
	
	// sessions is a map of session IDs to sessions
	sessions map[string]*Session
	
	// sessionsMu protects sessions
	sessionsMu sync.RWMutex
	
	// attestationProvider provides TEE attestation services
	attestationProvider AttestedProvider
	
	// messageHandler handles FIX messages with attestation
	messageHandler interface {
		HandleMessage(msg *quickfix.Message) error
		AddAttestation(msg *quickfix.Message) error
		VerifyAttestation(msg *quickfix.Message) error
	}
	
	// nasdaqSessions are our connections to NASDAQ
	nasdaqSessions []*NasdaqSession
	
	// metrics for session performance
	metrics *Metrics
	
	// ctx is the context for the manager
	ctx context.Context
	
	// cancel is the cancel function for the context
	cancel context.CancelFunc
}

// NewManager creates a new session manager
func NewManager(cfg config.FIXGatewayConfig, attestationProvider AttestedProvider, messageHandler interface {
	HandleMessage(msg *quickfix.Message) error
	AddAttestation(msg *quickfix.Message) error
	VerifyAttestation(msg *quickfix.Message) error
}) (*Manager, error) {
	ctx, cancel := context.WithCancel(context.Background())
	
	m := &Manager{
		config:              cfg,
		attestationProvider: attestationProvider,
		messageHandler:      messageHandler,
		sessions:            make(map[string]*Session),
		metrics:             NewMetrics(),
		ctx:                 ctx,
		cancel:              cancel,
	}
	
	return m, nil
}

// Start starts the session manager
func (m *Manager) Start() error {
	// Create QuickFIX settings
	settings := m.createQuickFixSettings()
	
	// Create application callbacks
	application := NewApplication(m)
	
	// Create initiator
	initiator, err := quickfix.NewInitiator(application, quickfix.NewMemoryStoreFactory(), settings, m.logFactory())
	if err != nil {
		return fmt.Errorf("failed to create initiator: %w", err)
	}
	
	m.initiator = initiator
	
	// Start initiator
	if err := m.initiator.Start(); err != nil {
		return fmt.Errorf("failed to start initiator: %w", err)
	}
	
	// Start NASDAQ sessions
	if err := m.startNasdaqSessions(); err != nil {
		m.Stop()
		return fmt.Errorf("failed to start NASDAQ sessions: %w", err)
	}
	
	// Start attestation verification
	if m.config.Attestation.Enabled {
		go m.runAttestationVerification()
	}
	
	return nil
}

// Stop stops the session manager
func (m *Manager) Stop() error {
	// Cancel context
	m.cancel()
	
	// Stop NASDAQ sessions
	for _, s := range m.nasdaqSessions {
		s.Stop()
	}
	
	// Stop initiator
	if m.initiator != nil {
		m.initiator.Stop()
	}
	
	return nil
}

// createQuickFixSettings creates QuickFIX settings from configuration
func (m *Manager) createQuickFixSettings() *quickfix.Settings {
	settings := quickfix.NewSettings()
	
	// Configure global settings
	globalSettings := settings.GlobalSettings()
	globalSettings.Set("ScreenLogShowIncoming", "Y")
	globalSettings.Set("ScreenLogShowOutgoing", "Y")
	globalSettings.Set("ScreenLogShowEvents", "Y")
	
	// Configure session settings
	sessionSettings := quickfix.NewSessionSettings()
	sessionSettings.Set("ResetOnLogon", "Y")
	sessionSettings.Set("HeartBtInt", "30")
	
	// Create session ID and add the session settings
	sessionSettings.Set("BeginString", m.getBeginString(m.config.NASDAQ.PrimarySession.FIXVersion))
	sessionSettings.Set("SenderCompID", m.config.NASDAQ.SenderCompID)
	sessionSettings.Set("TargetCompID", m.config.NASDAQ.TargetCompID)
	
	// Add session settings for this session ID
	_, err := settings.AddSession(sessionSettings)
	if err != nil {
		fmt.Printf("Warning: could not add session settings: %v\n", err)
	}
	
	return settings
}

// getBeginString returns the FIX BeginString for the given version
func (m *Manager) getBeginString(version string) string {
	switch version {
	case "4.2":
		return "FIX.4.2"
	case "4.4":
		return "FIX.4.4"
	case "5.0SP2":
		return "FIXT.1.1"
	default:
		return "FIX.4.4" // Default to 4.4
	}
}

// startNasdaqSessions starts the NASDAQ sessions
func (m *Manager) startNasdaqSessions() error {
	// Create primary session
	primary, err := NewNasdaqSession(m.config.NASDAQ.PrimarySession, m.attestationProvider, m.metrics, true)
	if err != nil {
		return fmt.Errorf("failed to create primary NASDAQ session: %w", err)
	}
	
	// Create backup session
	backup, err := NewNasdaqSession(m.config.NASDAQ.BackupSession, m.attestationProvider, m.metrics, false)
	if err != nil {
		return fmt.Errorf("failed to create backup NASDAQ session: %w", err)
	}
	
	// Add sessions
	m.nasdaqSessions = append(m.nasdaqSessions, primary, backup)
	
	// Start sessions
	for _, session := range m.nasdaqSessions {
		if err := session.Start(); err != nil {
			return fmt.Errorf("failed to start NASDAQ session: %w", err)
		}
	}
	
	return nil
}

// runAttestationVerification runs periodic attestation verification
func (m *Manager) runAttestationVerification() {
	ticker := time.NewTicker(m.config.Attestation.AttestationFrequency)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			m.verifyAttestation()
		case <-m.ctx.Done():
			return
		}
	}
}

// verifyAttestation verifies attestation for all sessions
func (m *Manager) verifyAttestation() {
	if err := m.attestationProvider.VerifyAttestation(); err != nil {
		// If attestation fails, log and take action based on policy
		// For now, just log the error
		fmt.Printf("Attestation verification failed: %v\n", err)
	}
}

// logFactory returns a QuickFIX log factory
func (m *Manager) logFactory() quickfix.LogFactory {
	// In QuickFIX/Go v0.9.7, screen logging is configured through settings
	return nil
}

// Metrics represents session metrics
type Metrics struct {
	// MessagesSent is the number of messages sent
	MessagesSent int64
	
	// MessagesReceived is the number of messages received
	MessagesReceived int64
	
	// LastAttestation is the time of the last attestation
	LastAttestation time.Time
	
	// P95Latency is the 95th percentile latency
	P95Latency time.Duration
	
	// mu protects metrics
	mu sync.RWMutex
}

// NewMetrics creates new session metrics
func NewMetrics() *Metrics {
	return &Metrics{}
}

// AttestedProvider provides TEE attestation services
type AttestedProvider interface {
	// VerifyAttestation verifies the TEE attestation
	VerifyAttestation() error
	
	// AttestMessage attests a FIX message
	AttestMessage(message []byte) ([]byte, error)
	
	// VerifyMessageAttestation verifies a message attestation
	VerifyMessageAttestation(message, attestation []byte) error
}
