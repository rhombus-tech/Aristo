#!/bin/bash
# Deploy RSA accumulator service with dual-format parameter validation for NASDAQ TEE infrastructure
# This script deploys the high-performance RSA accumulator service alongside the parameter validators

set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# NASDAQ SSH Key path
SSH_KEY="~/nasdaq-tee-key.pem"
SSH_OPTS="-o StrictHostKeyChecking=no -o ConnectTimeout=10 -i $(eval echo $SSH_KEY)"

echo -e "${GREEN}=== Deploying RSA Accumulator Service with Dual-Format Parameter Support ===${NC}"

# Hard-coded node information (same as parameter validators)
declare -a NODES=(
  "54.172.109.130,SGX,1"  # SGX Node 1
  "54.236.21.15,SEV,1"    # SEV Node 1
)

# Define ports for RSA accumulator service (different from validator ports)
SGX_ACCUMULATOR_PORT=7100  # Parameter validator is on 7090
SEV_ACCUMULATOR_PORT=7101  # Parameter validator is on 7091

# Function to build Go RSA accumulator with dual-format parameter support
build_sgx_rsa_accumulator() {
  echo -e "${YELLOW}Building Go RSA accumulator for SGX node with dual-format parameter support...${NC}"
  cd "$(dirname "$0")/../../accumulator"
  
  # Ensure we have all dependencies
  go mod tidy
  
  # Build with dual-format parameter validation flags
  go build -o rsa_accumulator_sgx -tags "sgx_support" \
    -ldflags "-X main.supportLengthPrefix=true -X main.supportDirectFormat=true -X main.parallelism=8 -X main.port=${SGX_ACCUMULATOR_PORT}" \
    ./cmd/high_perf_rsa/main.go
    
  if [ $? -ne 0 ]; then
    echo -e "${RED}Failed to build SGX RSA accumulator${NC}"
    exit 1
  fi
  
  echo -e "${GREEN}Successfully built SGX RSA accumulator${NC}"
}

# Function to build Rust RSA accumulator for SEV node
build_sev_rsa_accumulator() {
  echo -e "${YELLOW}Building Rust RSA accumulator for SEV node with dual-format parameter support...${NC}"
  cd "$(dirname "$0")/../../../execution/accumulator"
  
  # Ensure we have all dependencies
  cargo update
  
  # Build with dual-format parameter validation feature flags
  cargo build --release --features "length_prefix_support,direct_format_support" -p rsa-accumulator-service
  
  if [ $? -ne 0 ]; then
    echo -e "${RED}Failed to build SEV RSA accumulator${NC}"
    exit 1
  fi
  
  echo -e "${GREEN}Successfully built SEV RSA accumulator${NC}"
}

# Function to deploy RSA accumulator to SGX node
deploy_sgx_accumulator() {
  SGX_NODE=$(echo "${NODES[0]}" | cut -d',' -f1)
  
  echo -e "${YELLOW}Deploying RSA accumulator to SGX node (${SGX_NODE})...${NC}"
  
  # Create deployment directory
  ssh $SSH_OPTS ubuntu@${SGX_NODE} "mkdir -p ~/nasdaq-tee/rsa-accumulator"
  
  # Copy binary and configuration
  scp $SSH_OPTS "$(dirname "$0")/../../accumulator/rsa_accumulator_sgx" ubuntu@${SGX_NODE}:~/nasdaq-tee/rsa-accumulator/
  
  # Create environment file with dual-format config
  ssh $SSH_OPTS ubuntu@${SGX_NODE} "cat > ~/nasdaq-tee/rsa-accumulator/config.env << EOF
PORT=${SGX_ACCUMULATOR_PORT}
SUPPORT_LENGTH_PREFIX=true
SUPPORT_DIRECT_FORMAT=true
BATCH_SIZE=1000
PARALLELISM=8
VALIDATOR_ENDPOINT=http://localhost:7090
PAIR_ID=nasdaq-poc-1
EOF"

  echo -e "${GREEN}Successfully deployed RSA accumulator to SGX node${NC}"
}

# Function to deploy RSA accumulator to SEV node
deploy_sev_accumulator() {
  SEV_NODE=$(echo "${NODES[1]}" | cut -d',' -f1)
  
  echo -e "${YELLOW}Deploying RSA accumulator to SEV node (${SEV_NODE})...${NC}"
  
  # Create deployment directory
  ssh $SSH_OPTS ubuntu@${SEV_NODE} "mkdir -p ~/nasdaq-tee/rsa-accumulator"
  
  # Copy binary and configuration
  scp $SSH_OPTS "$(dirname "$0")/../../../execution/accumulator/target/release/rsa-accumulator-service" ubuntu@${SEV_NODE}:~/nasdaq-tee/rsa-accumulator/
  
  # Create environment file with dual-format config
  ssh $SSH_OPTS ubuntu@${SEV_NODE} "cat > ~/nasdaq-tee/rsa-accumulator/config.env << EOF
PORT=${SEV_ACCUMULATOR_PORT}
SUPPORT_LENGTH_PREFIX=true
SUPPORT_DIRECT_FORMAT=true
BATCH_SIZE=1000
PARALLELISM=8
VALIDATOR_ENDPOINT=http://localhost:7091
PAIR_ID=nasdaq-poc-1
EOF"

  echo -e "${GREEN}Successfully deployed RSA accumulator to SEV node${NC}"
}

# Function to start RSA accumulator service on SGX node
start_sgx_accumulator() {
  SGX_NODE=$(echo "${NODES[0]}" | cut -d',' -f1)
  
  echo -e "${YELLOW}Starting RSA accumulator service on SGX node (${SGX_NODE})...${NC}"
  
  # Stop any existing service
  ssh $SSH_OPTS ubuntu@${SGX_NODE} "pkill -f rsa_accumulator_sgx || true"
  
  # Start service with nohup
  ssh $SSH_OPTS ubuntu@${SGX_NODE} "cd ~/nasdaq-tee/rsa-accumulator && source config.env && nohup ./rsa_accumulator_sgx > accumulator.log 2>&1 &"
  
  # Check if service is running
  sleep 2
  ssh $SSH_OPTS ubuntu@${SGX_NODE} "pgrep -f rsa_accumulator_sgx"
  if [ $? -ne 0 ]; then
    echo -e "${RED}Failed to start RSA accumulator service on SGX node${NC}"
    exit 1
  fi
  
  echo -e "${GREEN}Successfully started RSA accumulator service on SGX node${NC}"
  ssh $SSH_OPTS ubuntu@${SGX_NODE} "tail -n 10 ~/nasdaq-tee/rsa-accumulator/accumulator.log"
}

# Function to start RSA accumulator service on SEV node
start_sev_accumulator() {
  SEV_NODE=$(echo "${NODES[1]}" | cut -d',' -f1)
  
  echo -e "${YELLOW}Starting RSA accumulator service on SEV node (${SEV_NODE})...${NC}"
  
  # Stop any existing service
  ssh $SSH_OPTS ubuntu@${SEV_NODE} "pkill -f rsa-accumulator-service || true"
  
  # Start service with nohup
  ssh $SSH_OPTS ubuntu@${SEV_NODE} "cd ~/nasdaq-tee/rsa-accumulator && source config.env && nohup ./rsa-accumulator-service > accumulator.log 2>&1 &"
  
  # Check if service is running
  sleep 2
  ssh $SSH_OPTS ubuntu@${SEV_NODE} "pgrep -f rsa-accumulator-service"
  if [ $? -ne 0 ]; then
    echo -e "${RED}Failed to start RSA accumulator service on SEV node${NC}"
    exit 1
  fi
  
  echo -e "${GREEN}Successfully started RSA accumulator service on SEV node${NC}"
  ssh $SSH_OPTS ubuntu@${SEV_NODE} "tail -n 10 ~/nasdaq-tee/rsa-accumulator/accumulator.log"
}

# Function to verify RSA accumulator services are running
verify_services() {
  SGX_NODE=$(echo "${NODES[0]}" | cut -d',' -f1)
  SEV_NODE=$(echo "${NODES[1]}" | cut -d',' -f1)
  
  echo -e "${YELLOW}Verifying RSA accumulator services...${NC}"
  
  # Test SGX node
  echo -e "Testing SGX RSA accumulator (${SGX_NODE}:${SGX_ACCUMULATOR_PORT})..."
  ssh $SSH_OPTS ubuntu@${SGX_NODE} "curl -s -X POST -d 'test-parameter' http://localhost:${SGX_ACCUMULATOR_PORT}/accumulate"
  if [ $? -ne 0 ]; then
    echo -e "${RED}SGX RSA accumulator is not responding${NC}"
  else
    echo -e "${GREEN}SGX RSA accumulator is running${NC}"
  fi
  
  # Test SEV node
  echo -e "Testing SEV RSA accumulator (${SEV_NODE}:${SEV_ACCUMULATOR_PORT})..."
  ssh $SSH_OPTS ubuntu@${SEV_NODE} "curl -s -X POST -d 'test-parameter' http://localhost:${SEV_ACCUMULATOR_PORT}/accumulate"
  if [ $? -ne 0 ]; then
    echo -e "${RED}SEV RSA accumulator is not responding${NC}"
  else
    echo -e "${GREEN}SEV RSA accumulator is running${NC}"
  fi
  
  echo -e "${GREEN}Verification complete${NC}"
}

# Main execution
echo -e "${GREEN}Starting RSA Accumulator deployment process...${NC}"

# Build RSA accumulators
build_sgx_rsa_accumulator
build_sev_rsa_accumulator

# Deploy RSA accumulators
deploy_sgx_accumulator
deploy_sev_accumulator

# Start RSA accumulators
start_sgx_accumulator
start_sev_accumulator

# Verify services
verify_services

echo -e "${GREEN}RSA Accumulator deployment completed successfully!${NC}"
echo -e "SGX RSA Accumulator: $(echo "${NODES[0]}" | cut -d',' -f1):${SGX_ACCUMULATOR_PORT}"
echo -e "SEV RSA Accumulator: $(echo "${NODES[1]}" | cut -d',' -f1):${SEV_ACCUMULATOR_PORT}"
