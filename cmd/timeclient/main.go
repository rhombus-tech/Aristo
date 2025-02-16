package main

import (
	"context"
	"flag"
	"log"
	"time"

	"github.com/rhombus-tech/vm/timeserver"
)

func main() {
	var (
		regionID = flag.String("region", "", "Region ID to get timestamp from")
	)
	flag.Parse()

	if *regionID == "" {
		log.Fatal("region ID is required")
	}

	// Create time network with default settings
	network := timeserver.NewTimeNetwork(2, 100*time.Millisecond)

	// Add servers
	server1, err := timeserver.NewTimeServer("server1", *regionID, "http://localhost:8080")
	if err != nil {
		log.Fatalf("Failed to create server1: %v", err)
	}
	if err := network.AddServer(server1); err != nil {
		log.Fatalf("Failed to add server1: %v", err)
	}

	server2, err := timeserver.NewTimeServer("server2", *regionID, "http://localhost:8081")
	if err != nil {
		log.Fatalf("Failed to create server2: %v", err)
	}
	if err := network.AddServer(server2); err != nil {
		log.Fatalf("Failed to add server2: %v", err)
	}

	// Start the network
	if err := network.Start(); err != nil {
		log.Fatalf("Failed to start network: %v", err)
	}
	defer network.Stop()

	// Get timestamp
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	timestamp, err := network.GetVerifiedTimestamp(ctx, *regionID)
	if err != nil {
		log.Fatalf("Failed to get timestamp: %v", err)
	}

	log.Printf("Verified timestamp: %v", timestamp.Time)
	log.Printf("Region: %s", timestamp.RegionID)
	log.Printf("Quorum size: %d", timestamp.QuorumSize)
	for _, proof := range timestamp.Proofs {
		log.Printf("Server %s proof delay: %v", proof.ServerID, proof.Delay)
	}
}
