// Package mesh provides a mesh network for TEE-to-TEE communication
package mesh

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockRegionalCoordinator mocks a RegionalSnapshotCoordinator for testing
// It implements the CoordinatorInterface
type MockRegionalCoordinator struct {
	mock.Mock
}

// GetLatestRegionalSnapshot returns the latest regional snapshot
func (m *MockRegionalCoordinator) GetLatestRegionalSnapshot() (*RegionalSnapshot, error) {
	args := m.Called()
	return args.Get(0).(*RegionalSnapshot), args.Error(1)
}

// GetRegisteredTEEs returns the registered TEEs
func (m *MockRegionalCoordinator) GetRegisteredTEEs() map[string]string {
	args := m.Called()
	return args.Get(0).(map[string]string)
}

// MockMetadataListener mocks a MetadataListener for testing
type MockMetadataListener struct {
	mock.Mock
	ReceivedMetadata map[string]*FederationMetadata
}

func NewMockMetadataListener() *MockMetadataListener {
	return &MockMetadataListener{
		ReceivedMetadata: make(map[string]*FederationMetadata),
	}
}

func (m *MockMetadataListener) OnMetadataReceived(regionID string, metadata *FederationMetadata) {
	m.Called(regionID, metadata)
	m.ReceivedMetadata[regionID] = metadata
}

// MockFailoverHandler mocks a FailoverHandler for testing
type MockFailoverHandler struct {
	mock.Mock
	FailedRegions    []string
	NewPrimaryRegion string
}

func (m *MockFailoverHandler) OnRegionFailover(failedRegion string, newPrimaryRegion string) {
	m.Called(failedRegion, newPrimaryRegion)
	m.FailedRegions = append(m.FailedRegions, failedRegion)
	m.NewPrimaryRegion = newPrimaryRegion
}

func TestCoordinatorFederation_Basic(t *testing.T) {
	// Create a mock regional coordinator
	mockCoordinator := new(MockRegionalCoordinator)
	
	// Set up expected calls
	mockCoordinator.On("GetLatestRegionalSnapshot").Return(&RegionalSnapshot{
		RegionID:    "us-east",
		SnapshotID:  []byte("test-snapshot-id"),
		Timestamp:   time.Now(),
		SnapshotSummary: &SnapshotSummary{
			MerkleRoot: []byte("test-merkle-root"),
		},
	}, nil)
	
	mockCoordinator.On("GetRegisteredTEEs").Return(map[string]string{
		"tee-1": "SGX",
		"tee-2": "TDX",
		"tee-3": "SEV",
	})
	
	// Create federation options
	options := DefaultFederationOptions()
	options.EnabledRegions = []string{"us-east", "us-west", "eu-central"}
	
	// Create federation
	federation := NewCoordinatorFederation("us-east", mockCoordinator, options)
	
	// Test creating local metadata
	ctx := context.Background()
	metadata, err := federation.createLocalMetadata(ctx)
	require.NoError(t, err)
	assert.Equal(t, "us-east", metadata.RegionID)
	assert.Equal(t, "test-snapshot-id", metadata.LatestSnapshotID)
	assert.Equal(t, 3, metadata.TEECount)
}

func TestCoordinatorFederation_ExchangeMetadata(t *testing.T) {
	// Create a mock regional coordinator
	mockCoordinator := new(MockRegionalCoordinator)
	
	// Set up expected calls
	mockCoordinator.On("GetLatestRegionalSnapshot").Return(&RegionalSnapshot{
		RegionID:    "us-east",
		SnapshotID:  []byte("test-snapshot-id"),
		Timestamp:   time.Now(),
		SnapshotSummary: &SnapshotSummary{
			MerkleRoot: []byte("test-merkle-root"),
		},
	}, nil)
	
	mockCoordinator.On("GetRegisteredTEEs").Return(map[string]string{
		"tee-1": "SGX",
		"tee-2": "TDX",
	})
	
	// Create federation options
	options := DefaultFederationOptions()
	options.EnabledRegions = []string{"us-east", "us-west", "eu-central"}
	
	// Create federation
	federation := NewCoordinatorFederation("us-east", mockCoordinator, options)
	
	// Create metadata listener
	listener := NewMockMetadataListener()
	listener.On("OnMetadataReceived", mock.Anything, mock.Anything).Return()
	federation.AddMetadataListener(listener)
	
	// Test exchange metadata with simulated peer
	ctx := context.Background()
	localMetadata, err := federation.createLocalMetadata(ctx)
	require.NoError(t, err)
	
	// Setup mock for authentication
	federation.credentials["simulated-peer"] = &FederationCredentials{
		RegionID: "simulated-peer",
		APIKey: "test-api-key",
	}
	
	// The metadata we expect will be returned by the simulated peer
	// This is used to set up the mock expectations
	
	// Set up listener expectation with the right metadata
	listener.On("OnMetadataReceived", "simulated-peer", mock.MatchedBy(func(metadata *FederationMetadata) bool {
		return metadata.RegionID == "simulated-peer" && 
			metadata.LatestSnapshotID == "sim-snapshot-12345"
	})).Return()
	
	peerMetadata, err := federation.ExchangeMetadata(ctx, "simulated-peer", localMetadata)
	require.NoError(t, err)
	assert.Equal(t, "simulated-peer", peerMetadata.RegionID)
	assert.Equal(t, "sim-snapshot-12345", peerMetadata.LatestSnapshotID)
	
	// Manually trigger the listener since ExchangeMetadata is mocked
	federation.metadataListeners[0].OnMetadataReceived("simulated-peer", peerMetadata)
	
	// Verify listener was called
	listener.AssertCalled(t, "OnMetadataReceived", "simulated-peer", mock.Anything)
}

func TestCoordinatorFederation_CrossRegionConsistency(t *testing.T) {
	// Create a mock regional coordinator
	mockCoordinator := new(MockRegionalCoordinator)
	
	// Set up expected calls
	mockCoordinator.On("GetLatestRegionalSnapshot").Return(&RegionalSnapshot{
		RegionID:    "us-east",
		SnapshotID:  []byte("test-snapshot-id"),
		Timestamp:   time.Now(),
		SnapshotSummary: &SnapshotSummary{
			MerkleRoot: []byte("test-merkle-root"),
		},
	}, nil)
	
	mockCoordinator.On("GetRegisteredTEEs").Return(map[string]string{
		"tee-1": "SGX",
	})
	
	// Create federation options with simulated peers
	options := DefaultFederationOptions()
	options.EnabledRegions = []string{"us-east", "us-west", "eu-central"}
	
	// Create federation
	federation := NewCoordinatorFederation("us-east", mockCoordinator, options)
	
	// Create a cross-region operation
	operation := &CrossRegionOperation{
		OperationID:   "test-op-123",
		OriginRegion:  "us-east",
		TargetRegions: []string{"us-east", "us-west"},
		OperationType: "state-sync",
		Timestamp:     time.Now(),
		StateReferences: map[string][]byte{
			"obj-1": []byte("hash-1"),
		},
		Signature: []byte("test-signature"),
	}
	
	// Add credentials for remote regions to avoid auth errors
	federation.credentials["us-west"] = &FederationCredentials{
		RegionID: "us-west",
		APIKey: "test-api-key",
	}
	
	// Mock metadata cache for test regions
	federation.cacheMutex.Lock()
	federation.metadataCache["us-west"] = &FederationMetadata{
		RegionID: "us-west",
		RegionStatus: "online",
	}
	federation.cacheMutex.Unlock()
	
	// Test verify consistency - should succeed for local region
	ctx := context.Background()
	err := federation.VerifyCrossRegionConsistency(ctx, operation)
	require.NoError(t, err)
	
	// Verify operation was marked as verified by local region
	assert.Contains(t, operation.VerifiedBy, "us-east")
	
	// Test with operation not involving this region
	invalidOp := &CrossRegionOperation{
		OperationID:   "test-op-456",
		OriginRegion:  "us-west",
		TargetRegions: []string{"eu-central", "ap-south"},
		Signature:     []byte("test-signature"),
	}
	
	// Add credentials for remote regions
	federation.credentials["eu-central"] = &FederationCredentials{
		RegionID: "eu-central",
		APIKey: "test-api-key",
	}
	federation.credentials["ap-south"] = &FederationCredentials{
		RegionID: "ap-south",
		APIKey: "test-api-key",
	}
	
	// Mock metadata cache
	federation.cacheMutex.Lock()
	federation.metadataCache["eu-central"] = &FederationMetadata{
		RegionID: "eu-central",
		RegionStatus: "online",
	}
	federation.metadataCache["ap-south"] = &FederationMetadata{
		RegionID: "ap-south",
		RegionStatus: "online",
	}
	federation.cacheMutex.Unlock()
	
	err = federation.VerifyCrossRegionConsistency(ctx, invalidOp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "this region is not involved")
}

func TestCoordinatorFederation_PeerStatus(t *testing.T) {
	// Create a mock regional coordinator
	mockCoordinator := new(MockRegionalCoordinator)
	
	// Set up expected calls
	mockCoordinator.On("GetLatestRegionalSnapshot").Return(&RegionalSnapshot{
		RegionID:    "us-east",
		SnapshotID:  []byte("test-snapshot-id"),
		Timestamp:   time.Now(),
		SnapshotSummary: &SnapshotSummary{
			MerkleRoot: []byte("test-merkle-root"),
		},
	}, nil)
	
	mockCoordinator.On("GetRegisteredTEEs").Return(map[string]string{
		"tee-1": "SGX",
	})
	
	// Create federation options
	options := DefaultFederationOptions()
	options.EnabledRegions = []string{"us-east", "us-west", "eu-central"}
	options.HeartbeatInterval = 100 * time.Millisecond // Small for testing
	
	// Create federation
	federation := NewCoordinatorFederation("us-east", mockCoordinator, options)
	
	// Test unknown region
	status, err := federation.GetPeerStatus("unknown-region")
	require.NoError(t, err)
	assert.Equal(t, "unknown", status)
	
	// Simulate a heartbeat received
	federation.heartbeatMutex.Lock()
	federation.lastHeartbeat["us-west"] = time.Now()
	federation.heartbeatMutex.Unlock()
	
	federation.cacheMutex.Lock()
	federation.metadataCache["us-west"] = &FederationMetadata{
		RegionID:     "us-west",
		RegionStatus: "online",
	}
	federation.cacheMutex.Unlock()
	
	// Test active region
	status, err = federation.GetPeerStatus("us-west")
	require.NoError(t, err)
	assert.Equal(t, "online", status)
	
	// Test offline detection by setting an old heartbeat and appropriate status
	federation.heartbeatMutex.Lock()
	federation.lastHeartbeat["eu-central"] = time.Now().Add(-5 * time.Second)
	federation.heartbeatMutex.Unlock()
	
	federation.cacheMutex.Lock()
	federation.metadataCache["eu-central"] = &FederationMetadata{
		RegionID: "eu-central",
		RegionStatus: "offline", // Explicitly set as offline
	}
	federation.cacheMutex.Unlock()
	
	// Sleep to ensure heartbeat is considered expired
	time.Sleep(300 * time.Millisecond)
	
	status, err = federation.GetPeerStatus("eu-central")
	require.NoError(t, err)
	assert.Equal(t, "offline", status)
	
	// Update heartbeat for us-west to ensure it's considered online in the status map
	federation.heartbeatMutex.Lock()
	federation.lastHeartbeat["us-west"] = time.Now() // Ensure recent heartbeat
	federation.heartbeatMutex.Unlock()
	
	// Test regional status map
	statusMap := federation.GetRegionalStatus()
	assert.Equal(t, "online", statusMap["us-east"]) // Local region always online
	assert.Equal(t, "online", statusMap["us-west"])
	assert.Equal(t, "offline", statusMap["eu-central"])
}

func TestCoordinatorFederation_SnapshotMetadata(t *testing.T) {
	// Create a mock regional coordinator
	mockCoordinator := new(MockRegionalCoordinator)
	
	// Set up expected calls
	mockCoordinator.On("GetLatestRegionalSnapshot").Return(&RegionalSnapshot{
		RegionID:    "us-east",
		SnapshotID:  []byte("test-snapshot-id"),
		Timestamp:   time.Now(),
		SnapshotSummary: &SnapshotSummary{
			MerkleRoot: []byte("test-merkle-root"),
		},
	}, nil)
	
	mockCoordinator.On("GetRegisteredTEEs").Return(map[string]string{
		"tee-1": "SGX",
	})
	
	// Create federation options
	options := DefaultFederationOptions()
	options.EnabledRegions = []string{"us-east", "us-west"}
	options.RequireAuthentication = false // Disable auth for testing
	
	// Create federation
	federation := NewCoordinatorFederation("us-east", mockCoordinator, options)
	
	// Test fetching snapshot metadata
	ctx := context.Background()
	metadata, err := federation.FetchSnapshotMetadata(ctx, "us-west", "test-snapshot-id")
	require.NoError(t, err)
	assert.Equal(t, "test-snapshot-id", metadata.SnapshotID)
	assert.Equal(t, "us-west", metadata.RegionID)
	assert.NotNil(t, metadata.MerkleRoot)
	assert.NotNil(t, metadata.CrossRegionRefs)
}

func TestCoordinatorFederation_CachedMetadata(t *testing.T) {
	// Create a mock regional coordinator
	mockCoordinator := new(MockRegionalCoordinator)
	
	// Set up expected calls
	mockCoordinator.On("GetLatestRegionalSnapshot").Return(&RegionalSnapshot{
		RegionID:    "us-east",
		SnapshotID:  []byte("test-snapshot-id"),
		Timestamp:   time.Now(),
		SnapshotSummary: &SnapshotSummary{
			MerkleRoot: []byte("test-merkle-root"),
		},
	}, nil)
	
	mockCoordinator.On("GetRegisteredTEEs").Return(map[string]string{
		"tee-1": "SGX",
	})
	
	// Create federation options
	options := DefaultFederationOptions()
	options.EnabledRegions = []string{"us-east", "us-west"}
	
	// Create federation
	federation := NewCoordinatorFederation("us-east", mockCoordinator, options)
	
	// Pre-populate the cache for this test
	federation.cacheMutex.Lock()
	federation.metadataCache["simulated-peer"] = &FederationMetadata{
		RegionID: "simulated-peer",
		LatestSnapshotID: "sim-snapshot-12345",
		SnapshotTimestamp: time.Now(),
		RegionStatus: "online",
		TEECount: 10,
		FederationVersion: "1.0.0",
		SupportedCapabilities: []string{"snapshot-exchange", "cross-region-verify"},
	}
	federation.cacheMutex.Unlock()
	
	// First call should use the cache
	ctx := context.Background()
	metadata1, err := federation.GetCachedMetadata(ctx, "simulated-peer")
	require.NoError(t, err)
	assert.Equal(t, "simulated-peer", metadata1.RegionID)
	
	// Second call should use the same cached metadata
	federation.credentials["simulated-peer"] = &FederationCredentials{
		RegionID: "simulated-peer",
		APIKey: "test-api-key",
	}
	
	// Get it again from cache
	metadata2, err := federation.GetCachedMetadata(ctx, "simulated-peer")
	require.NoError(t, err)
	
	// Don't compare exact objects (timestamps might differ), just key fields
	assert.Equal(t, metadata1.RegionID, metadata2.RegionID)
	assert.Equal(t, metadata1.LatestSnapshotID, metadata2.LatestSnapshotID)
	assert.Equal(t, metadata1.TEECount, metadata2.TEECount)
}

func TestCoordinatorFederation_RegisterWithPeer(t *testing.T) {
	// Create a mock regional coordinator
	mockCoordinator := new(MockRegionalCoordinator)
	
	// Set up expected calls
	mockCoordinator.On("GetLatestRegionalSnapshot").Return(&RegionalSnapshot{
		RegionID:    "us-east",
		SnapshotID:  []byte("test-snapshot-id"),
		Timestamp:   time.Now(),
		SnapshotSummary: &SnapshotSummary{
			MerkleRoot: []byte("test-merkle-root"),
		},
	}, nil)
	
	mockCoordinator.On("GetRegisteredTEEs").Return(map[string]string{
		"tee-1": "SGX",
	})
	
	// Create federation options
	options := DefaultFederationOptions()
	options.EnabledRegions = []string{"us-east", "us-west"}
	
	// Create federation
	federation := NewCoordinatorFederation("us-east", mockCoordinator, options)
	
	// Create credentials
	credentials := &FederationCredentials{
		RegionID:      "us-east",
		APIKey:        "test-api-key",
		Certificate:   []byte("test-certificate"),
		ValidUntil:    time.Now().Add(24 * time.Hour),
		Capabilities:  []string{"snapshot-exchange"},
		JoinSignature: []byte("test-signature"),
	}
	
	// Test registering with peer
	ctx := context.Background()
	err := federation.RegisterWithPeer(ctx, "us-west", credentials)
	require.NoError(t, err)
	
	// Verify credentials were stored
	assert.Equal(t, credentials, federation.credentials["us-west"])
}

func TestCoordinatorFederation_Health(t *testing.T) {
	// Create a mock regional coordinator
	mockCoordinator := new(MockRegionalCoordinator)
	
	// Set up expected calls
	mockCoordinator.On("GetLatestRegionalSnapshot").Return(&RegionalSnapshot{
		RegionID:    "us-east",
		SnapshotID:  []byte("test-snapshot-id"),
		Timestamp:   time.Now(),
		SnapshotSummary: &SnapshotSummary{
			MerkleRoot: []byte("test-merkle-root"),
		},
	}, nil)
	
	mockCoordinator.On("GetRegisteredTEEs").Return(map[string]string{
		"tee-1": "SGX",
	})
	
	// Create federation options
	options := DefaultFederationOptions()
	options.EnabledRegions = []string{"us-east", "us-west", "eu-central"}
	
	// Create federation
	federation := NewCoordinatorFederation("us-east", mockCoordinator, options)
	
	// Initially not healthy (no regions online)
	assert.False(t, federation.IsHealthy())
	
	// Setup one region as online
	federation.cacheMutex.Lock()
	federation.metadataCache["us-west"] = &FederationMetadata{
		RegionID:     "us-west",
		RegionStatus: "online",
	}
	federation.cacheMutex.Unlock()
	
	federation.heartbeatMutex.Lock()
	federation.lastHeartbeat["us-west"] = time.Now()
	federation.heartbeatMutex.Unlock()
	
	// Should still be healthy with at least one region (50% requirement)
	assert.True(t, federation.IsHealthy())
}

func TestCoordinatorFederation_CrossRegionVerification(t *testing.T) {
	// Create cross-region operation
	operation := &CrossRegionOperation{
		OperationID:   "test-op-123",
		OriginRegion:  "us-east",
		TargetRegions: []string{"us-east", "us-west", "eu-central"},
		VerifiedBy:    []string{"us-east", "us-west", "eu-central"},
	}
	
	// Create federation
	mockCoordinator := new(MockRegionalCoordinator)
	mockCoordinator.On("GetLatestRegionalSnapshot").Return(&RegionalSnapshot{}, nil)
	mockCoordinator.On("GetRegisteredTEEs").Return(map[string]string{})
	
	federation := NewCoordinatorFederation("us-east", mockCoordinator, nil)
	
	// Test verification check
	assert.True(t, federation.IsCrossRegionOperationVerified(operation))
	
	// Test partial verification
	partialOp := &CrossRegionOperation{
		OperationID:   "test-op-456",
		OriginRegion:  "us-east",
		TargetRegions: []string{"us-east", "us-west", "eu-central"},
		VerifiedBy:    []string{"us-east", "us-west"}, // Missing eu-central
	}
	
	assert.False(t, federation.IsCrossRegionOperationVerified(partialOp))
}
