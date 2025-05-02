package policy

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bytecodealliance/wasmtime-go"
)

// PolicyEngine implements a WebAssembly-based policy engine for attestation verification
type PolicyEngine struct {
	// WebAssembly runtime state
	engine      *wasmtime.Engine
	store       *wasmtime.Store
	wasmCache   map[string]*wasmtime.Module
	
	// Policy state
	policyMu    sync.RWMutex
	policies    map[string]*VerificationPolicy
	
	// Configuration
	policyDir   string
	cacheDir    string
	logger      *log.Logger
}

// VerificationPolicy defines a declarative attestation verification policy
type VerificationPolicy struct {
	// Policy metadata
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	
	// WebAssembly module info
	WasmPath    string    `json:"wasm_path"`
	WasmHash    string    `json:"wasm_hash"`
	
	// Constraint definitions
	Constraints []Constraint `json:"constraints"`
	
	// Target TEE types
	TargetTEEs  []string   `json:"target_tees"`
}

// Constraint defines a single verification rule
type Constraint struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`      // e.g., "measurement", "quote", "certificate", "timestamp", etc.
	Operator    string    `json:"operator"`  // e.g., "equals", "contains", "greater_than", etc.
	ExpectedValue interface{} `json:"expected_value"`
	ErrorMessage string    `json:"error_message"`
	Severity     string    `json:"severity"` // "info", "warning", "error", "fatal"
	
	// For complex constraints using WebAssembly
	FunctionName string    `json:"function_name,omitempty"`
	Arguments    []string  `json:"arguments,omitempty"`
}

// PolicyResult contains the result of a policy evaluation
type PolicyResult struct {
	PolicyID    string
	Valid       bool
	Violations  []ConstraintViolation
	EvaluatedAt time.Time
	Duration    time.Duration
}

// ConstraintViolation represents a single constraint violation
type ConstraintViolation struct {
	ConstraintID string
	Message      string
	Severity     string
	ActualValue  interface{}
}

// NewPolicyEngine creates a new policy engine
func NewPolicyEngine(policyDir string, options ...PolicyOption) (*PolicyEngine, error) {
	engine := &PolicyEngine{
		engine:     wasmtime.NewEngine(),
		wasmCache:  make(map[string]*wasmtime.Module),
		policies:   make(map[string]*VerificationPolicy),
		policyDir:  policyDir,
		cacheDir:   filepath.Join(policyDir, "cache"),
		logger:     log.New(os.Stdout, "[PolicyEngine] ", log.LstdFlags),
	}
	
	// Apply options
	for _, option := range options {
		option(engine)
	}
	
	// Configure WebAssembly engine with secure defaults
	config := wasmtime.NewConfig()
	// Set security/performance limits appropriate for verification policies
	config.SetWasmMultiMemory(false)        // Disable multi-memory proposal for security
	
	// Create engine with security config and store
	engine.engine = wasmtime.NewEngineWithConfig(config)
	engine.store = wasmtime.NewStore(engine.engine)
	
	// Create directories if they don't exist
	if err := os.MkdirAll(engine.policyDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create policy directory: %w", err)
	}
	
	if err := os.MkdirAll(engine.cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}
	
	// Load policies
	if err := engine.LoadPolicies(); err != nil {
		return nil, fmt.Errorf("failed to load policies: %w", err)
	}
	
	return engine, nil
}

// PolicyOption configures the policy engine
type PolicyOption func(*PolicyEngine)

// WithLogger sets a custom logger
func WithLogger(logger *log.Logger) PolicyOption {
	return func(e *PolicyEngine) {
		e.logger = logger
	}
}

// WithCacheDir sets a custom cache directory
func WithCacheDir(cacheDir string) PolicyOption {
	return func(e *PolicyEngine) {
		e.cacheDir = cacheDir
	}
}

// LoadPolicies loads all policies from the policy directory
func (e *PolicyEngine) LoadPolicies() error {
	entries, err := ioutil.ReadDir(e.policyDir)
	if err != nil {
		return fmt.Errorf("failed to read policy directory: %w", err)
	}
	
	var loadErrors []error
	
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		
		policyPath := filepath.Join(e.policyDir, entry.Name())
		policy, err := LoadPolicyFromFile(policyPath)
		if err != nil {
			loadErrors = append(loadErrors, fmt.Errorf("failed to load policy %s: %w", policyPath, err))
			continue
		}
		
		e.policyMu.Lock()
		e.policies[policy.ID] = policy
		e.policyMu.Unlock()
		
		e.logger.Printf("Loaded policy %s: %s", policy.ID, policy.Name)
	}
	
	if len(loadErrors) > 0 {
		return fmt.Errorf("failed to load some policies: %v", loadErrors)
	}
	
	return nil
}

// LoadPolicyFromFile loads a single policy from a file
func LoadPolicyFromFile(path string) (*VerificationPolicy, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read policy file: %w", err)
	}
	
	var policy VerificationPolicy
	if err := json.Unmarshal(data, &policy); err != nil {
		return nil, fmt.Errorf("failed to parse policy file: %w", err)
	}
	
	// Validate policy
	if policy.ID == "" {
		return nil, errors.New("policy ID cannot be empty")
	}
	
	// If WebAssembly path is specified, ensure it exists
	if policy.WasmPath != "" {
		wasmPath := policy.WasmPath
		if !filepath.IsAbs(wasmPath) {
			wasmPath = filepath.Join(filepath.Dir(path), wasmPath)
		}
		
		if _, err := os.Stat(wasmPath); os.IsNotExist(err) {
			return nil, fmt.Errorf("WebAssembly module not found: %s", wasmPath)
		}
		
		// Calculate hash for the WebAssembly module
		wasmData, err := ioutil.ReadFile(wasmPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read WebAssembly module: %w", err)
		}
		
		policy.WasmHash = fmt.Sprintf("%x", sha256.Sum256(wasmData))
		
		// Update the WasmPath to be absolute
		policy.WasmPath = wasmPath
	}
	
	// Validate constraints
	for i, constraint := range policy.Constraints {
		if constraint.ID == "" {
			return nil, fmt.Errorf("constraint at index %d has empty ID", i)
		}
		
		if constraint.Type == "" {
			return nil, fmt.Errorf("constraint %s has empty type", constraint.ID)
		}
		
		if constraint.Type == "wasm" && constraint.FunctionName == "" {
			return nil, fmt.Errorf("WebAssembly constraint %s must specify a function name", constraint.ID)
		}
	}
	
	return &policy, nil
}

// EvaluateTDXAttestation evaluates TDX attestation against applicable policies
func (e *PolicyEngine) EvaluateTDXAttestation(ctx context.Context, attestation []byte, measurement []byte) (*PolicyResult, error) {
	return e.evaluateAttestation(ctx, "TDX", attestation, measurement)
}

// EvaluateSGXAttestation evaluates SGX attestation against applicable policies
func (e *PolicyEngine) EvaluateSGXAttestation(ctx context.Context, attestation []byte, measurement []byte) (*PolicyResult, error) {
	return e.evaluateAttestation(ctx, "SGX", attestation, measurement)
}

// EvaluateAMDSEVAttestation evaluates AMD SEV attestation against applicable policies
func (e *PolicyEngine) EvaluateAMDSEVAttestation(ctx context.Context, attestation []byte, measurement []byte) (*PolicyResult, error) {
	return e.evaluateAttestation(ctx, "AMD-SEV", attestation, measurement)
}

// evaluateAttestation is the internal implementation for policy evaluation
func (e *PolicyEngine) evaluateAttestation(ctx context.Context, teeType string, attestation []byte, measurement []byte) (*PolicyResult, error) {
	startTime := time.Now()
	
	// Filter policies applicable to this TEE type
	var applicablePolicies []*VerificationPolicy
	
	e.policyMu.RLock()
	for _, policy := range e.policies {
		for _, targetTEE := range policy.TargetTEEs {
			if targetTEE == teeType {
				applicablePolicies = append(applicablePolicies, policy)
				break
			}
		}
	}
	e.policyMu.RUnlock()
	
	if len(applicablePolicies) == 0 {
		return nil, fmt.Errorf("no applicable policies found for TEE type: %s", teeType)
	}
	
	// Use the first applicable policy (in a real system, you might have policy prioritization)
	policy := applicablePolicies[0]
	
	// Initialize result
	result := &PolicyResult{
		PolicyID:    policy.ID,
		Valid:       true,
		Violations:  []ConstraintViolation{},
		EvaluatedAt: time.Now(),
	}
	
	// Create evaluation context with attestation data
	evalCtx := &evaluationContext{
		teeType:      teeType,
		attestation:  attestation,
		measurement:  measurement,
		policyEngine: e,
	}
	
	// Evaluate each constraint
	for _, constraint := range policy.Constraints {
		if err := e.evaluateConstraint(ctx, constraint, evalCtx, result); err != nil {
			e.logger.Printf("Error evaluating constraint %s: %v", constraint.ID, err)
			
			// Add as violation
			result.Violations = append(result.Violations, ConstraintViolation{
				ConstraintID: constraint.ID,
				Message:      fmt.Sprintf("Error: %v", err),
				Severity:     "error",
			})
			
			// Mark as invalid for errors
			result.Valid = false
		}
	}
	
	// Check if there are any violations
	if len(result.Violations) > 0 {
		// Check if any of them are fatal
		for _, violation := range result.Violations {
			if violation.Severity == "fatal" {
				result.Valid = false
				break
			}
		}
	}
	
	result.Duration = time.Since(startTime)
	return result, nil
}

// evaluationContext holds the context for constraint evaluation
type evaluationContext struct {
	teeType      string
	attestation  []byte
	measurement  []byte
	policyEngine *PolicyEngine
}

// evaluateConstraint evaluates a single constraint against the attestation data
func (e *PolicyEngine) evaluateConstraint(ctx context.Context, constraint Constraint, evalCtx *evaluationContext, result *PolicyResult) error {
	// For simple constraints, evaluate directly
	switch constraint.Type {
	case "measurement":
		return e.evaluateMeasurementConstraint(constraint, evalCtx, result)
	case "wasm":
		return e.evaluateWasmConstraint(ctx, constraint, evalCtx, result)
	default:
		return fmt.Errorf("unsupported constraint type: %s", constraint.Type)
	}
}

// evaluateMeasurementConstraint evaluates a measurement constraint
func (e *PolicyEngine) evaluateMeasurementConstraint(constraint Constraint, evalCtx *evaluationContext, result *PolicyResult) error {
	switch constraint.Operator {
	case "equals":
		expected, ok := constraint.ExpectedValue.(string)
		if !ok {
			return fmt.Errorf("expected value must be a hex string for 'equals' operator")
		}
		
		// Special handling for ALLOWALL value which allows any measurement
		if expected == "ALLOWALL" {
			// Skip validation, all measurements are allowed
			return nil
		}

		// Compare measurement
		// In a real system, you would convert the expected value from hex and compare
		measurementHash := fmt.Sprintf("%x", sha256.Sum256(evalCtx.measurement))
		if measurementHash != expected {
			result.Violations = append(result.Violations, ConstraintViolation{
				ConstraintID: constraint.ID,
				Message:      constraint.ErrorMessage,
				Severity:     constraint.Severity,
				ActualValue:  measurementHash,
			})
			
			if constraint.Severity == "error" || constraint.Severity == "fatal" {
				result.Valid = false
			}
		}
		
		return nil
	default:
		return fmt.Errorf("unsupported operator for measurement constraint: %s", constraint.Operator)
	}
}

// WasmConstraint represents a WebAssembly-based constraint
type WasmConstraint struct {
	Name         string
	WasmModule   string
	FunctionName string
	Arguments    []string
}

// evaluateWasmConstraint evaluates a WebAssembly-based constraint
func (e *PolicyEngine) evaluateWasmConstraint(ctx context.Context, constraint Constraint, evalCtx *evaluationContext, result *PolicyResult) error {
	// Convert generic constraint to WasmConstraint format
	wasmConstraint := WasmConstraint{
		Name:         constraint.ID,
		WasmModule:   filepath.Join(e.policyDir, "wasm", constraint.Type+".wasm"),
		FunctionName: constraint.FunctionName,
		Arguments:    constraint.Arguments,
	}

	// Function name to call must be specified
	if wasmConstraint.FunctionName == "" {
		return fmt.Errorf("WebAssembly constraint must specify a function name")
	}

	// Log policy execution for tracing purposes (if needed)
	policyID := result.PolicyID
	if e.logger != nil {
		e.logger.Printf("Evaluating WebAssembly constraint for policy: %s", policyID)
	}
	
	// Get the WebAssembly module
	module, err := e.getOrLoadWasmModule(wasmConstraint.WasmModule)
	if err != nil {
		return fmt.Errorf("failed to load WebAssembly module: %w", err)
	}

	// Create store and get instance
	store := wasmtime.NewStore(e.engine)
	
	// Set up WebAssembly memory for data exchange
	memoryType := wasmtime.NewMemoryType(1, true, 10) // 1 minimum page, 10 maximum pages
	memory, err := wasmtime.NewMemory(store, memoryType)
	if err != nil {
		return fmt.Errorf("failed to create WebAssembly memory: %w", err)
	}
	
	// Set up imports (memory & host functions)
	imports := setupWasmImports(store, memory)
	instance, err := wasmtime.NewInstance(store, module, imports)
	if err != nil {
		return fmt.Errorf("failed to instantiate WebAssembly module: %w", err)
	}
	
	// Get function to call
	func_ := instance.GetFunc(store, wasmConstraint.FunctionName)
	if func_ == nil {
		return fmt.Errorf("function %s not found in WebAssembly module", wasmConstraint.FunctionName)
	}
	
	// Create a memory manager for the WebAssembly instance
	memMgr := newWasmMemoryManager(memory, store)
	
	// Prepare data to pass to WebAssembly
	data := map[string]interface{}{
		"attestation":  evalCtx.attestation,
		"measurement":  evalCtx.measurement,
		"tee_type":     evalCtx.teeType,
	}
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal data for WebAssembly: %w", err)
	}

	// Allocate memory and write data
	dataOffset, err := memMgr.allocate(len(dataBytes))
	if err != nil {
		return fmt.Errorf("failed to allocate WebAssembly memory: %w", err)
	}
	
	// Write data to WebAssembly memory
	err = memMgr.writeBytes(dataOffset, dataBytes)
	if err != nil {
		return fmt.Errorf("failed to write data to WebAssembly memory: %w", err)
	}
	
	// Prepare parameters for the WebAssembly function call
	var params []interface{}
	
	// Add data pointer and length as first two parameters
	params = append(params, int32(dataOffset), int32(len(dataBytes)))
	
	// Add any additional parameters from constraint.Arguments
	for _, arg := range wasmConstraint.Arguments {
		// Parse and convert argument based on type
		// For simplicity, we'll assume all arguments are integers
		var intValue int32
		_, err := fmt.Sscanf(arg, "%d", &intValue)
		if err == nil {
			params = append(params, intValue)
		} else {
			// For non-numeric arguments, pass as string pointer
			argBytes := []byte(arg)
			
			// Allocate memory for string argument
			argOffset, err := memMgr.allocate(len(argBytes) + 1) // +1 for null terminator
			if err != nil {
				return fmt.Errorf("failed to allocate memory for argument: %w", err)
			}
			
			// Write string argument to memory with null terminator
			argWithNull := append(argBytes, 0)
			memMgr.writeBytes(argOffset, argWithNull)
			
			// Pass pointer to the string
			params = append(params, int32(argOffset))
		}
	}

	// Call the WebAssembly function
	resultValue, err := func_.Call(store, params...)
	if err != nil {
		return fmt.Errorf("WebAssembly execution failed: %w", err)
	}

	// Extract result value and determine if constraint passed
	var valid bool
	var errorMessage string
	
	// Handle different result types returned by WebAssembly
	switch v := resultValue.(type) {
	case int32:
		// Simple integer result: 1 = success, 0 = failure, other = error code
		valid = v == 1
		if !valid {
			errorMessage = fmt.Sprintf("Constraint failed with code %d", v)
		}
		
	case int64:
		// Integer result as int64
		valid = v == 1
		if !valid {
			errorMessage = fmt.Sprintf("Constraint failed with code %d", v)
		}
		
	case []interface{}:
		// Multiple return values: typically [statusCode, errorMessagePtr]
		if len(v) > 0 {
			if statusCode, ok := v[0].(int32); ok {
				valid = statusCode == 1
				
				// If there's an error message pointer, read it
				if !valid && len(v) > 1 {
					if errPtr, ok := v[1].(int32); ok && errPtr > 0 {
						errorMessage = readNullTerminatedString(memory, store, uint32(errPtr))
					}
				}
			}
		}
		
	default:
		// Unexpected result type
		return fmt.Errorf("unexpected result type from WebAssembly function: %T", resultValue)
	}
	
	// Use the constraint's error message if none was provided by the Wasm function
	if !valid && errorMessage == "" {
		errorMessage = constraint.ErrorMessage
		if errorMessage == "" {
			errorMessage = "WebAssembly constraint failed"
		}
	}

	// Record violation if constraint failed
	if !valid {
		result.Violations = append(result.Violations, ConstraintViolation{
			ConstraintID: constraint.ID,
			Message:      errorMessage,
			Severity:     constraint.Severity,
			ActualValue:  nil, // Could extract from WebAssembly memory if needed
		})
		
		// Mark result as invalid if severity is error or fatal
		if constraint.Severity == "error" || constraint.Severity == "fatal" {
			result.Valid = false
		}
	}
	
	return nil
}

// AddPolicy adds a new policy to the engine
func (e *PolicyEngine) AddPolicy(policy *VerificationPolicy) error {
	e.policyMu.Lock()
	defer e.policyMu.Unlock()
	
	// Check if policy already exists
	if _, exists := e.policies[policy.ID]; exists {
		return fmt.Errorf("policy with ID %s already exists", policy.ID)
	}
	
	// Add policy
	e.policies[policy.ID] = policy
	
	// Save policy to disk
	if err := e.savePolicy(policy); err != nil {
		return fmt.Errorf("failed to save policy: %w", err)
	}
	
	return nil
}

// savePolicy saves a policy to disk
func (e *PolicyEngine) savePolicy(policy *VerificationPolicy) error {
	data, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal policy: %w", err)
	}
	
	// Create policy file path
	policyPath := filepath.Join(e.policyDir, fmt.Sprintf("%s.json", policy.ID))
	
	// Write to disk
	if err := ioutil.WriteFile(policyPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write policy file: %w", err)
	}
	
	return nil
}

// GetPolicy returns a policy by ID
func (e *PolicyEngine) GetPolicy(id string) (*VerificationPolicy, error) {
	e.policyMu.RLock()
	defer e.policyMu.RUnlock()
	
	policy, exists := e.policies[id]
	if !exists {
		return nil, fmt.Errorf("policy not found: %s", id)
	}
	
	return policy, nil
}
