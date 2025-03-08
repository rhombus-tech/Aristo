#!/bin/bash

set -e

# Define paths and variables
BASE_DIR=$(dirname $0)
cd $BASE_DIR

CONTROLLER_DIR="$BASE_DIR/execution/controller"
CONTRACTS_DIR="$CONTROLLER_DIR/tests/contracts/simple_add"
CONTRACT_BUILD_DIR="$CONTRACTS_DIR/target/wasm32-unknown-unknown/release"
CONTRACT_WASM="$CONTRACT_BUILD_DIR/simple_add.wasm"

# Configure to use real hardware TEE 
USE_SIM=""  # Empty for real hardware, use "--simulate" for local testing
PORT="50051"
BASE_DATA_DIR="/tmp/tee-controller"  # Where contract state is stored

echo "========================================"
echo " TEE Test Environment Setup"
echo "========================================"

# Create necessary directories
mkdir -p "$BASE_DATA_DIR"
mkdir -p "$CONTRACT_BUILD_DIR"

# Step 1: Build the WebAssembly contract
echo "Building simple_add contract..."
cd "$CONTRACTS_DIR"
if ! cargo build --target wasm32-unknown-unknown --release; then
    echo "Failed to build simple_add contract"
    exit 1
fi
cd - > /dev/null

if [ ! -f "$CONTRACT_WASM" ]; then
    echo "WASM file not found at: $CONTRACT_WASM"
    exit 1
fi
echo "✓ Contract built successfully"

# Step 2: Build the controller
echo "Building TEE controller..."
cd "$CONTROLLER_DIR"
if ! cargo build --release --bin tee-controller; then
    echo "Failed to build tee-controller"
    exit 1
fi
cd - > /dev/null

# Step 3: Build the test client
echo "Building test client..."
cd "$CONTROLLER_DIR"
if ! cargo build --release --bin test-client; then
    echo "Failed to build test-client"
    exit 1
fi
cd - > /dev/null

echo "✓ Build process completed successfully"
echo ""
echo "========================================"
echo " Starting TEE Controller"
echo "========================================"
echo "To start the controller in a new terminal, run:"
echo "$CONTROLLER_DIR/target/release/tee-controller --port $PORT --base-dir $BASE_DATA_DIR $USE_SIM --bypass-attestation"
echo ""
echo "To test the controller with the test client, run:"
echo "$CONTROLLER_DIR/target/release/test-client"
echo ""
echo "========================================"

# Ask if we should start the controller
read -p "Start the controller now? (y/n) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo "Starting TEE controller..."
    $CONTROLLER_DIR/target/release/tee-controller --port $PORT --base-dir $BASE_DATA_DIR $USE_SIM --bypass-attestation
fi
