package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/rhombus-tech/vm/tee/observability/metrics"
)

func main() {
	// Parse command-line flags
	port := flag.Int("port", 9090, "Port to expose metrics on")
	regionID := flag.String("region", "us-east", "Region ID for this monitoring instance")
	meshAPI := flag.String("mesh-api", "http://localhost:8080", "URL for the mesh API")
	execAPI := flag.String("exec-api", "http://localhost:8081", "URL for the execution API")
	flag.Parse()

	fmt.Printf("Starting TEE monitoring system for region: %s\n", *regionID)
	fmt.Printf("Metrics will be exposed on port: %d\n", *port)
	fmt.Printf("Connecting to mesh API at: %s\n", *meshAPI)
	fmt.Printf("Connecting to execution API at: %s\n", *execAPI)

	// Create the monitoring service configuration
	config := &metrics.MonitorConfig{
		Port:            *port,
		RegionID:        *regionID,
		MeshAPIURL:      *meshAPI,
		ExecutionAPIURL: *execAPI,
	}

	// Create and start the monitoring service
	monitor, err := metrics.NewMonitoringService(config)
	if err != nil {
		log.Fatalf("Failed to create monitoring service: %v", err)
	}

	if err := monitor.Start(); err != nil {
		log.Fatalf("Failed to start monitoring service: %v", err)
	}

	fmt.Println("TEE Monitoring system is running")
	fmt.Println("----------------------------------")
	fmt.Printf("View metrics at: http://localhost:%d/metrics\n", *port)
	fmt.Printf("Check status at: http://localhost:%d/status\n", *port)
	fmt.Println("Import the dashboard from: tee/observability/dashboards/tee_performance_dashboard.json")
	fmt.Println("----------------------------------")
	fmt.Println("Press Ctrl+C to exit")

	// Set up a channel to handle shutdown signals
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	// Wait for termination signal
	<-sigs
	fmt.Println("\nShutting down monitoring service...")

	// Gracefully shut down the monitoring service
	if err := monitor.Stop(); err != nil {
		log.Printf("Error shutting down monitoring service: %v", err)
	}

	fmt.Println("Monitoring service stopped")
}
