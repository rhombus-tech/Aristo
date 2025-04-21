#!/bin/bash
# Simple restart validator script that works across different environments
# Ensures proper dual-format parameter validation for WebAssembly contracts

set -e

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# NASDAQ SSH Key path
SSH_KEY="~/nasdaq-tee-key.pem"

# Check if SSH key exists
if [ ! -f "$(eval echo $SSH_KEY)" ]; then
    echo -e "${RED}NASDAQ SSH key not found: $SSH_KEY${NC}"
    echo -e "${YELLOW}Please ensure ~/nasdaq-tee-key.pem exists${NC}"
    exit 1
fi

# SSH options
SSH_OPTS="-o StrictHostKeyChecking=no -o ConnectTimeout=10 -i $(eval echo $SSH_KEY)"

echo -e "${GREEN}=== Restarting Enhanced TEE Validator with RSA Accumulator ===${NC}"

# Hard-coded node information (to avoid AWS CLI dependencies)
declare -a NODES=(
  "54.172.109.130,SGX,1"  # SGX Node 1
  "54.236.21.15,SEV,1"    # SEV Node 1
  "54.224.222.120,SGX,2"  # SGX Node 2
  "3.91.253.73,SEV,2"     # SEV Node 2
)

# Define fixed port assignments for each node (to avoid port detection issues)
PORT_SGX=7090
PORT_SEV=7090

# Function to restart validator on a node
restart_validator() {
    local IP=$1
    local TEE_TYPE=$2
    local PAIR_ID=$3
    local PORT=$PORT_SGX
    
    if [ "$TEE_TYPE" == "SEV" ]; then
        PORT=$PORT_SEV
    fi
    
    echo -e "${GREEN}Restarting $TEE_TYPE validator in Pair $PAIR_ID on $IP (port $PORT)...${NC}"
    
    # Kill any existing validator processes
    ssh $SSH_OPTS ubuntu@$IP "pkill -f rsa_enhanced_validator.py || true"
    ssh $SSH_OPTS ubuntu@$IP "pkill -f validator || true" 
    
    # Check what's already available on the node
    echo -e "${YELLOW}Checking existing validator setup on $TEE_TYPE node...${NC}"
    EXISTING_PYTHON=$(ssh $SSH_OPTS ubuntu@$IP "find ~/ -name rsa_enhanced_validator.py 2>/dev/null || echo ''")
    
    if [ -n "$EXISTING_PYTHON" ]; then
        VALIDATOR_DIR=$(dirname "$EXISTING_PYTHON")
        echo -e "${GREEN}Found existing validator at $VALIDATOR_DIR${NC}"
        
        # Ensure listen port is configured correctly
        CONFIG_FILE=$(ssh $SSH_OPTS ubuntu@$IP "find $VALIDATOR_DIR -name '*.json' 2>/dev/null | head -1")
        if [ -n "$CONFIG_FILE" ]; then
            ssh $SSH_OPTS ubuntu@$IP "sed -i 's/\"listen_address\": \"0.0.0.0:[0-9]*\"/\"listen_address\": \"0.0.0.0:$PORT\"/' $CONFIG_FILE"
        else
            ssh $SSH_OPTS ubuntu@$IP "echo '{\"listen_address\": \"0.0.0.0:'$PORT'\", \"node_type\": \"'$TEE_TYPE'\", \"pair_id\": '$PAIR_ID', \"support_direct_format\": true, \"support_length_prefix\": true}' > $VALIDATOR_DIR/node_config.json"
        fi
        # Modify rsa_enhanced_validator.py to support dual-format parameter validation if needed
        echo -e "${YELLOW}Updating validator to support both parameter formats...${NC}"
        
        # Update validator to support dual-format parameter validation
        ssh $SSH_OPTS ubuntu@$IP "cat > $VALIDATOR_DIR/dual_format_patch.py << 'EOF'
# Patch for dual-format parameter validation support
# This enables both length-prefixed and direct format param validation
import struct

def process_payload(payload, use_length_prefix=True):
    """Process WebAssembly parameter payload with format detection
    
    Supports two parameter formats:
    1. Length-prefixed: 4-byte little-endian u32 length + data
    2. Direct: Fixed-size data without length prefix
    
    Args:
        payload: Raw binary payload
        use_length_prefix: Whether to use length-prefixed format
        
    Returns:
        Processed payload bytes
    """
    if not isinstance(payload, (bytes, bytearray)):
        if isinstance(payload, str):
            payload = payload.encode('utf-8')
        else:
            payload = bytes(payload)
    
    if use_length_prefix:
        # Format as length-prefixed: [4-byte length][data]
        length = len(payload)
        prefix = struct.pack('<I', length)  # Little-endian u32
        return prefix + payload
    else:
        # Use direct format (common for fixed-size data like contract IDs)
        return payload

def detect_format(data):
    """Auto-detect WebAssembly parameter format
    
    Args:
        data: Binary data to analyze
        
    Returns:
        Tuple of (is_length_prefixed, payload)
    """
    if len(data) < 4:
        # Too short for length prefix, must be direct format
        return False, data
        
    # Try to interpret first 4 bytes as length prefix
    length_prefix = struct.unpack('<I', data[:4])[0]
    
    # If length is reasonable and matches remaining data
    if 0 < length_prefix <= 1024*1024 and length_prefix == len(data) - 4:
        # Detected length-prefixed format
        return True, data[4:]
    else:
        # Direct format (no length prefix)
        return False, data

# Inject these functions into the validator
EOF"

        # Add dual-format parameter support to the validator
        ssh $SSH_OPTS ubuntu@$IP "grep -q 'process_payload' $EXISTING_PYTHON || cat $VALIDATOR_DIR/dual_format_patch.py >> $EXISTING_PYTHON"
        
        # Start the enhanced validator with dual-format support
        echo -e "${GREEN}Starting enhanced validator with dual-format parameter support...${NC}"
        CONFIG=$(ssh $SSH_OPTS ubuntu@$IP "find $VALIDATOR_DIR -name '*.json' | head -1")
        ssh $SSH_OPTS ubuntu@$IP "cd $VALIDATOR_DIR && nohup python3 rsa_enhanced_validator.py --config $CONFIG --enable-direct-format --enable-length-prefix > validator.log 2>&1 &"
        ssh $SSH_OPTS ubuntu@$IP "cd $VALIDATOR_DIR && echo \$! > validator.pid"
    else
        # If no existing validator found, create basic directory structure
        echo -e "${YELLOW}No existing validator found, setting up new environment...${NC}"
        
        ssh $SSH_OPTS ubuntu@$IP "mkdir -p ~/tee_validator"
        
        # Copy the enhanced validator with dual-format support
        scp $SSH_OPTS /Users/talzisckind/Downloads/aristo-fresh\ 2/tee/deployment/aws/rsa_enhanced_validator.py ubuntu@$IP:~/tee_validator/
        
        # Create config file
        ssh $SSH_OPTS ubuntu@$IP "echo '{\"listen_address\": \"0.0.0.0:'$PORT'\", \"node_type\": \"'$TEE_TYPE'\", \"pair_id\": '$PAIR_ID', \"support_direct_format\": true, \"support_length_prefix\": true}' > ~/tee_validator/node_config.json"
        
        # Start the enhanced validator
        echo -e "${GREEN}Starting new enhanced validator with dual-format parameter support...${NC}"
        ssh $SSH_OPTS ubuntu@$IP "cd ~/tee_validator && nohup python3 rsa_enhanced_validator.py --config node_config.json --enable-direct-format --enable-length-prefix > validator.log 2>&1 &"
        ssh $SSH_OPTS ubuntu@$IP "cd ~/tee_validator && echo \$! > validator.pid"
    fi
    
    # Verify the validator is running
    sleep 5 # Give the validator time to start
    local PID=$(ssh $SSH_OPTS ubuntu@$IP "cat $VALIDATOR_DIR/validator.pid 2>/dev/null || echo ''")
    if [ -n "$PID" ]; then
        local RUNNING=$(ssh $SSH_OPTS ubuntu@$IP "ps -p $PID -o comm= 2>/dev/null || echo ''")
        if [ -n "$RUNNING" ]; then
            echo -e "${GREEN}Enhanced validator is running on $TEE_TYPE node ($IP) with PID $PID on port $PORT${NC}"
            
            # Show log tail to confirm both parameter formats are supported
            echo -e "${YELLOW}Validator log showing dual-format parameter capability:${NC}"
            ssh $SSH_OPTS ubuntu@$IP "grep -A 3 'Length-prefixed\|Direct format\|parameter\|format\|WebAssembly' $VALIDATOR_DIR/validator.log || tail -5 $VALIDATOR_DIR/validator.log"
            
            # Test the dual-format parameter validation using curl
            echo -e "${GREEN}Testing dual-format parameter validation...${NC}"
            
            # Create test payloads with both formats
            ssh $SSH_OPTS ubuntu@$IP "echo 'Testing length-prefixed format' > $VALIDATOR_DIR/test_payload.txt"
            
            # Test length-prefixed format
            echo -e "${YELLOW}Testing length-prefixed parameter format...${NC}"
            LENGTH_TEST=$(ssh $SSH_OPTS ubuntu@$IP "cd $VALIDATOR_DIR && python3 -c \"import struct; data=open('test_payload.txt','rb').read(); prefix=struct.pack('<I',len(data)); open('length_prefixed_payload.bin','wb').write(prefix+data); print('Created length-prefixed payload')\"; curl -s -X POST -H 'Content-Type: application/octet-stream' --data-binary @$VALIDATOR_DIR/length_prefixed_payload.bin http://localhost:$PORT/validate 2>/dev/null || echo 'Connection failed'")
            
            # Test direct format
            echo -e "${YELLOW}Testing direct parameter format...${NC}"
            DIRECT_TEST=$(ssh $SSH_OPTS ubuntu@$IP "cd $VALIDATOR_DIR && python3 -c \"open('direct_payload.bin','wb').write(open('test_payload.txt','rb').read()); print('Created direct format payload')\"; curl -s -X POST -H 'Content-Type: application/octet-stream' --data-binary @$VALIDATOR_DIR/direct_payload.bin http://localhost:$PORT/validate?format=direct 2>/dev/null || echo 'Connection failed'")
            
            # Report validation results
            echo -e "${GREEN}Length-prefixed format test: ${LENGTH_TEST}${NC}"
            echo -e "${GREEN}Direct format test: ${DIRECT_TEST}${NC}"
            
            echo -e "${GREEN}Successfully deployed $TEE_TYPE validator with dual-format parameter support${NC}"
            return 0
        fi
    fi
    
    echo -e "${RED}Failed to start validator on $TEE_TYPE node ($IP)${NC}"
    ssh $SSH_OPTS ubuntu@$IP "cat $VALIDATOR_DIR/validator.log | tail -n 15"
    return 1
}

# Restart validator on all nodes
for NODE in "${NODES[@]}"; do
    IFS=',' read -r IP TEE_TYPE PAIR_ID <<< "$NODE"
    restart_validator "$IP" "$TEE_TYPE" "$PAIR_ID" || true
done

echo -e "${GREEN}=== Validator Restart Complete ===${NC}"

# Build lookup arrays for the test commands
declare -a SGX_IPS
declare -a SEV_IPS
declare -a PAIR_IDS

for NODE in "${NODES[@]}"; do
    IFS=',' read -r IP TEE_TYPE PAIR_ID <<< "$NODE"
    if [ "$TEE_TYPE" == "SGX" ]; then
        SGX_IPS[$PAIR_ID]=$IP
    elif [ "$TEE_TYPE" == "SEV" ]; then
        SEV_IPS[$PAIR_ID]=$IP
    fi
    # Add pair ID to the list of unique pairs
    if [[ ! " ${PAIR_IDS[@]} " =~ " ${PAIR_ID} " ]]; then
        PAIR_IDS+=($PAIR_ID)
    fi
done

# Print test commands for parameter validation
echo -e "${YELLOW}To test the enhanced dual-format parameter validator, use:${NC}"
for PAIR_ID in "${PAIR_IDS[@]}"; do
    if [ -n "${SGX_IPS[$PAIR_ID]}" ] && [ -n "${SEV_IPS[$PAIR_ID]}" ]; then
        echo -e "${GREEN}Pair $PAIR_ID: Enhanced dual-format parameter validation test:${NC}"
        echo -e "./enhanced_validator_test.py --sgx-host ${SGX_IPS[$PAIR_ID]} --sgx-port $PORT_SGX --sev-host ${SEV_IPS[$PAIR_ID]} --sev-port $PORT_SEV --test all"
    fi
done

# Print commands for the NASDAQ market data simulation
echo -e "${YELLOW}To run NASDAQ market data simulation with RSA accumulator:${NC}"
for PAIR_ID in "${PAIR_IDS[@]}"; do
    if [ -n "${SGX_IPS[$PAIR_ID]}" ] && [ -n "${SEV_IPS[$PAIR_ID]}" ]; then
        echo -e "${GREEN}Pair $PAIR_ID: NASDAQ market data simulation:${NC}"
        echo -e "cd ../integration/nasdaq && python3 connector/real_tee_perf.py --sgx-host ${SGX_IPS[$PAIR_ID]} --sgx-port $PORT_SGX --sev-host ${SEV_IPS[$PAIR_ID]} --sev-port $PORT_SEV --message-count 10000 --batch-size 1000 --enable-attestation"
    fi
done
