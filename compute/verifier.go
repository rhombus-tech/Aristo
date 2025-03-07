package compute

import (
	"context"
)

// TEEVerifier defines the interface for verifying TEE attestations
type TEEVerifier interface {
	// VerifyAttestation verifies that a TEE attestation is valid
	VerifyAttestation(ctx context.Context, attestation []byte) error
}
