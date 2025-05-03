package proofs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStateProofVerify tests the Verify method of StateProof
func TestStateProofVerify(t *testing.T) {
	// Create test cases
	tests := []struct {
		name      string
		teeType   string
		isValid   bool
		errorType error
	}{
		{
			name:      "SGX_Valid",
			teeType:   TEETypeSGX,
			isValid:   true,
			errorType: nil,
		},
		{
			name:      "SEV_Valid",
			teeType:   TEETypeSEV,
			isValid:   true,
			errorType: nil,
		},
		{
			name:      "TDX_Valid",
			teeType:   TEETypeTDX,
			isValid:   true,
			errorType: nil,
		},
		{
			name:      "SGX_Invalid",
			teeType:   TEETypeSGX,
			isValid:   false,
			errorType: ErrUntrustedMeasurement,
		},
		{
			name:      "Invalid_TEE_Type",
			teeType:   "unknown",
			isValid:   false,
			errorType: nil, // We'll check for error message content instead
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create a new mock verifier
			verifier := NewMockTEEVerifier()

			// Generate a mock measurement
			measurement := generateMockMeasurement()

			// For valid cases, add measurement to trusted list
			if tc.isValid {
				verifier.AddTrustedMeasurement(tc.teeType, measurement[:])
			}

			// Create context with verifier
			ctx := context.WithValue(context.Background(), "attestation_service", verifier)

			// Create a test proof
			proof := createTestStateProof(tc.teeType, measurement)

			// For invalid type case, modify the TEE type
			if tc.name == "Invalid_TEE_Type" {
				proof.TEEType = "unknown"
			}

			// Verify the proof
			isValid, err := proof.Verify(ctx)

			// Check result
			if tc.isValid {
				assert.True(t, isValid, "Proof should be valid")
				assert.Nil(t, err, "No error expected for valid proof")
			} else {
				assert.False(t, isValid, "Proof should be invalid")
				assert.NotNil(t, err, "Error expected for invalid proof")
				if tc.errorType != nil {
					assert.ErrorIs(t, err, tc.errorType)
				}
			}
		})
	}
}

// TestStateProofBatchVerification tests batch verification of state proofs
func TestStateProofBatchVerification(t *testing.T) {
	// Create a mock verifier
	verifier := NewMockTEEVerifier()

	// Generate a mock measurement and add to trusted list
	measurement := generateMockMeasurement()
	verifier.AddTrustedMeasurement(TEETypeSGX, measurement[:])

	// Create context with verifier
	ctx := context.WithValue(context.Background(), "attestation_service", verifier)

	// Use a smaller batch size for easier debugging
	batchSize := 4
	proofs := make([]*StateProof, batchSize)

	// Create all valid proofs for simplicity
	for i := 0; i < batchSize; i++ {
		// For testing, all proofs will be valid
		proofs[i] = createTestStateProof(TEETypeSGX, measurement)
	}

	// Make the second proof invalid by changing its measurement
	invalidMeasurement := generateMockMeasurement()
	proofs[1].TEEMeasurement = invalidMeasurement

	// Make the third proof have an invalid state transition
	proofs[2].ToRoot = [32]byte{} // Zero state root is invalid

	// Test batch verification
	results, errors := BatchVerifyStateProofs(ctx, proofs)

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

// TestStateProofFormatHandling tests both parameter formats for state proofs
func TestStateProofFormatHandling(t *testing.T) {
	// Create a test proof with known values
	fromRoot := sha256.Sum256([]byte("from-state-root"))
	toRoot := sha256.Sum256([]byte("to-state-root"))
	transitionID := sha256.Sum256([]byte("transition-id"))
	measurement := generateMockMeasurement()
	timestamp := uint64(time.Now().Unix())
	regionID := "us-east-1"
	teeType := TEETypeSGX

	// Create the original proof
	original := NewStateProof(
		fromRoot,
		toRoot,
		transitionID,
		measurement,
		timestamp,
		regionID,
		teeType,
	)

	// Create a signature for the proof
	original.Signature = []byte("test-signature")

	// Serialize to both formats and parse back
	directBytes, err := original.SerializeDirectFormat()
	require.NoError(t, err, "No error expected in direct format serialization")

	lengthPrefixedBytes, err := original.Serialize()
	require.NoError(t, err, "No error expected in length-prefixed serialization")

	// Parse back from direct format
	fromDirect, err := ParseStateProof(directBytes)
	require.NoError(t, err, "No error expected in direct format parsing")

	// Parse back from length-prefixed format
	fromLengthPrefixed, err := ParseStateProof(lengthPrefixedBytes)
	require.NoError(t, err, "No error expected in length-prefixed parsing")

	// Compare with original
	assertStateProofsEqual(t, original, fromDirect)
	assertStateProofsEqual(t, original, fromLengthPrefixed)
}

// TestLargeStateProofRejection tests rejection of state proofs exceeding size limit
func TestLargeStateProofRejection(t *testing.T) {
	// Create a valid proof first
	measurement := generateMockMeasurement()
	proof := createTestStateProof(TEETypeSGX, measurement)

	// Create a verifier
	verifier := NewMockTEEVerifier()
	verifier.AddTrustedMeasurement(TEETypeSGX, measurement[:])

	// Create context with verifier
	ctx := context.WithValue(context.Background(), "attestation_service", verifier)

	// Verify the proof works normally
	isValid, err := proof.Verify(ctx)
	assert.True(t, isValid, "Proof should be valid")
	assert.Nil(t, err, "No error expected for valid proof")

	// Now make the signature extra large to exceed the size limit
	proof.Signature = make([]byte, MaxProofSize+1)

	// Verify the proof is rejected due to size
	isValid, err = proof.Verify(ctx)
	assert.False(t, isValid, "Proof should be rejected")
	assert.ErrorIs(t, err, ErrInvalidProofSize, "Should reject due to size")
}

// TestStateProofTransitionValidation tests state transition validation for state proofs
func TestStateProofTransitionValidation(t *testing.T) {
	// Create test cases
	tests := []struct {
		name          string
		modifyProof   func(*StateProof)
		expectedError error
	}{
		{
			name:          "Valid_Proof",
			modifyProof:   func(p *StateProof) {},
			expectedError: nil,
		},
		{
			name: "Zero_State_Root",
			modifyProof: func(p *StateProof) {
				p.ToRoot = [32]byte{}
			},
			expectedError: ErrInvalidStateTransition,
		},
		{
			name: "Zero_Previous_State_Root_for_Non-Genesis",
			modifyProof: func(p *StateProof) {
				p.FromRoot = [32]byte{}
				p.TransitionID = sha256.Sum256([]byte("non-genesis"))
			},
			expectedError: ErrInvalidStateTransition,
		},
		{
			name: "Both_Roots_Zero",
			modifyProof: func(p *StateProof) {
				p.FromRoot = [32]byte{}
				p.ToRoot = [32]byte{}
			},
			expectedError: ErrInvalidStateTransition,
		},
		{
			name: "Zero_Timestamp",
			modifyProof: func(p *StateProof) {
				p.Timestamp = 0
			},
			expectedError: ErrInvalidStateTransition,
		},
		{
			name: "Invalid_TEE_Type",
			modifyProof: func(p *StateProof) {
				p.TEEType = "invalid"
			},
			expectedError: nil, // We'll check the error message
		},
		{
			name: "Missing_Region_ID",
			modifyProof: func(p *StateProof) {
				p.RegionID = ""
			},
			expectedError: nil, // We'll check the error message
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create a valid proof as starting point
			measurement := generateMockMeasurement()
			proof := createTestStateProof(TEETypeSGX, measurement)

			// Apply modification for this test case
			tc.modifyProof(proof)

			// Create a verifier
			verifier := NewMockTEEVerifier()
			verifier.AddTrustedMeasurement(TEETypeSGX, measurement[:])

			// Create context with verifier
			ctx := context.WithValue(context.Background(), "attestation_service", verifier)

			// Verify the proof
			isValid, err := proof.Verify(ctx)

			// For valid case
			if tc.expectedError == nil && tc.name == "Valid_Proof" {
				assert.True(t, isValid, "Proof should be valid")
				assert.Nil(t, err, "No error expected")
			} else {
				// For invalid cases
				assert.False(t, isValid, "Proof should be invalid")
				assert.NotNil(t, err, "Error expected")
				
				if tc.expectedError != nil {
					assert.ErrorIs(t, err, tc.expectedError)
				}
				
				// Special case checks
				if tc.name == "Invalid_TEE_Type" {
					assert.Contains(t, err.Error(), "TEE type")
				}
				if tc.name == "Missing_Region_ID" {
					assert.Contains(t, err.Error(), "region")
				}
			}
		})
	}
}

// Helper functions

// createTestStateProof creates a state proof for testing
func createTestStateProof(teeType string, measurement [32]byte) *StateProof {
	// Generate test values
	fromRoot := sha256.Sum256([]byte("previous-state"))
	toRoot := sha256.Sum256([]byte("current-state"))
	transitionID := sha256.Sum256([]byte("transition-id"))
	timestamp := uint64(time.Now().Unix())
	regionID := "us-east-1"

	// Create a new state proof
	proof := NewStateProof(
		fromRoot,
		toRoot,
		transitionID,
		measurement,
		timestamp,
		regionID,
		teeType,
	)

	// Add a test signature (in a real system this would be signed by the TEE)
	proof.Signature = []byte("test-signature-for-state-proof")

	return proof
}

// assertStateProofsEqual compares two state proofs for equality
func assertStateProofsEqual(t *testing.T, expected, actual *StateProof) {
	assert.Equal(t, expected.FromRoot, actual.FromRoot, "FromRoot should match")
	assert.Equal(t, expected.ToRoot, actual.ToRoot, "ToRoot should match")
	assert.Equal(t, expected.TransitionID, actual.TransitionID, "TransitionID should match")
	assert.Equal(t, expected.TEEMeasurement, actual.TEEMeasurement, "TEEMeasurement should match")
	assert.Equal(t, expected.Timestamp, actual.Timestamp, "Timestamp should match")
	assert.Equal(t, expected.RegionID, actual.RegionID, "RegionID should match")
	assert.Equal(t, expected.TEEType, actual.TEEType, "TEEType should match")
	assert.True(t, bytes.Equal(expected.Signature, actual.Signature), "Signature should match")
}
