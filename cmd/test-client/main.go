package main

import (
    "context"
    "encoding/json"
    "flag"
    "fmt"
    "log"
    "net/http"
    "time"

    "github.com/rhombus-tech/vm/timeserver"
)

func main() {
    var (
        server1Addr = flag.String("server1", "http://localhost:8080", "Address of first time server")
        server2Addr = flag.String("server2", "http://localhost:8081", "Address of second time server")
        regionID    = flag.String("region", "local", "Region ID for time servers")
    )
    flag.Parse()

    // Test server health
    checkServerHealth(*server1Addr)
    checkServerHealth(*server2Addr)

    // Create time network
    network := timeserver.NewTimeNetwork(2, 100*time.Millisecond)

    // Create and add servers
    server1, err := timeserver.NewTimeServer("server1", *regionID, *server1Addr)
    if err != nil {
        log.Fatalf("Failed to create server1: %v", err)
    }
    if err := network.AddServer(server1); err != nil {
        log.Fatalf("Failed to add server1: %v", err)
    }

    server2, err := timeserver.NewTimeServer("server2", *regionID, *server2Addr)
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

    // Get verified timestamp
    ctx := context.Background()
    timestamp, err := network.GetVerifiedTimestamp(ctx, *regionID)
    if err != nil {
        log.Fatalf("Failed to get verified timestamp: %v", err)
    }

    fmt.Printf("\nVerified Timestamp:\n")
    fmt.Printf("Time: %v\n", timestamp.Time)
    fmt.Printf("Region: %s\n", timestamp.RegionID)
    fmt.Printf("Quorum Size: %d\n", timestamp.QuorumSize)
    fmt.Printf("\nProofs from servers:\n")
    
    for _, proof := range timestamp.Proofs {
        fmt.Printf("Server %s:\n", proof.ServerID)
        fmt.Printf("  Signature: %x\n", proof.Signature)
        fmt.Printf("  Delay: %v\n", proof.Delay)
    }
}

func checkServerHealth(addr string) {
    resp, err := http.Get(addr + "/health")
    if err != nil {
        log.Printf("Failed to check health of %s: %v", addr, err)
        return
    }
    defer resp.Body.Close()

    var status timeserver.ServerStatus
    if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
        log.Printf("Failed to decode health response from %s: %v", addr, err)
        return
    }

    fmt.Printf("\nServer Health Check for %s:\n", addr)
    fmt.Printf("ID: %s\n", status.ID)
    fmt.Printf("Region: %s\n", status.RegionID)
    fmt.Printf("Status: %s\n", status.Status)
    fmt.Printf("Uptime: %s\n", status.Uptime)
    fmt.Printf("Current Time: %v\n", status.Timestamp)
}
