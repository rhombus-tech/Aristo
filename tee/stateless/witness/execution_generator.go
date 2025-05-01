// Package witness provides components for generating stateless proofs
package witness

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"time"
	
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	
	"github.com/rhombus-tech/vm/tee/stateless/core"
	"github.com/rhombus-tech/vm/tee/stateless/proofs"
)

var (
	// ErrTransactionNotFound indicates a transaction was not found
	ErrTransactionNotFound = errors.New("transaction not found")
	
	// ErrStateRootMismatch indicates a state root mismatch
	ErrStateRootMismatch = errors.New("state root mismatch")
	
	// ErrInvalidTransaction indicates an invalid transaction
	ErrInvalidTransaction = errors.New("invalid transaction")
)

// ExecutionClient represents the interface to the TEE execution environment
type ExecutionClient interface {
	// ExecuteTransaction executes a transaction in the TEE
	ExecuteTransaction(
		ctx context.Context,
		txID ids.ID,
		inputs [][]byte,
	) ([][]byte, [sha256.Size]byte, error)
	
	// GetStateRoot gets the current state root
	GetStateRoot(ctx context.Context) ([sha256.Size]byte, error)
}

// MockExecutionClient is a mock implementation of ExecutionClient
type MockExecutionClient struct {
	// In a real implementation, this would connect to your TEE execution service
	stateRoot     [sha256.Size]byte
	teeType       string
	regionID      string
	enclaveID     []byte
	log           logging.Logger
	lock          sync.RWMutex
	transactions  map[ids.ID]*TransactionRecord
	attestationSvc AttestationService
}

// TransactionRecord represents a record of an executed transaction
type TransactionRecord struct {
	TxID            ids.ID
	Inputs          [][]byte
	Outputs         [][]byte
	PrevStateRoot   [sha256.Size]byte
	NewStateRoot    [sha256.Size]byte
	Timestamp       uint64
	ExecutionProof  *proofs.ExecutionProof
}

// NewMockExecutionClient creates a new mock execution client
func NewMockExecutionClient(
	teeType string,
	regionID string,
	log logging.Logger,
	attestationSvc AttestationService,
) *MockExecutionClient {
	// Initialize with a deterministic state root
	initialStateRoot := [sha256.Size]byte{}
	copy(initialStateRoot[:], []byte("initial state root"))
	
	// Mock enclave ID
	enclaveID := make([]byte, 16)
	copy(enclaveID, []byte("MockEnclaveID"))
	
	return &MockExecutionClient{
		stateRoot:     initialStateRoot,
		teeType:       teeType,
		regionID:      regionID,
		enclaveID:     enclaveID,
		log:           log,
		transactions:  make(map[ids.ID]*TransactionRecord),
		attestationSvc: attestationSvc,
	}
}

// ExecuteTransaction executes a transaction in the mock TEE
func (c *MockExecutionClient) ExecuteTransaction(
	ctx context.Context,
	txID ids.ID,
	inputs [][]byte,
) ([][]byte, [sha256.Size]byte, error) {
	// Check context for cancellation
	select {
	case <-ctx.Done():
		return nil, [sha256.Size]byte{}, ctx.Err()
	default:
		// Continue processing
	}
	
	c.lock.Lock()
	defer c.lock.Unlock()
	
	// Get current state root before execution
	prevStateRoot := c.stateRoot
	
	// Create mock outputs by hashing inputs
	outputs := make([][]byte, len(inputs))
	for i, input := range inputs {
		hasher := sha256.New()
		hasher.Write(input)
		hasher.Write(txID[:])
		outputs[i] = hasher.Sum(nil)
	}
	
	// Calculate new state root
	stateRootHasher := sha256.New()
	stateRootHasher.Write(prevStateRoot[:])
	stateRootHasher.Write(txID[:])
	for _, output := range outputs {
		stateRootHasher.Write(output)
	}
	
	newStateRoot := [sha256.Size]byte{}
	copy(newStateRoot[:], stateRootHasher.Sum(nil))
	
	// Update the state root
	c.stateRoot = newStateRoot
	
	// Log the transaction
	c.log.Debug(fmt.Sprintf("Executed transaction: txID=%s inputs=%d outputs=%d prevStateRoot=%x newStateRoot=%x",
		txID.String(), len(inputs), len(outputs), prevStateRoot, newStateRoot))
	
	// Generate attestation for this execution
	attestation, err := c.attestationSvc.GetAttestation(c.enclaveID)
	if err != nil {
		return nil, [sha256.Size]byte{}, fmt.Errorf("failed to generate attestation: %w", err)
	}
	
	// Convert to fixed-size array
	var measurementArr [32]byte
	if len(attestation.Measurement) > 32 {
		copy(measurementArr[:], attestation.Measurement[:32])
	} else {
		copy(measurementArr[:], attestation.Measurement)
	}
	
	// Create execution proof
	timestamp := uint64(time.Now().UnixNano())
	executionProof := proofs.NewExecutionProof(
		txID,
		inputs,
		outputs,
		measurementArr,
		c.teeType,
		c.enclaveID,
		c.regionID,
		timestamp,
		newStateRoot,
		prevStateRoot,
	)
	
	// Sign the proof
	err = executionProof.Sign(nil) // Mock signing
	if err != nil {
		return nil, [sha256.Size]byte{}, fmt.Errorf("failed to sign execution proof: %w", err)
	}
	
	// Store the transaction record
	c.transactions[txID] = &TransactionRecord{
		TxID:            txID,
		Inputs:          inputs,
		Outputs:         outputs,
		PrevStateRoot:   prevStateRoot,
		NewStateRoot:    newStateRoot,
		Timestamp:       timestamp,
		ExecutionProof:  executionProof,
	}
	
	return outputs, newStateRoot, nil
}

// GetStateRoot gets the current state root from the mock TEE
func (c *MockExecutionClient) GetStateRoot(ctx context.Context) ([sha256.Size]byte, error) {
	// Check context for cancellation
	select {
	case <-ctx.Done():
		return [sha256.Size]byte{}, ctx.Err()
	default:
		// Continue processing
	}
	
	c.lock.RLock()
	defer c.lock.RUnlock()
	
	return c.stateRoot, nil
}

// GetTransactionProof gets the execution proof for a transaction
func (c *MockExecutionClient) GetTransactionProof(txID ids.ID) (*proofs.ExecutionProof, error) {
	c.lock.RLock()
	defer c.lock.RUnlock()
	
	tx, ok := c.transactions[txID]
	if !ok {
		return nil, ErrTransactionNotFound
	}
	
	return tx.ExecutionProof, nil
}

// ExecutionGenerator generates execution proofs for transactions
type ExecutionGenerator struct {
	client        ExecutionClient
	attestationSvc AttestationService
	teeType       string
	regionID      string
	enclaveID     []byte // For accumulator-based attestation
	log           logging.Logger
}

// NewExecutionGenerator creates a new execution generator
func NewExecutionGenerator(
	client ExecutionClient,
	attestationSvc AttestationService,
	teeType string,
	regionID string,
	log logging.Logger,
) *ExecutionGenerator {
	// Generate a default enclave ID
	enclaveID := make([]byte, 16)
	copy(enclaveID, []byte("ExecutionGenerator"))
	return &ExecutionGenerator{
		client:        client,
		attestationSvc: attestationSvc,
		teeType:       teeType,
		regionID:      regionID,
		enclaveID:     enclaveID,
		log:           log,
	}
}

// ExecuteTransaction executes a transaction and generates an execution proof
func (g *ExecutionGenerator) ExecuteTransaction(
	ctx context.Context,
	txID ids.ID,
	inputs [][]byte,
) (core.StatelessProof, error) {
	// Execute the transaction
	outputs, newStateRoot, err := g.client.ExecuteTransaction(ctx, txID, inputs)
	if err != nil {
		return nil, fmt.Errorf("failed to execute transaction: %w", err)
	}
	
	// Get the previous state root
	prevStateRoot, err := g.client.GetStateRoot(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get state root: %w", err)
	}
	
	// Generate attestation for the enclave
	attestation, err := g.attestationSvc.GetAttestation(g.enclaveID)
	if err != nil {
		return nil, fmt.Errorf("failed to generate attestation: %w", err)
	}
	
	// Convert to fixed-size array
	var measurementArr [32]byte
	if len(attestation.Measurement) > 32 {
		copy(measurementArr[:], attestation.Measurement[:32])
	} else {
		copy(measurementArr[:], attestation.Measurement)
	}
	
	// Create execution proof
	executionProof := proofs.NewExecutionProof(
		txID,
		inputs,
		outputs,
		measurementArr,
		g.teeType,
		g.enclaveID, // Use our enclave ID since the attestation doesn't have one
		g.regionID,
		uint64(time.Now().UnixNano()),
		newStateRoot,
		prevStateRoot,
	)
	
	// Sign the proof
	err = executionProof.Sign(nil) // In a real implementation, this would use a proper signer
	if err != nil {
		return nil, fmt.Errorf("failed to sign execution proof: %w", err)
	}
	
	g.log.Debug(fmt.Sprintf("Generated execution proof: txID=%s teeType=%s regionID=%s",
		txID.String(), g.teeType, g.regionID))
	
	return executionProof, nil
}

// VerifyExecution verifies the execution of a transaction
func (g *ExecutionGenerator) VerifyExecution(
	ctx context.Context,
	proof core.StatelessProof,
) (bool, error) {
	// Check if this is an execution proof
	executionProof, ok := proof.(*proofs.ExecutionProof)
	if !ok {
		return false, fmt.Errorf("expected execution proof, got %T", proof)
	}
	
	// Get the current state root
	stateRoot, err := g.client.GetStateRoot(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to get state root: %w", err)
	}
	
	// Verify the state root matches
	if executionProof.StateRoot != stateRoot {
		return false, ErrStateRootMismatch
	}
	
	// Verify the proof
	return executionProof.Verify(ctx)
}
