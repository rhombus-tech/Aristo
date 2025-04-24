package accumulator

import (
	"encoding/binary"
	"testing"
)

func TestParseDualFormat(t *testing.T) {
	t.Run("LengthPrefixedFormat", func(t *testing.T) {
		// Create a length-prefixed parameter
		paramSize := uint32(32)
		param := make([]byte, 4+paramSize)
		binary.LittleEndian.PutUint32(param[0:4], paramSize)
		for i := uint32(0); i < paramSize; i++ {
			param[4+i] = byte(i % 256)
		}

		// Test parsing
		data, format, err := ParseDualFormat(param, 32)
		if err != nil {
			t.Fatalf("Failed to parse dual format: %v", err)
		}

		// Verify format detection
		if format != "length_prefixed" {
			t.Errorf("Expected format 'length_prefixed', got: %s", format)
		}

		// Verify data extraction
		if len(data) != int(paramSize) {
			t.Errorf("Expected data length %d, got: %d", paramSize, len(data))
		}

		// Verify data content
		for i := uint32(0); i < paramSize; i++ {
			if data[i] != byte(i % 256) {
				t.Errorf("Data mismatch at index %d: expected %d, got %d", i, i % 256, data[i])
				break
			}
		}
	})

	t.Run("DirectFormat", func(t *testing.T) {
		// Create a direct format parameter (typical contract ID)
		paramSize := 32
		param := make([]byte, paramSize)
		for i := 0; i < paramSize; i++ {
			param[i] = byte(i)
		}

		// Test parsing
		data, format, err := ParseDualFormat(param, paramSize)
		if err != nil {
			t.Fatalf("Failed to parse dual format: %v", err)
		}

		// Verify format detection
		if format != "direct" {
			t.Errorf("Expected format 'direct', got: %s", format)
		}

		// Verify data content (should be unchanged)
		if len(data) != paramSize {
			t.Errorf("Expected data length %d, got: %d", paramSize, len(data))
		}

		// Verify data content
		for i := 0; i < paramSize; i++ {
			if data[i] != byte(i) {
				t.Errorf("Data mismatch at index %d: expected %d, got %d", i, i, data[i])
				break
			}
		}
	})

	t.Run("InvalidLengthPrefix", func(t *testing.T) {
		// Create parameter with unreasonable length prefix
		param := make([]byte, 8)
		// Set length to something unreasonable like 3.5 billion
		binary.LittleEndian.PutUint32(param[0:4], 3500000000)
		
		// Test parsing
		data, format, err := ParseDualFormat(param, 32)
		
		// Since the length is unreasonable, it should treat it as direct format
		if err != nil {
			t.Fatalf("Expected successful parsing as direct format, got error: %v", err)
		}
		
		if format != "direct" {
			t.Errorf("Expected format 'direct' for unreasonable length, got: %s", format)
		}
		
		// Data should be unchanged
		if len(data) != len(param) {
			t.Errorf("Expected data length %d, got: %d", len(param), len(data))
		}
	})

	t.Run("TooShortForLengthPrefix", func(t *testing.T) {
		// Create parameter that's too short for length prefix
		param := make([]byte, 3) // Need at least 4 bytes for length prefix
		
		// Test parsing
		data, format, err := ParseDualFormat(param, 32)
		
		// Should be treated as direct format
		if err != nil {
			t.Fatalf("Expected successful parsing as direct format, got error: %v", err)
		}
		
		if format != "direct" {
			t.Errorf("Expected format 'direct' for too short data, got: %s", format)
		}
		
		// Data should be unchanged
		if len(data) != len(param) {
			t.Errorf("Expected data length %d, got: %d", len(param), len(data))
		}
	})

	t.Run("EmptyParameter", func(t *testing.T) {
		// Test with empty parameter
		param := make([]byte, 0)
		
		// Test parsing
		data, format, err := ParseDualFormat(param, 32)
		
		// Should be treated as direct format
		if err != nil {
			t.Fatalf("Expected successful parsing as direct format, got error: %v", err)
		}
		
		if format != "direct" {
			t.Errorf("Expected format 'direct' for empty data, got: %s", format)
		}
		
		// Data should be empty
		if len(data) != 0 {
			t.Errorf("Expected empty data, got length: %d", len(data))
		}
	})

	t.Run("LengthPrefixZero", func(t *testing.T) {
		// Create parameter with zero length
		param := make([]byte, 4)
		binary.LittleEndian.PutUint32(param[0:4], 0)
		
		// Test parsing
		data, format, err := ParseDualFormat(param, 32)
		
		// Zero length is valid in length-prefixed format
		if err != nil {
			t.Fatalf("Failed to parse dual format: %v", err)
		}
		
		if format != "length_prefixed" {
			t.Errorf("Expected format 'length_prefixed' for zero length, got: %s", format)
		}
		
		// Data should be empty
		if len(data) != 0 {
			t.Errorf("Expected empty data for zero length, got length: %d", len(data))
		}
	})

	t.Run("MaxParameterSize", func(t *testing.T) {
		const MaxSize = 1024
		
		// Create parameter with maximum allowed size
		paramSize := uint32(MaxSize)
		param := make([]byte, 4+paramSize)
		binary.LittleEndian.PutUint32(param[0:4], paramSize)
		
		// Test parsing
		data, format, err := ParseDualFormat(param, 32)
		if err != nil {
			t.Fatalf("Failed to parse dual format: %v", err)
		}
		
		if format != "length_prefixed" {
			t.Errorf("Expected format 'length_prefixed' for max size, got: %s", format)
		}
		
		if len(data) != int(paramSize) {
			t.Errorf("Expected data length %d, got: %d", paramSize, len(data))
		}
	})

	t.Run("DirectFormatWrongSize", func(t *testing.T) {
		// Create a direct format parameter with wrong size
		expectedSize := 32
		actualSize := 20
		param := make([]byte, actualSize)
		
		// Test parsing
		data, format, err := ParseDualFormat(param, expectedSize)
		
		// It should still parse as direct format but flag the size mismatch
		if err != nil {
			t.Fatalf("Expected successful parsing as direct format, got error: %v", err)
		}
		
		if format != "direct" {
			t.Errorf("Expected format 'direct' for wrong size, got: %s", format)
		}
		
		// Data should be unchanged
		if len(data) != actualSize {
			t.Errorf("Expected data length %d, got: %d", actualSize, len(data))
		}
	})
}

// BenchmarkParseDualFormat benchmarks the performance of dual format parsing
func BenchmarkParseDualFormat(b *testing.B) {
	b.Run("LengthPrefixed", func(b *testing.B) {
		// Create a length-prefixed parameter
		paramSize := uint32(32)
		param := make([]byte, 4+paramSize)
		binary.LittleEndian.PutUint32(param[0:4], paramSize)
		
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _, _ = ParseDualFormat(param, 32)
		}
	})

	b.Run("DirectFormat", func(b *testing.B) {
		// Create a direct format parameter
		param := make([]byte, 32)
		
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _, _ = ParseDualFormat(param, 32)
		}
	})

	b.Run("UnreasonableLength", func(b *testing.B) {
		// Create parameter with unreasonable length
		param := make([]byte, 8)
		binary.LittleEndian.PutUint32(param[0:4], 3500000000)
		
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _, _ = ParseDualFormat(param, 32)
		}
	})
}
