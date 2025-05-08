package mesh

import (
	"context"
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockBlockchainClient is a mock implementation of the BlockchainClient interface for testing
type MockBlockchainClient struct {
	AnchorCalled bool
	AnchoredData []byte // Changed from AnchorData to avoid name conflict with method
	ReturnTxID   string
	ReturnError  error
}

func (m *MockBlockchainClient) AnchorData(ctx context.Context, data []byte) (string, error) {
	m.AnchorCalled = true
	m.AnchoredData = data // Updated to use the renamed field
	return m.ReturnTxID, m.ReturnError
}

func TestCreateBlockchainAnchorData(t *testing.T) {
	// Create a sample regional snapshot
	snapshot := createSampleRegionalSnapshot()
	
	// Convert to blockchain anchor data
	anchorData, err := CreateBlockchainAnchorData(snapshot)
	
	// Verify the conversion was successful
	require.NoError(t, err)
	require.NotNil(t, anchorData)
	
	// Verify the fields were properly extracted
	assert.Equal(t, hex.EncodeToString(snapshot.SnapshotID), anchorData.SnapshotID)
	assert.Equal(t, snapshot.RegionID, anchorData.RegionID)
	assert.Equal(t, snapshot.Timestamp.Unix(), anchorData.Timestamp)
	
	// Verify consensus data
	assert.Equal(t, snapshot.ConsensusInfo.TEECount, anchorData.TEECount)
	assert.Equal(t, snapshot.ConsensusInfo.ConsensusLevel, anchorData.ConsensusLevel)
}

func TestAnchorToBlockchain(t *testing.T) {
	// Create sample coordinator
	coordinator := createSampleCoordinator()
	
	// Create mock blockchain client
	mockClient := &MockBlockchainClient{
		ReturnTxID: "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
	}
	
	// Inject mock client
	coordinator.blockchainClient = mockClient
	coordinator.blockchainAnchorEnabled = true
	
	// Create sample snapshot
	snapshot := createSampleRegionalSnapshot()
	
	// Call the method to test
	err := coordinator.AnchorToBlockchain(context.Background(), snapshot)
	
	// Verify the method executed successfully
	require.NoError(t, err)
	
	// Verify the mock client was called
	assert.True(t, mockClient.AnchorCalled)
	assert.NotEmpty(t, mockClient.AnchoredData)
	
	// Verify the transaction ID was stored in the snapshot metadata
	assert.Equal(t, 
		mockClient.ReturnTxID, 
		snapshot.Metadata["blockchain_tx_id"],
	)
	
	// Verify the anchor time was recorded
	_, hasAnchorTime := snapshot.Metadata["blockchain_anchor_time"]
	assert.True(t, hasAnchorTime)
}

func TestBlockchainAnchoringDisabled(t *testing.T) {
	// Create sample coordinator with anchoring disabled
	coordinator := createSampleCoordinator()
	coordinator.blockchainAnchorEnabled = false
	
	// Create mock blockchain client to verify it's not called
	mockClient := &MockBlockchainClient{}
	coordinator.blockchainClient = mockClient
	
	// Create sample snapshot
	snapshot := createSampleRegionalSnapshot()
	
	// Call the method to test
	err := coordinator.AnchorToBlockchain(context.Background(), snapshot)
	
	// Verify the method executed successfully without error
	require.NoError(t, err)
	
	// Verify the mock client was NOT called
	assert.False(t, mockClient.AnchorCalled)
	
	// Verify no transaction ID was stored
	_, hasTxID := snapshot.Metadata["blockchain_tx_id"]
	assert.False(t, hasTxID)
}

// Helper function to create a sample regional snapshot for testing
func createSampleRegionalSnapshot() *RegionalSnapshot {
	// Create a snapshot summary
	summary := &SnapshotSummary{
		MerkleRoot:     []byte("mock-merkle-root"),
		StateRootHashes: map[string][]byte{
			"tee1": []byte("hash1"),
			"tee2": []byte("hash2"),
		},
		ObjectCount:    10,
		TotalStateSize: 1024,
	}
	
	// Create consensus info
	consensusInfo := &SnapshotConsensusInfo{
		TEECount:         3,
		ParticipatingTEEs: 3,
		ConsensusLevel:    1.0,
		ConsensusMethod:   "unanimous",
		ConsensusSuccess:  true,
	}
	
	// Create a regional snapshot
	snapshot := &RegionalSnapshot{
		RegionID:            "test-region",
		SnapshotID:          []byte("test-snapshot-id"),
		Timestamp:           time.Now().UTC(),
		TEESnapshotIDs:      [][]byte{[]byte("tee1-snapshot"), []byte("tee2-snapshot")},
		SnapshotSummary:     summary,
		ConsensusInfo:       consensusInfo,
		CoordinatorSignature: []byte("coordinator-signature"),
		VerifierSignatures:   [][]byte{[]byte("verifier1-signature"), []byte("verifier2-signature")},
		Metadata:            make(map[string]interface{}),
	}
	
	return snapshot
}

// Helper function to create a sample coordinator for testing
func createSampleCoordinator() *RegionalSnapshotCoordinator {
	return &RegionalSnapshotCoordinator{
		regionID:                "test-region",
		policy:                  DefaultRegionalSnapshotPolicy(),
		teeRegistry:             make(map[string]string),
		ongoingCollections:      make(map[string]*SnapshotCollectionStatus),
		blockchainAnchorEnabled: true,
		blockchainEndpoint:      "http://localhost:8545",
	}
}
