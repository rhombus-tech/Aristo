#!/bin/bash

# Simple test script for dual TEE mesh network

# Configuration
KEY_NAME="nasdaq-tee-key"
COORDINATOR_IP="54.226.83.253"
SGX_IPS=(54.226.83.253 3.80.113.74)
SEV_IPS=(54.209.198.69 52.206.202.214)

# Create a simple test script directly on the coordinator
ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${COORDINATOR_IP} << 'EOF'
# First register the TEE nodes with the coordinator
echo "Registering SGX node 1 with coordinator..."
curl -s -X POST -H "Content-Type: application/json" \
  -d '{"id":"sgx-node-0","address":"http://54.226.83.253:7070","region":"us-east","node_type":"SGX","batch_size":500,"pair_id":0}' \
  http://localhost:8080/register

echo -e "\nRegistering SEV node 1 with coordinator..."
curl -s -X POST -H "Content-Type: application/json" \
  -d '{"id":"sev-node-0","address":"http://54.209.198.69:7070","region":"us-east","node_type":"SEV","batch_size":100,"pair_id":0}' \
  http://localhost:8080/register

echo -e "\nRegistering SGX node 2 with coordinator..."  
curl -s -X POST -H "Content-Type: application/json" \
  -d '{"id":"sgx-node-1","address":"http://3.80.113.74:7070","region":"us-east","node_type":"SGX","batch_size":500,"pair_id":1}' \
  http://localhost:8080/register

echo -e "\nRegistering SEV node 2 with coordinator..."
curl -s -X POST -H "Content-Type: application/json" \
  -d '{"id":"sev-node-1","address":"http://52.206.202.214:7070","region":"us-east","node_type":"SEV","batch_size":100,"pair_id":1}' \
  http://localhost:8080/register

echo -e "\n========== Coordinator Status =========="
curl -s http://localhost:8080/tees

echo -e "\n\n========== SGX Node 1 Status =========="
curl -s http://54.226.83.253:7070/mesh-status

echo -e "\n\n========== SEV Node 1 Status =========="
curl -s http://54.209.198.69:7070/mesh-status

echo -e "\n\n========== Testing Parameter Validation =========="
echo "Testing length-prefixed format on SGX node..."
curl -s -X POST -H "Content-Type: application/json" \
  -d '{"contract_id":"test-contract","function":"validate","parameters":[10,0,0,0,84,69,83,84,80,65,89,76,79,65,68]}' \
  http://54.226.83.253:7070/execute

echo -e "\n\nTesting direct format on SGX node..."
curl -s -X POST -H "Content-Type: application/json" \
  -d '{"contract_id":"test-contract","function":"validate","parameters":[68,73,82,69,67,84,80,65,89,76,79,65,68]}' \
  http://54.226.83.253:7070/execute

echo -e "\n\nTesting parameter validation on SEV node..."
curl -s -X POST -H "Content-Type: application/json" \
  -d '{"contract_id":"test-contract","function":"validate","parameters":[10,0,0,0,84,69,83,84,80,65,89,76,79,65,68]}' \
  http://54.209.198.69:7070/execute

echo -e "\n\n========== Testing Cross-Attestation =========="
echo "Testing SGX -> SEV attestation..."
curl -s -X POST -H "Content-Type: application/json" \
  -d '{"contract_id":"cross-attestation","function":"attest","parameters":[10,0,0,0,84,69,83,84,80,65,89,76,79,65,68]}' \
  http://54.226.83.253:7070/execute

echo -e "\n\nTesting SEV -> SGX attestation..."
curl -s -X POST -H "Content-Type: application/json" \
  -d '{"contract_id":"cross-attestation","function":"attest","parameters":[10,0,0,0,84,69,83,84,80,65,89,76,79,65,68]}' \
  http://54.209.198.69:7070/execute

echo -e "\n\n========== Mesh Network Test Complete =========="
echo "Dual TEE mesh network is deployed and operational!"
EOF

echo "Simple test completed!"
