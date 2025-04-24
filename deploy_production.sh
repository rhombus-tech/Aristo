#!/bin/bash
# deploy_production.sh - Production deployment script for RSA Accumulator
# Deploys optimized accumulator proxy with dual-format parameter validation

set -e

# Production configuration
SGX_NODES=("ec2-54-225-41-220.compute-1.amazonaws.com" "ec2-52-54-181-245.compute-1.amazonaws.com")
SEV_NODES=("ec2-3-81-65-148.compute-1.amazonaws.com" "ec2-54-161-2-145.compute-1.amazonaws.com")
KEY_FILE="$HOME/nasdaq-tee-key.pem"
SSH_USER="ubuntu"
ACCUMULATOR_PORT=7101

# Build optimized binaries
echo "Building optimized production binaries..."
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/accumulator_proxy_sgx ./cmd/accumulator_proxy/
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/accumulator_proxy_sev ./cmd/accumulator_proxy/

# Verify WebAssembly module
if [ ! -f "bin/rsa_accumulator.wasm" ]; then
  echo "ERROR: WebAssembly module not found at bin/rsa_accumulator.wasm"
  exit 1
fi

# Create paired node configs
echo "Creating cross-validation configurations..."
mkdir -p deploy/sgx deploy/sev

# Create SGX config with SEV peer for cross-validation
cat > deploy/sgx/config.env << EOF
PORT=${ACCUMULATOR_PORT}
TEE_TYPE=sgx
TEE_ID=prod-sgx-accumulator
ENABLE_CROSS_VALIDATE=true
PEER_ENDPOINTS=${SEV_NODES[0]}:${ACCUMULATOR_PORT}
MAX_PARAMETER_SIZE=1024
CONTRACT_ID_SIZE=32
BATCH_SIZE=250
MAX_PARALLEL_BATCHES=8
MAX_CACHE_SIZE=1024
ENABLE_LENGTH_PREFIX=true
ENABLE_DIRECT_FORMAT=true
PREFETCH=true
EOF

# Create SEV config with SGX peer for cross-validation
cat > deploy/sev/config.env << EOF
PORT=${ACCUMULATOR_PORT}
TEE_TYPE=sev
TEE_ID=prod-sev-accumulator
ENABLE_CROSS_VALIDATE=true
PEER_ENDPOINTS=${SGX_NODES[0]}:${ACCUMULATOR_PORT}
MAX_PARAMETER_SIZE=1024
CONTRACT_ID_SIZE=32
BATCH_SIZE=250
MAX_PARALLEL_BATCHES=8
MAX_CACHE_SIZE=1024
ENABLE_LENGTH_PREFIX=true
ENABLE_DIRECT_FORMAT=true
PREFETCH=true
EOF

# Copy Enarx configuration
cp execution/accumulator/enarx_config.toml deploy/sgx/
cp execution/accumulator/enarx_config.toml deploy/sev/

# Create systemd service template
cat > deploy/accumulator-proxy.service << EOF
[Unit]
Description=Wasmlanche RSA Accumulator Proxy
After=network.target

[Service]
Type=simple
User=ubuntu
WorkingDirectory=/opt/wasmlanche/accumulator
EnvironmentFile=/opt/wasmlanche/accumulator/config.env
ExecStart=/opt/wasmlanche/accumulator/accumulator_proxy --port=\${PORT} --wasm-path=/opt/wasmlanche/accumulator/rsa_accumulator.wasm --tee-type=\${TEE_TYPE} --tee-id=\${TEE_ID} --enable-cross-validate=\${ENABLE_CROSS_VALIDATE} --peer-endpoints=\${PEER_ENDPOINTS} --max-parameter-size=\${MAX_PARAMETER_SIZE} --contract-id-size=\${CONTRACT_ID_SIZE} --batch-size=\${BATCH_SIZE} --max-parallel-batches=\${MAX_PARALLEL_BATCHES} --max-cache-size=\${MAX_CACHE_SIZE} --enable-length-prefix=\${ENABLE_LENGTH_PREFIX} --enable-direct-format=\${ENABLE_DIRECT_FORMAT} --prefetch=\${PREFETCH}
Restart=always
RestartSec=5
# Performance tuning
LimitNOFILE=65535
LimitNPROC=65535
TasksMax=infinity
# Memory optimization
MemoryLow=2G
MemoryHigh=6G
# CPU priority
CPUWeight=90
IOWeight=90

[Install]
WantedBy=multi-user.target
EOF

# Deploy to SGX nodes
echo "Deploying to SGX nodes with cross-validation..."
for node in "${SGX_NODES[@]}"; do
  echo "Deploying to SGX node: $node..."
  
  # Copy files
  scp -i "$KEY_FILE" bin/accumulator_proxy_sgx "$SSH_USER@$node:~/accumulator_proxy"
  scp -i "$KEY_FILE" bin/rsa_accumulator.wasm "$SSH_USER@$node:~/rsa_accumulator.wasm"
  scp -i "$KEY_FILE" deploy/sgx/config.env "$SSH_USER@$node:~/config.env"
  scp -i "$KEY_FILE" deploy/sgx/enarx_config.toml "$SSH_USER@$node:~/enarx_config.toml"
  scp -i "$KEY_FILE" deploy/accumulator-proxy.service "$SSH_USER@$node:~/accumulator-proxy.service"
  
  # Install on remote node
  ssh -i "$KEY_FILE" "$SSH_USER@$node" << EOF
    sudo mkdir -p /opt/wasmlanche/accumulator
    sudo cp ~/accumulator_proxy /opt/wasmlanche/accumulator/
    sudo cp ~/rsa_accumulator.wasm /opt/wasmlanche/accumulator/
    sudo cp ~/config.env /opt/wasmlanche/accumulator/
    sudo cp ~/enarx_config.toml /opt/wasmlanche/accumulator/
    sudo cp ~/accumulator-proxy.service /etc/systemd/system/
    
    # Start service
    sudo systemctl daemon-reload
    sudo systemctl restart accumulator-proxy
    sudo systemctl enable accumulator-proxy
EOF
  echo "Deployed to SGX node: $node"
done

# Deploy to SEV nodes
echo "Deploying to SEV nodes with cross-validation..."
for node in "${SEV_NODES[@]}"; do
  echo "Deploying to SEV node: $node..."
  
  # Copy files
  scp -i "$KEY_FILE" bin/accumulator_proxy_sev "$SSH_USER@$node:~/accumulator_proxy"
  scp -i "$KEY_FILE" bin/rsa_accumulator.wasm "$SSH_USER@$node:~/rsa_accumulator.wasm"
  scp -i "$KEY_FILE" deploy/sev/config.env "$SSH_USER@$node:~/config.env"
  scp -i "$KEY_FILE" deploy/sev/enarx_config.toml "$SSH_USER@$node:~/enarx_config.toml"
  scp -i "$KEY_FILE" deploy/accumulator-proxy.service "$SSH_USER@$node:~/accumulator-proxy.service"
  
  # Install on remote node
  ssh -i "$KEY_FILE" "$SSH_USER@$node" << EOF
    sudo mkdir -p /opt/wasmlanche/accumulator
    sudo cp ~/accumulator_proxy /opt/wasmlanche/accumulator/
    sudo cp ~/rsa_accumulator.wasm /opt/wasmlanche/accumulator/
    sudo cp ~/config.env /opt/wasmlanche/accumulator/
    sudo cp ~/enarx_config.toml /opt/wasmlanche/accumulator/
    sudo cp ~/accumulator-proxy.service /etc/systemd/system/
    
    # Start service
    sudo systemctl daemon-reload
    sudo systemctl restart accumulator-proxy
    sudo systemctl enable accumulator-proxy
EOF
  echo "Deployed to SEV node: $node"
done

# Verify deployments
echo "Verifying deployments..."
for node in "${SGX_NODES[@]}" "${SEV_NODES[@]}"; do
  echo -n "Checking $node: "
  if curl -s "http://$node:$ACCUMULATOR_PORT/health" | grep -q "ok"; then
    echo -e "\033[32mOK\033[0m"
  else
    echo -e "\033[31mFAILED\033[0m"
  fi
done

echo ""
echo "Production deployment complete!"
echo "Monitoring endpoints:"
echo "- Health: http://[NODE]:$ACCUMULATOR_PORT/health"
echo "- Stats: http://[NODE]:$ACCUMULATOR_PORT/stats"
