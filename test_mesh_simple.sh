#!/bin/bash

# Set variables and directories
BASE_DIR="/tmp/tee-controller"
CONTRACT_DIR="$BASE_DIR/contracts"
STATE_DIR="$BASE_DIR/state"
INPUT_DIR="$BASE_DIR"
WASM_PATH="./execution/controller/tests/contracts/simple_add/target/wasm32-unknown-unknown/release/simple_add.wasm"

# Make sure base directories exist
mkdir -p "$CONTRACT_DIR" "$STATE_DIR" "$INPUT_DIR"

# Create input data files
echo '{"a": 576791163, "b": 741679162}' > "$INPUT_DIR/input.json"
echo '{"a": 5, "b": 10, "method": "add"}' > "$INPUT_DIR/input_with_method.json"

# Check if input files were created successfully
if [ ! -f "$INPUT_DIR/input.json" ] || [ ! -f "$INPUT_DIR/input_with_method.json" ]; then
    echo "ERROR: Failed to create input files"
    exit 1
fi

# Make sure the WASM file exists
if [ ! -f "$WASM_PATH" ]; then
    echo "ERROR: WASM file does not exist at $WASM_PATH"
    exit 1
fi

FUNCTION_CALL="add"

echo "Running standard paired execution test..."
./execution/target/debug/tee-controller --simulate --verbose --mesh-enabled --region-id test-region --tee-type sgx --base-dir $BASE_DIR execute-paired \
    --wasm-module $WASM_PATH \
    --input $INPUT_DIR/input.json \
    --contract-id test-contract \
    --operation-id op-$(date +%s | tail -c 8) \
    --function-call $FUNCTION_CALL \
    --timeout 5000

if [ $? -ne 0 ]; then
    echo "ERROR: Paired execution test failed"
    exit 1
fi

echo "Running mesh execution test..."
# The correct parameter is --region not --region-id for mesh commands
./execution/target/debug/tee-controller --simulate --verbose --mesh-enabled --region-id test-region --tee-type sgx --base-dir $BASE_DIR mesh-execute \
    --target-tee "tee-simulated-target" \
    --region test-region \
    --tee-type sgx \
    --wasm-module $WASM_PATH \
    --input $INPUT_DIR/input_with_method.json

if [ $? -ne 0 ]; then
    echo "ERROR: Mesh execution test failed"
    exit 1
fi

echo "Running mesh execution with cache test..."
./execution/target/debug/tee-controller --simulate --verbose --mesh-enabled --region-id test-region --tee-type sgx --base-dir $BASE_DIR execute-with-mesh-cache \
    --target-tee "tee-simulated-target" \
    --region test-region \
    --tee-type sgx \
    --wasm-module $WASM_PATH \
    --input $INPUT_DIR/input_with_method.json \
    --use-cache \
    --cache-ttl-sec 60

if [ $? -ne 0 ]; then
    echo "ERROR: Mesh execution with cache test failed"
    exit 1
fi

echo "Running peer discovery test..."
# The correct parameter is --region for peer discovery and --max-results (not --max-peers)
./execution/target/debug/tee-controller --simulate --verbose --mesh-enabled --region-id test-region --tee-type sgx --base-dir $BASE_DIR discover-peers \
    --region test-region \
    --tee-type sgx \
    --max-results 10

if [ $? -ne 0 ]; then
    echo "ERROR: Peer discovery test failed"
    exit 1
fi

echo "All tests completed successfully!"
