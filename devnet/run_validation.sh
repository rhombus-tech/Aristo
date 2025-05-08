#!/bin/bash
set -e

echo "Running validation tests on local TEE mesh network..."

# Register the TEE nodes (workers) with the coordinator
echo "----------------------------------------------------------------------"
echo "Registering SGX1 worker..."
REGISTER_RESULT=$(curl -s -X POST -H "Content-Type: application/json" -d '{
  "worker_id": "sgx1",
  "enclave_id": [0, 1, 2, 3, 4, 5, 6, 7, 8, 9],
  "attestation": "simulated-sgx-attestation"
}' http://127.0.0.1:9080/workers)
echo "Registration response: $REGISTER_RESULT"

echo "Registering SGX2 worker..."
REGISTER_RESULT=$(curl -s -X POST -H "Content-Type: application/json" -d '{
  "worker_id": "sgx2",
  "enclave_id": [10, 11, 12, 13, 14, 15, 16, 17, 18, 19],
  "attestation": "simulated-sgx-attestation"
}' http://127.0.0.1:9080/workers)
echo "Registration response: $REGISTER_RESULT"

echo "Registering SEV1 worker..."
REGISTER_RESULT=$(curl -s -X POST -H "Content-Type: application/json" -d '{
  "worker_id": "sev1",
  "enclave_id": [20, 21, 22, 23, 24, 25, 26, 27, 28, 29],
  "attestation": "simulated-sev-attestation"
}' http://127.0.0.1:9080/workers)
echo "Registration response: $REGISTER_RESULT"

echo "Registering SEV2 worker..."
REGISTER_RESULT=$(curl -s -X POST -H "Content-Type: application/json" -d '{
  "worker_id": "sev2",
  "enclave_id": [30, 31, 32, 33, 34, 35, 36, 37, 38, 39],
  "attestation": "simulated-sev-attestation"
}' http://127.0.0.1:9080/workers)
echo "Registration response: $REGISTER_RESULT"

# Register TEE pairs for cross-attestation and mesh communication
echo "----------------------------------------------------------------------"
echo "Registering SGX-SEV TEE pair for cross-attestation..."
PAIR_RESULT=$(curl -s -X POST -H "Content-Type: application/json" -d '{
  "region_id": "local-region-1",
  "primary_worker_id": "sgx1",
  "secondary_worker_id": "sev1"
}' http://127.0.0.1:9080/regions/local-region-1/pairs/register)
echo "Pair registration response: $PAIR_RESULT"

echo "Registering SGX-SGX TEE pair for mesh communication..."
PAIR_RESULT=$(curl -s -X POST -H "Content-Type: application/json" -d '{
  "region_id": "local-region-1",
  "primary_worker_id": "sgx1",
  "secondary_worker_id": "sgx2"
}' http://127.0.0.1:9080/regions/local-region-1/pairs/register)
echo "Pair registration response: $PAIR_RESULT"

# Allow time for registration to complete
sleep 2

# Parameter validation test with length-prefixed format
echo "----------------------------------------------------------------------"
echo "Testing parameter validation with length-prefixed format..."
echo "Payload: 0x00000004AABBCCDD (4-byte length prefix + AABBCCDD data)"
echo "TEE Type: SGX"
echo "Parameter Format: length_prefix"
echo "----------------------------------------------------------------------"
RESULT=$(curl -s -X POST -H "Content-Type: application/json" -d '{
  "worker_id": "sgx1",
  "task_type": "execute",
  "payload": {
    "data": "0x00000004AABBCCDD",
    "format": "length_prefix",
    "validation_level": "strict"
  }
}' http://127.0.0.1:9080/tasks)

echo "RESPONSE:"
echo "$RESULT" | jq '.' 2>/dev/null || echo "$RESULT"

# Check task status if we got a task ID
TASK_ID=$(echo "$RESULT" | jq -r '.data.task_id' 2>/dev/null)
if [ "$TASK_ID" != "null" ] && [ -n "$TASK_ID" ]; then
  echo "Checking task status for ID: $TASK_ID"
  sleep 1
  STATUS=$(curl -s -X GET http://127.0.0.1:9080/tasks/$TASK_ID)
  echo "TASK STATUS:"
  echo "$STATUS" | jq '.' 2>/dev/null || echo "$STATUS"
fi
echo

# Parameter validation test with direct format
echo "----------------------------------------------------------------------"
echo "Testing parameter validation with direct format..."
echo "Payload: 0xAABBCCDD (direct data without length prefix)"
echo "TEE Type: SGX"
echo "Parameter Format: direct"
echo "----------------------------------------------------------------------"
RESULT=$(curl -s -X POST -H "Content-Type: application/json" -d '{
  "worker_id": "sgx1",
  "task_type": "execute",
  "payload": {
    "data": "0xAABBCCDD",
    "format": "direct",
    "validation_level": "strict"
  }
}' http://127.0.0.1:9080/tasks)

echo "RESPONSE:"
echo "$RESULT" | jq '.' 2>/dev/null || echo "$RESULT"

# Check task status if we got a task ID
TASK_ID=$(echo "$RESULT" | jq -r '.data.task_id' 2>/dev/null)
if [ "$TASK_ID" != "null" ] && [ -n "$TASK_ID" ]; then
  echo "Checking task status for ID: $TASK_ID"
  sleep 1
  STATUS=$(curl -s -X GET http://127.0.0.1:9080/tasks/$TASK_ID)
  echo "TASK STATUS:"
  echo "$STATUS" | jq '.' 2>/dev/null || echo "$STATUS"
fi
echo

# Cross-attestation test between SGX and SEV
echo "----------------------------------------------------------------------"
echo "Testing cross-attestation between SGX and SEV..."
echo "Payload: 0x00000004AABBCCDD (4-byte length prefix + AABBCCDD data)"
echo "Primary TEE: SGX (sgx1)"
echo "Secondary TEE: SEV (sev1)"
echo "Parameter Format: length_prefix"
echo "----------------------------------------------------------------------"
RESULT=$(curl -s -X POST -H "Content-Type: application/json" -d '{
  "primary_worker_id": "sgx1",
  "secondary_worker_id": "sev1",
  "task_type": "cross_attestation",
  "payload": {
    "data": "0x00000004AABBCCDD",
    "format": "length_prefix",
    "validation_level": "strict"
  }
}' http://127.0.0.1:9080/tasks)

echo "RESPONSE:"
echo "$RESULT" | jq '.' 2>/dev/null || echo "$RESULT"

# Check task status if we got a task ID
TASK_ID=$(echo "$RESULT" | jq -r '.data.task_id' 2>/dev/null)
if [ "$TASK_ID" != "null" ] && [ -n "$TASK_ID" ]; then
  echo "Checking task status for ID: $TASK_ID"
  sleep 1
  STATUS=$(curl -s -X GET http://127.0.0.1:9080/tasks/$TASK_ID)
  echo "TASK STATUS:"
  echo "$STATUS" | jq '.' 2>/dev/null || echo "$STATUS"
fi
echo

# Mesh network direct communication test using gRPC simulation
echo "----------------------------------------------------------------------"
echo "Testing mesh network communication through coordinator..."
echo "Payload: 0x00000004AABBCCDD (4-byte length prefix + AABBCCDD data)"
echo "Primary TEE: SGX (sgx1)"
echo "Secondary TEE: SGX (sgx2)"
echo "Parameter Format: length_prefix"
echo "----------------------------------------------------------------------"
RESULT=$(curl -s -X POST -H "Content-Type: application/json" -d '{
  "primary_worker_id": "sgx1",
  "secondary_worker_id": "sgx2",
  "task_type": "mesh_execute",
  "payload": {
    "data": "0x00000004AABBCCDD",
    "format": "length_prefix",
    "validation_level": "strict"
  }
}' http://127.0.0.1:9080/tasks)

echo "RESPONSE:"
echo "$RESULT" | jq '.' 2>/dev/null || echo "$RESULT"

# Check task status if we got a task ID
TASK_ID=$(echo "$RESULT" | jq -r '.data.task_id' 2>/dev/null)
if [ "$TASK_ID" != "null" ] && [ -n "$TASK_ID" ]; then
  echo "Checking task status for ID: $TASK_ID"
  sleep 1
  STATUS=$(curl -s -X GET http://127.0.0.1:9080/tasks/$TASK_ID)
  echo "TASK STATUS:"
  echo "$STATUS" | jq '.' 2>/dev/null || echo "$STATUS"
fi
echo

# Performance benchmark test with batch size 100 and 8 threads
echo "----------------------------------------------------------------------"
echo "Running performance benchmark with optimized parameters..."
echo "Batch Size: 100"
echo "Thread Count: 8"
echo "Duration: 5 seconds"
echo "Parameter Format: length_prefix"
echo "----------------------------------------------------------------------"
RESULT=$(curl -s -X POST -H "Content-Type: application/json" -d '{
  "worker_id": "sgx1",
  "task_type": "benchmark",
  "payload": {
    "batch_size": 100,
    "thread_count": 8,
    "duration_seconds": 5,
    "format": "length_prefix"
  }
}' http://127.0.0.1:9080/tasks)

echo "RESPONSE:"
echo "$RESULT" | jq '.' 2>/dev/null || echo "$RESULT"

# Check task status if we got a task ID
TASK_ID=$(echo "$RESULT" | jq -r '.data.task_id' 2>/dev/null)
if [ "$TASK_ID" != "null" ] && [ -n "$TASK_ID" ]; then
  echo "Checking task status for ID: $TASK_ID"
  sleep 1
  STATUS=$(curl -s -X GET http://127.0.0.1:9080/tasks/$TASK_ID)
  echo "TASK STATUS:"
  echo "$STATUS" | jq '.' 2>/dev/null || echo "$STATUS"
fi

echo

# WebAssembly contract test
if [ -f ../contracts/simple_add.wasm ]; then
  echo "Testing WebAssembly contract execution..."
  # First deploy the contract
  CONTRACT_ID=$(curl -s -X POST -H "Content-Type: application/json" -d '{
    "operation": "deploy_contract",
    "contract_path": "../contracts/simple_add.wasm",
    "tee_type": "dual",
    "primary_id": "sgx1",
    "secondary_id": "sev1"
  }' http://127.0.0.1:9080/contract | jq -r '.contract_id')
  
  # Then execute the contract
  if [ -n "$CONTRACT_ID" ]; then
    echo "Contract deployed with ID: $CONTRACT_ID"
    echo "Executing contract..."
    curl -X POST -H "Content-Type: application/json" -d "{
      \"operation\": \"execute_contract\",
      \"contract_id\": \"$CONTRACT_ID\",
      \"function\": \"add\",
      \"parameters\": \"0x0000000800000019000000272A\",
      \"tee_type\": \"dual\",
      \"primary_id\": \"sgx1\",
      \"secondary_id\": \"sev1\"
    }" http://127.0.0.1:9080/contract
    echo
  else
    echo "Failed to deploy contract"
  fi
fi

echo

echo "Validation tests completed!"
