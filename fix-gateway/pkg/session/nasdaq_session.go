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

// NasdaqSession represents a FIX session with NASDAQ
type NasdaqSession struct {
	// config is the session configuration
	config config.SessionConfig
	
	// attestationProvider provides TEE attestation services
	attestationProvider AttestedProvider
	
	// metrics tracks session performance
	metrics *Metrics
	
	// isPrimary indicates if this is the primary session
	isPrimary bool
	
	// session is the underlying QuickFIX session
	session quickfix.SessionID
	
	// status tracks the session status
	status SessionStatus
	
	// statusMu protects the status
	statusMu sync.RWMutex
	
	// lastHeartbeat is the time of the last heartbeat
	lastHeartbeat time.Time
	
	// ctx is the context for the session
	ctx context.Context
	
	// cancel is the cancel function for the context
	cancel context.CancelFunc
	
	// messageBuffer buffers messages during attestation verification
	messageBuffer *MessageBuffer
}

// SessionStatus represents the status of a session
type SessionStatus int

const (
	// StatusDisconnected indicates the session is disconnected
	StatusDisconnected SessionStatus = iota
	
	// StatusConnecting indicates the session is connecting
	StatusConnecting
	
	// StatusLoggedOn indicates the session is logged on
	StatusLoggedOn
	
	// StatusAttestation indicates the session is performing attestation
	StatusAttestation
	
	// StatusActive indicates the session is active and attested
	StatusActive
)

// NewNasdaqSession creates a new NASDAQ session
func NewNasdaqSession(
	cfg config.SessionConfig,
	attestationProvider AttestedProvider,
	metrics *Metrics,
	isPrimary bool,
) (*NasdaqSession, error) {
	ctx, cancel := context.WithCancel(context.Background())
	
	s := &NasdaqSession{
		config:              cfg,
		attestationProvider: attestationProvider,
		metrics:             metrics,
		isPrimary:           isPrimary,
		status:              StatusDisconnected,
		ctx:                 ctx,
		cancel:              cancel,
		messageBuffer:       NewMessageBuffer(),
	}
	
	return s, nil
}

// Start starts the session
func (s *NasdaqSession) Start() error {
	s.setStatus(StatusConnecting)
	
	// Session creation happens in the session manager
	// Here we just set up monitoring and attestation
	
	go s.monitorSession()
	
	return nil
}

// Stop stops the session
func (s *NasdaqSession) Stop() {
	s.cancel()
	s.setStatus(StatusDisconnected)
}

// monitorSession monitors the session status
func (s *NasdaqSession) monitorSession() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			s.checkSessionHealth()
		case <-s.ctx.Done():
			return
		}
	}
}

// checkSessionHealth checks the health of the session
func (s *NasdaqSession) checkSessionHealth() {
	// Check if we're logged on
	if !s.isLoggedOn() {
		return
	}
	
	// If we're not in active status, try to transition to active
	if s.getStatus() != StatusActive {
		if err := s.performAttestation(); err != nil {
			fmt.Printf("Failed to perform attestation: %v\n", err)
			return
		}
	}
	
	// Check heartbeat
	if time.Since(s.lastHeartbeat) > 30*time.Second {
		fmt.Println("No heartbeat received in 30 seconds, disconnecting")
		s.setStatus(StatusDisconnected)
	}
}

// isLoggedOn returns true if the session is logged on
func (s *NasdaqSession) isLoggedOn() bool {
	// Check if status is LoggedOn or Active
	status := s.getStatus()
	return status == StatusLoggedOn || status == StatusActive
}

// getStatus returns the session status
func (s *NasdaqSession) getStatus() SessionStatus {
	s.statusMu.RLock()
	defer s.statusMu.RUnlock()
	return s.status
}

// setStatus sets the session status
func (s *NasdaqSession) setStatus(status SessionStatus) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	s.status = status
}

// performAttestation performs TEE attestation for the session
func (s *NasdaqSession) performAttestation() error {
	s.setStatus(StatusAttestation)
	
	// Verify the attestation
	if err := s.attestationProvider.VerifyAttestation(); err != nil {
		s.setStatus(StatusLoggedOn) // Revert to logged on state
		return fmt.Errorf("attestation verification failed: %w", err)
	}
	
	// Update metrics
	s.metrics.mu.Lock()
	s.metrics.LastAttestation = time.Now()
	s.metrics.mu.Unlock()
	
	// Set status to active
	s.setStatus(StatusActive)
	
	// Process buffered messages
	s.processBufferedMessages()
	
	return nil
}

// processBufferedMessages processes buffered messages
func (s *NasdaqSession) processBufferedMessages() {
	messages := s.messageBuffer.Drain()
	for _, msg := range messages {
		// Process each message
		s.processMessage(msg)
	}
}

// processMessage processes a FIX message with attestation
func (s *NasdaqSession) processMessage(message []byte) error {
	// If not active, buffer the message
	if s.getStatus() != StatusActive {
		s.messageBuffer.Add(message)
		return nil
	}
	
	// Attest the message
	attestation, err := s.attestationProvider.AttestMessage(message)
	if err != nil {
		return fmt.Errorf("failed to attest message: %w", err)
	}
	
	// TODO: Process the attested message with business logic
	_ = attestation // Use the attestation
	
	// Update metrics
	s.metrics.mu.Lock()
	s.metrics.MessagesReceived++
	s.metrics.mu.Unlock()
	
	return nil
}

// SendMessage sends a FIX message
func (s *NasdaqSession) SendMessage(message []byte) error {
	// If not active, return error
	if s.getStatus() != StatusActive {
		return fmt.Errorf("session not active")
	}
	
	// Attest the message
	attestation, err := s.attestationProvider.AttestMessage(message)
	if err != nil {
		return fmt.Errorf("failed to attest message: %w", err)
	}
	
	// TODO: Send the message with attestation
	_ = attestation // Use the attestation
	
	// Update metrics
	s.metrics.mu.Lock()
	s.metrics.MessagesSent++
	s.metrics.mu.Unlock()
	
	return nil
}

// MessageBuffer buffers messages
type MessageBuffer struct {
	messages [][]byte
	mu       sync.Mutex
}

// NewMessageBuffer creates a new message buffer
func NewMessageBuffer() *MessageBuffer {
	return &MessageBuffer{
		messages: make([][]byte, 0),
	}
}

// Add adds a message to the buffer
func (b *MessageBuffer) Add(message []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.messages = append(b.messages, message)
}

// Drain drains the buffer and returns all messages
func (b *MessageBuffer) Drain() [][]byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	messages := b.messages
	b.messages = make([][]byte, 0)
	return messages
}
