package main

import (
    "encoding/binary"
    "fmt"
    "io"
)

// ParseDualFormat handles either length-prefixed or direct format data
func ParseDualFormat(data []byte, expectedDirectSize int) ([]byte, string, error) {
    // Check if we have enough data for a length prefix (4 bytes)
    if len(data) < 4 {
        // Too short for length prefix, assume direct format
        return data, "direct", nil
    }

    // Try to interpret first 4 bytes as a length prefix (little-endian u32)
    length := binary.LittleEndian.Uint32(data[:4])
    
    // Check if the length prefix is reasonable (between 0 and 1MB)
    // and matches the actual data length
    if length > 0 && length <= 1024*1024 && length == uint32(len(data)-4) {
        // Valid length prefix, use length-prefixed format
        return data[4:], "length-prefixed", nil
    }
    
    // No valid length prefix, use direct format
    return data, "direct", nil
}

// WriteDualFormat converts data to either length-prefixed or direct format
func WriteDualFormat(w io.Writer, data []byte, useLengthPrefix bool) error {
    if useLengthPrefix {
        // Write length prefix (4-byte little-endian u32)
        lenBytes := make([]byte, 4)
        binary.LittleEndian.PutUint32(lenBytes, uint32(len(data)))
        if _, err := w.Write(lenBytes); err != nil {
            return fmt.Errorf("failed to write length prefix: %w", err)
        }
    }
    
    // Write the actual data
    if _, err := w.Write(data); err != nil {
        return fmt.Errorf("failed to write data: %w", err)
    }
    
    return nil
}
