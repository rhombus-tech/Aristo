// Copyright (C) 2025, Rhombus Technologies. All rights reserved.
// See the file LICENSE for licensing terms.

// Test runner for the TEE polynomial deployment
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rhombus-tech/vm/tee/stateless/archive"
)

// MockTEEController simulates a TEE controller for local testing
type MockTEEController struct {
	server *http.Server
}

// Start begins the mock TEE controller on the specified port
func (m *MockTEEController) Start(port int) error {
	mux := http.NewServeMux()
	
	// Handle execution requests
	mux.HandleFunc("/execute", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		
		// Parse the request body
		var payload struct {
			Input     []byte `json:"input"`
			Operation string `json:"operation"`
		}
		
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
			return
		}
		
		// SECURITY: Protect against 3.5B byte vulnerability and follow parameter safety patterns
		// This implements the parameter safety patterns from Wasmlanche contracts
		if len(payload.Input) == 0 || len(payload.Input) > 1024*1024 { // Max 1MB
			log.Printf("SECURITY: Rejecting unreasonable input length: %d bytes", len(payload.Input))
			http.Error(w, "Invalid input length", http.StatusBadRequest)
			return
		}
		
		// Generate appropriate response based on operation
		var response struct {
			Result []byte `json:"result"`
			Error  string `json:"error,omitempty"`
		}
		
		switch payload.Operation {
		case "secure_commit":
			log.Printf("Mock TEE: Processing secure_commit operation")
			
			// SECURITY: Check for dual-format parameters - handle both length-prefixed and direct formats
			// This follows the contract security pattern from the memory
			var dataFormat string
			if len(payload.Input) >= 4 {
				// First check if the first 4 bytes represent a reasonable length
				prefixLen := uint32(payload.Input[0]) | 
				             uint32(payload.Input[1]) << 8 | 
				             uint32(payload.Input[2]) << 16 | 
				             uint32(payload.Input[3]) << 24
				             
				if prefixLen > 0 && prefixLen <= 1024*1024 && int(prefixLen+4) <= len(payload.Input) {
					dataFormat = "length-prefixed"
					log.Printf("Detected length-prefixed format with length %d", prefixLen)
				} else {
					dataFormat = "direct"
					log.Printf("Detected direct data format (no valid length prefix)")
				}
			} else {
				dataFormat = "direct"
				log.Printf("Input too short for length prefix, using direct format")
			}
			
			log.Printf("Data format: %s", dataFormat)
			
			// Mock response for secure_commit:
			// [commitment_size(u32)][commitment][attestation_size(u32)][attestation]
			commitment := make([]byte, 128)
			for i := range commitment {
				commitment[i] = byte(i % 256)
			}
			
			attestation := make([]byte, 64)
			attestation[0] = 'T'
			attestation[1] = 'E'
			attestation[2] = 'A'
			attestation[3] = 'T'
			
			// Create response buffer with proper length prefixes
			respBuf := make([]byte, 4 + len(commitment) + 4 + len(attestation))
			
			// Write commitment size as little-endian u32
			respBuf[0] = byte(len(commitment))
			respBuf[1] = byte(len(commitment) >> 8)
			respBuf[2] = byte(len(commitment) >> 16)
			respBuf[3] = byte(len(commitment) >> 24)
			
			// Write commitment
			copy(respBuf[4:4+len(commitment)], commitment)
			
			// Write attestation size as little-endian u32
			attestOffset := 4 + len(commitment)
			respBuf[attestOffset] = byte(len(attestation))
			respBuf[attestOffset+1] = byte(len(attestation) >> 8)
			respBuf[attestOffset+2] = byte(len(attestation) >> 16)
			respBuf[attestOffset+3] = byte(len(attestation) >> 24)
			
			// Write attestation
			copy(respBuf[attestOffset+4:], attestation)
			
			response.Result = respBuf
			
		case "secure_open_at_point":
			log.Printf("Mock TEE: Processing secure_open_at_point operation")
			
			// SECURITY: Check for dual-format parameters - handle both formats
			// This follows the same security pattern as in the memory
			var dataFormat string
			if len(payload.Input) >= 4 {
				// Check if first 4 bytes represent a reasonable length
				prefixLen := uint32(payload.Input[0]) | 
				             uint32(payload.Input[1]) << 8 | 
				             uint32(payload.Input[2]) << 16 | 
				             uint32(payload.Input[3]) << 24
				             
				if prefixLen > 0 && prefixLen <= 1024*1024 && int(prefixLen+4) <= len(payload.Input) {
					dataFormat = "length-prefixed"
					log.Printf("Detected length-prefixed format with length %d", prefixLen)
				} else {
					dataFormat = "direct"
					log.Printf("Detected direct data format (no valid length prefix)")
				}
			} else {
				dataFormat = "direct"
				log.Printf("Input too short for length prefix, using direct format")
			}
			
			log.Printf("Data format: %s", dataFormat)
			
			// Mock response for secure_open_at_point:
			// A single byte: 1 for success, 0 for failure
			response.Result = []byte{1} // Always succeed in tests
			
		default:
			response.Error = fmt.Sprintf("Unknown operation: %s", payload.Operation)
		}
		
		// Write the response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})
	
	// Create server
	m.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}
	
	// Start server in a goroutine
	go func() {
		log.Printf("Starting mock TEE controller on port %d", port)
		if err := m.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Error starting mock TEE controller: %v", err)
		}
	}()
	
	return nil
}

// Stop shuts down the mock TEE controller
func (m *MockTEEController) Stop(ctx context.Context) error {
	log.Printf("Stopping mock TEE controller")
	return m.server.Shutdown(ctx)
}

// createLocalConfig creates a configuration file for local testing
func createLocalConfig() (string, error) {
	// Create local configuration file
	configDir := "./test_config"
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}
	
	configPath := configDir + "/config.json"
	
	// Create local config - ensure parameter formats are handled securely
	config := archive.TEEPolynomialDeploymentConfig{
		TEEEndpoint: "http://localhost:8080/execute",
		ZKArchiveConfig: archive.ZKArchiveConfig{
			BatchSize:           10,              // Small batch for quick testing
			ArchivalPeriod:      2 * time.Second, // Quick archival for testing
			ReferencePoints:     5,               // Fewer reference points for testing
			RecursiveProofLevels: 2,              // Fewer levels for testing
			Parallelism:         2,               // Fewer workers for testing
			TEEVerifiedOnly:     true,
		},
		BatchSize:          5,
		UseAcceleration:    false, // No acceleration for local testing
		MaxProofSize:       1024 * 64, 
		FieldElementSize:   32,    // pasta_curves::Fp is 32 bytes
		EnableMetrics:      true,
		MetricsEndpoint:    "",    // No remote metrics for local testing
		LogLevel:           "debug",
		PerformanceLogFreq: 1,     // Log every proof for testing
	}
	
	// Save config file
	configJSON, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal config: %w", err)
	}
	
	if err := os.WriteFile(configPath, configJSON, 0644); err != nil {
		return "", fmt.Errorf("failed to write config file: %w", err)
	}
	
	log.Printf("Created local test config at %s", configPath)
	return configPath, nil
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Starting TEE Polynomial Integration Test")
	log.Println("=======================================")
	
	// Create a context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Start mock TEE controller
	mockTEE := &MockTEEController{}
	err := mockTEE.Start(8080)
	if err != nil {
		log.Fatalf("Failed to start mock TEE controller: %v", err)
	}
	
	// Set up graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	
	go func() {
		sig := <-sigCh
		log.Printf("Received signal: %v", sig)
		cancel()
	}()

	// Create local configuration
	configPath, err := createLocalConfig()
	if err != nil {
		log.Fatalf("Failed to create local config: %v", err)
	}
	
	// Run the deployment - this executes our dual-format parameter handling
	log.Println("Starting TEE polynomial deployment with local config")
	log.Println("Press Ctrl+C to stop the test")
	
	// This uses the RunTEEPolynomialDeployment function we created earlier
	go func() {
		archive.RunTEEPolynomialDeployment(configPath)
	}()
	
	// Wait for context cancellation (CTRL+C)
	<-ctx.Done()
	
	// Clean up
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	
	if err := mockTEE.Stop(shutdownCtx); err != nil {
		log.Printf("Error stopping mock TEE controller: %v", err)
	}
	
	log.Println("=======================================")
	log.Println("TEE Polynomial Integration Test completed")
}
