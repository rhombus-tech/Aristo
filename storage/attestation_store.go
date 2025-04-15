// Package storage provides persistence capabilities for the TEE verification system
package storage

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
)

const (
	// attestationPrefix is the prefix for attestation keys in the database
	attestationPrefix = "attestation/"
)

// AttestationStore provides persistence for attestations
type AttestationStore struct {
	db database.Database
}

// NewAttestationStore creates a new store for attestations
func NewAttestationStore(db database.Database) *AttestationStore {
	return &AttestationStore{
		db: db,
	}
}

// StoreAttestation persists an attestation to storage
func (s *AttestationStore) StoreAttestation(ctx context.Context, txID ids.ID, attestation []byte) error {
	key := []byte(attestationPrefix + txID.String())
	return s.db.Put(key, attestation)
}

// GetAttestation retrieves an attestation from storage
func (s *AttestationStore) GetAttestation(ctx context.Context, txID ids.ID) ([]byte, error) {
	key := []byte(attestationPrefix + txID.String())
	attestation, err := s.db.Get(key)
	if err != nil {
		if err == database.ErrNotFound {
			return nil, fmt.Errorf("attestation not found for tx %s", txID)
		}
		return nil, fmt.Errorf("failed to get attestation: %w", err)
	}
	return attestation, nil
}

// DeleteAttestation removes an attestation from storage
func (s *AttestationStore) DeleteAttestation(ctx context.Context, txID ids.ID) error {
	key := []byte(attestationPrefix + txID.String())
	return s.db.Delete(key)
}

// AttestationManager provides methods for managing attestations
type AttestationManager interface {
	StoreAttestation(ctx context.Context, txID ids.ID, attestation []byte) error
	GetAttestation(ctx context.Context, txID ids.ID) ([]byte, error)
}

// DatabaseStateManager provides attestation storage capabilities
type DatabaseStateManager struct {
	db               database.Database
	attestationStore *AttestationStore
}

// NewDatabaseStateManager creates a database state manager with attestation support
func NewDatabaseStateManager(db database.Database) *DatabaseStateManager {
	return &DatabaseStateManager{
		db:               db,
		attestationStore: NewAttestationStore(db),
	}
}

// StoreAttestation stores an attestation via the attestation store
func (m *DatabaseStateManager) StoreAttestation(ctx context.Context, txID ids.ID, attestation []byte) error {
	return m.attestationStore.StoreAttestation(ctx, txID, attestation)
}

// GetAttestation retrieves an attestation via the attestation store
func (m *DatabaseStateManager) GetAttestation(ctx context.Context, txID ids.ID) ([]byte, error) {
	return m.attestationStore.GetAttestation(ctx, txID)
}
