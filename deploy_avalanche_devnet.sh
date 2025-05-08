#!/bin/bash
# Deploy Avalanche validators and configure them to work with the TEE mesh network
set -e

if [ ! -f "tee_deployment_info.json" ]; then
    echo "Error: tee_deployment_info.json not found. Please run deploy_single_region.sh first."
    exit 1
fi

# Load deployment information
REGION_ID=$(jq -r '.region_id' tee_deployment_info.json)
COORDINATOR_IP=$(jq -r '.coordinator' tee_deployment_info.json)
SGX_IP_1=$(jq -r '.tee_pairs[0].sgx' tee_deployment_info.json)
SEV_IP_1=$(jq -r '.tee_pairs[0].sev' tee_deployment_info.json)
SGX_IP_2=$(jq -r '.tee_pairs[1].sgx' tee_deployment_info.json)
SEV_IP_2=$(jq -r '.tee_pairs[1].sev' tee_deployment_info.json)

KEY_PATH="~/.ssh/tee-access-key.pem"
TIMEOUT=10
AVALANCHE_VERSION="1.10.15"
NETWORK_ID="devnet-${REGION_ID}"
VM_ID="tGas3T58KzdjLHhBDMnH2TvrddhqTji5iZAMZ3RXs2NLpSnhH"

echo "Deploying Avalanche devnet with validators integrated with TEE mesh network in ${REGION_ID}"

# Create security group for Avalanche validators
echo "Creating security group for Avalanche validators..."
SG_ID=$(aws ec2 create-security-group \
  --group-name avalanche-${REGION_ID}-sg \
  --description "Security group for Avalanche validators in ${REGION_ID}" \
  --output text \
  --query 'GroupId')

# Allow SSH and Avalanche ports
aws ec2 authorize-security-group-ingress \
  --group-id ${SG_ID} \
  --protocol tcp \
  --port 22 \
  --cidr 0.0.0.0/0

aws ec2 authorize-security-group-ingress \
  --group-id ${SG_ID} \
  --protocol tcp \
  --port 9650 \
  --cidr 0.0.0.0/0

aws ec2 authorize-security-group-ingress \
  --group-id ${SG_ID} \
  --protocol tcp \
  --port 9651 \
  --cidr 0.0.0.0/0

# Deploy 5 validator nodes
echo "Deploying Avalanche validator nodes..."
VALIDATOR_IDS=($(aws ec2 run-instances \
  --image-id ami-030f04819b19327fc \
  --count 5 \
  --instance-type c5.2xlarge \
  --key-name tee-access-key \
  --security-group-ids ${SG_ID} \
  --tag-specifications "ResourceType=instance,Tags=[{Key=Name,Value=avalanche-validator-${REGION_ID}}]" \
  --output text \
  --query 'Instances[*].InstanceId'))

echo "Waiting for validator instances to be ready..."
aws ec2 wait instance-running --instance-ids ${VALIDATOR_IDS[@]}

# Get validator public IPs
VALIDATOR_IPS=()
for ID in "${VALIDATOR_IDS[@]}"; do
    IP=$(aws ec2 describe-instances --instance-ids ${ID} --query 'Reservations[0].Instances[0].PublicIpAddress' --output text)
    VALIDATOR_IPS+=($IP)
done

echo "Validators deployed with IPs: ${VALIDATOR_IPS[@]}"

# Build MorpheusVM plugin
echo "Building MorpheusVM plugin..."
PLUGIN_DIR="./plugins"
mkdir -p ${PLUGIN_DIR}
go build -o ${PLUGIN_DIR}/morpheusvm ./cmd/morpheusvm

# Setup bootstrap node (first validator)
BOOTSTRAP_IP=${VALIDATOR_IPS[0]}
echo "Setting up bootstrap node at ${BOOTSTRAP_IP}..."
scp -i $KEY_PATH -o StrictHostKeyChecking=no -o ConnectTimeout=$TIMEOUT \
    ${PLUGIN_DIR}/morpheusvm admin@${BOOTSTRAP_IP}:/home/admin/morpheusvm

ssh -i $KEY_PATH -o StrictHostKeyChecking=no -o ConnectTimeout=$TIMEOUT admin@${BOOTSTRAP_IP} << EOF
    # Install Avalanche
    curl -sSfL https://raw.githubusercontent.com/ava-labs/avalanche-cli/main/scripts/install.sh | sh -s -- -b /usr/local/bin
    
    # Create plugin directory
    mkdir -p ~/.avalanchego/plugins
    mv ./morpheusvm ~/.avalanchego/plugins/
    
    # Create config with TEE connection info
    cat > ~/.avalanchego/configs/tee_config.json << EOC
{
    "coordinator_endpoint": "${COORDINATOR_IP}:7070",
    "region_id": "${REGION_ID}",
    "tee_pairs": [
        {
            "sgx": "${SGX_IP_1}:7080",
            "sev": "${SEV_IP_1}:7080"
        },
        {
            "sgx": "${SGX_IP_2}:7080",
            "sev": "${SEV_IP_2}:7080"
        }
    ],
    "batch_size": 100,
    "thread_count": 8,
    "timeserver_endpoint": "${COORDINATOR_IP}:7075"
}
EOC
    
    # Create bootstrap node config
    cat > ~/.avalanchego/config.json << EOC
{
    "network-id": "${NETWORK_ID}",
    "health-check-frequency": "2s",
    "log-level": "info",
    "public-ip": "${BOOTSTRAP_IP}",
    "http-host": "0.0.0.0",
    "vms": ["${VM_ID}"],
    "tee-config-file": "~/.avalanchego/configs/tee_config.json"
}
EOC
    
    # Start bootstrap node
    nohup avalanchego > avalanche.log 2>&1 &
    echo "Bootstrap node started"
    
    # Wait for node to initialize
    sleep 20
    
    # Get bootstrap node ID
    BOOTSTRAP_ID=\$(curl -s -X POST --data '{"jsonrpc": "2.0", "id":1, "method": "info.getNodeID"}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/info | jq -r '.result.nodeID')
    echo \${BOOTSTRAP_ID} > bootstrap_id.txt
EOF

# Get bootstrap node ID
BOOTSTRAP_ID=$(ssh -i $KEY_PATH -o StrictHostKeyChecking=no -o ConnectTimeout=$TIMEOUT admin@${BOOTSTRAP_IP} "cat bootstrap_id.txt")
BOOTSTRAP_URL="${BOOTSTRAP_IP}:9651"

echo "Bootstrap node ID: ${BOOTSTRAP_ID}"
echo "Bootstrap URL: ${BOOTSTRAP_URL}"

# Setup other validators
for ((i=1; i<${#VALIDATOR_IPS[@]}; i++)); do
    VALIDATOR_IP=${VALIDATOR_IPS[$i]}
    echo "Setting up validator node ${i} at ${VALIDATOR_IP}..."
    
    scp -i $KEY_PATH -o StrictHostKeyChecking=no -o ConnectTimeout=$TIMEOUT \
        ${PLUGIN_DIR}/morpheusvm admin@${VALIDATOR_IP}:/home/admin/morpheusvm
    
    ssh -i $KEY_PATH -o StrictHostKeyChecking=no -o ConnectTimeout=$TIMEOUT admin@${VALIDATOR_IP} << EOF
        # Install Avalanche
        curl -sSfL https://raw.githubusercontent.com/ava-labs/avalanche-cli/main/scripts/install.sh | sh -s -- -b /usr/local/bin
        
        # Create plugin directory
        mkdir -p ~/.avalanchego/plugins
        mv ./morpheusvm ~/.avalanchego/plugins/
        
        # Create config with TEE connection info
        cat > ~/.avalanchego/configs/tee_config.json << EOC
{
    "coordinator_endpoint": "${COORDINATOR_IP}:7070",
    "region_id": "${REGION_ID}",
    "tee_pairs": [
        {
            "sgx": "${SGX_IP_1}:7080",
            "sev": "${SEV_IP_1}:7080"
        },
        {
            "sgx": "${SGX_IP_2}:7080",
            "sev": "${SEV_IP_2}:7080"
        }
    ],
    "batch_size": 100,
    "thread_count": 8,
    "timeserver_endpoint": "${COORDINATOR_IP}:7075"
}
EOC
        
        # Create validator config with bootstrap info
        cat > ~/.avalanchego/config.json << EOC
{
    "network-id": "${NETWORK_ID}",
    "health-check-frequency": "2s",
    "log-level": "info",
    "public-ip": "${VALIDATOR_IP}",
    "http-host": "0.0.0.0",
    "bootstrap-ids": ["${BOOTSTRAP_ID}"],
    "bootstrap-ips": ["${BOOTSTRAP_URL}"],
    "vms": ["${VM_ID}"],
    "tee-config-file": "~/.avalanchego/configs/tee_config.json"
}
EOC
        
        # Start validator node
        nohup avalanchego > avalanche.log 2>&1 &
        echo "Validator node started"
EOF
done

# Save validator information
cat > avalanche_deployment_info.json << EOF
{
    "network_id": "${NETWORK_ID}",
    "bootstrap_node": {
        "id": "${BOOTSTRAP_ID}",
        "ip": "${BOOTSTRAP_IP}",
        "api_endpoint": "http://${BOOTSTRAP_IP}:9650"
    },
    "validators": [
EOF

for ((i=0; i<${#VALIDATOR_IPS[@]}; i++)); do
    if [ $i -gt 0 ]; then
        echo "        ," >> avalanche_deployment_info.json
    fi
    echo "        {" >> avalanche_deployment_info.json
    echo "            \"ip\": \"${VALIDATOR_IPS[$i]}\"," >> avalanche_deployment_info.json
    echo "            \"api_endpoint\": \"http://${VALIDATOR_IPS[$i]}:9650\"" >> avalanche_deployment_info.json
    echo "        }" >> avalanche_deployment_info.json
done

cat >> avalanche_deployment_info.json << EOF
    ],
    "vm_id": "${VM_ID}",
    "tee_integration": {
        "region_id": "${REGION_ID}",
        "coordinator": "${COORDINATOR_IP}:7070",
        "batch_size": 100,
        "thread_count": 8
    }
}
EOF

echo "Avalanche devnet with ${#VALIDATOR_IPS[@]} validators deployed successfully!"
echo "Waiting for network to stabilize..."
sleep 30

echo "Creating Morpheus blockchain..."
ssh -i $KEY_PATH -o StrictHostKeyChecking=no -o ConnectTimeout=$TIMEOUT admin@${BOOTSTRAP_IP} << EOF
    # Get current chains
    CHAINS=\$(curl -s -X POST --data '{"jsonrpc":"2.0","method":"platform.getBlockchains","params":{},"id":1}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/P)
    echo "Current chains: \${CHAINS}"
    
    # Create Morpheus blockchain
    echo "Creating Morpheus blockchain..."
    CREATE_RESULT=\$(curl -s -X POST --data '{
        "jsonrpc": "2.0",
        "method": "platform.createBlockchain",
        "params": {
            "subnetID": "11111111111111111111111111111111LpoYY",
            "vmID": "${VM_ID}",
            "name": "morpheus",
            "genesisData": "0x0000000000000000000000000000000000000000000000000000000000000000",
            "encoding": "hex"
        },
        "id": 1
    }' -H 'content-type:application/json;' 127.0.0.1:9650/ext/P)
    echo "Create blockchain result: \${CREATE_RESULT}"
EOF

echo "Devnet deployment information saved to avalanche_deployment_info.json"
echo "To validate the deployment, run: ./validate_avalanche_devnet.sh"
