package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rhombus-tech/vm/coordination"
)

func main() {
	// Parse command line flags
	port := flag.Int("port", 8080, "Port to run the coordinator service on")
	baseDir := flag.String("base-dir", "/tmp/coordinator", "Base directory for storing coordinator data")
	simulateMode := flag.Bool("simulate", false, "Run in simulation mode")
	verboseMode := flag.Bool("verbose", false, "Enable verbose logging")
	flag.Parse()

	// Set up logging
	if *verboseMode {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	} else {
		log.SetFlags(log.LstdFlags)
	}

	log.Printf("Starting Coordinator Server on port %d", *port)
	log.Printf("Base directory: %s", *baseDir)

	// Create base directory if it doesn't exist
	if err := os.MkdirAll(*baseDir, 0755); err != nil {
		log.Fatalf("Failed to create base directory: %v", err)
	}

	// Initialize in-memory storage for testing
	store := coordination.NewInMemoryStorage()

	// Create coordinator configuration
	config := &coordination.Config{
		MinWorkers:         2,
		MaxWorkers:         100,
		WorkerTimeout:      time.Second * 30,
		MaxTasks:           1000,
		TaskQueueSize:      1000,
		TaskTimeout:        time.Second * 30,
		TaskCleanupInterval: time.Hour,
		ChannelTimeout:     time.Second * 10,
		MaxMessageSize:     1024 * 1024, // 1MB
		EncryptionEnabled:  !*simulateMode,
		RequireAttestation: !*simulateMode,
		AttestationTimeout: time.Second * 30,
		StoragePath:        *baseDir,
		PersistenceEnabled: false, // Use in-memory storage
		MaxObjects:         1000,
		MaxEvents:          1000,
	}

	// Create the coordinator
	coordinator, err := coordination.NewCoordinator(config, nil, store)
	if err != nil {
		log.Fatalf("Failed to create coordinator: %v", err)
	}

	// Start the coordinator
	if err := coordinator.Start(); err != nil {
		log.Fatalf("Failed to start coordinator: %v", err)
	}
	defer coordinator.Stop()

	// Create and start the HTTP server
	addr := fmt.Sprintf(":%d", *port)
	server := coordination.NewCoordinatorServer(coordinator, addr)

	// Handle graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the server in a goroutine
	go func() {
		if err := server.Start(); err != nil {
			log.Fatalf("Failed to start HTTP server: %v", err)
		}
	}()

	// Set up signal handling
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Wait for shutdown signal
	<-stop
	log.Println("Shutting down coordinator server...")

	// Create a deadline to wait for
	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 5*time.Second)
	defer shutdownCancel()

	// Attempt to gracefully shut down the server
	if err := server.Stop(shutdownCtx); err != nil {
		log.Printf("Error during server shutdown: %v", err)
	}

	log.Println("Coordinator server stopped")
}
