// Package mesh provides a mesh network for TEE-to-TEE communication
package mesh

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"time"
)

var (
	// ErrInvalidSnapshot indicates an invalid state snapshot
	ErrInvalidSnapshot = errors.New("invalid state snapshot")
	
	// ErrSnapshotVerificationFailed indicates a failure in snapshot verification
	ErrSnapshotVerificationFailed = errors.New("snapshot verification failed")
	
	// ErrSnapshotOutdated indicates a snapshot that is too old to be used
	ErrSnapshotOutdated = errors.New("snapshot is outdated")
)

// SnapshotType defines the type of snapshot
type SnapshotType int

const (
	// FullSnapshot is a complete state snapshot
	FullSnapshot SnapshotType = iota
	
	// DeltaSnapshot is an incremental snapshot based on a previous full snapshot
	DeltaSnapshot
)

// StateSnapshot represents a cryptographically verifiable snapshot of state
type StateSnapshot struct {
	// Core identification
	ObjectID     string            // ID of the object this snapshot represents
	RegionID     string            // Region where this snapshot was created
	SnapshotID   []byte            // Unique identifier for this snapshot (hash)
	SnapshotType SnapshotType      // Type of snapshot (full or delta)
	
	// State data
	StateData       []byte                  // The actual state data, possibly compressed
	AccumulatorState []byte                 // The 32-byte accumulator state at snapshot time
	PreviousSnapshotID []byte              // Reference to previous snapshot (for deltas or chain)
	
	// TEE attestation
	TEEMeasurement []byte                  // Measurement of the TEE that created this snapshot
	TEEID          string                  // ID of the TEE that created this snapshot
	TEEType        string                  // Type of TEE (SGX or SEV)
	TEESignature   []byte                  // Signature from the TEE covering snapshot contents
	
	// Metadata
	Timestamp        time.Time              // When this snapshot was created
	RegionalMetadata map[string]interface{} // Region-specific metadata for policy enforcement
	Version          uint64                 // Version number for the snapshot format
	
	// Performance optimization
	DataHash         []byte                 // Hash of the state data for quick verification
	CompressionType  string                 // Type of compression used, if any
	OriginalSize     uint64                 // Original size before compression
}

// SnapshotChainInfo contains information about a chain of snapshots
type SnapshotChainInfo struct {
	ObjectID         string
	LatestSnapshotID []byte
	SnapshotCount    uint64
	OldestTimestamp  time.Time
	LatestTimestamp  time.Time
}

// SnapshotError defines errors related to snapshot operations
type SnapshotError struct {
	Operation string
	ObjectID  string
	Err       error
}

// Error implements the error interface
func (e *SnapshotError) Error() string {
	return fmt.Sprintf("snapshot error in %s for object %s: %v", e.Operation, e.ObjectID, e.Err)
}

// Unwrap returns the underlying error
func (e *SnapshotError) Unwrap() error {
	return e.Err
}

// ComputeSnapshotID calculates a unique identifier for a snapshot
func ComputeSnapshotID(snapshot *StateSnapshot) []byte {
	// Create a hash using the key components of the snapshot
	hasher := sha256.New()
	hasher.Write([]byte(snapshot.ObjectID))
	hasher.Write([]byte(snapshot.RegionID))
	hasher.Write([]byte(snapshot.TEEID))
	hasher.Write(snapshot.AccumulatorState)
	hasher.Write([]byte(snapshot.Timestamp.String()))
	return hasher.Sum(nil)
}
