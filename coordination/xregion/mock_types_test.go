package xregion

import (
	"encoding/json"
)

// Mock types needed for testing

// AttestationData represents TEE attestation information
type AttestationData struct {
	ID        string
	Timestamp int64
	Data      []byte
	Signature []byte
	TEEType   string
}

// Serialize converts AttestationData to bytes
func (a *AttestationData) Serialize() ([]byte, error) {
	return json.Marshal(a)
}

// AttestationResponse represents the response to an attestation request
type AttestationResponse struct {
	Success   bool
	Message   string
	Timestamp int64
	Signature []byte
}

// VerifierInfo represents information about a TEE verifier
type VerifierInfo struct {
	ID         string
	PublicKey  []byte
	RegionID   string
	TEEType    string
	Capabilities []string
}

// Serialize converts VerifierInfo to bytes
func (v *VerifierInfo) Serialize() ([]byte, error) {
	return json.Marshal(v)
}

// RangeRequest represents a request for a range proof from another region
func (r *RangeRequest) Serialize() ([]byte, error) {
	// Simplified implementation for testing
	return json.Marshal(r)
}
