package consensus

import (
	"fmt"
	"sort"
	"time"
)

// TimestampProof represents a collection of signed timestamps
type TimestampProof struct {
	// Timestamps from different servers
	Timestamps []SignedTimestamp

	// Time when the proof was created
	CreatedAt time.Time
}

// SignedTimestamp represents a single timestamp with its signature
type SignedTimestamp struct {
	// Server that provided the timestamp
	ServerID string

	// Region of the server
	RegionID string

	// The actual timestamp
	Time time.Time

	// Signature of the timestamp
	Signature []byte

	// Processing latency
	Latency time.Duration
}

// Verifier handles timestamp verification according to consensus rules
type Verifier struct {
	config *ConsensusConfig
}

// NewVerifier creates a new timestamp verifier
func NewVerifier(config *ConsensusConfig) (*Verifier, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return &Verifier{config: config}, nil
}

// VerifyProof checks if a timestamp proof is valid according to consensus rules
func (v *Verifier) VerifyProof(proof *TimestampProof) error {
	if len(proof.Timestamps) < v.config.MinSignatures {
		return fmt.Errorf("insufficient signatures: got %d, need %d", 
			len(proof.Timestamps), v.config.MinSignatures)
	}

	// Check proof age
	age := time.Since(proof.CreatedAt)
	if age > v.config.ValidityWindow {
		return fmt.Errorf("proof too old: %v > %v", age, v.config.ValidityWindow)
	}

	// Verify region diversity
	regions := make(map[string]bool)
	for _, ts := range proof.Timestamps {
		regions[ts.RegionID] = true
	}
	if len(regions) < v.config.MinRegions {
		return fmt.Errorf("insufficient regions: got %d, need %d",
			len(regions), v.config.MinRegions)
	}

	// Verify timestamp consistency
	if err := v.verifyTimestampConsistency(proof.Timestamps); err != nil {
		return fmt.Errorf("timestamp inconsistency: %w", err)
	}

	// Verify processing latencies
	if err := v.verifyLatencies(proof.Timestamps); err != nil {
		return fmt.Errorf("latency violation: %w", err)
	}

	return nil
}

// verifyTimestampConsistency checks if timestamps are within MaxDeviation
func (v *Verifier) verifyTimestampConsistency(timestamps []SignedTimestamp) error {
	if len(timestamps) == 0 {
		return fmt.Errorf("no timestamps to verify")
	}

	// Sort timestamps
	sorted := make([]SignedTimestamp, len(timestamps))
	copy(sorted, timestamps)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Time.Before(sorted[j].Time)
	})

	// Check max deviation between earliest and latest
	deviation := sorted[len(sorted)-1].Time.Sub(sorted[0].Time)
	if deviation > v.config.MaxDeviation {
		return fmt.Errorf("time deviation too large: %v > %v",
			deviation, v.config.MaxDeviation)
	}

	return nil
}

// verifyLatencies checks if processing times are within MaxLatency
func (v *Verifier) verifyLatencies(timestamps []SignedTimestamp) error {
	for _, ts := range timestamps {
		if ts.Latency > v.config.MaxLatency {
			return fmt.Errorf("server %s latency too high: %v > %v",
				ts.ServerID, ts.Latency, v.config.MaxLatency)
		}
	}
	return nil
}

// GetConsensusTime returns the agreed-upon time from a set of timestamps
func (v *Verifier) GetConsensusTime(timestamps []SignedTimestamp) time.Time {
	if len(timestamps) == 0 {
		return time.Time{}
	}

	// Use median time as consensus
	sorted := make([]SignedTimestamp, len(timestamps))
	copy(sorted, timestamps)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Time.Before(sorted[j].Time)
	})

	medianIdx := len(sorted) / 2
	return sorted[medianIdx].Time
}
