#!/bin/bash
# Script to deploy the Go-based parameter validator service
# This script replaces the Python TEE parameter validation service with our high-performance Go implementation

set -e

# Configuration
SSH_KEY="$HOME/nasdaq-tee-key.pem"
SSH_USER="ubuntu"
GO_BINARY="parameter_validator"
SERVICE_FILE="go-validator.service"
SERVICE_NAME="go-validator"
PYTHON_SERVICE_NAME="python-validator"

# Build the Go binary
echo "Building parameter validator binary..."
mkdir -p bin
GOOS=linux GOARCH=amd64 go build -o bin/$GO_BINARY ./cmd/parameter_validator/

# SGX and SEV node endpoints
SGX_NODES=("ec2-54-225-41-220.compute-1.amazonaws.com" "ec2-52-54-181-245.compute-1.amazonaws.com")
SEV_NODES=("ec2-3-81-65-148.compute-1.amazonaws.com" "ec2-54-161-2-145.compute-1.amazonaws.com")

# Deploy to SGX nodes
deploy_sgx() {
  for node in "${SGX_NODES[@]}"; do
    echo "Deploying to SGX node: $node"
    
    # Copy binary and service file
    scp -i $SSH_KEY bin/$GO_BINARY $SSH_USER@$node:~/$GO_BINARY
    scp -i $SSH_KEY deployment/$SERVICE_FILE $SSH_USER@$node:~/$SERVICE_FILE
    
    # Configure for SGX mode
    ssh -i $SSH_KEY $SSH_USER@$node "sudo sed -i 's/Environment=TEE_TYPE=sgx/Environment=TEE_TYPE=sgx/g' ~/$SERVICE_FILE"
    ssh -i $SSH_KEY $SSH_USER@$node "sudo sed -i 's/Environment=PEER_ENDPOINT=localhost:7301/Environment=PEER_ENDPOINT=${SEV_NODES[0]}:7300/g' ~/$SERVICE_FILE"
    
    # Configure WebAssembly accumulator integration
    ssh -i $SSH_KEY $SSH_USER@$node "sudo sed -i '/^ExecStart=/ s/$/ --sgx-node-ip=${SGX_NODES[0]} --sev-node-ip=${SEV_NODES[0]} --batch-size=10 --batch-interval=50 --enable-accumulator=true/' ~/$SERVICE_FILE"
    
    # Stop Python service, install Go service
    ssh -i $SSH_KEY $SSH_USER@$node "sudo systemctl stop $PYTHON_SERVICE_NAME || true"
    ssh -i $SSH_KEY $SSH_USER@$node "sudo mv ~/$GO_BINARY /usr/local/bin/"
    ssh -i $SSH_KEY $SSH_USER@$node "sudo mv ~/$SERVICE_FILE /etc/systemd/system/"
    ssh -i $SSH_KEY $SSH_USER@$node "sudo systemctl daemon-reload"
    ssh -i $SSH_KEY $SSH_USER@$node "sudo systemctl enable $SERVICE_NAME"
    ssh -i $SSH_KEY $SSH_USER@$node "sudo systemctl start $SERVICE_NAME"
    
    echo "Deployed Go validator to SGX node: $node"
  done
}

# Deploy to SEV nodes
deploy_sev() {
  for node in "${SEV_NODES[@]}"; do
    echo "Deploying to SEV node: $node"
    
    # Copy binary and service file
    scp -i $SSH_KEY bin/$GO_BINARY $SSH_USER@$node:~/$GO_BINARY
    scp -i $SSH_KEY deployment/$SERVICE_FILE $SSH_USER@$node:~/$SERVICE_FILE
    
    # Configure for SEV mode
    ssh -i $SSH_KEY $SSH_USER@$node "sudo sed -i 's/Environment=TEE_TYPE=sgx/Environment=TEE_TYPE=sev/g' ~/$SERVICE_FILE"
    ssh -i $SSH_KEY $SSH_USER@$node "sudo sed -i 's/Environment=PEER_ENDPOINT=localhost:7301/Environment=PEER_ENDPOINT=${SGX_NODES[0]}:7300/g' ~/$SERVICE_FILE"
    
    # Configure WebAssembly accumulator integration
    ssh -i $SSH_KEY $SSH_USER@$node "sudo sed -i '/^ExecStart=/ s/$/ --sgx-node-ip=${SGX_NODES[0]} --sev-node-ip=${SEV_NODES[0]} --batch-size=10 --batch-interval=50 --enable-accumulator=true/' ~/$SERVICE_FILE"
    
    # Stop Python service, install Go service
    ssh -i $SSH_KEY $SSH_USER@$node "sudo systemctl stop $PYTHON_SERVICE_NAME || true"
    ssh -i $SSH_KEY $SSH_USER@$node "sudo mv ~/$GO_BINARY /usr/local/bin/"
    ssh -i $SSH_KEY $SSH_USER@$node "sudo mv ~/$SERVICE_FILE /etc/systemd/system/"
    ssh -i $SSH_KEY $SSH_USER@$node "sudo systemctl daemon-reload"
    ssh -i $SSH_KEY $SSH_USER@$node "sudo systemctl enable $SERVICE_NAME"
    ssh -i $SSH_KEY $SSH_USER@$node "sudo systemctl start $SERVICE_NAME"
    
    echo "Deployed Go validator to SEV node: $node"
  done
}

# Main deployment function
deploy() {
  echo "Starting parameter validator deployment"
  deploy_sgx
  deploy_sev
  echo "Deployment complete. Go-based parameter validator service is now running."
}

# Run deployment
deploy
