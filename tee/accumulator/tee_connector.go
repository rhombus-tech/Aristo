// Package accumulator provides a high-performance cryptographic accumulator
// with cross-TEE integration capabilities
package accumulator

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
	
	pb "github.com/rhombus-tech/vm/tee/proto"
)

// WasmInterface defines methods for communicating with the WebAssembly accumulator
type WasmInterface interface {
	// ExecuteInTee executes a function inside a TEE environment
	ExecuteInTee(ctx context.Context, function string, params []byte, useLengthPrefix bool) ([]byte, error)
	
	// GetTeeType returns the type of TEE (SGX or SEV)
	GetTeeType() string
	
	// GetTeeID returns the identifier for the TEE
	GetTeeID() string
}

// TeeConnector integrates the high-performance Go accumulator with the Rust WebAssembly accumulator
type TeeConnector struct {
	// High-performance Go accumulator client
	client *OptimizedRsaClient
	
	// Interface to the WebAssembly TEE
	wasmInterface WasmInterface
	
	// Regional information
	region string
	
	// Performance tracking
	startTime time.Time
	opCount   uint64
}

// TeeConnectorOptions configures a new TeeConnector
type TeeConnectorOptions struct {
	// BatchSize for accumulator operations
	BatchSize int
	
	// EnableAsync for asynchronous processing
	EnableAsync bool
	
	// Parallelism level for concurrent operations
	Parallelism int
	
	// RegionID for cross-regional verification
	RegionID string
	
	// ModulusBits for RSA operations (default: 2048)
	ModulusBits int
	
	// VerifyInterval for batch processing
	VerifyInterval time.Duration
}

// DefaultTeeConnectorOptions returns sensible defaults for production use
func DefaultTeeConnectorOptions() TeeConnectorOptions {
	return TeeConnectorOptions{
		BatchSize:      1000,
		EnableAsync:    true,
		Parallelism:    8,
		RegionID:       "us-east-1",
		ModulusBits:    2048,
		VerifyInterval: 100 * time.Millisecond,
	}
}

// NewTeeConnector creates a new connector between the Go and Rust accumulators
func NewTeeConnector(wasmInterface WasmInterface, opts TeeConnectorOptions) (*TeeConnector, error) {
	// Create the optimized RSA client with the specified options
	client, err := NewOptimizedRsaClient(
		wasmInterface.GetTeeID(),
		wasmInterface.GetTeeType(),
		OptimizedRsaOptions{
			BatchSize:      opts.BatchSize,
			EnableAsync:    opts.EnableAsync,
			Parallelism:    opts.Parallelism,
			ModulusBits:    opts.ModulusBits,
			VerifyInterval: opts.VerifyInterval,
			RegionID:       opts.RegionID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create optimized RSA client: %v", err)
	}
	
	return &TeeConnector{
		client:        client,
		wasmInterface: wasmInterface,
		region:        opts.RegionID,
		startTime:     time.Now(),
	}, nil
}

// Close cleans up resources used by the connector
func (c *TeeConnector) Close() {
	c.client.Close()
}

// VerifyAttestationWithCrossCheck verifies an attestation using both accumulators
// with cross-TEE verification between SGX and SEV
func (c *TeeConnector) VerifyAttestationWithCrossCheck(
	ctx context.Context, 
	sgxAttestation, sevAttestation []byte,
) (bool, error) {
	// First, verify in the Rust WebAssembly accumulator inside the TEE
	valid, err := c.verifyInTee(ctx, sgxAttestation, sevAttestation)
	if err != nil {
		return false, fmt.Errorf("TEE verification failed: %v", err)
	}
	
	if !valid {
		return false, nil
	}
	
	// Now, verify using the Go high-performance accumulator for redundancy
	// Create a combined hash of both attestations for the accumulator element
	combinedHash := sha256.Sum256(append(sgxAttestation, sevAttestation...))
	
	element := &pb.AccumulatorElement{
		Executor:    c.wasmInterface.GetTeeID(),
		Measurement: combinedHash[:],
		EnclaveType: c.wasmInterface.GetTeeType(),
		Timestamp:   uint64(time.Now().Unix()),
	}
	
	// Add element to the high-performance accumulator
	c.client.AddToBatch(element)
	
	// Process the batch if needed
	if err := c.client.ProcessBatch(ctx); err != nil {
		return false, fmt.Errorf("failed to process batch: %v", err)
	}
	
	// Get witness for the element
	witness, err := c.client.GetWitnessForElement(element)
	if err != nil {
		return false, fmt.Errorf("failed to get witness: %v", err)
	}
	
	// Verify the witness
	valid, err = c.client.VerifyWitness(witness)
	if err != nil {
		return false, fmt.Errorf("failed to verify witness: %v", err)
	}
	
	// Increment operation counter for performance tracking
	c.opCount++
	
	return valid, nil
}

// BatchVerifyAttestations verifies multiple attestations in a batch for high throughput
func (c *TeeConnector) BatchVerifyAttestations(
	ctx context.Context,
	attestations []struct {
		SGXTEE []byte
		SEVTEE []byte
	},
) (map[string]bool, error) {
	results := make(map[string]bool)
	
	// Prepare elements for the Go accumulator
	elements := make([]*pb.AccumulatorElement, 0, len(attestations))
	
	// Prepare params for batch verification in the TEE
	batchParams := make([]struct {
		ID            string `json:"id"`
		SGXAttestation []byte `json:"sgx_attestation"`
		SEVAttestation []byte `json:"sev_attestation"`
	}, len(attestations))
	
	// Process each attestation pair
	for i, att := range attestations {
		id := fmt.Sprintf("attestation-%d", i)
		
		// Add to batch params for TEE verification
		batchParams[i] = struct {
			ID            string `json:"id"`
			SGXAttestation []byte `json:"sgx_attestation"`
			SEVAttestation []byte `json:"sev_attestation"`
		}{
			ID:            id,
			SGXAttestation: att.SGXTEE,
			SEVAttestation: att.SEVTEE,
		}
		
		// Create element for Go accumulator
		combinedHash := sha256.Sum256(append(att.SGXTEE, att.SEVTEE...))
		elements = append(elements, &pb.AccumulatorElement{
			Executor:    id,
			Measurement: combinedHash[:],
			EnclaveType: c.wasmInterface.GetTeeType(),
			Timestamp:   uint64(time.Now().Unix()),
		})
		
		// Initialize result as false
		results[id] = false
	}
	
	// Verify in the TEE
	teeResults, err := c.batchVerifyInTee(ctx, batchParams)
	if err != nil {
		return results, fmt.Errorf("TEE batch verification failed: %v", err)
	}
	
	// Add valid elements to the Go accumulator
	validElements := make([]*pb.AccumulatorElement, 0)
	for id, valid := range teeResults {
		results[id] = valid
		if valid {
			for _, elem := range elements {
				if elem.Executor == id {
					validElements = append(validElements, elem)
					break
				}
			}
		}
	}
	
	// Only continue with Go verification for elements that passed TEE verification
	if len(validElements) > 0 {
		// Add elements to the high-performance accumulator
		for _, elem := range validElements {
			c.client.AddToBatch(elem)
		}
		
		// Process the batch
		if err := c.client.ProcessBatch(ctx); err != nil {
			return results, fmt.Errorf("failed to process batch: %v", err)
		}
		
		// Get and verify witnesses
		for _, elem := range validElements {
			witness, err := c.client.GetWitnessForElement(elem)
			if err != nil {
				results[elem.Executor] = false
				continue
			}
			
			valid, err := c.client.VerifyWitness(witness)
			if err != nil {
				results[elem.Executor] = false
				continue
			}
			
			results[elem.Executor] = valid
		}
	}
	
	// Update operation count
	c.opCount += uint64(len(attestations))
	
	return results, nil
}

// verifyInTee performs verification inside the WebAssembly TEE
func (c *TeeConnector) verifyInTee(ctx context.Context, sgxAttestation, sevAttestation []byte) (bool, error) {
	// Prepare parameters for WebAssembly execution
	params := struct {
		SGXAttestation []byte `json:"sgx_attestation"`
		SEVAttestation []byte `json:"sev_attestation"`
	}{
		SGXAttestation: sgxAttestation,
		SEVAttestation: sevAttestation,
	}
	
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return false, fmt.Errorf("failed to marshal parameters: %v", err)
	}
	
	// Execute verification in the TEE using Rust WebAssembly accumulator
	resultBytes, err := c.wasmInterface.ExecuteInTee(ctx, "verify_attestation", paramsBytes, true)
	if err != nil {
		return false, fmt.Errorf("TEE execution failed: %v", err)
	}
	
	var teeResult struct {
		Valid bool   `json:"valid"`
		Error string `json:"error,omitempty"`
	}
	
	if err := json.Unmarshal(resultBytes, &teeResult); err != nil {
		return false, fmt.Errorf("failed to parse TEE result: %v", err)
	}
	
	if teeResult.Error != "" {
		return false, fmt.Errorf("TEE error: %s", teeResult.Error)
	}
	
	return teeResult.Valid, nil
}

// batchVerifyInTee performs batch verification inside the WebAssembly TEE
func (c *TeeConnector) batchVerifyInTee(
	ctx context.Context,
	batchParams []struct {
		ID            string `json:"id"`
		SGXAttestation []byte `json:"sgx_attestation"`
		SEVAttestation []byte `json:"sev_attestation"`
	},
) (map[string]bool, error) {
	// Marshal batch parameters
	paramsBytes, err := json.Marshal(struct {
		Attestations []struct {
			ID            string `json:"id"`
			SGXAttestation []byte `json:"sgx_attestation"`
			SEVAttestation []byte `json:"sev_attestation"`
		} `json:"attestations"`
	}{
		Attestations: batchParams,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal batch parameters: %v", err)
	}
	
	// Execute batch verification in the TEE
	resultBytes, err := c.wasmInterface.ExecuteInTee(ctx, "batch_verify_attestation", paramsBytes, true)
	if err != nil {
		return nil, fmt.Errorf("TEE batch execution failed: %v", err)
	}
	
	var teeResult struct {
		Results map[string]bool `json:"results"`
		Error   string          `json:"error,omitempty"`
	}
	
	if err := json.Unmarshal(resultBytes, &teeResult); err != nil {
		return nil, fmt.Errorf("failed to parse TEE batch result: %v", err)
	}
	
	if teeResult.Error != "" {
		return nil, fmt.Errorf("TEE batch error: %s", teeResult.Error)
	}
	
	return teeResult.Results, nil
}

// RegisterAttestation registers a new attestation with both accumulators
func (c *TeeConnector) RegisterAttestation(ctx context.Context, attestation []byte) (bool, error) {
	// Register in the TEE
	params := struct {
		Attestation []byte `json:"attestation"`
	}{
		Attestation: attestation,
	}
	
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return false, fmt.Errorf("failed to marshal parameters: %v", err)
	}
	
	// Execute registration in the TEE
	resultBytes, err := c.wasmInterface.ExecuteInTee(ctx, "register_attestation", paramsBytes, true)
	if err != nil {
		return false, fmt.Errorf("TEE execution failed: %v", err)
	}
	
	var teeResult struct {
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}
	
	if err := json.Unmarshal(resultBytes, &teeResult); err != nil {
		return false, fmt.Errorf("failed to parse TEE result: %v", err)
	}
	
	if !teeResult.Success {
		return false, fmt.Errorf("TEE registration failed: %s", teeResult.Error)
	}
	
	// Add to the Go accumulator as well
	element := &pb.AccumulatorElement{
		Executor:    c.wasmInterface.GetTeeID(),
		Measurement: attestation,
		EnclaveType: c.wasmInterface.GetTeeType(),
		Timestamp:   uint64(time.Now().Unix()),
	}
	
	c.client.AddToBatch(element)
	
	// Process the batch
	if err := c.client.ProcessBatch(ctx); err != nil {
		return false, fmt.Errorf("failed to process batch: %v", err)
	}
	
	c.opCount++
	
	return true, nil
}

// GetPerformanceStats returns performance statistics for the connector
func (c *TeeConnector) GetPerformanceStats() map[string]interface{} {
	// Get stats from the Go accumulator
	goStats := c.client.GetPerformanceStats()
	
	// Calculate overall stats
	elapsedSeconds := time.Since(c.startTime).Seconds()
	overallTPS := float64(c.opCount) / elapsedSeconds
	
	// Combine stats
	stats := map[string]interface{}{
		"operation_count":        c.opCount,
		"elapsed_seconds":        elapsedSeconds,
		"overall_tps":            overallTPS,
		"go_accumulator":         goStats,
		"tee_type":               c.wasmInterface.GetTeeType(),
		"tee_id":                 c.wasmInterface.GetTeeID(),
		"region":                 c.region,
	}
	
	return stats
}
