package vm

import (
	"encoding/binary"
	"fmt"
	"sync"

	"go.uber.org/zap"
)

// Parameter format constants
const (
	// MaxParameterSize is the maximum size allowed for WebAssembly contract parameters
	MaxParameterSize = 1024 * 1024 // 1MB

	// MinParameterLengthPrefix is the minimum acceptable value for a length prefix
	MinParameterLengthPrefix = 0
)

// ParameterFormatType identifies the format used for a parameter
type ParameterFormatType int

const (
	LengthPrefixed ParameterFormatType = iota // 4-byte length prefix followed by data
	DirectData                                // Raw data without length prefix
)

// ParamResult contains the processed parameter data and metadata
type ParamResult struct {
	Data       []byte             // The extracted parameter data
	Format     ParameterFormatType // The detected format
	OrigLength int                // Original length of the input
	ProcLength int                // Processed length after extraction
}

// NewParamResult creates a new parameter result object
func NewParamResult(data []byte, format ParameterFormatType, origLength int) *ParamResult {
	return &ParamResult{
		Data:       data,
		Format:     format,
		OrigLength: origLength,
		ProcLength: len(data),
	}
}

// ParameterHandler processes WebAssembly contract parameters
type ParameterHandler struct {
	logger *zap.Logger
	mutex  sync.RWMutex
	cache  map[string]*ParamResult // Optional cache for repeated parameters
}

// NewParameterHandler creates a new parameter handler
func NewParameterHandler(logger *zap.Logger) *ParameterHandler {
	return &ParameterHandler{
		logger: logger,
		cache:  make(map[string]*ParamResult),
	}
}

// ParseParameter handles both parameter formats (length-prefixed and direct data)
func (h *ParameterHandler) ParseParameter(data []byte) (*ParamResult, error) {
	if len(data) == 0 {
		return NewParamResult([]byte{}, DirectData, 0), nil
	}

	origLength := len(data)

	if len(data) < 4 {
		// Too short for length prefix, treat as direct data
		h.logger.Debug("Parameter too short for length prefix, treating as direct data",
			zap.Int("length", len(data)))
		return NewParamResult(data, DirectData, origLength), nil
	}

	// Try interpreting first 4 bytes as a length prefix
	length := binary.LittleEndian.Uint32(data[:4])

	// Check if the length value is reasonable
	if length > MinParameterLengthPrefix && length <= MaxParameterSize && int(length)+4 <= len(data) {
		// This is likely a length-prefixed parameter
		h.logger.Debug("Detected length-prefixed parameter",
			zap.Uint32("prefix_length", length),
			zap.Int("total_length", len(data)))
		return NewParamResult(data[4:4+length], LengthPrefixed, origLength), nil
	}

	// If length is unreasonable, treat as direct data
	h.logger.Debug("Length prefix unreasonable, treating as direct data",
		zap.Uint32("detected_length", length),
		zap.Int("actual_length", len(data)))
	return NewParamResult(data, DirectData, origLength), nil
}

// ProcessContractParams handles WebAssembly contract parameters safely
func (h *ParameterHandler) ProcessContractParams(params []byte) ([]byte, error) {
	if len(params) == 0 {
		return []byte{}, nil
	}

	// Parse the parameter format
	result, err := h.ParseParameter(params)
	if err != nil {
		return nil, fmt.Errorf("failed to parse parameters: %w", err)
	}

	// Log parameter details for debugging
	h.logger.Debug("Contract parameter preprocessing",
		zap.Int("original_length", result.OrigLength),
		zap.Int("processed_length", result.ProcLength),
		zap.String("format", formatTypeToString(result.Format)),
		zap.String("hex_preview", formatHexPreview(result.Data, 16)), // Show first 16 bytes in hex
	)

	return result.Data, nil
}

// Helper functions

// formatTypeToString converts parameter format type to string
func formatTypeToString(format ParameterFormatType) string {
	switch format {
	case LengthPrefixed:
		return "length-prefixed"
	case DirectData:
		return "direct-data"
	default:
		return "unknown"
	}
}

// formatHexPreview formats the first n bytes of data as hex for logging
func formatHexPreview(data []byte, n int) string {
	if len(data) == 0 {
		return "<empty>"
	}

	previewLen := n
	if len(data) < n {
		previewLen = len(data)
	}

	preview := make([]byte, previewLen*2)
	const hexChars = "0123456789abcdef"
	for i := 0; i < previewLen; i++ {
		preview[i*2] = hexChars[data[i]>>4]
		preview[i*2+1] = hexChars[data[i]&0x0f]
	}

	suffix := ""
	if len(data) > n {
		suffix = "..."
	}

	return fmt.Sprintf("%s%s", string(preview), suffix)
}
