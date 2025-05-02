// Copyright (C) 2025, Rhombus Technologies. All rights reserved.
// See the file LICENSE for licensing terms.

package archive

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"log"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/rhombus-tech/vm/tee/stateless/core"
)

// TEEPolynomialArchivalExample demonstrates how to use the TEE-backed polynomial 
// commitment implementation for production ZK archiving
func TEEPolynomialArchivalExample() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	
	// Initialize context with cancellation for proper shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Step 1: Create a mock environment for demonstration
	log.Println("Setting up a mock stateless blockchain environment...")
	chain := newMockStatelessChain()
	
	// Create 1000 blocks with TEE attestation for the demo
	createTestBlocks(ctx, chain, 1000)
	
	// Step 2: Create a stateless verifier
	log.Println("Creating stateless verifier...")
	verifier := &mockStatelessVerifier{chain: chain}
	
	// Step 3: Set up the ZK archival configuration
	log.Println("Setting up ZK archival system with TEE polynomial commitments...")
	zkConfig := DefaultZKArchiveConfig()
	zkConfig.BatchSize = 50           // Archive 50 blocks per batch
	zkConfig.ArchivalPeriod = 5 * time.Second  // Process every 5 seconds
	zkConfig.ReferencePoints = 20     // Keep 20 reference points
	zkConfig.RecursiveProofLevels = 3 // Use 3 levels of recursive proofs
	zkConfig.TEEVerifiedOnly = true   // Only archive blocks with TEE verification
	
	// Step 4: Create the TEE polynomial circuit
	// In production, this would point to your actual TEE controller
	teeEndpoint := "https://tee-controller.example.com/execute"
	
	// For local testing, you might use:
	// teeEndpoint := "http://localhost:8080/execute"
	
	// Log the interfaces we're implementing with our circuit
	log.Printf("Implementing ZKCircuit interface compliant with %T", (core.StatelessBlock)(nil))
	
	circuit := NewTEEPolynomialCircuit(
		teeEndpoint,
		WithMaxBatchSize(50),
		WithAcceleration(true),
	)
	
	// Step 5: Create the ZK integration with the TEE polynomial circuit
	verifyFunc := func(ctx context.Context, fromHeight, toHeight uint64, params []byte) (bool, error) {
		// Apply dual-format parameter handling for security
		parsedParams, format, err := ParseDualFormatParameter(params, true, true)
		if err != nil {
			log.Printf("Parameter parsing error: %v", err)
			return false, err
		}
		
		log.Printf("Using %s parameter format for verification", format)
		
		// Use the parsed parameters for verification
		return verifier.VerifyStateTransition(ctx, fromHeight, toHeight, parsedParams)
	}
	
	getStateFunc := func(ctx context.Context, stateRoot [32]byte, key []byte) ([]byte, error) {
		// Apply dual-format parameter handling for keys
		parsedKey, format, err := ParseDualFormatParameter(key, true, true)
		if err != nil {
			log.Printf("Key parsing error: %v", err)
			return nil, err
		}
		
		log.Printf("Using %s key format for state query", format)
		
		// Convert stateRoot byte array to ids.ID
		var idRoot ids.ID
		copy(idRoot[:], stateRoot[:])
		
		return verifier.GetStateValue(ctx, idRoot, parsedKey)
	}
	
	// Create the ZK integration
	integration, err := NewZKArchiveIntegration(chain, verifier, zkConfig, circuit, verifyFunc, getStateFunc)
	if err != nil {
		log.Fatalf("Failed to create ZK integration: %v", err)
	}
	
	// Start the ZK archival process
	err = integration.Start(ctx)
	if err != nil {
		log.Fatalf("Failed to start ZK archival: %v", err)
	}
	
	// Step 6: Demonstrate different parameter formats with rigorous security
	log.Println("Demonstrating TEE-backed ZK archival with dual-format parameter handling...")
	
	// Create a test parameter in length-prefixed format (WebAssembly standard)
	testParamValue := []byte("tee_polynomial_parameter")
	lengthPrefixedParam := make([]byte, 4+len(testParamValue))
	binary.LittleEndian.PutUint32(lengthPrefixedParam[:4], uint32(len(testParamValue)))
	copy(lengthPrefixedParam[4:], testParamValue)
	
	// Create a test parameter in direct format
	directParam := []byte("direct_format_param")
	
	// Demonstrate protection against the 3.5B byte vulnerability
	log.Println("Demonstrating protection against memory exploitation attacks...")
	
	// Create a malicious parameter that would normally crash
	// This simulates the 3.5 billion byte attack seen in WebAssembly contracts
	maliciousParam := make([]byte, 8)
	binary.LittleEndian.PutUint32(maliciousParam[:4], 3500000000) // 3.5 billion bytes
	
	// Test with each parameter format
	log.Println("Testing with length-prefixed parameter format...")
	testTEEPolynomialVerification(ctx, integration, 100, 200, lengthPrefixedParam)
	
	log.Println("Testing with direct parameter format...")
	testTEEPolynomialVerification(ctx, integration, 300, 400, directParam)
	
	// The malicious parameter should be properly handled without crashing
	log.Println("Testing with malicious parameter (3.5B attack)...")
	testTEEPolynomialVerification(ctx, integration, 500, 600, maliciousParam)
	
	// Step 7: Wait for some archival to complete and display stats
	log.Println("Waiting for archival to progress...")
	time.Sleep(10 * time.Second)
	
	// Get compression stats
	stats := integration.GetCompressionStats()
	
	log.Printf("TEE Polynomial ZK Archival Results:")
	log.Printf("- Total blocks archived: %d", stats.TotalBlocksArchived)
	log.Printf("- Storage saved: %.2f MB", float64(stats.TotalStorageSavedBytes)/(1024*1024))
	log.Printf("- Compression ratio: %.2fx", stats.CompressionRatio)
	log.Printf("- Verification latency: %d ms", stats.VerificationLatencyMs)
	log.Printf("- Recursive proofs generated: %d", stats.RecursiveProofsGenerated)
	
	// Step 8: Clean up
	integration.Stop()
	log.Println("TEE polynomial archival example completed")
}

// Test verification of state transitions with each parameter format
func testTEEPolynomialVerification(
	ctx context.Context,
	integration *ZKArchiveIntegration,
	fromHeight, toHeight uint64,
	params []byte,
) {
	// Display the parameter format and content
	if len(params) >= 4 {
		prefixLength := binary.LittleEndian.Uint32(params[:4])
		if prefixLength > 0 && prefixLength <= 1024*1024 && int(prefixLength+4) <= len(params) {
			log.Printf("Parameter is length-prefixed: length=%d, content=%s",
				prefixLength, hex.EncodeToString(params[4:4+prefixLength]))
		} else {
			log.Printf("Parameter using direct format: %s", hex.EncodeToString(params))
		}
	} else {
		log.Printf("Parameter using direct format: %s", hex.EncodeToString(params))
	}
	
	// Verify the state transition using the TEE-backed ZK proofs
	log.Printf("Verifying state transition from height %d to %d using TEE ZK proofs...", 
		fromHeight, toHeight)
	
	startTime := time.Now()
	verified, err := integration.VerifyHistoricalStateTransition(ctx, fromHeight, toHeight, params)
	duration := time.Since(startTime)
	
	if err != nil {
		log.Printf("Verification failed: %v", err)
		return
	}
	
	if verified {
		log.Printf("Successfully verified state transition in %s using TEE-backed ZK proofs", 
			duration)
	} else {
		log.Printf("State transition verification failed")
	}
}

// RunTEEPolynomialArchivalExample runs the TEE Polynomial ZK archival example
func RunTEEPolynomialArchivalExample() {
	TEEPolynomialArchivalExample()
}
