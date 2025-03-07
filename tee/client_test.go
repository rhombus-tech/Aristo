// File: tee/client_test.go
package tee

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFormatParameters tests both parameter formatting functions
func TestFormatParameters(t *testing.T) {
	// Test cases for FormatParameters
	testCases := []struct {
		name           string
		input          []byte
		useLengthPrefix bool
		expected       []byte
	}{
		{
			name:           "With length prefix",
			input:          []byte("hello"),
			useLengthPrefix: true,
			expected:       []byte{5, 0, 0, 0, 'h', 'e', 'l', 'l', 'o'},
		},
		{
			name:           "Without length prefix",
			input:          []byte("direct"),
			useLengthPrefix: false,
			expected:       []byte("direct"),
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := FormatParameters(tc.input, tc.useLengthPrefix)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestParseParameterBytes tests the parameter parsing function
func TestParseParameterBytes(t *testing.T) {
	testCases := []struct {
		name           string
		input          []byte
		expected       []byte
		expectError    bool
	}{
		{
			name:           "Valid length-prefixed",
			input:          []byte{5, 0, 0, 0, 'h', 'e', 'l', 'l', 'o'},
			expected:       []byte("hello"),
			expectError:    false,
		},
		{
			name:           "Valid 32-byte direct",
			input:          make([]byte, 32),
			expected:       make([]byte, 32),
			expectError:    false,
		},
		{
			name:           "Invalid format",
			input:          []byte{1, 2, 3},
			expectError:    true,
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ParseParameterBytes(tc.input)
			
			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expected, result)
			}
		})
	}
}
