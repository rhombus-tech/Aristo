package proofs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockTEEVerifier implements TEEVerifier for testing
type MockTEEVerifier struct {
	// Mutex for thread safety
	mutex sync.Mutex
	
	// Trusted measurements by type
	trustedMeasurements map[string]map[string]bool // map[teeType]map[measurementHex]bool
	
	// Control behavior of verification
	shouldFailMeasurement bool
	shouldFailSignature   bool
	
	// Track verification calls
	measurementVerifications int
	signatureVerifications   int
	
	// For tracking verified measurements in tests
	verifiedMeasurements map[string]bool
}

// NewMockTEEVerifier creates a new mock TEE verifier
func NewMockTEEVerifier() *MockTEEVerifier {
	return &MockTEEVerifier{
		trustedMeasurements: map[string]map[string]bool{
			TEETypeSGX: make(map[string]bool),
			TEETypeSEV: make(map[string]bool),
			TEETypeTDX: make(map[string]bool),
		},
		verifiedMeasurements: make(map[string]bool),
	}
}

// AddTrustedMeasurement adds a measurement to the trusted list
func (m *MockTEEVerifier) AddTrustedMeasurement(teeType string, measurement []byte) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	if _, ok := m.trustedMeasurements[teeType]; !ok {
		m.trustedMeasurements[teeType] = make(map[string]bool)
	}
	m.trustedMeasurements[teeType][string(measurement)] = true
}

// VerifyMeasurement implements TEEVerifier.VerifyMeasurement
func (m *MockTEEVerifier) VerifyMeasurement(ctx context.Context, teeType string, measurement []byte) (bool, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	m.measurementVerifications++
	
	// Record this measurement was verified
	measurementKey := teeType + "-" + string(measurement)
	m.verifiedMeasurements[measurementKey] = true
	
	// Fail if configured to do so
	if m.shouldFailMeasurement {
		return false, nil
	}
	
	// Check if measurement is trusted
	if measurementMap, ok := m.trustedMeasurements[teeType]; ok {
		return measurementMap[string(measurement)], nil
	}
	
	return false, nil
}

// VerifySignature implements TEEVerifier.VerifySignature
func (m *MockTEEVerifier) VerifySignature(ctx context.Context, teeType string, measurement []byte, enclaveID []byte, message []byte, signature []byte) (bool, error) {
	// Need to lock for incrementing counter
	m.mutex.Lock()
	m.signatureVerifications++
	m.mutex.Unlock()
	
	// Fail if configured to do so
	if m.shouldFailSignature {
		return false, nil
	}
	
	// Get the result directly without calling VerifyMeasurement to avoid deadlock
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	if measurementMap, ok := m.trustedMeasurements[teeType]; ok {
		return measurementMap[string(measurement)], nil
	}
	
	return false, nil
}

// TestExecutionProofVerify tests the Verify method of ExecutionProof
func TestExecutionProofVerify(t *testing.T) {
	// Create a mock verifier
	verifier := NewMockTEEVerifier()
	
	// Generate a mock measurement for each TEE type
	sgxMeasurement := generateMockMeasurement()
	sevMeasurement := generateMockMeasurement()
	tdxMeasurement := generateMockMeasurement()
	
	// Add measurements to trusted list
	verifier.AddTrustedMeasurement(TEETypeSGX, sgxMeasurement[:])
	verifier.AddTrustedMeasurement(TEETypeSEV, sevMeasurement[:])
	verifier.AddTrustedMeasurement(TEETypeTDX, tdxMeasurement[:])
	
	// Create context with verifier
	ctx := context.WithValue(context.Background(), ContextKeyAttestationService, verifier)
	
	// Test each TEE type
	tests := []struct {
		name      string
		teeType   string
		measurement [32]byte
		shouldPass bool
	}{
		{"SGX Valid", TEETypeSGX, sgxMeasurement, true},
		{"SEV Valid", TEETypeSEV, sevMeasurement, true},
		{"TDX Valid", TEETypeTDX, tdxMeasurement, true},
		{"SGX Invalid", TEETypeSGX, generateMockMeasurement(), false},
		{"Invalid TEE Type", "invalid", generateMockMeasurement(), false},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a valid proof
			proof := createTestProof(tt.teeType, tt.measurement)
			
			// Verify the proof
			result, err := proof.Verify(ctx)
			
			if tt.shouldPass {
				assert.True(t, result, "Proof should be valid")
				assert.Nil(t, err, "No error should be returned")
			} else {
				assert.False(t, result, "Proof should be invalid")
				assert.NotNil(t, err, "Error should be returned")
			}
		})
	}
}

// TestBatchVerification tests batch verification of proofs
func TestBatchVerification(t *testing.T) {
	// Create a mock verifier
	verifier := NewMockTEEVerifier()
	
	// Generate a mock measurement and add to trusted list
	measurement := generateMockMeasurement()
	verifier.AddTrustedMeasurement(TEETypeSGX, measurement[:])
	
	// Create context with verifier
	ctx := context.WithValue(context.Background(), ContextKeyAttestationService, verifier)
	
	// Use a smaller batch size for easier debugging
	batchSize := 4
	proofs := make([]*ExecutionProof, batchSize)
	
	// Create all valid proofs for simplicity
	for i := 0; i < batchSize; i++ {
		// For testing, all proofs will be valid
		proofs[i] = createTestProof(TEETypeSGX, measurement)
	}
	
	// Make the second proof invalid by changing its measurement
	invalidMeasurement := generateMockMeasurement()
	proofs[1].TEEMeasurement = invalidMeasurement
	
	// Make the third proof have an invalid state transition
	proofs[2].StateRoot = [32]byte{} // Zero state root is invalid
	
	// Test batch verification
	results, errors := BatchVerifyExecutionProofs(ctx, proofs)
	
	// Verify results
	require.Equal(t, batchSize, len(results), "Should return results for all proofs")
	require.Equal(t, batchSize, len(errors), "Should return errors for all proofs")
	
	// First proof should be valid
	assert.True(t, results[0], "First proof should be valid")
	assert.Nil(t, errors[0], "No error for valid proof")
	
	// Second proof should be invalid due to untrusted measurement
	assert.False(t, results[1], "Second proof should be invalid")
	assert.NotNil(t, errors[1], "Error for invalid measurement")
	assert.Contains(t, errors[1].Error(), "measurement", "Error should mention measurement")
	
	// Third proof should be invalid due to state transition
	assert.False(t, results[2], "Third proof should be invalid")
	assert.NotNil(t, errors[2], "Error for invalid state")
	assert.Contains(t, errors[2].Error(), "state", "Error should mention state")
	
	// Fourth proof should be valid
	assert.True(t, results[3], "Fourth proof should be valid")
	assert.Nil(t, errors[3], "No error for valid proof")
	
	// Check verification counts
	// All proofs should have their measurements verified (at least initially)
	assert.Equal(t, batchSize, verifier.measurementVerifications, "Should verify all measurements")
	
	// Signature verification behavior may vary based on the exact implementation
	// The key point is that it's doing verification in parallel and catching all errors
	assert.GreaterOrEqual(t, verifier.signatureVerifications, 2, "Should verify at least the valid signatures")
}

// TestParameterFormatHandling tests both parameter formats
func TestParameterFormatHandling(t *testing.T) {
	// Create a valid proof
	teeType := TEETypeSGX
	measurement := generateMockMeasurement()
	originalProof := createTestProof(teeType, measurement)
	
	// Serialize with direct format
	directBytes, err := originalProof.SerializeDirectFormat()
	require.NoError(t, err, "Failed to serialize in direct format")
	
	// Serialize with length-prefixed format (default)
	lengthPrefixedBytes, err := originalProof.Serialize()
	require.NoError(t, err, "Failed to serialize in length-prefixed format")
	
	// Parse both formats
	directProof, err := ParseExecutionProof(directBytes)
	require.NoError(t, err, "Failed to parse direct format")
	
	lengthPrefixedProof, err := ParseExecutionProof(lengthPrefixedBytes)
	require.NoError(t, err, "Failed to parse length-prefixed format")
	
	// Verify fields match in both parsed proofs
	assertProofsEqual(t, originalProof, directProof)
	assertProofsEqual(t, originalProof, lengthPrefixedProof)
}

// TestLargeProofRejection tests rejection of proofs exceeding size limit
func TestLargeProofRejection(t *testing.T) {
	// Create a mock verifier
	verifier := NewMockTEEVerifier()
	
	// Generate a mock measurement and add to trusted list
	measurement := generateMockMeasurement()
	verifier.AddTrustedMeasurement(TEETypeSGX, measurement[:])
	
	// Create context with verifier
	ctx := context.WithValue(context.Background(), ContextKeyAttestationService, verifier)
	
	// Create a valid proof with extremely large inputs to exceed size limits
	proof := createTestProof(TEETypeSGX, measurement)
	
	// Add a large number of large inputs to exceed size
	largeInput := make([]byte, 1024*1024) // 1MB input
	_, err := rand.Read(largeInput)
	require.NoError(t, err, "Failed to generate random data")
	
	proof.Inputs = [][]byte{largeInput}
	
	// Verify the proof should fail due to size
	result, err := proof.Verify(ctx)
	assert.False(t, result, "Proof should be invalid due to size")
	assert.Equal(t, ErrInvalidProofSize, err, "Should return invalid proof size error")
}

// TestStateTransitionValidation tests state transition validation
func TestStateTransitionValidation(t *testing.T) {
	// Create a mock verifier
	verifier := NewMockTEEVerifier()
	
	// Generate a mock measurement and add to trusted list
	measurement := generateMockMeasurement()
	verifier.AddTrustedMeasurement(TEETypeSGX, measurement[:])
	
	// Create context with verifier
	ctx := context.WithValue(context.Background(), ContextKeyAttestationService, verifier)
	
	tests := []struct {
		name        string
		modifyProof func(*ExecutionProof)
		shouldPass  bool
		errorType   error
	}{
		{
			"Valid Proof",
			func(p *ExecutionProof) {},
			true,
			nil,
		},
		{
			"Zero State Root",
			func(p *ExecutionProof) {
				p.StateRoot = [32]byte{}
			},
			false,
			ErrInvalidExecution,
		},
		{
			"Zero Previous State Root for Non-Genesis",
			func(p *ExecutionProof) {
				p.PrevStateRoot = [32]byte{}
				p.TxID = ids.GenerateTestID()
			},
			false,
			ErrInvalidExecution,
		},
		{
			"Empty Inputs",
			func(p *ExecutionProof) {
				p.Inputs = [][]byte{}
			},
			false,
			ErrInvalidInputs,
		},
		{
			"Empty Outputs",
			func(p *ExecutionProof) {
				p.Outputs = [][]byte{}
			},
			false,
			ErrInvalidInputs,
		},
		{
			"Small Input",
			func(p *ExecutionProof) {
				p.Inputs = [][]byte{{1, 2, 3}} // < 4 bytes
			},
			false,
			ErrInvalidExecution,
		},
		{
			"Small Output",
			func(p *ExecutionProof) {
				p.Outputs = [][]byte{{1, 2, 3}} // < 4 bytes
			},
			false,
			ErrInvalidExecution,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a valid proof
			proof := createTestProof(TEETypeSGX, measurement)
			
			// Apply the modification
			tt.modifyProof(proof)
			
			// Verify the proof
			result, err := proof.Verify(ctx)
			
			if tt.shouldPass {
				assert.True(t, result, "Proof should be valid")
				assert.Nil(t, err, "No error should be returned")
			} else {
				assert.False(t, result, "Proof should be invalid")
				if tt.errorType != nil {
					assert.ErrorIs(t, err, tt.errorType, "Incorrect error type")
				} else {
					assert.NotNil(t, err, "Error should be returned")
				}
			}
		})
	}
}

// Helper functions

// createTestProof creates a proof for testing
func createTestProof(teeType string, measurement [32]byte) *ExecutionProof {
	// Generate random data for fields
	txID := ids.GenerateTestID()
	
	// Create inputs
	inputs := [][]byte{
		{0, 1, 2, 3, 4, 5, 6, 7}, // 8 bytes
		{8, 9, 10, 11, 12, 13, 14, 15}, // 8 bytes
	}
	
	// Create outputs
	outputs := [][]byte{
		{16, 17, 18, 19, 20, 21, 22, 23}, // 8 bytes
		{24, 25, 26, 27, 28, 29, 30, 31}, // 8 bytes
	}
	
	// Generate random state roots
	var stateRoot, prevStateRoot [32]byte
	_, _ = rand.Read(stateRoot[:])
	_, _ = rand.Read(prevStateRoot[:])
	
	// Generate enclaveID and signature
	enclaveID := make([]byte, 16)
	_, _ = rand.Read(enclaveID)
	
	signature := make([]byte, 64)
	_, _ = rand.Read(signature)
	
	// Create proof
	proof := NewExecutionProof(
		txID,
		inputs,
		outputs,
		measurement,
		teeType,
		enclaveID,
		"us-east-1", // regionID
		uint64(time.Now().UnixNano()),
		stateRoot,
		prevStateRoot,
	)
	
	// Calculate hashes (normally done in NewExecutionProof)
	inputsHasher := sha256.New()
	for _, input := range inputs {
		inputsHasher.Write(input)
	}
	
	copy(proof.InputsHash[:], inputsHasher.Sum(nil))
	
	outputsHasher := sha256.New()
	for _, output := range outputs {
		outputsHasher.Write(output)
	}
	
	copy(proof.OutputsHash[:], outputsHasher.Sum(nil))
	
	// Set signature
	proof.Signature = signature
	
	return proof
}

// generateMockMeasurement generates a random 32-byte TEE measurement
func generateMockMeasurement() [32]byte {
	var measurement [32]byte
	_, _ = rand.Read(measurement[:])
	return measurement
}

// assertProofsEqual compares two proofs for equality
func assertProofsEqual(t *testing.T, expected, actual *ExecutionProof) {
	assert.Equal(t, expected.TxID, actual.TxID, "TxID mismatch")
	assert.Equal(t, expected.InputsHash, actual.InputsHash, "InputsHash mismatch")
	assert.Equal(t, expected.OutputsHash, actual.OutputsHash, "OutputsHash mismatch")
	assert.Equal(t, expected.TEEMeasurement, actual.TEEMeasurement, "TEEMeasurement mismatch")
	assert.Equal(t, expected.TEEType, actual.TEEType, "TEEType mismatch")
	assert.Equal(t, expected.RegionID, actual.RegionID, "RegionID mismatch")
	assert.Equal(t, expected.Timestamp, actual.Timestamp, "Timestamp mismatch")
	assert.Equal(t, expected.StateRoot, actual.StateRoot, "StateRoot mismatch")
	assert.Equal(t, expected.PrevStateRoot, actual.PrevStateRoot, "PrevStateRoot mismatch")
	
	// Compare inputs
	assert.Equal(t, len(expected.Inputs), len(actual.Inputs), "Inputs length mismatch")
	for i := range expected.Inputs {
		assert.Equal(t, expected.Inputs[i], actual.Inputs[i], "Input %d mismatch", i)
	}
	
	// Compare outputs
	assert.Equal(t, len(expected.Outputs), len(actual.Outputs), "Outputs length mismatch")
	for i := range expected.Outputs {
		assert.Equal(t, expected.Outputs[i], actual.Outputs[i], "Output %d mismatch", i)
	}
}
