#!/bin/bash
# Test script for the mesh caching functionality with TEE pairs

# Set up vars
PROJECT_ROOT="/Users/talzisckind/Downloads/aristo-fresh 2"
CONTROLLER_PATH="${PROJECT_ROOT}/execution/target/debug/tee-controller"
TEST_DATA_DIR="/tmp/tee-test-data"
INPUT_FILE="${TEST_DATA_DIR}/input.json"
WASM_FILE="${TEST_DATA_DIR}/simple.wasm"

# Create test directory and files
mkdir -p "${TEST_DATA_DIR}"
echo '{"test":"data"}' > "${INPUT_FILE}"

# Copy test WASM file to test directory
cp "${PROJECT_ROOT}/execution/controller/tests/contracts/simple_add/target/wasm32-unknown-unknown/release/simple_add.wasm" "${WASM_FILE}"

# Build the Rust controller if needed
echo "Building Rust controller..."
(cd "${PROJECT_ROOT}/execution/controller" && cargo build)

# Define parameters for the test
REGION_ID="test-region"
TEST_OBJECT_ID="test-wasm-object-1"
CONTRACT_ID="test-contract-001"

# Run the controller with paired execution mode
echo "Executing WASM module using paired execution (SGX + SEV)..."
"${CONTROLLER_PATH}" --simulate --mesh-enabled \
  --region-id "${REGION_ID}" \
  execute-paired \
  --wasm-module "${WASM_FILE}" \
  --input "${INPUT_FILE}" \
  --contract-id "${CONTRACT_ID}" \
  --operation-id "test-op-001" \
  --timeout 5000

# Run a second time to test cache
echo "Running a second time to test cache..."
"${CONTROLLER_PATH}" --simulate --mesh-enabled \
  --region-id "${REGION_ID}" \
  execute-paired \
  --wasm-module "${WASM_FILE}" \
  --input "${INPUT_FILE}" \
  --contract-id "${CONTRACT_ID}" \
  --operation-id "test-op-002" \
  --timeout 5000 \
  --use-cache

echo "Test completed!"
