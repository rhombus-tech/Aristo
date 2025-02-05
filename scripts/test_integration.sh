#!/usr/bin/env bash
set -e

# Build simulator
echo "Building simulator..."
cd hyper/x/contracts/simulator
cargo build
cd ../../../../

# Build wasmlanche
echo "Building wasmlanche..."
cd hyper/x/contracts/wasmlanche
cargo build --features simulator
cd ../../../../

# Build and start TEE service
echo "Building and starting TEE service..."
cd execution/controller
cargo build
RUST_LOG=debug DYLD_LIBRARY_PATH=../../hyper/x/contracts/simulator/target/debug cargo run --bin tee-controller &
TEE_PID=$!
cd ../..

# Wait for TEE service to be ready
sleep 2

# Build and start VM
echo "Building and starting VM..."
cd hyper/cmd/hypersdk-cli
go build -o vm .
./vm --tee.sgx-endpoint=localhost:50051 --tee.sev-endpoint=localhost:50052 &
VM_PID=$!
cd ../../..

# Wait for VM to be ready
sleep 2

# Deploy test contract
echo "Deploying test contract..."
CONTRACT_ID=$(curl -s -X POST http://localhost:9650/ext/vm/tee/contract \
  -H "Content-Type: application/json" \
  -d '{
    "code": "AGFzbQEAAAABBwFgAn9/AX8DAgEABQMBABEHEwIGbWVtb3J5AgAIaW5jcmVtZW50AAA=",
    "function": "increment",
    "args": []
  }' | jq -r '.contract_id')

echo "Contract deployed with ID: ${CONTRACT_ID}"

# Call the contract
echo "Calling contract..."
RESULT=$(curl -s -X POST http://localhost:9650/ext/vm/tee/execute \
  -H "Content-Type: application/json" \
  -d "{
    \"contract_id\": \"${CONTRACT_ID}\",
    \"function\": \"increment\",
    \"args\": []
  }")

echo "Execution result: ${RESULT}"

# Check state
echo "Checking contract state..."
STATE=$(curl -s "http://localhost:9650/ext/vm/tee/state/${CONTRACT_ID}")
echo "Contract state: ${STATE}"

# Cleanup
echo "Cleaning up..."
kill $TEE_PID
kill $VM_PID
