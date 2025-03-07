// File: tee/helpers.go
package tee

import (
	"encoding/binary"
	"fmt"
)

// Helper functions for parameter handling in WebAssembly contracts

// FormatParameters prepares parameters for WebAssembly contracts following the
// proper conventions. If useLengthPrefix is true, it prepends a 4-byte length prefix
// to the data (used by most WebAssembly contracts). If false, it passes the
// raw data directly (used mainly for contract IDs in tests).
func FormatParameters(data []byte, useLengthPrefix bool) []byte {
	if !useLengthPrefix {
		return data
	}

	// Use length-prefixed format
	length := uint32(len(data))
	result := make([]byte, 4+len(data))
	binary.LittleEndian.PutUint32(result[:4], length)
	copy(result[4:], data)
	return result
}

// ParseParameterBytes decodes data from WebAssembly contracts, handling both length-prefixed
// and direct data formats. This matches the behavior in the executeAction and DeployContract
// methods in the WebAssembly runtime.
func ParseParameterBytes(params []byte) ([]byte, error) {
	// Check if params start with a length prefix (4 bytes little-endian u32)
	if len(params) >= 4 {
		// Read the length prefix
		lengthBytes := params[0:4]
		length := binary.LittleEndian.Uint32(lengthBytes)
		
		// Validate length is reasonable
		if length > 0 && length <= 1024 && uint32(len(params)) >= 4+length {
			// This is a length-prefixed format
			return params[4:4+length], nil
		}
	}
	
	// Fallback to direct data format (if expected fixed size)
	if len(params) == 32 {
		// If it's exactly 32 bytes, treat as a contract ID in direct format
		return params, nil
	}
	
	// Unsupported format
	return nil, fmt.Errorf("invalid parameter format: not length-prefixed and not 32 bytes")
}

// DeployContractParams provides a convenient way to prepare parameters for contract deployment
type DeployContractParams struct {
	ContractCode []byte
	InitArgs     []byte
	RegionID     string
	ContractName string
}

// ParameterOptions controls how parameters are encoded
type ParameterOptions struct {
	// UseLengthPrefix determines if parameters should be length-prefixed
	UseLengthPrefix bool
}

// DefaultParameterOptions returns the default parameter options
func DefaultParameterOptions() ParameterOptions {
	return ParameterOptions{
		UseLengthPrefix: true,
	}
}

// WithLengthPrefix sets whether to use length prefixing
func (o ParameterOptions) WithLengthPrefix(use bool) ParameterOptions {
	o.UseLengthPrefix = use
	return o
}
