// Copyright (C) 2025. All rights reserved.
// See the file LICENSE for licensing terms.

package attestation

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/rhombus-tech/aristo/fix-gateway/pkg/config"
)

// TEEProvider provides TEE attestation services for FIX messages
// using your triple TEE architecture (SGX/SEV/TDX)
type TEEProvider struct {
	// config is the attestation configuration
	config config.AttestationConfig
	
	// teeTypes contains the enabled TEE types
	teeTypes []TEEType
	
	// verifiers contains TEE verifiers for each type
	verifiers map[TEEType]Verifier
	
	// verifiersMu protects verifiers
	verifiersMu sync.RWMutex
	
	// lastVerification is the time of the last verification
	lastVerification time.Time
	
	// status is the current attestation status
	status AttestationStatus
	
	// statusMu protects status
	statusMu sync.RWMutex
}

// AttestationStatus represents attestation status
type AttestationStatus int

const (
	// StatusUnknown indicates unknown attestation status
	StatusUnknown AttestationStatus = iota
	
	// StatusVerifying indicates attestation is being verified
	StatusVerifying
	
	// StatusVerified indicates attestation is verified
	StatusVerified
	
	// StatusFailed indicates attestation verification failed
	StatusFailed
)

// Verifier interface for TEE attestation
type Verifier interface {
	// Verify verifies the TEE attestation
	Verify() error
	
	// GetTEEType returns the TEE type
	GetTEEType() TEEType
	
	// AttestData attests data with the TEE
	AttestData(data []byte) (*TEEAttestation, error)
	
	// VerifyAttestation verifies data attestation
	VerifyAttestation(data []byte, att *TEEAttestation) error
}

// NewTEEProvider creates a new TEE attestation provider
func NewTEEProvider(cfg config.AttestationConfig) (*TEEProvider, error) {
	p := &TEEProvider{
		config:    cfg,
		verifiers: make(map[TEEType]Verifier),
		status:    StatusUnknown,
	}
	
	// Initialize TEE types
	if err := p.initTEETypes(); err != nil {
		return nil, fmt.Errorf("failed to initialize TEE types: %w", err)
	}
	
	// Initialize verifiers
	if err := p.initVerifiers(); err != nil {
		return nil, fmt.Errorf("failed to initialize verifiers: %w", err)
	}
	
	return p, nil
}

// initTEETypes initializes TEE types from configuration
func (p *TEEProvider) initTEETypes() error {
	for _, teeTypeStr := range p.config.TEETypes {
		teeType, err := p.teeTypeFromString(teeTypeStr)
		if err != nil {
			return err
		}
		p.teeTypes = append(p.teeTypes, teeType)
	}
	
	if len(p.teeTypes) == 0 {
		// Default to all types if none specified
		p.teeTypes = []TEEType{
			TEETypeSGX,
			TEETypeSEV,
			TEETypeTDX,
		}
	}
	
	return nil
}

// teeTypeFromString converts a string TEE type to TEEType
func (p *TEEProvider) teeTypeFromString(teeType string) (TEEType, error) {
	switch teeType {
	case "SGX":
		return TEETypeSGX, nil
	case "SEV":
		return TEETypeSEV, nil
	case "TDX":
		return TEETypeTDX, nil
	default:
		return TEETypeUnknown, fmt.Errorf("unknown TEE type: %s", teeType)
	}
}

// initVerifiers initializes verifiers for each TEE type
func (p *TEEProvider) initVerifiers() error {
	p.verifiersMu.Lock()
	defer p.verifiersMu.Unlock()
	
	for _, teeType := range p.teeTypes {
		verifier, err := p.createVerifier(teeType)
		if err != nil {
			return fmt.Errorf("failed to create verifier for %s: %w", teeType, err)
		}
		p.verifiers[teeType] = verifier
	}
	
	return nil
}

// createVerifier creates a verifier for the specified TEE type
func (p *TEEProvider) createVerifier(teeType TEEType) (Verifier, error) {
	switch teeType {
	case TEETypeSGX:
		return NewSGXVerifier()
	case TEETypeSEV:
		return NewSEVVerifier()
	case TEETypeTDX:
		return NewTDXVerifier()
	default:
		return nil, fmt.Errorf("unsupported TEE type: %s", teeType)
	}
}

// VerifyAttestation verifies the TEE attestation
func (p *TEEProvider) VerifyAttestation() error {
	p.setStatus(StatusVerifying)
	
	// Verify each TEE type
	var errs []error
	for _, teeType := range p.teeTypes {
		verifier, err := p.getVerifier(teeType)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		
		if err := verifier.Verify(); err != nil {
			errs = append(errs, fmt.Errorf("%s verification failed: %w", teeType, err))
		}
	}
	
	// If cross-verification is enabled, we need all verifiers to succeed
	if p.config.CrossVerification && len(errs) > 0 {
		p.setStatus(StatusFailed)
		return fmt.Errorf("cross-verification failed: %v", errs)
	}
	
	// If cross-verification is disabled, we just need one verifier to succeed
	if !p.config.CrossVerification && len(errs) == len(p.teeTypes) {
		p.setStatus(StatusFailed)
		return fmt.Errorf("all verifiers failed: %v", errs)
	}
	
	p.setStatus(StatusVerified)
	p.lastVerification = time.Now()
	
	return nil
}

// AttestMessage attests a FIX message using the TEE
func (p *TEEProvider) AttestMessage(message []byte) ([]byte, error) {
	// Check if attestation is verified
	if p.getStatus() != StatusVerified {
		return nil, fmt.Errorf("attestation not verified")
	}
	
	// Get the first available verifier
	var verifier Verifier
	var err error
	for _, teeType := range p.teeTypes {
		verifier, err = p.getVerifier(teeType)
		if err == nil {
			break
		}
	}
	
	if verifier == nil {
		return nil, fmt.Errorf("no verifier available")
	}
	
	// Create attestation data
	timestamp := time.Now().UnixNano()
	messageHash := sha256.Sum256(message)
	
	// Input data for attestation: timestamp + message hash
	data := make([]byte, 8+32)
	binary.LittleEndian.PutUint64(data[0:8], uint64(timestamp))
	copy(data[8:], messageHash[:])
	
	// Get attestation from TEE
	att, err := verifier.AttestData(data)
	if err != nil {
		return nil, fmt.Errorf("failed to attest data: %w", err)
	}
	
	// Serialize attestation
	attBytes, err := att.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal attestation: %w", err)
	}
	
	return attBytes, nil
}

// VerifyMessageAttestation verifies a message attestation
func (p *TEEProvider) VerifyMessageAttestation(message, attestationBytes []byte) error {
	// Check if attestation is verified
	if p.getStatus() != StatusVerified {
		return fmt.Errorf("attestation not verified")
	}
	
	// Parse attestation
	att := &TEEAttestation{}
	if err := att.UnmarshalBinary(attestationBytes); err != nil {
		return fmt.Errorf("failed to unmarshal attestation: %w", err)
	}
	
	// Verify with the appropriate TEE verifier
	verifier, err := p.getVerifier(att.Type)
	if err != nil {
		return fmt.Errorf("no verifier available for type %s: %w", att.Type, err)
	}
	
	// Create message hash
	messageHash := sha256.Sum256(message)
	
	// Timestamp is the first 8 bytes of the input hash
	timestamp := binary.LittleEndian.Uint64(att.InputHash[0:8])
	
	// Verify hash matches
	expectedInputHash := make([]byte, 8+32)
	binary.LittleEndian.PutUint64(expectedInputHash[0:8], timestamp)
	copy(expectedInputHash[8:], messageHash[:])
	
	// Create expected hash
	expectedHash := sha256.Sum256(expectedInputHash)
	
	// Verify input hash
	if !equalBytes(att.InputHash, expectedHash[:]) {
		return fmt.Errorf("input hash mismatch")
	}
	
	// Verify attestation with TEE
	return verifier.VerifyAttestation(expectedInputHash, att)
}

// equalBytes compares two byte slices for equality
func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// getVerifier returns the verifier for the specified TEE type
func (p *TEEProvider) getVerifier(teeType TEEType) (Verifier, error) {
	p.verifiersMu.RLock()
	defer p.verifiersMu.RUnlock()
	
	verifier, ok := p.verifiers[teeType]
	if !ok {
		return nil, fmt.Errorf("no verifier for TEE type: %s", teeType)
	}
	
	return verifier, nil
}

// getStatus returns the current attestation status
func (p *TEEProvider) getStatus() AttestationStatus {
	p.statusMu.RLock()
	defer p.statusMu.RUnlock()
	return p.status
}

// setStatus sets the attestation status
func (p *TEEProvider) setStatus(status AttestationStatus) {
	p.statusMu.Lock()
	defer p.statusMu.Unlock()
	p.status = status
}
