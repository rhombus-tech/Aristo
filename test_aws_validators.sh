#!/bin/bash
# Test script for AWS-deployed dual-format parameter validators

# AWS TEE node endpoints
SGX_ENDPOINTS=("ec2-54-225-41-220.compute-1.amazonaws.com" "ec2-52-54-181-245.compute-1.amazonaws.com")
SEV_ENDPOINTS=("ec2-3-81-65-148.compute-1.amazonaws.com" "ec2-54-161-2-145.compute-1.amazonaws.com")
PORT=7300

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Generate test data
# 1. Direct format (32-byte contract ID)
echo -e "${YELLOW}Generating test data...${NC}"
dd if=/dev/urandom of=test_direct_format.bin bs=32 count=1 2>/dev/null
hexdump -C test_direct_format.bin | head -2

# 2. Length-prefixed format (4-byte length + data)
python3 -c "import struct; data = open('test_direct_format.bin', 'rb').read(); open('test_length_prefixed.bin', 'wb').write(struct.pack('<I', len(data)) + data)"
hexdump -C test_length_prefixed.bin | head -2

# Test all validator endpoints
echo -e "${YELLOW}\nTesting SGX validators:${NC}"
for endpoint in "${SGX_ENDPOINTS[@]}"; do
  echo -e "\nTesting $endpoint..."
  
  # Test direct format
  echo -e "${YELLOW}Testing direct format:${NC}"
  curl -s -X POST -H "Content-Type: application/octet-stream" --data-binary @test_direct_format.bin \
    http://$endpoint:$PORT/validate | jq .
    
  # Test length-prefixed format
  echo -e "${YELLOW}Testing length-prefixed format:${NC}"
  curl -s -X POST -H "Content-Type: application/octet-stream" --data-binary @test_length_prefixed.bin \
    http://$endpoint:$PORT/validate | jq .
    
  # Get validator stats
  echo -e "${YELLOW}Validator statistics:${NC}"
  curl -s http://$endpoint:$PORT/stats | jq .
done

echo -e "${YELLOW}\nTesting SEV validators:${NC}"
for endpoint in "${SEV_ENDPOINTS[@]}"; do
  echo -e "\nTesting $endpoint..."
  
  # Test direct format
  echo -e "${YELLOW}Testing direct format:${NC}"
  curl -s -X POST -H "Content-Type: application/octet-stream" --data-binary @test_direct_format.bin \
    http://$endpoint:$PORT/validate | jq .
    
  # Test length-prefixed format
  echo -e "${YELLOW}Testing length-prefixed format:${NC}"
  curl -s -X POST -H "Content-Type: application/octet-stream" --data-binary @test_length_prefixed.bin \
    http://$endpoint:$PORT/validate | jq .
    
  # Get validator stats
  echo -e "${YELLOW}Validator statistics:${NC}"
  curl -s http://$endpoint:$PORT/stats | jq .
done

# Test cross-validation between SGX and SEV
echo -e "${YELLOW}\nTesting cross-validation between SGX and SEV:${NC}"
echo -e "SGX -> SEV:"
curl -s -X POST -H "Content-Type: application/octet-stream" --data-binary @test_direct_format.bin \
  http://${SGX_ENDPOINTS[0]}:$PORT/cross-validate?peer=${SEV_ENDPOINTS[0]} | jq .

echo -e "SEV -> SGX:"
curl -s -X POST -H "Content-Type: application/octet-stream" --data-binary @test_direct_format.bin \
  http://${SEV_ENDPOINTS[0]}:$PORT/cross-validate?peer=${SGX_ENDPOINTS[0]} | jq .

# Performance test
echo -e "${YELLOW}\nRunning performance test on SGX validator:${NC}"
echo "Sending 1000 requests in parallel..."

time for i in {1..1000}; do
  curl -s -X POST -H "Content-Type: application/octet-stream" --data-binary @test_direct_format.bin \
    http://${SGX_ENDPOINTS[0]}:$PORT/validate > /dev/null &
done
wait

echo -e "${YELLOW}Final validator statistics after performance test:${NC}"
curl -s http://${SGX_ENDPOINTS[0]}:$PORT/stats | jq .

# Cleanup
rm test_direct_format.bin test_length_prefixed.bin
echo -e "${GREEN}Test complete!${NC}"
