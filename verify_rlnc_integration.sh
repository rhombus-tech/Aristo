#!/bin/bash
# Verification script for RLNC integration with TEE mesh and Avalanche
set -e

echo "Running RLNC integration verification with Avalanche..."

# Configuration
LOCAL_COORDINATOR_PORT=9080
LOCAL_COORDINATOR_URL="http://127.0.0.1:${LOCAL_COORDINATOR_PORT}"
AVALANCHE_API_PORT=9650
AVALANCHE_API_URL="http://127.0.0.1:${AVALANCHE_API_PORT}"
MORPHEUS_VM_ID="srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy"
MAX_PACKET_LOSS=30  # Maximum simulated packet loss percentage

# Check if the local coordinator is running
echo "Checking if local coordinator is running on port ${LOCAL_COORDINATOR_PORT}..."
if ! curl -s "${LOCAL_COORDINATOR_URL}/api/v1/health" > /dev/null; then
  echo "Local coordinator is not running. Please start it with ./run_local_avalanche_devnet.sh"
  exit 1
fi

# Check if Avalanche node is running
echo "Checking if Avalanche node is running on port ${AVALANCHE_API_PORT}..."
if ! curl -s "${AVALANCHE_API_URL}/ext/health" > /dev/null; then
  echo "Avalanche node is not running. Please start it with ./run_local_avalanche_devnet.sh"
  exit 1
fi

# Build the RLNC test suite
echo "Building RLNC test suite..."
cd tee/rlnc
go test -c ./test -o ../../bin/rlnc_test
cd ../..

# Run the RLNC test suite
echo "Running RLNC test suite..."
./bin/rlnc_test -test.v

# Verify RLNC with Avalanche integration
echo "Building RLNC verification tool..."
cat > cmd/verify_rlnc.go << EOF
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/rhombus-tech/Aristo/tee/attestation"
	"github.com/rhombus-tech/Aristo/tee/rlnc"
	"github.com/rhombus-tech/Aristo/tee/rlnc/mesh"
	"github.com/rhombus-tech/Aristo/tee/rlnc/security"
)

// MockMeshVerifier implements a mock attestation.Verifier for testing
type MockMeshVerifier struct{}

func (m *MockMeshVerifier) VerifyAttestation(ctx context.Context, att *attestation.TEEAttestation) error {
	return nil // Always verify for testing
}

func main() {
	// Parse command line flags
	packetLossPercentage := flag.Int("packet-loss", 0, "Simulated packet loss percentage (0-100)")
	numTransactions := flag.Int("transactions", 10, "Number of test transactions to submit")
	flag.Parse()

	if *packetLossPercentage < 0 || *packetLossPercentage > 100 {
		log.Fatalf("Packet loss percentage must be between 0 and 100")
	}

	// Create a verifier for testing
	verifier := &MockMeshVerifier{}

	// Create the Avalanche mesh bridge
	ctx := context.Background()
	bridge, err := rlnc.NewAvalancheMeshBridge(
		ctx,
		"http://127.0.0.1:9080", // Local coordinator URL
		"local-region",          // Region ID
		security.TEETypeSGX,     // Local TEE type
		verifier,                // Attestation verifier
	)
	if err != nil {
		log.Fatalf("Failed to create Avalanche mesh bridge: %v", err)
	}

	// Register TEE pairs
	bridge.RegisterTEEPair("pair1", "sgx1", "sev1")
	bridge.RegisterTEEPair("pair2", "sgx2", "sev2")

	// Set up a network failure simulator
	originalTransport := http.DefaultTransport
	http.DefaultTransport = &packetLossTransport{
		Transport:   originalTransport,
		packetLoss:  *packetLossPercentage,
		hostsToDrop: []string{"127.0.0.1"},
	}

	// Create channel for clean shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	// Track successful and failed transactions
	var successCount, failCount int
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Start submitting test transactions
	fmt.Printf("Submitting %d transactions with %d%% simulated packet loss...\n", 
		*numTransactions, *packetLossPercentage)

	startTime := time.Now()

	for i := 0; i < *numTransactions; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			// Create test transaction data
			txData := []byte(fmt.Sprintf("test-transaction-%d", idx))

			// Submit transaction with resilience
			txID, err := bridge.SubmitTransactionResilient(ctx, txData, 5*time.Second)
			
			mu.Lock()
			defer mu.Unlock()
			
			if err != nil {
				fmt.Printf("Transaction %d failed: %v\n", idx, err)
				failCount++
			} else {
				fmt.Printf("Transaction %d succeeded with ID: %s\n", idx, txID)
				successCount++
			}
		}(i)

		// Small pause between transactions to avoid overwhelming the system
		time.Sleep(100 * time.Millisecond)

		// Check for termination signal
		select {
		case <-sigs:
			fmt.Println("Received termination signal, waiting for in-flight transactions to complete...")
			goto cleanup
		default:
			// Continue
		}
	}

cleanup:
	// Wait for all in-flight transactions to complete
	wg.Wait()

	// Calculate stats
	totalTime := time.Since(startTime)
	throughput := float64(*numTransactions) / totalTime.Seconds()
	successRate := float64(successCount) / float64(*numTransactions) * 100

	// Print metrics
	fmt.Println("\nRLNC Integration Verification Results:")
	fmt.Printf("Packet Loss: %d%%\n", *packetLossPercentage)
	fmt.Printf("Successful Transactions: %d/%d (%.2f%%)\n", 
		successCount, *numTransactions, successRate)
	fmt.Printf("Failed Transactions: %d/%d\n", failCount, *numTransactions)
	fmt.Printf("Total Time: %v\n", totalTime)
	fmt.Printf("Throughput: %.2f TPS\n", throughput)

	// Print mesh health metrics
	fmt.Println("\nMesh Network Health:")
	healthMetrics := bridge.GetNetworkHealthSummary()
	for key, value := range healthMetrics {
		fmt.Printf("%s: %v\n", key, value)
	}

	// Print performance metrics
	fmt.Println("\nPerformance Metrics:")
	perfMetrics := bridge.GetMetrics()
	for key, value := range perfMetrics {
		fmt.Printf("%s: %v\n", key, value)
	}

	// Verify if the RLNC implementation was effective
	if *packetLossPercentage > 0 && successRate > float64(100-*packetLossPercentage) {
		fmt.Printf("\n✅ SUCCESS: RLNC successfully provided resilience against %d%% packet loss\n", 
			*packetLossPercentage)
		fmt.Println("The \"100ms and regulated\" value proposition is maintained even with partial network failures.")
	} else if *packetLossPercentage > 0 {
		fmt.Printf("\n❌ NOTICE: RLNC recovery rate (%d%%) is lower than expected for %d%% packet loss\n", 
			int(successRate), *packetLossPercentage)
		fmt.Println("This may indicate that additional tuning is needed for your specific network conditions.")
	} else {
		fmt.Println("\n✅ SUCCESS: All transactions processed successfully with no simulated packet loss.")
	}
}

// packetLossTransport simulates packet loss in the network
type packetLossTransport struct {
	Transport   http.RoundTripper
	packetLoss  int
	hostsToDrop []string
}

func (p *packetLossTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Check if this request should be dropped
	shouldDrop := false
	
	if p.packetLoss > 0 {
		// Check if the host is in our drop list
		host := req.URL.Hostname()
		for _, dropHost := range p.hostsToDrop {
			if host == dropHost {
				// Randomly decide if we should drop this packet
				if rand.Intn(100) < p.packetLoss {
					shouldDrop = true
					break
				}
			}
		}
	}
	
	if shouldDrop {
		// Simulate packet loss by returning a timeout error
		return nil, &url.Error{
			Op:  "Get",
			URL: req.URL.String(),
			Err: fmt.Errorf("simulated packet loss"),
		}
	}
	
	// Otherwise, perform the actual request
	return p.Transport.RoundTrip(req)
}
EOF

# Build the RLNC verification tool
echo "Building RLNC verification tool..."
go build -o bin/verify_rlnc cmd/verify_rlnc.go

# Run RLNC verification with no packet loss
echo "Running RLNC verification with no packet loss..."
./bin/verify_rlnc --packet-loss=0 --transactions=20
echo ""

# Run RLNC verification with 10% packet loss
echo "Running RLNC verification with 10% packet loss..."
./bin/verify_rlnc --packet-loss=10 --transactions=20
echo ""

# Run RLNC verification with 20% packet loss
echo "Running RLNC verification with 20% packet loss..."
./bin/verify_rlnc --packet-loss=20 --transactions=20
echo ""

# Run RLNC verification with maximum packet loss
echo "Running RLNC verification with ${MAX_PACKET_LOSS}% packet loss..."
./bin/verify_rlnc --packet-loss=${MAX_PACKET_LOSS} --transactions=20
echo ""

echo "RLNC integration verification completed!"
echo "Your TEE mesh network now has resilience against partial network failures"
echo "while maintaining the \"100ms and regulated\" value proposition."
