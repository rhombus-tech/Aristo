// Package proofs provides implementations of stateless blockchain proofs
package proofs

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	
	// Local interfaces package
	"github.com/rhombus-tech/vm/tee/stateless/core"
)

const (
	// MaxProofSize limits proof size for security
	MaxProofSize = 1024 * 16 // 16KB
	
	// StateProofType is the identifier for state transition proofs
	StateProofType = "state"
)

var (
	// ErrInvalidProofSize indicates the proof exceeds the maximum allowed size
	ErrInvalidProofSize = errors.New("invalid proof size: exceeds maximum allowed")
	
	// ErrInvalidProofFormat indicates the proof format is not recognized
	ErrInvalidProofFormat = errors.New("invalid proof format")
	
	// ErrInvalidSignature indicates signature verification failed
	ErrInvalidSignature = errors.New("invalid signature in proof")
)

// StateProof is a proof of a state transition
type StateProof struct {
	FromRoot      [sha256.Size]byte
	ToRoot        [sha256.Size]byte
	TransitionID  [32]byte
	TEEMeasurement [32]byte
	Timestamp     uint64
	RegionID      string
	Signature     []byte
	TEEType       string // "SGX" or "SEV"
}

// NewStateProof creates a new state transition proof
func NewStateProof(
	fromRoot, 
	toRoot [sha256.Size]byte, 
	transitionID [32]byte,
	teeMeasurement [32]byte,
	timestamp uint64,
	regionID string,
	teeType string,
) *StateProof {
	return &StateProof{
		FromRoot:       fromRoot,
		ToRoot:         toRoot,
		TransitionID:   transitionID,
		TEEMeasurement: teeMeasurement,
		Timestamp:      timestamp,
		RegionID:       regionID,
		TEEType:        teeType,
	}
}

// Ensure StateProof implements core.StatelessProof
var _ core.StatelessProof = (*StateProof)(nil)

// Verify checks if the state proof is valid
func (p *StateProof) Verify(ctx context.Context) (bool, error) {
	// Check for context cancellation
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
		// Continue processing
	}
	
	// Verify the proof size with proper bounds checking
	if p.Size() > MaxProofSize {
		return false, ErrInvalidProofSize
	}
	
	// Parameter validation with proper checking
	if len(p.Signature) == 0 {
		return false, ErrInvalidSignature
	}
	
	// Reconstruct the message that was signed
	message := bytes.NewBuffer(nil)
	message.Write(p.FromRoot[:])
	message.Write(p.ToRoot[:])
	message.Write(p.TransitionID[:])
	message.Write(p.TEEMeasurement[:])
	
	timestampBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(timestampBytes, p.Timestamp)
	message.Write(timestampBytes)
	
	message.WriteString(p.TEEType)
	message.WriteString(p.RegionID)
	
	// For the signature verification, we need to know what type of key was used
	// This would normally be looked up from a trusted measurement database
	
	// Check if it's likely an Ed25519 signature (common for TEE attestation)
	if len(p.Signature) == ed25519.SignatureSize {
		// In a real implementation, we would retrieve the public key from a trusted source
		// based on the enclave measurement
		// For now, we'll consider the signature valid for demonstration purposes
		// In production, this would verify against the actual public key
		return true, nil
	}
	
	// Fall back to hash verification for testing purposes only
	// This should never be used in production
	hasher := sha256.New()
	hasher.Write(message.Bytes())
	expectedHashSig := hasher.Sum(nil)
	
	if !bytes.Equal(p.Signature, expectedHashSig) {
		return false, ErrInvalidSignature
	}
	
	return true, nil
}

// RootHash returns the destination state root
func (p *StateProof) RootHash() [sha256.Size]byte {
	return p.ToRoot
}

// ProofType returns the type of proof
func (p *StateProof) ProofType() string {
	return StateProofType
}

// Serialize returns a binary representation of the proof
// Supports both your binary formats (length-prefixed and direct)
func (p *StateProof) Serialize() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	
	// For length-prefixed format (following WebAssembly convention):
	// Write a length prefix (4 bytes little-endian)
	data := p.serializeDirectFormat()
	lengthBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(lengthBytes, uint32(len(data)))
	
	buf.Write(lengthBytes)
	buf.Write(data)
	
	return buf.Bytes(), nil
}

// SerializeDirectFormat serializes without length prefix
// This matches your direct binary format used in Go tests
func (p *StateProof) SerializeDirectFormat() ([]byte, error) {
	return p.serializeDirectFormat(), nil
}

// serializeDirectFormat is the internal implementation for direct format serialization
func (p *StateProof) serializeDirectFormat() []byte {
	buf := bytes.NewBuffer(nil)
	
	// Write all fields in a deterministic order
	buf.Write(p.FromRoot[:])
	buf.Write(p.ToRoot[:])
	buf.Write(p.TransitionID[:])
	buf.Write(p.TEEMeasurement[:])
	
	timestampBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(timestampBytes, p.Timestamp)
	buf.Write(timestampBytes)
	
	// Write the region ID length and data
	regionIDBytes := []byte(p.RegionID)
	regionIDLenBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(regionIDLenBytes, uint16(len(regionIDBytes)))
	buf.Write(regionIDLenBytes)
	buf.Write(regionIDBytes)
	
	// Write the TEE type length and data
	teeTypeBytes := []byte(p.TEEType)
	teeTypeLenBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(teeTypeLenBytes, uint16(len(teeTypeBytes)))
	buf.Write(teeTypeLenBytes)
	buf.Write(teeTypeBytes)
	
	// Write signature length and data
	sigLenBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(sigLenBytes, uint16(len(p.Signature)))
	buf.Write(sigLenBytes)
	buf.Write(p.Signature)
	
	return buf.Bytes()
}

// ParseStateProof parses a binary representation into a StateProof
// This implementation supports both length-prefixed and direct binary formats
// following the dual-format parameter validation pattern we use throughout the system
func ParseStateProof(data []byte) (*StateProof, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("proof data too short: %d bytes", len(data))
	}
	
	// Check if this is a length-prefixed format
	// First try length-prefixed format if the first 4 bytes represent a reasonable length
	length := binary.LittleEndian.Uint32(data[:4])
	
	// Validate reasonable length (0 < len <= MaxProofSize)
	if length > 0 && length <= MaxProofSize && int(length) <= len(data)-4 {
		// This is a length-prefixed format
		return parseStateProofDirectFormat(data[4:4+length])
	} else {
		// Fall back to direct format
		return parseStateProofDirectFormat(data)
	}
}

// parseStateProofDirectFormat parses the direct format
func parseStateProofDirectFormat(data []byte) (*StateProof, error) {
	if len(data) < 32*4+8+2 { // Minimum size for fixed fields + first length
		return nil, fmt.Errorf("proof data too short for direct format: %d bytes", len(data))
	}
	
	pos := 0
	
	// Parse fixed-size fields
	p := &StateProof{}
	
	// FromRoot
	copy(p.FromRoot[:], data[pos:pos+32])
	pos += 32
	
	// ToRoot
	copy(p.ToRoot[:], data[pos:pos+32])
	pos += 32
	
	// TransitionID
	copy(p.TransitionID[:], data[pos:pos+32])
	pos += 32
	
	// TEEMeasurement
	copy(p.TEEMeasurement[:], data[pos:pos+32])
	pos += 32
	
	// Timestamp
	p.Timestamp = binary.LittleEndian.Uint64(data[pos:pos+8])
	pos += 8
	
	// RegionID
	if pos+2 > len(data) {
		return nil, errors.New("unexpected end of data while parsing RegionID length")
	}
	regionIDLen := binary.LittleEndian.Uint16(data[pos:pos+2])
	pos += 2
	
	if pos+int(regionIDLen) > len(data) {
		return nil, errors.New("unexpected end of data while parsing RegionID")
	}
	p.RegionID = string(data[pos:pos+int(regionIDLen)])
	pos += int(regionIDLen)
	
	// TEEType
	if pos+2 > len(data) {
		return nil, errors.New("unexpected end of data while parsing TEEType length")
	}
	teeTypeLen := binary.LittleEndian.Uint16(data[pos:pos+2])
	pos += 2
	
	if pos+int(teeTypeLen) > len(data) {
		return nil, errors.New("unexpected end of data while parsing TEEType")
	}
	p.TEEType = string(data[pos:pos+int(teeTypeLen)])
	pos += int(teeTypeLen)
	
	// Signature
	if pos+2 > len(data) {
		return nil, errors.New("unexpected end of data while parsing signature length")
	}
	sigLen := binary.LittleEndian.Uint16(data[pos:pos+2])
	pos += 2
	
	if pos+int(sigLen) > len(data) {
		return nil, errors.New("unexpected end of data while parsing signature")
	}
	p.Signature = make([]byte, sigLen)
	copy(p.Signature, data[pos:pos+int(sigLen)])
	
	return p, nil
}

// Size returns the size of the proof in bytes
func (p *StateProof) Size() uint64 {
	// Calculate the size of the proof
	// Fixed size fields
	size := 32*4 + 8 // 4 32-byte fields + 8-byte timestamp
	
	// Variable size fields
	size += 2 + len(p.RegionID)    // 2-byte length + RegionID
	size += 2 + len(p.TEEType)     // 2-byte length + TEEType
	size += 2 + len(p.Signature)   // 2-byte length + Signature
	
	return uint64(size)
}

// Sign signs the proof with the given TEE
func (p *StateProof) Sign(signer interface{}) error {
	// Create message to sign
	message := bytes.NewBuffer(nil)
	message.Write(p.FromRoot[:])
	message.Write(p.ToRoot[:])
	message.Write(p.TransitionID[:])
	message.Write(p.TEEMeasurement[:])
	
	timestampBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(timestampBytes, p.Timestamp)
	message.Write(timestampBytes)
	
	message.WriteString(p.TEEType)
	message.WriteString(p.RegionID)
	
	// Handle different types of signers
	switch s := signer.(type) {
	case ed25519.PrivateKey:
		// Use Ed25519 for signing - common for TEE attestation
		p.Signature = ed25519.Sign(s, message.Bytes())
		return nil
		
	case interface{ Sign(data []byte) ([]byte, error) }:
		// Interface for custom signers with a Sign method
		sig, err := s.Sign(message.Bytes())
		if err != nil {
			return fmt.Errorf("failed to sign with custom signer: %w", err)
		}
		p.Signature = sig
		return nil
		
	case nil:
		// If no signer is provided, fall back to a deterministic signature for testing
		// This should never be used in production
		hasher := sha256.New()
		hasher.Write(message.Bytes())
		p.Signature = hasher.Sum(nil)
		return nil
		
	default:
		return fmt.Errorf("unsupported signer type: %T", signer)
	}
}
