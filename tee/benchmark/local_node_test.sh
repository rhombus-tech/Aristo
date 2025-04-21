#!/bin/bash
# Run performance tests directly on a TEE node
# This script should be copied to an SGX node and executed there

# Parameters for the test
BATCH_SIZE=1000
THREAD_COUNT=8
TEST_DURATION=300  # seconds
PAIR_IP="$1"  # Pass the paired SEV node IP as an argument

if [ -z "$PAIR_IP" ]; then
  echo "Usage: $0 <paired-sev-node-ip>"
  exit 1
fi

echo "=== TEE Node Local Performance Test ==="
echo "Configuration:"
echo "  - Batch Size: $BATCH_SIZE"
echo "  - Thread Count: $THREAD_COUNT" 
echo "  - Test Duration: $TEST_DURATION seconds"
echo "  - Paired Node IP: $PAIR_IP"
echo "======================================"

# Create test directory
mkdir -p ~/tee-benchmark
cd ~/tee-benchmark

# Write test data generator
cat > generate_test_data.go << 'EOF'
package main

import (
    "fmt"
    "os"
    "strconv"
    "time"
    "crypto/sha256"
    "encoding/hex"
    "sync"
    "runtime"
)

func generateTestData(size int) []string {
    result := make([]string, size)
    for i := 0; i < size; i++ {
        data := fmt.Sprintf("tx_%d_%d", i, time.Now().UnixNano())
        result[i] = data
    }
    return result
}

func processData(data []string, threadCount int) {
    batchSize := len(data)
    runtime.GOMAXPROCS(threadCount)
    
    var wg sync.WaitGroup
    results := make([]string, batchSize)
    
    // Create worker pool
    jobs := make(chan int, batchSize)
    
    // Launch workers
    for w := 0; w < threadCount; w++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for i := range jobs {
                // Simulate accumulator work with SHA-256 hashing
                h := sha256.New()
                h.Write([]byte(data[i]))
                results[i] = hex.EncodeToString(h.Sum(nil))
            }
        }()
    }
    
    // Send jobs
    for i := 0; i < batchSize; i++ {
        jobs <- i
    }
    close(jobs)
    
    // Wait for completion
    wg.Wait()
}

func main() {
    args := os.Args[1:]
    if len(args) < 3 {
        fmt.Println("Usage: go run generate_test_data.go <batch-size> <thread-count> <duration-seconds>")
        os.Exit(1)
    }
    
    batchSize, _ := strconv.Atoi(args[0])
    threadCount, _ := strconv.Atoi(args[1])
    durationSec, _ := strconv.Atoi(args[2])
    
    fmt.Printf("Starting performance test with batch size %d, thread count %d for %d seconds\n", 
        batchSize, threadCount, durationSec)
    
    startTime := time.Now()
    endTime := startTime.Add(time.Duration(durationSec) * time.Second)
    
    txCount := 0
    batchCount := 0
    
    for time.Now().Before(endTime) {
        data := generateTestData(batchSize)
        batchStart := time.Now()
        
        // Process the batch 
        processData(data, threadCount)
        
        batchEnd := time.Now()
        batchDuration := batchEnd.Sub(batchStart)
        
        txCount += batchSize
        batchCount++
        
        if batchCount % 10 == 0 {
            elapsed := time.Since(startTime)
            tps := float64(txCount) / elapsed.Seconds()
            fmt.Printf("Processed %d transactions, %d batches, Current TPS: %.2f\n", 
                txCount, batchCount, tps)
        }
    }
    
    totalDuration := time.Since(startTime)
    tps := float64(txCount) / totalDuration.Seconds()
    
    fmt.Println("\n=== Performance Test Results ===")
    fmt.Printf("Total Transactions: %d\n", txCount)
    fmt.Printf("Total Batches: %d\n", batchCount)
    fmt.Printf("Test Duration: %.2f seconds\n", totalDuration.Seconds())
    fmt.Printf("Transactions Per Second: %.2f\n", tps)
    fmt.Printf("Batch Processing Time (avg): %.2f ms\n", 
        (totalDuration.Seconds() * 1000) / float64(batchCount))
    fmt.Println("================================")
}
EOF

# Make sure Go is installed
if ! command -v go &> /dev/null; then
    echo "Installing Go..."
    sudo apt-get update
    sudo apt-get install -y golang
fi

# Run the test
go run generate_test_data.go $BATCH_SIZE $THREAD_COUNT $TEST_DURATION

# Display node information to verify TEE capabilities
echo -e "\n=== Node Information ==="
echo "CPU Info:"
lscpu | grep -E "Model name|Socket|Core|Thread"

echo -e "\nTEE Environment:"
if [ -d "/dev/sgx" ] || [ -d "/sys/devices/virtual/sgx" ]; then
    echo "Intel SGX: Available"
    ls -la /dev/sgx* 2>/dev/null || echo "No SGX device files found"
elif [ -e "/dev/sev" ] || [ -e "/dev/sev-guest" ]; then
    echo "AMD SEV: Available"
    ls -la /dev/sev* 2>/dev/null || echo "No SEV device files found"
else 
    echo "No TEE environment detected"
fi

# Check if our optimized TEE service is running
echo -e "\nTEE Service Status:"
sudo systemctl status optimized-tee || echo "Optimized TEE service not found"

# Check for any accumulator processes
echo -e "\nAccumulator Processes:"
ps aux | grep -i accumulator

echo -e "\nTest completed!"
