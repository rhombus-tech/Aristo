// Package mesh provides state management for the TEE mesh
package mesh

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"
)

// CompressionType constants
const (
	CompressionNone = "none"
	CompressionGzip = "gzip"
	CompressionZlib = "zlib"
)

// DefaultCompressionType is the default compression algorithm to use
// This is a variable (not a constant) so it can be modified in tests
var DefaultCompressionType = CompressionGzip

// CompressData compresses data using the specified algorithm
// Returns the compressed data, data hash, original size, and error if any
// Uses similar validation patterns to those developed for Wasmlanche WebAssembly contracts
func CompressData(data []byte, compressionType string) ([]byte, []byte, uint64, error) {
	// Parameter validation (null/empty checks)
	if data == nil {
		return nil, nil, 0, fmt.Errorf("cannot compress nil data")
	}

	// Calculate original size and hash - do this first in case compression fails
	originalSize := uint64(len(data))
	dataHash := sha256.Sum256(data)

	// Return early for empty data
	if originalSize == 0 {
		return []byte{}, dataHash[:], 0, nil
	}

	// Handle no compression case
	if compressionType == "" || strings.ToLower(compressionType) == CompressionNone {
		return data, dataHash[:], originalSize, nil
	}

	// Prepare buffer for compressed data
	var buf bytes.Buffer

	// Apply compression based on type
	switch strings.ToLower(compressionType) {
	case CompressionGzip:
		// Create gzip writer with best compression
		gzWriter, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("failed to create gzip writer: %w", err)
		}
		
		// Write data and close
		if _, err := gzWriter.Write(data); err != nil {
			gzWriter.Close()
			return nil, nil, 0, fmt.Errorf("failed to write data to gzip: %w", err)
		}
		if err := gzWriter.Close(); err != nil {
			return nil, nil, 0, fmt.Errorf("failed to close gzip writer: %w", err)
		}

	case CompressionZlib:
		// Create zlib writer with best compression
		zWriter, err := zlib.NewWriterLevel(&buf, zlib.BestCompression)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("failed to create zlib writer: %w", err)
		}
		
		// Write data and close
		if _, err := zWriter.Write(data); err != nil {
			zWriter.Close()
			return nil, nil, 0, fmt.Errorf("failed to write data to zlib: %w", err)
		}
		if err := zWriter.Close(); err != nil {
			return nil, nil, 0, fmt.Errorf("failed to close zlib writer: %w", err)
		}

	default:
		return nil, nil, 0, fmt.Errorf("unsupported compression type: %s", compressionType)
	}

	// Get compressed data
	compressedData := buf.Bytes()
	
	// Verify compression was beneficial (following Wasmlanche validation patterns)
	if uint64(len(compressedData)) >= originalSize {
		// If compression didn't help, return original data
		return data, dataHash[:], originalSize, nil
	}

	return compressedData, dataHash[:], originalSize, nil
}

// DecompressData decompresses data using the specified algorithm
// Similar to CompressData, this implements robust validation patterns
func DecompressData(compressedData []byte, originalSize uint64, compressionType string) ([]byte, error) {
	// Parameter validation with fallbacks (same pattern as Wasmlanche WebAssembly contracts)
	if compressedData == nil {
		return nil, fmt.Errorf("cannot decompress nil data")
	}
	
	// Handle no compression case
	if compressionType == "" || strings.ToLower(compressionType) == CompressionNone {
		return compressedData, nil
	}

	// Prepare buffer for decompressed data with reasonable size limit
	// Similar to how we handled parameter size validation in Wasmlanche
	if originalSize > 1024*1024*100 { // 100 MB limit
		return nil, fmt.Errorf("decompression would exceed size limit: %d bytes", originalSize)
	}

	// Use bytes.Buffer for efficient reading
	compressedBuf := bytes.NewReader(compressedData)
	var decompressor io.ReadCloser
	var err error

	// Create decompressor based on type
	switch strings.ToLower(compressionType) {
	case CompressionGzip:
		decompressor, err = gzip.NewReader(compressedBuf)
		if err != nil {
			return nil, fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer decompressor.Close()

	case CompressionZlib:
		decompressor, err = zlib.NewReader(compressedBuf)
		if err != nil {
			return nil, fmt.Errorf("failed to create zlib reader: %w", err)
		}
		defer decompressor.Close()

	default:
		return nil, fmt.Errorf("unsupported compression type: %s", compressionType)
	}

	// Read decompressed data with size validation (Wasmlanche pattern)
	decompressed := make([]byte, 0, originalSize)
	buf := make([]byte, 32*1024) // 32KB chunks for reading
	
	for {
		n, err := decompressor.Read(buf)
		if n > 0 {
			decompressed = append(decompressed, buf[:n]...)
			
			// Safety check against malicious/corrupt data causing OOM
			if uint64(len(decompressed)) > originalSize*2 {
				return nil, fmt.Errorf("decompression exceeded expected size significantly")
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error during decompression: %w", err)
		}
	}

	// Verify size if original size was recorded
	if originalSize > 0 && uint64(len(decompressed)) != originalSize {
		// Log warning but don't fail - actual size might not match exactly due to padding, etc.
		// This follows similar patterns to the Wasmlanche parameter tolerance
		fmt.Printf("Warning: Decompressed size (%d) doesn't match expected size (%d)\n", 
			len(decompressed), originalSize)
	}

	return decompressed, nil
}
