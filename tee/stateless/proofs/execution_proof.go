// Package proofs provides implementations of stateless blockchain proofs
package proofs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"sync"
	
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
	// 1. Size validation (ensuring protection against the 3.5GB vulnerability)
	if p.Size() > MaxExecutionProofSize {
		return false, ErrInvalidProofSize
	}
	
	// 2. Input/output validation
	if err := p.verifyInputsAndOutputs(); err != nil {
		return false, err
	}
	
	// 3. TEE measurement verification
	if err := p.verifyTEEMeasurement(ctx); err != nil {
		return false, fmt.Errorf("TEE measurement verification failed: %w", err)
	}
	
	// 4. Signature verification
	if err := p.verifySignature(ctx); err != nil {
		return false, fmt.Errorf("signature verification failed: %w", err)
	}
	
	// 5. State transition validation
	if err := p.verifyStateTransition(); err != nil {
		return false, fmt.Errorf("state transition verification failed: %w", err)
	}
	
	return true, nil
}

// verifyInputsAndOutputs validates that inputs and outputs match their hashes
func (p *ExecutionProof) verifyInputsAndOutputs() error {
	// Verify inputs and outputs are present
	if len(p.Inputs) == 0 || len(p.Outputs) == 0 {
		return ErrInvalidInputs
	}
	
	// Verify the input hash
	inputsHasher := sha256.New()
	for _, input := range p.Inputs {
		inputsHasher.Write(input)
	}
	
	calculatedInputsHash := [sha256.Size]byte{}
	copy(calculatedInputsHash[:], inputsHasher.Sum(nil))
	
	if calculatedInputsHash != p.InputsHash {
		return fmt.Errorf("%w: inputs hash mismatch", ErrInvalidExecution)
	}
	
	// Verify the output hash
	outputsHasher := sha256.New()
	for _, output := range p.Outputs {
		outputsHasher.Write(output)
	}
	
	calculatedOutputsHash := [sha256.Size]byte{}
	copy(calculatedOutputsHash[:], outputsHasher.Sum(nil))
	
	if calculatedOutputsHash != p.OutputsHash {
		return fmt.Errorf("%w: outputs hash mismatch", ErrInvalidExecution)
	}
	
	return nil
}

// verifyTEEMeasurement verifies the TEE measurement against the attestation service
func (p *ExecutionProof) verifyTEEMeasurement(ctx context.Context) error {
	// Get attestation verifier from context
	verifier, err := getTEEVerifierFromContext(ctx)
	if err != nil {
		return err
	}
	
	// Validate TEE type
	if p.TEEType != TEETypeSGX && p.TEEType != TEETypeSEV && p.TEEType != TEETypeTDX {
		return fmt.Errorf("invalid TEE type: %s", p.TEEType)
	}
	
	// Check if the TEE measurement is trusted
	isTrusted, err := verifier.VerifyMeasurement(ctx, p.TEEType, p.TEEMeasurement[:])
	if err != nil {
		return fmt.Errorf("measurement verification error: %w", err)
	}
	if !isTrusted {
		return fmt.Errorf("untrusted measurement for %s TEE", p.TEEType)
	}
	
	return nil
}

// verifySignature verifies the signature from the TEE
func (p *ExecutionProof) verifySignature(ctx context.Context) error {
	// 1. If signature is empty, reject immediately
	if len(p.Signature) == 0 {
		return errors.New("missing signature")
	}
	
	// 2. Get signature verifier from context
	verifier, err := getTEEVerifierFromContext(ctx)
	if err != nil {
		return err
	}
	
	// 3. Create message to verify (everything except signature)
	signatureMessage := p.buildSignatureMessage()
	
	// 4. Verify signature using the appropriate verifier
	isValid, err := verifier.VerifySignature(ctx, p.TEEType, p.TEEMeasurement[:], p.EnclaveID, signatureMessage, p.Signature)
	if err != nil {
		return fmt.Errorf("signature verification error: %w", err)
	}
	if !isValid {
		return errors.New("invalid signature")
	}
	
	return nil
}

// buildSignatureMessage builds the message that was signed by the TEE
func (p *ExecutionProof) buildSignatureMessage() []byte {
	// Create a buffer to build the message that was signed
	var signatureMessage bytes.Buffer
	
	// Add fields to the message in the same order they're serialized
	signatureMessage.Write(p.TxID[:])
	signatureMessage.Write(p.InputsHash[:])
	signatureMessage.Write(p.OutputsHash[:])
	signatureMessage.Write(p.TEEMeasurement[:])
	signatureMessage.Write([]byte(p.TEEType))
	signatureMessage.Write(p.EnclaveID)
	signatureMessage.Write([]byte(p.RegionID))
	
	// Add timestamp as 8-byte big-endian uint64
	timestampBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(timestampBytes, p.Timestamp)
	signatureMessage.Write(timestampBytes)
	
	// Add state roots
	signatureMessage.Write(p.StateRoot[:])
	signatureMessage.Write(p.PrevStateRoot[:])
	
	return signatureMessage.Bytes()
}

// verifyStateTransition verifies that the state transition is valid
func (p *ExecutionProof) verifyStateTransition() error {
	// 1. Verify the previous state root is not zero (except for genesis)
	isZero := true
	for _, b := range p.PrevStateRoot {
		if b != 0 {
			isZero = false
			break
		}
	}
	
	if isZero && p.TxID != ids.Empty {
		return fmt.Errorf("%w: previous state root is zero for non-genesis transaction", ErrInvalidExecution)
	}
	
	// 2. Verify the state root is not zero
	isZero = true
	for _, b := range p.StateRoot {
		if b != 0 {
			isZero = false
			break
		}
	}
	
	if isZero {
		return fmt.Errorf("%w: state root cannot be zero", ErrInvalidExecution)
	}
	
	// 3. Verify inputs and outputs consistency for state transition
	if err := p.verifyInputOutputConsistency(); err != nil {
		return err
	}
	
	return nil
}

// verifyInputOutputConsistency verifies that inputs and outputs are consistent
func (p *ExecutionProof) verifyInputOutputConsistency() error {
	// Basic validation that inputs and outputs follow required patterns
	// In a real system, this would validate the actual state transition logic
	
	// Rule 1: Every transaction must have at least one input and one output
	if len(p.Inputs) == 0 || len(p.Outputs) == 0 {
		return fmt.Errorf("%w: transaction must have inputs and outputs", ErrInvalidExecution)
	}
	
	// Rule 2: Each input must be at least 4 bytes for type identification
	for i, input := range p.Inputs {
		if len(input) < 4 {
			return fmt.Errorf("%w: input %d is too small (minimum 4 bytes)", ErrInvalidExecution, i)
		}
	}
	
	// Rule 3: Each output must be at least 4 bytes for type identification
	for i, output := range p.Outputs {
		if len(output) < 4 {
			return fmt.Errorf("%w: output %d is too small (minimum 4 bytes)", ErrInvalidExecution, i)
		}
	}
	
	// Add more state transition validation rules here based on your specific business logic
	
	return nil
}

// TEEVerifier defines the interface for verifying TEE measurements and signatures
type TEEVerifier interface {
	// VerifyMeasurement verifies if a TEE measurement is trusted
	VerifyMeasurement(ctx context.Context, teeType string, measurement []byte) (bool, error)
	
	// VerifySignature verifies a signature from a TEE
	VerifySignature(ctx context.Context, teeType string, measurement []byte, enclaveID []byte, message []byte, signature []byte) (bool, error)
}

// getTEEVerifierFromContext extracts the TEE verifier from context
func getTEEVerifierFromContext(ctx context.Context) (TEEVerifier, error) {
	// Get TEE verifier from context
	verifierValue := ctx.Value(ContextKeyAttestationService)
	if verifierValue == nil {
		return nil, errors.New("TEE verifier not found in context")
	}
	
	// Type assertion
	verifier, ok := verifierValue.(TEEVerifier)
	if !ok {
		return nil, errors.New("invalid TEE verifier type in context")
	}
	
	return verifier, nil
}

// RootHash returns the state root resulting from this execution
func (p *ExecutionProof) RootHash() [sha256.Size]byte {
	return p.StateRoot
}

// BatchVerifyExecutionProofs verifies multiple execution proofs in parallel
// Optimized for high-frequency trading scenarios with efficient batching
func BatchVerifyExecutionProofs(ctx context.Context, proofs []*ExecutionProof) ([]bool, []error) {
	if len(proofs) == 0 {
		return []bool{}, []error{}
	}
	
	// Determine optimal number of workers based on available cores
	numWorkers := runtime.NumCPU()
	if numWorkers < 2 {
		numWorkers = 2 // Minimum 2 workers
	}
	if numWorkers > 8 {
		numWorkers = 8 // Maximum 8 workers to avoid excessive context switching
	}
	
	// Calculate optimal chunk size for proofs distribution
	chunkSize := calculateOptimalChunkSize(len(proofs), numWorkers)
	
	// Initialize results
	results := make([]bool, len(proofs))
	errors := make([]error, len(proofs))
	
	// Use wait group to synchronize workers
	var wg sync.WaitGroup
	
	// Process proofs in chunks
	for workerID := 0; workerID < numWorkers; workerID++ {
		// Calculate chunk range for this worker
		start := workerID * chunkSize
		end := start + chunkSize
		if end > len(proofs) {
			end = len(proofs)
		}
		if start >= len(proofs) {
			break
		}
		
		wg.Add(1)
		go func(start, end int) {
			defer wg.Done()
			
			// Process each proof in this chunk
			for i := start; i < end; i++ {
				// Skip nil proofs
				if proofs[i] == nil {
					results[i] = false
					errors[i] = fmt.Errorf("nil proof")
					continue
				}
				
				// Verify the proof
				results[i], errors[i] = proofs[i].Verify(ctx)
			}
		}(start, end)
	}
	
	// Wait for all workers to complete
	wg.Wait()
	
	return results, errors
}

// calculateOptimalChunkSize calculates the optimal chunk size for batch processing
func calculateOptimalChunkSize(totalItems, numWorkers int) int {
	// Ensure at least 1 item per worker
	minItemsPerWorker := 1
	
	// Calculate items per worker
	itemsPerWorker := totalItems / numWorkers
	
	// If items per worker is less than minimum, adjust workers down
	if itemsPerWorker < minItemsPerWorker {
		itemsPerWorker = minItemsPerWorker
	}
	
	// Max 100 items per worker to avoid excessive memory usage
	maxItemsPerWorker := 100
	if itemsPerWorker > maxItemsPerWorker {
		itemsPerWorker = maxItemsPerWorker
	}
	
	return itemsPerWorker
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



const (
	// TEECertificateVersionV1 is the version 1 of TEE certificates
	TEECertificateVersionV1 = 1
	
	// MaxExecutionProofSize is the maximum size of an execution proof in bytes
	MaxExecutionProofSize = 1024 * 1024 // 1MB
	
	// TEETypeSGX represents Intel SGX TEEs
	TEETypeSGX = "sgx"
	
	// TEETypeSEV represents AMD SEV TEEs
	TEETypeSEV = "sev"
	
	// TEETypeTDX represents Intel TDX TEEs
	TEETypeTDX = "tdx"
	
	// ContextKeyAttestationService is the key for the attestation service in the context
	ContextKeyAttestationService = "attestation_service"
)

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
