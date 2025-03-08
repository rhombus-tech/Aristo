#!/bin/bash

set -e

# Define color codes for better output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Define paths and variables
BASE_DIR=$(dirname $0)
cd $BASE_DIR

CONTROLLER_DIR="$BASE_DIR/execution/controller"
CONTRACTS_DIR="$CONTROLLER_DIR/tests/contracts/simple_add"
CONTRACT_BUILD_DIR="$CONTRACTS_DIR/target/wasm32-unknown-unknown/release"
CONTRACT_WASM="$CONTRACT_BUILD_DIR/simple_add.wasm"
TEST_CLIENT="$BASE_DIR/execution/target/release/test-client"

echo -e "${BLUE}========================================"
echo " TEE Contract Validation Test"
echo -e "========================================${NC}"

# Check that the contract is built
if [ ! -f "$CONTRACT_WASM" ]; then
    echo -e "${RED}WASM contract not found at: $CONTRACT_WASM"
    echo -e "Please run ./test_tee_setup.sh first${NC}"
    exit 1
fi

# Check that the test client is built
if [ ! -f "$TEST_CLIENT" ]; then
    echo -e "${RED}Test client not found at: $TEST_CLIENT"
    echo -e "Please run ./test_tee_setup.sh first${NC}"
    exit 1
fi

# First, check if the controller is running
if ! nc -z localhost 50051 2>/dev/null; then
    echo -e "${YELLOW}TEE Controller doesn't appear to be running on port 50051"
    echo -e "Please start the controller first with:${NC}"
    echo "$CONTROLLER_DIR/target/release/tee-controller --port 50051 --base-dir /tmp/tee-controller --simulate --bypass-attestation"
    exit 1
fi

echo -e "${GREEN}TEE Controller is running on port 50051${NC}"
echo ""

# Run a series of tests with different parameters
function run_test() {
    local test_name=$1
    local function_name=$2
    local params=$3
    
    echo -e "${BLUE}====== Test: $test_name ======${NC}"
    echo "Function: $function_name"
    echo "Parameters: $params"
    
    # Run the test
    if $TEST_CLIENT --function "$function_name" --params "$params"; then
        echo -e "${GREEN}✓ Test passed${NC}"
    else
        echo -e "${RED}✗ Test failed${NC}"
    fi
    echo ""
}

# Run tests with different parameter values
echo -e "${YELLOW}Running validation tests...${NC}"
run_test "Basic Addition" "add" "42,58"
run_test "Zero Values" "add" "0,0"
run_test "Negative Values" "add" "-5,10"
run_test "Large Values" "add" "100000,200000"

# Now test the direct function 
echo -e "${YELLOW}Testing direct function calls...${NC}"
run_test "Direct Add" "add_direct" "42"

echo -e "${BLUE}========================================"
echo " Validation Testing Complete"
echo -e "========================================${NC}"
