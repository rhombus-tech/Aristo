#!/bin/bash
# Restart validator service on all TEE nodes
# Ensures proper dual-format parameter validation for NASDAQ integration

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

# Get instance data directly using AWS CLI
echo -e "${YELLOW}Looking for running EC2 instances...${NC}"

# Get instance IDs and IPs in a simple format
INSTANCE_DATA=$(aws ec2 describe-instances --filters "Name=instance-state-name,Values=running" --query "Reservations[*].Instances[*].[InstanceId,PublicIpAddress]" --output text)

# Parse the INSTANCE_DATA into separate arrays
INSTANCE_IDS=()
INSTANCE_IPS=()

while read -r ID IP; do
    INSTANCE_IDS+=("$ID")
    INSTANCE_IPS+=("$IP")
done <<< "$INSTANCE_DATA"

# Count number of running instances
INSTANCE_COUNT=${#INSTANCE_IDS[@]}

echo -e "${GREEN}Found $INSTANCE_COUNT running instances.${NC}"

# Display found instances
for ((i=0; i<INSTANCE_COUNT; i++)); do
    echo -e "${YELLOW}Instance ${INSTANCE_IDS[$i]} has IP ${INSTANCE_IPS[$i]}${NC}"
done

# Function to restart the validator on a node
restart_validator() {
    local IP=$1
    local ID=$2
    
    echo -e "${GREEN}Restarting validator on instance $ID ($IP)...${NC}"
    
    # Kill any existing validator processes
    ssh $SSH_OPTS ubuntu@$IP "pkill -f rsa_enhanced_validator.py || true"
    
    # Find an available port (try ports 7090-7099)
    local AVAILABLE_PORT=""
    for PORT in {7090..7099}; do
        # Check if port is in use
        if ! ssh $SSH_OPTS ubuntu@$IP "netstat -tuln | grep :$PORT"; then
            AVAILABLE_PORT=$PORT
            break
        fi
    done
    
    if [ -z "$AVAILABLE_PORT" ]; then
        echo -e "${RED}No available ports found on instance $ID ($IP)${NC}"
        return 1
    fi
    
    echo -e "${GREEN}Using port $AVAILABLE_PORT on instance $ID ($IP)${NC}"
    
    # Update the config file to use the available port
    ssh $SSH_OPTS ubuntu@$IP "cd ~/tee_validator && sed -i 's/\"listen_address\": \"0.0.0.0:[0-9]*\"/\"listen_address\": \"0.0.0.0:$AVAILABLE_PORT\"/' node_*.json"
    
    # Start the validator
    ssh $SSH_OPTS ubuntu@$IP "cd ~/tee_validator && nohup python3 rsa_enhanced_validator.py --config \$(ls node_*.json) > validator.log 2>&1 &"
    ssh $SSH_OPTS ubuntu@$IP "cd ~/tee_validator && echo \$! > validator.pid"
    
    # Verify the validator is running
    sleep 2
    local PID=$(ssh $SSH_OPTS ubuntu@$IP "cat ~/tee_validator/validator.pid 2>/dev/null || echo ''")
    if [ -n "$PID" ]; then
        local RUNNING=$(ssh $SSH_OPTS ubuntu@$IP "ps -p $PID -o comm= 2>/dev/null || echo ''")
        if [ -n "$RUNNING" ]; then
            echo -e "${GREEN}Validator is running on instance $ID ($IP) with PID $PID on port $AVAILABLE_PORT${NC}"
            ssh $SSH_OPTS ubuntu@$IP "tail -n 10 ~/tee_validator/validator.log"
            return 0
        fi
    fi
    
    echo -e "${RED}Failed to start validator on instance $ID ($IP)${NC}"
    ssh $SSH_OPTS ubuntu@$IP "cat ~/tee_validator/validator.log | tail -n 20"
    return 1
}

# Restart validator on all instances
for ((i=0; i<INSTANCE_COUNT; i++)); do
    restart_validator "${INSTANCE_IPS[$i]}" "${INSTANCE_IDS[$i]}" || true
done

echo -e "${GREEN}=== Validator Restart Complete ===${NC}"

# Print test commands for different instance pairs
echo -e "${YELLOW}To test the validator, use the following commands:${NC}"

# Find SGX and SEV nodes based on assigned tags
SGX_IPS=()
SEV_IPS=()
SGX_PORTS=()
SEV_PORTS=()

for ((i=0; i<INSTANCE_COUNT; i++)); do
    IP="${INSTANCE_IPS[$i]}"
    ID="${INSTANCE_IDS[$i]}"
    
    # Get the assigned TEE type from the instance tag
    TEE_TYPE=$(aws ec2 describe-tags --filters "Name=resource-id,Values=$ID" "Name=key,Values=TEEType" --query "Tags[0].Value" --output text 2>/dev/null || echo "")
    
    # Get the port from the config file
    PORT=$(ssh $SSH_OPTS ubuntu@$IP "grep -o '\"listen_address\": \"0.0.0.0:[0-9]*\"' ~/tee_validator/node_*.json 2>/dev/null | grep -o '[0-9]*'" 2>/dev/null || echo "")
    
    if [ "$TEE_TYPE" == "SGX" ]; then
        SGX_IPS+=("$IP")
        SGX_PORTS+=("$PORT")
    elif [ "$TEE_TYPE" == "SEV" ]; then
        SEV_IPS+=("$IP")
        SEV_PORTS+=("$PORT")
    fi
done

# Generate test commands for each SGX-SEV pair
for ((i=0; i<${#SGX_IPS[@]} && i<${#SEV_IPS[@]}; i++)); do
    echo -e "${GREEN}Pair $((i+1)): Enhanced dual-format parameter validation test:${NC}"
    echo -e "python3 enhanced_validator_test.py --sgx-host ${SGX_IPS[$i]} --sgx-port ${SGX_PORTS[$i]} --sev-host ${SEV_IPS[$i]} --sev-port ${SEV_PORTS[$i]} --test all"
    
    echo -e "${GREEN}Pair $((i+1)): NASDAQ market data simulation:${NC}"
    echo -e "cd ../integration/nasdaq && python3 connector/real_tee_perf.py --sgx-host ${SGX_IPS[$i]} --sgx-port ${SGX_PORTS[$i]} --sev-host ${SEV_IPS[$i]} --sev-port ${SEV_PORTS[$i]} --message-count 10000 --batch-size 1000 --enable-attestation"
done
