package consensus

import (
	"testing"
	"time"
)

func TestVerifier(t *testing.T) {
	config := &ConsensusConfig{
		MinSignatures:   3,
		MaxDeviation:    time.Second,
		MinRegions:      2,
		ValidityWindow:  time.Minute,
		MaxLatency:      time.Second / 10,
	}

	verifier, err := NewVerifier(config)
	if err != nil {
		t.Fatalf("Failed to create verifier: %v", err)
	}

	now := time.Now()
	validProof := &TimestampProof{
		CreatedAt: now,
		Timestamps: []SignedTimestamp{
			{
				ServerID:  "server1",
				RegionID:  "region1",
				Time:      now,
				Signature: []byte("sig1"),
				Latency:   time.Millisecond * 50,
			},
			{
				ServerID:  "server2",
				RegionID:  "region1",
				Time:      now.Add(time.Millisecond * 100),
				Signature: []byte("sig2"),
				Latency:   time.Millisecond * 40,
			},
			{
				ServerID:  "server3",
				RegionID:  "region2",
				Time:      now.Add(time.Millisecond * 200),
				Signature: []byte("sig3"),
				Latency:   time.Millisecond * 30,
			},
		},
	}

	// Test valid proof
	if err := verifier.VerifyProof(validProof); err != nil {
		t.Errorf("Valid proof rejected: %v", err)
	}

	// Test insufficient signatures
	insufficientSigs := &TimestampProof{
		CreatedAt:  now,
		Timestamps: validProof.Timestamps[:2],
	}
	if err := verifier.VerifyProof(insufficientSigs); err == nil {
		t.Error("Expected error for insufficient signatures")
	}

	// Test insufficient regions
	sameRegion := &TimestampProof{
		CreatedAt: now,
		Timestamps: []SignedTimestamp{
			validProof.Timestamps[0],
			validProof.Timestamps[1],
			{
				ServerID:  "server4",
				RegionID:  "region1",
				Time:      now,
				Signature: []byte("sig4"),
				Latency:   time.Millisecond * 20,
			},
		},
	}
	if err := verifier.VerifyProof(sameRegion); err == nil {
		t.Error("Expected error for insufficient regions")
	}

	// Test excessive time deviation
	highDeviation := &TimestampProof{
		CreatedAt: now,
		Timestamps: []SignedTimestamp{
			validProof.Timestamps[0],
			{
				ServerID:  "server5",
				RegionID:  "region2",
				Time:      now.Add(2 * time.Second),
				Signature: []byte("sig5"),
				Latency:   time.Millisecond * 20,
			},
			validProof.Timestamps[2],
		},
	}
	if err := verifier.VerifyProof(highDeviation); err == nil {
		t.Error("Expected error for excessive time deviation")
	}

	// Test high latency
	highLatency := &TimestampProof{
		CreatedAt: now,
		Timestamps: []SignedTimestamp{
			validProof.Timestamps[0],
			{
				ServerID:  "server6",
				RegionID:  "region2",
				Time:      now,
				Signature: []byte("sig6"),
				Latency:   time.Second,
			},
			validProof.Timestamps[2],
		},
	}
	if err := verifier.VerifyProof(highLatency); err == nil {
		t.Error("Expected error for high latency")
	}

	// Test expired proof
	oldProof := &TimestampProof{
		CreatedAt:  now.Add(-2 * time.Minute),
		Timestamps: validProof.Timestamps,
	}
	if err := verifier.VerifyProof(oldProof); err == nil {
		t.Error("Expected error for expired proof")
	}
}
