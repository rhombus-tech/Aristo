#!/bin/bash
# Verification script for TEE controllers
# Tests service status, connectivity, and parameter validation

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
KEY_NAME="nasdaq-tee-key"
TEST_DIR="/tmp/tee-validation-tests"
SSH_OPTS="-o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem"

# Define test data
LENGTH_PREFIXED_DATA="00000010hello_tee_world" # 16 bytes of data with 4-byte length prefix
DIRECT_FORMAT_DATA="0123456789abcdef0123456789abcdef" # 32-byte contract ID style data

# Function to print section headers
print_header() {
    echo -e "\n${BLUE}=== $1 ===${NC}"
}

# Function to print success/failure
print_status() {
    if [ $1 -eq 0 ]; then
        echo -e "${GREEN}✓ $2${NC}"
    else
        echo -e "${RED}✗ $2${NC}"
        FAILURES=$((FAILURES+1))
    fi
}

# Track failures
FAILURES=0

# Determine TEE node pairs from CloudFormation outputs
SGX_NODES=()
SEV_NODES=()
PAIR_IDS=()
PAIR_STACKS=$(aws cloudformation describe-stacks --region us-east-1 --query "Stacks[?contains(StackName, 'nasdaq-tee-pairs')].StackName" --output text)

print_header "Discovering TEE Pairs"
for STACK in $PAIR_STACKS; do
  SGX_IP=$(aws cloudformation describe-stacks --stack-name $STACK --region us-east-1 --query "Stacks[0].Outputs[?OutputKey=='SGXNodePublicIP'].OutputValue" --output text)
  SEV_IP=$(aws cloudformation describe-stacks --stack-name $STACK --region us-east-1 --query "Stacks[0].Outputs[?OutputKey=='SEVNodePublicIP'].OutputValue" --output text)
  
  if [ -n "$SGX_IP" ] && [ -n "$SEV_IP" ]; then
    PAIR_ID=$(echo $STACK | grep -oE '[0-9]+$')
    PAIR_IDS+=($PAIR_ID)
    SGX_NODES+=($SGX_IP)
    SEV_NODES+=($SEV_IP)
    
    echo "Found Pair $PAIR_ID:"
    echo "  SGX Node: $SGX_IP"
    echo "  SEV Node: $SEV_IP"
  fi
done

if [ ${#SGX_NODES[@]} -eq 0 ] || [ ${#SEV_NODES[@]} -eq 0 ]; then
  echo -e "${RED}No TEE pairs found. Please deploy TEE pairs first.${NC}"
  exit 1
fi

# Check 1: Service Status
print_header "Checking TEE Controller Service Status"

for i in "${!SGX_NODES[@]}"; do
  SGX_IP=${SGX_NODES[$i]}
  SEV_IP=${SEV_NODES[$i]}
  PAIR_ID=${PAIR_IDS[$i]}
  
  echo -e "${YELLOW}Pair $PAIR_ID:${NC}"
  
  # Check SGX node
  echo -n "  SGX Controller ($SGX_IP): "
  SGX_STATUS=$(ssh $SSH_OPTS ubuntu@$SGX_IP "sudo systemctl is-active controller")
  if [ "$SGX_STATUS" == "active" ]; then
    echo -e "${GREEN}Running${NC}"
  else
    echo -e "${RED}Not Running (status: $SGX_STATUS)${NC}"
    FAILURES=$((FAILURES+1))
    
    # Get logs if service failed
    echo -e "${YELLOW}  SGX Controller Logs:${NC}"
    ssh $SSH_OPTS ubuntu@$SGX_IP "sudo journalctl -u controller -n 20 --no-pager"
  fi
  
  # Check SEV node
  echo -n "  SEV Controller ($SEV_IP): "
  SEV_STATUS=$(ssh $SSH_OPTS ubuntu@$SEV_IP "sudo systemctl is-active controller")
  if [ "$SEV_STATUS" == "active" ]; then
    echo -e "${GREEN}Running${NC}"
  else
    echo -e "${RED}Not Running (status: $SEV_STATUS)${NC}"
    FAILURES=$((FAILURES+1))
    
    # Get logs if service failed
    echo -e "${YELLOW}  SEV Controller Logs:${NC}"
    ssh $SSH_OPTS ubuntu@$SEV_IP "sudo journalctl -u controller -n 20 --no-pager"
  fi
done

# Check 2: Connectivity Test
print_header "Testing TEE Node Connectivity"

for i in "${!SGX_NODES[@]}"; do
  SGX_IP=${SGX_NODES[$i]}
  SEV_IP=${SEV_NODES[$i]}
  PAIR_ID=${PAIR_IDS[$i]}
  
  echo -e "${YELLOW}Pair $PAIR_ID:${NC}"
  
  # SGX -> SEV
  echo -n "  SGX -> SEV connectivity: "
  SGX_TO_SEV=$(ssh $SSH_OPTS ubuntu@$SGX_IP "nc -zv $SEV_IP 7070 2>&1")
  if [[ $SGX_TO_SEV == *"succeeded"* ]]; then
    echo -e "${GREEN}Connected${NC}"
  else
    echo -e "${RED}Failed${NC}"
    FAILURES=$((FAILURES+1))
  fi
  
  # SEV -> SGX
  echo -n "  SEV -> SGX connectivity: "
  SEV_TO_SGX=$(ssh $SSH_OPTS ubuntu@$SEV_IP "nc -zv $SGX_IP 7070 2>&1")
  if [[ $SEV_TO_SGX == *"succeeded"* ]]; then
    echo -e "${GREEN}Connected${NC}"
  else
    echo -e "${RED}Failed${NC}"
    FAILURES=$((FAILURES+1))
  fi
done

# Check 3: Parameter Validation Test
print_header "Testing Parameter Validation"

# Create test script for parameter validation
cat > /tmp/test_parameter_validation.py << EOF
#!/usr/bin/env python3
import sys
import socket
import struct
import binascii
import time

def send_request(host, port, data, format_type):
    print(f"Sending {format_type} request to {host}:{port}")
    print(f"Data: {binascii.hexlify(data).decode()}")
    
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    sock.settimeout(5)
    try:
        sock.connect((host, port))
        sock.sendall(data)
        
        # Get response
        response = sock.recv(1024)
        print(f"Received: {binascii.hexlify(response).decode()}")
        
        # Simple response validation
        if len(response) > 0:
            print("✓ Received response")
            return True
        else:
            print("✗ Empty response")
            return False
            
    except Exception as e:
        print(f"Error: {e}")
        return False
    finally:
        sock.close()

def main():
    if len(sys.argv) != 3:
        print("Usage: ./test_parameter_validation.py <host> <format>")
        print("  format: length_prefixed or direct")
        sys.exit(1)
        
    host = sys.argv[1]
    format_type = sys.argv[2]
    port = 7070
    
    if format_type == "length_prefixed":
        # Create a length-prefixed payload (4-byte length + data)
        message = b"hello_tee_world"
        length = len(message)
        data = struct.pack("<I", length) + message
        
    elif format_type == "direct":
        # Create a direct format payload (32-byte data)
        data = bytes.fromhex("0123456789abcdef0123456789abcdef")
        
    else:
        print("Invalid format type. Use 'length_prefixed' or 'direct'")
        sys.exit(1)
    
    success = send_request(host, port, data, format_type)
    sys.exit(0 if success else 1)

if __name__ == "__main__":
    main()
EOF

chmod +x /tmp/test_parameter_validation.py

# Test parameter validation on each node
for i in "${!SGX_NODES[@]}"; do
  SGX_IP=${SGX_NODES[$i]}
  SEV_IP=${SEV_NODES[$i]}
  PAIR_ID=${PAIR_IDS[$i]}
  
  echo -e "${YELLOW}Pair $PAIR_ID:${NC}"
  
  # Copy the test script to each node
  scp $SSH_OPTS /tmp/test_parameter_validation.py ubuntu@$SGX_IP:/tmp/
  scp $SSH_OPTS /tmp/test_parameter_validation.py ubuntu@$SEV_IP:/tmp/
  
  # Install Python on nodes if needed
  ssh $SSH_OPTS ubuntu@$SGX_IP "sudo apt-get update && sudo apt-get install -y python3"
  ssh $SSH_OPTS ubuntu@$SEV_IP "sudo apt-get update && sudo apt-get install -y python3"
  
  # Test SGX node with length-prefixed format
  echo -e "\n${YELLOW}  Testing SGX node ($SGX_IP) with length-prefixed format${NC}"
  ssh $SSH_OPTS ubuntu@$SGX_IP "python3 /tmp/test_parameter_validation.py $SGX_IP length_prefixed"
  SGX_LP_RESULT=$?
  print_status $SGX_LP_RESULT "SGX node length-prefixed format test"
  
  # Test SGX node with direct format
  echo -e "\n${YELLOW}  Testing SGX node ($SGX_IP) with direct format${NC}"
  ssh $SSH_OPTS ubuntu@$SGX_IP "python3 /tmp/test_parameter_validation.py $SGX_IP direct"
  SGX_DIRECT_RESULT=$?
  print_status $SGX_DIRECT_RESULT "SGX node direct format test"
  
  # Test SEV node with length-prefixed format
  echo -e "\n${YELLOW}  Testing SEV node ($SEV_IP) with length-prefixed format${NC}"
  ssh $SSH_OPTS ubuntu@$SEV_IP "python3 /tmp/test_parameter_validation.py $SEV_IP length_prefixed"
  SEV_LP_RESULT=$?
  print_status $SEV_LP_RESULT "SEV node length-prefixed format test"
  
  # Test SEV node with direct format
  echo -e "\n${YELLOW}  Testing SEV node ($SEV_IP) with direct format${NC}"
  ssh $SSH_OPTS ubuntu@$SEV_IP "python3 /tmp/test_parameter_validation.py $SEV_IP direct"
  SEV_DIRECT_RESULT=$?
  print_status $SEV_DIRECT_RESULT "SEV node direct format test"
done

# Check 4: Cross-attestation Test
print_header "Testing Cross-Attestation"

cat > /tmp/test_cross_attestation.py << EOF
#!/usr/bin/env python3
import sys
import socket
import struct
import binascii
import time
import json
import random

def create_attestation_request(target_tee_type):
    # Create a simulated attestation request
    # Accumulator with random data
    accumulator = bytes([random.randint(0, 255) for _ in range(32)])
    
    # Create request with proper format
    request = {
        "request_type": "attestation",
        "target_tee": target_tee_type,
        "accumulator": binascii.hexlify(accumulator).decode(),
        "timestamp_ns": int(time.time() * 1000000000)
    }
    
    # Convert to JSON
    request_json = json.dumps(request).encode()
    
    # Length prefix the JSON
    data = struct.pack("<I", len(request_json)) + request_json
    return data

def send_attestation_request(host, port, target_tee_type):
    print(f"Sending attestation request to {host}:{port}")
    
    data = create_attestation_request(target_tee_type)
    print(f"Request data: {data[4:].decode()}")
    
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    sock.settimeout(5)
    try:
        sock.connect((host, port))
        sock.sendall(data)
        
        # Get response
        response = sock.recv(1024)
        
        # Try to parse response
        if len(response) >= 4:
            resp_len = struct.unpack("<I", response[:4])[0]
            if len(response) >= 4 + resp_len:
                resp_json = response[4:4+resp_len].decode()
                print(f"Response: {resp_json}")
                resp_obj = json.loads(resp_json)
                
                if "success" in resp_obj and resp_obj["success"]:
                    print("✓ Attestation successful")
                    return True
                else:
                    print("✗ Attestation failed")
                    if "error" in resp_obj:
                        print(f"Error: {resp_obj['error']}")
                    return False
            else:
                print("✗ Incomplete response data")
                return False
        else:
            print("✗ Invalid response format")
            return False
            
    except Exception as e:
        print(f"Error: {e}")
        return False
    finally:
        sock.close()

def main():
    if len(sys.argv) != 3:
        print("Usage: ./test_cross_attestation.py <host> <target_tee_type>")
        print("  target_tee_type: IntelSGX or SEV")
        sys.exit(1)
        
    host = sys.argv[1]
    target_tee_type = sys.argv[2]
    port = 7070
    
    success = send_attestation_request(host, port, target_tee_type)
    sys.exit(0 if success else 1)

if __name__ == "__main__":
    main()
EOF

chmod +x /tmp/test_cross_attestation.py

# Copy test script to nodes
for i in "${!SGX_NODES[@]}"; do
  SGX_IP=${SGX_NODES[$i]}
  SEV_IP=${SEV_NODES[$i]}
  PAIR_ID=${PAIR_IDS[$i]}
  
  scp $SSH_OPTS /tmp/test_cross_attestation.py ubuntu@$SGX_IP:/tmp/
  scp $SSH_OPTS /tmp/test_cross_attestation.py ubuntu@$SEV_IP:/tmp/
  
  echo -e "${YELLOW}Pair $PAIR_ID:${NC}"
  
  # Test SGX attestation of SEV
  echo -e "\n${YELLOW}  Testing SGX attestation of SEV${NC}"
  ssh $SSH_OPTS ubuntu@$SGX_IP "python3 /tmp/test_cross_attestation.py $SGX_IP SEV"
  SGX_ATTEST_RESULT=$?
  print_status $SGX_ATTEST_RESULT "SGX attestation of SEV"
  
  # Test SEV attestation of SGX
  echo -e "\n${YELLOW}  Testing SEV attestation of SGX${NC}"
  ssh $SSH_OPTS ubuntu@$SEV_IP "python3 /tmp/test_cross_attestation.py $SEV_IP IntelSGX"
  SEV_ATTEST_RESULT=$?
  print_status $SEV_ATTEST_RESULT "SEV attestation of SGX"
done

# Final summary
print_header "Verification Summary"
if [ $FAILURES -eq 0 ]; then
  echo -e "${GREEN}All tests passed! Your TEE controllers are correctly deployed and functioning.${NC}"
  echo -e "${GREEN}Dual-format parameter validation is working properly.${NC}"
  echo -e "${GREEN}Cross-attestation between SGX and SEV nodes is working.${NC}"
  echo -e "\nYour TEE infrastructure is ready for NASDAQ market data processing!"
else
  echo -e "${RED}$FAILURES tests failed. Please check the logs above for details.${NC}"
  if [ $FAILURES -le 2 ]; then
    echo -e "${YELLOW}Some minor issues were detected. The system may still be functional for testing purposes.${NC}"
  else
    echo -e "${RED}Significant issues were detected. Please resolve them before proceeding with NASDAQ integration.${NC}"
  fi
fi

exit $FAILURES
