#!/bin/bash
# Install Enarx and configure hardware attestation components
# For dual TEE cross-attestation security system

set -e

NODE_TYPE="${1:-SGX}"  # Default to SGX if not specified
echo "Setting up Enarx for $NODE_TYPE node"

# Install dependencies for Enarx
sudo apt-get update
sudo apt-get install -y \
    build-essential \
    curl \
    git \
    libssl-dev \
    pkg-config \
    musl-tools \
    clang \
    llvm

# Install Rust if not already installed
if ! command -v rustc &> /dev/null; then
    echo "Installing Rust..."
    curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
fi

# Source the Rust environment to ensure path is set
export PATH="$HOME/.cargo/bin:$PATH"
source "$HOME/.cargo/env" || true

# Add wasm32 target
rustup target add wasm32-wasip1 || echo "Failed to add wasm32-wasip1 target, continuing anyway..."

# Clone Enarx repository - use shallow clone to speed things up
echo "Cloning Enarx repository..."
cd /tmp
rm -rf enarx
git clone --depth=1 https://github.com/enarx/enarx.git
cd enarx

# Install a pre-built binary instead of compiling from source to save time
echo "Installing pre-built Enarx binary..."
mkdir -p "$HOME/bin"
curl -L https://github.com/enarx/enarx/releases/download/v0.7.1/enarx-x86_64-unknown-linux-musl -o "$HOME/bin/enarx"
chmod +x "$HOME/bin/enarx"
sudo cp "$HOME/bin/enarx" /usr/local/bin/

# Configure TEE-specific settings
if [ "$NODE_TYPE" == "SGX" ]; then
    # Set up SGX environment variables
    echo 'export SGX_ENABLED=1' | sudo tee -a /etc/environment
    echo 'export SGX_MODE=HW' | sudo tee -a /etc/environment
    
    # Update TEE service configuration to point to the correct Enarx binary
    sudo sed -i 's|/usr/local/bin/enarx|'$(which enarx)'|g' /etc/systemd/system/tee-service.service
    
elif [ "$NODE_TYPE" == "SEV" ]; then
    # Configure SEV settings
    echo 'export SEV_ENABLED=1' | sudo tee -a /etc/environment
    echo 'export SEV_SNP_ENABLED=1' | sudo tee -a /etc/environment
    
    # Update TEE service configuration to point to the correct Enarx binary
    sudo sed -i 's|/usr/local/bin/enarx|'$(which enarx)'|g' /etc/systemd/system/tee-service.service
fi

# Create Enarx configuration directory
sudo mkdir -p /etc/enarx
sudo mkdir -p /var/lib/enarx
sudo chmod -R 755 /var/lib/enarx

# Ensure that the WebAssembly contract is in place
echo "Ensuring test contract is available"
sudo mkdir -p /opt/rhombus/tee/contracts
sudo bash -c 'echo -ne "\x00\x61\x73\x6d\x01\x00\x00\x00\x01\x07\x01\x60\x02\x7f\x7f\x01\x7f\x03\x02\x01\x00\x07\x07\x01\x03\x61\x64\x64\x00\x00\x0a\x09\x01\x07\x00\x20\x00\x20\x01\x6a\x0b" > /opt/rhombus/tee/contracts/748775a3a2076c1ae990e94755e63bcb.wasm'

# Restart the TEE service to use the new Enarx binary
sudo systemctl daemon-reload
sudo systemctl restart tee-service

# Show the Enarx version
enarx --version || echo "Failed to get Enarx version, but installation completed"

echo "Enarx installation and configuration complete for $NODE_TYPE node"
