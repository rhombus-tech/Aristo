#!/bin/bash
# Deploy the execution submodule to TEE nodes
# This script compiles and deploys the controller to SGX and SEV nodes

set -e

# Configuration
KEY_NAME="nasdaq-tee-key"
PAIR_CONFIG_DIR="/Users/talzisckind/Downloads/aristo-fresh 2/tee/deployment/aws/pair_configs"
PROJECT_DIR="/Users/talzisckind/Downloads/aristo-fresh 2"
EXECUTION_DIR="${PROJECT_DIR}/execution"
BUILD_DIR="${PROJECT_DIR}/build_execution"

# Create build directory
mkdir -p "${BUILD_DIR}"

# Determine TEE node pairs from CloudFormation outputs
SGX_NODES=()
SEV_NODES=()
PAIR_STACKS=$(aws cloudformation describe-stacks --region us-east-1 --query "Stacks[?contains(StackName, 'nasdaq-tee-pairs')].StackName" --output text)

echo "=== TEE Pairs Found ==="
mkdir -p "${PAIR_CONFIG_DIR}"
for STACK in $PAIR_STACKS; do
  SGX_IP=$(aws cloudformation describe-stacks --stack-name $STACK --region us-east-1 --query "Stacks[0].Outputs[?OutputKey=='SGXNodePublicIP'].OutputValue" --output text)
  SEV_IP=$(aws cloudformation describe-stacks --stack-name $STACK --region us-east-1 --query "Stacks[0].Outputs[?OutputKey=='SEVNodePublicIP'].OutputValue" --output text)
  
  if [ -n "$SGX_IP" ] && [ -n "$SEV_IP" ]; then
    SGX_NODES+=($SGX_IP)
    SEV_NODES+=($SEV_IP)
    PAIR_ID=$(echo $STACK | grep -oE '[0-9]+$')
    
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
echo "=== Building Execution Controller ==="
cd "${EXECUTION_DIR}"

# Check for cargo
if ! command -v cargo &> /dev/null; then
  echo "Rust not found. Installing Rust..."
  curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
  source "$HOME/.cargo/env"
fi

# Build optimized release binaries
echo "Building controller binaries from workspace..."
cargo build --release --bin tee-controller
cargo build --release --bin coordinator_mock

# Create deployment package
echo "Creating deployment package..."
mkdir -p "${BUILD_DIR}/bin"
mkdir -p "${BUILD_DIR}/config"
cp "${EXECUTION_DIR}/target/release/tee-controller" "${BUILD_DIR}/bin/controller"
cp "${EXECUTION_DIR}/target/release/coordinator_mock" "${BUILD_DIR}/bin/coordinator"
cp -r "${PAIR_CONFIG_DIR}"/* "${BUILD_DIR}/config/"

# Create configuration for controllers
cat > "${BUILD_DIR}/config/controller_config.json" << EOF
{
  "listen_address": "0.0.0.0:7070",
  "tee_type": "PLACEHOLDER",
  "region_id": "us-east-1",
  "coordinator_mode": true,
  "coordinator_url": "PLACEHOLDER",
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
  }
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

# Deploy to SGX nodes
echo "=== Deploying to SGX Nodes ==="
for SGX_IP in "${SGX_NODES[@]}"; do
  echo "Deploying to SGX Node: $SGX_IP"
  
  # Create directory structure
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SGX_IP "sudo mkdir -p /opt/rhombus/execution/{bin,config,logs}"
  
  # Copy files
  scp -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem -r "${BUILD_DIR}/bin/"* ubuntu@$SGX_IP:/tmp/
  scp -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem -r "${BUILD_DIR}/config/"* ubuntu@$SGX_IP:/tmp/
  scp -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem "${BUILD_DIR}/controller.service" ubuntu@$SGX_IP:/tmp/
  
  # Configure for SGX
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SGX_IP "sudo sed 's/PLACEHOLDER/IntelSGX/g' /tmp/controller_config.json > /tmp/sgx_controller_config.json"
  
  # Move files and set permissions
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SGX_IP "sudo cp /tmp/controller /tmp/coordinator /opt/rhombus/execution/bin/ && \
                                                                        sudo cp /tmp/*.json /opt/rhombus/execution/config/ && \
                                                                        sudo cp /tmp/sgx_controller_config.json /opt/rhombus/execution/config/controller_config.json && \
                                                                        sudo cp /tmp/controller.service /etc/systemd/system/ && \
                                                                        sudo chmod +x /opt/rhombus/execution/bin/*"
  
  # Install dependencies if needed
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SGX_IP "sudo apt-get update && sudo apt-get install -y libssl-dev"
  
  # Start the service
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SGX_IP "sudo systemctl daemon-reload && \
                                                                       sudo systemctl enable controller && \
                                                                       sudo systemctl start controller"
done

# Deploy to SEV nodes
echo "=== Deploying to SEV Nodes ==="
for SEV_IP in "${SEV_NODES[@]}"; do
  echo "Deploying to SEV Node: $SEV_IP"
  
  # Create directory structure
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SEV_IP "sudo mkdir -p /opt/rhombus/execution/{bin,config,logs}"
  
  # Copy files
  scp -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem -r "${BUILD_DIR}/bin/"* ubuntu@$SEV_IP:/tmp/
  scp -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem -r "${BUILD_DIR}/config/"* ubuntu@$SEV_IP:/tmp/
  scp -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem "${BUILD_DIR}/controller.service" ubuntu@$SEV_IP:/tmp/
  
  # Configure for SEV
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SEV_IP "sudo sed 's/PLACEHOLDER/SEV/g' /tmp/controller_config.json > /tmp/sev_controller_config.json"
  
  # Move files and set permissions
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SEV_IP "sudo cp /tmp/controller /tmp/coordinator /opt/rhombus/execution/bin/ && \
                                                                      sudo cp /tmp/*.json /opt/rhombus/execution/config/ && \
                                                                      sudo cp /tmp/sev_controller_config.json /opt/rhombus/execution/config/controller_config.json && \
                                                                      sudo cp /tmp/controller.service /etc/systemd/system/ && \
                                                                      sudo chmod +x /opt/rhombus/execution/bin/*"
  
  # Install dependencies if needed
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SEV_IP "sudo apt-get update && sudo apt-get install -y libssl-dev"
  
  # Start the service
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SEV_IP "sudo systemctl daemon-reload && \
                                                                     sudo systemctl enable controller && \
                                                                     sudo systemctl start controller"
done

# Deploy coordinator to the first SGX node and configure it
COORDINATOR_IP=${SGX_NODES[0]}
echo "=== Deploying Coordinator to $COORDINATOR_IP ==="

# Create coordinator service file
cat > "${BUILD_DIR}/coordinator.service" << EOF
[Unit]
Description=TEE Execution Coordinator
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/rhombus/execution
ExecStart=/opt/rhombus/execution/bin/coordinator --config /opt/rhombus/execution/config/coordinator_config.json
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

# Create coordinator configuration
cat > "${BUILD_DIR}/config/coordinator_config.json" << EOF
{
  "listen_address": "0.0.0.0:8080",
  "region_id": "us-east-1",
  "timestamp_signing": {
    "enabled": true,
    "algorithm": "ed25519",
    "verification_required": true
  }
}
EOF

# Copy and configure coordinator
scp -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem "${BUILD_DIR}/coordinator.service" ubuntu@$COORDINATOR_IP:/tmp/
scp -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem "${BUILD_DIR}/config/coordinator_config.json" ubuntu@$COORDINATOR_IP:/tmp/

# Install and start coordinator service
ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$COORDINATOR_IP "sudo cp /tmp/coordinator_config.json /opt/rhombus/execution/config/ && \
                                                                           sudo cp /tmp/coordinator.service /etc/systemd/system/ && \
                                                                           sudo systemctl daemon-reload && \
                                                                           sudo systemctl enable coordinator && \
                                                                           sudo systemctl start coordinator"

# Update all controller configurations to point to coordinator
for ((i=0; i<${#SGX_NODES[@]}; i++)); do
  SGX_IP=${SGX_NODES[$i]}
  SEV_IP=${SEV_NODES[$i]}
  
  echo "Configuring SGX Node $SGX_IP to use Coordinator $COORDINATOR_IP"
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SGX_IP "sudo sed -i 's/\"coordinator_url\": \"PLACEHOLDER\"/\"coordinator_url\": \"http:\/\/$COORDINATOR_IP:8080\"/g' /opt/rhombus/execution/config/controller_config.json && \
                                                                       sudo systemctl restart controller"
  
  echo "Configuring SEV Node $SEV_IP to use Coordinator $COORDINATOR_IP"
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SEV_IP "sudo sed -i 's/\"coordinator_url\": \"PLACEHOLDER\"/\"coordinator_url\": \"http:\/\/$COORDINATOR_IP:8080\"/g' /opt/rhombus/execution/config/controller_config.json && \
                                                                     sudo systemctl restart controller"
done

echo "=== Waiting for services to initialize (30 seconds) ==="
sleep 30

# Final check: verify controllers and coordinator are running
echo "=== Verifying Services ==="
COORDINATOR_STATUS=$(ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$COORDINATOR_IP "sudo systemctl is-active coordinator")
echo "Coordinator status: $COORDINATOR_STATUS"

for SGX_IP in "${SGX_NODES[@]}"; do
  SGX_STATUS=$(ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SGX_IP "sudo systemctl is-active controller")
  echo "SGX Controller status at $SGX_IP: $SGX_STATUS"
done

for SEV_IP in "${SEV_NODES[@]}"; do
  SEV_STATUS=$(ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SEV_IP "sudo systemctl is-active controller")
  echo "SEV Controller status at $SEV_IP: $SEV_STATUS"
done

echo "=== Execution Submodule Deployment Complete ==="
echo "Your TEE nodes are now configured with dual-format parameter validation and cross-attestation"
echo "The coordinator-based timestamp implementation is running at $COORDINATOR_IP:8080"
echo "Ready for NASDAQ market data simulation testing"
