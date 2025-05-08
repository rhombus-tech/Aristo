package xregion

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockTransport implements a basic transport for testing
type MockTransport struct {
	sentPackets     map[string][]*RLNCPacket
	deliverySuccess float64
	packetLoss      float64
}

func NewMockTransport(deliverySuccess, packetLoss float64) *MockTransport {
	return &MockTransport{
		sentPackets:     make(map[string][]*RLNCPacket),
		deliverySuccess: deliverySuccess,
		packetLoss:      packetLoss,
	}
}

// TestRLNCTransportBasic tests basic RLNC transport functionality
func TestRLNCTransportBasic(t *testing.T) {
	// Create test config with smaller generation size for faster testing
	config := &RLNCTransportConfig{
		GenSize:       8,
		MinRedundancy: 1.5,
		MaxRedundancy: 3.0,
		AdaptiveMode:  true,
		MaxPacketSize: 4 * 1024,
		Enabled:       true,
	}

	// Create transport
	transport := NewRLNCTransport(nil, config)
	require.NotNil(t, transport)

	// Verify default metrics are initialized
	metrics := transport.GetMetrics()
	assert.Equal(t, int64(0), metrics["packets_encoded"])
	assert.Equal(t, int64(0), metrics["packets_decoded"])
}

// TestRLNCNetworkHealthAdaptation tests that redundancy adapts to network health
func TestRLNCNetworkHealthAdaptation(t *testing.T) {
	config := &RLNCTransportConfig{
		GenSize:       8,
		MinRedundancy: 1.5,
		MaxRedundancy: 3.0,
		AdaptiveMode:  true,
		Enabled:       true,
	}

	transport := NewRLNCTransport(nil, config)
	require.NotNil(t, transport)

	// Test regions with different health scores
	regions := []struct {
		id     string
		health float64
	}{
		{"region1", 1.0},  // Perfect health
		{"region2", 0.5},  // Medium health
		{"region3", 0.1},  // Poor health
	}

	// Update health metrics
	for _, region := range regions {
		transport.UpdateNetworkHealth(region.id, region.health)
	}

	// Check redundancy calculation
	for _, region := range regions {
		redundancy := transport.calculateRedundancyForRegion(region.id)
		
		// Check that redundancy is inversely related to health
		// For health=1.0 (perfect), redundancy should be minimum
		// For health=0.0 (poor), redundancy should be maximum
		expectedMin := config.MinRedundancy
		expectedMax := config.MaxRedundancy
		
		if region.health == 1.0 {
			assert.InDelta(t, expectedMin, redundancy, 0.01)
		} else if region.health == 0.0 {
			assert.InDelta(t, expectedMax, redundancy, 0.01)
		} else {
			// Should be in between min and max
			assert.Greater(t, redundancy, expectedMin)
			assert.Less(t, redundancy, expectedMax)
		}
	}

	// Test with adaptive mode disabled
	config.AdaptiveMode = false
	transport = NewRLNCTransport(nil, config)
	
	for _, region := range regions {
		transport.UpdateNetworkHealth(region.id, region.health)
		redundancy := transport.calculateRedundancyForRegion(region.id)
		// With adaptive mode disabled, should always use minimum redundancy
		assert.InDelta(t, config.MinRedundancy, redundancy, 0.01)
	}
}

// TestRLNCCoordinator tests the RLNC-enhanced coordinator
func TestRLNCCoordinator(t *testing.T) {
	// Create basic coordinator config
	coordConfig := &CoordinatorConfig{
		RegionID: "test-region",
	}

	// Create RLNC config
	rlncConfig := &RLNCTransportConfig{
		GenSize:       8,
		MinRedundancy: 1.5,
		MaxRedundancy: 3.0,
		AdaptiveMode:  true,
		Enabled:       true,
	}

	// Create RLNC coordinator
	coordinator, err := NewRLNCCoordinator(coordConfig, rlncConfig)
	require.NoError(t, err)
	require.NotNil(t, coordinator)

	// Check that metrics are initialized
	metrics := coordinator.GetRLNCMetrics()
	assert.NotNil(t, metrics)
	assert.Equal(t, int64(0), metrics.CrossRegionMessagesSent)
}

// TestRLNCPacketEncoding simulates packet encoding and loss to test recovery capabilities
func TestRLNCPacketEncoding(t *testing.T) {
	// Skip if running short tests
	if testing.Short() {
		t.Skip("Skipping extended test in short mode")
	}

	// Create test data
	testData := []byte("This is a test message for RLNC encoding and decoding across regions with potential packet loss to verify the resilience capabilities of the implementation")

	// Create test config
	config := &RLNCTransportConfig{
		GenSize:       8,
		MinRedundancy: 1.5,
		MaxRedundancy: 2.0,
		AdaptiveMode:  true,
		Enabled:       true,
	}

	// Create base transport
	baseTrans := NewDefaultBaseTransport()
	baseTrans.SetRegionConnected("test-region", true)
	baseTrans.SetRegionHealth("test-region", 0.8)
	
	// Create transport
	transport := NewRLNCTransport(baseTrans, config)
	
	// Generate packets using proper RLNC encoding
	messageID := "test-message-rlnc"
	
	// Initialize packet receiver (this simulates what happens on the receiving end)
	receiveCount := 0
	var receivedData []byte
	var decodeComplete bool
	
	baseTrans.SetReceiveCallback(func(sourceRegion string, packet *RLNCPacket) {
		receiveCount++
		// Try to decode with each packet received
		data, done, err := transport.ReceiveFromRegion(sourceRegion, packet)
		if err == nil && done {
			receiveDone := false
			if data != nil && !decodeComplete {
				decodeComplete = true
				receiveDone = true
				receivedData = data
			}
			if receiveDone {
				// Successfully decoded message
				t.Logf("Successfully decoded message after %d packets", receiveCount)
			}
		}
	})

	// Send message
	ctx := context.Background()
	err := transport.SendToRegion(ctx, "test-region", messageID, testData)
	require.NoError(t, err)
	
	// Give it a moment to process
	time.Sleep(100 * time.Millisecond)
	
	// Verify decoding was successful
	assert.True(t, decodeComplete, "Should have decoded the message successfully")
	assert.Equal(t, testData, receivedData, "Decoded data should match original data")
	
	// Verify metrics
	metrics := transport.GetMetrics()
	assert.NotZero(t, metrics["packets_encoded"], "Should have encoded packets")
}

// TestCrossRegionRLNCAttestation tests RLNC-based attestation exchange
func TestCrossRegionRLNCAttestation(t *testing.T) {
	// Create RLNC transport for testing
	config := &RLNCTransportConfig{
		GenSize:       8,
		MinRedundancy: 1.5,
		MaxRedundancy: 3.0,
		AdaptiveMode:  true,
		Enabled:       true,
	}

	// Create base transport
	baseTrans := NewDefaultBaseTransport()
	baseTrans.SetRegionConnected("target-region", true)
	baseTrans.SetRegionHealth("target-region", 0.8)

	// Create RLNC coordinator with properly configured transport
	coordConfig := &CoordinatorConfig{
		RegionID: "test-region",
	}
	
	// Create RLNC transport and coordinator
	transport := NewRLNCTransport(baseTrans, config)
	coordinator := &RLNCCoordinator{
		regionID:        coordConfig.RegionID,
		rlncConfig:      config,
		rlncTransport:   transport,
		rlncMetrics:     &RLNCMetrics{RegionReliabilityScores: make(map[string]float64)},
		connectedRegions: make(map[string]bool),
	}

	// Initialize the callback to simulate receiving side
	receivedAttestation := false
	baseTrans.SetReceiveCallback(func(sourceRegion string, packet *RLNCPacket) {
		// Simply mark that we received the packet
		receivedAttestation = true
	})

	// Create mock attestation data
	attestation := &AttestationDataWithRLNC{
		ID:        "test-attestation",
		Timestamp: time.Now().Unix(),
		Data:      []byte("mock attestation data"),
		TEEType:   "SGX",
		RLNCInfo: RLNCMetadata{
			GenSize:      8,
			Redundancy:   1.5,
			AdaptiveMode: true,
		},
	}

	// Send attestation
	ctx := context.Background()
	resp, err := coordinator.ExchangeAttestationWithRLNC(ctx, "target-region", attestation)
	
	// Check if the send was successful
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Contains(t, resp.Message, "RLNC")
	
	// Give it a moment to process
	time.Sleep(100 * time.Millisecond)
	
	// Verify we received the attestation
	assert.True(t, receivedAttestation, "Should have received the attestation packet")
}

// Utility function to generate random float between 0 and 1
func randFloat() float64 {
	return float64(time.Now().UnixNano() % 100) / 100.0
}
