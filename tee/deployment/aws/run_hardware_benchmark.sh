#!/bin/bash

# Script to run benchmarks on actual AWS TEE nodes
set -e

# Configuration
SSH_KEY="~/.ssh/tee-access-key"
USERNAME="ubuntu"
REGION="us-east-1"

# These are the specific instance IDs we found earlier
SGX_INSTANCES="i-00e38fb76e0e77bb6 i-0b24b97ad7d7922aa"
SEV_INSTANCES="i-011c91b6513c9a499 i-0b2ebde88de87aaca"

# Create a temporary directory for our test
TEMP_DIR=$(mktemp -d)
echo "Using temporary directory: $TEMP_DIR"

# Create benchmarking package
echo "=== Building benchmark package ==="
mkdir -p "$TEMP_DIR/benchmark"
cp -r /Users/talzisckind/Downloads/aristo-fresh\ 2/tee/accumulator "$TEMP_DIR/benchmark/"
cp -r /Users/talzisckind/Downloads/aristo-fresh\ 2/tee/proto "$TEMP_DIR/benchmark/"

# Create Go module for benchmark
cat > "$TEMP_DIR/benchmark/go.mod" << EOF
module benchmark

go 1.18

require (
    github.com/rhombus-tech/vm/tee/accumulator v0.0.0
    github.com/rhombus-tech/vm/tee/proto v0.0.0
)

replace github.com/rhombus-tech/vm/tee/accumulator => ./accumulator
replace github.com/rhombus-tech/vm/tee/proto => ./proto
EOF

# Create a benchmark runner
cat > "$TEMP_DIR/benchmark/main.go" << EOF
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/rhombus-tech/vm/tee/accumulator"
	pb "github.com/rhombus-tech/vm/tee/proto"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: hardware_benchmark <node_count> <batch_size>")
		os.Exit(1)
	}

	// Parse command line arguments
	nodeCount, err := strconv.Atoi(os.Args[1])
	if err != nil {
		fmt.Printf("Invalid node count: %v\n", err)
		os.Exit(1)
	}
	
	batchSize, err := strconv.Atoi(os.Args[2])
	if err != nil {
		fmt.Printf("Invalid batch size: %v\n", err)
		os.Exit(1)
	}
	
	// Configuration
	totalOperations := 10000
	operationsPerNode := totalOperations / nodeCount
	
	fmt.Printf("Running hardware benchmark with %d nodes, batch size %d\n", nodeCount, batchSize)
	
	// Create clients for each node
	clients := make([]*accumulator.OptimizedRsaClient, nodeCount)
	for i := 0; i < nodeCount; i++ {
		teeType := "SGX"
		if i%2 == 1 {
			teeType = "SEV"
		}
		
		client, err := accumulator.NewOptimizedRsaClient(
			fmt.Sprintf("node-%d", i),
			teeType,
			accumulator.OptimizedRsaOptions{
				BatchSize:      batchSize,
				EnableAsync:    true,
				Parallelism:    8,
				RegionID:       "us-east-1",
				ModulusBits:    1024, // Smaller for testing
				VerifyInterval: 100 * time.Millisecond,
			},
		)
		if err != nil {
			fmt.Printf("Failed to create client %d: %v\n", i, err)
			os.Exit(1)
		}
		defer client.Close()
		clients[i] = client
	}
	
	// Start benchmark
	startTime := time.Now()
	
	// Use WaitGroup to wait for all operations
	var wg sync.WaitGroup
	wg.Add(nodeCount)
	
	// Start goroutine for each client
	for i := 0; i < nodeCount; i++ {
		go func(clientIndex int) {
			defer wg.Done()
			
			client := clients[clientIndex]
			teeType := "SGX"
			if clientIndex%2 == 1 {
				teeType = "SEV"
			}
			
			// Generate and process elements
			for j := 0; j < operationsPerNode; j++ {
				element := createRandomElement(teeType, clientIndex*operationsPerNode+j)
				client.AddToBatch(element)
				
				// Process batch if it's full
				if j%batchSize == batchSize-1 {
					_ = client.ProcessBatch(context.Background())
				}
			}
			
			// Process any remaining elements
			_ = client.ProcessBatch(context.Background())
		}(i)
	}
	
	// Wait for all operations to complete
	wg.Wait()
	
	// Calculate elapsed time and TPS
	elapsedTime := time.Since(startTime)
	tps := float64(totalOperations) / elapsedTime.Seconds()
	
	fmt.Printf("\n=== Benchmark Results ===\n")
	fmt.Printf("Configuration: %d nodes, batch size %d\n", nodeCount, batchSize)
	fmt.Printf("Total operations: %d\n", totalOperations)
	fmt.Printf("Elapsed time: %v\n", elapsedTime)
	fmt.Printf("Transactions per second (TPS): %.2f\n\n", tps)
	
	// Get stats from clients
	for i, client := range clients {
		stats := client.GetPerformanceStats()
		fmt.Printf("Client %d stats: %v\n", i, stats)
	}
}

// createRandomElement creates a random accumulator element for testing
func createRandomElement(teeType string, index int) *pb.AccumulatorElement {
	return &pb.AccumulatorElement{
		Executor:    fmt.Sprintf("executor-%d", index),
		Measurement: []byte(fmt.Sprintf("measurement-%d", index)),
		EnclaveType: teeType,
		Timestamp:   uint64(time.Now().UnixNano()),
	}
}
EOF

# Prepare benchmark for each instance type
prepare_benchmark_for_instance() {
    INSTANCE_ID=$1
    TEE_TYPE=$2
    
    IP=$(aws ec2 describe-instances --instance-ids "$INSTANCE_ID" --region "$REGION" --query "Reservations[0].Instances[0].PublicIpAddress" --output text)
    echo "Preparing benchmark for $TEE_TYPE instance $INSTANCE_ID ($IP)"
    
    # Create remote directory
    ssh -o StrictHostKeyChecking=no -i "$SSH_KEY" "$USERNAME@$IP" "mkdir -p ~/tee-benchmark"
    
    # Copy benchmark files
    scp -o StrictHostKeyChecking=no -i "$SSH_KEY" -r "$TEMP_DIR/benchmark/"* "$USERNAME@$IP:~/tee-benchmark/"
    
    # Set up Go environment if needed
    ssh -o StrictHostKeyChecking=no -i "$SSH_KEY" "$USERNAME@$IP" "command -v go >/dev/null 2>&1 || { sudo apt-get update && sudo apt-get install -y golang; }"
    
    # Initialize Go modules
    ssh -o StrictHostKeyChecking=no -i "$SSH_KEY" "$USERNAME@$IP" "cd ~/tee-benchmark && go mod tidy && go get -v ./..."
    
    echo "Benchmark prepared on $TEE_TYPE instance $INSTANCE_ID"
}

# Run benchmarks for different configurations
run_benchmark() {
    INSTANCE_ID=$1
    TEE_TYPE=$2
    NODE_COUNT=$3
    BATCH_SIZE=$4
    
    IP=$(aws ec2 describe-instances --instance-ids "$INSTANCE_ID" --region "$REGION" --query "Reservations[0].Instances[0].PublicIpAddress" --output text)
    echo "Running benchmark on $TEE_TYPE instance $INSTANCE_ID ($IP)"
    echo "Configuration: $NODE_COUNT nodes, batch size $BATCH_SIZE"
    
    # Run benchmark
    ssh -o StrictHostKeyChecking=no -i "$SSH_KEY" "$USERNAME@$IP" "cd ~/tee-benchmark && go build -o hardware_benchmark && ./hardware_benchmark $NODE_COUNT $BATCH_SIZE"
}

# Prepare benchmarks on one instance of each type
echo "=== Preparing benchmark environments ==="
SGX_INSTANCE=$(echo $SGX_INSTANCES | cut -d' ' -f1)
SEV_INSTANCE=$(echo $SEV_INSTANCES | cut -d' ' -f1)

prepare_benchmark_for_instance "$SGX_INSTANCE" "SGX"
prepare_benchmark_for_instance "$SEV_INSTANCE" "SEV"

# Run benchmarks with different configurations
echo "=== Running benchmarks ==="

# Test configurations
NODE_COUNTS=(1 2 4)
BATCH_SIZES=(100 500 1000)

for NODE_COUNT in "${NODE_COUNTS[@]}"; do
    for BATCH_SIZE in "${BATCH_SIZES[@]}"; do
        echo ""
        echo "=============================================="
        echo "SGX Benchmark: $NODE_COUNT nodes, batch size $BATCH_SIZE"
        echo "=============================================="
        run_benchmark "$SGX_INSTANCE" "SGX" "$NODE_COUNT" "$BATCH_SIZE"
        
        echo ""
        echo "=============================================="
        echo "SEV Benchmark: $NODE_COUNT nodes, batch size $BATCH_SIZE"
        echo "=============================================="
        run_benchmark "$SEV_INSTANCE" "SEV" "$NODE_COUNT" "$BATCH_SIZE"
    done
done

# Clean up
echo "=== Cleaning up ==="
rm -rf "$TEMP_DIR"
ssh -o StrictHostKeyChecking=no -i "$SSH_KEY" "$USERNAME@$SGX_INSTANCE" "rm -rf ~/tee-benchmark"
ssh -o StrictHostKeyChecking=no -i "$SSH_KEY" "$USERNAME@$SEV_INSTANCE" "rm -rf ~/tee-benchmark"

echo "=== Benchmark complete ==="
