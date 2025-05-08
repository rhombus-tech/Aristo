// Package rlnc provides integration between RLNC and Avalanche
package rlnc

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rhombus-tech/vm/tee/rlnc/core"
	"github.com/rhombus-tech/vm/tee/rlnc/mesh"
	"github.com/rhombus-tech/vm/tee/rlnc/security"
)

var (
	// Error definitions
	ErrAvalancheConnectionFailed = errors.New("failed to connect to Avalanche")
	ErrInvalidAvalancheResponse  = errors.New("invalid response from Avalanche")
	ErrNetworkPartition          = errors.New("network partition detected")
)

// AvalancheMeshBridge integrates the RLNC mesh client with Avalanche
// This provides resilience against partial network failures while
// maintaining your "100ms and regulated" value proposition
type AvalancheMeshBridge struct {
	// Underlying regional mesh client
	meshClient *mesh.RegionalMeshClient
	
	// Coordinator URL (local or remote)
	coordinatorURL string
	
	// Region ID
	regionID string
	
	// Map of TEE pairs for cross-attestation
	teePairs sync.Map

	// RLNC configuration settings
	rlncConfig struct {
		enabled             bool
		adaptiveMode        bool
		minRedundancyFactor float64
		maxRedundancyFactor float64
		adaptiveThreshold   float64
		currentRedundancy   float64
		mu                  sync.RWMutex
	}
	
	// Performance metrics for Avalanche integrations
	metrics struct {
		TotalTransactions    int64
		SuccessfulTxs        int64
		FailedTxs            int64
		AvgLatencyMs         float64
		RecoveredFromFailure int64
		mu                   sync.Mutex
	}
	
	// TEE attestation verifier
	attestationVerifier *security.RLNCAttestationVerifier
}

// TEEPair represents a pair of TEEs (SGX and SEV) for cross-attestation
type TEEPair struct {
	SGXID string
	SEVID string
}

// NewAvalancheMeshBridge creates a new bridge between Avalanche and the TEE mesh network
func NewAvalancheMeshBridge(
	ctx context.Context,
	coordinatorURL string,
	regionID string,
	localTEEType security.TEEType,
	attestationSvc security.AttestationService,
	rlncEnabled bool,
) (*AvalancheMeshBridge, error) {
	// Create the underlying mesh client
	meshClient, err := mesh.NewRegionalMeshClient(ctx, regionID, localTEEType, attestationSvc)
	if err != nil {
		return nil, fmt.Errorf("failed to create mesh client: %w", err)
	}
	
	// Create the attestation verifier
	// Enable RLNC for attestation by default for enhanced reliability
	attVerifier := security.NewRLNCAttestationVerifier(attestationSvc, true, true)
	
	bridge := &AvalancheMeshBridge{
		meshClient:          meshClient,
		coordinatorURL:      coordinatorURL,
		regionID:            regionID,
		attestationVerifier: attVerifier,
	}
	
	// Initialize RLNC configuration with default values
	bridge.rlncConfig.enabled = rlncEnabled
	bridge.rlncConfig.adaptiveMode = true
	bridge.rlncConfig.minRedundancyFactor = 1.25 // 25% redundancy minimum
	bridge.rlncConfig.maxRedundancyFactor = 3.0  // 200% redundancy maximum
	bridge.rlncConfig.adaptiveThreshold = 0.05   // 5% packet loss triggers adaptation
	bridge.rlncConfig.currentRedundancy = 1.5    // Start with 50% redundancy
	
	return bridge, nil
}

// RegisterTEEPair registers a TEE pair for cross-attestation
func (b *AvalancheMeshBridge) RegisterTEEPair(pairID string, sgxID, sevID string) {
	pair := TEEPair{
		SGXID: sgxID,
		SEVID: sevID,
	}
	
	b.teePairs.Store(pairID, pair)
	
	// Register the TEEs with the mesh client for proper typing
	b.meshClient.RegisterTEE(sgxID, security.TEETypeSGX)
	b.meshClient.RegisterTEE(sevID, security.TEETypeSEV)
}

// SetRLNCConfig configures the RLNC parameters for the bridge
func (b *AvalancheMeshBridge) SetRLNCConfig(enabled, adaptiveMode bool, minRedundancy, maxRedundancy float64) {
	b.rlncConfig.mu.Lock()
	defer b.rlncConfig.mu.Unlock()
	
	b.rlncConfig.enabled = enabled
	b.rlncConfig.adaptiveMode = adaptiveMode
	
	// Validate and set redundancy parameters
	if minRedundancy >= 1.0 && minRedundancy <= maxRedundancy {
		b.rlncConfig.minRedundancyFactor = minRedundancy
	}
	
	if maxRedundancy >= minRedundancy {
		b.rlncConfig.maxRedundancyFactor = maxRedundancy
	}
	
	// Reset current redundancy to minimum
	b.rlncConfig.currentRedundancy = minRedundancy
}

// GetRLNCConfig returns the current RLNC configuration
func (b *AvalancheMeshBridge) GetRLNCConfig() map[string]interface{} {
	b.rlncConfig.mu.RLock()
	defer b.rlncConfig.mu.RUnlock()
	
	return map[string]interface{}{
		"enabled":              b.rlncConfig.enabled,
		"adaptive_mode":        b.rlncConfig.adaptiveMode,
		"min_redundancy":       b.rlncConfig.minRedundancyFactor,
		"max_redundancy":       b.rlncConfig.maxRedundancyFactor,
		"current_redundancy":   b.rlncConfig.currentRedundancy,
		"adaptive_threshold":   b.rlncConfig.adaptiveThreshold,
	}
}

// adjustRedundancyBasedOnNetworkConditions dynamically adjusts the RLNC redundancy
// factor based on recent network performance to optimize resilience vs overhead
func (b *AvalancheMeshBridge) adjustRedundancyBasedOnNetworkConditions() float64 {
	// Only adjust if adaptive mode is enabled
	b.rlncConfig.mu.RLock()
	if !b.rlncConfig.adaptiveMode || !b.rlncConfig.enabled {
		// Return current value without adjusting
		currentRedundancy := b.rlncConfig.currentRedundancy
		b.rlncConfig.mu.RUnlock()
		return currentRedundancy
	}
	b.rlncConfig.mu.RUnlock()
	
	// Get current network health metrics
	health, err := b.meshClient.GetNetworkHealthSummary(b.regionID)
	if err != nil {
		// If we can't get network health, use default redundancy
		b.rlncConfig.mu.RLock()
		currentRedundancy := b.rlncConfig.currentRedundancy
		b.rlncConfig.mu.RUnlock()
		return currentRedundancy
	}
	
	// Calculate new redundancy factor based on packet loss rate
	// Higher packet loss = higher redundancy
	newRedundancy := 1.0 + health.PacketLossRate*5.0 // Base formula
	
	// Apply constraints
	b.rlncConfig.mu.Lock()
	defer b.rlncConfig.mu.Unlock()
	
	if newRedundancy < b.rlncConfig.minRedundancyFactor {
		newRedundancy = b.rlncConfig.minRedundancyFactor
	} else if newRedundancy > b.rlncConfig.maxRedundancyFactor {
		newRedundancy = b.rlncConfig.maxRedundancyFactor
	}
	
	// Smooth transition (don't change too abruptly)
	b.rlncConfig.currentRedundancy = b.rlncConfig.currentRedundancy*0.7 + newRedundancy*0.3
	
	return b.rlncConfig.currentRedundancy
}

// SubmitTransactionResilient submits a transaction to Avalanche with resilience
// against partial network failures using RLNC
func (b *AvalancheMeshBridge) SubmitTransactionResilient(
	ctx context.Context,
	txData []byte,
	timeout time.Duration,
) (string, error) {
	startTime := time.Now()
	
	// Track metrics
	defer func() {
		latency := float64(time.Since(startTime).Milliseconds())
		b.metrics.mu.Lock()
		b.metrics.TotalTransactions++
		
		// Update average latency using exponential moving average
		if b.metrics.AvgLatencyMs == 0 {
			b.metrics.AvgLatencyMs = latency
		} else {
			b.metrics.AvgLatencyMs = b.metrics.AvgLatencyMs*0.95 + latency*0.05
		}
		b.metrics.mu.Unlock()
	}()
	
	// Get all TEE pairs for distribution
	var teePairsList []TEEPair
	b.teePairs.Range(func(_, value interface{}) bool {
		pair := value.(TEEPair)
		teePairsList = append(teePairsList, pair)
		return true
	})
	
	if len(teePairsList) == 0 {
		return "", fmt.Errorf("no TEE pairs registered")
	}
	
	// Use RLNC to distribute the transaction to multiple TEE pairs
	// This provides resilience against partial network failures
	generationSize := len(teePairsList)
	if generationSize > 32 {
		generationSize = 32 // Cap to reasonable size
	}
	
	// Create an encoder for the transaction data
	generationID := generateUniqueID()
	securityParams := core.DefaultSecurityParams()
	encoder, err := core.NewEncoder(generationSize, len(txData), securityParams, generationID)
	if err != nil {
		return "", fmt.Errorf("failed to create encoder: %w", err)
	}
	
	// Add the transaction data as a single packet
	// In a real implementation, you might split large transactions
	if err := encoder.AddPacket(txData); err != nil {
		return "", fmt.Errorf("failed to encode transaction: %w", err)
	}
	
	// Track which TEEs have successfully processed the transaction
	successfulTEEs := sync.Map{}
	failedTEEs := sync.Map{}
	
	// Distribute encoded packets to TEE pairs
	var wg sync.WaitGroup
	for i, pair := range teePairsList[:generationSize] {
		wg.Add(1)
		go func(index int, teePair TEEPair) {
			defer wg.Done()
			
			// Encode a packet for this TEE pair
			encodedPacket, err := encoder.EncodePacket()
			if err != nil {
				failedTEEs.Store(teePair, err)
				return
			}
			
			// Try SGX first, fall back to SEV if SGX fails
			ctx, cancel := context.WithTimeout(ctx, timeout/2) // Half timeout for first attempt
			defer cancel()
			
			err = b.meshClient.SendDataResilient(ctx, teePair.SGXID, encodedPacket, timeout/2)
			if err != nil {
				// Fall back to SEV
				ctx, cancel := context.WithTimeout(ctx, timeout/2)
				defer cancel()
				
				err = b.meshClient.SendDataResilient(ctx, teePair.SEVID, encodedPacket, timeout/2)
				if err != nil {
					failedTEEs.Store(teePair, err)
					return
				}
			}
			
			successfulTEEs.Store(teePair, true)
		}(i, pair)
	}
	
	// Wait for all distributions to complete or timeout
	wg.Wait()
	
	// Count successes and failures
	successCount := 0
	failureCount := 0
	
	successfulTEEs.Range(func(_, _ interface{}) bool {
		successCount++
		return true
	})
	
	failedTEEs.Range(func(_, _ interface{}) bool {
		failureCount++
		return true
	})
	
	// Check if RLNC is enabled
	b.rlncConfig.mu.RLock()
	rlncEnabled := b.rlncConfig.enabled
	b.rlncConfig.mu.RUnlock()
	
	// If RLNC is enabled but transaction failed, try recovery
	if rlncEnabled && successCount < generationSize/2 && successCount > 0 {
		// Attempt RLNC-based recovery for partially failed transaction broadcast
		// Pass a pointer to the map to avoid copying the mutex
		recoverySuccess, recoveredTxID := b.attemptRLNCRecovery(ctx, txData, generationID, &successfulTEEs, timeout)
		if recoverySuccess {
			// Update metrics after successful recovery
			b.metrics.mu.Lock()
			b.metrics.SuccessfulTxs++
			b.metrics.RecoveredFromFailure++
			b.metrics.mu.Unlock()
			
			// If network conditions required recovery, increase redundancy for future transactions
			if b.rlncConfig.adaptiveMode {
				b.adjustRedundancyBasedOnNetworkConditions()
			}
			
			return recoveredTxID, nil
		}
	}
	
	// If not enough TEEs processed the transaction and recovery failed, consider it a failure
	// We need at least 50% of TEEs to have succeeded for consensus
	if successCount < generationSize/2 {
		b.metrics.mu.Lock()
		b.metrics.FailedTxs++
		b.metrics.mu.Unlock()
		
		return "", fmt.Errorf("%w: only %d/%d TEEs processed the transaction", 
			ErrNetworkPartition, successCount, generationSize)
	}
	
	// At this point, we've successfully distributed the transaction to enough TEEs
	// In a real implementation, we would wait for consensus and return the txID
	
	// For now, just return a simulated success
	txID := fmt.Sprintf("tx-%x", generationID)
	
	// Track metrics
	b.metrics.mu.Lock()
	b.metrics.SuccessfulTxs++
	if failureCount > 0 {
		b.metrics.RecoveredFromFailure++
	}
	b.metrics.mu.Unlock()
	
	return txID, nil
}

// attemptRLNCRecovery tries to recover a partially failed transaction
// using RLNC decoding from the successful nodes
func (b *AvalancheMeshBridge) attemptRLNCRecovery(
	ctx context.Context,
	originalTxData []byte,
	generationID []byte,
	successfulTEEs *sync.Map, // Use pointer to avoid copying the mutex
	timeout time.Duration,
) (bool, string) {
	// Create a decoder for recovery
	securityParams := core.DefaultSecurityParams()
	// Adjust parameters based on estimated generation size
	estimatedGenSize := 8 // Start with a conservative estimate
	estimatedPacketSize := len(originalTxData)
	decoder, err := core.NewDecoder(estimatedGenSize, estimatedPacketSize, securityParams)
	if err != nil {
		return false, ""
	}
	
	// Collect encoded packets from successful TEEs
	packetsCollected := 0
	encodedData := make([][]byte, 0)
	
	// Use pointer to sync.Map to avoid copying mutex
	successfulTEEs.Range(func(keyObj, _ interface{}) bool {
		teePair := keyObj.(TEEPair)

		// Try to collect encoded data from this TEE
		ctxTimeout, cancel := context.WithTimeout(ctx, timeout/4)
		defer cancel()

		// Try both TEEs in the pair
		for _, teeID := range []string{teePair.SGXID, teePair.SEVID} {
			// Request the encoded packet from the TEE
			// Note: This would typically be implemented in the RegionalMeshClient
			// For now, we simulate this by sending a direct query to the TEE
			packet, err := b.requestEncodedPacketFromTEE(ctxTimeout, teeID, generationID)
			if err == nil && len(packet) > 0 {
				encodedData = append(encodedData, packet)
				packetsCollected++
				break // Found a packet from this pair, move to next pair
			}
		}

		return true // Continue iterating
	})
	
	// If we couldn't collect any packets, recovery failed
	if packetsCollected == 0 {
		return false, ""
	}
	
	// Add all collected packets to the decoder
	for _, packet := range encodedData {
		if err := decoder.AddPacket(packet); err != nil {
			continue // Skip invalid packets
		}
	}
	
	// Check if we can decode the original transaction
	// We need at least as many packets as the generation size
	if packetsCollected < estimatedGenSize {
		return false, ""
	}
	
	// Decode the original transaction
	decodedData, err := decoder.Decode()
	if err != nil || len(decodedData) == 0 {
		return false, ""
	}
	
	// Verify that the decoded data matches the original transaction
	// In practice, you would verify this through hashes or signatures
	// For simplicity, we're doing a direct comparison here
	if !bytes.Equal(decodedData[0], originalTxData) {
		return false, ""
	}
	
	// At this point, recovery was successful
	// In a real implementation, we would submit the recovered transaction to Avalanche
	// We would also verify the transaction through TEE attestation
	txID := fmt.Sprintf("recovered-tx-%x", generationID)
	
	return true, txID
}

// requestEncodedPacketFromTEE requests an encoded packet from a TEE for recovery
// This simulates what would be provided by the mesh client in production
func (b *AvalancheMeshBridge) requestEncodedPacketFromTEE(
	ctx context.Context,
	teeID string,
	generationID []byte,
) ([]byte, error) {
	// In a real implementation, this would send a request to the TEE to get
	// the encoded packet it has for this generation ID
	// For now, we simulate this with a placeholder
	
	// Check if we have registered this TEE by looking it up in our pairs
	var foundTEE bool
	b.teePairs.Range(func(_, value interface{}) bool {
		pair := value.(TEEPair)
		if pair.SGXID == teeID || pair.SEVID == teeID {
			foundTEE = true
			return false // Stop iterating
		}
		return true // Continue iterating
	})
	
	if !foundTEE {
		return nil, fmt.Errorf("unknown TEE ID: %s", teeID)
	}
	
	// Simulate a packet that would have been received earlier
	// In production, this would be an actual network request to retrieve
	// a stored packet from the TEE
	
	// Create a simple simulated packet (16 bytes) with the generation ID embedded
	// This is just a placeholder for demonstration purposes
	packet := make([]byte, 16 + len(generationID))
	
	// Embed generation ID and some random data
	copy(packet, generationID)
	
	// Add random coefficients (simulating RLNC encoding)
	_, err := rand.Read(packet[len(generationID):])
	if err != nil {
		return nil, fmt.Errorf("failed to generate simulated packet: %w", err)
	}
	
	return packet, nil
}

// GetMetrics returns the current performance metrics
func (b *AvalancheMeshBridge) GetMetrics() map[string]interface{} {
	b.metrics.mu.Lock()
	defer b.metrics.mu.Unlock()
	
	meshMetrics := b.meshClient.GetPerformanceMetrics()
	
	// Get RLNC configuration
	rlncConfig := b.GetRLNCConfig()
	
	return map[string]interface{}{
		"total_transactions":     b.metrics.TotalTransactions,
		"successful_transactions": b.metrics.SuccessfulTxs,
		"failed_transactions":    b.metrics.FailedTxs,
		"avg_latency_ms":         b.metrics.AvgLatencyMs,
		"recovered_failures":     b.metrics.RecoveredFromFailure,
		"encoded_packets":        meshMetrics.EncodedPackets,
		"decoded_packets":        meshMetrics.DecodedPackets,
		"avg_encoding_time_us":   meshMetrics.AvgEncodingTimeUs,
		"avg_decoding_time_us":   meshMetrics.AvgDecodingTimeUs,
		"rlnc_enabled":           rlncConfig["enabled"],
		"rlnc_adaptive_mode":     rlncConfig["adaptive_mode"],
		"rlnc_current_redundancy": rlncConfig["current_redundancy"],
	}
}

// GetNetworkHealthSummary returns a summary of the network health
func (b *AvalancheMeshBridge) GetNetworkHealthSummary() map[string]interface{} {
	health, err := b.meshClient.GetNetworkHealthSummary(b.regionID)
	if err != nil {
		return map[string]interface{}{
			"error": err.Error(),
		}
	}
	
	return map[string]interface{}{
		"packet_loss_rate":   health.PacketLossRate,
		"average_latency_ms": health.AverageLatencyMs,
		"timeout_count":      health.TimeoutCount,
		"resilience_mode":    int(health.CurrentMode),
		"last_updated":       health.LastUpdated.String(),
		"success_count":      health.SuccessCount,
		"failure_count":      health.FailureCount,
	}
}

// generateUniqueID generates a unique ID for RLNC generations
func generateUniqueID() []byte {
	id := make([]byte, 16)
	// Use crypto/rand for secure randomness
	// This is critical for security in production environments
	_, err := rand.Read(id)
	if err != nil {
		// Fall back to a deterministic but still unique ID based on time
		// Not as secure, but prevents complete failure
		timeVal := time.Now().UnixNano()
		binary.BigEndian.PutUint64(id[:8], uint64(timeVal))
		binary.BigEndian.PutUint64(id[8:], uint64(timeVal>>32))
	}
	return id
}
