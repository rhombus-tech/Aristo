#!/bin/bash
# Deploy enhanced TEE validator with RSA accumulator integration to all nodes as pairs

set -e

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== Deploying Enhanced TEE Validator with RSA Accumulator to Paired Nodes ===${NC}"

# Get instance data directly from AWS using a simpler format
echo -e "${YELLOW}Looking for running EC2 instances...${NC}"

# Get instance IDs of running instances
INSTANCE_IDS=($(aws ec2 describe-instances --filters "Name=instance-state-name,Values=running" --query "Reservations[*].Instances[*].InstanceId" --output text))

# Count number of running instances
INSTANCE_COUNT=${#INSTANCE_IDS[@]}

echo -e "${YELLOW}Found $INSTANCE_COUNT running instances.${NC}"

# Make sure we have at least 2 instances for pairing
if [ $INSTANCE_COUNT -lt 2 ]; then
    echo -e "${RED}Not enough running instances for pairing. Need at least 2 instances.${NC}"
    exit 1
fi

# Get the IP addresses for each instance
declare -A INSTANCE_IPS

for ID in "${INSTANCE_IDS[@]}"; do
    IP=$(aws ec2 describe-instances --instance-ids "$ID" --query "Reservations[0].Instances[0].PublicIpAddress" --output text)
    INSTANCE_IPS[$ID]=$IP
    echo -e "Instance $ID has IP ${INSTANCE_IPS[$ID]}"
done

echo -e "${YELLOW}Found $INSTANCE_COUNT instances.${NC}"

# Assigning instances as pairs (alternating SGX and SEV)
PAIR_COUNT=$((INSTANCE_COUNT / 2))
echo -e "${GREEN}Creating $PAIR_COUNT TEE pairs${NC}"

# Create TEE type assignment file
cat > tee_assignments.txt <<EOL
# TEE assignments for validator deployment
# Format: INSTANCE_ID,PUBLIC_IP,TEE_TYPE,PAIR_ID
EOL

# Assign TEE types (even indices as SGX, odd indices as SEV)
PAIR_ID=1
for ((i=0; i<INSTANCE_COUNT; i+=2)); do
    # Get SGX instance (even index)
    SGX_ID=${INSTANCE_IDS[$i]}
    SGX_IP=${INSTANCE_IPS[$SGX_ID]}
    
    # Get SEV instance (odd index)
    if [ $((i+1)) -lt $INSTANCE_COUNT ]; then
        SEV_ID=${INSTANCE_IDS[$((i+1))]}
        SEV_IP=${INSTANCE_IPS[$SEV_ID]}
        
        # Add both to assignments
        echo "$SGX_ID,$SGX_IP,SGX,$PAIR_ID" >> tee_assignments.txt
        echo "$SEV_ID,$SEV_IP,SEV,$PAIR_ID" >> tee_assignments.txt
        
        echo -e "${YELLOW}Pair $PAIR_ID: SGX ($SGX_IP) and SEV ($SEV_IP)${NC}"
        
        PAIR_ID=$((PAIR_ID+1))
    else
        # Odd number of instances, last one becomes SGX without a pair
        echo "$SGX_ID,$SGX_IP,SGX,$PAIR_ID" >> tee_assignments.txt
        echo -e "${YELLOW}Pair $PAIR_ID: SGX ($SGX_IP) without partner (odd number of instances)${NC}"
    fi
done

echo -e "${YELLOW}TEE assignments:${NC}"
cat tee_assignments.txt

# Update EC2 instance tags with TEE type
echo -e "${GREEN}Updating EC2 instance tags with TEE types...${NC}"
while IFS=, read -r INSTANCE_ID PUBLIC_IP TEE_TYPE PAIR_ID || [[ -n "$INSTANCE_ID" ]]; do
    # Skip comment lines
    [[ "$INSTANCE_ID" =~ ^#.* ]] && continue
    
    echo -e "Tagging $INSTANCE_ID as $TEE_TYPE (Pair $PAIR_ID)"
    aws ec2 create-tags --resources $INSTANCE_ID --tags Key=TEEType,Value=$TEE_TYPE Key=PairId,Value=$PAIR_ID
done < tee_assignments.txt

# Create validator distribution package
VALIDATOR_FILE="rsa_enhanced_validator.py"
DIST_DIR="tee_validator_dist"

# Ensure validator file exists
if [ ! -f "$VALIDATOR_FILE" ]; then
    echo -e "${RED}Validator file not found: $VALIDATOR_FILE${NC}"
    exit 1
fi

# Prepare distribution
mkdir -p $DIST_DIR
cp $VALIDATOR_FILE $DIST_DIR/

# Loop through pairs and create paired configs
echo -e "${GREEN}Creating paired configurations...${NC}"
declare -a PAIRS
while IFS=, read -r INSTANCE_ID PUBLIC_IP TEE_TYPE PAIR_ID || [[ -n "$INSTANCE_ID" ]]; do
    # Skip comment lines
    [[ "$INSTANCE_ID" =~ ^#.* ]] && continue
    
    # Add to pairs array
    PAIRS+=("$INSTANCE_ID,$PUBLIC_IP,$TEE_TYPE,$PAIR_ID")
done < tee_assignments.txt

# Display the pairs array
echo -e "${YELLOW}Configured instance pairs:${NC}"
for PAIR in "${PAIRS[@]}"; do
    echo "  $PAIR"
done

# Create pair-specific configurations
for ((i=0; i<${#PAIRS[@]}; i++)); do
    IFS=, read -r INSTANCE_ID PUBLIC_IP TEE_TYPE PAIR_ID <<< "${PAIRS[$i]}"
    
    # Find partner in same pair
    PARTNER_IP=""
    PARTNER_TYPE=""
    for ((j=0; j<${#PAIRS[@]}; j++)); do
        if [ $i -ne $j ]; then
            IFS=, read -r P_INSTANCE_ID P_PUBLIC_IP P_TEE_TYPE P_PAIR_ID <<< "${PAIRS[$j]}"
            if [ "$PAIR_ID" == "$P_PAIR_ID" ]; then
                PARTNER_IP=$P_PUBLIC_IP
                PARTNER_TYPE=$P_TEE_TYPE
                break
            fi
        fi
    done
    
    # Create node-specific config
    CONFIG_FILE="$DIST_DIR/${TEE_TYPE,,}_node_${PAIR_ID}_config.json"
    
    cat > $CONFIG_FILE <<EOL
{
  "tee_type": "$TEE_TYPE",
  "node_id": "${TEE_TYPE,,}-node-${PAIR_ID}",
  "parameter_validation": {
    "length_prefixed": true,
    "direct_format": true,
    "max_size": 1024,
    "format_detection": true
  },
  "partner_tee": {
    "tee_type": "$PARTNER_TYPE",
    "node_id": "${PARTNER_TYPE,,}-node-${PAIR_ID}",
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
    echo -e "Created config for ${TEE_TYPE} node $PAIR_ID with partner ${PARTNER_TYPE}"
done

# Create deployment script
DEPLOY_SCRIPT="$DIST_DIR/setup_validator.sh"
cat > $DEPLOY_SCRIPT <<EOL
#!/bin/bash
# Setup script for TEE validator

# Get instance metadata
INSTANCE_ID=\$(curl -s http://169.254.169.254/latest/meta-data/instance-id)
PUBLIC_IP=\$(curl -s http://169.254.169.254/latest/meta-data/public-ipv4)

# Install dependencies
sudo apt-get update
sudo apt-get install -y python3 python3-pip
pip3 install --user typing

# Get TEE type and pair ID from instance tags
TEE_TYPE=\$(aws ec2 describe-tags --filters "Name=resource-id,Values=\$INSTANCE_ID" "Name=key,Values=TEEType" --query "Tags[0].Value" --output text)
PAIR_ID=\$(aws ec2 describe-tags --filters "Name=resource-id,Values=\$INSTANCE_ID" "Name=key,Values=PairId" --query "Tags[0].Value" --output text)

echo "Setting up \$TEE_TYPE node in pair \$PAIR_ID"

# Use appropriate config
CONFIG_FILE="\${TEE_TYPE,,}_node_\${PAIR_ID}_config.json"
if [ ! -f "\$CONFIG_FILE" ]; then
    echo "Config file not found: \$CONFIG_FILE"
    exit 1
fi

# Start validator
chmod +x rsa_enhanced_validator.py
python3 rsa_enhanced_validator.py --config \$CONFIG_FILE > validator.log 2>&1 &
echo \$! > validator.pid

echo "TEE Validator started. Check validator.log for details."
EOL

chmod +x $DEPLOY_SCRIPT

# Deploy to all instances
echo -e "${GREEN}Deploying to all instances...${NC}"
while IFS=, read -r INSTANCE_ID PUBLIC_IP TEE_TYPE PAIR_ID || [[ -n "$INSTANCE_ID" ]]; do
    # Skip comment lines
    [[ "$INSTANCE_ID" =~ ^#.* ]] && continue
    
    echo -e "${YELLOW}Deploying to $TEE_TYPE node ($PUBLIC_IP) in pair $PAIR_ID${NC}"
    
    # Create deployment package
    ssh -o StrictHostKeyChecking=no ubuntu@$PUBLIC_IP "mkdir -p ~/tee_validator"
    scp -r $DIST_DIR/* ubuntu@$PUBLIC_IP:~/tee_validator/
    
    # Run setup script
    ssh ubuntu@$PUBLIC_IP "cd ~/tee_validator && chmod +x setup_validator.sh && ./setup_validator.sh"
    
    echo -e "${GREEN}Deployment complete for $TEE_TYPE node in pair $PAIR_ID${NC}"
done < tee_assignments.txt

echo -e "${GREEN}=== All deployments complete ===${NC}"
echo -e "To test cross-attestation, use the enhanced_validator_test.py script with the following pairs:"

# Print test command examples
while IFS=, read -r INSTANCE_ID PUBLIC_IP TEE_TYPE PAIR_ID || [[ -n "$INSTANCE_ID" ]]; do
    # Skip comment lines
    [[ "$INSTANCE_ID" =~ ^#.* ]] && continue
    
    # Find partner in same pair
    PARTNER_IP=""
    PARTNER_TYPE=""
    for ((j=0; j<${#PAIRS[@]}; j++)); do
        IFS=, read -r P_INSTANCE_ID P_PUBLIC_IP P_TEE_TYPE P_PAIR_ID <<< "${PAIRS[$j]}"
        if [ "$PAIR_ID" == "$P_PAIR_ID" ] && [ "$TEE_TYPE" != "$P_TEE_TYPE" ]; then
            PARTNER_IP=$P_PUBLIC_IP
            PARTNER_TYPE=$P_TEE_TYPE
            break
        fi
    done
    
    if [ "$TEE_TYPE" == "SGX" ]; then
        echo -e "Pair $PAIR_ID: ./enhanced_validator_test.py --sgx-host $PUBLIC_IP --sev-host $PARTNER_IP --test all"
    fi
done < tee_assignments.txt

# Clean up
rm -rf $DIST_DIR
