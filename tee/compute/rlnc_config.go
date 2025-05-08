package compute

// RLNCConfig contains configuration options for Random Linear Network Coding
type RLNCConfig struct {
	// Whether RLNC is enabled
	Enabled bool
	// Whether to use adaptive mode to adjust to network conditions
	AdaptiveMode bool
	// Generation size for RLNC encoding/decoding
	GenSize int
	// Minimum redundancy factor (1.0 = no redundancy)
	MinRedundancy float64
	// Maximum redundancy factor for adaptive mode
	MaxRedundancy float64
}

// DefaultRLNCConfig returns the default RLNC configuration
func DefaultRLNCConfig() *RLNCConfig {
	return &RLNCConfig{
		Enabled:      true,
		AdaptiveMode: true,
		GenSize:      8,
		MinRedundancy: 1.3, // 30% redundancy minimum
		MaxRedundancy: 2.5, // 150% redundancy maximum 
	}
}
