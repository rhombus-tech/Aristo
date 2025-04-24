#!/bin/bash
# Test script for dual-format parameter validation to run directly on AWS node

# Create test data
cd /tmp
echo "Creating test data..."
dd if=/dev/urandom of=test_direct.bin bs=32 count=1 2>/dev/null
echo "Created direct format test data (32 bytes)"

# Create length-prefixed version using Python
cat > create_prefixed.py << EOF
import struct
with open("test_direct.bin", "rb") as f:
    data = f.read()
with open("test_length_prefixed.bin", "wb") as f:
    f.write(struct.pack("<I", len(data)) + data)
print("Created length-prefixed test data (%d bytes)" % (len(data) + 4))
EOF

python3 create_prefixed.py

# Test direct format parameter validation
echo -e "\n=== Testing Direct Format Validation ==="
curl -s -X POST -H "Content-Type: application/octet-stream" --data-binary @test_direct.bin http://localhost:7300/validate
echo

# Test length-prefixed format validation
echo -e "\n=== Testing Length-Prefixed Format Validation ==="
curl -s -X POST -H "Content-Type: application/octet-stream" --data-binary @test_length_prefixed.bin http://localhost:7300/validate
echo

# Get validator stats
echo -e "\n=== Validator Statistics ==="
curl -s http://localhost:7300/stats
echo

# Test cross-validation if peer endpoint is provided
PEER="ec2-54-225-41-220.compute-1.amazonaws.com"
if [[ $(hostname) == *"54-225-41-220"* ]]; then
  PEER="ec2-3-81-65-148.compute-1.amazonaws.com"
fi

echo -e "\n=== Testing Cross-Validation with $PEER ==="
curl -s -X POST -H "Content-Type: application/octet-stream" --data-binary @test_direct.bin "http://localhost:7300/cross-validate?peer=$PEER:7300"
echo

# Clean up
rm test_direct.bin test_length_prefixed.bin create_prefixed.py
echo -e "\nTest complete!"
