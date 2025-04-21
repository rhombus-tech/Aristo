// Package for parameter validation utilities
package main

import (
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"log"
)

// ParseDualFormatParameters handles parameter validation for both formats:
// 1. Length-prefixed format: 4-byte little-endian u32 length prefix followed by data
// 2. Direct data format: no length prefix, raw data
func ParseDualFormatParameters(data []byte, supportLengthPrefix, supportDirectFormat bool) ([]byte, string, error) {
	// First try length-prefixed format if supported
	if supportLengthPrefix && len(data) >= 4 {
		length := binary.LittleEndian.Uint32(data[:4])
		// Validate reasonable length (0 < len <= 1024KB)
		if length > 0 && length <= 1024*1024 {
			if int(length+4) <= len(data) {
				// Successfully parsed length-prefixed format
				log.Printf("Detected length-prefixed format: length=%d", length)
				return data[4:4+length], "length-prefixed", nil
			}
		}
	}
	
	// Fall back to direct format if supported
	if supportDirectFormat {
		log.Printf("Using direct data format: length=%d", len(data))
		return data, "direct", nil
	}
	
	return nil, "", fmt.Errorf("invalid parameter format or unsupported format")
}
