#!/bin/bash

echo "Starting simple HTTP endpoint test for coordinator..."

# Try a simple health check
echo "Testing health endpoint..."
curl -v http://localhost:8080/health

# Try registering a worker with the exact format expected
echo -e "\n\nTesting worker registration..."
curl -v -X POST http://localhost:8080/workers/register \
  -H "Content-Type: application/json" \
  -d '{"worker_id":"test-worker","enclave_id":[0,1,2,3]}'

# Try getting workers
echo -e "\n\nTesting get workers..."
curl -v http://localhost:8080/workers
