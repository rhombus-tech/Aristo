#!/bin/bash
# Build and deploy TEE controllers directly on the target machines
# This addresses the binary compatibility issue

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
KEY_NAME="nasdaq-tee-key"
PAIR_CONFIG_DIR="/Users/talzisckind/Downloads/aristo-fresh 2/tee/deployment/aws/pair_configs"
PROJECT_DIR="/Users/talzisckind/Downloads/aristo-fresh 2"
SSH_OPTS="-o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem"

# Create pair config directory if it doesn't exist
mkdir -p "${PAIR_CONFIG_DIR}"

# Hard-code node IPs based on previous deployment
SGX_NODES=("54.172.109.130" "54.224.222.120")
SEV_NODES=("54.236.21.15" "3.91.253.73")
PAIR_IDS=("2" "1")

echo -e "${BLUE}=== Creating TEE Pair Configurations ===${NC}"

for i in {0..1}; do
  SGX_IP=${SGX_NODES[$i]}
  SEV_IP=${SEV_NODES[$i]}
  PAIR_ID=${PAIR_IDS[$i]}
  
  echo "Creating config for Pair $PAIR_ID:"
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
done

echo -e "${BLUE}=== Preparing Source Code for Remote Build ===${NC}"

# Create build script to run on target machines
cat > /tmp/build_controller.sh << 'EOF'
#!/bin/bash
set -e

# Install dependencies if not already installed
if ! command -v rustc &> /dev/null; then
  echo "Installing Rust..."
  curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
  source $HOME/.cargo/env
fi

sudo apt-get update
sudo apt-get install -y git build-essential pkg-config libssl-dev

# Set up build directory
BUILD_DIR=$HOME/tee-build
mkdir -p $BUILD_DIR
cd $BUILD_DIR

# Clone the repository or update it if it exists
if [ -d "execution" ]; then
  echo "Updating existing code..."
  cd execution
  git pull
else
  echo "Cloning repository..."
  # We'll use the tarball of the source code instead of git clone
  mkdir -p execution
fi

# Build the controller
cd $BUILD_DIR/execution
echo "Building controller..."
source $HOME/.cargo/env
cargo build --release --bin tee-controller

echo "Build completed successfully"
EOF

chmod +x /tmp/build_controller.sh

# Create deployment script to run on target machines
cat > /tmp/setup_controller.sh << 'EOF'
#!/bin/bash
set -e

NODE_TYPE=$1
PARTNER_IP=$2
PARTNER_TYPE=$3
NODE_ID=$4

# Set up directories
sudo mkdir -p /opt/rhombus/execution/{bin,config,logs}

# Copy built binary
sudo cp $HOME/tee-build/execution/target/release/tee-controller /opt/rhombus/execution/bin/controller
sudo chmod +x /opt/rhombus/execution/bin/controller

# Create controller config
cat > /tmp/controller_config.json << EOFINNER
{
  "listen_address": "0.0.0.0:7070",
  "tee_type": "$NODE_TYPE",
  "node_id": "$NODE_ID",
  "partner_tee": {
    "ip": "$PARTNER_IP",
    "tee_type": "$PARTNER_TYPE",
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
EOFINNER

sudo cp /tmp/controller_config.json /opt/rhombus/execution/config/

# Create service file
cat > /tmp/controller.service << EOFINNER
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
EOFINNER

sudo cp /tmp/controller.service /etc/systemd/system/

# Start the service
sudo systemctl daemon-reload
sudo systemctl enable controller
sudo systemctl restart controller

echo "Controller setup completed for $NODE_TYPE node"
EOF

chmod +x /tmp/setup_controller.sh

echo -e "${BLUE}=== Creating Source Code Package ===${NC}"

# Create a source code package to upload
cd "$PROJECT_DIR"
tar -czf /tmp/execution_source.tar.gz execution/

echo -e "${BLUE}=== Deploying to TEE Nodes ===${NC}"

for i in {0..1}; do
  SGX_IP=${SGX_NODES[$i]}
  SEV_IP=${SEV_NODES[$i]}
  PAIR_ID=${PAIR_IDS[$i]}
  
  echo -e "${YELLOW}Deploying to Pair $PAIR_ID:${NC}"
  
  # Deploy to SGX Node
  echo "Uploading source code to SGX Node ($SGX_IP)..."
  scp $SSH_OPTS /tmp/execution_source.tar.gz ubuntu@$SGX_IP:~/
  scp $SSH_OPTS /tmp/build_controller.sh ubuntu@$SGX_IP:~/
  scp $SSH_OPTS /tmp/setup_controller.sh ubuntu@$SGX_IP:~/
  
  echo "Extracting source code on SGX Node..."
  ssh $SSH_OPTS ubuntu@$SGX_IP "mkdir -p ~/tee-build/execution && tar -xzf ~/execution_source.tar.gz -C ~/tee-build"
  
  echo "Building controller on SGX Node (this may take a few minutes)..."
  ssh $SSH_OPTS ubuntu@$SGX_IP "bash ~/build_controller.sh"
  
  echo "Setting up controller service on SGX Node..."
  ssh $SSH_OPTS ubuntu@$SGX_IP "bash ~/setup_controller.sh IntelSGX $SEV_IP SEV sgx-$PAIR_ID"
  
  # Deploy to SEV Node
  echo "Uploading source code to SEV Node ($SEV_IP)..."
  scp $SSH_OPTS /tmp/execution_source.tar.gz ubuntu@$SEV_IP:~/
  scp $SSH_OPTS /tmp/build_controller.sh ubuntu@$SEV_IP:~/
  scp $SSH_OPTS /tmp/setup_controller.sh ubuntu@$SEV_IP:~/
  
  echo "Extracting source code on SEV Node..."
  ssh $SSH_OPTS ubuntu@$SEV_IP "mkdir -p ~/tee-build/execution && tar -xzf ~/execution_source.tar.gz -C ~/tee-build"
  
  echo "Building controller on SEV Node (this may take a few minutes)..."
  ssh $SSH_OPTS ubuntu@$SEV_IP "bash ~/build_controller.sh"
  
  echo "Setting up controller service on SEV Node..."
  ssh $SSH_OPTS ubuntu@$SEV_IP "bash ~/setup_controller.sh SEV $SGX_IP IntelSGX sev-$PAIR_ID"
done

echo -e "${BLUE}=== Waiting for services to initialize (15 seconds) ===${NC}"
sleep 15

# Final check: verify controllers are running
echo -e "${BLUE}=== Verifying TEE Controller Services ===${NC}"
for i in {0..1}; do
  SGX_IP=${SGX_NODES[$i]}
  SEV_IP=${SEV_NODES[$i]}
  PAIR_ID=${PAIR_IDS[$i]}
  
  echo -e "${YELLOW}Pair $PAIR_ID:${NC}"
  
  # Check SGX node
  echo -n "  SGX Controller status: "
  SGX_STATUS=$(ssh $SSH_OPTS ubuntu@$SGX_IP "sudo systemctl is-active controller")
  if [ "$SGX_STATUS" == "active" ]; then
    echo -e "${GREEN}Running${NC}"
  else
    echo -e "${RED}Not Running (status: $SGX_STATUS)${NC}"
    echo "  Checking logs:"
    ssh $SSH_OPTS ubuntu@$SGX_IP "sudo journalctl -u controller -n 5 --no-pager"
  fi
  
  # Check SEV node
  echo -n "  SEV Controller status: "
  SEV_STATUS=$(ssh $SSH_OPTS ubuntu@$SEV_IP "sudo systemctl is-active controller")
  if [ "$SEV_STATUS" == "active" ]; then
    echo -e "${GREEN}Running${NC}"
  else
    echo -e "${RED}Not Running (status: $SEV_STATUS)${NC}"
    echo "  Checking logs:"
    ssh $SSH_OPTS ubuntu@$SEV_IP "sudo journalctl -u controller -n 5 --no-pager"
  fi
  
  echo ""
done

echo -e "${BLUE}=== TEE Controller Deployment Complete ===${NC}"
echo "Your TEE controllers have been built directly on the target machines,"
echo "configured with dual-format parameter validation and cross-attestation,"
echo "and deployed as system services."
