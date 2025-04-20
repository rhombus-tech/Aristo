package accumulator

import (
	"context"
	"testing"
	"time"

	pb "github.com/rhombus-tech/vm/tee/proto"
)

func TestAccumulatorClient(t *testing.T) {
	// Create a new client
	client := NewClient("test-tee", "SGX")
	
	// Refresh the accumulator to initialize it
	err := client.RefreshAccumulator(context.Background())
	if err != nil {
		t.Fatalf("Failed to refresh accumulator: %v", err)
	}
	
	// Short delay to ensure timestamps are different
	time.Sleep(10 * time.Millisecond)
	
	// Get the local witness
	witness, err := client.GetLocalWitness(context.Background())
	if err != nil {
		t.Fatalf("Failed to get local witness: %v", err)
	}
	
	// Verify the local witness
	valid, err := client.VerifyWitness(witness, true)
	if err != nil {
		t.Fatalf("Failed to verify witness: %v", err)
	}
	
	if !valid {
		t.Error("Expected local witness to be valid")
	}
	
	// Test with a modified witness that should fail verification
	// Create a new witness rather than copying to avoid copying mutex
	invalidWitness := &pb.AccumulatorWitness{
		Element:        witness.Element,
		Value:          append([]byte{}, witness.Value...),
		LastAccumulator: witness.LastAccumulator,
		LastUpdate:     witness.LastUpdate,
	}
	// Modify the value to make it invalid
	if len(invalidWitness.Value) > 0 {
		invalidWitness.Value[0]++
	}
	
	valid, err = client.VerifyWitness(invalidWitness, true)
	if err == nil {
		t.Error("Expected error when verifying invalid witness")
	}
	
	if valid {
		t.Error("Expected invalid witness to fail verification")
	}
	
	// Test with a nil witness
	valid, err = client.VerifyWitness(nil, true)
	if err == nil {
		t.Error("Expected error when verifying nil witness")
	}
	
	if valid {
		t.Error("Expected nil witness to fail verification")
	}
	
	// Test with an old witness
	// Create a new witness rather than copying to avoid copying mutex
	oldWitness := &pb.AccumulatorWitness{
		Element:        witness.Element,
		Value:          append([]byte{}, witness.Value...),
		LastAccumulator: witness.LastAccumulator,
		LastUpdate:     uint64(time.Now().AddDate(0, 0, -2).Unix()), // 2 days old
	}
	
	valid, err = client.VerifyWitness(oldWitness, true)
	if err == nil {
		t.Error("Expected error when verifying old witness")
	}
	
	if valid {
		t.Error("Expected old witness to fail verification")
	}
}

func TestDebugWitness(t *testing.T) {
	// Create a new client
	client := NewClient("test-tee", "SGX")
	
	// Refresh the accumulator to initialize it
	err := client.RefreshAccumulator(context.Background())
	if err != nil {
		t.Fatalf("Failed to refresh accumulator: %v", err)
	}
	
	// Short delay to ensure timestamps are different
	time.Sleep(10 * time.Millisecond)
	
	// Get the local witness
	witness, err := client.GetLocalWitness(context.Background())
	if err != nil {
		t.Fatalf("Failed to get local witness: %v", err)
	}
	
	// Verify the local witness
	valid, err := client.VerifyWitness(witness, true)
	if err != nil {
		t.Fatalf("Failed to verify witness: %v", err)
	}
	if !valid {
		t.Error("Expected local witness to be valid")
	}
	
	// Test the debug output
	debug := client.DebugWitness(witness)
	if debug == "" {
		t.Error("Expected non-empty debug output")
	}
	
	// Test with nil witness
	debug = client.DebugWitness(nil)
	if debug != "nil witness" {
		t.Errorf("Expected 'nil witness' for nil witness, got '%s'", debug)
	}
}
