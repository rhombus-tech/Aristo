#!/bin/bash
# TEE Node Setup Script for Azure Deployment
# This script sets up and configures a TEE node (Intel SGX or AMD SEV)
# Usage: ./setup_tee_node.sh [SGX|SEV] [PAIR_INDEX]

set -e

TEE_TYPE=$1
PAIR_INDEX=$2
REPO_DIR="$(pwd)"
MESH_PORT=8080
NASDAQ_PORT=9000
LOG_DIR="/var/log/aristo-tee"
CONFIG_DIR="/etc/aristo-tee"

# Validate inputs
if [[ "$TEE_TYPE" != "SGX" && "$TEE_TYPE" != "SEV" ]]; then
    echo "Error: TEE type must be either SGX or SEV"
    exit 1
fi

if [[ -z "$PAIR_INDEX" ]]; then
    echo "Error: PAIR_INDEX must be specified"
    exit 1
fi

echo "Setting up $TEE_TYPE TEE node for pair $PAIR_INDEX"

# Create directories
sudo mkdir -p $LOG_DIR
sudo mkdir -p $CONFIG_DIR
sudo chown $(whoami):$(whoami) $LOG_DIR
sudo chown $(whoami):$(whoami) $CONFIG_DIR

# Install required packages
echo "Installing dependencies..."
if [ "$TEE_TYPE" == "SGX" ]; then
    sudo apt-get update
    sudo apt-get install -y build-essential git python3-pip python3-dev \
        libsgx-dcap-ql libsgx-dcap-default-qpl sgx-aesm-service \
        libsgx-enclave-common libsgx-urts libsgx-uae-service \
        docker.io docker-compose
    
    # Start SGX services
    sudo systemctl start aesmd
    sudo systemctl enable aesmd
elif [ "$TEE_TYPE" == "SEV" ]; then
    sudo apt-get update
    sudo apt-get install -y build-essential git python3-pip python3-dev \
        docker.io docker-compose amd-sev-snp-platform
fi

# Setup Python environment
echo "Setting up Python environment..."
python3 -m pip install --upgrade pip
python3 -m pip install -r $REPO_DIR/tee/integration/nasdaq/requirements.txt

# Generate node configuration
NODE_ID="${TEE_TYPE}-${PAIR_INDEX}"
PAIR_ID="pair-${PAIR_INDEX}"

# Determine paired node type and ID
if [ "$TEE_TYPE" == "SGX" ]; then
    PAIRED_TYPE="SEV"
else
    PAIRED_TYPE="SGX"
fi
PAIRED_NODE_ID="${PAIRED_TYPE}-${PAIR_INDEX}"

# Generate configuration file
echo "Generating TEE node configuration..."
cat > $CONFIG_DIR/tee_config.json << EOF
{
    "node_id": "${NODE_ID}",
    "tee_type": "${TEE_TYPE}",
    "pair_id": "${PAIR_ID}",
    "paired_node_id": "${PAIRED_NODE_ID}",
    "paired_tee_type": "${PAIRED_TYPE}",
    "mesh_port": ${MESH_PORT},
    "nasdaq_port": ${NASDAQ_PORT},
    "attestation": {
        "report_interval_ms": 5000,
        "verification_timeout_ms": 100,
        "max_verification_attempts": 3
    },
    "runtime": {
        "log_level": "info",
        "performance_mode": true,
        "constant_time_ops": true,
        "max_connections": 1000
    }
}
EOF

# Create environment file for containerized deployment
cat > $CONFIG_DIR/tee.env << EOF
TEE_NODE_ID=${NODE_ID}
TEE_TYPE=${TEE_TYPE}
TEE_PAIR_ID=${PAIR_ID}
PAIRED_NODE_ID=${PAIRED_NODE_ID}
PAIRED_TEE_TYPE=${PAIRED_TYPE}
MESH_PORT=${MESH_PORT}
NASDAQ_PORT=${NASDAQ_PORT}
LOG_LEVEL=info
PERFORMANCE_MODE=true
CONSTANT_TIME_OPS=true
MAX_CONNECTIONS=1000
EOF

# Setup service for the TEE node
echo "Setting up systemd service..."
cat > /tmp/aristo-tee.service << EOF
[Unit]
Description=Aristo TEE ${TEE_TYPE} Node
After=network.target

[Service]
Type=simple
User=$(whoami)
WorkingDirectory=${REPO_DIR}
EnvironmentFile=${CONFIG_DIR}/tee.env
ExecStart=${REPO_DIR}/tee/integration/nasdaq/run_with_config.sh ${CONFIG_DIR}/tee_config.json
Restart=on-failure
RestartSec=5s
StandardOutput=append:${LOG_DIR}/tee-node.log
StandardError=append:${LOG_DIR}/tee-node-error.log

[Install]
WantedBy=multi-user.target
EOF

sudo mv /tmp/aristo-tee.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable aristo-tee.service

# Setup NASDAQ ITCH simulation service (only on SGX nodes for this setup)
if [ "$TEE_TYPE" == "SGX" ]; then
    echo "Setting up NASDAQ ITCH simulation service..."
    cat > /tmp/aristo-nasdaq-sim.service << EOF
[Unit]
Description=Aristo NASDAQ ITCH Simulator
After=aristo-tee.service

[Service]
Type=simple
User=$(whoami)
WorkingDirectory=${REPO_DIR}
EnvironmentFile=${CONFIG_DIR}/tee.env
ExecStart=${REPO_DIR}/tee/integration/nasdaq/run_itch_simulation.sh
Restart=on-failure
RestartSec=5s
StandardOutput=append:${LOG_DIR}/nasdaq-sim.log
StandardError=append:${LOG_DIR}/nasdaq-sim-error.log

[Install]
WantedBy=multi-user.target
EOF

    sudo mv /tmp/aristo-nasdaq-sim.service /etc/systemd/system/
    sudo systemctl daemon-reload
    sudo systemctl enable aristo-nasdaq-sim.service
fi

# Generate mesh network configuration
echo "Generating mesh network configuration..."
MESH_CONFIG_FILE=$CONFIG_DIR/mesh_config.json
cat > $MESH_CONFIG_FILE << EOF
{
    "node_id": "${NODE_ID}",
    "tee_type": "${TEE_TYPE}",
    "listen_port": ${MESH_PORT},
    "attestation_interval_ms": 5000,
    "mesh_nodes": [
        {
            "node_id": "${PAIRED_NODE_ID}",
            "tee_type": "${PAIRED_TYPE}",
            "host": "${PAIRED_NODE_ID}",
            "port": ${MESH_PORT}
        }
    ],
    "verification": {
        "timeout_ms": 100,
        "max_retry_attempts": 3,
        "constant_time": true
    }
}
EOF

# Start the services
echo "Starting services..."
sudo systemctl start aristo-tee.service
if [ "$TEE_TYPE" == "SGX" ]; then
    sudo systemctl start aristo-nasdaq-sim.service
fi

echo "TEE node setup complete. Services started."
echo "Check logs at $LOG_DIR for operation details."
