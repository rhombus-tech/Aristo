// Copyright (C) 2025, Rhombus Technologies. All rights reserved.
// See the file LICENSE for licensing terms.

package archive

import (
	"encoding/binary"

	"github.com/ava-labs/avalanchego/ids"
)

// makeBlockIDFromHeight creates a deterministic block ID from height and state root
// This is necessary to work with the core interface which requires IDs for GetBlock
func makeBlockIDFromHeight(height uint64, stateRoot [32]byte) ids.ID {
	var blockID ids.ID
	binary.LittleEndian.PutUint64(blockID[:8], height)
	copy(blockID[8:], stateRoot[:24])
	return blockID
}

// ParseDualFormatParameter handles both length-prefixed and direct parameter formats
// using the same robust approach that prevents WebAssembly contract issues.
// This provides protection against the 3.5 billion byte length issue seen in WebAssembly contracts.
func ParseDualFormatParameter(data []byte, supportLengthPrefix, supportDirectFormat bool) ([]byte, string, error) {
	// First try length-prefixed format if supported
	if supportLengthPrefix && len(data) >= 4 {
		length := binary.LittleEndian.Uint32(data[:4])
		// Validate reasonable length (0 < len <= 1024KB)
		// This explicitly prevents the "3.5 billion byte length" issue in WebAssembly contracts
		if length > 0 && length <= 1024*1024 {
			if int(length+4) <= len(data) {
				// Successfully parsed length-prefixed format
				return data[4:4+length], "length-prefixed", nil
			}
		}
	}
	
	// Fall back to direct format if supported
	if supportDirectFormat {
		return data, "direct", nil
	}
	
	return nil, "", nil
}
