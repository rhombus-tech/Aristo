package policy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	// Global policy engine instance
	globalEngine     *PolicyEngine
	globalEngineMu   sync.Mutex
	globalEngineOnce sync.Once
)

// PolicyVerifier provides an interface to verify attestations using the policy engine
type PolicyVerifier struct {
	engine *PolicyEngine
}

// VerificationOptions configure the verification process
type VerificationOptions struct {
	// Whether to enforce policy constraints
	EnforcePolicy bool
	
	// Policy ID to use (empty means use default)
	PolicyID string
	
	// TEE type to verify
	TEEType string
	
	// Timeout for verification (0 means no timeout)
	Timeout time.Duration
}

// NewPolicyVerifier creates a new policy verifier with the global engine
func NewPolicyVerifier() (*PolicyVerifier, error) {
	engine, err := GetGlobalPolicyEngine()
	if err != nil {
		return nil, err
	}
	
	return &PolicyVerifier{
		engine: engine,
	}, nil
}

// NewPolicyVerifierWithEngine creates a new policy verifier with a custom engine
func NewPolicyVerifierWithEngine(engine *PolicyEngine) *PolicyVerifier {
	return &PolicyVerifier{
		engine: engine,
	}
}

// GetGlobalPolicyEngine returns the global policy engine instance
func GetGlobalPolicyEngine() (*PolicyEngine, error) {
	var initErr error
	
	globalEngineOnce.Do(func() {
		globalEngineMu.Lock()
		defer globalEngineMu.Unlock()
		
		// Determine policy directory
		policyDir := os.Getenv("TDX_POLICY_DIR")
		if policyDir == "" {
			// Default to a standard location
			policyDir = "/etc/tdx/policies"
			
			// For development, use a local directory if the default doesn't exist
			if _, err := os.Stat(policyDir); os.IsNotExist(err) {
				policyDir = "policies"
			}
		}
		
		// Create policy engine
		engine, err := NewPolicyEngine(policyDir)
		if err != nil {
			initErr = err
			return
		}
		
		globalEngine = engine
	})
	
	if initErr != nil {
		return nil, initErr
	}
	
	if globalEngine == nil {
		return nil, errors.New("failed to initialize global policy engine")
	}
	
	return globalEngine, nil
}

// VerifyTDXAttestation verifies TDX attestation against policies
func (v *PolicyVerifier) VerifyTDXAttestation(ctx context.Context, attestation []byte, measurement []byte, options *VerificationOptions) (*PolicyResult, error) {
	if options == nil {
		options = &VerificationOptions{
			EnforcePolicy: true,
			TEEType:       "TDX",
		}
	}
	
	// Add timeout if specified
	if options.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, options.Timeout)
		defer cancel()
	}
	
	result, err := v.engine.EvaluateTDXAttestation(ctx, attestation, measurement)
	if err != nil {
		return nil, err
	}
	
	// If policy enforcement is off, we still evaluate but don't fail on violations
	if !options.EnforcePolicy {
		result.Valid = true
	}
	
	return result, nil
}

// VerifySGXAttestation verifies SGX attestation against policies
func (v *PolicyVerifier) VerifySGXAttestation(ctx context.Context, attestation []byte, measurement []byte, options *VerificationOptions) (*PolicyResult, error) {
	if options == nil {
		options = &VerificationOptions{
			EnforcePolicy: true,
			TEEType:       "SGX",
		}
	}
	
	// Add timeout if specified
	if options.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, options.Timeout)
		defer cancel()
	}
	
	result, err := v.engine.EvaluateSGXAttestation(ctx, attestation, measurement)
	if err != nil {
		return nil, err
	}
	
	// If policy enforcement is off, we still evaluate but don't fail on violations
	if !options.EnforcePolicy {
		result.Valid = true
	}
	
	return result, nil
}

// VerifyAMDSEVAttestation verifies AMD SEV attestation against policies
func (v *PolicyVerifier) VerifyAMDSEVAttestation(ctx context.Context, attestation []byte, measurement []byte, options *VerificationOptions) (*PolicyResult, error) {
	if options == nil {
		options = &VerificationOptions{
			EnforcePolicy: true,
			TEEType:       "AMD-SEV",
		}
	}
	
	// Add timeout if specified
	if options.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, options.Timeout)
		defer cancel()
	}
	
	result, err := v.engine.EvaluateAMDSEVAttestation(ctx, attestation, measurement)
	if err != nil {
		return nil, err
	}
	
	// If policy enforcement is off, we still evaluate but don't fail on violations
	if !options.EnforcePolicy {
		result.Valid = true
	}
	
	return result, nil
}

// BatchVerificationResult contains the results of a batch verification
type BatchVerificationResult struct {
	SuccessCount   int
	FailureCount   int
	TotalCount     int
	Results        map[string]*PolicyResult
	Duration       time.Duration
	StartTime      time.Time
	EndTime        time.Time
}

// BatchVerifyTDXAttestations verifies multiple TDX attestations in a batch
func (v *PolicyVerifier) BatchVerifyTDXAttestations(ctx context.Context, attestations map[string][]byte, measurements map[string][]byte, options *VerificationOptions) (*BatchVerificationResult, error) {
	startTime := time.Now()
	
	// Initialize result
	result := &BatchVerificationResult{
		Results:   make(map[string]*PolicyResult),
		StartTime: startTime,
	}
	
	// For each attestation, evaluate against the policy
	for id, attestation := range attestations {
		measurement, ok := measurements[id]
		if !ok {
			return nil, fmt.Errorf("measurement not found for attestation %s", id)
		}
		
		// Verify attestation
		policyResult, err := v.VerifyTDXAttestation(ctx, attestation, measurement, options)
		if err != nil {
			return nil, fmt.Errorf("failed to verify attestation %s: %w", id, err)
		}
		
		// Store result
		result.Results[id] = policyResult
		
		// Update counts
		result.TotalCount++
		if policyResult.Valid {
			result.SuccessCount++
		} else {
			result.FailureCount++
		}
	}
	
	// Update timing
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(startTime)
	
	return result, nil
}

// LoadPolicyFromJSON loads a policy from a JSON string
func LoadPolicyFromJSON(jsonStr string) (*VerificationPolicy, error) {
	var policy VerificationPolicy
	if err := json.Unmarshal([]byte(jsonStr), &policy); err != nil {
		return nil, fmt.Errorf("failed to parse policy JSON: %w", err)
	}
	
	return &policy, nil
}

// CreateDefaultPolicies creates default policies for development/testing
func CreateDefaultPolicies(policyDir string) error {
	// Create directory if it doesn't exist
	if err := os.MkdirAll(policyDir, 0755); err != nil {
		return fmt.Errorf("failed to create policy directory: %w", err)
	}
	
	// Create a default policy for TDX
	tdxPolicy := VerificationPolicy{
		ID:          "default-tdx-policy",
		Name:        "Default TDX Policy",
		Version:     "1.0.0",
		Description: "Default policy for TDX attestation",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		TargetTEEs:  []string{"TDX"},
		Constraints: []Constraint{
			{
				ID:            "tdx-pcr-check",
				Type:          "measurement",
				Operator:      "equals",
				ExpectedValue: "ALLOWALL", // Special value to allow all measurements during development
				ErrorMessage:  "TDX measurement mismatch",
				Severity:      "fatal",
			},
		},
	}
	
	// Save policy to file
	tdxPolicyPath := filepath.Join(policyDir, "default-tdx-policy.json")
	tdxPolicyJSON, err := json.MarshalIndent(tdxPolicy, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal TDX policy: %w", err)
	}
	
	if err := ioutil.WriteFile(tdxPolicyPath, tdxPolicyJSON, 0644); err != nil {
		return fmt.Errorf("failed to write TDX policy file: %w", err)
	}
	
	// Create a default policy for SGX
	sgxPolicy := VerificationPolicy{
		ID:          "default-sgx-policy",
		Name:        "Default SGX Policy",
		Version:     "1.0.0",
		Description: "Default policy for SGX attestation",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		TargetTEEs:  []string{"SGX"},
		Constraints: []Constraint{
			{
				ID:            "sgx-mr-enclave-check",
				Type:          "measurement",
				Operator:      "equals",
				ExpectedValue: "ALLOWALL", // Special value to allow all measurements during development
				ErrorMessage:  "SGX MRENCLAVE mismatch",
				Severity:      "fatal",
			},
		},
	}
	
	// Save policy to file
	sgxPolicyPath := filepath.Join(policyDir, "default-sgx-policy.json")
	sgxPolicyJSON, err := json.MarshalIndent(sgxPolicy, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal SGX policy: %w", err)
	}
	
	if err := ioutil.WriteFile(sgxPolicyPath, sgxPolicyJSON, 0644); err != nil {
		return fmt.Errorf("failed to write SGX policy file: %w", err)
	}
	
	return nil
}
