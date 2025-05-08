package xregion

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// RLNCTransportConfig contains configuration for RLNC-based cross-region transport
type RLNCTransportConfig struct {
	// Generation size for RLNC encoding/decoding
	GenSize int
	// Minimum redundancy factor (1.0 = no redundancy)
	MinRedundancy float64
	// Maximum redundancy factor for adaptive mode
	MaxRedundancy float64
	// Whether to use adaptive mode to adjust to network conditions
	AdaptiveMode bool
	// Maximum packet size for chunking large messages
	MaxPacketSize int
	// Whether RLNC is enabled
	Enabled bool
}

// DefaultRLNCTransportConfig returns the default config for cross-region RLNC
func DefaultRLNCTransportConfig() *RLNCTransportConfig {
	return &RLNCTransportConfig{
		GenSize:       12,       // Larger generation size for cross-region
		MinRedundancy: 2.0,      // Higher minimum redundancy for cross-region
		MaxRedundancy: 4.0,      // Higher maximum redundancy for cross-region
		AdaptiveMode:  true,
		MaxPacketSize: 16 * 1024, // 16KB packet size
		Enabled:       true,
	}
}

// RLNCTransport implements resilient cross-region transport using RLNC
type RLNCTransport struct {
	// Configuration for RLNC
	config *RLNCTransportConfig
	
	// Underlying transport layer (non-RLNC)
	baseTransport interface{} // Would be replaced with actual transport interface
	
	// Network health metrics
	metrics struct {
		mu              sync.RWMutex
		packetsEncoded  int64
		packetsDecoded  int64
		recoverySuccess int64
		recoveryFailure int64
		netHealth       map[string]float64 // Health by region ID
	}
	
	// Message tracking
	messages struct {
		mu            sync.RWMutex
		messageByID   map[string][]byte
		timeouts      map[string]time.Time
		pendingRegion map[string]string // Maps message ID to region
	}
	
	// Decoding state tracking
	decodingStates map[string]*DecodingState
}

// NewRLNCTransport creates a new RLNC-based transport layer
func NewRLNCTransport(baseTransport interface{}, config *RLNCTransportConfig) *RLNCTransport {
	if config == nil {
		config = DefaultRLNCTransportConfig()
	}
	
	transport := &RLNCTransport{
		config:        config,
		baseTransport: baseTransport,
		decodingStates: make(map[string]*DecodingState),
	}
	
	// Initialize maps
	transport.metrics.netHealth = make(map[string]float64)
	transport.messages.messageByID = make(map[string][]byte)
	transport.messages.timeouts = make(map[string]time.Time)
	transport.messages.pendingRegion = make(map[string]string)
	
	// Start cleanup goroutine
	go transport.cleanupExpiredMessages()
	
	return transport
}

// cleanupExpiredMessages periodically removes expired messages
func (t *RLNCTransport) cleanupExpiredMessages() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for range ticker.C {
		now := time.Now()
		t.messages.mu.Lock()
		
		for id, expiry := range t.messages.timeouts {
			if now.After(expiry) {
				// Remove expired message
				delete(t.messages.messageByID, id)
				delete(t.messages.timeouts, id)
				delete(t.messages.pendingRegion, id)
				
				// Also remove decoding state if it exists
				delete(t.decodingStates, id)
			}
		}
		
		t.messages.mu.Unlock()
	}
}

// BaseTransport defines the interface for the underlying transport layer
type BaseTransport interface {
	// SendPacket sends a single packet to a target region
	SendPacket(targetRegion string, packet []byte) error
	// GetRegionHealth returns the health status of a region (0.0-1.0)
	GetRegionHealth(region string) float64
	// IsRegionConnected returns whether a region is currently connected
	IsRegionConnected(region string) bool
}

// defaultBaseTransport provides a basic implementation for testing
type defaultBaseTransport struct {
	connectedRegions map[string]bool
	regionHealth     map[string]float64
	receiveCallback  func(sourceRegion string, packet *RLNCPacket)
}

// NewDefaultBaseTransport creates a basic transport for testing
func NewDefaultBaseTransport() *defaultBaseTransport {
	return &defaultBaseTransport{
		connectedRegions: make(map[string]bool),
		regionHealth:     make(map[string]float64),
	}
}

// SendPacket sends a packet to a target region
func (t *defaultBaseTransport) SendPacket(targetRegion string, packet []byte) error {
	if !t.IsRegionConnected(targetRegion) {
		return fmt.Errorf("region %s is not connected", targetRegion)
	}
	
	// Simulate network behavior based on health
	health := t.GetRegionHealth(targetRegion)
	
	// Simulate packet loss
	if rand.Float64() > health {
		// Packet lost
		return nil
	}
	
	// Deserialize packet
	rlncPacket := &RLNCPacket{}
	err := json.Unmarshal(packet, rlncPacket)
	if err != nil {
		return fmt.Errorf("failed to deserialize packet: %w", err)
	}
	
	// Deliver packet if callback is set
	if t.receiveCallback != nil {
		t.receiveCallback(targetRegion, rlncPacket)
	}
	
	return nil
}

// GetRegionHealth returns region health (default: 0.8)
func (t *defaultBaseTransport) GetRegionHealth(region string) float64 {
	health, exists := t.regionHealth[region]
	if !exists {
		return 0.8 // Default health
	}
	return health
}

// IsRegionConnected returns whether a region is connected
func (t *defaultBaseTransport) IsRegionConnected(region string) bool {
	connected, exists := t.connectedRegions[region]
	return exists && connected
}

// SetRegionHealth sets the health of a region
func (t *defaultBaseTransport) SetRegionHealth(region string, health float64) {
	t.regionHealth[region] = health
}

// SetRegionConnected sets whether a region is connected
func (t *defaultBaseTransport) SetRegionConnected(region string, connected bool) {
	t.connectedRegions[region] = connected
}

// SetReceiveCallback sets the callback for received packets
func (t *defaultBaseTransport) SetReceiveCallback(callback func(sourceRegion string, packet *RLNCPacket)) {
	t.receiveCallback = callback
}

// SendToRegion sends a message to another region with RLNC encoding for resilience
func (t *RLNCTransport) SendToRegion(ctx context.Context, targetRegion string, messageID string, data []byte) error {
	// Skip RLNC if disabled
	if !t.config.Enabled {
		// Use base transport directly if available
		baseTransport, ok := t.baseTransport.(BaseTransport)
		if !ok {
			return fmt.Errorf("base transport not available")
		}
		
		// Create a simple packet without RLNC encoding
		packet := &RLNCPacket{
			MessageID:    messageID,
			PacketNumber: 0,
			TotalPackets: 1,
			Data:         data,
			IsRLNC:       false,
		}
		
		// Serialize and send packet
		packetData, err := json.Marshal(packet)
		if err != nil {
			return fmt.Errorf("failed to serialize packet: %w", err)
		}
		
		return baseTransport.SendPacket(targetRegion, packetData)
	}
	
	// Store original message
	t.messages.mu.Lock()
	t.messages.messageByID[messageID] = data
	t.messages.pendingRegion[messageID] = targetRegion
	// Set timeout for 10 minutes 
	t.messages.timeouts[messageID] = time.Now().Add(10 * time.Minute)
	t.messages.mu.Unlock()
	
	// Calculate current redundancy based on network health
	redundancy := t.calculateRedundancyForRegion(targetRegion)
	
	// Instead of using the RLNC encoder directly, we'll simulate the packet generation
	// In a real implementation, you would use the actual encoder
	
	// Calculate number of packets to send based on redundancy
	packetsToSend := int(float64(t.config.GenSize) * redundancy)
	
	// Send simulated packets
	for i := 0; i < packetsToSend; i++ {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		
		// Create a real RLNC-encoded packet
		// Generate a deterministic but unique coefficient vector for this packet
		coeffVector := generateCoefficientVector(t.config.GenSize, messageID, i)
		
		// Divide the original data into generation-sized chunks
		chunks := splitIntoChunks(data, t.config.GenSize)
		
		// Perform linear combination based on coefficient vector
		encodedData := linearCombine(chunks, coeffVector)
		
		// Create packet structure for transmitting
		packetToSend := &RLNCPacket{
			MessageID:    messageID,
			PacketNumber: i,
			TotalPackets: packetsToSend,
			Data:         encodedData,
			Coefficients: coeffVector,
			IsRLNC:       true,
		}
		
		// Get the base transport
		baseTransport, ok := t.baseTransport.(BaseTransport)
		if !ok {
			return fmt.Errorf("base transport not properly configured")
		}
		
		// Serialize the packet
		packetData, err := json.Marshal(packetToSend)
		if err != nil {
			return fmt.Errorf("failed to serialize packet: %w", err)
		}
		
		// Send the packet
		err = baseTransport.SendPacket(targetRegion, packetData)
		if err != nil {
			// Log error but continue with other packets
			fmt.Printf("Failed to send packet %d for message %s: %v\n", i, messageID, err)
			continue
		}
		
		// Update metrics
		t.metrics.mu.Lock()
		t.metrics.packetsEncoded++
		t.metrics.mu.Unlock()
	}
	
	return nil
}

// DecodingState maintains the state needed for decoding a message
type DecodingState struct {
	MessageID      string
	GenSize        int
	ExpectedSize   int
	Packets        map[int]*RLNCPacket
	CoefficientMat [][]byte
	DataMat        [][]byte
	Decoded        bool
	Result         []byte
	LastUpdate     time.Time
}

// NewDecodingState creates a new decoding state
func NewDecodingState(messageID string, genSize int) *DecodingState {
	return &DecodingState{
		MessageID:    messageID,
		GenSize:      genSize,
		Packets:      make(map[int]*RLNCPacket),
		LastUpdate:   time.Now(),
		Decoded:      false,
	}
}

// ReceiveFromRegion processes an incoming RLNC packet from another region
func (t *RLNCTransport) ReceiveFromRegion(sourceRegion string, packet *RLNCPacket) ([]byte, bool, error) {
	// Skip RLNC processing if disabled or if the packet is not RLNC encoded
	if !t.config.Enabled || !packet.IsRLNC {
		// Return the packet data directly
		return packet.Data, true, nil
	}
	
	messageID := packet.MessageID
	
	// Check if we already have the original message
	t.messages.mu.RLock()
	originalMessage, exists := t.messages.messageByID[messageID]
	t.messages.mu.RUnlock()
	
	// If we have the original message, return it
	if exists {
		// Update metrics
		t.metrics.mu.Lock()
		t.metrics.packetsDecoded++
		t.metrics.recoverySuccess++
		t.metrics.mu.Unlock()
		
		return originalMessage, true, nil
	}
	
	// We don't have the original message, so we need to decode it
	// Get or create the decoding state for this message
	t.messages.mu.Lock()
	decodingState, exists := t.decodingStates[messageID]
	if !exists {
		// Create a new decoding state
		decodingState = NewDecodingState(messageID, packet.GenSize)
		t.decodingStates[messageID] = decodingState
	}
	
	// Add the packet to the decoding state
	decodingState.Packets[packet.PacketNumber] = packet
	decodingState.LastUpdate = time.Now()
	
	// Update coefficient matrix
	if decodingState.CoefficientMat == nil {
		decodingState.CoefficientMat = make([][]byte, 0, decodingState.GenSize)
		decodingState.DataMat = make([][]byte, 0, decodingState.GenSize)
	}
	
	// Add this packet to our matrices if we don't already have it
	if len(decodingState.CoefficientMat) < decodingState.GenSize {
		decodingState.CoefficientMat = append(decodingState.CoefficientMat, packet.Coefficients)
		decodingState.DataMat = append(decodingState.DataMat, packet.Data)
	}
	
	// Try to decode if we have enough packets
	if len(decodingState.CoefficientMat) >= decodingState.GenSize && !decodingState.Decoded {
		decoded, err := decodeRLNC(decodingState.CoefficientMat, decodingState.DataMat, decodingState.GenSize)
		if err == nil {
			// Successfully decoded
			decodingState.Decoded = true
			decodingState.Result = reassembleChunks(decoded)
			
			// Store the result
			t.messages.messageByID[messageID] = decodingState.Result
			
			// Update metrics
			t.metrics.mu.Lock()
			t.metrics.packetsDecoded++
			t.metrics.recoverySuccess++
			t.metrics.mu.Unlock()
			
			t.messages.mu.Unlock()
			return decodingState.Result, true, nil
		}
	}
	
	// Not enough packets yet or decoding failed
	t.messages.mu.Unlock()
	
	// Update metrics
	t.metrics.mu.Lock()
	t.metrics.packetsDecoded++
	t.metrics.mu.Unlock()
	
	// Return partial data
	return packet.Data, false, nil
}

// createEncoder creates a simulated RLNC encoder for a message
// In a real implementation, this would use the actual RLNC encoder
func (t *RLNCTransport) createEncoder(messageID string, data []byte) (interface{}, error) {
	// This is a mock implementation - in a real scenario, you would actually
	// create and return an RLNC encoder
	
	// Return a simple wrapper that encapsulates the data
	return &mockEncoder{
		data:      data,
		genSize:   t.config.GenSize,
		messageID: messageID,
	}, nil
}

// mockEncoder is a simple mock implementation of an RLNC encoder
type mockEncoder struct {
	data      []byte
	genSize   int
	messageID string
	packetNum int
}

// EncodePacket creates a simulated encoded packet
func (e *mockEncoder) EncodePacket() ([]byte, error) {
	// In a real implementation, this would create a linearly combined packet
	// For our mock, we'll just return a segment of the original data
	packetSize := len(e.data) / e.genSize
	if packetSize < 1 {
		packetSize = 1
	}

	// Create a simulated packet
	packet := make([]byte, packetSize)
	start := (e.packetNum % e.genSize) * packetSize
	end := start + packetSize
	if end > len(e.data) {
		end = len(e.data)
	}
	
	copy(packet, e.data[start:end])
	e.packetNum++
	
	return packet, nil
}

// calculateRedundancyForRegion calculates the appropriate redundancy for a region
func (t *RLNCTransport) calculateRedundancyForRegion(region string) float64 {
	// If adaptive mode is disabled, return the minimum redundancy
	if !t.config.AdaptiveMode {
		return t.config.MinRedundancy
	}
	
	// Get network health for the region
	t.metrics.mu.RLock()
	health, exists := t.metrics.netHealth[region]
	t.metrics.mu.RUnlock()
	
	// If no health data, use default
	if !exists {
		return (t.config.MinRedundancy + t.config.MaxRedundancy) / 2
	}
	
	// Scale redundancy based on network health (0.0-1.0)
	// Lower health = higher redundancy
	scaledRedundancy := t.config.MinRedundancy + 
		(t.config.MaxRedundancy - t.config.MinRedundancy) * (1.0 - health)
	
	return scaledRedundancy
}

// UpdateNetworkHealth updates the network health metrics for a region
func (t *RLNCTransport) UpdateNetworkHealth(region string, health float64) {
	t.metrics.mu.Lock()
	defer t.metrics.mu.Unlock()
	t.metrics.netHealth[region] = health
}

// GetMetrics returns the current RLNC metrics
func (t *RLNCTransport) GetMetrics() map[string]interface{} {
	t.metrics.mu.RLock()
	defer t.metrics.mu.RUnlock()
	
	// Copy metrics to a map
	metrics := map[string]interface{}{
		"packets_encoded":   t.metrics.packetsEncoded,
		"packets_decoded":   t.metrics.packetsDecoded,
		"recovery_success":  t.metrics.recoverySuccess,
		"recovery_failure":  t.metrics.recoveryFailure,
		"network_health":    make(map[string]float64),
	}
	
	// Copy network health
	for region, health := range t.metrics.netHealth {
		metrics["network_health"].(map[string]float64)[region] = health
	}
	
	return metrics
}

// generateCoefficientVector creates a deterministic but unique coefficient vector for RLNC encoding
// In production, this would use a secure random number generator or a deterministic
// PRNG initialized with a secure seed
func generateCoefficientVector(genSize int, messageID string, packetIndex int) []byte {
	// Create a deterministic seed based on message ID and packet index
	seed := int64(0)
	for i, c := range messageID {
		seed += int64(c) * int64(i+1)
	}
	seed += int64(packetIndex) * 1000
	
	// Initialize a deterministic random source
	rng := rand.New(rand.NewSource(seed))
	
	// Generate coefficients
	coeffs := make([]byte, genSize)
	for i := 0; i < genSize; i++ {
		// Use values 1-255 to avoid zero coefficients
		coeffs[i] = byte(1 + rng.Intn(254))
	}
	
	return coeffs
}

// splitIntoChunks divides the data into chunks of equal size for RLNC processing
func splitIntoChunks(data []byte, genSize int) [][]byte {
	if len(data) == 0 {
		return [][]byte{}
	}
	
	// Calculate chunk size (divide total length by generation size)
	// This is a simple approach; a real implementation might use fixed-size chunks
	chunkSize := (len(data) + genSize - 1) / genSize
	
	// Create the chunks
	chunks := make([][]byte, genSize)
	for i := 0; i < genSize; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > len(data) {
			end = len(data)
		}
		
		if start < len(data) {
			chunks[i] = make([]byte, chunkSize)
			copy(chunks[i], data[start:end])
			// Pad the last chunk if needed
			if end-start < chunkSize {
				// Remaining bytes are left as zeros
			}
		} else {
			// Empty chunk (all zeros)
			chunks[i] = make([]byte, chunkSize)
		}
	}
	
	return chunks
}

// linearCombine performs the linear combination of chunks based on coefficients
func linearCombine(chunks [][]byte, coefficients []byte) []byte {
	if len(chunks) == 0 || len(coefficients) == 0 || len(chunks) != len(coefficients) {
		return nil
	}
	
	chunkSize := len(chunks[0])
	result := make([]byte, chunkSize)
	
	// Perform Galois field operations for each byte position across all chunks
	for i := 0; i < chunkSize; i++ {
		for j := 0; j < len(chunks); j++ {
			if i < len(chunks[j]) {
				// Multiply coefficient by data byte and add to result (XOR in GF(2^8))
				// In GF(2^8), addition is XOR and multiplication is more complex
				// For simplicity, we're using a simplified multiplication here
				result[i] ^= gfMultiply(coefficients[j], chunks[j][i])
			}
		}
	}
	
	return result
}

// gfMultiply performs multiplication in GF(2^8)
// This is a simplified implementation; production code would use look-up tables or optimized algorithms
func gfMultiply(a, b byte) byte {
	result := byte(0)
	for i := 0; i < 8; i++ {
		if (b & 1) != 0 {
			result ^= a
		}
		
		// Check if high bit is set before shifting
		highBit := (a & 0x80) != 0
		
		// Shift a left by 1 bit
		a <<= 1
		
		// If high bit was set, XOR with the reducing polynomial
		if highBit {
			a ^= 0x1D // Standard reducing polynomial for AES (x^8 + x^4 + x^3 + x + 1)
		}
		
		b >>= 1
	}
	
	return result
}

// decodeRLNC attempts to decode RLNC-encoded chunks using Gaussian elimination
func decodeRLNC(coefficientMatrix [][]byte, dataMatrix [][]byte, genSize int) ([][]byte, error) {
	if len(coefficientMatrix) < genSize || len(dataMatrix) < genSize {
		return nil, fmt.Errorf("insufficient packets for decoding")
	}
	
	// Create copies of the matrices for processing
	coeffMat := make([][]byte, genSize)
	dataMat := make([][]byte, genSize)
	for i := 0; i < genSize; i++ {
		coeffMat[i] = make([]byte, genSize)
		copy(coeffMat[i], coefficientMatrix[i])
		
		dataMat[i] = make([]byte, len(dataMatrix[i]))
		copy(dataMat[i], dataMatrix[i])
	}
	
	// Perform Gaussian elimination
	for i := 0; i < genSize; i++ {
		// Find pivot
		pivotRow := -1
		for j := i; j < genSize; j++ {
			if coeffMat[j][i] != 0 {
				pivotRow = j
				break
			}
		}
		
		if pivotRow == -1 {
			return nil, fmt.Errorf("singular coefficient matrix")
		}
		
		// Swap rows if needed
		if pivotRow != i {
			coeffMat[i], coeffMat[pivotRow] = coeffMat[pivotRow], coeffMat[i]
			dataMat[i], dataMat[pivotRow] = dataMat[pivotRow], dataMat[i]
		}
		
		// Normalize pivot row
		pivot := coeffMat[i][i]
		invPivot := gfInverse(pivot)
		for j := i; j < genSize; j++ {
			coeffMat[i][j] = gfMultiply(coeffMat[i][j], invPivot)
		}
		
		for j := 0; j < len(dataMat[i]); j++ {
			dataMat[i][j] = gfMultiply(dataMat[i][j], invPivot)
		}
		
		// Eliminate other rows
		for j := 0; j < genSize; j++ {
			if j != i && coeffMat[j][i] != 0 {
				eliminator := coeffMat[j][i]
				
				for k := i; k < genSize; k++ {
					coeffMat[j][k] ^= gfMultiply(eliminator, coeffMat[i][k])
				}
				
				for k := 0; k < len(dataMat[j]); k++ {
					dataMat[j][k] ^= gfMultiply(eliminator, dataMat[i][k])
				}
			}
		}
	}
	
	return dataMat, nil
}

// gfInverse computes the multiplicative inverse in GF(2^8)
// This is a naive implementation; production code would use lookup tables
func gfInverse(value byte) byte {
	if value == 0 {
		return 0 // No inverse for 0
	}
	
	// Extended Euclidean algorithm or brute force approach
	for i := byte(1); i < 255; i++ {
		if gfMultiply(value, i) == 1 {
			return i
		}
	}
	
	// This should never happen for non-zero elements in GF(2^8)
	return 0
}

// reassembleChunks combines the decoded chunks back into the original message
func reassembleChunks(chunks [][]byte) []byte {
	if len(chunks) == 0 {
		return []byte{}
	}
	
	chunkSize := len(chunks[0])
	totalSize := chunkSize * len(chunks)
	
	// Create the result buffer
	result := make([]byte, totalSize)
	
	// Copy each chunk into the appropriate position
	for i, chunk := range chunks {
		copy(result[i*chunkSize:], chunk)
	}
	
	// Remove trailing zeros (padding)
	for i := totalSize - 1; i >= 0; i-- {
		if result[i] != 0 {
			result = result[:i+1]
			break
		}
	}
	
	return result
}

// RLNCPacket represents an RLNC-encoded packet for cross-region transport
type RLNCPacket struct {
	// ID of the message this packet belongs to
	MessageID string `json:"message_id"`
	// Packet number in the sequence
	PacketNumber int `json:"packet_number"`
	// Total number of packets in the sequence
	TotalPackets int `json:"total_packets"`
	// The actual packet data
	Data []byte `json:"data"`
	// Whether this packet is RLNC encoded
	IsRLNC bool `json:"is_rlnc"`
	// Coefficient vector for this packet (only used when IsRLNC is true)
	Coefficients []byte `json:"coefficients,omitempty"`
	// Generation size (only used when IsRLNC is true)
	GenSize int `json:"gen_size,omitempty"`
	// Timestamp for metrics
	Timestamp int64 `json:"timestamp,omitempty"`
}
