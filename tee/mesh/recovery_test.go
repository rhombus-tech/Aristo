package mesh

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecoveryManager(t *testing.T) {
	// Create mock dependencies
	mockStateManager := &MockStateManager{}
	mockSnapshotStorage := &MockSnapshotStorage{}

	// Create a coordinator with the mocks
	coordinator := createSampleCoordinator()
	
	// Create recovery options
	options := &RecoveryOptions{
		TEEID:             "test-tee",
		TEEType:           "SGX",
		RegionID:          "test-region",
		MaxRecoveryTime:   1 * time.Minute,
		VerifyBlockchain:  false, // Disable for testing
		VerifyAttestation: false, // Disable for testing
	}
	
	// Create the recovery manager
	recoveryManager := NewRecoveryManager(
		mockStateManager,
		mockSnapshotStorage,
		coordinator,
		options,
	)
	
	require.NotNil(t, recoveryManager, "Recovery manager should not be nil")
	
	// Check initial status
	inProgress, lastTime, lastErr := recoveryManager.GetRecoveryStatus()
	assert.False(t, inProgress, "Recovery should not be in progress initially")
	assert.Zero(t, lastTime, "Last recovery time should be zero initially")
	assert.Nil(t, lastErr, "Last recovery error should be nil initially")
	
	// Verify the returned recovery info
	info := recoveryManager.GetDetailedRecoveryInfo()
	assert.False(t, info.InProgress, "Recovery info should match status")
	assert.Equal(t, 0, info.RecoveredObjects, "Should have no recovered objects initially")
}

func TestRecoveryAuthentication(t *testing.T) {
	// Create dependencies
	mockStateManager := &MockStateManager{}
	mockSnapshotStorage := &MockSnapshotStorage{}
	coordinator := createSampleCoordinator()
	
	// Create recovery options
	options := &RecoveryOptions{
		TEEID:    "test-tee-auth",
		TEEType:  "SGX",
		RegionID: "test-region",
	}
	
	// Create recovery manager
	recoveryManager := NewRecoveryManager(
		mockStateManager,
		mockSnapshotStorage,
		coordinator,
		options,
	)
	
	// Verify authentication works
	err := recoveryManager.authenticateToCoordinator(context.Background())
	assert.NoError(t, err, "Authentication should succeed")
	
	// Verify the TEE was registered with the coordinator
	registeredTEEs := coordinator.GetRegisteredTEEs()
	assert.Contains(t, registeredTEEs, "test-tee-auth", "TEE should be registered with coordinator")
	assert.Equal(t, "SGX", registeredTEEs["test-tee-auth"], "TEE type should be correctly registered")
}

func TestSnapshotVerification(t *testing.T) {
	// Create dependencies
	mockStateManager := &MockStateManager{}
	mockSnapshotStorage := &MockSnapshotStorage{}
	coordinator := createSampleCoordinator()
	
	// Create recovery options
	options := &RecoveryOptions{
		TEEID:             "test-tee",
		TEEType:           "SGX",
		RegionID:          "test-region",
		VerifyBlockchain:  false, // Disable for this test
	}
	
	// Create recovery manager
	recoveryManager := NewRecoveryManager(
		mockStateManager,
		mockSnapshotStorage,
		coordinator,
		options,
	)
	
	// Create a valid snapshot for testing
	snapshot := createSampleRegionalSnapshot()
	snapshot.CoordinatorSignature = []byte("valid-signature")
	snapshot.ConsensusInfo.ConsensusLevel = 0.8 // Above the 2/3 threshold
	
	// Verify it passes basic verification
	err := recoveryManager.verifySnapshotIntegrity(context.Background(), snapshot)
	assert.NoError(t, err, "Valid snapshot should pass verification")
	
	// Create an invalid snapshot with insufficient consensus
	invalidSnapshot := createSampleRegionalSnapshot()
	invalidSnapshot.CoordinatorSignature = []byte("valid-signature")
	invalidSnapshot.ConsensusInfo.ConsensusLevel = 0.5 // Below the 2/3 threshold
	
	// Verify it fails verification
	err = recoveryManager.verifySnapshotIntegrity(context.Background(), invalidSnapshot)
	assert.Error(t, err, "Invalid snapshot should fail verification")
}

func TestBlockchainVerification(t *testing.T) {
	// Create dependencies
	mockStateManager := &MockStateManager{}
	mockSnapshotStorage := &MockSnapshotStorage{}
	coordinator := createSampleCoordinator()
	
	// Create a mock blockchain client
	mockClient := &MockBlockchainClient{
		ReturnTxID: "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
	}
	
	// Create recovery options with blockchain verification enabled
	options := &RecoveryOptions{
		TEEID:             "test-tee",
		TEEType:           "SGX",
		RegionID:          "test-region",
		VerifyBlockchain:  true,
		BlockchainEndpoint: "http://localhost:8545",
	}
	
	// Create recovery manager
	recoveryManager := NewRecoveryManager(
		mockStateManager,
		mockSnapshotStorage,
		coordinator,
		options,
	)
	
	// Set our mock client
	recoveryManager.blockchainClient = mockClient
	
	// Create a valid snapshot with blockchain anchor
	snapshot := createSampleRegionalSnapshot()
	snapshot.CoordinatorSignature = []byte("valid-signature")
	snapshot.ConsensusInfo.ConsensusLevel = 0.8
	
	// Add blockchain transaction ID to metadata
	if snapshot.Metadata == nil {
		snapshot.Metadata = make(map[string]interface{})
	}
	snapshot.Metadata["blockchain_tx_id"] = "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
	
	// Verify it passes blockchain verification
	err := recoveryManager.verifyBlockchainAnchor(context.Background(), snapshot)
	assert.NoError(t, err, "Valid blockchain anchor should pass verification")
	
	// Create an invalid snapshot with missing blockchain anchor
	invalidSnapshot := createSampleRegionalSnapshot()
	invalidSnapshot.CoordinatorSignature = []byte("valid-signature")
	invalidSnapshot.ConsensusInfo.ConsensusLevel = 0.8
	// No blockchain transaction ID
	
	// Verify it fails blockchain verification
	err = recoveryManager.verifyBlockchainAnchor(context.Background(), invalidSnapshot)
	assert.Error(t, err, "Missing blockchain anchor should fail verification")
}
