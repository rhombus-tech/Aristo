#!/bin/bash
# Deploy TEE controllers for parameter validation testing
# This script deploys the controller to SGX and SEV nodes with direct pairing

set -e

# Configuration
KEY_NAME="nasdaq-tee-key"
PAIR_CONFIG_DIR="/Users/talzisckind/Downloads/aristo-fresh 2/tee/deployment/aws/pair_configs"
PROJECT_DIR="/Users/talzisckind/Downloads/aristo-fresh 2"
EXECUTION_DIR="${PROJECT_DIR}/execution"
BUILD_DIR="${PROJECT_DIR}/build_execution"

# Create build directory
mkdir -p "${BUILD_DIR}"
mkdir -p "${PAIR_CONFIG_DIR}"

# Determine TEE node pairs from CloudFormation outputs
SGX_NODES=()
SEV_NODES=()
PAIR_IDS=()
PAIR_STACKS=$(aws cloudformation describe-stacks --region us-east-1 --query "Stacks[?contains(StackName, 'nasdaq-tee-pairs')].StackName" --output text)

echo "=== TEE Pairs Found ==="
for STACK in $PAIR_STACKS; do
  SGX_IP=$(aws cloudformation describe-stacks --stack-name $STACK --region us-east-1 --query "Stacks[0].Outputs[?OutputKey=='SGXNodePublicIP'].OutputValue" --output text)
  SEV_IP=$(aws cloudformation describe-stacks --stack-name $STACK --region us-east-1 --query "Stacks[0].Outputs[?OutputKey=='SEVNodePublicIP'].OutputValue" --output text)
  
  if [ -n "$SGX_IP" ] && [ -n "$SEV_IP" ]; then
    PAIR_ID=$(echo $STACK | grep -oE '[0-9]+$')
    PAIR_IDS+=($PAIR_ID)
    SGX_NODES+=($SGX_IP)
    SEV_NODES+=($SEV_IP)
    
    echo "Pair $PAIR_ID:"
    echo "  SGX Node: $SGX_IP"
    echo "  SEV Node: $SEV_IP"
    
    # Create pair configuration files
    cat > "${PAIR_CONFIG_DIR}/pair_${PAIR_ID}.json" << EOF
{
  "pair_id": $PAIR_ID,
  "region_id": "us-east-1",
  "sgx_node": {
    "ip": "$SGX_IP",
    "tee_type": "IntelSGX",
    "port": 7070
  },
  "sev_node": {
    "ip": "$SEV_IP",
    "tee_type": "SEV",
    "port": 7070
  },
  "cross_attestation": {
    "enabled": true,
    "accumulator_size": 32,
    "verification_ms_target": 100
  },
  "parameter_validation": {
    "length_prefixed": true,
    "direct_format": true,
    "max_size": 1024,
    "format_detection": true
  }
}
EOF
  fi
done

if [ ${#SGX_NODES[@]} -eq 0 ] || [ ${#SEV_NODES[@]} -eq 0 ]; then
  echo "No TEE pairs found. Please deploy TEE pairs first."
  exit 1
fi

echo "Found ${#SGX_NODES[@]} TEE pairs"

# Build the controller for deployment
echo "=== Building TEE Controller ==="
cd "${EXECUTION_DIR}"

# Check for cargo
if ! command -v cargo &> /dev/null; then
  echo "Rust not found. Installing Rust..."
  curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
  source "$HOME/.cargo/env"
fi

# Build optimized release binary for controller
echo "Building controller binary from workspace..."
cargo build --release --bin tee-controller

# Create deployment package
echo "Creating deployment package..."
mkdir -p "${BUILD_DIR}/bin"
mkdir -p "${BUILD_DIR}/config"
cp "${EXECUTION_DIR}/target/release/tee-controller" "${BUILD_DIR}/bin/controller"
cp -r "${PAIR_CONFIG_DIR}"/* "${BUILD_DIR}/config/"

# Create configuration for controllers
cat > "${BUILD_DIR}/controller_config_template.json" << EOF
{
  "listen_address": "0.0.0.0:7070",
  "tee_type": "PLACEHOLDER_TYPE",
  "node_id": "PLACEHOLDER_ID",
  "partner_tee": {
    "ip": "PLACEHOLDER_PARTNER_IP",
    "tee_type": "PLACEHOLDER_PARTNER_TYPE",
    "port": 7070
  },
  "region_id": "us-east-1",
  "parameter_validation": {
    "length_prefixed": true,
    "direct_format": true,
    "max_size": 1024,
    "format_detection": true
  },
  "accumulator": {
    "size_bytes": 32,
    "batch_size": 1000,
    "threads": 8
  },
  "direct_pairing": true
}
EOF

# Create service file for systemd
cat > "${BUILD_DIR}/controller.service" << EOF
[Unit]
Description=TEE Execution Controller
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/rhombus/execution
ExecStart=/opt/rhombus/execution/bin/controller --config /opt/rhombus/execution/config/controller_config.json
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

# Deploy to each TEE pair
echo "=== Deploying to TEE Nodes ==="
for i in "${!SGX_NODES[@]}"; do
  SGX_IP=${SGX_NODES[$i]}
  SEV_IP=${SEV_NODES[$i]}
  PAIR_ID=${PAIR_IDS[$i]}
  
  # Deploy to SGX Node
  echo "Deploying to SGX Node: $SGX_IP (Pair $PAIR_ID)"
  
  # Create directory structure
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SGX_IP "sudo mkdir -p /opt/rhombus/execution/{bin,config,logs}"
  
  # Copy files
  scp -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem -r "${BUILD_DIR}/bin/"* ubuntu@$SGX_IP:/tmp/
  scp -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem -r "${BUILD_DIR}/config/"* ubuntu@$SGX_IP:/tmp/
  scp -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem "${BUILD_DIR}/controller.service" ubuntu@$SGX_IP:/tmp/
  scp -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem "${BUILD_DIR}/controller_config_template.json" ubuntu@$SGX_IP:/tmp/
  
  # Configure for SGX with direct pairing to SEV
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SGX_IP "cat /tmp/controller_config_template.json | \
    sed 's/PLACEHOLDER_TYPE/IntelSGX/g' | \
    sed 's/PLACEHOLDER_ID/sgx-$PAIR_ID/g' | \
    sed 's/PLACEHOLDER_PARTNER_IP/$SEV_IP/g' | \
    sed 's/PLACEHOLDER_PARTNER_TYPE/SEV/g' > \
    /tmp/controller_config.json"
  
  # Move files and set permissions
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SGX_IP "sudo cp /tmp/controller /opt/rhombus/execution/bin/ && \
                                                                      sudo cp /tmp/*.json /opt/rhombus/execution/config/ && \
                                                                      sudo cp /tmp/controller_config.json /opt/rhombus/execution/config/ && \
                                                                      sudo cp /tmp/controller.service /etc/systemd/system/ && \
                                                                      sudo chmod +x /opt/rhombus/execution/bin/*"
  
  # Install dependencies if needed
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SGX_IP "sudo apt-get update && sudo apt-get install -y libssl-dev"
  
  # Start the service
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SGX_IP "sudo systemctl daemon-reload && \
                                                                     sudo systemctl enable controller && \
                                                                     sudo systemctl start controller"
  
  # Deploy to SEV Node
  echo "Deploying to SEV Node: $SEV_IP (Pair $PAIR_ID)"
  
  # Create directory structure
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SEV_IP "sudo mkdir -p /opt/rhombus/execution/{bin,config,logs}"
  
  # Copy files
  scp -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem -r "${BUILD_DIR}/bin/"* ubuntu@$SEV_IP:/tmp/
  scp -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem -r "${BUILD_DIR}/config/"* ubuntu@$SEV_IP:/tmp/
  scp -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem "${BUILD_DIR}/controller.service" ubuntu@$SEV_IP:/tmp/
  scp -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem "${BUILD_DIR}/controller_config_template.json" ubuntu@$SEV_IP:/tmp/
  
  # Configure for SEV with direct pairing to SGX
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SEV_IP "cat /tmp/controller_config_template.json | \
    sed 's/PLACEHOLDER_TYPE/SEV/g' | \
    sed 's/PLACEHOLDER_ID/sev-$PAIR_ID/g' | \
    sed 's/PLACEHOLDER_PARTNER_IP/$SGX_IP/g' | \
    sed 's/PLACEHOLDER_PARTNER_TYPE/IntelSGX/g' > \
    /tmp/controller_config.json"
  
  # Move files and set permissions
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SEV_IP "sudo cp /tmp/controller /opt/rhombus/execution/bin/ && \
                                                                    sudo cp /tmp/*.json /opt/rhombus/execution/config/ && \
                                                                    sudo cp /tmp/controller_config.json /opt/rhombus/execution/config/ && \
                                                                    sudo cp /tmp/controller.service /etc/systemd/system/ && \
                                                                    sudo chmod +x /opt/rhombus/execution/bin/*"
  
  # Install dependencies if needed
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SEV_IP "sudo apt-get update && sudo apt-get install -y libssl-dev"
  
  # Start the service
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SEV_IP "sudo systemctl daemon-reload && \
                                                                   sudo systemctl enable controller && \
                                                                   sudo systemctl start controller"
done

echo "=== Waiting for services to initialize (30 seconds) ==="
sleep 30

# Final check: verify controllers are running
echo "=== Verifying TEE Controller Services ==="
for i in "${!SGX_NODES[@]}"; do
  SGX_IP=${SGX_NODES[$i]}
  SEV_IP=${SEV_NODES[$i]}
  PAIR_ID=${PAIR_IDS[$i]}
  
  SGX_STATUS=$(ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SGX_IP "sudo systemctl is-active controller")
  echo "SGX Controller status at $SGX_IP (Pair $PAIR_ID): $SGX_STATUS"
  
  SEV_STATUS=$(ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SEV_IP "sudo systemctl is-active controller")
  echo "SEV Controller status at $SEV_IP (Pair $PAIR_ID): $SEV_STATUS"
done

echo "=== TEE Controller Deployment Complete ==="
echo "Your TEE nodes are now configured with dual-format parameter validation and cross-attestation"
echo "Each SGX node is directly paired with its corresponding SEV node"
echo "Ready for NASDAQ market data simulation testing with parameter validation"
