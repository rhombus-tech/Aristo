#!/bin/bash
# deploy_accumulator.sh - Deploy the RSA Accumulator with the Proxy
# This script deploys both the accumulator proxy and the WebAssembly module to the TEE instances

set -e

# Configuration
SGX_NODES=("ec2-54-225-41-220.compute-1.amazonaws.com" "ec2-52-54-181-245.compute-1.amazonaws.com")
SEV_NODES=("ec2-3-81-65-148.compute-1.amazonaws.com" "ec2-54-161-2-145.compute-1.amazonaws.com")
KEY_FILE="$HOME/nasdaq-tee-key.pem"
SSH_USER="ubuntu"
WASM_PATH="bin/rsa_accumulator.wasm"
PROXY_PATH="bin/accumulator_proxy"
ENARX_CONFIG="execution/accumulator/enarx_config.toml"

# Helper function to display status
function status() {
  echo -e "\033[1;34m[INFO]\033[0m $1"
}

# Build the components
status "Building accumulator proxy..."
go build -o bin/accumulator_proxy ./cmd/accumulator_proxy/

# Using existing WASM module - already built in previous steps
status "Using WebAssembly module at $WASM_PATH"

# Deploy to SGX nodes
status "Deploying to SGX nodes..."
for node in "${SGX_NODES[@]}"; do
  status "Deploying to $node..."
  
  # Copy files
  scp -i "$KEY_FILE" "$PROXY_PATH" "$SSH_USER@$node:~/"
  scp -i "$KEY_FILE" "$WASM_PATH" "$SSH_USER@$node:~/"
  scp -i "$KEY_FILE" "$ENARX_CONFIG" "$SSH_USER@$node:~/enarx_config.toml"
  
  # Setup and start service
  ssh -i "$KEY_FILE" "$SSH_USER@$node" << EOF
    sudo mkdir -p /opt/wasmlanche/accumulator
    sudo cp ~/accumulator_proxy /opt/wasmlanche/accumulator/
    sudo cp ~/rsa_accumulator.wasm /opt/wasmlanche/accumulator/
    sudo cp ~/enarx_config.toml /opt/wasmlanche/accumulator/
    
    # Create systemd service if it doesn't exist
    if [ ! -f /etc/systemd/system/accumulator-proxy.service ]; then
      sudo tee /etc/systemd/system/accumulator-proxy.service > /dev/null << EOT
[Unit]
Description=Wasmlanche RSA Accumulator Proxy
After=network.target

[Service]
Type=simple
User=ubuntu
WorkingDirectory=/opt/wasmlanche/accumulator
ExecStart=/opt/wasmlanche/accumulator/accumulator_proxy --wasm-path=/opt/wasmlanche/accumulator/rsa_accumulator.wasm --port=7101 --tee-type=sgx --enable-cross-validate=true
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOT
    fi
    
    # Start service
    sudo systemctl daemon-reload
    sudo systemctl restart accumulator-proxy
    sudo systemctl enable accumulator-proxy
    
    echo "Service status:"
    sudo systemctl status accumulator-proxy --no-pager
EOF
  
  status "Successfully deployed to $node"
done

# Deploy to SEV nodes
status "Deploying to SEV nodes..."
for node in "${SEV_NODES[@]}"; do
  status "Deploying to $node..."
  
  # Copy files
  scp -i "$KEY_FILE" "$PROXY_PATH" "$SSH_USER@$node:~/"
  scp -i "$KEY_FILE" "$WASM_PATH" "$SSH_USER@$node:~/"
  scp -i "$KEY_FILE" "$ENARX_CONFIG" "$SSH_USER@$node:~/enarx_config.toml"
  
  # Setup and start service
  ssh -i "$KEY_FILE" "$SSH_USER@$node" << EOF
    sudo mkdir -p /opt/wasmlanche/accumulator
    sudo cp ~/accumulator_proxy /opt/wasmlanche/accumulator/
    sudo cp ~/rsa_accumulator.wasm /opt/wasmlanche/accumulator/
    sudo cp ~/enarx_config.toml /opt/wasmlanche/accumulator/
    
    # Create systemd service if it doesn't exist
    if [ ! -f /etc/systemd/system/accumulator-proxy.service ]; then
      sudo tee /etc/systemd/system/accumulator-proxy.service > /dev/null << EOT
[Unit]
Description=Wasmlanche RSA Accumulator Proxy
After=network.target

[Service]
Type=simple
User=ubuntu
WorkingDirectory=/opt/wasmlanche/accumulator
ExecStart=/opt/wasmlanche/accumulator/accumulator_proxy --wasm-path=/opt/wasmlanche/accumulator/rsa_accumulator.wasm --port=7101 --tee-type=sev --enable-cross-validate=true
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOT
    fi
    
    # Start service
    sudo systemctl daemon-reload
    sudo systemctl restart accumulator-proxy
    sudo systemctl enable accumulator-proxy
    
    echo "Service status:"
    sudo systemctl status accumulator-proxy --no-pager
EOF
  
  status "Successfully deployed to $node"
done

status "Deployment complete! Services should be running on all nodes."
status "You can test with:"
status "curl http://[NODE_IP]:7101/health"

# Verify installation
status "Verifying installation..."
for node in "${SGX_NODES[@]}" "${SEV_NODES[@]}"; do
  echo -n "Checking $node: "
  if curl -s "http://$node:7101/health" | grep -q "ok"; then
    echo -e "\033[1;32mOK\033[0m"
  else
    echo -e "\033[1;31mFAILED\033[0m"
  fi
done

echo ""
status "You can now configure the parameter validator to use the accumulator proxy endpoints:"
status "- SGX nodes: ${SGX_NODES[0]}:7101"
status "- SEV nodes: ${SEV_NODES[0]}:7101"
