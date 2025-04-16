package attestation

import (
	"encoding/hex"
)

// Implement mapping of TEE IDs to their regions
func (r *MockTEERegistry) registerTEERegion(teeID []byte, region string) {
	// Initialize the map if it's nil
	if r.teeRegions == nil {
		r.teeRegions = make(map[string]string)
	}
	
	// Store the mapping of TEE ID to region
	r.teeRegions[hex.EncodeToString(teeID)] = region
}

// getTEERegion retrieves the region for a given TEE ID
func (r *MockTEERegistry) getTEERegion(teeID []byte) (string, bool) {
	// Check if the registry has region mappings
	if r.teeRegions == nil {
		return "", false
	}
	
	// Look up the region for this TEE ID
	region, ok := r.teeRegions[hex.EncodeToString(teeID)]
	if !ok {
		return "", false
	}
	
	return region, true
}
