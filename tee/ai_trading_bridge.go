// Package tee provides integration with Trusted Execution Environments
package tee

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/rhombus-tech/vm/core"
)

// AITradingBridge connects TDX attestation with polynomial commitment verification
// for high-performance, regulatory-compliant AI trading
type AITradingBridge struct {
	executor       *Executor
	policyEnforcer *AIModelPolicyEnforcer
}

// AITradingResult encapsulates the result of an AI trading operation
// with all necessary proofs and attestations
type AITradingResult struct {
	// Core execution result
	ExecutionResult *core.ExecutionResult
	
	// AI-specific fields
	ModelID         string
	ModelPolicy     *AIModelPolicy
	TradeSignals    []TradeSignal
	
	// Polynomial commitment data
	PolynomialHash  []byte
	Witness         []byte
	
	// Timing data
	ExecutionTime   time.Duration
	VerificationTime time.Duration
}

// TradeSignal represents a single trade recommendation from the AI model
type TradeSignal struct {
	Asset           string  // Asset symbol (BTC, ETH, etc.)
	Direction       int8    // 1 = buy, -1 = sell, 0 = hold
	Quantity        float64 // Quantity to trade
	ConfidenceScore float64 // 0.0-1.0 confidence in the signal
	Timestamp       int64   // Unix timestamp when signal was generated
}

// AIModelPolicyEnforcer enforces trading policies for AI models
type AIModelPolicyEnforcer struct {
	// Rate limiting by model ID
	tradeCounts     map[string]int32
	lastResetTime   map[string]time.Time
	
	// Trading constraints
	assetWhitelist  map[string]bool
}

// NewAITradingBridge creates a new bridge between TDX attestation and polynomial commitments
func NewAITradingBridge(executor *Executor) *AITradingBridge {
	return &AITradingBridge{
		executor: executor,
		policyEnforcer: &AIModelPolicyEnforcer{
			tradeCounts:   make(map[string]int32),
			lastResetTime: make(map[string]time.Time),
			assetWhitelist: make(map[string]bool),
		},
	}
}

// ExecuteTradingModel executes an AI trading model with TDX attestation and
// returns the result with polynomial commitment verification
func (b *AITradingBridge) ExecuteTradingModel(
	ctx context.Context, 
	modelID string, 
	input []byte,
	userID string, // Added userID for regulatory audit trail
) (*AITradingResult, error) {
	startTime := time.Now()
	
	// Create execution request
	req := &core.ExecutionRequest{
		IdTo:         modelID,
		FunctionCall: "execute_trading_model",
		Parameters:   input,
		RegionId:     "global", // Using global region ID for AI trading
	}
	
	// Execute the AI workload using TDX and SGX/SEV verification
	result, err := b.executor.ExecuteAIWorkload(ctx, req)
	if err != nil {
		// Log the failed execution attempt for regulatory compliance
		store, storeErr := GetModelWhitelistStore()
		if storeErr == nil && len(result.Attestations) > 0 {
			store.LogModelExecution(
				modelID,
				result.Attestations[0].Measurement,
				userID,
				false,
				fmt.Sprintf("Execution failed: %v", err),
			)
		}
		return nil, fmt.Errorf("AI workload execution failed: %w", err)
	}
	
	// Extract AI model policy from the attestation
	policy, err := extractPolicyFromAttestation(result)
	if err != nil {
		return nil, fmt.Errorf("failed to extract policy: %w", err)
	}
	
	// Log successful execution for regulatory compliance
	store, storeErr := GetModelWhitelistStore()
	if storeErr == nil && len(result.Attestations) > 0 {
		store.LogModelExecution(
			modelID,
			result.Attestations[0].Measurement,
			userID,
			true,
			"Execution succeeded",
		)
	}
	
	// Parse trade signals from result
	signals, err := parseTradeSignals(result.Output)
	if err != nil {
		return nil, fmt.Errorf("failed to parse trade signals: %w", err)
	}
	
	// Enforce policy constraints on the trade signals
	validatedSignals, err := b.enforceTradePolicy(policy, signals)
	if err != nil {
		return nil, fmt.Errorf("policy enforcement failed: %w", err)
	}
	
	// Generate polynomial commitment for the trade signals
	polyHash, witness, err := generatePolynomialCommitment(validatedSignals, result.Attestations)
	if err != nil {
		return nil, fmt.Errorf("failed to generate polynomial commitment: %w", err)
	}
	
	executionTime := time.Since(startTime)
	
	// Return complete result with all verifiable data
	return &AITradingResult{
		ExecutionResult:  result,
		ModelID:          modelID,
		ModelPolicy:      policy,
		TradeSignals:     validatedSignals,
		PolynomialHash:   polyHash,
		Witness:          witness,
		ExecutionTime:    executionTime,
		VerificationTime: executionTime / 2, // Estimate verification time as half of total execution time
	}, nil
}

// enforceTradePolicy applies policy constraints to the trade signals
// Returns filtered signals that comply with policy requirements
func (b *AITradingBridge) enforceTradePolicy(
	policy *AIModelPolicy, 
	signals []TradeSignal,
) ([]TradeSignal, error) {
	// Initialize result
	validSignals := make([]TradeSignal, 0, len(signals))
	
	// Check rate limits
	modelID := policy.ID
	now := time.Now()
	lastReset, exists := b.policyEnforcer.lastResetTime[modelID]
	
	// Reset counters after a minute
	if !exists || now.Sub(lastReset) > time.Minute {
		b.policyEnforcer.tradeCounts[modelID] = 0
		b.policyEnforcer.lastResetTime[modelID] = now
	}
	
	// Check if rate limit is exceeded
	currentCount := b.policyEnforcer.tradeCounts[modelID]
	if currentCount >= int32(policy.MaxTradesPerMin) {
		return nil, fmt.Errorf("rate limit exceeded: %d trades in last minute (max: %d)",
			currentCount, policy.MaxTradesPerMin)
	}
	
	// Check each signal against policy
	for _, signal := range signals {
		// Check if asset is allowed
		assetAllowed := false
		for _, asset := range policy.AllowedAssets {
			if signal.Asset == asset {
				assetAllowed = true
				break
			}
		}
		
		if !assetAllowed {
			continue // Skip signals for non-allowed assets
		}
		
		// Apply confidence threshold (arbitrary threshold for example)
		if signal.ConfidenceScore < 0.7 {
			continue // Skip low-confidence signals
		}
		
		// Calculate order size in USD (simplified)
		// In production would use current price data
		// orderSizeUSD := signal.Quantity * getCurrentPrice(signal.Asset)
		orderSizeUSD := signal.Quantity * 1000 // Simplified for example
		
		// Check max order size
		if uint64(orderSizeUSD) > policy.MaxOrderSize {
			continue // Skip orders that exceed max size
		}
		
		// Signal passed all policy checks
		validSignals = append(validSignals, signal)
	}
	
	// Update trade count
	b.policyEnforcer.tradeCounts[modelID] += int32(len(validSignals))
	
	return validSignals, nil
}

// extractPolicyFromAttestation extracts the AI model policy from the execution result
func extractPolicyFromAttestation(result *core.ExecutionResult) (*AIModelPolicy, error) {
	// Ensure we have sufficient attestations
	if len(result.Attestations) < 1 {
		return nil, fmt.Errorf("no attestations in result")
	}
	
	// Find TDX attestation (could be any index)
	var tdxMeasurement []byte
	var tdxFound bool
	
	for _, att := range result.Attestations {
		// Identify TDX by measurement length and format
		// TDX measurements are 48 bytes (SHA-384)
		if len(att.Measurement) == 48 {
			// Additional TDX-specific validation could be added here
			// For now, we're using measurement length as a heuristic
			tdxMeasurement = att.Measurement
			tdxFound = true
			break
		}
	}
	
	// Ensure we found a TDX attestation
	if !tdxFound || len(tdxMeasurement) != 48 {
		return nil, fmt.Errorf("valid TDX attestation not found")
	}
	
	// Verify measurement against RSA accumulator
	accumulatorPath := os.Getenv("TDX_ACCUMULATOR_PATH")
	verified, err := verifyMeasurementWithAccumulator(tdxMeasurement, accumulatorPath)
	if err != nil {
		return nil, fmt.Errorf("accumulator verification error: %w", err)
	}
	if !verified {
		return nil, fmt.Errorf("model measurement verification failed")
	}
	
	// Get model policy from measurement
	return getAIModelPolicy(tdxMeasurement)
}

// parseTradeSignals converts raw output bytes to structured trade signals
func parseTradeSignals(output []byte) ([]TradeSignal, error) {
	// In production, this would parse JSON/Protobuf/etc.
	// For this example, using a simplified format
	
	// Check for valid AI output signature (from validateAIOutputStructure)
	if len(output) < 8 || output[0] != 'A' || output[1] != 'I' {
		return nil, fmt.Errorf("invalid AI output format")
	}
	
	// Parse signals (simplified placeholder)
	// In production would deserialize from a proper format
	
	// Example hardcoded signals for demonstration
	signals := []TradeSignal{
		{
			Asset:           "BTC",
			Direction:       1, // Buy
			Quantity:        0.5,
			ConfidenceScore: 0.85,
			Timestamp:       time.Now().Unix(),
		},
		{
			Asset:           "ETH",
			Direction:       1, // Buy
			Quantity:        5.0,
			ConfidenceScore: 0.78,
			Timestamp:       time.Now().Unix(),
		},
		{
			Asset:           "AAPL",
			Direction:       -1, // Sell
			Quantity:        10.0,
			ConfidenceScore: 0.92,
			Timestamp:       time.Now().Unix(),
		},
	}
	
	return signals, nil
}

// generatePolynomialCommitment creates a cryptographic commitment to the trade signals
// This allows verifiable proofs while preserving the confidentiality of the AI model
// while maintaining regulatory-grade auditability
func generatePolynomialCommitment(
	signals []TradeSignal, 
	attestations [2]core.TEEAttestation,
) ([]byte, []byte, error) {
	// Validate inputs using dual-format parameter validation pattern
	if len(signals) == 0 {
		return nil, nil, fmt.Errorf("no trade signals provided")
	}
	
	if len(attestations[0].Measurement) == 0 || len(attestations[1].Measurement) == 0 {
		return nil, nil, fmt.Errorf("invalid attestation measurements")
	}
	
	// Find TDX attestation (first attestation should be TDX, index 0)
	var tdxIndex int
	if len(attestations[0].Measurement) == 48 {
		tdxIndex = 0
	} else if len(attestations[1].Measurement) == 48 {
		tdxIndex = 1
	} else {
		return nil, nil, fmt.Errorf("TDX attestation with 48-byte measurement not found")
	}
	
	// Create polynomial commitment hash
	h := sha256.New()
	
	// Add TDX attestation measurement first for regulatory identification
	h.Write(attestations[tdxIndex].Measurement)
	
	// Add timestamp from attestation for time-binding
	timeBytes, err := attestations[tdxIndex].Timestamp.MarshalBinary()
	if err != nil {
		return nil, nil, fmt.Errorf("timestamp encoding error: %w", err)
	}
	h.Write(timeBytes)
	
	// Add second attestation measurement (SGX or SEV) for defense-in-depth
	sgxSevIndex := 1 - tdxIndex // Other index
	h.Write(attestations[sgxSevIndex].Measurement)
	
	// Add signals to the commitment with bounds checking
	for _, signal := range signals {
		// Asset: First validate length to prevent buffer overflows
		assetBytes := []byte(signal.Asset)
		if len(assetBytes) > 10 { // Max reasonable symbol length
			return nil, nil, fmt.Errorf("asset symbol too long: %s", signal.Asset)
		}
		h.Write(assetBytes)
		
		// Direction: validate to -1, 0, 1
		if signal.Direction < -1 || signal.Direction > 1 {
			return nil, nil, fmt.Errorf("invalid trade direction: %d", signal.Direction)
		}
		h.Write([]byte{byte(signal.Direction + 1)}) // Shift to 0,1,2 for safety
		
		// Convert float to bytes (simplified)
		qtyBytes := []byte(fmt.Sprintf("%f", signal.Quantity))
		h.Write(qtyBytes)
		
		confBytes := []byte(fmt.Sprintf("%f", signal.ConfidenceScore))
		h.Write(confBytes)
		
		tsBytes := []byte(fmt.Sprintf("%d", signal.Timestamp))
		h.Write(tsBytes)
	}
	
	// Generate the polynomial hash
	polyHash := h.Sum(nil)
	
	// Generate a dummy witness (in production this would be a real witness)
	witness := make([]byte, 64)
	copy(witness, polyHash)
	for i := 32; i < 64; i++ {
		witness[i] = byte(i)
	}
	
	return polyHash, witness, nil
}

// VerifyTrading verifies a previously executed trading operation
// This can be used for regulatory compliance and auditing
func (b *AITradingBridge) VerifyTrading(
	ctx context.Context,
	polyHash []byte, 
	witness []byte, 
	attestations [2]core.TEEAttestation,
) (bool, error) {
	// Start verification timer
	startTime := time.Now()
	
	// Verify TDX attestation first (sub-millisecond)
	if err := b.executor.verifyTDXAttestation(attestations[0]); err != nil {
		return false, fmt.Errorf("TDX attestation verification failed: %w", err)
	}
	
	// Verify other attestations
	if err := b.executor.verifyAttestation(attestations[1]); err != nil {
		return false, fmt.Errorf("SGX attestation verification failed: %w", err)
	}
	
	// In production, verify polynomial commitment with actual system
	// This is a simplified placeholder implementation
	
	// Calculate expected hash from witness
	expectedHash := sha256.Sum256(witness)
	
	// Check if hashes match
	verified := bytes.Equal(polyHash[:32], expectedHash[:])
	
	// Log verification time for performance tracking
	verificationTime := time.Since(startTime)
	if verificationTime.Milliseconds() > 1 {
		fmt.Printf("Warning: Trade verification took %v (expected sub-millisecond)\n", 
			verificationTime)
	}
	
	return verified, nil
}

// GetWhitelistedModels returns the list of whitelisted AI trading models
func (b *AITradingBridge) GetWhitelistedModels() ([]*AIModelPolicy, error) {
	// Get the whitelist store with production database
	store, err := GetModelWhitelistStore()
	if err != nil {
		// If database connection fails, fall back to in-memory map
		// This ensures system availability even during database issues
		
		// Log the error and proceed with fallback
		fmt.Printf("Warning: Using in-memory whitelist fallback for GetWhitelistedModels: %v\n", err)
		
		// Initialize the in-memory map
		initMeasurementMap()
		
		models := make([]*AIModelPolicy, 0, len(measurementMap))
		for _, policy := range measurementMap {
			if policy.Approved && time.Now().Before(policy.ExpiresAt) {
				models = append(models, policy)
			}
		}
		
		return models, nil
	}
	
	// Get policies from the persistent store
	policies, err := store.GetAllPolicies()
	if err != nil {
		return nil, fmt.Errorf("failed to get policies: %w", err)
	}
	
	// Filter approved policies
	approvedPolicies := make([]*AIModelPolicy, 0, len(policies))
	for _, policy := range policies {
		if policy.Approved && time.Now().Before(policy.ExpiresAt) {
			approvedPolicies = append(approvedPolicies, policy)
		}
	}
	
	return approvedPolicies, nil
}

// GetModelMeasurement returns the hex-encoded measurement for a model ID
func (b *AITradingBridge) GetModelMeasurement(modelID string) (string, error) {
	// Get the whitelist store with production database
	store, err := GetModelWhitelistStore()
	if err != nil {
		// If database connection fails, fall back to in-memory map
		// This ensures system availability even during database issues
		
		// Log the error and proceed with fallback
		fmt.Printf("Warning: Using in-memory whitelist fallback for GetModelMeasurement: %v\n", err)
		
		// Initialize the in-memory map
		initMeasurementMap()
		
		// Find model by ID in memory map
		for _, policy := range measurementMap {
			if policy.ID == modelID {
				return hex.EncodeToString(policy.Measurement[:]), nil
			}
		}
		
		return "", fmt.Errorf("model ID not found in whitelist: %s", modelID)
	}
	
	// Get policies from the persistent store
	policies, err := store.GetAllPolicies()
	if err != nil {
		return "", fmt.Errorf("failed to get policies: %w", err)
	}
	
	// Find model by ID
	for _, policy := range policies {
		if policy.ID == modelID {
			return hex.EncodeToString(policy.Measurement[:]), nil
		}
	}
	
	return "", fmt.Errorf("model ID not found in whitelist: %s", modelID)
}
