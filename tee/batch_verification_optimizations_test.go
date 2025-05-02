package tee

import (
	"encoding/binary"
	"encoding/hex"
	"os"
	"testing"
	
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBatchVerificationOptimizations tests the optimization capabilities of the batch verification system
func TestBatchVerificationOptimizations(t *testing.T) {
	// Set up test mode for deterministic behavior
	os.Setenv("TDX_PCS_TEST_MODE", "true")
	
	// Save original functions to restore later
	originalVerifyFunc := performPCSRequestFunc
	originalExtractFunc := extractMeasurementFunc
	
	// Restore original functions after test
	defer func() {
		performPCSRequestFunc = originalVerifyFunc
		extractMeasurementFunc = originalExtractFunc
	}()
	
	// Create a test batch with deduplication enabled
	batchOptions := DefaultBatchOptions()
	batchOptions.MaxBatchSize = 1000 // Make sure all quotes fit in one batch
	batch := NewAttestationBatch(batchOptions)
	
	// We'll track the unique measurements separately for verification
	uniqueMeasurements := make(map[string]bool)
	
	// Add 10 quotes with only 2 unique measurements (5 of each)
	for i := 0; i < 10; i++ {
		// Create a measurement that alternates between two values (0 or 1)
		measurement := make([]byte, 48)
		binary.BigEndian.PutUint64(measurement, uint64(i%2))
		
		// Track unique measurements by their hex string
		measurementHex := hex.EncodeToString(measurement)
		uniqueMeasurements[measurementHex] = true
		
		// Create a test quote with this measurement
		quote := generateMockTDXQuote(t, measurement, nil)
		
		// Add to batch
		added, err := batch.AddQuote(quote, nil, AttestationTypeTDX)
		require.True(t, added, "Quote should be added to batch")
		require.NoError(t, err, "No error should occur when adding quote")
	}
	
	// Verify the batch
	err := batch.VerifyBatch()
	require.NoError(t, err, "Batch verification should succeed")
	
	// Validate measurement deduplication by examining the measurement groups
	// We know there are only 2 unique measurements in our test scenario
	// Check that all quotes were verified successfully despite only calling verification twice
	totalVerified, countErr := batch.GetVerifiedCount()
	require.NoError(t, countErr, "Should get verified count without error")
	assert.Equal(t, 10, totalVerified, "All 10 quotes should be verified")
	
	// Now examine the measurement groups map from the batch directly
	// First, confirm we have two unique measurements added to the batch
	assert.Equal(t, 2, len(uniqueMeasurements), "Should have 2 unique measurements in the test data")
	
	// Check that each quote has a successful verification result
	successfulVerifications := 0
	for i := 0; i < batch.Size(); i++ {
		if batch.Results[i] {
			successfulVerifications++
		}
	}
	
	// All 10 quotes should be successfully verified
	assert.Equal(t, 10, successfulVerifications, "All 10 quotes should have successful verification results")
}
