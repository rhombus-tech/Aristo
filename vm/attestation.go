// Package vm provides the core attestation-first blockchain validation system
package vm

import (
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"go.uber.org/zap"
)

// AttestationType defines the type of TEE attestation
type AttestationType int

const (
	SGXAttestation    AttestationType = iota // Intel SGX Attestation
	SEVAttestation                           // AMD SEV Attestation
	DualAttestation                          // Dual (SGX+SEV) Attestation
)

// StateProofType defines the type of state transition proof
type StateProofType int

const (
	AccumulatorProof StateProofType = iota // Accumulator-based state proof
	MerkleProof                             // Merkle-based state proof
	DirectProof                             // Direct state change proof
)

// StateTransitionProof represents a proof of state changes without requiring full state reconstruction
type StateTransitionProof struct {
	Type                StateProofType // Type of state proof (Accumulator, Merkle, Direct)
	AccumulatorValue    []byte        // Current accumulator value after transaction
	PreviousAccumulator []byte        // Previous accumulator value before transaction
	Witness             []byte        // Witness value for verification
	ChangedKeys         []string      // List of state keys that changed
	ChangedValues       [][]byte      // List of new values for changed keys
	TxHash              []byte        // Hash of the transaction
}

// AccumulatorWitness contains data needed to verify a state transition
// This matches the structure from the accumulator.rs contract
type AccumulatorWitness struct {
	Value           []byte // Witness value
	LastAccumulator []byte // Last known accumulator value
	Executor        string // Executor ID
	Measurement     []byte // Enclave measurement
	EnclaveType     string // Type of enclave (SGX/SEV)
	Timestamp       uint64 // Timestamp of the witness
}

// Attestation represents a cryptographic proof of execution from a TEE
type Attestation struct {
	TxID           ids.ID               // Transaction ID this attestation is for
	Type           AttestationType      // Type of attestation (SGX, SEV, or Dual)
	EnclaveID      []byte               // Public key or identifier of the enclave
	RegionID       string               // Region identifier where the attestation was generated
	Measurement    []byte               // Enclave measurement (hash of enclave code)
	Signature      []byte               // Primary signature of the attestation
	CrossSignature []byte               // Secondary signature for dual attestation
	Timestamp      time.Time            // Time when the attestation was generated
	Data           []byte               // Result data from execution (state hash)
	StateProof     *StateTransitionProof // Proof of state transition
	Accumulator    *AccumulatorWitness   // Accumulator witness for verification
}

// VerifySignature verifies the cryptographic signature of an attestation
func (a *Attestation) VerifySignature() error {
	// Basic verification based on attestation type
	switch a.Type {
	case SGXAttestation:
		// Verify SGX signature
		if len(a.Signature) == 0 {
			return fmt.Errorf("missing signature in SGX attestation")
		}
		
		if err := verifySignatureForMessage(a.EnclaveID, a.Data, a.Signature); err != nil {
			return fmt.Errorf("SGX signature verification failed: %w", err)
		}
		
		return nil
		
	case SEVAttestation:
		// Verify SEV signature
		if len(a.Signature) == 0 {
			return fmt.Errorf("missing signature in SEV attestation")
		}
		
		if err := verifySignatureForMessage(a.EnclaveID, a.Data, a.Signature); err != nil {
			return fmt.Errorf("SEV signature verification failed: %w", err)
		}
		
		return nil
		
	case DualAttestation:
		// Verify both signatures
		if len(a.Signature) == 0 {
			return fmt.Errorf("missing primary signature in dual attestation")
		}
		
		if len(a.CrossSignature) == 0 {
			return fmt.Errorf("missing cross signature in dual attestation")
		}
		
		if err := verifySignatureForMessage(a.EnclaveID, a.Data, a.Signature); err != nil {
			return fmt.Errorf("primary signature verification failed: %w", err)
		}
		
		if err := verifySignatureForMessage(a.EnclaveID, a.Data, a.CrossSignature); err != nil {
			return fmt.Errorf("cross signature verification failed: %w", err)
		}
		
		return nil
		
	default:
		return fmt.Errorf("unknown attestation type: %d", a.Type)
	}
}

func verifySignatureForMessage(pubKey, message, signature []byte) error {
	// Placeholder for actual cryptographic verification
	// In a real implementation, this would use proper cryptographic libraries
	// to verify the signature against the message using the public key
	return nil
}

// String returns the string representation of the attestation type
func (t AttestationType) String() string {
	switch t {
	case SGXAttestation:
		return "SGX"
	case SEVAttestation:
		return "SEV"
	case DualAttestation:
		return "Dual"
	default:
		return "Unknown"
	}
}

// LogFields returns a list of zap fields for structured logging
func (a *Attestation) LogFields() []zap.Field {
	fields := []zap.Field{
		zap.Stringer("txID", a.TxID),
		zap.String("type", a.Type.String()),
		zap.String("region", a.RegionID),
		zap.Time("timestamp", a.Timestamp),
		zap.Int("data_size", len(a.Data)),
	}
	
	// Add state proof information if available
	if a.StateProof != nil {
		fields = append(fields, 
			zap.Int("proof_type", int(a.StateProof.Type)),
			zap.Int("changed_keys", len(a.StateProof.ChangedKeys)),
		)
	}
	
	return fields
}

// VerifyStateTransition verifies a state transition proof without reconstructing the full state
func (a *Attestation) VerifyStateTransition() error {
	if a.StateProof == nil {
		return fmt.Errorf("missing state transition proof")
	}

	// Different verification strategies based on proof type
	switch a.StateProof.Type {
	case AccumulatorProof:
		return a.verifyAccumulatorProof()
	case MerkleProof:
		return a.verifyMerkleProof()
	case DirectProof:
		return a.verifyDirectProof()
	default:
		return fmt.Errorf("unsupported state proof type: %d", a.StateProof.Type)
	}
}

// verifyAccumulatorProof verifies an accumulator-based state transition proof
func (a *Attestation) verifyAccumulatorProof() error {
	// Ensure we have an accumulator witness
	if a.Accumulator == nil {
		return fmt.Errorf("missing accumulator witness for accumulator proof")
	}

	// Verify accumulator witness against current accumulator value
	calculatedWitness := computeWitness(a.StateProof.PreviousAccumulator, a.Accumulator.Measurement)
	if !compareBytes(calculatedWitness, a.Accumulator.Value) {
		return fmt.Errorf("invalid accumulator witness")
	}

	// Verify the state transition by checking the updated accumulator value
	// This avoids having to replay the full transaction
	externalTxIDBytes := a.TxID[:] // Convert to byte slice
	expectedAccumulator := updateAccumulator(a.StateProof.PreviousAccumulator, externalTxIDBytes, a.StateProof.ChangedKeys, a.StateProof.ChangedValues)
	if !compareBytes(expectedAccumulator, a.StateProof.AccumulatorValue) {
		return fmt.Errorf("invalid state transition in accumulator")
	}

	return nil
}

// verifyMerkleProof verifies a Merkle-based state transition proof
func (a *Attestation) verifyMerkleProof() error {
	// Not fully implemented - would verify inclusion proofs for state changes
	return fmt.Errorf("merkle proof verification not yet implemented")
}

// verifyDirectProof verifies a direct state change proof
func (a *Attestation) verifyDirectProof() error {
	// Simple verification of state changes by hashing
	// Only suitable for small state changes
	if len(a.StateProof.ChangedKeys) != len(a.StateProof.ChangedValues) {
		return fmt.Errorf("mismatched key-value pairs in direct proof")
	}

	// Compute hash of state changes
	hash := sha256.New()
	for i, key := range a.StateProof.ChangedKeys {
		hash.Write([]byte(key))
		hash.Write(a.StateProof.ChangedValues[i])
	}

	// Verify against transaction hash
	computedHash := hash.Sum(nil)
	if !compareBytes(computedHash, a.StateProof.TxHash) {
		return fmt.Errorf("invalid state change hash")
	}

	return nil
}

// Helper functions for accumulator operations

// computeWitness calculates a witness value from an accumulator and element
func computeWitness(accumulator, element []byte) []byte {
	hash := sha256.New()
	hash.Write(accumulator)
	hash.Write(element)
	return hash.Sum(nil)
}

// updateAccumulator updates the accumulator with new state changes
func updateAccumulator(currentAccumulator, txID []byte, changedKeys []string, changedValues [][]byte) []byte {
	hash := sha256.New()
	hash.Write(currentAccumulator)
	hash.Write(txID)

	// Add all state changes to the accumulator
	for i, key := range changedKeys {
		hash.Write([]byte(key))
		hash.Write(changedValues[i])
	}

	return hash.Sum(nil)
}

// compareBytes compares two byte slices for equality
func compareBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
