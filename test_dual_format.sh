#!/bin/bash
# Simple test for dual-format parameter validation on AWS nodes

# Define endpoints
SGX_NODE="ec2-54-225-41-220.compute-1.amazonaws.com:7300"
SEV_NODE="ec2-3-81-65-148.compute-1.amazonaws.com:7300"

# Generate test data
echo -e "Generating test data..."
# Direct format (32-byte contract ID)
dd if=/dev/urandom of=test_direct.bin bs=32 count=1 2>/dev/null

# Create length-prefixed format (4 bytes length + data)
python3 -c '
import struct
with open("test_direct.bin", "rb") as f:
    data = f.read()
with open("test_length_prefixed.bin", "wb") as f:
    f.write(struct.pack("<I", len(data)) + data)
'

echo -e "\n=== Testing SGX Node with Direct Format ==="
curl -s -X POST -H "Content-Type: application/octet-stream" --data-binary @test_direct.bin http://$SGX_NODE/validate

echo -e "\n=== Testing SGX Node with Length-Prefixed Format ==="
curl -s -X POST -H "Content-Type: application/octet-stream" --data-binary @test_length_prefixed.bin http://$SGX_NODE/validate

echo -e "\n=== Testing SEV Node with Direct Format ==="
curl -s -X POST -H "Content-Type: application/octet-stream" --data-binary @test_direct.bin http://$SEV_NODE/validate

echo -e "\n=== Testing SEV Node with Length-Prefixed Format ==="
curl -s -X POST -H "Content-Type: application/octet-stream" --data-binary @test_length_prefixed.bin http://$SEV_NODE/validate

echo -e "\n=== Testing Cross-Validation (SGX → SEV) ==="
curl -s -X POST -H "Content-Type: application/octet-stream" --data-binary @test_direct.bin \
  "http://$SGX_NODE/cross-validate?peer=ec2-3-81-65-148.compute-1.amazonaws.com:7300"

echo -e "\n=== Testing Cross-Validation (SEV → SGX) ==="
curl -s -X POST -H "Content-Type: application/octet-stream" --data-binary @test_direct.bin \
  "http://$SEV_NODE/cross-validate?peer=ec2-54-225-41-220.compute-1.amazonaws.com:7300"

# Clean up
rm test_direct.bin test_length_prefixed.bin

echo -e "\nTest complete!"
