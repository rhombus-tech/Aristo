package consensus

import "time"

// TimestampProof represents a proof of time from multiple servers
type TimestampProof struct {
	Timestamp  time.Time
	ServerIDs  []string
	RegionID   string
	Signatures [][]byte
	Proofs     []*ServerProof
}

// ServerProof represents a proof from a single server
type ServerProof struct {
	ServerID  string
	RegionID  string
	Time      time.Time
	Signature []byte
	Latency   time.Duration
}

// Config defines the consensus configuration
type Config struct {
	// MinSignatures is the minimum number of valid signatures required
	MinSignatures int

	// MaxDeviation is the maximum allowed time deviation between servers
	MaxDeviation time.Duration

	// MinRegions is the minimum number of unique regions required
	MinRegions int

	// ValidityWindow is the time window in which timestamps are considered valid
	ValidityWindow time.Duration

	// MaxLatency is the maximum allowed latency for server responses
	MaxLatency time.Duration
}

// NewDefaultConfig creates a new config with default values
func NewDefaultConfig() *Config {
	return &Config{
		MinSignatures:  2,
		MaxDeviation:   5 * time.Second,
		MinRegions:     1,
		ValidityWindow: 30 * time.Second,
		MaxLatency:     100 * time.Millisecond,
	}
}
