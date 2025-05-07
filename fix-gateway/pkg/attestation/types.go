// Copyright (C) 2025. All rights reserved.
// See the file LICENSE for licensing terms.

package attestation

// TEEType represents a TEE type
type TEEType int

// TEE types
const (
	TEETypeUnknown TEEType = 0
	TEETypeSGX     TEEType = 1
	TEETypeSEV     TEEType = 2
	TEETypeTDX     TEEType = 3
)

// String returns a string representation of the TEE type
func (t TEEType) String() string {
	switch t {
	case TEETypeSGX:
		return "SGX"
	case TEETypeSEV:
		return "SEV"
	case TEETypeTDX:
		return "TDX"
	default:
		return "Unknown"
	}
}

// TEEAttestation represents a TEE attestation
type TEEAttestation struct {
	// Type is the TEE type
	Type TEEType
	
	// InputHash is the hash of the input data
	InputHash []byte
	
	// OutputHash is the hash of the output data
	OutputHash []byte
	
	// Report is the attestation report
	Report []byte
	
	// Timestamp is the attestation timestamp
	Timestamp int64
}

// MarshalBinary marshals the attestation to binary
func (a *TEEAttestation) MarshalBinary() ([]byte, error) {
	// This is a placeholder implementation - in a real system we would use your existing marshaling
	// For now, just concatenate the fields with length prefixes
	result := make([]byte, 0)
	
	// Add type
	result = append(result, byte(a.Type))
	
	// Add input hash with length prefix
	result = append(result, byte(len(a.InputHash)))
	result = append(result, a.InputHash...)
	
	// Add output hash with length prefix
	result = append(result, byte(len(a.OutputHash)))
	result = append(result, a.OutputHash...)
	
	// Add report with length prefix
	result = append(result, byte(len(a.Report)))
	result = append(result, a.Report...)
	
	// Add timestamp (8 bytes)
	timestampBytes := make([]byte, 8)
	for i := 0; i < 8; i++ {
		timestampBytes[i] = byte(a.Timestamp >> (i * 8))
	}
	result = append(result, timestampBytes...)
	
	return result, nil
}

// UnmarshalBinary unmarshals the attestation from binary
func (a *TEEAttestation) UnmarshalBinary(data []byte) error {
	// This is a placeholder implementation - in a real system we would use your existing unmarshaling
	if len(data) < 1 {
		return nil
	}
	
	// Extract type
	a.Type = TEEType(data[0])
	pos := 1
	
	// Extract input hash
	if pos < len(data) {
		length := int(data[pos])
		pos++
		if pos+length <= len(data) {
			a.InputHash = data[pos : pos+length]
			pos += length
		}
	}
	
	// Extract output hash
	if pos < len(data) {
		length := int(data[pos])
		pos++
		if pos+length <= len(data) {
			a.OutputHash = data[pos : pos+length]
			pos += length
		}
	}
	
	// Extract report
	if pos < len(data) {
		length := int(data[pos])
		pos++
		if pos+length <= len(data) {
			a.Report = data[pos : pos+length]
			pos += length
		}
	}
	
	// Extract timestamp
	if pos+8 <= len(data) {
		for i := 0; i < 8; i++ {
			a.Timestamp |= int64(data[pos+i]) << (i * 8)
		}
	}
	
	return nil
}
