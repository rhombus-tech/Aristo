// Package test provides comprehensive testing for the RLNC implementation
package test

import (
	"bytes"
	"context"
	"crypto/rand"
	"testing"
	"time"

	"github.com/rhombus-tech/vm/tee/rlnc/core"
	"github.com/rhombus-tech/vm/tee/rlnc/security"
)

// TestBasicEncodeDecodeFlow tests the basic flow of encoding and decoding
func TestBasicEncodeDecodeFlow(t *testing.T) {
	// Test parameters
	genSize := 8
	packetSize := 1024
	testData := make([][]byte, genSize)
	
	// Generate random test data
	for i := 0; i < genSize; i++ {
		testData[i] = make([]byte, packetSize)
		if _, err := rand.Read(testData[i]); err != nil {
			t.Fatalf("Failed to generate random data: %v", err)
		}
	}
	
	// Create an encoder
	generationID := make([]byte, 16)
	if _, err := rand.Read(generationID); err != nil {
		t.Fatalf("Failed to generate ID: %v", err)
	}
	
	securityParams := core.DefaultSecurityParams()
	encoder, err := core.NewEncoder(genSize, packetSize, securityParams, generationID)
	if err != nil {
		t.Fatalf("Failed to create encoder: %v", err)
	}
	
	// Add packets to the encoder
	for i, packet := range testData {
		if err := encoder.AddPacket(packet); err != nil {
			t.Fatalf("Failed to add packet %d: %v", i, err)
		}
	}
	
	// Create a decoder
	decoder, err := core.NewDecoder(genSize, packetSize, securityParams)
	if err != nil {
		t.Fatalf("Failed to create decoder: %v", err)
	}
	
	// Generate and decode exactly genSize coded packets
	// This tests the minimum case where we have just enough packets
	for i := 0; i < genSize; i++ {
		// Encode a packet
		encodedPacket, err := encoder.EncodePacket()
		if err != nil {
			t.Fatalf("Failed to encode packet %d: %v", i, err)
		}
		
		// Add to decoder
		if err := decoder.AddPacket(encodedPacket); err != nil {
			t.Fatalf("Failed to add encoded packet %d to decoder: %v", i, err)
		}
	}
	
	// Check if decodable
	if !decoder.IsDecodable() {
		t.Fatalf("Decoder should be decodable with %d packets", genSize)
	}
	
	// Decode
	decodedData, err := decoder.Decode()
	if err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}
	
	// Verify decoded data
	for i, original := range testData {
		if i >= len(decodedData) {
			t.Fatalf("Missing decoded packet %d", i)
		}
		
		if !bytes.Equal(original, decodedData[i]) {
			t.Fatalf("Decoded packet %d does not match original", i)
		}
	}
}

// TestPartialPacketLoss tests resilience against packet loss
func TestPartialPacketLoss(t *testing.T) {
	// Test parameters
	genSize := 8
	packetSize := 1024
	lossRate := 0.25 // 25% packet loss
	
	testData := make([][]byte, genSize)
	
	// Generate random test data
	for i := 0; i < genSize; i++ {
		testData[i] = make([]byte, packetSize)
		if _, err := rand.Read(testData[i]); err != nil {
			t.Fatalf("Failed to generate random data: %v", err)
		}
	}
	
	// Create an encoder
	generationID := make([]byte, 16)
	if _, err := rand.Read(generationID); err != nil {
		t.Fatalf("Failed to generate ID: %v", err)
	}
	
	securityParams := core.DefaultSecurityParams()
	encoder, err := core.NewEncoder(genSize, packetSize, securityParams, generationID)
	if err != nil {
		t.Fatalf("Failed to create encoder: %v", err)
	}
	
	// Add packets to the encoder
	for i, packet := range testData {
		if err := encoder.AddPacket(packet); err != nil {
			t.Fatalf("Failed to add packet %d: %v", i, err)
		}
	}
	
	// Create a decoder
	decoder, err := core.NewDecoder(genSize, packetSize, securityParams)
	if err != nil {
		t.Fatalf("Failed to create decoder: %v", err)
	}
	
	// Calculate how many extra packets to generate for resilience
	extraPackets := int(float64(genSize) * lossRate / (1.0 - lossRate))
	totalPackets := genSize + extraPackets
	
	// Generate encoded packets
	encodedPackets := make([][]byte, totalPackets)
	for i := 0; i < totalPackets; i++ {
		var err error
		encodedPackets[i], err = encoder.EncodePacket()
		if err != nil {
			t.Fatalf("Failed to encode packet %d: %v", i, err)
		}
	}
	
	// Simulate packet loss by only using a subset of packets
	// Skip some packets to simulate loss, but ensure we have at least genSize packets
	receivedCount := 0
	for i, packet := range encodedPackets {
		// Simulate random packet loss, but ensure we use at least genSize packets
		usePacket := (float64(i) / float64(totalPackets) > lossRate) || (receivedCount < genSize && i >= totalPackets-genSize)
		
		if usePacket {
			if err := decoder.AddPacket(packet); err != nil {
				t.Fatalf("Failed to add encoded packet %d to decoder: %v", i, err)
			}
			receivedCount++
		}
	}
	
	// Ensure we received at least genSize packets
	if receivedCount < genSize {
		t.Fatalf("Test error: Not enough packets after loss simulation, got %d need %d", receivedCount, genSize)
	}
	
	// Check if decodable
	if !decoder.IsDecodable() {
		t.Fatalf("Decoder should be decodable with %d packets", receivedCount)
	}
	
	// Decode
	decodedData, err := decoder.Decode()
	if err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}
	
	// Verify decoded data
	for i, original := range testData {
		if i >= len(decodedData) {
			t.Fatalf("Missing decoded packet %d", i)
		}
		
		if !bytes.Equal(original, decodedData[i]) {
			t.Fatalf("Decoded packet %d does not match original", i)
		}
	}
}

// TestSecurityProperties tests the security properties of the RLNC implementation
func TestSecurityProperties(t *testing.T) {
	// Test parameters
	genSize := 8
	packetSize := 1024
	
	// Generate random test data
	testData := make([][]byte, genSize)
	for i := 0; i < genSize; i++ {
		testData[i] = make([]byte, packetSize)
		if _, err := rand.Read(testData[i]); err != nil {
			t.Fatalf("Failed to generate random data: %v", err)
		}
	}
	
	// Create an encoder with default security parameters
	generationID := make([]byte, 16)
	if _, err := rand.Read(generationID); err != nil {
		t.Fatalf("Failed to generate ID: %v", err)
	}
	
	securityParams := core.DefaultSecurityParams()
	encoder, err := core.NewEncoder(genSize, packetSize, securityParams, generationID)
	if err != nil {
		t.Fatalf("Failed to create encoder: %v", err)
	}
	
	// Add packets to the encoder
	for i, packet := range testData {
		if err := encoder.AddPacket(packet); err != nil {
			t.Fatalf("Failed to add packet %d: %v", i, err)
		}
	}
	
	// Generate encoded packets
	encodedPackets := make([][]byte, genSize)
	for i := 0; i < genSize; i++ {
		var err error
		encodedPackets[i], err = encoder.EncodePacket()
		if err != nil {
			t.Fatalf("Failed to encode packet %d: %v", i, err)
		}
	}
	
	// Verify no two encoded packets are identical
	// This tests the coefficient generation entropy
	for i := 0; i < genSize; i++ {
		for j := i + 1; j < genSize; j++ {
			if bytes.Equal(encodedPackets[i], encodedPackets[j]) {
				t.Fatalf("Encoded packets %d and %d are identical, suggesting poor entropy", i, j)
			}
		}
	}
	
	// Create a decoder
	decoder, err := core.NewDecoder(genSize, packetSize, securityParams)
	if err != nil {
		t.Fatalf("Failed to create decoder: %v", err)
	}
	
	// Add all encoded packets to the decoder
	for i, packet := range encodedPackets {
		if err := decoder.AddPacket(packet); err != nil {
			t.Fatalf("Failed to add encoded packet %d to decoder: %v", i, err)
		}
	}
	
	// Decode
	decodedData, err := decoder.Decode()
	if err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}
	
	// Verify decoded data
	for i, original := range testData {
		if !bytes.Equal(original, decodedData[i]) {
			t.Fatalf("Decoded packet %d does not match original", i)
		}
	}
	
	// Test attestation creation
	ctx := context.Background()
	teeType := security.TEETypeSGX
	coefficients := []byte{1, 2, 3, 4} // Example coefficients
	secParamsBytes := []byte{5, 6, 7, 8} // Example security params
	
	att, err := security.CreateEncodingAttestation(
		ctx,
		teeType,
		coefficients,
		generationID,
		genSize,
		secParamsBytes,
	)
	if err != nil {
		t.Fatalf("Failed to create attestation: %v", err)
	}
	
	// Verify attestation properties
	if att.TEEType != teeType {
		t.Fatalf("Attestation has wrong TEE type: got %v, want %v", att.TEEType, teeType)
	}
	
	if att.GenerationSize != genSize {
		t.Fatalf("Attestation has wrong generation size: got %v, want %v", att.GenerationSize, genSize)
	}
	
	if !bytes.Equal(att.GenerationID, generationID) {
		t.Fatalf("Attestation has wrong generation ID")
	}
	
	// Verify timestamp is recent
	if time.Since(att.Timestamp) > 5*time.Second {
		t.Fatalf("Attestation timestamp is too old: %v", att.Timestamp)
	}
}

// TestPerformance measures the performance of encoding and decoding
func TestPerformance(t *testing.T) {
	// Test parameters with multiple sizes to understand scaling
	testSizes := []struct {
		genSize    int
		packetSize int
		name       string
	}{
		{8, 1024, "Small-8x1KB"},
		{16, 1024, "Medium-16x1KB"},
		{32, 1024, "Large-32x1KB"},
		{8, 16 * 1024, "Small-8x16KB"},
		{16, 16 * 1024, "Medium-16x16KB"},
	}
	
	for _, size := range testSizes {
		t.Run(size.name, func(t *testing.T) {
			genSize := size.genSize
			packetSize := size.packetSize
			
			// Generate random test data
			testData := make([][]byte, genSize)
			for i := 0; i < genSize; i++ {
				testData[i] = make([]byte, packetSize)
				if _, err := rand.Read(testData[i]); err != nil {
					t.Fatalf("Failed to generate random data: %v", err)
				}
			}
			
			// Create an encoder
			generationID := make([]byte, 16)
			if _, err := rand.Read(generationID); err != nil {
				t.Fatalf("Failed to generate ID: %v", err)
			}
			
			securityParams := core.DefaultSecurityParams()
			
			// Measure encoding time
			startEncode := time.Now()
			
			encoder, err := core.NewEncoder(genSize, packetSize, securityParams, generationID)
			if err != nil {
				t.Fatalf("Failed to create encoder: %v", err)
			}
			
			// Add packets to the encoder
			for i, packet := range testData {
				if err := encoder.AddPacket(packet); err != nil {
					t.Fatalf("Failed to add packet %d: %v", i, err)
				}
			}
			
			// Generate encoded packets
			encodedPackets := make([][]byte, genSize)
			for i := 0; i < genSize; i++ {
				var err error
				encodedPackets[i], err = encoder.EncodePacket()
				if err != nil {
					t.Fatalf("Failed to encode packet %d: %v", i, err)
				}
			}
			
			encodeTime := time.Since(startEncode)
			encodeTimePerPacket := encodeTime / time.Duration(genSize)
			
			// Measure decoding time
			startDecode := time.Now()
			
			decoder, err := core.NewDecoder(genSize, packetSize, securityParams)
			if err != nil {
				t.Fatalf("Failed to create decoder: %v", err)
			}
			
			// Add all encoded packets to the decoder
			for i, packet := range encodedPackets {
				if err := decoder.AddPacket(packet); err != nil {
					t.Fatalf("Failed to add encoded packet %d to decoder: %v", i, err)
				}
			}
			
			// Decode
			decodedData, err := decoder.Decode()
			if err != nil {
				t.Fatalf("Failed to decode: %v", err)
			}
			
			decodeTime := time.Since(startDecode)
			
			// Verify decoded data
			for i, original := range testData {
				if !bytes.Equal(original, decodedData[i]) {
					t.Fatalf("Decoded packet %d does not match original", i)
				}
			}
			
			// Log performance metrics
			t.Logf("Performance (%s):", size.name)
			t.Logf("  Total encode time: %v", encodeTime)
			t.Logf("  Encode time per packet: %v", encodeTimePerPacket)
			t.Logf("  Total decode time: %v", decodeTime)
			
			// Ensure we meet the 100ms target for reasonable sizes
			if (encodeTime + decodeTime) > 100*time.Millisecond && packetSize <= 1024 {
				t.Logf("  Warning: Total processing time exceeds 100ms target: %v", encodeTime+decodeTime)
			}
		})
	}
}
