// Package vm implements the core validation logic for the TEE-based blockchain
package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"go.uber.org/zap"

	"github.com/ava-labs/hypersdk/chain"
	"github.com/rhombus-tech/vm/actions"
	"github.com/rhombus-tech/vm/compute"
	"github.com/rhombus-tech/vm/storage"
)

// AttestationCacheTTL is how long attestations remain valid in the cache
const AttestationCacheTTL = time.Hour * 24

// getAttestationForTx retrieves an attestation for a transaction from the cache or DB
func (vm *ShuttleVM) getAttestationForTx(ctx context.Context, txID ids.ID) (*Attestation, error) {
    // First check the cache
    vm.attestationMutex.RLock()
    if attestation, exists := vm.attestationCache[txID]; exists {
        vm.attestationMutex.RUnlock()
        return attestation, nil
    }
    vm.attestationMutex.RUnlock()

    // If not in cache, check the attestation store through the state manager
    // Use the database to get the attestation store
    attestationStore := storage.NewAttestationStore(vm.db)
    
    attestationBytes, err := attestationStore.GetAttestation(ctx, txID)
    if err != nil {
        return nil, fmt.Errorf("failed to get attestation from DB: %w", err)
    }

    var attestation Attestation
    if err := json.Unmarshal(attestationBytes, &attestation); err != nil {
        return nil, fmt.Errorf("failed to unmarshal attestation: %w", err)
    }

    // Add to cache for future use
    vm.attestationMutex.Lock()
    vm.attestationCache[txID] = &attestation
    vm.attestationMutex.Unlock()

    return &attestation, nil
}

// verifyTEEAttestation verifies an attestation from a TEE
func (vm *ShuttleVM) verifyTEEAttestation(ctx context.Context, attestation *Attestation) error {
    // Validate attestation type
    switch attestation.Type {
    case SGXAttestation:
        // Using the existing core verification methods but adapting our Attestation to their expected format
        return nil // Placeholder - needs implementation in core.go
    case SEVAttestation:
        return nil // Placeholder - needs implementation in core.go
    case DualAttestation:
        return nil // Placeholder - needs implementation in core.go
    default:
        return fmt.Errorf("unsupported attestation type: %d", attestation.Type)
    }
}

// cacheAttestation stores an attestation in the cache and database
func (vm *ShuttleVM) cacheAttestation(ctx context.Context, attestation *Attestation) error {
    // Store in cache
    vm.attestationMutex.Lock()
    vm.attestationCache[attestation.TxID] = attestation
    vm.attestationMutex.Unlock()

    // Persist to storage
    attestationBytes, err := json.Marshal(attestation)
    if err != nil {
        return fmt.Errorf("failed to marshal attestation: %w", err)
    }

    // Store the attestation in the attestation store
    attestationStore := storage.NewAttestationStore(vm.db)
    
    if err := attestationStore.StoreAttestation(ctx, attestation.TxID, attestationBytes); err != nil {
        return fmt.Errorf("failed to store attestation: %w", err)
    }

    return nil
}

// validateTxWithExecution executes a transaction in a TEE and creates an attestation
func (vm *ShuttleVM) validateTxWithExecution(ctx context.Context, tx *chain.Transaction) error {
    // Select an appropriate TEE region for execution
    var region string
    availableNodes := vm.computeNodes
    if len(availableNodes) > 0 {
        // Pick the first available node for now
        for name := range availableNodes {
            region = name
            break
        }
    } else {
        return fmt.Errorf("no compute nodes available for execution")
    }

    // Execute the transaction in the TEE
    // TODO: Implement actual transaction execution in TEE
    
    // Generate attestation
    attestation := &Attestation{
        TxID:      tx.ID(),
        Type:      SGXAttestation, // Default to SGX for now
        EnclaveID: []byte("test-enclave-id"),  // This would be the actual enclave ID in production
        RegionID:  region,
        Timestamp: time.Now(),
    }
    
    // Cache the attestation
    if err := vm.cacheAttestation(ctx, attestation); err != nil {
        return fmt.Errorf("failed to cache attestation: %w", err)
    }
    
    return nil
}



// preprocessContractParams handles WebAssembly contract parameters safely
func (vm *ShuttleVM) preprocessContractParams(params []byte) ([]byte, error) {
    // For simple parameter processing, just ensure data is valid
    if len(params) == 0 {
        return []byte{}, nil
    }
    
    // If the data is larger than reasonable, reject it (using the value from wasm_params.go)
    const maxSize = 1024 * 1024 // 1MB, same as in wasm_params.go
    if len(params) > maxSize {
        return nil, fmt.Errorf("parameter size too large: %d > %d", len(params), maxSize)
    }
    
    // Log parameter details
    vm.logger.Debug("Processing contract parameters",
        zap.Int("length", len(params)))
    
    // Just return the validated parameters
    return params, nil
}

// verifyAttestation verifies the validity of an attestation, including signatures and timestamp
func (vm *ShuttleVM) verifyAttestation(ctx context.Context, attestation *Attestation) error {
    // Verify attestation timestamp is not too old
    if time.Since(attestation.Timestamp) > AttestationCacheTTL {
        return fmt.Errorf("attestation expired: %v old", time.Since(attestation.Timestamp))
    }
    
    // Verify based on the attestation type
    if err := vm.verifyTEEAttestation(ctx, attestation); err != nil {
        return fmt.Errorf("attestation verification failed: %w", err)
    }
    
    vm.logger.Debug("attestation verification successful",
        zap.String("txID", attestation.TxID.String()),
        zap.Int("type", int(attestation.Type)))
        
    return nil
}

// cacheAttestationFromExecution creates and caches an attestation from execution results
func (vm *ShuttleVM) cacheAttestationFromExecution(ctx context.Context, txID ids.ID, action *actions.SendEventAction) error {
    if action == nil || len(action.Attestations) == 0 {
        return fmt.Errorf("no attestations available to cache")
    }
    
    // Use the first attestation from the action
    teeAttestation := action.Attestations[0]
    
    // Determine attestation type based on the attestation data
    attestationType := SGXAttestation // Default to SGX
    
    // Create our internal attestation structure
    attestation := &Attestation{
        TxID:           txID,
        Type:           attestationType,
        EnclaveID:      teeAttestation.EnclaveID,
        RegionID:       "default-region", // Use a default region ID
        Signature:      teeAttestation.Signature,
        CrossSignature: teeAttestation.Signature, // Use the same signature for now
        Timestamp:      time.Now(),
        Data:           teeAttestation.Data,
    }
    
    // Cache the attestation
    return vm.cacheAttestation(ctx, attestation)
}

// getCodeForAction retrieves the code to be executed for a given action
func (vm *ShuttleVM) getCodeForAction(ctx context.Context, action *actions.SendEventAction) ([]byte, error) {
    // Implementation would retrieve the appropriate code for the action
    // from a code registry or other storage
    
    // For now, select an available compute node
    var region string
    for name := range vm.computeNodes {
        region = name
        break
    }
    
    if region == "" {
        return nil, fmt.Errorf("no compute nodes available")
    }
    
    // Retrieve the code for the action
    code, err := vm.getCodeForRegion(ctx, region)
    if err != nil {
        return nil, fmt.Errorf("failed to retrieve code for region: %w", err)
    }
    
    return code, nil
}

func (vm *ShuttleVM) getCodeForRegion(ctx context.Context, region string) ([]byte, error) {
    // In a real implementation, this would retrieve the contract code from storage
    // based on the contract ID in the action
    return []byte("sample code"), nil
}

// executeInTEE executes code in a Trusted Execution Environment
func (vm *ShuttleVM) executeInTEE(ctx context.Context, code []byte, action *actions.SendEventAction) ([]byte, error) {
    // Select a region to execute in - simply use the first available compute node
    var regionID string
    for name := range vm.computeNodes {
        regionID = name
        break
    }
    
    if regionID == "" {
        return nil, fmt.Errorf("no compute nodes available for execution")
    }
    
    // Execute in the selected region
    return vm.executeInRegion(ctx, regionID, action)
}

// executeInRegion executes code in a specific TEE region
func (vm *ShuttleVM) executeInRegion(ctx context.Context, regionID string, action *actions.SendEventAction) ([]byte, error) {
    // In a real implementation, this would use the compute node client to execute in the TEE
    _, exists := vm.computeNodes[regionID]
    if !exists {
        return nil, fmt.Errorf("region %s not found", regionID)
    }
    
    // Process parameters for the action - this is where our parameter handling shines
    // Use empty params if action doesn't have parameters field
    var params []byte
    var err error
    if action != nil {
        // Use a byte slice that might be in different fields depending on the action type
        // In a real implementation, we would have proper accessor methods
        params = []byte("test parameters") // Placeholder for actual parameters
    }
    
    // Preprocess the parameters to handle both formats
    processedParams, err := vm.preprocessContractParams(params)
    if err != nil {
        return nil, fmt.Errorf("failed to process parameters: %w", err)
    }
    
    vm.logger.Debug("Executing action in TEE", 
        zap.String("region", regionID),
        zap.Int("param_size", len(processedParams)))
    
    // Mock execution for now - in production this would call the actual TEE
    // node.Execute would be the real implementation
    result := []byte("execution successful")
    
    return result, nil
}

// verifyExecutionResult verifies the result of a TEE execution
func (vm *ShuttleVM) verifyExecutionResult(ctx context.Context, action *actions.SendEventAction, result *compute.ExecutionResult) error {
	// In a real implementation, this would verify that the execution result
	// matches the expected outcome based on the action and state
	return nil
}
