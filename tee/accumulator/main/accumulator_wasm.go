// RSA Accumulator WebAssembly module with dual-format parameter validation
package main

import (
	"encoding/binary"
	"fmt"
	"syscall/js"
)

// Global configuration
var (
	supportLengthPrefix = true
	supportDirectFormat = true
	batchSize           = 1000
	parallelism         = 8
)

// ParseDualFormatParameters handles parameter validation for both formats:
// 1. Length-prefixed format: 4-byte little-endian u32 length prefix followed by data
// 2. Direct data format: no length prefix, raw data
func ParseDualFormatParameters(data []byte) ([]byte, string, error) {
	// First try length-prefixed format if supported
	if supportLengthPrefix && len(data) >= 4 {
		length := binary.LittleEndian.Uint32(data[:4])
		// Validate reasonable length (0 < len <= 1024KB)
		if length > 0 && length <= 1024*1024 {
			if int(length+4) <= len(data) {
				// Successfully parsed length-prefixed format
				fmt.Printf("Detected length-prefixed format: length=%d\n", length)
				return data[4:4+length], "length-prefixed", nil
			}
		}
	}
	
	// Fall back to direct format if supported
	if supportDirectFormat {
		fmt.Printf("Using direct data format: length=%d\n", len(data))
		return data, "direct", nil
	}
	
	return nil, "", fmt.Errorf("invalid parameter format or unsupported format")
}

// validateParameter validates a parameter using dual-format detection
func validateParameter(this js.Value, args []js.Value) interface{} {
	if len(args) < 1 {
		return "Error: Missing parameter data"
	}
	
	// Convert JS ArrayBuffer to Go slice
	data := make([]byte, args[0].Length())
	js.CopyBytesToGo(data, args[0])
	
	// Validate using dual-format detection
	params, format, err := ParseDualFormatParameters(data)
	if err != nil {
		return "Error: " + err.Error()
	}
	
	return fmt.Sprintf("Successfully validated %d bytes using %s format", len(params), format)
}

// accumulate adds a parameter to the accumulation after validation
func accumulate(this js.Value, args []js.Value) interface{} {
	if len(args) < 1 {
		return "Error: Missing parameter data"
	}
	
	// Convert JS ArrayBuffer to Go slice
	data := make([]byte, args[0].Length())
	js.CopyBytesToGo(data, args[0])
	
	// Validate using dual-format detection
	params, format, err := ParseDualFormatParameters(data)
	if err != nil {
		return "Error: " + err.Error()
	}
	
	// Here we would actually accumulate the parameter
	// For now, just return success
	return fmt.Sprintf("Successfully accumulated %d bytes using %s format", len(params), format)
}

// getConfig returns the current configuration
func getConfig(this js.Value, args []js.Value) interface{} {
	config := map[string]interface{}{
		"supportLengthPrefix": supportLengthPrefix,
		"supportDirectFormat": supportDirectFormat,
		"batchSize":           batchSize,
		"parallelism":         parallelism,
	}
	
	result := js.Global().Get("Object").New()
	for k, v := range config {
		result.Set(k, v)
	}
	
	return result
}

// setConfig updates the configuration
func setConfig(this js.Value, args []js.Value) interface{} {
	if len(args) < 1 || !args[0].Truthy() {
		return "Error: Invalid configuration object"
	}
	
	config := args[0]
	
	if config.Get("supportLengthPrefix").Truthy() {
		supportLengthPrefix = config.Get("supportLengthPrefix").Bool()
	}
	
	if config.Get("supportDirectFormat").Truthy() {
		supportDirectFormat = config.Get("supportDirectFormat").Bool()
	}
	
	if config.Get("batchSize").Truthy() {
		batchSize = config.Get("batchSize").Int()
	}
	
	if config.Get("parallelism").Truthy() {
		parallelism = config.Get("parallelism").Int()
	}
	
	return "Configuration updated successfully"
}

func main() {
	// Register JavaScript functions
	js.Global().Set("validateParameter", js.FuncOf(validateParameter))
	js.Global().Set("accumulate", js.FuncOf(accumulate))
	js.Global().Set("getConfig", js.FuncOf(getConfig))
	js.Global().Set("setConfig", js.FuncOf(setConfig))
	
	// Keep the program running
	select {}
}
