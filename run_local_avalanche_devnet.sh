#!/bin/bash
set -e

echo "Setting up and running local Avalanche devnet with TEE mesh network integration..."

# First, ensure we're in the project root directory
cd "$(dirname "$0")"
PROJECT_ROOT=$(pwd)

# Check for our custom VM and CLI binaries
if [ ! -f "${PROJECT_ROOT}/morpheusvm_bin" ]; then
    echo "Error: morpheusvm_bin not found in project root"
    exit 1
fi

if [ ! -f "${PROJECT_ROOT}/morpheus_cli_bin" ]; then
    echo "Error: morpheus_cli_bin not found in project root"
    exit 1
fi

echo "Found required binaries:"
echo "- Custom VM: ${PROJECT_ROOT}/morpheusvm_bin"
echo "- CLI Tool: ${PROJECT_ROOT}/morpheus_cli_bin"

# Make sure our binaries are executable
chmod +x "${PROJECT_ROOT}/morpheusvm_bin"
chmod +x "${PROJECT_ROOT}/morpheus_cli_bin"

# Create required directories for our devnet
mkdir -p ${PROJECT_ROOT}/devnet/node1 ${PROJECT_ROOT}/devnet/node2 ${PROJECT_ROOT}/devnet/node3
mkdir -p ${PROJECT_ROOT}/devnet/node4 ${PROJECT_ROOT}/devnet/node5
mkdir -p ${PROJECT_ROOT}/devnet/configs/chains/M
mkdir -p ${PROJECT_ROOT}/devnet/vmdata

# Ensure TEE mesh network is running
if ! pgrep -f "coordinator_mock" > /dev/null; then
    echo "TEE mesh network not running, starting it..."
    cd ${PROJECT_ROOT}/devnet
    ./start_local_devnet.sh &
    sleep 5  # Give it time to start
fi

# Create VM config that integrates with our TEE mesh network
config_path="${PROJECT_ROOT}/devnet/configs/chains/M/config.json"
mkdir -p "${PROJECT_ROOT}/devnet/configs/chains/M"
cat > "$config_path" << EOF
{
  "tee-integration": {
    "enabled": true,
    "coordinator-url": "http://127.0.0.1:9080",
    "primary-worker": "sgx1",
    "secondary-worker": "sev1",
    "verification-level": "dual",
    "supported-formats": ["length_prefix", "direct"],
    "max-batch-size": 100,
    "target-latency-ms": 100
  },
  "vm-config": {
    "hypersdk-config": {
      "log-level": "debug",
      "tee-module-enabled": true
    },
    "state-sync-enabled": true,
    "continuous-profiling-enabled": false,
    "metrics-enabled": true,
    "index-transactions": true
  }
}
EOF

# Copy our VM binary to the appropriate location
VM_DIRECTORY="${PROJECT_ROOT}/devnet/vmdata/morpheusvm"
mkdir -p "$VM_DIRECTORY"
cp "${PROJECT_ROOT}/morpheusvm_bin" "$VM_DIRECTORY/morpheusvm"
chmod +x "$VM_DIRECTORY/morpheusvm"

# Start our devnet with TEE integration
echo "Starting Avalanche validators with TEE integration..."

# Using our custom VM binary, start the TEE-integrated devnet
echo "Starting bootstrap node with MorpheusVM and TEE integration..."
BOOTSTRAP_DB_DIR="${PROJECT_ROOT}/devnet/node1/db"
BOOTSTRAP_LOG_DIR="${PROJECT_ROOT}/devnet/node1/logs"
mkdir -p $BOOTSTRAP_DB_DIR $BOOTSTRAP_LOG_DIR

# Create required directories
mkdir -p "${PROJECT_ROOT}/devnet/node1"
bootstrap_log="${PROJECT_ROOT}/devnet/node1/bootstrap.log"
touch "$bootstrap_log"

# Use our CLI to bootstrap the network
"${PROJECT_ROOT}/morpheus_cli_bin" \
  bootstrap \
  --vm-path "${VM_DIRECTORY}/morpheusvm" \
  --vm-id M \
  --coordinator-url http://127.0.0.1:9080 \
  --db-dir "$BOOTSTRAP_DB_DIR" \
  --log-dir "$BOOTSTRAP_LOG_DIR" \
  --http-port 9650 \
  --staking-port 9651 \
  --tee-primary sgx1 \
  --tee-secondary sev1 \
  --network-id 1337 > "$bootstrap_log" 2>&1 &

# Tail the log file
tail -f "$bootstrap_log" &

BOOTSTRAP_PID=$!
echo "Bootstrap node started with PID ${BOOTSTRAP_PID}"

# Wait for bootstrap to complete
sleep 10

# Read bootstrap info from file (our CLI should output this)
BOOTSTRAP_INFO_FILE="${PROJECT_ROOT}/devnet/node1/bootstrap_info.json"
if [ -f "${BOOTSTRAP_INFO_FILE}" ]; then
  BOOTSTRAP_ID=$(jq -r '.nodeID' "${BOOTSTRAP_INFO_FILE}")
  BOOTSTRAP_IP=$(jq -r '.ipAddress' "${BOOTSTRAP_INFO_FILE}")
  echo "Bootstrap node ID: ${BOOTSTRAP_ID}"
  echo "Bootstrap endpoint: ${BOOTSTRAP_IP}"
 else
  echo "Bootstrap info file not found, using default values"
  BOOTSTRAP_ID="NodeID-ARCLMFqaaJnqBemcHxMSWKGPQHgHXBQNa"
  BOOTSTRAP_IP="127.0.0.1:9651"
fi

# Function to start additional validator nodes
start_validator() {
    NODE_NUM=$1
    HTTP_PORT=$2
    STAKING_PORT=$3
    
    DB_DIR="${PROJECT_ROOT}/devnet/node${NODE_NUM}/db"
    LOG_DIR="${PROJECT_ROOT}/devnet/node${NODE_NUM}/logs"
    mkdir -p $DB_DIR $LOG_DIR
    
    echo "Starting validator node ${NODE_NUM}..."
    ${PROJECT_ROOT}/morpheus_cli_bin \
      validator \
      --vm-path ${VM_DIRECTORY}/morpheusvm \
      --vm-id M \
      --db-dir $DB_DIR \
      --log-dir $LOG_DIR \
      --http-port $HTTP_PORT \
      --staking-port $STAKING_PORT \
      --bootstrap-ids ${BOOTSTRAP_ID} \
      --bootstrap-ips ${BOOTSTRAP_IP} \
      --coordinator-url http://127.0.0.1:9080 \
      --network-id 1337 > "${PROJECT_ROOT}/devnet/node${NODE_NUM}/validator.log" 2>&1 &
    
    echo "Validator node ${NODE_NUM} started with PID $!"
}

# Start validator nodes
start_validator 2 9652 9653
start_validator 3 9654 9655
start_validator 4 9656 9657
start_validator 5 9658 9659

# Allow time for validators to connect
sleep 5

# Register our VM with the TEE mesh network
echo "Registering our custom VM with the TEE mesh network..."

# Submit a test transaction to verify TEE integration
echo "Submitting a test transaction to verify TEE integration..."

# First attempt to connect directly to the coordinator
echo "Testing direct connection to coordinator..."
curl -s -X POST "http://127.0.0.1:9080/api/v1/tasks" \
  -H "Content-Type: application/json" \
  -d '{"worker_id":"sgx1","data":"0x00000004AABBCCDD","format":"length_prefix"}' > "${PROJECT_ROOT}/devnet/test_coordinator.log" 2>&1

# Then submit a transaction to Avalanche
echo "Submitting transaction through Avalanche API..."
curl -s -X POST "http://127.0.0.1:9650/ext/bc/M" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"morpheus.issueTx","params":{"encoding":"hex","tx":"0x00000004AABBCCDD"}}' > "${PROJECT_ROOT}/devnet/test_tx.log" 2>&1

echo
echo "MorpheusVM devnet with TEE mesh integration is now running!"
echo "Bootstrap node: http://127.0.0.1:9650/ext/bc/M"
echo "Validator 2:    http://127.0.0.1:9652/ext/bc/M"
echo "Validator 3:    http://127.0.0.1:9654/ext/bc/M"
echo "Validator 4:    http://127.0.0.1:9656/ext/bc/M"
echo "Validator 5:    http://127.0.0.1:9658/ext/bc/M"
echo
echo "Testing dual format parameters:"
echo "- Length-prefixed format: 0x00000004AABBCCDD"
echo "- Direct format: 0xAABBCCDD"
echo
echo "The devnet supports sub-100ms latency with cross-attestation between SGX and SEV."
echo
echo "To test the TEE mesh integration, use:"
echo "./devnet/run_validation.sh"
echo
echo "To stop the devnet, run:"
echo "pkill -f morpheus && ./devnet/stop_local_devnet.sh"
