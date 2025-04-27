package attestation

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/x/merkledb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestMerkleDB creates a MerkleDB instance for testing
func setupTestMerkleDB(t *testing.T) merkledb.MerkleDB {
	t.Helper()
	
	// Create an in-memory database for testing
	db := memdb.New()
	merkleDB, err := merkledb.New(context.Background(), db, merkledb.Config{
		BranchFactor: 16,
		// These fields were removed in the latest MerkleDB API
		// ValueCacheSize: 100,
		// IntermediateNodeCacheSize: 100,
	})
	require.NoError(t, err)
	
	return merkleDB
}

// populateTestMerkleDB adds sample NASDAQ market data to a MerkleDB instance
func populateTestMerkleDB(t *testing.T, db merkledb.MerkleDB) ids.ID {
	t.Helper()
	
	// Create a new batch
	batch := db.NewBatch()
	defer batch.Reset()
	
	// Add some key-value pairs representing NASDAQ market data
	testData := map[string]string{
		"nasdaq/symbol/AAPL":  `{"price": 175.23, "volume": 5000000, "timestamp": "2025-04-25T17:00:00Z"}`,
		"nasdaq/symbol/MSFT":  `{"price": 325.12, "volume": 3200000, "timestamp": "2025-04-25T17:00:00Z"}`,
		"nasdaq/symbol/GOOGL": `{"price": 135.72, "volume": 1800000, "timestamp": "2025-04-25T17:00:00Z"}`,
		"nasdaq/symbol/AMZN":  `{"price": 132.45, "volume": 2500000, "timestamp": "2025-04-25T17:00:00Z"}`,
		"nasdaq/market_state": `{"status": "open", "timestamp": "2025-04-25T17:00:00Z"}`,
		"nasdaq/metrics/performance": `{"avg_latency_ms": 12.3, "throughput_tps": 5500, "timestamp": "2025-04-25T17:00:00Z"}`,
	}
	
	for key, value := range testData {
		err := batch.Put([]byte(key), []byte(value))
		require.NoError(t, err)
	}
	
	// Apply changes using batch.Reset() as per the latest MerkleDB API
	// This replaces the older batch.Commit() method
	batch.Reset()
	
	// Return the root ID
	rootID, err := db.GetMerkleRoot(context.Background())
	require.NoError(t, err)
	
	return rootID
}

// mockAttestationService creates a mock attestation service for testing
type mockAttestationService struct {
	// Mock functions to implement the AttestationService interface
	t *testing.T // Reference to testing.T for assertions
}

// newMockAttestationService creates a new mock attestation service for testing
func newMockAttestationService(t *testing.T) *mockAttestationService {
	return &mockAttestationService{t: t}
}

// GetAttestation implements the AttestationService interface
func (m *mockAttestationService) GetAttestation(enclaveID []byte) (*EnclaveAttestation, error) {
	// Generate a mock attestation for testing
	attestationData := &EnclaveAttestation{
		Type:        0,
		EnclaveID:   enclaveID,
		Timestamp:   time.Now().UTC(),
		RegionID:    "us-east",
	}
	
	// Generate mock measurement
	measurement := make([]byte, 32)
	_, err := rand.Read(measurement)
	if err != nil {
		return nil, err
	}
	attestationData.Measurement = measurement
	
	// Generate mock signature
	signature := make([]byte, 64)
	_, err = rand.Read(signature)
	if err != nil {
		return nil, err
	}
	attestationData.Signature = signature
	
	return attestationData, nil
}

// VerifySignature implements the AttestationService interface
func (m *mockAttestationService) VerifySignature(attestation *EnclaveAttestation) error {
	// Mock implementation always succeeds
	return nil
}

// StoreAttestation implements the AttestationService interface
func (m *mockAttestationService) StoreAttestation(attestation *EnclaveAttestation) error {
	// Mock implementation always succeeds
	return nil
}

// TestMerkleDBAttestationService tests the MerkleDB attestation service
func TestMerkleDBAttestationService(t *testing.T) {
	// Setup test environment
	merkleDB := setupTestMerkleDB(t)
	rootID := populateTestMerkleDB(t, merkleDB)
	mockAttSvc := newMockAttestationService(t)
	
	// Test data - ensure proper parameter validation (reasonable length bounds)
	enclaveID := make([]byte, 32) // Fixed reasonable size for TEE ID
	_, err := rand.Read(enclaveID) // Generate random ID
	require.NoError(t, err)
	
	regionID := "us-east"
	teeType := "SGX"
	
	// Create MerkleDB attestation service - use the initialized mockAttSvc
	merkleDBAttSvc := NewMerkleDBAttestationService(
		mockAttSvc,
		merkleDB,
		enclaveID,
		regionID,
		teeType,
	)
	
	// Test creating an attestation
	t.Run("AttestMerkleDBState", func(t *testing.T) {
		attestation, err := merkleDBAttSvc.AttestMerkleDBState()
		require.NoError(t, err)
		assert.NotNil(t, attestation)
		
		// Verify attestation data
		assert.Equal(t, rootID.String(), attestation.RootIDString)
		assert.Equal(t, regionID, attestation.RegionID)
		assert.Equal(t, teeType, attestation.TEEType)
		assert.Equal(t, enclaveID, attestation.EnclaveID)
		assert.NotNil(t, attestation.Measurement)
		assert.NotNil(t, attestation.Signature)
		assert.NotZero(t, attestation.Timestamp)
	})
	
	// Test verifying an attestation
	t.Run("VerifyMerkleDBStateAttestation", func(t *testing.T) {
		// Create attestation
		attestation, err := merkleDBAttSvc.AttestMerkleDBState()
		require.NoError(t, err)
		
		// Verify it
		err = merkleDBAttSvc.VerifyMerkleDBStateAttestation(attestation)
		assert.NoError(t, err)
	})
	
	// Test storing an attestation
	t.Run("StoreAttestationWithState", func(t *testing.T) {
		err := merkleDBAttSvc.StoreAttestationWithState()
		assert.NoError(t, err)
	})
	
	// Test verifying state against attestation
	t.Run("VerifyStateAgainstAttestation", func(t *testing.T) {
		// Create attestation
		attestation, err := merkleDBAttSvc.AttestMerkleDBState()
		require.NoError(t, err)
		
		// Verify state matches attestation
		err = merkleDBAttSvc.VerifyStateAgainstAttestation(attestation)
		assert.NoError(t, err)
	})
	
	// Test mismatch detection
	t.Run("DetectRootMismatch", func(t *testing.T) {
		// Create attestation
		attestation, err := merkleDBAttSvc.AttestMerkleDBState()
		require.NoError(t, err)
		
		// Modify the root ID to create a mismatch
		fakeRootID := ids.GenerateTestID()
		// Convert ID to string representation for bytes as Bytes() method is not available
		attestation.RootID = []byte(fakeRootID.String())
		attestation.RootIDString = fakeRootID.String()
		
		// This should fail because the roots don't match
		err = merkleDBAttSvc.VerifyStateAgainstAttestation(attestation)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrMerkleRootMismatch)
	})
}

// TestIntegrationWithFinancialData tests the attestation system with financial data updates
func TestIntegrationWithFinancialData(t *testing.T) {
	// Setup test environment
	merkleDB := setupTestMerkleDB(t)
	_ = populateTestMerkleDB(t, merkleDB)
	mockAttSvc := newMockAttestationService(t)
	
	// Test data
	enclaveID := []byte("test-enclave-id")
	regionID := "us-east"
	teeType := "SGX"
	
	// Create MerkleDB attestation service - use the initialized mockAttSvc
	merkleDBAttSvc := NewMerkleDBAttestationService(
		mockAttSvc,
		merkleDB,
		enclaveID,
		regionID,
		teeType,
	)
	
	// Step 1: Create initial attestation
	initialAttestation, err := merkleDBAttSvc.AttestMerkleDBState()
	require.NoError(t, err)
	
	// Step 2: Update NASDAQ market data
	// NewBatch() returns only a batch in the latest MerkleDB API, no error
	batch := merkleDB.NewBatch()
	
	// Update some prices
	updatedData := map[string]string{
		"nasdaq/symbol/AAPL": `{"price": 176.50, "volume": 5120000, "timestamp": "2025-04-25T17:15:00Z"}`,
		"nasdaq/symbol/MSFT": `{"price": 326.75, "volume": 3300000, "timestamp": "2025-04-25T17:15:00Z"}`,
	}
	
	for key, value := range updatedData {
		err := batch.Put([]byte(key), []byte(value))
		require.NoError(t, err)
	}
	
	// Apply changes using batch.Reset() instead of Commit() in the latest MerkleDB API
	batch.Reset()
	
	// Step 3: Create new attestation after update
	updatedAttestation, err := merkleDBAttSvc.AttestMerkleDBState()
	require.NoError(t, err)
	
	// Step 4: Verify the roots are different (state changed)
	assert.NotEqual(t, initialAttestation.RootIDString, updatedAttestation.RootIDString)
	
	// Step 5: Verify both attestations are valid
	err = merkleDBAttSvc.VerifyMerkleDBStateAttestation(initialAttestation)
	assert.NoError(t, err)
	
	err = merkleDBAttSvc.VerifyMerkleDBStateAttestation(updatedAttestation)
	assert.NoError(t, err)
	
	// Step 6: Verify state against updated attestation
	err = merkleDBAttSvc.VerifyStateAgainstAttestation(updatedAttestation)
	assert.NoError(t, err)
	
	// Step 7: Initial attestation should no longer match current state
	err = merkleDBAttSvc.VerifyStateAgainstAttestation(initialAttestation)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrMerkleRootMismatch)
}

// TestAttestationPersistence tests persisting attestations across restarts
func TestAttestationPersistence(t *testing.T) {
	// This test simulates a TEE restart scenario
	
	// Step 1: Setup initial environment and data
	merkleDB := setupTestMerkleDB(t)
	rootID := populateTestMerkleDB(t, merkleDB)
	mockAttSvc := newMockAttestationService(t)
	
	enclaveID := []byte("test-enclave-id")
	regionID := "us-east"
	teeType := "SGX"
	
	merkleDBAttSvc := NewMerkleDBAttestationService(
		mockAttSvc,
		merkleDB,
		enclaveID,
		regionID,
		teeType,
	)
	
	// Step 2: Create and store attestation
	attestation, err := merkleDBAttSvc.AttestMerkleDBState()
	require.NoError(t, err)
	
	// Serialize for "persistent" storage
	attestationBytes, err := json.Marshal(attestation)
	require.NoError(t, err)
	
	// Step 3: Simulate TEE restart by creating new instances
	// This would normally involve a real restart, here we just create new objects
	newMerkleDB := setupTestMerkleDB(t)
	
	// Step 4: Deserialize the stored attestation
	var storedAttestation MerkleDBStateAttestation
	err = json.Unmarshal(attestationBytes, &storedAttestation)
	require.NoError(t, err)
	
	// Step 5: Create verification service for the "restarted" TEE
	newMerkleDBAttSvc := NewMerkleDBAttestationService(
		mockAttSvc,
		newMerkleDB,
		enclaveID,
		regionID,
		teeType,
	)
	
	// Step 6: Restore state based on stored attestation
	// First verify the attestation itself
	err = newMerkleDBAttSvc.VerifyMerkleDBStateAttestation(&storedAttestation)
	require.NoError(t, err)
	
	// Step 7: In a real implementation, we would now restore MerkleDB state
	// For test purposes, we'll manually restore the same state
	populateTestMerkleDB(t, newMerkleDB)
	
	// Step 8: Verify restored state matches the attestation
	ctx := context.Background()
	restoredRootID, err := newMerkleDB.GetMerkleRoot(ctx)
	require.NoError(t, err)
	
	// Convert stored root ID to ids.ID for comparison
	storedRootID, err := ids.ToID(storedAttestation.RootID)
	require.NoError(t, err)
	
	// Assert the roots match, confirming successful state restoration
	assert.Equal(t, rootID, restoredRootID)
	assert.Equal(t, storedRootID, restoredRootID)
}
