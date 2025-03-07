// Package mocks provides mock implementations of interfaces for testing
package mocks

import (
	"context"

	"github.com/rhombus-tech/vm/compute"
	"github.com/stretchr/testify/mock"
)

// TEEVerifier is a mock implementation of the TEEVerifier interface
type TEEVerifier struct {
	mock.Mock
}

// VerifyAttestation implements compute.TEEVerifier
func (m *TEEVerifier) VerifyAttestation(ctx context.Context, attestation []byte) error {
	args := m.Called(ctx, attestation)
	return args.Error(0)
}

// Ensure TEEVerifier implements compute.TEEVerifier
var _ compute.TEEVerifier = (*TEEVerifier)(nil)
