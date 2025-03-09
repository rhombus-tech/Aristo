// Package accumulator provides functionality for interacting with the TEE attestation accumulator
package accumulator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	pb "github.com/rhombus-tech/vm/tee/proto"
)

// Client provides an interface to the accumulator system
type Client struct {
	// teeID is the ID of the local TEE
	teeID string

	// teeType is the type of the local TEE (SGX, SEV)
	teeType string

	// accumulatorHash is the current accumulator value
	accumulatorHash []byte
	
	// witnessCache caches witness values for known TEEs
	witnessCache map[string]*pb.AccumulatorWitness
	
	// lastUpdate tracks when the client was last updated
	lastUpdate time.Time
}

// NewClient creates a new accumulator client
func NewClient(teeID, teeType string) *Client {
	return &Client{
		teeID:         teeID,
		teeType:       teeType,
		witnessCache:  make(map[string]*pb.AccumulatorWitness),
		accumulatorHash: make([]byte, 32), // Will be updated during first attestation
		lastUpdate:    time.Now(),
	}
}

// VerifyWitness verifies a witness provided by a TEE
// In a real implementation, this would verify the merkle proof against the root
// skipCacheCheck can be set to true for testing purposes to bypass cached witness checks
func (c *Client) VerifyWitness(witness *pb.AccumulatorWitness, skipCacheCheck ...bool) (bool, error) {
	if witness == nil {
		return false, fmt.Errorf("witness is nil")
	}
	
	// Check if the witness is too old
	maxAge := time.Hour * 24 // 1 day
	if time.Now().Unix()-int64(witness.LastUpdate) > int64(maxAge.Seconds()) {
		return false, fmt.Errorf("witness is too old: %d", witness.LastUpdate)
	}
	
	// In a real implementation, this would compute the witness verification
	// using the merkle proof. For now, we'll do a simple check.
	
	// Skip the cache check if requested (for testing)
	skipCheck := len(skipCacheCheck) > 0 && skipCacheCheck[0]
	
	// Check if this is a known TEE in our cache
	teeID := witness.Element.Executor
	if !skipCheck {
		if cachedWitness, ok := c.witnessCache[teeID]; ok {
			// If we've seen this TEE before, check if the witness is newer
			if witness.LastUpdate <= cachedWitness.LastUpdate {
				// Witness is not newer than what we've seen, could be replay
				return false, fmt.Errorf("witness is not newer than cached witness")
			}
		}
	}
	
	// Simple verification: hash the element to see if it matches the witness value
	valid := c.verifyWitnessHash(witness)
	if !valid {
		return false, fmt.Errorf("witness hash verification failed")
	}
	
	// Cache the witness for future reference
	c.witnessCache[teeID] = witness
	
	return true, nil
}

// verifyWitnessHash does a simple hash verification of the witness
// In a real implementation, this would use the accumulator's verification algorithm
func (c *Client) verifyWitnessHash(witness *pb.AccumulatorWitness) bool {
	// For demonstration, we'll just check if hashing the element produces a value
	// that starts with the same byte as the witness value
	elementBytes := []byte(witness.Element.Executor)
	elementBytes = append(elementBytes, witness.Element.Measurement...)
	elementBytes = append(elementBytes, witness.Element.EnclaveType...)
	elementBytes = append(elementBytes, fmt.Sprintf("%d", witness.Element.Timestamp)...)
	
	hash := sha256.Sum256(elementBytes)
	
	// In a real implementation, this would be a proper cryptographic verification
	// For now, we'll just check if the first byte matches for demonstration
	return len(witness.Value) > 0 && len(hash) > 0 && witness.Value[0] == hash[0]
}

// GetLocalWitness returns the witness for the local TEE
func (c *Client) GetLocalWitness(ctx context.Context) (*pb.AccumulatorWitness, error) {
	// In a real implementation, this would call the accumulator contract to get the witness
	// For now, we'll create a mock witness
	measurement := make([]byte, 32)
	for i := range measurement {
		measurement[i] = byte(i)
	}
	
	// Create a witness with a valid hash
	element := &pb.AccumulatorElement{
		Executor:    c.teeID,
		Measurement: measurement,
		EnclaveType: c.teeType,
		Timestamp:   uint64(time.Now().Unix()),
	}
	
	// Hash the element to create a valid witness value
	elementBytes := []byte(element.Executor)
	elementBytes = append(elementBytes, element.Measurement...)
	elementBytes = append(elementBytes, element.EnclaveType...)
	elementBytes = append(elementBytes, fmt.Sprintf("%d", element.Timestamp)...)
	
	hash := sha256.Sum256(elementBytes)
	
	witness := &pb.AccumulatorWitness{
		Value:          hash[:],
		LastAccumulator: c.accumulatorHash,
		Element:        element,
		LastUpdate:     uint64(time.Now().Unix()),
	}
	
	// Update our cache
	c.witnessCache[c.teeID] = witness
	
	return witness, nil
}

// RefreshAccumulator refreshes the local accumulator value
// In a real implementation, this would query the accumulator contract
func (c *Client) RefreshAccumulator(ctx context.Context) error {
	// In a real implementation, this would call the accumulator contract
	// For now, we'll just create a mock accumulator value
	hash := sha256.Sum256([]byte(fmt.Sprintf("accumulator-%d", time.Now().Unix())))
	c.accumulatorHash = hash[:]
	c.lastUpdate = time.Now()
	
	return nil
}

// DebugWitness returns a string representation of a witness for debugging
func (c *Client) DebugWitness(witness *pb.AccumulatorWitness) string {
	if witness == nil {
		return "nil witness"
	}
	
	return fmt.Sprintf(
		"Witness for %s (type: %s)\n"+
			"Value: %s\n"+
			"Last Accumulator: %s\n"+
			"Last Update: %s\n"+
			"Measurement: %s",
		witness.Element.Executor,
		witness.Element.EnclaveType,
		hex.EncodeToString(witness.Value),
		hex.EncodeToString(witness.LastAccumulator),
		time.Unix(int64(witness.LastUpdate), 0).Format(time.RFC3339),
		hex.EncodeToString(witness.Element.Measurement),
	)
}
