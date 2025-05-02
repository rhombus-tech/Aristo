// batch_verification_test.go - Tests for compressed batch verification
package tee

import (
	"crypto/rand"
	"encoding/binary"
	"net/http"
	"os"
	"testing"
	"time"
	
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper to generate mock TDX quotes for testing
func generateMockTDXQuote(t *testing.T, measurement []byte, reportData []byte) []byte {
	require.NotNil(t, measurement, "Measurement must not be nil")
	require.Len(t, measurement, 48, "TDX measurement must be 48 bytes")
	
	// Structure based on TDX quote format
	buffer := make([]byte, 512)
	
	// TDX quote header (simplified for testing)
	binary.BigEndian.PutUint32(buffer[0:4], 1) // Version
	binary.BigEndian.PutUint32(buffer[4:8], AttestationTypeTDX) // Type
	
	// Add mock measurement data at expected position
	copy(buffer[64:112], measurement)
	
	// Add report data if provided
	if reportData != nil {
		copy(buffer[200:264], reportData)
	}
	
	// Fill remainder with pseudo-random data
	rand.Read(buffer[264:])
	
	return buffer
}

// We're using the extractMeasurementFunc from batch_verification.go

// Helper to mock PCS verification for tests
func mockPerformPCSRequest(client *http.Client, requestBody []byte, apiKey string) (*PCSVerificationResponse, error) {
	// Create a quote from the request body (simplified for test)
	quote := requestBody
	// Extract measurement from mock quote - for tests this is part of the mock quote
	measurement := quote[64:112]
	
	// Create mock response with the measurement
	resp := &PCSVerificationResponse{
		Version:     "4.0",
		RequestID:   "test-request-id",
		Timestamp:   time.Now().Format(time.RFC3339),
		QuoteStatus: "OK",
		Result: PCSVerificationResult{
			Code:    0,
			Message: "OK",
		},
		TCBInfo: PCSTCBInfo{
			TCBStatus: "UpToDate",
			TCBDate:   time.Now().Format("2006-01-02"),
		},
		QuoteReport: PCSQuoteReport{
			HeaderInfo: PCSHeaderInfo{
				Version:         1,
				AttestationType: 0,
				TEEType:         "TDX",
			},
			TDReport: PCSTDReport{
				MRTD:        encodeHex(measurement),
				MRCONFIGID:  encodeHex([]byte{0x01, 0x02, 0x03, 0x04}),
				MROWNER:     encodeHex([]byte{0x05, 0x06, 0x07, 0x08}),
				RTMR0:       encodeHex([]byte{0x0a, 0x0b, 0x0c, 0x0d}),
				ReportData:  encodeHex(quote[200:264]),
				TEEType:     "TDX",
			},
		},
	}
	
	return resp, nil
}

// Helper to encode bytes as hex string
func encodeHex(data []byte) string {
	result := make([]byte, len(data)*2)
	const hextable = "0123456789abcdef"
	for i, b := range data {
		result[i*2] = hextable[b>>4]
		result[i*2+1] = hextable[b&0x0f]
	}
	return string(result)
}

func TestAttestationBatch(t *testing.T) {
	// Set up test mode
	os.Setenv("TDX_PCS_TEST_MODE", "true")
	
	// Use a mock verification function for testing
	originalVerifyFunc := performPCSRequestFunc
	performPCSRequestFunc = mockPerformPCSRequest
	
	// Save and restore original measurement extraction function
	originalExtractFunc := extractMeasurementFunc
	defer func() {
		performPCSRequestFunc = originalVerifyFunc
		extractMeasurementFunc = originalExtractFunc
	}()
	
	t.Run("BatchCreation", func(t *testing.T) {
		// Create batch with default options
		batch := NewAttestationBatch(nil)
		assert.NotNil(t, batch, "Should create batch with default options")
		if batch.Options.MaxBatchSize != DefaultMaxBatchSize {
			t.Fatalf("Expected MaxBatchSize to be %d, got %d", DefaultMaxBatchSize, batch.Options.MaxBatchSize)
		}
	
		// Customize options
		customOptions := &BatchOptions{
			MaxBatchSize: 123,
		}
	
		customBatch := NewAttestationBatch(customOptions)
		if customBatch.Options.MaxBatchSize != 123 {
			t.Errorf("Expected MaxBatchSize to be 123, got %d", customBatch.Options.MaxBatchSize)
		}
	})
	
	t.Run("AddQuote", func(t *testing.T) {
		batch := NewAttestationBatch(DefaultBatchOptions())
		
		// Create sample measurement
		measurement := make([]byte, 48)
		for i := range measurement {
			measurement[i] = byte(i % 256)
		}
		
		// Add a valid quote
		quote := generateMockTDXQuote(t, measurement, nil)
		added, err := batch.AddQuote(quote, nil, AttestationTypeTDX)
		assert.True(t, added, "Should add valid quote")
		assert.NoError(t, err, "Should not return error for valid quote")
		assert.Equal(t, 1, batch.Size(), "Batch should have 1 quote")
		
		// Try adding invalid quote (too small)
		added, err = batch.AddQuote([]byte{1, 2, 3}, nil, AttestationTypeTDX)
		assert.False(t, added, "Should not add invalid quote")
		assert.Error(t, err, "Should return error for invalid quote")
		assert.Equal(t, 1, batch.Size(), "Batch should still have 1 quote")
	})
	
	t.Run("VerifyBatch", func(t *testing.T) {
		batch := NewAttestationBatch(DefaultBatchOptions())
		
		// Create 5 quotes with same measurement
		measurement := make([]byte, 48)
		rand.Read(measurement)
		
		for i := 0; i < 5; i++ {
			reportData := make([]byte, 64)
			binary.BigEndian.PutUint64(reportData, uint64(i))
			
			quote := generateMockTDXQuote(t, measurement, reportData)
			added, err := batch.AddQuote(quote, nil, AttestationTypeTDX)
			require.True(t, added, "Should add quote")
			require.NoError(t, err, "Should not return error")
		}
		
		// Verify the batch
		err := batch.VerifyBatch()
		assert.NoError(t, err, "Should verify batch without error")
		
		// Check results
		assert.Len(t, batch.Results, 5, "Should have 5 results")
		assert.Len(t, batch.ResponseData, 5, "Should have 5 response data")
		
		for i := 0; i < 5; i++ {
			assert.True(t, batch.Results[i], "Quote %d should be verified", i)
			assert.NotNil(t, batch.ResponseData[i], "Response %d should not be nil", i)
		}
	})
	
	t.Run("GetVerifiedMeasurements", func(t *testing.T) {
		batch := NewAttestationBatch(DefaultBatchOptions())
		
		// Create 3 quotes with same measurement - using deterministic pattern
		measurement := make([]byte, 48)
		for i := 0; i < 48; i++ {
			measurement[i] = byte(i % 256)
		}
		
		// Register a mock for measurement extraction to ensure consistent behavior
		oldExtractFunc := extractMeasurementFunc
		extractMeasurementFunc = func(quoteBytes []byte) ([]byte, error) {
			return measurement, nil
		}
		// Restore the original function when the test is done
		defer func() { extractMeasurementFunc = oldExtractFunc }()
		
		// Add quotes to batch
		for i := 0; i < 3; i++ {
			quote := generateMockTDXQuote(t, measurement, nil)
			added, err := batch.AddQuote(quote, nil, AttestationTypeTDX)
			require.True(t, added, "Should add quote")
			require.NoError(t, err, "Should not return error")
		}
		
		// Verify the batch
		err := batch.VerifyBatch()
		require.NoError(t, err, "Should verify batch without error")
		
		// Get measurements
		measurements, err := batch.GetVerifiedMeasurements()
		assert.NoError(t, err, "Should get measurements without error")
		assert.Len(t, measurements, 3, "Should have 3 measurements")
		
		// Check all measurements match
		for i, m := range measurements {
			assert.Equal(t, measurement, m, "Measurement %d should match", i)
		}
	})
	
	t.Run("GetCompressedWitness", func(t *testing.T) {
		batch := NewAttestationBatch(DefaultBatchOptions())
		
		// Create 10 quotes with same measurement
		measurement := make([]byte, 48)
		rand.Read(measurement)
		
		quotes := make([][]byte, 10)
		for i := 0; i < 10; i++ {
			reportData := make([]byte, 64)
			binary.BigEndian.PutUint64(reportData, uint64(i))
			
			quote := generateMockTDXQuote(t, measurement, reportData)
			quotes[i] = quote
			
			added, err := batch.AddQuote(quote, nil, AttestationTypeTDX)
			require.True(t, added, "Should add quote")
			require.NoError(t, err, "Should not return error")
		}
		
		// Verify the batch
		err := batch.VerifyBatch()
		require.NoError(t, err, "Should verify batch without error")
		
		// Get compressed witness
		witness, err := batch.GetCompressedWitness()
		assert.NoError(t, err, "Should create compressed witness")
		assert.NotNil(t, witness, "Witness should not be nil")
		
		// Witness should be smaller than combined quotes
		totalQuoteSize := 0
		for _, q := range quotes {
			totalQuoteSize += len(q)
		}
		assert.Less(t, len(witness), totalQuoteSize, "Witness should be smaller than combined quotes")
		
		// Verify the witness
		valid, err := VerifyCompressedWitness(witness, quotes)
		assert.NoError(t, err, "Should verify witness without error")
		assert.True(t, valid, "Witness should be valid")
	})
	
	t.Run("BatchVerificationError", func(t *testing.T) {
		// Create a batch error
		batchError := &BatchVerificationError{
			BatchSize:     5,
			FailedIndices: []int{1, 3},
			Reasons:       []string{"error1", "error2"},
		}
		
		// Error message should include stats
		errorMsg := batchError.Error()
		assert.Contains(t, errorMsg, "2 of 5", "Error message should include failure stats")
	})
	
	t.Run("MerkleRoot", func(t *testing.T) {
		// For deterministic testing with pseudo-random data, we need fixed seed
		// Create quotes with fixed data for predictable Merkle roots
		fixedQuotes := make([][]byte, 3)
		
		// Create 3 quotes with identical content to ensure merkle roots match
		for i := 0; i < 3; i++ {
			measurement := make([]byte, 48)
			// Fill with a predictable pattern
			for j := 0; j < 48; j++ {
				measurement[j] = byte((i * j) % 256)
			}
			
			// Create identical quotes for each batch to ensure merkle roots match
			fixedQuotes[i] = make([]byte, 512)
			// Add recognizable header
			binary.BigEndian.PutUint32(fixedQuotes[i][0:4], 1) // Version
			binary.BigEndian.PutUint32(fixedQuotes[i][4:8], AttestationTypeTDX) // Type
			
			// Add measurement at fixed position
			copy(fixedQuotes[i][64:112], measurement)
		}
		
		// First batch
		batch1 := NewAttestationBatch(DefaultBatchOptions())
		for i := 0; i < 3; i++ {
			batch1.AddQuote(fixedQuotes[i], nil, AttestationTypeTDX)
		}
		
		// Compute Merkle root for first batch
		err := batch1.computeMerkleRoot()
		assert.NoError(t, err, "Should compute merkle root")
		assert.NotNil(t, batch1.merkleRoot, "Merkle root should not be nil")
		assert.Len(t, batch1.merkleRoot, 32, "Merkle root should be 32 bytes")
		
		// Second batch with same fixed quotes
		batch2 := NewAttestationBatch(DefaultBatchOptions())
		for i := 0; i < 3; i++ {
			batch2.AddQuote(fixedQuotes[i], nil, AttestationTypeTDX)
		}
		
		// Compute Merkle root for second batch
		err = batch2.computeMerkleRoot()
		assert.NoError(t, err, "Should compute merkle root for second batch")
		
		// Since we're using identical quote data, merkle roots must match
		assert.Equal(t, batch1.merkleRoot, batch2.merkleRoot, "Merkle roots should match for identical batches")
	})
	
	t.Run("BatchOptions", func(t *testing.T) {
		// Test default options
		defaults := DefaultBatchOptions()
		if defaults.MaxBatchSize != DefaultMaxBatchSize {
			t.Errorf("Expected default MaxBatchSize to be %d, got %d", DefaultMaxBatchSize, defaults.MaxBatchSize)
		}
		if !defaults.ReuseMerkleRoots {
			t.Errorf("Expected default ReuseMerkleRoots to be true")
		}
		
		// Test AI trading options
		aiOptions := AITradingBatchOptions()
		if aiOptions.MaxBatchSize != AITradingMaxBatch {
			t.Errorf("Expected AI trading MaxBatchSize to be %d, got %d", AITradingMaxBatch, aiOptions.MaxBatchSize)
		}
		if aiOptions.VerificationMode != "fast" {
			t.Errorf("Expected AI trading VerificationMode to be 'fast', got '%s'", aiOptions.VerificationMode)
		}
		if !aiOptions.MeasurementCache {
			t.Errorf("Expected AI trading MeasurementCache to be true")
		}
		
		// Verify that defaults don't affect each other
		defaults = DefaultBatchOptions()
		if defaults.MaxBatchSize != DefaultMaxBatchSize {
			t.Errorf("Expected default MaxBatchSize to be %d, got %d", DefaultMaxBatchSize, defaults.MaxBatchSize)
		}
		if !defaults.ReuseMerkleRoots {
			t.Errorf("Expected default ReuseMerkleRoots to be true")
		}
		
		aiOptions = AITradingBatchOptions()
		if aiOptions.MaxBatchSize != AITradingMaxBatch {
			t.Errorf("Expected AI trading MaxBatchSize to be %d, got %d", AITradingMaxBatch, aiOptions.MaxBatchSize)
		}
		if aiOptions.VerificationMode != "fast" {
			t.Errorf("Expected AI trading VerificationMode to be 'fast', got '%s'", aiOptions.VerificationMode)
		}
		if !aiOptions.MeasurementCache {
			t.Errorf("Expected AI trading MeasurementCache to be true")
		}
		
		// Custom options test
		options := &BatchOptions{
			MaxBatchSize: 42,
		}
		if options.MaxBatchSize != 42 {
			t.Errorf("Expected MaxBatchSize to be 42, got %d", options.MaxBatchSize)
		}
	})
	
	t.Run("BatchOptions", func(t *testing.T) {
		// Test default options
		defaults := DefaultBatchOptions()
		if defaults.MaxBatchSize != DefaultMaxBatchSize {
			t.Errorf("Expected default MaxBatchSize to be %d, got %d", DefaultMaxBatchSize, defaults.MaxBatchSize)
		}
		if !defaults.ReuseMerkleRoots {
			t.Errorf("Expected default ReuseMerkleRoots to be true")
		}
		
		// Test AI trading options
		aiOptions := AITradingBatchOptions()
		if aiOptions.MaxBatchSize != AITradingMaxBatch {
			t.Errorf("Expected AI trading MaxBatchSize to be %d, got %d", AITradingMaxBatch, aiOptions.MaxBatchSize)
		}
		if aiOptions.VerificationMode != "fast" {
			t.Errorf("Expected AI trading VerificationMode to be 'fast', got '%s'", aiOptions.VerificationMode)
		}
		if !aiOptions.MeasurementCache {
			t.Errorf("Expected AI trading MeasurementCache to be true")
		}
		
		// Verify that defaults don't affect each other
		defaults = DefaultBatchOptions()
		if defaults.MaxBatchSize != DefaultMaxBatchSize {
			t.Errorf("Expected default MaxBatchSize to be %d, got %d", DefaultMaxBatchSize, defaults.MaxBatchSize)
		}
		if !defaults.ReuseMerkleRoots {
			t.Errorf("Expected default ReuseMerkleRoots to be true")
		}
		
		aiOptions = AITradingBatchOptions()
		if aiOptions.MaxBatchSize != AITradingMaxBatch {
			t.Errorf("Expected AI trading MaxBatchSize to be %d, got %d", AITradingMaxBatch, aiOptions.MaxBatchSize)
		}
		if aiOptions.VerificationMode != "fast" {
			t.Errorf("Expected AI trading VerificationMode to be 'fast', got '%s'", aiOptions.VerificationMode)
		}
		if !aiOptions.MeasurementCache {
			t.Errorf("Expected AI trading MeasurementCache to be true")
		}
		
		// Custom options test
		options := &BatchOptions{
			MaxBatchSize: 42,
		}
		if options.MaxBatchSize != 42 {
			t.Errorf("Expected MaxBatchSize to be 42, got %d", options.MaxBatchSize)
		}
	})
	
	t.Run("CompressionRatio", func(t *testing.T) {
		// Create larger batch to better measure compression
		options := DefaultBatchOptions()
		options.MaxBatchSize = 100
		batch := NewAttestationBatch(options)
		
		// Create 50 quotes with identical measurements
		measurement := make([]byte, 48)
		rand.Read(measurement)
		
		for i := 0; i < 50; i++ {
			reportData := make([]byte, 64)
			binary.BigEndian.PutUint64(reportData, uint64(i))
			
			quote := generateMockTDXQuote(t, measurement, reportData)
			batch.AddQuote(quote, nil, AttestationTypeTDX)
		}
		
		// Create 50 quotes with different measurements
		for i := 0; i < 50; i++ {
			uniqueMeasurement := make([]byte, 48)
			rand.Read(uniqueMeasurement)
			
			quote := generateMockTDXQuote(t, uniqueMeasurement, nil)
			batch.AddQuote(quote, nil, AttestationTypeTDX)
		}
		
		// Verify the batch
		err := batch.VerifyBatch()
		require.NoError(t, err, "Should verify batch without error")
		
		// Get compressed witness
		witness, err := batch.GetCompressedWitness()
		require.NoError(t, err, "Should create compressed witness")
		
		// Calculate compression ratio
		totalSize := 100 * 512 // 100 quotes of 512 bytes each
		ratio := float64(totalSize) / float64(len(witness))
		
		t.Logf("Compression ratio: %.2f (original: %d bytes, compressed: %d bytes)",
			ratio, totalSize, len(witness))
		
		// Should achieve at least 5x compression
		assert.Greater(t, ratio, 5.0, "Should achieve at least 5x compression")
	})
}
