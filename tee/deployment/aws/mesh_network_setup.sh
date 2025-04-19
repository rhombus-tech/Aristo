#!/bin/bash

# Mesh Network and Cross-Attestation Setup Script for AWS
# This script configures the mesh network between Intel SGX and AMD SEV nodes
# and sets up cross-attestation with parameter validation for WebAssembly contracts

set -e

# Configuration
STACK_NAME="dual-tee-architecture"
REGION="us-east-1"
SSH_KEY=""
SSH_OPTIONS="-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null"

# Parse command line arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    --stack-name)
      STACK_NAME="$2"
      shift 2
      ;;
    --region)
      REGION="$2"
      shift 2
      ;;
    --ssh-key)
      SSH_KEY="$2"
      shift 2
      ;;
    *)
      echo "Unknown option: $1"
      exit 1
      ;;
  esac
done

# Validate required parameters
if [ -z "$SSH_KEY" ]; then
  echo "Error: --ssh-key is required (path to private key file)"
  exit 1
fi

# Get instance information
echo "Retrieving instance information from CloudFormation stack..."
SGX_GROUP=$(aws cloudformation describe-stacks --stack-name $STACK_NAME --region $REGION --query "Stacks[0].Outputs[?OutputKey=='SGXNodeGroupName'].OutputValue" --output text)
SEV_GROUP=$(aws cloudformation describe-stacks --stack-name $STACK_NAME --region $REGION --query "Stacks[0].Outputs[?OutputKey=='SEVNodeGroupName'].OutputValue" --output text)
NASDAQ_CONNECTOR_IP=$(aws cloudformation describe-stacks --stack-name $STACK_NAME --region $REGION --query "Stacks[0].Outputs[?OutputKey=='NASDAQConnectorIP'].OutputValue" --output text)

# Get instance IPs
SGX_INSTANCES=$(aws autoscaling describe-auto-scaling-groups --auto-scaling-group-names $SGX_GROUP --region $REGION --query "AutoScalingGroups[0].Instances[*].InstanceId" --output text)
SEV_INSTANCES=$(aws autoscaling describe-auto-scaling-groups --auto-scaling-group-names $SEV_GROUP --region $REGION --query "AutoScalingGroups[0].Instances[*].InstanceId" --output text)

SGX_IPS=()
SEV_IPS=()

for INSTANCE in $SGX_INSTANCES; do
  IP=$(aws ec2 describe-instances --instance-ids $INSTANCE --region $REGION --query "Reservations[0].Instances[0].PrivateIpAddress" --output text)
  SGX_IPS+=("$IP")
done

for INSTANCE in $SEV_INSTANCES; do
  IP=$(aws ec2 describe-instances --instance-ids $INSTANCE --region $REGION --query "Reservations[0].Instances[0].PrivateIpAddress" --output text)
  SEV_IPS+=("$IP")
done

echo "=== Node Configuration ==="
echo "Intel SGX Nodes: ${SGX_IPS[@]}"
echo "AMD SEV Nodes: ${SEV_IPS[@]}"
echo "NASDAQ Connector: $NASDAQ_CONNECTOR_IP"

# Wait for nodes to initialize
echo "Waiting for nodes to complete initialization (60 seconds)..."
sleep 60

# Configure cross-attestation between SGX and SEV nodes
echo "Setting up cross-attestation between SGX and SEV nodes..."

# Generate mesh network configuration
MESH_CONFIG=$(cat <<EOF
{
  "network": {
    "name": "dual-tee-mesh",
    "region": "$REGION",
    "nodes": {
$(for IP in "${SGX_IPS[@]}"; do
  echo "      \"sgx-$IP\": {\"type\": \"SGX\", \"ip\": \"$IP\", \"port\": 7070},"
done)
$(for IP in "${SEV_IPS[@]}"; do
  echo "      \"sev-$IP\": {\"type\": \"SEV\", \"ip\": \"$IP\", \"port\": 7070},"
done)
      "nasdaq-connector": {"type": "CONNECTOR", "ip": "$NASDAQ_CONNECTOR_IP", "port": 9092}
    }
  },
  "attestation": {
    "verification_ms_target": 100,
    "attestation_types": ["SGX", "SEV"],
    "cross_attestation": true,
    "parameter_validation": {
      "length_prefixed": true,
      "direct_format": true,
      "max_size": 1024,
      "format_detection": true
    }
  }
}
EOF
)

# Configure SGX nodes
for IP in "${SGX_IPS[@]}"; do
  echo "Configuring SGX node $IP..."
  
  # Copy mesh network configuration
  echo "$MESH_CONFIG" | ssh $SSH_OPTIONS -i "$SSH_KEY" ubuntu@$IP "cat > /tmp/mesh_config.json"
  
  # Configure the node - bypassing strict key checking
  ssh $SSH_OPTIONS -i "$SSH_KEY" -o "IdentitiesOnly=yes" ubuntu@$IP <<'EOF'
    sudo mkdir -p /opt/rhombus/mesh
    sudo mv /tmp/mesh_config.json /opt/rhombus/mesh/
    
    # Install parameter validation handler for WebAssembly
    cat > /tmp/param_handler.js <<'EOT'
/**
 * Parameter format detection and validation handler
 * Supports both length-prefixed and direct parameter formats
 */
function validateParameter(buffer, expectedSize) {
  if (buffer.length < 4) {
    console.log("Parameter too short, using direct format");
    return { format: "direct", data: buffer };
  }
  
  // Check if first 4 bytes could be a reasonable length prefix
  const lengthValue = buffer.readUInt32LE(0);
  
  if (lengthValue > 0 && lengthValue <= 1024 && lengthValue + 4 <= buffer.length) {
    console.log("Detected length-prefixed format, length:", lengthValue);
    return { 
      format: "length-prefixed", 
      data: buffer.slice(4, 4 + lengthValue) 
    };
  } else {
    console.log("Using direct format, no valid length prefix detected");
    return { format: "direct", data: buffer };
  }
}

module.exports = { validateParameter };
EOT
    sudo mkdir -p /opt/rhombus/validation
    sudo mv /tmp/param_handler.js /opt/rhombus/validation/
    
    # Configure cross-attestation
    echo 'export TEE_TYPE="SGX"' | sudo tee -a /etc/environment
    echo 'export CROSS_ATTESTATION_ENABLED="true"' | sudo tee -a /etc/environment
    echo 'export PARTNER_TEE_TYPE="SEV"' | sudo tee -a /etc/environment
    echo 'export PARAMETER_VALIDATION="true"' | sudo tee -a /etc/environment
    
    # Restart services
    if [ -f /opt/rhombus/tee-integration/restart.sh ]; then
      cd /opt/rhombus/tee-integration && sudo ./restart.sh
    fi
EOF
done

# Configure SEV nodes
for IP in "${SEV_IPS[@]}"; do
  echo "Configuring SEV node $IP..."
  
  # Copy mesh network configuration
  echo "$MESH_CONFIG" | ssh $SSH_OPTIONS -i "$SSH_KEY" ubuntu@$IP "cat > /tmp/mesh_config.json"
  
  # Configure the node - bypassing strict key checking
  ssh $SSH_OPTIONS -i "$SSH_KEY" -o "IdentitiesOnly=yes" ubuntu@$IP <<'EOF'
    sudo mkdir -p /opt/rhombus/mesh
    sudo mv /tmp/mesh_config.json /opt/rhombus/mesh/
    
    # Install parameter validation handler for WebAssembly
    cat > /tmp/param_handler.js <<'EOT'
/**
 * Parameter format detection and validation handler
 * Supports both length-prefixed and direct parameter formats
 */
function validateParameter(buffer, expectedSize) {
  if (buffer.length < 4) {
    console.log("Parameter too short, using direct format");
    return { format: "direct", data: buffer };
  }
  
  // Check if first 4 bytes could be a reasonable length prefix
  const lengthValue = buffer.readUInt32LE(0);
  
  if (lengthValue > 0 && lengthValue <= 1024 && lengthValue + 4 <= buffer.length) {
    console.log("Detected length-prefixed format, length:", lengthValue);
    return { 
      format: "length-prefixed", 
      data: buffer.slice(4, 4 + lengthValue) 
    };
  } else {
    console.log("Using direct format, no valid length prefix detected");
    return { format: "direct", data: buffer };
  }
}

module.exports = { validateParameter };
EOT
    sudo mkdir -p /opt/rhombus/validation
    sudo mv /tmp/param_handler.js /opt/rhombus/validation/
    
    # Configure cross-attestation
    echo 'export TEE_TYPE="SEV"' | sudo tee -a /etc/environment
    echo 'export CROSS_ATTESTATION_ENABLED="true"' | sudo tee -a /etc/environment
    echo 'export PARTNER_TEE_TYPE="SGX"' | sudo tee -a /etc/environment
    echo 'export PARAMETER_VALIDATION="true"' | sudo tee -a /etc/environment
    
    # Restart services
    if [ -f /opt/rhombus/tee-integration/restart.sh ]; then
      cd /opt/rhombus/tee-integration && sudo ./restart.sh
    fi
EOF
done

# Configure NASDAQ connector
echo "Configuring NASDAQ connector..."
ssh $SSH_OPTIONS -i "$SSH_KEY" ubuntu@$NASDAQ_CONNECTOR_IP <<EOF
  sudo mkdir -p /opt/rhombus/mesh
  sudo bash -c 'cat > /opt/rhombus/mesh/tee_nodes.json' << EOT
{
  "sgx_nodes": [$(printf '"%s",' "${SGX_IPS[@]}" | sed 's/,$//')],
  "sev_nodes": [$(printf '"%s",' "${SEV_IPS[@]}" | sed 's/,$//')],
  "region": "$REGION"
}
EOT

  # Update NASDAQ Kafka configuration
  sudo bash -c 'cat >> /opt/kafka/kafka_2.13-3.2.3/config/nasdaq-connector.properties' << EOT
# TEE integration configuration
tee.nodes.file=/opt/rhombus/mesh/tee_nodes.json
tee.attestation.required=true
tee.cross.attestation=true
EOT

  # Restart NASDAQ connector service
  sudo systemctl restart market-data
EOF

echo "=== Mesh Network and Cross-Attestation Setup Complete ==="
echo "The dual TEE architecture is now configured with cross-attestation between Intel SGX and AMD SEV nodes"
echo "Parameter validation for both length-prefixed and direct formats is enabled for WebAssembly contracts"
echo ""
echo "Next steps:"
echo "1. Test cross-attestation between nodes"
echo "2. Verify NASDAQ market data integration"
echo "3. Run performance tests for parameter validation"

exit 0
