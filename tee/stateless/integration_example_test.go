package stateless

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"

	"github.com/rhombus-tech/vm/tee/stateless/chain"
	"github.com/rhombus-tech/vm/tee/stateless/core"
)

// MockStatelessProof implements core.StatelessProof for testing
type MockStatelessProof struct {
	data           []byte
	proofType      string
	rootHash       [sha256.Size]byte
	isLengthPrefixed bool // Whether the proof is already length-prefixed
}

func (m *MockStatelessProof) RootHash() [sha256.Size]byte {
	return m.rootHash
}

func (m *MockStatelessProof) ID() ids.ID {
	hash := sha256.Sum256(m.data)
	var id ids.ID
	copy(id[:], hash[:])
	return id
}

func (m *MockStatelessProof) Serialize() ([]byte, error) {
	if m.isLengthPrefixed {
		return m.data, nil
	}
	return m.data, nil
}

func (m *MockStatelessProof) ProofType() string {
	return m.proofType
}

func (m *MockStatelessProof) Size() uint64 {
	return uint64(len(m.data))
}

func (m *MockStatelessProof) Verify(ctx context.Context) (bool, error) {
	return true, nil
}



// TestDualFormatParameterValidation tests the validateDualFormatParameter function
// TestDualFormatParameter verifies our core.StatelessProof compatibility
func TestDualFormatParameter(t *testing.T) {
	// Verify our mock implements core.StatelessProof interface
	var _ core.StatelessProof = &MockStatelessProof{}
	t.Log("MockStatelessProof correctly implements core.StatelessProof interface")
}

// TestDualFormatParameterValidation tests the validateDualFormatParameter function
func TestDualFormatParameterValidation(t *testing.T) {
	tests := []struct {
		name           string
		inputData      []byte
		expectedValid  bool
		expectedFormat string
		expectError    bool
	}{
		{
			name:           "Empty parameter",
			inputData:      []byte{},
			expectedValid:  false,
			expectedFormat: "",
			expectError:    true,
		},
		{
			name:           "Valid length-prefixed parameter",
			inputData:      createLengthPrefixedData([]byte("test data")),
			expectedValid:  true,
			expectedFormat: "length-prefixed",
			expectError:    false,
		},
		{
			name:           "Incomplete length-prefixed parameter",
			inputData:      append([]byte{10, 0, 0, 0}, []byte("test")...),  // Length 10 but only 4 bytes
			expectedValid:  false,
			expectedFormat: "length-prefixed-incomplete",
			expectError:    true,
		},
		{
			name:           "Unreasonable length parameter",
			inputData:      append([]byte{0xFF, 0xFF, 0xFF, 0x7F}, []byte("test")...),  // Length > 1MB 
			expectedValid:  true,
			expectedFormat: "direct",
			expectError:    false,
		},
		{
			name:           "Direct format (32 bytes)",
			inputData:      bytes.Repeat([]byte{1}, 32),
			expectedValid:  true,
			expectedFormat: "direct",
			expectError:    false,
		},
		{
			name:           "Direct-short format",
			inputData:      []byte("short"),
			expectedValid:  true,
			expectedFormat: "direct", // First 4 bytes "shor" are interpreted as length prefix with unreasonable value
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid, format, err := validateDualFormatParameter(tt.inputData)
			
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			
			assert.Equal(t, tt.expectedValid, valid)
			assert.Equal(t, tt.expectedFormat, format)
		})
	}
}

// TestDualFormatParameterParsing tests the parseDualFormatParameter function
func TestDualFormatParameterParsing(t *testing.T) {
	testData := []byte("test parameter data")
	
	// Create length-prefixed version
	lengthPrefixed := createLengthPrefixedData(testData)
	
	// Test parsing length-prefixed format
	t.Run("Parse length-prefixed parameter", func(t *testing.T) {
		parsed, format, err := parseDualFormatParameter(lengthPrefixed)
		require.NoError(t, err)
		assert.Equal(t, "length-prefixed", format)
		assert.Equal(t, testData, parsed)
	})
	
	// Test parsing direct format
	t.Run("Parse direct parameter", func(t *testing.T) {
		parsed, format, err := parseDualFormatParameter(testData)
		require.NoError(t, err)
		assert.Equal(t, "direct", format)
		assert.Equal(t, testData, parsed)
	})
}

// TestSerializeDualFormatData tests the serializeDualFormatData function
func TestSerializeDualFormatData(t *testing.T) {
	testData := []byte("test parameter data")
	
	// Test serializing raw data
	t.Run("Serialize direct data", func(t *testing.T) {
		serialized, err := serializeDualFormatData(testData)
		require.NoError(t, err)
		
		// Verify it's properly length-prefixed
		require.True(t, len(serialized) >= 4)
		length := binary.LittleEndian.Uint32(serialized[:4])
		assert.Equal(t, uint32(len(testData)), length)
		assert.Equal(t, testData, serialized[4:4+length])
	})
	
	// Test serializing already length-prefixed data
	t.Run("Serialize already length-prefixed data", func(t *testing.T) {
		lengthPrefixed := createLengthPrefixedData(testData)
		serialized, err := serializeDualFormatData(lengthPrefixed)
		require.NoError(t, err)
		
		// Should return the same data without double-wrapping
		assert.Equal(t, lengthPrefixed, serialized)
	})
	
	// Test serializing empty data
	t.Run("Serialize empty data", func(t *testing.T) {
		_, err := serializeDualFormatData([]byte{})
		assert.Error(t, err)
	})
}

// TestSerializeProof tests the serializeProof function
func TestSerializeProof(t *testing.T) {
	testData := []byte("test proof data")
	
	// Test proof that's not length-prefixed
	t.Run("Serialize non-prefixed proof", func(t *testing.T) {
		mockProof := &MockStatelessProof{
			data:      testData,
			proofType: "test",
			isLengthPrefixed: false,
		}
		
		serialized, err := serializeProof(mockProof)
		require.NoError(t, err)
		
		// Verify it's properly length-prefixed
		require.True(t, len(serialized) >= 4)
		length := binary.LittleEndian.Uint32(serialized[:4])
		assert.Equal(t, uint32(len(testData)), length)
		assert.Equal(t, testData, serialized[4:4+length])
	})
	
	// Test proof that's already length-prefixed
	t.Run("Serialize already prefixed proof", func(t *testing.T) {
		prefixedData := createLengthPrefixedData(testData)
		mockProof := &MockStatelessProof{
			data:      prefixedData,
			proofType: "test",
			isLengthPrefixed: true,
		}
		
		serialized, err := serializeProof(mockProof)
		require.NoError(t, err)
		
		// Should return the same data without double-wrapping
		assert.Equal(t, prefixedData, serialized)
	})
}

// TestWasmlanczeIntegrationScenarios simulates real-world WebAssembly contract scenarios
func TestWasmlanczeIntegrationScenarios(t *testing.T) {
	// Test real-world integration scenarios
	// Test scenario that simulates the 3.5 billion byte length bug from Wasmlanche
	t.Run("Huge length prefix detection", func(t *testing.T) {
		// Create a parameter with an unreasonable length prefix that would crash without validation
		// This directly simulates the bug mentioned in our previous work with WebAssembly contracts
		hugeLength := uint32(3_500_000_000) // 3.5 billion - the same size that caused contract panics
		badData := make([]byte, 8) // Just enough data to hold the prefix and some content
		binary.LittleEndian.PutUint32(badData[:4], hugeLength)
		
		// This should detect the unreasonable length and handle it as direct format
		valid, format, err := validateDualFormatParameter(badData)
		assert.True(t, valid) // Should be valid as direct format
		assert.Equal(t, "direct", format)
		assert.NoError(t, err)
		
		// When parsing, should also handle it correctly
		parsed, format, err := parseDualFormatParameter(badData)
		assert.NoError(t, err)
		assert.Equal(t, badData, parsed) // Should return the raw data
		assert.Equal(t, "direct", format)
	})
	
	// Test a contract ID direct format parameter (common in Go tests)
	t.Run("Contract ID direct format", func(t *testing.T) {
		contractID := bytes.Repeat([]byte{0x01}, 32) // 32-byte contract ID
		
		valid, format, err := validateDualFormatParameter(contractID)
		assert.True(t, valid)
		assert.Equal(t, "direct", format)
		assert.NoError(t, err)
	})
}

// Helper functions
func createLengthPrefixedData(data []byte) []byte {
	result := make([]byte, 4+len(data))
	binary.LittleEndian.PutUint32(result[:4], uint32(len(data)))
	copy(result[4:], data)
	return result
}

// TestBlockCreationAndVerification tests creating and verifying blocks
func TestBlockCreationAndVerification(t *testing.T) {
	// Create a mock setup
	_ = context.Background() // Not used in this test but would be in real implementation
	
	// Create these but don't use them until we need real integration tests
	_ = &MockStatelessChain{}
	_ = &MockWitnessGenerator{}
	
	// Create mock proofs
	proofs := createMockProofs(3)
	
	// Create a state root
	stateRoot := [sha256.Size]byte{}
	copy(stateRoot[:], []byte("test-state-root"))
	
	// Test new block creation
	t.Run("Block Creation", func(t *testing.T) {
		// Create a new block
		parentID := ids.Empty
		height := uint64(1)
		timestamp := time.Now()
		
		block, err := chain.NewStatelessBlock(
			parentID,
			height,
			timestamp,
			proofs,
			stateRoot,
		)
		
		// Verify block creation
		require.NoError(t, err)
		require.NotNil(t, block)
		
		// Verify block properties
		assert.Equal(t, parentID, block.ParentID())
		assert.Equal(t, height, block.Height())
		assert.Equal(t, stateRoot, block.StateRoot())
		
		// Verify proofs in block
		blockProofs := block.Proofs()
		assert.Equal(t, len(proofs), len(blockProofs))
	})
	
	// Test parameter serialization in block proofs
	t.Run("Block Proof Parameter Serialization", func(t *testing.T) {
		// This test focuses on ensuring proof parameters are properly serialized in blocks
		// Create a block with proofs that have both formats (direct and length-prefixed)
		mixedProofs := []core.StatelessProof{
			// Direct format proof
			&MockStatelessProof{
				data:            []byte("direct-format-proof"),
				proofType:       "test-direct",
				rootHash:        stateRoot,
				isLengthPrefixed: false,
			},
			// Length-prefixed proof
			&MockStatelessProof{
				data:            createLengthPrefixedData([]byte("length-prefixed-proof")),
				proofType:       "test-prefixed",
				rootHash:        stateRoot,
				isLengthPrefixed: true,
			},
		}
		
		// Create a block with mixed format proofs
		block, err := chain.NewStatelessBlock(
			ids.Empty,
			1,
			time.Now(),
			mixedProofs,
			stateRoot,
		)
		require.NoError(t, err)
		
		// Serialize the block
		blockBytes, err := block.Bytes()
		require.NoError(t, err)
		
		// Block serialization should succeed
		assert.NotEmpty(t, blockBytes)
	})
}

// TestChainOperations tests the stateless chain operations
func TestChainOperations(t *testing.T) {
	ctx := context.Background()
	
	// Create a mock chain implementation
	chainImpl := &MockStatelessChain{}
	
	// Test chain growth
	t.Run("Chain Growth", func(t *testing.T) {
		// Initial height should be 0
		height, err := chainImpl.GetHeight(ctx)
		require.NoError(t, err)
		assert.Equal(t, uint64(0), height)
		
		// Create and add blocks sequentially
		parentID := ids.Empty
		var lastBlockID ids.ID
		
		// Add 5 blocks to the chain
		for i := uint64(1); i <= 5; i++ {
			// Create a block
			stateRoot := [sha256.Size]byte{}
			copy(stateRoot[:], []byte(fmt.Sprintf("state-root-%d", i)))
			
			// Create proofs for this block
			proofs := createMockProofs(2)
			
			// Create the block
			block, err := chain.NewStatelessBlock(
				parentID,
				i,
				time.Now(),
				proofs,
				stateRoot,
			)
			require.NoError(t, err)
			
			// Add block to chain
			err = chainImpl.AddBlock(ctx, block)
			require.NoError(t, err)
			
			// Update parent for next block
			parentID = block.ID()
			lastBlockID = block.ID()
		}
		
		// Verify chain height
		height, err = chainImpl.GetHeight(ctx)
		require.NoError(t, err)
		assert.Equal(t, uint64(5), height)
		
		// Test block retrieval by ID
		block, err := chainImpl.GetBlock(ctx, lastBlockID)
		require.NoError(t, err)
		assert.NotNil(t, block)
		assert.Equal(t, uint64(5), block.Height())
	})
}

// TestProofVerification tests verifying different types of proofs
func TestProofVerification(t *testing.T) {
	// Context is used in actual validation but not in our mock test
	_ = context.Background()
	
	// Test the core validateDualFormatParameter function with proofs
	t.Run("Proof Parameter Validation", func(t *testing.T) {
		// Create a length-prefixed proof parameter
		lengthPrefixedParam := createLengthPrefixedData([]byte("proof-data"))
		
		// Validate the parameter
		valid, format, err := validateDualFormatParameter(lengthPrefixedParam)
		require.NoError(t, err)
		assert.True(t, valid)
		assert.Equal(t, "length-prefixed", format)
		
		// Test parsing the parameter
		parsedData, format, err := parseDualFormatParameter(lengthPrefixedParam)
		require.NoError(t, err)
		assert.Equal(t, "length-prefixed", format)
		assert.Equal(t, []byte("proof-data"), parsedData)
	})
	
	// Test Web Assembly integration with the problematic 3.5B parameter
	t.Run("Wasmlanche Parameter Safety", func(t *testing.T) {
		// Create a badly formatted parameter with excessive length
		// as seen in the WebAssembly contract bug
		excessiveParam := make([]byte, 8) // 4 bytes for length + 4 bytes content
		// Set length to 3.5 billion (would crash without validation)
		binary.LittleEndian.PutUint32(excessiveParam[:4], 3_500_000_000)
		// Add some content
		copy(excessiveParam[4:], []byte("data"))
		
		// This should be caught by our validation
		valid, format, err := validateDualFormatParameter(excessiveParam)
		assert.True(t, valid) // It's valid but as direct format
		assert.Equal(t, "direct", format) // Should be treated as direct format
		assert.NoError(t, err)
		
		// When creating a parameter for a WebAssembly contract, we should properly format
		safeParam, err := serializeDualFormatData([]byte("safe-contract-param"))
		require.NoError(t, err)
		
		// Verify it's properly length-prefixed
		assert.True(t, len(safeParam) > 4)
		length := binary.LittleEndian.Uint32(safeParam[:4])
		// Get the actual length of the original parameter
		expectedLength := uint32(len([]byte("safe-contract-param")))
		assert.Equal(t, expectedLength, length)
	})
}

// TestFullStatelessVerificationFlow tests the complete verification flow
func TestFullStatelessVerificationFlow(t *testing.T) {
	// This is a simplified version focusing just on parameter handling
	// Full integration testing would require actual TEE hardware
	t.Skip("Integration test requires real infrastructure - use when in TEE environment")
	
	// If running in an environment with all required infrastructure:
	/*
	ctx := context.Background()
	
	// Create a verification layer with real components
	layer, err := SetupStatelessVerificationLayer()
	require.NoError(t, err)
	
	// Generate real proofs
	generator := layer.GetWitnessGenerator()
	prevStateRoot := [32]byte{} // Genesis root
	newStateRoot := [32]byte{}
	copy(newStateRoot[:], []byte("new-state-root"))
	
	// Generate a state witness (proof)
	proof, err := generator.GenerateStateWitness(ctx, prevStateRoot, newStateRoot)
	require.NoError(t, err)
	require.NotNil(t, proof)
	
	// Create and add a block with the proof
	blockID, err := layer.CreateAndAddBlock(ctx, []core.StatelessProof{proof}, newStateRoot)
	require.NoError(t, err)
	assert.NotEqual(t, ids.Empty, blockID)
	
	// Verify the block can be retrieved
	block, err := layer.GetBlockByID(ctx, blockID)
	require.NoError(t, err)
	assert.NotNil(t, block)
	
	// Verify block contents
	assert.Equal(t, newStateRoot, block.StateRoot())
	assert.Equal(t, uint64(1), block.Height())
	
	// Verify mesh network received the block notification
	// This would need more complex test infrastructure
	*/
}

// MockStatelessChain mocks the StatelessChain interface for testing
type MockStatelessChain struct {
	blocks map[ids.ID]core.StatelessBlock
	height uint64
}

func (m *MockStatelessChain) AddBlock(ctx context.Context, block core.StatelessBlock) error {
	if m.blocks == nil {
		m.blocks = make(map[ids.ID]core.StatelessBlock)
	}
	m.blocks[block.ID()] = block
	if block.Height() > m.height {
		m.height = block.Height()
	}
	return nil
}

func (m *MockStatelessChain) GetBlock(ctx context.Context, id ids.ID) (core.StatelessBlock, error) {
	block, exists := m.blocks[id]
	if !exists {
		return nil, fmt.Errorf("block not found")
	}
	return block, nil
}

func (m *MockStatelessChain) GetHeight(ctx context.Context) (uint64, error) {
	return m.height, nil
}

func (m *MockStatelessChain) GetLatestStateRoot(ctx context.Context) ([sha256.Size]byte, error) {
	return [sha256.Size]byte{}, nil
}

func (m *MockStatelessChain) VerifyChain(ctx context.Context) (bool, error) {
	return true, nil
}

// MockWitnessGenerator mocks the WitnessGenerator interface for testing
type MockWitnessGenerator struct {}

func (m *MockWitnessGenerator) GenerateStateWitness(ctx context.Context, from, to [sha256.Size]byte) (core.StatelessProof, error) {
	return &MockStatelessProof{
		data:      []byte("mock-state-witness"),
		proofType: "state",
		rootHash:  to,
	}, nil
}

func (m *MockWitnessGenerator) GenerateExecutionWitness(ctx context.Context, txID ids.ID, inputs, outputs [][]byte) (core.StatelessProof, error) {
	return &MockStatelessProof{
		data:      []byte("mock-execution-witness"),
		proofType: "execution",
		rootHash:  [sha256.Size]byte{},
	}, nil
}

func (m *MockWitnessGenerator) GenerateAttestationWitness(ctx context.Context, attestation []byte, stateRoot [sha256.Size]byte) (core.StatelessProof, error) {
	return &MockStatelessProof{
		data:      []byte("mock-attestation-witness"),
		proofType: "attestation",
		rootHash:  stateRoot,
	}, nil
}

func (m *MockWitnessGenerator) Close() error {
	return nil
}

// Helper function to create mock proofs for testing
func createMockProofs(count int) []core.StatelessProof {
	var proofs []core.StatelessProof
	for i := 0; i < count; i++ {
		proofs = append(proofs, &MockStatelessProof{
			data:      []byte(fmt.Sprintf("mock-proof-%d", i)),
			proofType: "mock",
			rootHash:  [sha256.Size]byte{},
		})
	}
	return proofs
}
