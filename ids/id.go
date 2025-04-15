package ids

import (
	"encoding/hex"
	"errors"
	"fmt"
)

const (
	// IDLen is the fixed length of a transaction ID in bytes
	IDLen = 32
)

var (
	// ErrInvalidIDLength is returned when an ID has an invalid length
	ErrInvalidIDLength = errors.New("invalid ID length")
	// ErrIDHexInvalid is returned when an ID contains invalid hex characters
	ErrIDHexInvalid = errors.New("invalid hex characters in ID")
	// Empty is an empty ID
	Empty = ID{}
)

// ID represents a 32-byte identifier for a transaction
type ID [IDLen]byte

// String returns the hex-encoded string representation of the ID
func (id ID) String() string {
	return hex.EncodeToString(id[:])
}

// Bytes returns the underlying bytes of the ID
func (id ID) Bytes() []byte {
	return id[:]
}

// FromString creates an ID from a string representation
func FromString(idStr string) (ID, error) {
	var id ID

	if len(idStr) != IDLen*2 {
		return Empty, fmt.Errorf("%w: expected %d characters but got %d", 
			ErrInvalidIDLength, IDLen*2, len(idStr))
	}

	decoded, err := hex.DecodeString(idStr)
	if err != nil {
		return Empty, fmt.Errorf("%w: %s", ErrIDHexInvalid, err)
	}

	copy(id[:], decoded)
	return id, nil
}

// FromBytes creates an ID from a byte slice
func FromBytes(bytes []byte) (ID, error) {
	var id ID

	if len(bytes) != IDLen {
		return Empty, fmt.Errorf("%w: expected %d bytes but got %d", 
			ErrInvalidIDLength, IDLen, len(bytes))
	}

	copy(id[:], bytes)
	return id, nil
}

// IsZero returns true if the ID is all zeros
func (id ID) IsZero() bool {
	for _, b := range id {
		if b != 0 {
			return false
		}
	}
	return true
}

// Equals returns true if the IDs are equal
func (id ID) Equals(other ID) bool {
	for i := 0; i < IDLen; i++ {
		if id[i] != other[i] {
			return false
		}
	}
	return true
}
