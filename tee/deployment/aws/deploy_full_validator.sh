#!/bin/bash
# Full deployment of enhanced TEE validator with RSA accumulator integration to paired nodes

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
SCP_OPTS="-o StrictHostKeyChecking=no -o ConnectTimeout=10 -i $(eval echo $SSH_KEY)"

echo -e "${GREEN}=== Full Deployment of Enhanced TEE Validator with RSA Accumulator ===${NC}"

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

# Make sure we have at least 2 instances for pairing
if [ $INSTANCE_COUNT -lt 2 ]; then
    echo -e "${RED}Not enough running instances for pairing. Need at least 2 instances.${NC}"
    exit 1
fi

# Display found instances
for ((i=0; i<INSTANCE_COUNT; i++)); do
    echo -e "${YELLOW}Instance ${INSTANCE_IDS[$i]} has IP ${INSTANCE_IPS[$i]}${NC}"
done

# Assigning instances as pairs
PAIR_COUNT=$(( (INSTANCE_COUNT + 1) / 2 ))
echo -e "${GREEN}Creating $PAIR_COUNT TEE pairs${NC}"

# Create TEE type assignments
echo -e "${YELLOW}Assigning TEE types to instances...${NC}"

# Create temp directory for configs
TEMP_DIR="tee_validator_dist"
mkdir -p $TEMP_DIR

# Generate assignments file
cat > $TEMP_DIR/tee_assignments.txt <<EOL
# TEE assignments for validator deployment
# Format: INSTANCE_ID,PUBLIC_IP,TEE_TYPE,PAIR_ID
EOL

# Assign TEE types (even indices as SGX, odd indices as SEV)
for ((i=0; i<INSTANCE_COUNT; i++)); do
    INSTANCE_ID=${INSTANCE_IDS[$i]}
    PUBLIC_IP=${INSTANCE_IPS[$i]}
    PAIR_ID=$(( i/2 + 1 ))
    
    if [ $((i % 2)) -eq 0 ]; then
        TEE_TYPE="SGX"
    else
        TEE_TYPE="SEV"
    fi
    
    echo "$INSTANCE_ID,$PUBLIC_IP,$TEE_TYPE,$PAIR_ID" >> $TEMP_DIR/tee_assignments.txt
    echo -e "${GREEN}Assigned $INSTANCE_ID (${PUBLIC_IP}) as $TEE_TYPE in Pair $PAIR_ID${NC}"
    
    # Tag the instance
    aws ec2 create-tags --resources $INSTANCE_ID --tags Key=TEEType,Value=$TEE_TYPE Key=PairId,Value=$PAIR_ID
done

# Copy validator script
cp rsa_enhanced_validator.py $TEMP_DIR/

# Create configurations for each node
echo -e "${GREEN}Creating node configurations...${NC}"

# Read assignments into arrays to make pairing easier
ASSIGN_IDS=()
ASSIGN_IPS=()
ASSIGN_TYPES=()
ASSIGN_PAIRS=()

while IFS=, read -r ID IP TYPE PAIR_ID; do
    # Skip comment lines
    [[ "$ID" =~ ^#.* ]] && continue
    
    ASSIGN_IDS+=("$ID")
    ASSIGN_IPS+=("$IP")
    ASSIGN_TYPES+=("$TYPE")
    ASSIGN_PAIRS+=("$PAIR_ID")
done < $TEMP_DIR/tee_assignments.txt

# Create pair configs
for ((i=0; i<${#ASSIGN_IDS[@]}; i++)); do
    ID="${ASSIGN_IDS[$i]}"
    IP="${ASSIGN_IPS[$i]}"
    TYPE="${ASSIGN_TYPES[$i]}"
    PAIR="${ASSIGN_PAIRS[$i]}"
    
    # Find partner in same pair
    PARTNER_IP=""
    PARTNER_TYPE=""
    
    for ((j=0; j<${#ASSIGN_IDS[@]}; j++)); do
        if [ $i -ne $j ] && [ "${ASSIGN_PAIRS[$j]}" == "$PAIR" ]; then
            PARTNER_IP="${ASSIGN_IPS[$j]}"
            PARTNER_TYPE="${ASSIGN_TYPES[$j]}"
            break
        fi
    done
    
    # If no partner found, use self as partner
    if [ -z "$PARTNER_IP" ]; then
        PARTNER_IP="$IP"
        PARTNER_TYPE="$TYPE"
        echo -e "${YELLOW}Warning: No partner found for $ID ($TYPE). Using self as partner.${NC}"
    fi
    
    # Create node config
    CONFIG_FILE="$TEMP_DIR/node_${ID}_config.json"
    
    # Convert type to lowercase for node_id
    TYPE_LOWER=$(echo "$TYPE" | tr '[:upper:]' '[:lower:]')
    PARTNER_TYPE_LOWER=$(echo "$PARTNER_TYPE" | tr '[:upper:]' '[:lower:]')
    
    cat > "$CONFIG_FILE" <<EOL
{
  "tee_type": "$TYPE",
  "node_id": "${TYPE_LOWER}-node-$PAIR",
  "parameter_validation": {
    "length_prefixed": true,
    "direct_format": true,
    "max_size": 1024,
    "format_detection": true
  },
  "partner_tee": {
    "tee_type": "$PARTNER_TYPE",
    "node_id": "${PARTNER_TYPE_LOWER}-node-$PAIR",
    "ip": "$PARTNER_IP"
  },
  "accumulator": {
    "size_bytes": 32,
    "batch_size": 1000,
    "threads": 8
  },
  "listen_address": "0.0.0.0:7070"
}
EOL
    echo -e "Created config for $TYPE node $ID with partner $PARTNER_TYPE"
done

# Create node setup script
cat > $TEMP_DIR/setup_node.sh <<EOL
#!/bin/bash
# Setup script for TEE validator node

set -e

# Install dependencies
sudo apt-get update
sudo apt-get install -y python3 python3-pip
pip3 install --user typing

# Get instance metadata
INSTANCE_ID=\$(curl -s http://169.254.169.254/latest/meta-data/instance-id)

# Find our config file
CONFIG_FILE="node_\${INSTANCE_ID}_config.json"
if [ ! -f "\$CONFIG_FILE" ]; then
    echo "ERROR: Config file not found: \$CONFIG_FILE"
    exit 1
fi

# Make validator executable
chmod +x rsa_enhanced_validator.py

# Kill any previous validator process
if [ -f validator.pid ]; then
    OLD_PID=\$(cat validator.pid)
    if ps -p \$OLD_PID > /dev/null; then
        echo "Stopping previous validator process (\$OLD_PID)"
        kill \$OLD_PID || true
    fi
fi

# Start validator
echo "Starting validator with config \$CONFIG_FILE"
nohup python3 rsa_enhanced_validator.py --config \$CONFIG_FILE > validator.log 2>&1 &
echo \$! > validator.pid

echo "Validator started with PID \$(cat validator.pid)"
echo "Log file: validator.log"

# Create status file for web access
mkdir -p ~/public_html
cat > ~/public_html/status.json <<EOF
{
  "status": "running",
  "timestamp": "\$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
  "instance_id": "\$INSTANCE_ID",
  "tee_type": "\$(jq -r .tee_type \$CONFIG_FILE)",
  "node_id": "\$(jq -r .node_id \$CONFIG_FILE)",
  "partner_type": "\$(jq -r .partner_tee.tee_type \$CONFIG_FILE)",
  "partner_id": "\$(jq -r .partner_tee.node_id \$CONFIG_FILE)",
  "accumulator": {
    "size_bytes": \$(jq -r .accumulator.size_bytes \$CONFIG_FILE),
    "batch_size": \$(jq -r .accumulator.batch_size \$CONFIG_FILE)
  }
}
EOF

# Set up basic health check web server if apache2 is installed
if command -v apache2 &> /dev/null; then
    sudo mkdir -p /var/www/html
    sudo cp ~/public_html/status.json /var/www/html/
    sudo chown -R www-data:www-data /var/www/html
fi

echo "Node setup complete"
EOL

chmod +x $TEMP_DIR/setup_node.sh

# Deploy to each node
echo -e "${GREEN}Deploying to all nodes...${NC}"

for ((i=0; i<${#ASSIGN_IDS[@]}; i++)); do
    ID="${ASSIGN_IDS[$i]}"
    IP="${ASSIGN_IPS[$i]}"
    TYPE="${ASSIGN_TYPES[$i]}"
    PAIR="${ASSIGN_PAIRS[$i]}"
    
    echo -e "${YELLOW}Deploying to $TYPE node $ID ($IP) in Pair $PAIR${NC}"
    
    # Create remote directory
    ssh $SSH_OPTS ubuntu@$IP "mkdir -p ~/tee_validator"
    
    # Copy files
    scp $SCP_OPTS $TEMP_DIR/rsa_enhanced_validator.py $TEMP_DIR/node_${ID}_config.json $TEMP_DIR/setup_node.sh ubuntu@$IP:~/tee_validator/
    
    # Run setup script
    ssh $SSH_OPTS ubuntu@$IP "cd ~/tee_validator && bash setup_node.sh"
    
    echo -e "${GREEN}Deployment complete for $TYPE node $ID${NC}"
done

# Clean up
rm -rf $TEMP_DIR

# Display test commands
echo -e "${GREEN}=== Deployment Complete ===${NC}"
echo -e "${YELLOW}To test cross-attestation, use the enhanced_validator_test.py script with these pairs:${NC}"

# Find unique pairs
UNIQUE_PAIRS=($(for p in "${ASSIGN_PAIRS[@]}"; do echo "$p"; done | sort -u))

for PAIR in "${UNIQUE_PAIRS[@]}"; do
    SGX_IP=""
    SEV_IP=""
    
    for ((i=0; i<${#ASSIGN_IDS[@]}; i++)); do
        if [ "${ASSIGN_PAIRS[$i]}" == "$PAIR" ]; then
            if [ "${ASSIGN_TYPES[$i]}" == "SGX" ]; then
                SGX_IP="${ASSIGN_IPS[$i]}"
            elif [ "${ASSIGN_TYPES[$i]}" == "SEV" ]; then
                SEV_IP="${ASSIGN_IPS[$i]}"
            fi
        fi
    done
    
    if [ -n "$SGX_IP" ] && [ -n "$SEV_IP" ]; then
        echo -e "${GREEN}Pair $PAIR:${NC} ./enhanced_validator_test.py --sgx-host $SGX_IP --sev-host $SEV_IP --test all"
    elif [ -n "$SGX_IP" ]; then
        echo -e "${YELLOW}Pair $PAIR:${NC} ./enhanced_validator_test.py --sgx-host $SGX_IP --test length-prefixed,direct,overflow,performance"
    elif [ -n "$SEV_IP" ]; then
        echo -e "${YELLOW}Pair $PAIR:${NC} ./enhanced_validator_test.py --sev-host $SEV_IP --test length-prefixed,direct,overflow,performance"
    fi
done
