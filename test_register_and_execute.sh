#!/bin/bash

set -e

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Base directory for the repository
REPO_DIR="/Users/talzisckind/Downloads/aristo-fresh 2"

# Temporary directory for test artifacts
TMP_DIR="/tmp/tee-controller"

# Define paths
RUST_CONTROLLER="${REPO_DIR}/execution/target/debug/tee-controller"
WASM_MODULE="${REPO_DIR}/execution/controller/tests/contracts/simple_add/target/wasm32-unknown-unknown/release/simple_add.wasm"
INPUT_FILE_BASIC="${TMP_DIR}/input.json"
INPUT_FILE_WITH_METHOD="${TMP_DIR}/input_with_method.json"

# Create operation ID (short unique ID for testing)
OPERATION_ID="op-$(date +%s | tail -c 8)"
REGION_ID="test-region"
TEE_TYPE="sgx"

# Create necessary directories
echo -e "${BLUE}Creating necessary directories...${NC}"
mkdir -p "${TMP_DIR}"
mkdir -p "${TMP_DIR}/sgx"
mkdir -p "${TMP_DIR}/sev"

# Create test input data
echo -e "${BLUE}Creating input files...${NC}"
echo '{"a": 576791163, "b": 741679162}' > "${INPUT_FILE_BASIC}"
echo '{"a": 5, "b": 10, "method": "add"}' > "${INPUT_FILE_WITH_METHOD}"

# Check that input files were created
if [ ! -f "${INPUT_FILE_BASIC}" ] || [ ! -f "${INPUT_FILE_WITH_METHOD}" ]; then
    echo -e "${RED}Error: Failed to create input files${NC}"
    exit 1
fi

# Check if the WASM module exists
if [ ! -f "${WASM_MODULE}" ]; then
    echo -e "${RED}Error: WASM module not found at ${WASM_MODULE}${NC}"
    exit 1
fi

echo -e "${GREEN}====== Testing Paired Execution with Mesh ======${NC}"

# Function to run a command and capture detailed output in case of failure
run_with_log() {
    local test_name=$1
    shift
    echo -e "${BLUE}Running $test_name...${NC}"
    
    "$@"
    local status=$?
    
    if [ $status -ne 0 ]; then
        echo -e "${RED}Test failed: $test_name${NC}"
        exit 1
    fi
    echo -e "${GREEN}Test succeeded: $test_name${NC}"
}

# Test 1: Basic paired execution with function call
echo -e "${BLUE}Test 1: Basic paired execution with 'add' method...${NC}"
run_with_log "paired_execution" "${RUST_CONTROLLER}" --simulate --verbose --mesh-enabled \
    --region-id ${REGION_ID} --tee-type ${TEE_TYPE} \
    --base-dir "${TMP_DIR}" \
    execute-paired \
    --wasm-module "${WASM_MODULE}" \
    --input "${INPUT_FILE_BASIC}" \
    --contract-id "test-contract" \
    --operation-id "${OPERATION_ID}" \
    --function-call "add" \
    --timeout 5000

# Test 2: Testing paired execution with mesh cache
echo -e "${BLUE}Test 2: Testing paired execution with mesh cache...${NC}"
run_with_log "paired_execution_cache" "${RUST_CONTROLLER}" --simulate --verbose --mesh-enabled \
    --region-id ${REGION_ID} --tee-type ${TEE_TYPE} \
    --base-dir "${TMP_DIR}" \
    execute-paired \
    --wasm-module "${WASM_MODULE}" \
    --input "${INPUT_FILE_BASIC}" \
    --contract-id "test-contract" \
    --operation-id "${OPERATION_ID}-2" \
    --function-call "add" \
    --timeout 5000 \
    --use-cache

# Test 3: Discover peers in the mesh network
echo -e "${BLUE}Test 3: Testing mesh peer discovery...${NC}"
run_with_log "discover_peers" "${RUST_CONTROLLER}" --simulate --verbose --mesh-enabled \
    --region-id ${REGION_ID} --tee-type ${TEE_TYPE} \
    --base-dir "${TMP_DIR}" \
    discover-peers \
    --region ${REGION_ID}

# Test 4: State synchronization between TEEs
echo -e "${BLUE}Test 4: Testing state synchronization...${NC}"
run_with_log "sync_state" "${RUST_CONTROLLER}" --simulate --verbose --mesh-enabled \
    --region-id ${REGION_ID} --tee-type ${TEE_TYPE} \
    --base-dir "${TMP_DIR}" \
    sync-state \
    --target-tee "tee-simulated-target" \
    --object-id "test-contract" \
    --use-deltas

# Test 5: Testing mesh execution with method in JSON
echo -e "${BLUE}Test 5: Testing mesh execution (method in JSON input)...${NC}"
run_with_log "mesh_execute" "${RUST_CONTROLLER}" --simulate --verbose --mesh-enabled \
    --region-id ${REGION_ID} --tee-type ${TEE_TYPE} \
    --base-dir "${TMP_DIR}" \
    mesh-execute \
    --target-tee "tee-simulated-target" \
    --region ${REGION_ID} \
    --tee-type ${TEE_TYPE} \
    --wasm-module "${WASM_MODULE}" \
    --input "${INPUT_FILE_WITH_METHOD}"

# Test 6: Testing mesh execution with cache
echo -e "${BLUE}Test 6: Testing mesh execution with cache...${NC}"
run_with_log "mesh_execute_cache" "${RUST_CONTROLLER}" --simulate --verbose --mesh-enabled \
    --region-id ${REGION_ID} --tee-type ${TEE_TYPE} \
    --base-dir "${TMP_DIR}" \
    execute-with-mesh-cache \
    --target-tee "tee-simulated-target" \
    --region ${REGION_ID} \
    --tee-type ${TEE_TYPE} \
    --wasm-module "${WASM_MODULE}" \
    --input "${INPUT_FILE_WITH_METHOD}" \
    --use-cache \
    --cache-ttl-sec 60

# Test 7: Testing peer discovery with max-results
echo -e "${BLUE}Test 7: Testing peer discovery with max-results...${NC}"
run_with_log "peer_discovery" "${RUST_CONTROLLER}" --simulate --verbose --mesh-enabled \
    --region-id ${REGION_ID} --tee-type ${TEE_TYPE} \
    --base-dir "${TMP_DIR}" \
    discover-peers \
    --region ${REGION_ID} \
    --tee-type ${TEE_TYPE} \
    --max-results 10

echo -e "${GREEN}====== Mesh functionality tests completed ======${NC}"
