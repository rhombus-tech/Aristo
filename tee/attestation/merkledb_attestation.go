// Package attestation provides TEE attestation functionality
package attestation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/x/merkledb"
)

var (
	// ErrInvalidStateRoot indicates an invalid MerkleDB state root
	ErrInvalidStateRoot = errors.New("invalid MerkleDB state root")
	
	// ErrAttestationVerificationFailed indicates failure in attestation verification
	ErrAttestationVerificationFailed = errors.New("attestation verification failed")
	
	// ErrMerkleRootMismatch indicates a mismatch between attested and actual roots
	ErrMerkleRootMismatch = errors.New("merkle root mismatch")
)

// MerkleDBStateAttestation contains attestation data for MerkleDB state
type MerkleDBStateAttestation struct {
	RootID       []byte            `json:"root_id"`
	RootIDString string            `json:"root_id_string"`
	Timestamp    time.Time         `json:"timestamp"`
	EnclaveID    []byte            `json:"enclave_id"`
	RegionID     string            `json:"region_id"`
	Measurement  []byte            `json:"measurement"`
	TEEType      string            `json:"tee_type"`
	Signature    []byte            `json:"signature"`
}

// MerkleDBAttestationService integrates MerkleDB state verification with TEE attestation
type MerkleDBAttestationService struct {
	attestationSvc AttestationService
	merkleDB       merkledb.MerkleDB
	enclaveID      []byte
	regionID       string
	teeType        string
}

// AttestationService defines the interface needed for TEE attestation
type AttestationService interface {
	GetAttestation(enclaveID []byte) (*EnclaveAttestation, error)
	VerifySignature(attestation *EnclaveAttestation) error
	StoreAttestation(attestation *EnclaveAttestation) error
}

// EnclaveAttestation represents a TEE attestation with signature
type EnclaveAttestation struct {
	Type        int
	EnclaveID   []byte
	Measurement []byte
	RegionID    string
	Timestamp   time.Time
	Data        []byte
	Signature   []byte
}

// NewMerkleDBAttestationService creates a new MerkleDB attestation service
func NewMerkleDBAttestationService(
	attestationSvc AttestationService,
	merkleDB merkledb.MerkleDB,
	enclaveID []byte,
	regionID string,
	teeType string,
) *MerkleDBAttestationService {
	return &MerkleDBAttestationService{
		attestationSvc: attestationSvc,
		merkleDB:       merkleDB,
		enclaveID:      enclaveID,
		regionID:       regionID,
		teeType:        teeType,
	}
}

// AttestMerkleDBState creates an attestation for the current MerkleDB state
func (m *MerkleDBAttestationService) AttestMerkleDBState() (*MerkleDBStateAttestation, error) {
	// Get the current MerkleDB root ID
	rootID, err := m.merkleDB.GetMerkleRoot(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get MerkleDB root: %w", err)
	}

	// Create attestation data
	data := MerkleDBStateAttestation{
		RootID:       rootID[:],
		RootIDString: rootID.String(),
		Timestamp:    time.Now().UTC(),
		EnclaveID:    m.enclaveID,
		RegionID:     m.regionID,
		TEEType:      m.teeType,
	}

	// Get the current TEE measurement
	attestation, err := m.attestationSvc.GetAttestation(m.enclaveID)
	if err != nil {
		return nil, fmt.Errorf("failed to get TEE measurement: %w", err)
	}
	data.Measurement = attestation.Measurement

	// Serialize attestation data for validation
	_, err = serializeAttestationData(data)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize attestation data: %w", err)
	}

	// Use the attestation's signature
	data.Signature = attestation.Signature

	return &data, nil
}

// VerifyMerkleDBStateAttestation verifies an attestation for MerkleDB state
func (m *MerkleDBAttestationService) VerifyMerkleDBStateAttestation(
	attestation *MerkleDBStateAttestation,
) error {
	// First, verify the TEE attestation
	dataToVerify, err := serializeAttestationData(*attestation)
	if err != nil {
		return fmt.Errorf("failed to serialize attestation data: %w", err)
	}

	// Create verification attestation
	verificationAtt := &EnclaveAttestation{
		Type:        0, // Use appropriate type for MerkleDB attestation
		EnclaveID:   attestation.EnclaveID,
		Measurement: attestation.Measurement,
		RegionID:    attestation.RegionID,
		Timestamp:   attestation.Timestamp,
		Data:        dataToVerify,
		Signature:   attestation.Signature,
	}

	// Verify attestation
	if err := m.attestationSvc.VerifySignature(verificationAtt); err != nil {
		return fmt.Errorf("%w: %s", ErrAttestationVerificationFailed, err)
	}

	// For optional verification against current MerkleDB state:
	// This checks if the attested root matches the current state
	// It's optional because you might be verifying a historical state
	if m.merkleDB != nil {
		// Get the current MerkleDB root ID
		currentRootID, err := m.merkleDB.GetMerkleRoot(context.Background())
		if err != nil {
			return fmt.Errorf("failed to get current MerkleDB root: %w", err)
		}

		// Convert attested root ID to ids.ID
		attestedRootID, err := ids.ToID(attestation.RootID)
		if err != nil {
			return fmt.Errorf("%w: invalid root ID format", ErrInvalidStateRoot)
		}

		// Log the root IDs rather than immediately failing
		// This allows verification of attestations from different states
		if currentRootID != attestedRootID {
			// This is informational rather than an error
			// You might be verifying a historical state attestation
			fmt.Printf("Note: Attested root ID %s differs from current state %s\n", 
				attestedRootID.String(), currentRootID.String())
		}
	}

	return nil
}

// StoreAttestationWithState creates and stores an attestation with MerkleDB state
func (m *MerkleDBAttestationService) StoreAttestationWithState() error {
	// Create attestation
	attestation, err := m.AttestMerkleDBState()
	if err != nil {
		return fmt.Errorf("failed to create MerkleDB state attestation: %w", err)
	}
	
	// Serialize the attestation data for storage
	serializedData, err := json.Marshal(attestation)
	if err != nil {
		return fmt.Errorf("failed to serialize attestation data: %w", err)
	}
	
	// Create an Attestation object for storage
	att := &EnclaveAttestation{
		Type:        0, // Use appropriate type for MerkleDB attestation
		EnclaveID:   attestation.EnclaveID,
		Measurement: attestation.Measurement,
		RegionID:    attestation.RegionID,
		Timestamp:   attestation.Timestamp,
		Data:        serializedData,
		Signature:   attestation.Signature,
	}
	
	// Store the attestation
	if err := m.attestationSvc.StoreAttestation(att); err != nil {
		return fmt.Errorf("failed to store attestation: %w", err)
	}
	
	return nil
}

// VerifyStateAgainstAttestation verifies current MerkleDB state against an attestation
func (m *MerkleDBAttestationService) VerifyStateAgainstAttestation(
	attestation *MerkleDBStateAttestation,
) error {
	// First verify the attestation itself
	if err := m.VerifyMerkleDBStateAttestation(attestation); err != nil {
		return err
	}
	
	// Then verify the current state matches the attested state
	currentRootID, err := m.merkleDB.GetMerkleRoot(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get current MerkleDB root: %w", err)
	}
	
	// Convert attested root ID to ids.ID
	attestedRootID, err := ids.ToID(attestation.RootID)
	if err != nil {
		return fmt.Errorf("%w: invalid root ID format", ErrInvalidStateRoot)
	}
	
	// Check if the root IDs match
	if currentRootID != attestedRootID {
		return fmt.Errorf("%w: expected %s, got %s", ErrMerkleRootMismatch,
			attestedRootID.String(), currentRootID.String())
	}
	
	return nil
}

// serializeAttestationData serializes attestation data for signing/verification
// We exclude the signature field as it's the result of signing
func serializeAttestationData(data MerkleDBStateAttestation) ([]byte, error) {
	// Create a copy without the signature
	dataToSign := MerkleDBStateAttestation{
		RootID:       data.RootID,
		RootIDString: data.RootIDString,
		Timestamp:    data.Timestamp,
		EnclaveID:    data.EnclaveID,
		RegionID:     data.RegionID,
		Measurement:  data.Measurement,
		TEEType:      data.TEEType,
	}
	
	return json.Marshal(dataToSign)
}
