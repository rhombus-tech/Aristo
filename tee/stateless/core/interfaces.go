// Package core provides the core interfaces for the stateless blockchain implementation
package core

import (
	"context"
	"crypto/sha256"
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

// StatelessProof represents a cryptographic proof that can be verified without state access
type StatelessProof interface {
	// Verify checks if the proof is valid
	Verify(ctx context.Context) (bool, error)
	
	// RootHash returns the Merkle root hash this proof is based on
	RootHash() [sha256.Size]byte
	
	// ProofType returns the type of proof (e.g., "state", "execution", "attestation")
	ProofType() string
	
	// Serialize returns a binary representation of the proof
	// Supports both length-prefixed and direct formats based on context
	Serialize() ([]byte, error)
	
	// Size returns the size of the proof in bytes
	Size() uint64
}

// WitnessGenerator creates stateless proofs for blockchain operations
type WitnessGenerator interface {
	// GenerateStateWitness creates a proof for a state root transition
	GenerateStateWitness(ctx context.Context, from, to [sha256.Size]byte) (StatelessProof, error)
	
	// GenerateExecutionWitness creates a proof for transaction execution
	GenerateExecutionWitness(ctx context.Context, txID ids.ID, inputs, outputs [][]byte) (StatelessProof, error)
	
	// GenerateAttestationWitness creates a proof linking TEE attestation to state
	GenerateAttestationWitness(ctx context.Context, attestation []byte, stateRoot [sha256.Size]byte) (StatelessProof, error)
	
	// Close releases any resources used by the generator
	Close() error
}

// StatelessVerifier verifies proofs without accessing state
type StatelessVerifier interface {
	// VerifyProof checks if a proof is valid without requiring state access
	VerifyProof(ctx context.Context, proof StatelessProof) (bool, error)
	
	// VerifyProofBatch efficiently verifies multiple proofs in a batch
	VerifyProofBatch(ctx context.Context, proofs []StatelessProof) ([]bool, error)
	
	// RegisterProofType registers a new proof type with the verifier
	RegisterProofType(proofType string, verifier func(context.Context, []byte) (bool, error)) error
}

// StatelessBlock represents a block in the stateless blockchain
type StatelessBlock interface {
	// ID returns the unique identifier of the block
	ID() ids.ID
	
	// ParentID returns the ID of the parent block
	ParentID() ids.ID
	
	// Height returns the height of the block in the chain
	Height() uint64
	
	// Timestamp returns when the block was created
	Timestamp() time.Time
	
	// Proofs returns the proofs contained in this block
	Proofs() []StatelessProof
	
	// StateRoot returns the root hash of the state after this block
	StateRoot() [sha256.Size]byte
	
	// Verify checks if all proofs in the block are valid
	Verify(ctx context.Context, verifier StatelessVerifier) (bool, error)
	
	// Bytes returns the serialized form of the block
	// Supports both length-prefixed and direct formats
	Bytes() ([]byte, error)
}

// TEEAttestationProvider interfaces with the TEE attestation system
type TEEAttestationProvider interface {
	// GetAttestation returns the attestation for a specific TEE
	GetAttestation(enclaveID []byte) ([]byte, error)
	
	// VerifyAttestation checks if an attestation is valid
	VerifyAttestation(attestation []byte) (bool, error)
	
	// GetTEEMeasurement returns the current TEE measurement
	GetTEEMeasurement(teeType string) ([]byte, error)
}

// StatelessChain manages the stateless blockchain
type StatelessChain interface {
	// AddBlock adds a new block to the chain
	AddBlock(ctx context.Context, block StatelessBlock) error
	
	// GetBlock retrieves a block by its ID
	GetBlock(ctx context.Context, id ids.ID) (StatelessBlock, error)
	
	// GetHeight returns the current height of the chain
	GetHeight(ctx context.Context) (uint64, error)
	
	// GetLatestStateRoot returns the latest state root
	GetLatestStateRoot(ctx context.Context) ([sha256.Size]byte, error)
	
	// VerifyChain verifies the entire chain or a segment of it
	VerifyChain(ctx context.Context, fromHeight, toHeight uint64) (bool, error)
}
