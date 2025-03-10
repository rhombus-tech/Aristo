#!/bin/bash
set -e

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}====== End-to-End Mesh Test ======${NC}"
echo "This test will verify the complete mesh flow from Go through the RustConnector"

# Step 1: Build components
echo -e "${BLUE}Step 1: Building components...${NC}"

# Build Rust controller
cd execution/controller
cargo build
cd ../..

# Build Go test
cd rustconnector
go build ./...
cd ..

# Step 2: Run the actual tests
echo -e "${BLUE}Step 2: Running integration tests...${NC}"

# Set environment variables for mesh functionality
export MESH_ENABLED=true
export DISCOVERY_ENDPOINT="localhost:50051"
export MAX_PEERS=5
export CIRCUIT_BREAKER_THRESHOLD_MS=1000
export PEER_REFRESH_INTERVAL_SEC=30

# Run the Go tests for the RustConnector mesh integration
cd rustconnector
go test -v -run=TestMeshIntegration

# Final report
echo -e "${GREEN}====== End-to-end test completed successfully ======${NC}"
