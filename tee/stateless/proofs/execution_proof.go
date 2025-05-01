// Package proofs provides implementations of stateless blockchain proofs
package proofs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	
	"github.com/ava-labs/avalanchego/ids"
	
	"github.com/rhombus-tech/vm/tee/stateless/core"
)

const (
	// ExecutionProofType is the identifier for execution proofs
	ExecutionProofType = "execution"
)

var (
	// ErrInvalidExecution indicates execution verification failed
	ErrInvalidExecution = errors.New("execution verification failed")
	
	// ErrInvalidInputs indicates execution inputs are invalid
	ErrInvalidInputs = errors.New("invalid execution inputs")
)

// ExecutionProof proves that a transaction was executed correctly
type ExecutionProof struct {
	// Transaction information
	TxID         ids.ID
	Inputs       [][]byte
	InputsHash   [sha256.Size]byte
	Outputs      [][]byte
	OutputsHash  [sha256.Size]byte
	
	// TEE information
	TEEMeasurement [32]byte
	TEEType        string
	EnclaveID      []byte
	RegionID       string
	
	// Proof information
	Timestamp      uint64
	StateRoot      [sha256.Size]byte
	PrevStateRoot  [sha256.Size]byte
	Signature      []byte
}

// NewExecutionProof creates a new execution proof
func NewExecutionProof(
	txID ids.ID,
	inputs [][]byte,
	outputs [][]byte,
	teeMeasurement [32]byte,
	teeType string,
	enclaveID []byte,
	regionID string,
	timestamp uint64,
	stateRoot [sha256.Size]byte,
	prevStateRoot [sha256.Size]byte,
) *ExecutionProof {
	// Calculate input and output hashes
	inputsHasher := sha256.New()
	for _, input := range inputs {
		inputsHasher.Write(input)
	}
	
	outputsHasher := sha256.New()
	for _, output := range outputs {
		outputsHasher.Write(output)
	}
	
	var inputsHash, outputsHash [sha256.Size]byte
	copy(inputsHash[:], inputsHasher.Sum(nil))
	copy(outputsHash[:], outputsHasher.Sum(nil))
	
	return &ExecutionProof{
		TxID:           txID,
		Inputs:         inputs,
		InputsHash:     inputsHash,
		Outputs:        outputs,
		OutputsHash:    outputsHash,
		TEEMeasurement: teeMeasurement,
		TEEType:        teeType,
		EnclaveID:      enclaveID,
		RegionID:       regionID,
		Timestamp:      timestamp,
		StateRoot:      stateRoot,
		PrevStateRoot:  prevStateRoot,
	}
}

// Ensure ExecutionProof implements core.StatelessProof
var _ core.StatelessProof = (*ExecutionProof)(nil)

// Verify checks if the execution proof is valid
func (p *ExecutionProof) Verify(ctx context.Context) (bool, error) {
	// Verify the proof size
	if p.Size() > MaxProofSize {
		return false, ErrInvalidProofSize
	}
	
	// Verify inputs and outputs are present
	if len(p.Inputs) == 0 || len(p.Outputs) == 0 {
		return false, ErrInvalidInputs
	}
	
	// Verify the input hash
	inputsHasher := sha256.New()
	for _, input := range p.Inputs {
		inputsHasher.Write(input)
	}
	
	calculatedInputsHash := [sha256.Size]byte{}
	copy(calculatedInputsHash[:], inputsHasher.Sum(nil))
	
	if calculatedInputsHash != p.InputsHash {
		return false, fmt.Errorf("%w: inputs hash mismatch", ErrInvalidExecution)
	}
	
	// Verify the output hash
	outputsHasher := sha256.New()
	for _, output := range p.Outputs {
		outputsHasher.Write(output)
	}
	
	calculatedOutputsHash := [sha256.Size]byte{}
	copy(calculatedOutputsHash[:], outputsHasher.Sum(nil))
	
	if calculatedOutputsHash != p.OutputsHash {
		return false, fmt.Errorf("%w: outputs hash mismatch", ErrInvalidExecution)
	}
	
	// In a real implementation, we would also:
	// 1. Verify the TEE measurements against a trusted attestation service
	// 2. Verify the signature is valid for the claimed TEE
	// 3. Verify the state transition is valid based on the inputs and outputs
	
	// For this implementation, we'll return true if the basic checks pass
	// In a production environment, this would include full cryptographic verification
	return true, nil
}

// RootHash returns the state root resulting from this execution
func (p *ExecutionProof) RootHash() [sha256.Size]byte {
	return p.StateRoot
}

// ProofType returns the type of proof
func (p *ExecutionProof) ProofType() string {
	return ExecutionProofType
}

// Serialize returns a binary representation of the proof
// Supports both length-prefixed and direct formats for WebAssembly compatibility
func (p *ExecutionProof) Serialize() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	
	// For length-prefixed format:
	// Write a length prefix (4 bytes little-endian)
	data := p.serializeDirectFormat()
	lengthBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(lengthBytes, uint32(len(data)))
	
	buf.Write(lengthBytes)
	buf.Write(data)
	
	return buf.Bytes(), nil
}

// SerializeDirectFormat serializes without length prefix
func (p *ExecutionProof) SerializeDirectFormat() ([]byte, error) {
	return p.serializeDirectFormat(), nil
}

// serializeDirectFormat is the internal implementation for direct format serialization
func (p *ExecutionProof) serializeDirectFormat() []byte {
	buf := bytes.NewBuffer(nil)
	
	// Transaction ID
	buf.Write(p.TxID[:])
	
	// Input hash
	buf.Write(p.InputsHash[:])
	
	// Output hash
	buf.Write(p.OutputsHash[:])
	
	// TEE measurement
	buf.Write(p.TEEMeasurement[:])
	
	// State roots
	buf.Write(p.StateRoot[:])
	buf.Write(p.PrevStateRoot[:])
	
	// Timestamp
	timestampBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(timestampBytes, p.Timestamp)
	buf.Write(timestampBytes)
	
	// TEE type
	teeTypeBytes := []byte(p.TEEType)
	teeTypeLenBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(teeTypeLenBytes, uint16(len(teeTypeBytes)))
	buf.Write(teeTypeLenBytes)
	buf.Write(teeTypeBytes)
	
	// Enclave ID
	enclaveIDLenBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(enclaveIDLenBytes, uint16(len(p.EnclaveID)))
	buf.Write(enclaveIDLenBytes)
	buf.Write(p.EnclaveID)
	
	// Region ID
	regionIDBytes := []byte(p.RegionID)
	regionIDLenBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(regionIDLenBytes, uint16(len(regionIDBytes)))
	buf.Write(regionIDLenBytes)
	buf.Write(regionIDBytes)
	
	// Signature
	sigLenBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(sigLenBytes, uint16(len(p.Signature)))
	buf.Write(sigLenBytes)
	buf.Write(p.Signature)
	
	// Number of inputs
	inputCountBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(inputCountBytes, uint32(len(p.Inputs)))
	buf.Write(inputCountBytes)
	
	// Inputs
	for _, input := range p.Inputs {
		inputLenBytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(inputLenBytes, uint32(len(input)))
		buf.Write(inputLenBytes)
		buf.Write(input)
	}
	
	// Number of outputs
	outputCountBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(outputCountBytes, uint32(len(p.Outputs)))
	buf.Write(outputCountBytes)
	
	// Outputs
	for _, output := range p.Outputs {
		outputLenBytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(outputLenBytes, uint32(len(output)))
		buf.Write(outputLenBytes)
		buf.Write(output)
	}
	
	return buf.Bytes()
}

// ParseExecutionProof parses a binary representation into an ExecutionProof
// Following your binary format handling, it supports both length-prefixed and direct formats
func ParseExecutionProof(data []byte) (*ExecutionProof, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("proof data too short: %d bytes", len(data))
	}
	
	// Check if this is a length-prefixed format
	// If the first 4 bytes represent a reasonable length (< MaxProofSize)
	// then treat it as length-prefixed, otherwise as direct format
	length := binary.LittleEndian.Uint32(data[:4])
	
	if length > 0 && length <= MaxProofSize && int(length) <= len(data)-4 {
		// Length-prefixed format
		return parseExecutionProofDirectFormat(data[4:4+length])
	} else {
		// Direct format (fixed size expected)
		return parseExecutionProofDirectFormat(data)
	}
}

// parseExecutionProofDirectFormat parses the direct format
func parseExecutionProofDirectFormat(data []byte) (*ExecutionProof, error) {
	minSize := 32 + 32 + 32 + 32 + 32 + 32 + 8 + 2 // Fixed size fields up to teeType length
	if len(data) < minSize {
		return nil, fmt.Errorf("proof data too short for direct format: %d bytes", len(data))
	}
	
	pos := 0
	
	// Parse fixed-size fields
	p := &ExecutionProof{}
	
	// TxID
	copy(p.TxID[:], data[pos:pos+32])
	pos += 32
	
	// InputsHash
	copy(p.InputsHash[:], data[pos:pos+32])
	pos += 32
	
	// OutputsHash
	copy(p.OutputsHash[:], data[pos:pos+32])
	pos += 32
	
	// TEEMeasurement
	copy(p.TEEMeasurement[:], data[pos:pos+32])
	pos += 32
	
	// StateRoot
	copy(p.StateRoot[:], data[pos:pos+32])
	pos += 32
	
	// PrevStateRoot
	copy(p.PrevStateRoot[:], data[pos:pos+32])
	pos += 32
	
	// Timestamp
	p.Timestamp = binary.LittleEndian.Uint64(data[pos:pos+8])
	pos += 8
	
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
	
	// EnclaveID
	if pos+2 > len(data) {
		return nil, errors.New("unexpected end of data while parsing EnclaveID length")
	}
	enclaveIDLen := binary.LittleEndian.Uint16(data[pos:pos+2])
	pos += 2
	
	if pos+int(enclaveIDLen) > len(data) {
		return nil, errors.New("unexpected end of data while parsing EnclaveID")
	}
	p.EnclaveID = make([]byte, enclaveIDLen)
	copy(p.EnclaveID, data[pos:pos+int(enclaveIDLen)])
	pos += int(enclaveIDLen)
	
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
	pos += int(sigLen)
	
	// Number of inputs
	if pos+4 > len(data) {
		return nil, errors.New("unexpected end of data while parsing input count")
	}
	inputCount := binary.LittleEndian.Uint32(data[pos:pos+4])
	pos += 4
	
	// Inputs
	p.Inputs = make([][]byte, inputCount)
	for i := uint32(0); i < inputCount; i++ {
		if pos+4 > len(data) {
			return nil, errors.New("unexpected end of data while parsing input length")
		}
		inputLen := binary.LittleEndian.Uint32(data[pos:pos+4])
		pos += 4
		
		if pos+int(inputLen) > len(data) {
			return nil, errors.New("unexpected end of data while parsing input")
		}
		p.Inputs[i] = make([]byte, inputLen)
		copy(p.Inputs[i], data[pos:pos+int(inputLen)])
		pos += int(inputLen)
	}
	
	// Number of outputs
	if pos+4 > len(data) {
		return nil, errors.New("unexpected end of data while parsing output count")
	}
	outputCount := binary.LittleEndian.Uint32(data[pos:pos+4])
	pos += 4
	
	// Outputs
	p.Outputs = make([][]byte, outputCount)
	for i := uint32(0); i < outputCount; i++ {
		if pos+4 > len(data) {
			return nil, errors.New("unexpected end of data while parsing output length")
		}
		outputLen := binary.LittleEndian.Uint32(data[pos:pos+4])
		pos += 4
		
		if pos+int(outputLen) > len(data) {
			return nil, errors.New("unexpected end of data while parsing output")
		}
		p.Outputs[i] = make([]byte, outputLen)
		copy(p.Outputs[i], data[pos:pos+int(outputLen)])
		pos += int(outputLen)
	}
	
	return p, nil
}

// Size returns the size of the proof in bytes
func (p *ExecutionProof) Size() uint64 {
	// Calculate the size of the proof
	size := 32*6 + 8 // Fixed size fields (TxID, hashes, roots, timestamp)
	
	// Variable size fields
	size += 2 + len(p.TEEType)
	size += 2 + len(p.EnclaveID)
	size += 2 + len(p.RegionID)
	size += 2 + len(p.Signature)
	
	// Inputs
	size += 4 // Input count
	for _, input := range p.Inputs {
		size += 4 + len(input) // Input length + input data
	}
	
	// Outputs
	size += 4 // Output count
	for _, output := range p.Outputs {
		size += 4 + len(output) // Output length + output data
	}
	
	return uint64(size)
}

// Sign signs the proof with the given TEE
func (p *ExecutionProof) Sign(signer interface{}) error {
	// Create message to sign
	message := bytes.NewBuffer(nil)
	message.Write(p.TxID[:])
	message.Write(p.InputsHash[:])
	message.Write(p.OutputsHash[:])
	message.Write(p.TEEMeasurement[:])
	message.Write(p.StateRoot[:])
	message.Write(p.PrevStateRoot[:])
	
	timestampBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(timestampBytes, p.Timestamp)
	message.Write(timestampBytes)
	
	message.WriteString(p.TEEType)
	message.Write(p.EnclaveID)
	message.WriteString(p.RegionID)
	
	// This is a simplified mock implementation
	// In a real implementation, you would use your actual crypto package
	// to sign the message with the TEE
	
	// Create a mock signature (32 bytes of mock data)
	sig := make([]byte, 32)
	copy(sig, message.Bytes()[:32]) // Simple mock
	
	p.Signature = sig
	return nil
}
