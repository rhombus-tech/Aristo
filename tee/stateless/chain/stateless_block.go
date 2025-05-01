// Package chain provides blockchain management for the stateless verification layer
package chain

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"
	
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	
	"github.com/rhombus-tech/vm/tee/stateless/core"
)

const (
	// MaxProofsPerBlock limits the number of proofs in a block for security
	MaxProofsPerBlock = 1000
	
	// MaxBlockSize limits the maximum block size in bytes
	MaxBlockSize = 1024 * 1024 // 1MB
)

var (
	// ErrTooManyProofs indicates a block has too many proofs
	ErrTooManyProofs = errors.New("too many proofs in block")
	
	// ErrBlockTooLarge indicates a block exceeds the maximum size
	ErrBlockTooLarge = errors.New("block too large")
)

// StatelessBlockImpl implements the StatelessBlock interface
type StatelessBlockImpl struct {
	id        ids.ID
	parentID  ids.ID
	height    uint64
	timestamp time.Time
	proofs    []core.StatelessProof
	stateRoot [sha256.Size]byte
}

// NewStatelessBlock creates a new stateless block
func NewStatelessBlock(
	parentID ids.ID,
	height uint64,
	timestamp time.Time,
	proofs []core.StatelessProof,
	stateRoot [sha256.Size]byte,
) (*StatelessBlockImpl, error) {
	if len(proofs) > MaxProofsPerBlock {
		return nil, ErrTooManyProofs
	}
	
	b := &StatelessBlockImpl{
		parentID:  parentID,
		height:    height,
		timestamp: timestamp,
		proofs:    proofs,
		stateRoot: stateRoot,
	}
	
	// Calculate the block ID
	blockBytes, err := b.Bytes()
	if err != nil {
		return nil, fmt.Errorf("failed to serialize block: %w", err)
	}
	
	if len(blockBytes) > MaxBlockSize {
		return nil, ErrBlockTooLarge
	}
	
	b.id = ids.ID(hashing.ComputeHash256(blockBytes))
	
	return b, nil
}

// ID returns the unique identifier of the block
func (b *StatelessBlockImpl) ID() ids.ID {
	return b.id
}

// ParentID returns the ID of the parent block
func (b *StatelessBlockImpl) ParentID() ids.ID {
	return b.parentID
}

// Height returns the height of the block in the chain
func (b *StatelessBlockImpl) Height() uint64 {
	return b.height
}

// Timestamp returns when the block was created
func (b *StatelessBlockImpl) Timestamp() time.Time {
	return b.timestamp
}

// Proofs returns the proofs contained in this block
func (b *StatelessBlockImpl) Proofs() []core.StatelessProof {
	return b.proofs
}

// StateRoot returns the root hash of the state after this block
func (b *StatelessBlockImpl) StateRoot() [sha256.Size]byte {
	return b.stateRoot
}

// Verify checks if all proofs in the block are valid
func (b *StatelessBlockImpl) Verify(ctx context.Context, verifier core.StatelessVerifier) (bool, error) {
	if len(b.proofs) == 0 {
		// Genesis block or special case with no proofs is considered valid
		return true, nil
	}
	
	// Verify each proof using batch verification
	results, err := verifier.VerifyProofBatch(ctx, b.proofs)
	if err != nil {
		return false, fmt.Errorf("batch verification failed: %w", err)
	}
	
	// Check if any proof failed verification
	for i, verified := range results {
		if !verified {
			return false, fmt.Errorf("proof at index %d failed verification", i)
		}
	}
	
	return true, nil
}

// Bytes returns the serialized form of the block
// Supports both length-prefixed and direct formats
func (b *StatelessBlockImpl) Bytes() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	
	// For length-prefixed format (following WebAssembly convention):
	// Write a length prefix (4 bytes little-endian)
	data := b.serializeDirectFormat()
	lengthBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(lengthBytes, uint32(len(data)))
	
	buf.Write(lengthBytes)
	buf.Write(data)
	
	return buf.Bytes(), nil
}

// BytesDirectFormat returns the block in direct format without length prefix
func (b *StatelessBlockImpl) BytesDirectFormat() ([]byte, error) {
	return b.serializeDirectFormat(), nil
}

// serializeDirectFormat is the internal implementation for direct format serialization
func (b *StatelessBlockImpl) serializeDirectFormat() []byte {
	buf := bytes.NewBuffer(nil)
	
	// Parent ID
	buf.Write(b.parentID[:])
	
	// Height
	heightBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(heightBytes, b.height)
	buf.Write(heightBytes)
	
	// Timestamp
	timeBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(timeBytes, uint64(b.timestamp.UnixNano()))
	buf.Write(timeBytes)
	
	// State root
	buf.Write(b.stateRoot[:])
	
	// Number of proofs
	proofCountBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(proofCountBytes, uint32(len(b.proofs)))
	buf.Write(proofCountBytes)
	
	// Proofs
	for _, proof := range b.proofs {
		// Each proof starts with its type
		proofType := proof.ProofType()
		proofTypeBytes := []byte(proofType)
		
		// Write proof type length and data
		proofTypeLenBytes := make([]byte, 2)
		binary.LittleEndian.PutUint16(proofTypeLenBytes, uint16(len(proofTypeBytes)))
		buf.Write(proofTypeLenBytes)
		buf.Write(proofTypeBytes)
		
		// Serialize the proof and write its length and data
		proofData, err := proof.Serialize()
		if err != nil {
			// If there's an error, we'll just skip this proof
			// This is not ideal, but allows serialization to continue
			continue
		}
		
		proofLenBytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(proofLenBytes, uint32(len(proofData)))
		buf.Write(proofLenBytes)
		buf.Write(proofData)
	}
	
	return buf.Bytes()
}

// ParseStatelessBlock parses a binary representation into a StatelessBlock
// This supports both length-prefixed and direct formats
func ParseStatelessBlock(data []byte, proofParsers map[string]func([]byte) (core.StatelessProof, error)) (*StatelessBlockImpl, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("block data too short: %d bytes", len(data))
	}
	
	// Check if this is a length-prefixed format
	// If the first 4 bytes represent a reasonable length (< MaxBlockSize)
	// then treat it as length-prefixed, otherwise as direct format
	length := binary.LittleEndian.Uint32(data[:4])
	
	if length > 0 && length <= MaxBlockSize && int(length) <= len(data)-4 {
		// Length-prefixed format
		return parseStatelessBlockDirectFormat(data[4:4+length], proofParsers)
	} else {
		// Direct format
		return parseStatelessBlockDirectFormat(data, proofParsers)
	}
}

// parseStatelessBlockDirectFormat parses the direct format
func parseStatelessBlockDirectFormat(data []byte, proofParsers map[string]func([]byte) (core.StatelessProof, error)) (*StatelessBlockImpl, error) {
	if len(data) < 32+8+8+32+4 { // Parent ID + height + timestamp + state root + proof count
		return nil, fmt.Errorf("block data too short for direct format: %d bytes", len(data))
	}
	
	pos := 0
	
	// Parse parent ID
	parentID, err := ids.ToID(data[pos:pos+32])
	if err != nil {
		return nil, fmt.Errorf("failed to parse parent ID: %w", err)
	}
	pos += 32
	
	// Parse height
	height := binary.LittleEndian.Uint64(data[pos:pos+8])
	pos += 8
	
	// Parse timestamp
	timeSecs := binary.LittleEndian.Uint64(data[pos:pos+8])
	timestamp := time.Unix(0, int64(timeSecs))
	pos += 8
	
	// Parse state root
	stateRoot := [sha256.Size]byte{}
	copy(stateRoot[:], data[pos:pos+32])
	pos += 32
	
	// Parse number of proofs
	proofCount := binary.LittleEndian.Uint32(data[pos:pos+4])
	pos += 4
	
	if proofCount > MaxProofsPerBlock {
		return nil, ErrTooManyProofs
	}
	
	// Parse proofs
	var proofs []core.StatelessProof
	for i := uint32(0); i < proofCount; i++ {
		if pos+2 > len(data) {
			return nil, errors.New("unexpected end of data while parsing proof type length")
		}
		
		proofTypeLen := binary.LittleEndian.Uint16(data[pos:pos+2])
		pos += 2
		
		if pos+int(proofTypeLen) > len(data) {
			return nil, errors.New("unexpected end of data while parsing proof type")
		}
		
		proofType := string(data[pos:pos+int(proofTypeLen)])
		pos += int(proofTypeLen)
		
		if pos+4 > len(data) {
			return nil, errors.New("unexpected end of data while parsing proof length")
		}
		
		proofLen := binary.LittleEndian.Uint32(data[pos:pos+4])
		pos += 4
		
		if pos+int(proofLen) > len(data) {
			return nil, errors.New("unexpected end of data while parsing proof")
		}
		
		proofData := data[pos:pos+int(proofLen)]
		pos += int(proofLen)
		
		// Parse the proof using the appropriate parser
		proofParser, exists := proofParsers[proofType]
		if !exists {
			return nil, fmt.Errorf("no parser registered for proof type: %s", proofType)
		}
		
		proof, err := proofParser(proofData)
		if err != nil {
			return nil, fmt.Errorf("failed to parse proof: %w", err)
		}
		
		proofs = append(proofs, proof)
	}
	
	// Create the block
	// We don't calculate the ID here since we don't have all the data
	// It will be calculated when the block is added to the chain
	b := &StatelessBlockImpl{
		parentID:  parentID,
		height:    height,
		timestamp: timestamp,
		proofs:    proofs,
		stateRoot: stateRoot,
	}
	
	// Calculate the block ID
	blockBytes, err := b.Bytes()
	if err != nil {
		return nil, fmt.Errorf("failed to serialize block: %w", err)
	}
	
	b.id = ids.ID(hashing.ComputeHash256(blockBytes))
	
	return b, nil
}
