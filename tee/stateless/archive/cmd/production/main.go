// Copyright (C) 2025, Rhombus Technologies. All rights reserved.
// See the file LICENSE for licensing terms.

// Production deployment entry point for TEE-backed polynomial ZK archival
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/rhombus-tech/vm/tee/stateless/archive"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	
	// Check for config file path argument
	if len(os.Args) < 2 {
		log.Fatalf("Usage: %s <config_file_path>", os.Args[0])
	}
	
	configPath := os.Args[1]
	
	// Verify config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		log.Fatalf("Config file not found: %s", configPath)
	}
	
	log.Printf("Starting TEE Polynomial ZK Archival with config from: %s", configPath)
	
	// Set up signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	
	// Start the deployment in a goroutine
	errCh := make(chan error, 1)
	go func() {
		if err := archive.RunTEEPolynomialDeployment(configPath); err != nil {
			errCh <- fmt.Errorf("deployment failed: %w", err)
		}
	}()
	
	// Wait for either an error or a signal
	select {
	case err := <-errCh:
		log.Fatalf("Error: %v", err)
	case sig := <-sigCh:
		log.Printf("Received signal: %v, shutting down gracefully", sig)
	}
	
	log.Println("TEE Polynomial ZK Archival service stopped")
}
