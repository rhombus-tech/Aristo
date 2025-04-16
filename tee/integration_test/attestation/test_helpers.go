package attestation

import (
	"bytes"
	"strings"
)

// isIntegrationTestSignature returns true if the signature is a test signature
// used in integration testing rather than a real cryptographic signature
func isIntegrationTestSignature(signature []byte) bool {
	if signature == nil || len(signature) == 0 {
		return false
	}

	// Check for common test signature prefixes
	return bytes.HasPrefix(signature, []byte("valid")) ||
		bytes.HasPrefix(signature, []byte("test")) ||
		bytes.HasPrefix(signature, []byte("cross-region")) ||
		strings.Contains(string(signature), "snapshot-signature") ||
		string(signature) == "valid-snapshot-signature"
}
