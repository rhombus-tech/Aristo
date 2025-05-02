// Copyright (C) 2025, Rhombus Technologies. All rights reserved.
// See the file LICENSE for licensing terms.

package archive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/rhombus-tech/vm/tee/stateless/core"
)

// TEE operation identifiers
const (
	OpSecureCommit     = "secure_commit"
	OpSecureOpenAtPoint = "secure_open_at_point"
	
	// Maximum reasonable parameter size
	MaxParamSize = 1024 * 1024 // 1MB
)

// encodeBlocksToMatrix encodes blocks into a matrix suitable for polynomial commitment
func (t *TEEPolynomialCircuit) encodeBlocksToMatrix(blocks []core.StatelessBlock) ([]byte, error) {
	// Calculate matrix dimensions
	// Each block requires 2 rows: one for block data, one for state transition
	rows := len(blocks) * 2
	
	// Each row has fixed columns: hash(32) + timestamp(8) + state(32) + flags(4)
	cols := 76 / t.fieldElementSize
	if 76 % t.fieldElementSize != 0 {
		cols++ // Round up if not evenly divisible
	}
	
	// Allocate buffer for the matrix
	// Matrix encoding format: [rows(u32)][cols(u32)][row-major data]
	matrixSize := 8 + (rows * cols * t.fieldElementSize)
	matrix := make([]byte, matrixSize)
	
	// Write header: rows and columns
	binary.LittleEndian.PutUint32(matrix[0:4], uint32(rows))
	binary.LittleEndian.PutUint32(matrix[4:8], uint32(cols))
	
	// Populate matrix with block data
	offset := 8
	for _, block := range blocks {
		// Get block data
		blockID := block.ID()
		stateRoot := block.StateRoot()
		timestamp := block.Timestamp().UnixNano()
		
		// First row: block ID and metadata
		copy(matrix[offset:offset+32], blockID[:])
		binary.LittleEndian.PutUint64(matrix[offset+32:offset+40], uint64(timestamp))
		// Add padding to complete the row
		offset += cols * t.fieldElementSize
		
		// Second row: state transition data
		copy(matrix[offset:offset+32], stateRoot[:])
		// Set a flag indicating if this block has TEE attestation
		hasAttestation := uint32(0)
		for _, proof := range block.Proofs() {
			if hasAttestationInProof(proof) {
				hasAttestation = 1
				break
			}
		}
		binary.LittleEndian.PutUint32(matrix[offset+32:offset+36], hasAttestation)
		// Add padding to complete the row
		offset += cols * t.fieldElementSize
	}
	
	return matrix, nil
}

// callTEESecureCommit calls the TEE controller to perform a secure commit operation
func (t *TEEPolynomialCircuit) callTEESecureCommit(ctx context.Context, matrix []byte) ([]byte, []byte, error) {
	// Create execution payload for TEE controller
	payload := struct {
		Input         []byte `json:"input"`
		Operation     string `json:"operation"`
		TargetTEE     string `json:"target_tee"`
		AllowFallback bool   `json:"allow_fallback"`
	}{
		Input:         matrix,
		Operation:     OpSecureCommit,
		TargetTEE:     "sgx", // Default to SGX for higher security
		AllowFallback: true,  // Allow fallback to another TEE if SGX is unavailable
	}
	
	// Call TEE controller
	response, err := t.callTEEController(ctx, payload)
	if err != nil {
		return nil, nil, err
	}
	
	// Extract commitment and attestation from response
	// Response format: [commitment_size(u32)][commitment][attestation_size(u32)][attestation]
	if len(response) < 8 {
		return nil, nil, fmt.Errorf("invalid response size: %d", len(response))
	}
	
	commitmentSize := binary.LittleEndian.Uint32(response[0:4])
	if commitmentSize == 0 || int(commitmentSize+8) > len(response) {
		return nil, nil, fmt.Errorf("invalid commitment size: %d", commitmentSize)
	}
	
	commitment := response[4 : 4+commitmentSize]
	
	attestationSizeOffset := 4 + commitmentSize
	if attestationSizeOffset+4 > uint32(len(response)) {
		return nil, nil, fmt.Errorf("response too small for attestation size")
	}
	
	attestationSize := binary.LittleEndian.Uint32(response[attestationSizeOffset : attestationSizeOffset+4])
	if attestationSize == 0 || attestationSizeOffset+4+attestationSize > uint32(len(response)) {
		return nil, nil, fmt.Errorf("invalid attestation size: %d", attestationSize)
	}
	
	attestation := response[attestationSizeOffset+4 : attestationSizeOffset+4+attestationSize]
	
	return commitment, attestation, nil
}

// callTEESecureOpenAtPoint calls the TEE controller to verify a commitment at a point
func (t *TEEPolynomialCircuit) callTEESecureOpenAtPoint(ctx context.Context, commitment, point []byte) (bool, error) {
	// Prepare input data: [commitment_size(u32)][commitment][point_size(u32)][point]
	inputSize := 8 + len(commitment) + len(point)
	input := make([]byte, inputSize)
	
	// Write commitment with length prefix
	binary.LittleEndian.PutUint32(input[0:4], uint32(len(commitment)))
	copy(input[4:4+len(commitment)], commitment)
	
	// Write point with length prefix
	pointOffset := 4 + len(commitment)
	binary.LittleEndian.PutUint32(input[pointOffset:pointOffset+4], uint32(len(point)))
	copy(input[pointOffset+4:], point)
	
	// Create execution payload
	payload := struct {
		Input         []byte `json:"input"`
		Operation     string `json:"operation"`
		TargetTEE     string `json:"target_tee"`
		AllowFallback bool   `json:"allow_fallback"`
	}{
		Input:         input,
		Operation:     OpSecureOpenAtPoint,
		TargetTEE:     "sgx", // Default to SGX
		AllowFallback: true,
	}
	
	// Call TEE controller
	response, err := t.callTEEController(ctx, payload)
	if err != nil {
		return false, err
	}
	
	// Parse response - a single byte indicating success (1) or failure (0)
	if len(response) < 1 {
		return false, fmt.Errorf("invalid response size: %d", len(response))
	}
	
	return response[0] == 1, nil
}

// callTEEController sends a request to the TEE controller and returns the response
func (t *TEEPolynomialCircuit) callTEEController(ctx context.Context, payload interface{}) ([]byte, error) {
	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 60 * time.Second,
	}
	
	// Marshal payload to JSON
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}
	
	// Create request with context
	req, err := http.NewRequestWithContext(ctx, "POST", t.teeEndpoint, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	// Set headers
	req.Header.Set("Content-Type", "application/json")
	
	// Send request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()
	
	// Check response status
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TEE controller returned status: %d", resp.StatusCode)
	}
	
	// Read response body
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	
	// Parse response
	var responseObj struct {
		Result []byte `json:"result"`
		Error  string `json:"error"`
	}
	
	if err := json.Unmarshal(body, &responseObj); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	
	// Check for error
	if responseObj.Error != "" {
		return nil, fmt.Errorf("TEE controller error: %s", responseObj.Error)
	}
	
	return responseObj.Result, nil
}

// generatePointVector generates an evaluation point from the requested heights
func (t *TEEPolynomialCircuit) generatePointVector(startHeight, endHeight uint64) ([]byte, error) {
	// Use a combination of heights to derive a unique evaluation point
	// Properly formatted as a vector for the TEE polynomial commitment operations
	
	// Create a unique seed from the heights
	seed := make([]byte, 16)
	binary.LittleEndian.PutUint64(seed[0:8], startHeight)
	binary.LittleEndian.PutUint64(seed[8:16], endHeight)
	
	// Hash the seed to get a deterministic value
	hash := sha256.Sum256(seed)
	
	// Create a vector with 2 field elements (r and r')
	// [length(u32)][field elements]
	vectorSize := 4 + (2 * t.fieldElementSize)
	vector := make([]byte, vectorSize)
	
	// Write vector length
	binary.LittleEndian.PutUint32(vector[0:4], 2)
	
	// Copy hash bytes into two field elements
	// This is a simplified approach - in production, we'd need to ensure
	// these are valid field elements in the correct range
	copy(vector[4:4+t.fieldElementSize], hash[0:t.fieldElementSize])
	copy(vector[4+t.fieldElementSize:], hash[t.fieldElementSize:2*t.fieldElementSize])
	
	return vector, nil
}

// combineCommitments combines individual commitments into a matrix for recursive proofs
func (t *TEEPolynomialCircuit) combineCommitments(proofs []PolynomialProof) ([]byte, error) {
	// Count total number of field elements in all commitments
	totalElements := 0
	for _, proof := range proofs {
		// Skip the 8-byte header (rows and cols)
		elements := (len(proof.Commitment) - 8) / t.fieldElementSize
		totalElements += elements
	}
	
	// Calculate matrix dimensions
	// Each commitment becomes a row in the new matrix
	rows := len(proofs)
	cols := totalElements / rows
	if totalElements % rows != 0 {
		cols++ // Round up if not evenly divisible
	}
	
	// Allocate buffer for the combined matrix
	matrixSize := 8 + (rows * cols * t.fieldElementSize)
	matrix := make([]byte, matrixSize)
	
	// Write header: rows and columns
	binary.LittleEndian.PutUint32(matrix[0:4], uint32(rows))
	binary.LittleEndian.PutUint32(matrix[4:8], uint32(cols))
	
	// Populate matrix with commitments
	offset := 8
	for _, proof := range proofs {
		// Extract commitment data (skip 8-byte header)
		commitmentData := proof.Commitment[8:]
		
		// Copy commitment data to the matrix
		copy(matrix[offset:offset+len(commitmentData)], commitmentData)
		
		// Move to the next row
		offset += cols * t.fieldElementSize
	}
	
	return matrix, nil
}

// verifyAttestation verifies the TEE attestation
func (t *TEEPolynomialCircuit) verifyAttestation(attestation []byte) bool {
	// In a production environment, this would perform a full verification
	// of the TEE attestation, including signature validation and checking
	// against known public keys
	
	// For now, we'll implement a basic structure check
	if len(attestation) < 8 {
		return false
	}
	
	// Check attestation header magic
	magic := binary.LittleEndian.Uint32(attestation[0:4])
	version := binary.LittleEndian.Uint32(attestation[4:8])
	
	// Magic should be "TEAT" in ASCII (0x54454154)
	return magic == 0x54454154 && version > 0
}

// serializeProof serializes a PolynomialProof into bytes
func serializeProof(proof PolynomialProof) ([]byte, error) {
	// Calculate total size
	totalSize := 8 + // Heights (2 uint64s)
		64 + // State roots (2 x 32 bytes)
		4 + len(proof.Commitment) + // Commitment with length
		4 + len(proof.AttestationData) + // Attestation with length
		4 + // Degree (uint32)
		4 + len(proof.Metadata) // Metadata with length
	
	// Allocate buffer
	buffer := make([]byte, totalSize)
	offset := 0
	
	// Write heights
	binary.LittleEndian.PutUint64(buffer[offset:offset+8], proof.StartHeight)
	offset += 8
	binary.LittleEndian.PutUint64(buffer[offset:offset+8], proof.EndHeight)
	offset += 8
	
	// Write state roots
	copy(buffer[offset:offset+32], proof.StartStateRoot[:])
	offset += 32
	copy(buffer[offset:offset+32], proof.EndStateRoot[:])
	offset += 32
	
	// Write commitment with length prefix
	binary.LittleEndian.PutUint32(buffer[offset:offset+4], uint32(len(proof.Commitment)))
	offset += 4
	copy(buffer[offset:offset+len(proof.Commitment)], proof.Commitment)
	offset += len(proof.Commitment)
	
	// Write attestation with length prefix
	binary.LittleEndian.PutUint32(buffer[offset:offset+4], uint32(len(proof.AttestationData)))
	offset += 4
	copy(buffer[offset:offset+len(proof.AttestationData)], proof.AttestationData)
	offset += len(proof.AttestationData)
	
	// Write degree
	binary.LittleEndian.PutUint32(buffer[offset:offset+4], proof.Degree)
	offset += 4
	
	// Write metadata with length prefix
	binary.LittleEndian.PutUint32(buffer[offset:offset+4], uint32(len(proof.Metadata)))
	offset += 4
	copy(buffer[offset:], proof.Metadata)
	
	return buffer, nil
}

// deserializeProof deserializes bytes into a PolynomialProof
func deserializeProof(data []byte) (PolynomialProof, error) {
	var proof PolynomialProof
	offset := 0
	
	// Ensure minimum size
	if len(data) < 8+64+4 {
		return proof, fmt.Errorf("data too small for proof: %d bytes", len(data))
	}
	
	// Read heights
	proof.StartHeight = binary.LittleEndian.Uint64(data[offset:offset+8])
	offset += 8
	proof.EndHeight = binary.LittleEndian.Uint64(data[offset:offset+8])
	offset += 8
	
	// Read state roots
	copy(proof.StartStateRoot[:], data[offset:offset+32])
	offset += 32
	copy(proof.EndStateRoot[:], data[offset:offset+32])
	offset += 32
	
	// Read commitment
	if offset+4 > len(data) {
		return proof, fmt.Errorf("data truncated at commitment length")
	}
	commitLen := binary.LittleEndian.Uint32(data[offset:offset+4])
	offset += 4
	
	if commitLen > MaxParamSize || offset+int(commitLen) > len(data) {
		return proof, fmt.Errorf("invalid commitment length: %d", commitLen)
	}
	
	proof.Commitment = make([]byte, commitLen)
	copy(proof.Commitment, data[offset:offset+int(commitLen)])
	offset += int(commitLen)
	
	// Read attestation
	if offset+4 > len(data) {
		return proof, fmt.Errorf("data truncated at attestation length")
	}
	attestLen := binary.LittleEndian.Uint32(data[offset:offset+4])
	offset += 4
	
	if attestLen > MaxParamSize || offset+int(attestLen) > len(data) {
		return proof, fmt.Errorf("invalid attestation length: %d", attestLen)
	}
	
	proof.AttestationData = make([]byte, attestLen)
	copy(proof.AttestationData, data[offset:offset+int(attestLen)])
	offset += int(attestLen)
	
	// Read degree
	if offset+4 > len(data) {
		return proof, fmt.Errorf("data truncated at degree")
	}
	proof.Degree = binary.LittleEndian.Uint32(data[offset:offset+4])
	offset += 4
	
	// Read metadata
	if offset+4 > len(data) {
		return proof, fmt.Errorf("data truncated at metadata length")
	}
	metaLen := binary.LittleEndian.Uint32(data[offset:offset+4])
	offset += 4
	
	if metaLen > MaxParamSize || offset+int(metaLen) > len(data) {
		return proof, fmt.Errorf("invalid metadata length: %d", metaLen)
	}
	
	if metaLen > 0 {
		proof.Metadata = make([]byte, metaLen)
		copy(proof.Metadata, data[offset:offset+int(metaLen)])
	}
	
	return proof, nil
}
