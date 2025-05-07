// Copyright (C) 2025. All rights reserved.
// See the file LICENSE for licensing terms.

package attestation

// AttestedProvider provides TEE attestation services
type AttestedProvider interface {
	// VerifyAttestation verifies the TEE attestation
	VerifyAttestation() error
	
	// AttestMessage attests a FIX message
	AttestMessage(message []byte) ([]byte, error)
	
	// VerifyMessageAttestation verifies a message attestation
	VerifyMessageAttestation(message, attestation []byte) error
}
