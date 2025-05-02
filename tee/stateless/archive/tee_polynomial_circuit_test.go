// Copyright (C) 2025, Rhombus Technologies. All rights reserved.
// See the file LICENSE for licensing terms.

package archive

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/rhombus-tech/vm/tee/stateless/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock TEE controller endpoint for testing
const testTEEEndpoint = "http://localhost:8080/execute"

// TestTEEPolynomialCircuitEndToEnd tests the complete integration of TEE-backed
// polynomial commitments with the ZK archival system
func TestTEEPolynomialCircuitEndToEnd(t *testing.T) {
	// Skip this test in regular CI/CD as it requires a running TEE controller
	if testing.Short() {
		t.Skip("Skipping TestTEEPolynomialCircuitEndToEnd in short mode")
	}

	// Initialize context
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create mock chain and blocks for testing
	chain := newMockStatelessChain()
	createTestBlocks(ctx, chain, 100)

	// Create the TEE polynomial circuit
	circuit := NewTEEPolynomialCircuit(
		testTEEEndpoint,
		WithMaxBatchSize(50),
		WithAcceleration(true),
	)

	// Test 1: Generate a proof for a range of blocks
	t.Run("GenerateProof", func(t *testing.T) {
		// Get blocks from the chain
		blocks, err := getAllBlocksInRange(ctx, chain, 10, 20)
		require.NoError(t, err)
		require.Len(t, blocks, 11)

		// Get start and end state
		startState := blocks[0].StateRoot()
		endState := blocks[len(blocks)-1].StateRoot()

		// Generate the proof
		proof, err := circuit.GenerateProof(
			ctx,
			blocks,
			startState,
			endState,
			10,
			20,
		)
		require.NoError(t, err)
		require.NotNil(t, proof)
		require.NotEmpty(t, proof)

		// Verify the proof
		verified, err := circuit.VerifyProof(ctx, proof, 10, 20)
		require.NoError(t, err)
		assert.True(t, verified, "Proof verification failed")

		// Test partial verification (subset of the range)
		verified, err = circuit.VerifyProof(ctx, proof, 12, 15)
		require.NoError(t, err)
		assert.True(t, verified, "Partial proof verification failed")
	})

	// Test 2: Generate recursive proofs
	t.Run("GenerateRecursiveProof", func(t *testing.T) {
		// Generate multiple individual proofs
		var proofs [][]byte
		ranges := []struct{ start, end uint64 }{
			{10, 20},
			{21, 30},
			{31, 40},
		}

		for _, r := range ranges {
			blocks, err := getAllBlocksInRange(ctx, chain, r.start, r.end)
			require.NoError(t, err)

			startState := blocks[0].StateRoot()
			endState := blocks[len(blocks)-1].StateRoot()

			proof, err := circuit.GenerateProof(
				ctx,
				blocks,
				startState,
				endState,
				r.start,
				r.end,
			)
			require.NoError(t, err)
			proofs = append(proofs, proof)
		}

		// Generate a recursive proof
		recursiveProof, err := circuit.GenerateRecursiveProof(
			ctx,
			proofs,
			10,
			40,
		)
		require.NoError(t, err)
		require.NotNil(t, recursiveProof)
		require.NotEmpty(t, recursiveProof)

		// Verify the recursive proof
		verified, err := circuit.VerifyRecursiveProof(ctx, recursiveProof, 10, 40)
		require.NoError(t, err)
		assert.True(t, verified, "Recursive proof verification failed")

		// Test partial verification of recursive proof
		verified, err = circuit.VerifyRecursiveProof(ctx, recursiveProof, 15, 35)
		require.NoError(t, err)
		assert.True(t, verified, "Partial recursive proof verification failed")
	})

	// Test 3: Dual-format parameter handling
	t.Run("DualFormatParameterHandling", func(t *testing.T) {
		// Test with length-prefixed format
		testParamValue := []byte("test_parameter")
		lengthPrefixedParam := make([]byte, 4+len(testParamValue))
		binary.LittleEndian.PutUint32(lengthPrefixedParam[:4], uint32(len(testParamValue)))
		copy(lengthPrefixedParam[4:], testParamValue)

		// Parse the parameter
		parsedBytes, format, err := ParseDualFormatParameter(lengthPrefixedParam, true, true)
		require.NoError(t, err)
		assert.Equal(t, "length-prefixed", format)
		assert.Equal(t, testParamValue, parsedBytes)

		// Test with direct format
		directParam := []byte("direct_format_param")
		parsedBytes, format, err = ParseDualFormatParameter(directParam, true, true)
		require.NoError(t, err)
		assert.Equal(t, "direct", format)
		assert.Equal(t, directParam, parsedBytes)

		// Test malicious 3.5B param handling
		maliciousParam := make([]byte, 8)
		binary.LittleEndian.PutUint32(maliciousParam[:4], 3500000000) // 3.5 billion bytes

		// This should be rejected
		parsedBytes, format, err = ParseDualFormatParameter(maliciousParam, true, true)
		require.NoError(t, err) // No error, but falls back to direct format
		assert.Equal(t, "direct", format)
		assert.Equal(t, maliciousParam, parsedBytes) // Treated as direct data
	})
}

// TestTEEPolynomialCircuitMock tests the TEE polynomial circuit with mock TEE responses
func TestTEEPolynomialCircuitMock(t *testing.T) {
	// This test uses mocks for the TEE controller to test the circuit's logic
	// without requiring a real TEE

	// Create mock TEE controller server
	mockTEEServer := createMockTEEServer(t)
	defer mockTEEServer.Close()

	// Create the TEE polynomial circuit with the mock server
	circuit := NewTEEPolynomialCircuit(mockTEEServer.URL)

	// Initialize context - not used in this test but kept for consistency with other tests
	_ = context.Background()

	// Test serialization/deserialization
	t.Run("ProofSerialization", func(t *testing.T) {
		// No need to use ctx here
		// Create a test proof
		originalProof := PolynomialProof{
			StartHeight:     10,
			EndHeight:       20,
			StartStateRoot:  sha256.Sum256([]byte("start")),
			EndStateRoot:    sha256.Sum256([]byte("end")),
			Commitment:      []byte("commitment-data"),
			AttestationData: []byte("attestation-data"),
			Degree:          10,
			Metadata:        []byte("metadata"),
		}

		// Serialize
		serialized, err := serializeProof(originalProof)
		require.NoError(t, err)
		require.NotEmpty(t, serialized)

		// Deserialize
		deserialized, err := deserializeProof(serialized)
		require.NoError(t, err)

		// Compare
		assert.Equal(t, originalProof.StartHeight, deserialized.StartHeight)
		assert.Equal(t, originalProof.EndHeight, deserialized.EndHeight)
		assert.Equal(t, originalProof.StartStateRoot, deserialized.StartStateRoot)
		assert.Equal(t, originalProof.EndStateRoot, deserialized.EndStateRoot)
		assert.Equal(t, originalProof.Commitment, deserialized.Commitment)
		assert.Equal(t, originalProof.AttestationData, deserialized.AttestationData)
		assert.Equal(t, originalProof.Degree, deserialized.Degree)
		assert.Equal(t, originalProof.Metadata, deserialized.Metadata)
	})

	// Test matrix encoding
	t.Run("MatrixEncoding", func(t *testing.T) {
		// Create mock blocks
		blocks := createMockBlocks(5)

		// Encode to matrix
		matrix, err := circuit.encodeBlocksToMatrix(blocks)
		require.NoError(t, err)
		require.NotEmpty(t, matrix)

		// Verify matrix header
		rows := binary.LittleEndian.Uint32(matrix[0:4])
		cols := binary.LittleEndian.Uint32(matrix[4:8])
		assert.Equal(t, uint32(10), rows) // 5 blocks * 2 rows per block
		assert.NotZero(t, cols)

		// Verify matrix size
		expectedSize := 8 + (int(rows) * int(cols) * circuit.fieldElementSize)
		assert.Equal(t, expectedSize, len(matrix))
	})
}

// getAllBlocksInRange gets all blocks in the specified height range
func getAllBlocksInRange(ctx context.Context, chain *mockStatelessChain, startHeight, endHeight uint64) ([]core.StatelessBlock, error) {
	// Get all blocks
	var allBlocks []core.StatelessBlock
	for _, block := range chain.blocks {
		allBlocks = append(allBlocks, block)
	}

	// Filter blocks in range
	var blocksInRange []core.StatelessBlock
	for _, block := range allBlocks {
		height := block.Height()
		if height >= startHeight && height <= endHeight {
			blocksInRange = append(blocksInRange, block)
		}
	}

	// Sort blocks by height
	sortBlocksByHeight(blocksInRange)

	if len(blocksInRange) != int(endHeight-startHeight+1) {
		return nil, fmt.Errorf("missing blocks in range %d-%d, found %d", startHeight, endHeight, len(blocksInRange))
	}

	return blocksInRange, nil
}

// sortBlocksByHeight sorts blocks by height
func sortBlocksByHeight(blocks []core.StatelessBlock) {
	// Use a simple bubble sort for clarity (not performance)
	for i := 0; i < len(blocks); i++ {
		for j := i + 1; j < len(blocks); j++ {
			if blocks[i].Height() > blocks[j].Height() {
				blocks[i], blocks[j] = blocks[j], blocks[i]
			}
		}
	}
}

// createMockBlocks creates mock blocks for testing
func createMockBlocks(count int) []core.StatelessBlock {
	blocks := make([]core.StatelessBlock, count)
	for i := 0; i < count; i++ {
		var stateRoot [32]byte
		for j := 0; j < 32; j++ {
			stateRoot[j] = byte(i * j)
		}

		blocks[i] = &mockStatelessBlock{
			id:        ids.GenerateTestID(),
			parentID:  ids.GenerateTestID(),
			height:    uint64(i),
			timestamp: time.Now().Add(time.Duration(i) * time.Minute),
			stateRoot: stateRoot,
			proofs:    []core.StatelessProof{},
			bytes:     []byte(fmt.Sprintf("block-%d", i)),
		}
	}
	return blocks
}

// TEEResponse represents a mock response from the TEE controller
type TEEResponse struct {
	Result []byte `json:"result"`
	Error  string `json:"error,omitempty"`
}

// createMockTEEServer creates a mock HTTP server that simulates the TEE controller
func createMockTEEServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only accept POST requests
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		// Read and parse the request body
		var payload struct {
			Input     []byte `json:"input"`
			Operation string `json:"operation"`
		}
		
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(TEEResponse{
				Error: fmt.Sprintf("Invalid request: %v", err),
			})
			return
		}

		// Generate appropriate response based on operation
		var response TEEResponse
		switch payload.Operation {
		case OpSecureCommit:
			// Mock secure_commit response:
			// [commitment_size(u32)][commitment][attestation_size(u32)][attestation]
			commitment := make([]byte, 128)
			for i := range commitment {
				commitment[i] = byte(i % 256)
			}
			
			attestation := make([]byte, 64)
			binary.LittleEndian.PutUint32(attestation[0:4], 0x54454154) // "TEAT" magic
			binary.LittleEndian.PutUint32(attestation[4:8], 1)          // Version 1
			
			// Create response buffer
			respBuf := make([]byte, 4 + len(commitment) + 4 + len(attestation))
			binary.LittleEndian.PutUint32(respBuf[0:4], uint32(len(commitment)))
			copy(respBuf[4:4+len(commitment)], commitment)
			
			attestOffset := 4 + len(commitment)
			binary.LittleEndian.PutUint32(respBuf[attestOffset:attestOffset+4], uint32(len(attestation)))
			copy(respBuf[attestOffset+4:], attestation)
			
			response.Result = respBuf
			
		case OpSecureOpenAtPoint:
			// Mock secure_open_at_point response:
			// A single byte: 1 for success, 0 for failure
			response.Result = []byte{1} // Always succeed in tests
			
		default:
			response.Error = fmt.Sprintf("Unknown operation: %s", payload.Operation)
		}

		// Write the response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
}
