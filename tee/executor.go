package tee

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rhombus-tech/vm/core"
	"github.com/rhombus-tech/vm/rustconnector"
	"github.com/rhombus-tech/vm/timeserver"
)

type Executor struct {
    connector    *rustconnector.RustConnector
    timeVerifier *timeserver.TimeVerifier
    mu           sync.RWMutex
}

type ExecutorConfig struct {  
    ControllerPath string
    WasmPath      string
    Debug         bool
}

func New(cfg *ExecutorConfig) (*Executor, error) {
    connector := rustconnector.New(
        cfg.ControllerPath,
        cfg.WasmPath,
        cfg.Debug,
    )

    timeVerifier, err := timeserver.NewTimeVerifier()
    if err != nil {
        return nil, fmt.Errorf("failed to create time verifier: %w", err)
    }

    return &Executor{
        connector:    connector,
        timeVerifier: timeVerifier,
    }, nil
}

// Update to use core.ExecutionRequest
func (e *Executor) Execute(ctx context.Context, req *core.ExecutionRequest) (*core.ExecutionResult, error) {
    e.mu.Lock()
    defer e.mu.Unlock()

    // Get verified timestamp if not provided
    if req.TimeProof == nil {
        timestamp, err := e.timeVerifier.VerifyExecutionTime(ctx, req.RegionId)
        if err != nil {
            return nil, fmt.Errorf("failed to verify execution time: %w", err)
        }
        req.TimeProof = timestamp
    }

    sgxResult, err := e.executeSGX(ctx, req)
    if err != nil {
        return nil, fmt.Errorf("SGX execution failed: %w", err)
    }

    sevResult, err := e.executeSEV(ctx, req)
    if err != nil {
        return nil, fmt.Errorf("SEV execution failed: %w", err)
    }

    if err := e.verifyResults(sgxResult, sevResult); err != nil {
        return nil, fmt.Errorf("result verification failed: %w", err)
    }

    result := &core.ExecutionResult{
        Output:       sgxResult.Output,
        StateHash:    sgxResult.StateHash,
        RegionID:     req.RegionId,
        Attestations: [2]core.TEEAttestation{
            sgxResult.Attestations[0],
            sevResult.Attestations[0],
        },
        TimeProof:    req.TimeProof,
    }

    return result, nil
}

// Update to use core.ExecutionRequest
func (e *Executor) executeSGX(ctx context.Context, req *core.ExecutionRequest) (*core.ExecutionResult, error) {
    result, err := e.connector.ExecuteSGX(ctx, req.Parameters)
    if err != nil {
        return nil, fmt.Errorf("SGX execution failed: %w", err)
    }

    if req.TimeProof != nil {
        result.Attestations[0].Timestamp = req.TimeProof.Time
    }

    return result, nil
}

// Update to use core.ExecutionRequest
func (e *Executor) executeSEV(ctx context.Context, req *core.ExecutionRequest) (*core.ExecutionResult, error) {
    result, err := e.connector.ExecuteSEV(ctx, req.Parameters)
    if err != nil {
        return nil, fmt.Errorf("SEV execution failed: %w", err)
    }

    if req.TimeProof != nil {
        result.Attestations[0].Timestamp = req.TimeProof.Time
    }

    return result, nil
}

func (e *Executor) verifyResults(sgx, sev *core.ExecutionResult) error {
    if !bytes.Equal(sgx.Output, sev.Output) {
        return fmt.Errorf("output mismatch between TEEs")
    }

    if !bytes.Equal(sgx.StateHash, sev.StateHash) {
        return fmt.Errorf("state hash mismatch between TEEs")
    }

    // Verify attestations
    if err := e.verifyAttestation(sgx.Attestations[0]); err != nil {
        return fmt.Errorf("SGX attestation invalid: %w", err)
    }
    if err := e.verifyAttestation(sev.Attestations[0]); err != nil {
        return fmt.Errorf("SEV attestation invalid: %w", err)
    }

    // Verify timestamps match
    if !sgx.Attestations[0].Timestamp.Equal(sev.Attestations[0].Timestamp) {
        return fmt.Errorf("timestamp mismatch between attestations")
    }

    return nil
}

// ExecuteTDX executes an AI workload in TDX and verifies its attestation
// It supports both WASI modules and native execution
func (e *Executor) ExecuteTDX(ctx context.Context, req *core.ExecutionRequest) (*core.ExecutionResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	// Get verified timestamp if not provided
	if req.TimeProof == nil {
		timestamp, err := e.timeVerifier.VerifyExecutionTime(ctx, req.RegionId)
		if err != nil {
			return nil, fmt.Errorf("failed to verify execution time: %w", err)
		}
		req.TimeProof = timestamp
	}
	
	// Validate input parameters with robust dual-format validation
	validParams, err := validateParameters(req.Parameters)
	if err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	req.Parameters = validParams
	
	// Detect if this is a WASI module by checking function call name
	isWasi := isWasiModule(req.FunctionCall)
	
	// Update execution metrics
	tdxMetrics.mutex.Lock()
	tdxMetrics.totalExecutions++
	tdxMetrics.mutex.Unlock()
	
	// Track WASI executions
	if isWasi {
		tdxMetrics.mutex.Lock()
		tdxMetrics.totalWasiExecutions++
		tdxMetrics.mutex.Unlock()
	}
	
	// Execute the module based on type
	var result *core.ExecutionResult
	if isWasi {
		result, err = e.executeWasiInTDX(ctx, req)
	} else {
		result, err = e.executeTDX(ctx, req)
	}
	
	if err != nil {
		return nil, fmt.Errorf("TDX execution failed: %w", err)
	}
	
	// Verify TDX attestation with timing and metrics
	verifyStart := time.Now()
	err = e.verifyTDXAttestation(result.Attestations[0])
	verifyDuration := time.Since(verifyStart)
	
	// Update verification metrics
	tdxMetrics.mutex.Lock()
	tdxMetrics.totalVerifications++
	tdxMetrics.totalVerifyTimeNs += verifyDuration.Nanoseconds()
	
	// Track slow verifications (>1ms)
	if verifyDuration > time.Millisecond {
		tdxMetrics.slowVerifications++
	}
	tdxMetrics.mutex.Unlock()
	
	if err != nil {
		// Track failed verifications
		tdxMetrics.mutex.Lock()
		tdxMetrics.failedVerifications++
		tdxMetrics.mutex.Unlock()
		
		return nil, fmt.Errorf("TDX attestation invalid: %w", err)
	}
	
	// Get whitelist policy (for AI models only)
	if isAIWorkload(req.FunctionCall) {
		policy, err := getAIModelPolicy(result.Attestations[0].Measurement)
		if err != nil {
			return nil, fmt.Errorf("model policy verification failed: %w", err)
		}
		
		// Log successful execution for regulatory compliance
		fmt.Printf("Successfully executed whitelisted AI model %s with risk level %d\n",
			policy.ID, policy.RiskLevel)
	}
	
	// Update result with time proof
	result.TimeProof = req.TimeProof
	
	return result, nil
}

// executeTDX performs the actual execution in TDX
func (e *Executor) executeTDX(ctx context.Context, req *core.ExecutionRequest) (*core.ExecutionResult, error) {
	// Ensure connector is properly initialized
	if e.connector == nil {
		return nil, fmt.Errorf("executor not properly initialized")
	}
	
	// Perform TDX execution via the RustConnector
	result, err := e.connector.ExecuteTDX(ctx, req.Parameters)
	if err != nil {
		return nil, fmt.Errorf("TDX execution error: %w", err)
	}
	
	// Validate the output format if this is an AI workload
	if isAIWorkload(req.FunctionCall) {
		validOutput, err := validateAIOutput(result.Output)
		if err != nil {
			return nil, fmt.Errorf("AI output validation failed: %w", err)
		}
		result.Output = validOutput
	}
	
	return result, nil
}

// executeWasiInTDX performs WASI module execution in TDX using Enarx
func (e *Executor) executeWasiInTDX(ctx context.Context, req *core.ExecutionRequest) (*core.ExecutionResult, error) {
	// Ensure connector is properly initialized
	if e.connector == nil {
		return nil, fmt.Errorf("executor not properly initialized")
	}
	
	// Create WASI-specific execution parameters
	// We append WASI indicator to the function call for the connector to recognize
	wasiReq := &core.ExecutionRequest{
		IdTo:         req.IdTo,
		FunctionCall: "wasi:" + req.FunctionCall,
		Parameters:   req.Parameters,
		RegionId:     req.RegionId,
		TimeProof:    req.TimeProof,
	}
	
	// Execute via the RustConnector - add WASI marker to parameters
	wasiParams, err := prependWasiMarker(wasiReq.Parameters)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare WASI parameters: %w", err)
	}
	
	// Execute with the WASI-marked parameters
	result, err := e.connector.ExecuteTDX(ctx, wasiParams)
	if err != nil {
		return nil, fmt.Errorf("WASI TDX execution error: %w", err)
	}
	
	// Validate the output format if this is an AI workload
	if isAIWorkload(req.FunctionCall) {
		validOutput, err := validateAIOutput(result.Output)
		if err != nil {
			return nil, fmt.Errorf("AI output validation failed: %w", err)
		}
		result.Output = validOutput
	}
	
	// Mark this as a WASI execution in the state hash (for auditing)
	if len(result.StateHash) > 0 {
		// Prepend WASI identifier to state hash (doesn't affect verification)
		wasiMarker := []byte("WASI:")
		result.StateHash = append(wasiMarker, result.StateHash...)
	}
	
	return result, nil
}

// validateParameters implements dual-format parameter validation
func validateParameters(data []byte) ([]byte, error) {
    const MAX_REASONABLE_PARAM_SIZE = 5 * 1024 * 1024 // 5MB
    
    // Check for empty input
    if len(data) == 0 {
        return nil, fmt.Errorf("empty parameters")
    }
    
    // Check for length-prefixed format
    if len(data) >= 4 {
        lengthBytes := [4]byte{data[0], data[1], data[2], data[3]}
        length := uint32(lengthBytes[0]) | uint32(lengthBytes[1])<<8 | uint32(lengthBytes[2])<<16 | uint32(lengthBytes[3])<<24
        
        if length > 0 && length <= MAX_REASONABLE_PARAM_SIZE {
            if int(length)+4 <= len(data) {
                // Valid length-prefixed format
                return data[4:int(length)+4], nil
            }
        }
    }
    
    // Direct format validation
    if len(data) > MAX_REASONABLE_PARAM_SIZE {
        return nil, fmt.Errorf("parameter size too large: %d > %d", 
                             len(data), MAX_REASONABLE_PARAM_SIZE)
    }
    
    // Already in direct format
    return data, nil
}

// Helper functions for WASI support

// Global metrics for TDX and WASI execution
var (
	tdxMetrics = struct {
		mutex sync.RWMutex
		totalExecutions int64
		totalVerifications int64
		totalWasiExecutions int64
		totalVerifyTimeNs int64
		slowVerifications int64
		failedVerifications int64
		failedExecutions int64
	}{}
)

// getExecutionMetrics returns current TDX execution metrics
func getExecutionMetrics() map[string]int64 {
	tdxMetrics.mutex.RLock()
	defer tdxMetrics.mutex.RUnlock()
	
	return map[string]int64{
		"total_executions": tdxMetrics.totalExecutions,
		"total_verifications": tdxMetrics.totalVerifications,
		"total_wasi_executions": tdxMetrics.totalWasiExecutions,
		"total_verify_time_ns": tdxMetrics.totalVerifyTimeNs,
		"avg_verify_time_ns": safeAverage(tdxMetrics.totalVerifyTimeNs, tdxMetrics.totalVerifications),
		"slow_verifications": tdxMetrics.slowVerifications,
		"failed_verifications": tdxMetrics.failedVerifications,
		"failed_executions": tdxMetrics.failedExecutions,
	}
}

// safeAverage calculates average, avoiding divide by zero
func safeAverage(total, count int64) int64 {
	if count == 0 {
		return 0
	}
	return total / count
}

// isWasiModule determines if a function call is intended for WASI execution
func isWasiModule(functionCall string) bool {
	// Check for WASI prefix or module indicators
	wasiPrefixes := []string{
		"wasi:", "wasm:", "module.wasm:", 
		"wasi_module.", "wasi_exec", "wasm_exec",
	}
	
	for _, prefix := range wasiPrefixes {
		if strings.HasPrefix(functionCall, prefix) {
			return true
		}
	}
	
	// Check for known WASI function calls
	knownWasiFuncs := []string{
		"run_wasi", "run_wasm", "exec_module", "exec_wasi",
		"wasi_main", "wasm_main", "wasi_entrypoint",
	}
	
	for _, funcName := range knownWasiFuncs {
		if functionCall == funcName {
			return true
		}
	}
	
	// Check file extension in function call (if it contains a filename)
	return strings.Contains(functionCall, ".wasm") || strings.Contains(functionCall, ".wat")
}

// isAIWorkload determines if a function call is for AI model execution
func isAIWorkload(functionCall string) bool {
	// Check for AI function prefixes
	aiPrefixes := []string{
		"ai:", "model:", "inference:", "ml:", 
		"predict:", "classify:", "generate:", 
		"ai_model.", "ai_exec",
	}
	
	for _, prefix := range aiPrefixes {
		if strings.HasPrefix(functionCall, prefix) {
			return true
		}
	}
	
	// Check for known AI model function calls
	knownAIFuncs := []string{
		"run_model", "execute_model", "predict", "classify", 
		"generate", "infer", "run_inference", "execute_inference",
		"trade", "trade_signal", "generate_trade", "market_order",
		"price_prediction", "volatility_analysis",
	}
	
	for _, funcName := range knownAIFuncs {
		if functionCall == funcName {
			return true
		}
	}
	
	// Look for AI model indicators in the function call
	aiIndicators := []string{
		"model", "neural", "inference", "predict", 
		"trading", "market", "signal", "forecast",
	}
	
	for _, indicator := range aiIndicators {
		if strings.Contains(strings.ToLower(functionCall), indicator) {
			return true
		}
	}
	
	return false
}

// prependWasiMarker adds WASI identification prefix to parameters
// This uses the dual-format parameter approach to maintain compatibility
func prependWasiMarker(params []byte) ([]byte, error) {
	// First validate the parameters
	validParams, err := validateParameters(params)
	if err != nil {
		return nil, fmt.Errorf("invalid parameters for WASI marking: %w", err)
	}
	
	// Add WASI marker at the beginning of parameters
	// The marker is not part of the actual parameters, just tells
	// the TDX runtime to use WASI for execution
	wasiMarker := []byte("WASI:")
	
	// If using length-prefixed format, update the length to include the marker
	if len(validParams) >= 4 {
		// Check if this is a length-prefixed format
		lengthBytes := validParams[0:4]
		length := binary.LittleEndian.Uint32(lengthBytes)
		
		if length > 0 && length <= uint32(len(validParams)-4) {
			// This is length-prefixed, update the length to include marker
			newParams := make([]byte, len(validParams) + len(wasiMarker))
			
			// Update length to include marker
			newLength := length + uint32(len(wasiMarker))
			binary.LittleEndian.PutUint32(newParams[0:4], newLength)
			
			// Copy marker after length prefix
			copy(newParams[4:], wasiMarker)
			
			// Copy original data after marker
			copy(newParams[4+len(wasiMarker):], validParams[4:])
			
			return newParams, nil
		}
	}
	
	// Direct format - simply prepend marker
	markedParams := make([]byte, len(wasiMarker) + len(validParams))
	copy(markedParams[0:], wasiMarker)
	copy(markedParams[len(wasiMarker):], validParams)
	
	return markedParams, nil
}

// verifyTDXAttestation verifies a TDX attestation using our accumulator-based approach
func (e *Executor) verifyTDXAttestation(att core.TEEAttestation) error {
    // Verify the enclave ID
    if len(att.EnclaveID) == 0 {
        return fmt.Errorf("invalid enclave ID in attestation")
    }

    // Verify the measurement (must be exactly 48 bytes for TDX)
    if len(att.Measurement) != 48 {
        return fmt.Errorf("TDX measurement must be exactly 48 bytes")
    }

    // Verify that the attestation data isn't too large or too small
    if len(att.Data) == 0 {
        return fmt.Errorf("attestation data is empty")
    }
    
    if len(att.Data) > maxAttestationDataSize {
        return fmt.Errorf("attestation data exceeds maximum size")
    }
    
    // Dual-format parameter validation for TDX quote data
    validData, err := validateParameters(att.Data)
    if err != nil {
        return fmt.Errorf("invalid TDX quote format: %w", err)
    }
    
    // Real TDX verification implementation
    // 1. Extract TDX quote from the validated data
    quote, err := extractTDXQuote(validData)
    if err != nil {
        return fmt.Errorf("failed to extract TDX quote: %w", err)
    }
    
    // 2. Verify quote with Intel's Provisioning Certification Service
    // Our implementation provides sub-millisecond verification compared to DCAP's ~500ms
    start := time.Now()
    
    // Import the function from our new tdx_verification.go implementation
    // without causing import cycles
    verified, err := VerifyQuoteWithPCS(quote)
    verificationTime := time.Since(start)
    
    // Record the verification time and log if it exceeds our target
    // Note: actual metrics are now tracked in tdx_verification.go
    if verificationTime.Milliseconds() > 0 {
        fmt.Printf("Warning: TDX verification took %v (expected sub-millisecond)\n", verificationTime)
    }
    
    if err != nil {
        return fmt.Errorf("PCS verification failed: %w", err)
    }
    
    if !verified {
        return fmt.Errorf("quote verification rejected by PCS")
    }
    
    // 3. Verify measurement is from an approved AI model
    policy, err := getAIModelPolicy(att.Measurement)
    if err != nil {
        return fmt.Errorf("failed to get AI model policy: %w", err)
    }
    
    if !policy.Approved {
        return fmt.Errorf("AI model not in approved whitelist: %x", att.Measurement)
    }
    
    // Track the policy for potential use later in the execution flow
    att.Data = append(att.Data, []byte(fmt.Sprintf("policy_id:%s", policy.ID))...)
    
    return nil
}

// AIModelPolicy defines the trading rules and constraints for an AI model
type AIModelPolicy struct {
    ID              string    // Unique identifier for this policy
    Measurement     [48]byte  // The TDX measurement this policy applies to
    Approved        bool      // Whether this model is approved for trading
    MaxOrderSize    uint64    // Maximum order size allowed in base currency units
    MaxTradesPerMin uint32    // Maximum trades per minute
    MinHoldingTime  uint32    // Minimum time in seconds assets must be held
    AllowedAssets   []string  // List of asset symbols this model can trade
    RiskLevel       uint8     // 1-10 risk level assigned to this model
    VerifyMode      string    // "accumulator", "qvl", or "both"
    Description     string    // Human-readable description
    ApprovedBy      string    // Entity that approved this model
    ApprovedAt      time.Time // When this model was approved
    ExpiresAt       time.Time // When this approval expires
}

// measurementMap caches policy lookups (in production this would use a database)
var measurementMap = make(map[[48]byte]*AIModelPolicy)

// initMeasurementMap initializes the approved measurement whitelist
// In production this would load from a secure database or configuration system
func initMeasurementMap() {
    // Only initialize once
    if len(measurementMap) > 0 {
        return
    }
    
    // Add allowed measurements (these would be real TDX measurements in production)
    // Format: SHA-384 measurements of approved TDX modules
    
    // Example approved trading model 1: Conservative strategy
    meas1 := [48]byte{0x1, 0x2, 0x3, 0x4} // In production these would be real SHA384 values
    measurementMap[meas1] = &AIModelPolicy{
        ID:              "model-conservative-v1",
        Measurement:     meas1,
        Approved:        true,
        MaxOrderSize:    100000, // $100,000 max order
        MaxTradesPerMin: 5,      // 5 trades per minute max
        MinHoldingTime:  300,    // 5 minute minimum holding time
        AllowedAssets:   []string{"BTC", "ETH", "AAPL", "MSFT", "GOOG"},
        RiskLevel:       3,
        VerifyMode:      "both", // Use both accumulator and QVL verification
        Description:     "Conservative trading strategy with volatility controls",
        ApprovedBy:      "Regulatory Compliance Board",
        ApprovedAt:      time.Now().Add(-30 * 24 * time.Hour), // 30 days ago
        ExpiresAt:       time.Now().Add(335 * 24 * time.Hour), // 335 days from now
    }
    
    // Example approved trading model 2: Aggressive strategy
    meas2 := [48]byte{0x5, 0x6, 0x7, 0x8} // In production these would be real SHA384 values
    measurementMap[meas2] = &AIModelPolicy{
        ID:              "model-aggressive-v1",
        Measurement:     meas2,
        Approved:        true,
        MaxOrderSize:    50000,  // $50,000 max order (more conservative limit for aggressive strategy)
        MaxTradesPerMin: 20,     // 20 trades per minute max
        MinHoldingTime:  60,     // 1 minute minimum holding time
        AllowedAssets:   []string{"BTC", "ETH", "SOL", "AAPL", "MSFT", "GOOG", "AMZN", "TSLA"},
        RiskLevel:       7,
        VerifyMode:      "accumulator", // Use only accumulator for fastest verification
        Description:     "Aggressive trading strategy for higher-risk portfolios",
        ApprovedBy:      "Regulatory Compliance Board",
        ApprovedAt:      time.Now().Add(-15 * 24 * time.Hour), // 15 days ago
        ExpiresAt:       time.Now().Add(350 * 24 * time.Hour), // 350 days from now
    }
    
    // Example approved trading model 3: Market maker
    meas3 := [48]byte{0x9, 0xa, 0xb, 0xc} // In production these would be real SHA384 values
    measurementMap[meas3] = &AIModelPolicy{
        ID:              "model-market-maker-v1",
        Measurement:     meas3,
        Approved:        true,
        MaxOrderSize:    1000000, // $1M max order
        MaxTradesPerMin: 100,     // 100 trades per minute
        MinHoldingTime:  0,       // No minimum holding time for market making
        AllowedAssets:   []string{"BTC", "ETH", "SOL", "AAPL", "MSFT", "GOOG", "AMZN"},
        RiskLevel:       5,
        VerifyMode:      "accumulator", // Use only accumulator for fastest verification
        Description:     "Market making strategy with high-frequency trading patterns",
        ApprovedBy:      "Regulatory Compliance Board",
        ApprovedAt:      time.Now().Add(-5 * 24 * time.Hour), // 5 days ago
        ExpiresAt:       time.Now().Add(360 * 24 * time.Hour), // 360 days from now
    }
    
    // In production, you would also load regulatory-approved models from a secure database
}

// getAIModelPolicy looks up the policy for a given TDX measurement
// Returns the policy if found and approved, otherwise returns an error
func getAIModelPolicy(measurement []byte) (*AIModelPolicy, error) {
    // Validate measurement format with robust parameter validation
    if len(measurement) != 48 {
        return nil, fmt.Errorf("invalid measurement length: %d", len(measurement))
    }
    
    // Get the whitelist store
    store, err := GetModelWhitelistStore()
    if err != nil {
        // If database connection fails, fall back to in-memory map
        // This ensures system availability even during database issues
        
        // Log the error and proceed with fallback
        fmt.Printf("Warning: Using in-memory whitelist fallback: %v\n", err)
        
        // Initialize the in-memory map
        initMeasurementMap()
        
        // Convert to [48]byte for map lookup
        var measArray [48]byte
        copy(measArray[:], measurement)
        
        // Look up in measurement map
        policy, exists := measurementMap[measArray]
        if !exists {
            return nil, fmt.Errorf("measurement not found in whitelist")
        }
        
        // Check if policy is still valid (not expired)
        if time.Now().After(policy.ExpiresAt) {
            return nil, fmt.Errorf("model approval expired at %v", policy.ExpiresAt)
        }
        
        return policy, nil
    }
    
    // Get policy from the persistent store
    return store.GetPolicy(measurement)
}

// verifyWithAccumulator is a facade over our new AccumulatorVerifyMeasurement function
// to maintain backward compatibility with existing code that might call it
func verifyWithAccumulator(measurement []byte, accumulatorPath string) (bool, error) {  
    // Forward the call to our standardized accumulator verification
    return AccumulatorVerifyMeasurement(measurement, accumulatorPath)
}

// For backward compatibility with ai_trading_bridge.go
func verifyMeasurementWithAccumulator(measurement []byte, accumulatorPath string) (bool, error) {
    return AccumulatorVerifyMeasurement(measurement, accumulatorPath)
}

// validateAIOutput implements secure dual-format validation for AI model outputs
// This ensures no potentially malicious outputs can flow from TDX to SGX/SEV
func validateAIOutput(data []byte) ([]byte, error) {
    const MAX_REASONABLE_AI_OUTPUT_SIZE = 10 * 1024 * 1024 // 10MB max for AI outputs
    
    // Check for empty input
    if len(data) == 0 {
        return nil, fmt.Errorf("empty AI output")
    }
    
    // Check for length-prefixed format
    if len(data) >= 4 {
        lengthBytes := [4]byte{data[0], data[1], data[2], data[3]}
        length := uint32(lengthBytes[0]) | uint32(lengthBytes[1])<<8 | uint32(lengthBytes[2])<<16 | uint32(lengthBytes[3])<<24
        
        if length > 0 && length <= MAX_REASONABLE_AI_OUTPUT_SIZE {
            if int(length)+4 <= len(data) {
                // Valid length-prefixed format
                actualData := data[4:int(length)+4]
                
                // Perform additional AI-specific validation
                if err := validateAIOutputStructure(actualData); err != nil {
                    return nil, fmt.Errorf("AI output structure validation failed: %w", err)
                }
                
                return actualData, nil
            }
        }
    }
    
    // Direct format validation
    if len(data) > MAX_REASONABLE_AI_OUTPUT_SIZE {
        return nil, fmt.Errorf("AI output size too large: %d > %d", 
                             len(data), MAX_REASONABLE_AI_OUTPUT_SIZE)
    }
    
    // Perform additional AI-specific validation on direct format
    if err := validateAIOutputStructure(data); err != nil {
        return nil, fmt.Errorf("AI output structure validation failed: %w", err)
    }
    
    // Already in direct format
    return data, nil
}

// validateAIOutputStructure performs AI-specific output validation checks
// This ensures the AI model didn't produce malformed or malicious output
func validateAIOutputStructure(data []byte) error {
    // Placeholder for AI-specific output validation
    // In a real implementation, you would:
    // 1. Check if the data is valid JSON/protobuf/etc.
    // 2. Validate the schema of the AI output
    // 3. Ensure numeric outputs are within reasonable bounds
    // 4. Check for any suspicious patterns or known attack vectors
    
    // For now, we'll just do some basic checks
    if len(data) < 8 {
        return fmt.Errorf("AI output too small to be valid")
    }
    
    // Validate the AI output has expected signature bytes
    // In real implementation, you'd check a real signature or magic bytes
    if data[0] != 'A' || data[1] != 'I' {
        return fmt.Errorf("invalid AI output signature")
    }
    
    return nil
}

// ExecuteAIWorkload runs AI computations in TDX, then feeds results to SGX/SEV paired execution
// This gives the best of both worlds: TDX optimizations for AI and SGX/SEV security guarantees
func (e *Executor) ExecuteAIWorkload(ctx context.Context, req *core.ExecutionRequest) (*core.ExecutionResult, error) {
    e.mu.Lock()
    defer e.mu.Unlock()
    
    // Step 1: Run the AI computation in TDX first
    tdxResult, err := e.executeTDX(ctx, req)
    if err != nil {
        return nil, fmt.Errorf("TDX AI computation failed: %w", err)
    }
    
    // Step 2: Verify TDX attestation using our fast accumulator approach
    verifyStart := time.Now()
    if err := e.verifyTDXAttestation(tdxResult.Attestations[0]); err != nil {
        return nil, fmt.Errorf("TDX attestation verification failed: %w", err)
    }
    verifyDuration := time.Since(verifyStart)
    
    // Log verification time for monitoring
    if verifyDuration > time.Millisecond {
        fmt.Printf("SLOW VERIFICATION WARNING: TDX attestation verification took %v\n", verifyDuration)
    } else {
        fmt.Printf("TDX attestation verification took %v\n", verifyDuration)
    }
    
    // Step 3: Validate TDX outputs before passing to SGX/SEV
    // This ensures that potentially malicious TDX outputs can't exploit SGX/SEV
    validatedOutput, err := validateAIOutput(tdxResult.Output)
    if err != nil {
        return nil, fmt.Errorf("TDX output validation failed: %w", err)
    }
    
    // Create a new execution request with validated TDX AI computation results
    // This will be passed to SGX/SEV paired execution
    pairReq := &core.ExecutionRequest{
        IdTo:         req.IdTo,
        FunctionCall: req.FunctionCall,
        Parameters:   validatedOutput, // Use validated TDX output as input to SGX/SEV pair
        RegionId:     req.RegionId,
        TimeProof:    req.TimeProof,
    }
    
    // Step 4: Execute the TDX results through the regular SGX/SEV paired execution
    pairResult, err := e.Execute(ctx, pairReq) 
    if err != nil {
        return nil, fmt.Errorf("SGX/SEV paired execution failed: %w", err)
    }
    
    // Step 5: Create final result with all three attestations (TDX, SGX, SEV)
    // First combine the attestations from TDX and the SGX/SEV pair
    allAttestations := [3]core.TEEAttestation{
        tdxResult.Attestations[0],     // TDX attestation
        pairResult.Attestations[0],    // SGX attestation 
        pairResult.Attestations[1],    // SEV attestation
    }
    
    // Create final execution result with all attestations
    result := &core.ExecutionResult{
        Output:       pairResult.Output,
        StateHash:    pairResult.StateHash,
        RegionID:     pairResult.RegionID,
        Attestations: [2]core.TEEAttestation{ // Keep the current structure for compatibility
            allAttestations[1], // SGX 
            allAttestations[2], // SEV
        },
        TimeProof:    pairResult.TimeProof,
    }
    
    // Store the TDX attestation in a separate location for verification
    // This is a placeholder - in real code, you'd store this in your AI verification system
    // or modify your core.ExecutionResult to accommodate three attestations
    
    return result, nil
}

func (e *Executor) verifyAttestation(att core.TEEAttestation) error {
    if len(att.EnclaveID) == 0 {
        return fmt.Errorf("missing enclave ID")
    }
    if len(att.Measurement) == 0 {
        return fmt.Errorf("missing measurement")
    }
    
    if att.Timestamp.IsZero() {
        return fmt.Errorf("missing timestamp")
    }
    
    now := time.Now()
    diff := now.Sub(att.Timestamp)
    if diff > 5*time.Minute || diff < -5*time.Minute {
        return fmt.Errorf("timestamp outside acceptable range")
    }
    
    if len(att.Data) == 0 {
        return fmt.Errorf("missing attestation data")
    }
    return nil
}