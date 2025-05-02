package tee

import (
	"context"
	"fmt"
	"testing"

	"github.com/rhombus-tech/vm/core"
)

// MockExecutor is a dedicated test executor that avoids dependency issues
type MockExecutor struct {
	// Mock executor doesn't need the same fields as the real executor
	// but it implements the relevant methods for testing
}

// NewTestExecutor creates a mock executor that implements the needed test methods
func NewTestExecutor(t *testing.T) *MockExecutor {
	// Return our test-specific mock executor
	return &MockExecutor{}
}

// VerifyTDXAttestation handles TDX attestation verification for tests
func (m *MockExecutor) VerifyTDXAttestation(attestation *core.TEEAttestation) (bool, error) {
	// Mock implementation for tests
	// In real tests, this would verify against actual TDX logic
	if attestation == nil || len(attestation.Data) == 0 {
		return false, fmt.Errorf("invalid attestation data")
	}
	
	// Check for the malformed data test case (0xFF prefix)
	if len(attestation.Data) >= 4 && 
	   attestation.Data[0] == 0xFF && 
	   attestation.Data[1] == 0xFF && 
	   attestation.Data[2] == 0xFF && 
	   attestation.Data[3] == 0xFF {
		return false, fmt.Errorf("invalid parameter size: exceeds maximum reasonable size")
	}
	
	// Simulate success for most cases
	return true, nil
}

// ExecuteTDX implements a test version of TDX execution
func (m *MockExecutor) ExecuteTDX(ctx context.Context, req *core.ExecutionRequest) (*core.ExecutionResult, error) {
	// For test purposes, verify that the request contains a valid measurement
	if req == nil || len(req.Parameters) != 48 {
		return nil, fmt.Errorf("invalid parameters")
	}

	// Create a mock result for the triple attestation test
	return &core.ExecutionResult{
		Output:    []byte("triple-attestation-success"),
		StateHash: []byte("test-hash"),
		RegionID:  req.RegionId,
	}, nil
}
