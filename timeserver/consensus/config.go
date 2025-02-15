package consensus

import (
	"fmt"
	"time"
)

// ConsensusConfig defines the parameters for timestamp verification
type ConsensusConfig struct {
	// MinSignatures is the minimum number of valid signatures required
	MinSignatures int

	// MaxDeviation is the maximum allowed time difference between timestamps
	MaxDeviation time.Duration

	// MinRegions is the minimum number of different regions required
	MinRegions int

	// ValidityWindow is how long a timestamp proof remains valid
	ValidityWindow time.Duration

	// MaxLatency is the maximum allowed processing time for a timestamp
	MaxLatency time.Duration
}

// Validate checks if the configuration is valid
func (c *ConsensusConfig) Validate() error {
	if c.MinSignatures < 1 {
		return fmt.Errorf("MinSignatures must be at least 1")
	}
	if c.MaxDeviation < time.Millisecond {
		return fmt.Errorf("MaxDeviation must be at least 1ms")
	}
	if c.MinRegions < 1 {
		return fmt.Errorf("MinRegions must be at least 1")
	}
	if c.ValidityWindow < time.Second {
		return fmt.Errorf("ValidityWindow must be at least 1s")
	}
	if c.MaxLatency < time.Millisecond {
		return fmt.Errorf("MaxLatency must be at least 1ms")
	}
	return nil
}

// NewDefaultConfig creates a ConsensusConfig with reasonable defaults
func NewDefaultConfig() *ConsensusConfig {
	return &ConsensusConfig{
		MinSignatures:   3,              // Require at least 3 signatures
		MaxDeviation:    time.Second,    // Allow 1 second deviation
		MinRegions:      2,              // Require at least 2 regions
		ValidityWindow:  time.Minute,    // Proofs valid for 1 minute
		MaxLatency:      time.Second/10, // Max 100ms processing time
	}
}
