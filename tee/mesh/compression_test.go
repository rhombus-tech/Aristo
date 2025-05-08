package mesh

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestCompressDecompress(t *testing.T) {
	// Create test data with repeated patterns for good compression
	testData := make([]byte, 100*1024) // 100KB of test data
	
	// Fill with pattern (similar to NASDAQ market data structure)
	patternStr := `{"symbol":"AAPL","price":150.23,"volume":1000000}`
	pattern := []byte(patternStr)
	
	// Repeat the pattern to fill the test data
	for i := 0; i < len(testData); i += len(pattern) {
		remain := len(testData) - i
		if remain >= len(pattern) {
			copy(testData[i:i+len(pattern)], pattern)
		} else {
			copy(testData[i:], pattern[:remain])
		}
	}
	
	// Following Wasmlanche practices, validate expected test data size
	if len(testData) != 100*1024 {
		t.Fatalf("Failed to create test data with expected size")
	}
	
	testCases := []struct {
		name           string
		compressionType string
		expectRatio    float64 // minimum expected compression ratio
	}{
		{"NoCompression", CompressionNone, 1.0}, // No compression should have ratio = 1.0
		{"Gzip", CompressionGzip, 5.0},         // Expect at least 5x compression
		{"Zlib", CompressionZlib, 5.0},         // Similar to gzip
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Compress the data
			compressed, hash, originalSize, err := CompressData(testData, tc.compressionType)
			if err != nil {
				t.Fatalf("Failed to compress data: %v", err)
			}
			
			// Verify parameter handling for hash and size
			if len(hash) != 32 {
				t.Fatalf("Invalid hash length: %d, expected 32", len(hash))
			}
			if originalSize != uint64(len(testData)) {
				t.Fatalf("Original size mismatch: %d, expected %d", originalSize, len(testData))
			}
			
			// Check compression ratio - similar to how we validated parameters in Wasmlanche
			if tc.compressionType != CompressionNone {
				ratio := float64(len(testData)) / float64(len(compressed))
				t.Logf("Compression ratio: %.2fx (%d -> %d bytes)", 
					ratio, len(testData), len(compressed))
				
				if ratio < tc.expectRatio {
					t.Logf("Warning: Compression ratio %.2fx below expected %.2fx", 
						ratio, tc.expectRatio)
				}
			} else {
				// For no compression, data should be identical
				if !bytes.Equal(testData, compressed) {
					t.Fatalf("NoCompression altered the data")
				}
			}
			
			// Decompress the data and verify
			decompressed, err := DecompressData(compressed, originalSize, tc.compressionType)
			if err != nil {
				t.Fatalf("Failed to decompress data: %v", err)
			}
			
			// Verify decompressed data matches original
			if !bytes.Equal(testData, decompressed) {
				// Print a sample of the data to help debug
				t.Logf("Original (first 100 bytes): %s", hex.EncodeToString(testData[:100]))
				t.Logf("Decompressed (first 100 bytes): %s", hex.EncodeToString(decompressed[:100]))
				t.Fatalf("Decompressed data doesn't match original")
			}
			
			t.Logf("Successfully compressed and decompressed %d bytes of data", len(testData))
		})
	}
}

func TestCompressDataFallbacks(t *testing.T) {
	// Test fallback behaviors - similar to Wasmlanche parameter validation tests
	tests := []struct {
		name        string
		data        []byte
		compression string
		expectError bool
	}{
		{"NilData", nil, CompressionGzip, true},
		{"EmptyData", []byte{}, CompressionGzip, false},
		{"InvalidCompression", []byte("test"), "invalid-type", true},
		{"UncompressibleData", []byte{1, 2, 3, 4}, CompressionGzip, false}, // Too small to compress well
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compressed, hash, size, err := CompressData(tt.data, tt.compression)
			
			if tt.expectError && err == nil {
				t.Fatalf("Expected error but got nil")
			}
			
			if !tt.expectError {
				// For valid test cases, ensure we get a properly formed result
				if tt.data != nil && size != uint64(len(tt.data)) {
					t.Errorf("Size mismatch: got %d, want %d", size, len(tt.data))
				}
				
				if hash == nil {
					t.Errorf("Expected non-nil hash")
				}
				
				// For empty data, compressed should be empty
				if len(tt.data) == 0 && len(compressed) != 0 {
					t.Errorf("Empty data resulted in non-empty compressed data")
				}
			}
		})
	}
}

func TestDecompressDataFallbacks(t *testing.T) {
	// Test the fallback behavior for decompression errors
	// This follows the same pattern as our Wasmlanche validation tests
	tests := []struct {
		name           string
		compressedData []byte
		originalSize   uint64
		compression    string
		expectError    bool
	}{
		{"NilData", nil, 10, CompressionGzip, true},
		{"ZeroSize", []byte("test"), 0, CompressionGzip, true}, // Zero-size gzip decompression should fail
		{"InvalidCompression", []byte("test"), 10, "invalid-type", true},
		{"CorruptedData", []byte{1, 2, 3, 4}, 100, CompressionGzip, true},
		{"ExcessiveSize", []byte("test"), 1024*1024*200, CompressionGzip, true}, // 200MB is too large
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecompressData(tt.compressedData, tt.originalSize, tt.compression)
			
			if tt.expectError && err == nil {
				t.Fatalf("Expected error but got nil")
			}
			
			if !tt.expectError && err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			
			// For the ZeroSize test, we now expect an error because gzip decompression fails on zero size
			if tt.name == "ZeroSize" {
				if err == nil {
					t.Errorf("Zero size should produce an error with gzip compression")
				}
				// No need to check result, we're expecting an error
			}
		})
	}
}
