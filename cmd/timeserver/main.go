package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/rhombus-tech/vm/timeserver"
)

func main() {
	var (
		id       = flag.String("id", "", "Server ID")
		regionID = flag.String("region", "", "Region ID")
		address  = flag.String("address", ":8080", "Server address")
	)
	flag.Parse()

	if *id == "" || *regionID == "" {
		log.Fatal("Server ID and Region ID are required")
	}

	// Create new time server
	server, err := timeserver.NewTimeServer(*id, *regionID, *address)
	if err != nil {
		log.Fatalf("Failed to create time server: %v", err)
	}

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	errChan := make(chan error, 1)
	go func() {
		errChan <- server.Serve(*address)
	}()

	// Wait for shutdown signal or error
	select {
	case err := <-errChan:
		log.Fatalf("Server error: %v", err)
	case sig := <-sigChan:
		log.Printf("Received signal %v, shutting down", sig)
	}
}
