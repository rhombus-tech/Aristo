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
    source $HOME/.cargo/env
fi

# Add wasm32 target
rustup target add wasm32-wasip1

# Clone Enarx repository
cd /tmp
git clone https://github.com/enarx/enarx.git
cd enarx

# Build the appropriate version based on node type
if [ "$NODE_TYPE" == "SGX" ]; then
    # Build with SGX support
    echo "Building Enarx with SGX support"
    cargo build --release --features=sgx
    
    # Install the SGX driver and SDK if needed
    echo "Installing SGX driver and SDK"
    curl -fsSL https://download.01.org/intel-sgx/sgx-linux/2.15/distro/ubuntu20.04-server/sgx_linux_x64_driver_2.11.0_2d2b795.bin -o sgx_driver.bin
    chmod +x sgx_driver.bin
    sudo ./sgx_driver.bin
    
    # Install SGX SDK
    wget https://download.01.org/intel-sgx/sgx-linux/2.15/distro/ubuntu20.04-server/sgx_linux_x64_sdk_2.15.100.3.bin
    chmod +x sgx_linux_x64_sdk_2.15.100.3.bin
    echo -e "no\n/opt/intel\n" | sudo ./sgx_linux_x64_sdk_2.15.100.3.bin
    
    # Set up SGX environment variables
    echo 'source /opt/intel/sgxsdk/environment' | sudo tee -a /etc/profile.d/sgx.sh
    echo 'export SGX_ENABLED=1' | sudo tee -a /etc/environment
    echo 'export SGX_MODE=HW' | sudo tee -a /etc/environment
    
elif [ "$NODE_TYPE" == "SEV" ]; then
    # Build with SEV support
    echo "Building Enarx with SEV support"
    cargo build --release --features=sev
    
    # Configure SEV settings
    echo 'export SEV_ENABLED=1' | sudo tee -a /etc/environment
    echo 'export SEV_SNP_ENABLED=1' | sudo tee -a /etc/environment
    
    # Check if SEV is available on the host
    if [ -e /dev/sev ]; then
        echo "SEV device found at /dev/sev"
    else
        echo "WARNING: SEV device not found, hardware attestation may not work properly"
    fi
fi

# Install Enarx
sudo cp target/release/enarx /usr/local/bin/
sudo chmod +x /usr/local/bin/enarx

# Create Enarx configuration directory
sudo mkdir -p /etc/enarx
sudo mkdir -p /var/lib/enarx
sudo chmod -R 755 /var/lib/enarx

# Set up support for WebAssembly contracts
sudo mkdir -p /opt/rhombus/tee/contracts

# Copy our test contract to the contracts directory
cat > /opt/rhombus/tee/contracts/748775a3a2076c1ae990e94755e63bcb.wasm << 'EOF'
\x00\x61\x73\x6d\x01\x00\x00\x00\x01\x07\x01\x60\x02\x7f\x7f\x01\x7f\x03\x02\x01\x00\x07\x07\x01\x03\x61\x64\x64\x00\x00\x0a\x09\x01\x07\x00\x20\x00\x20\x01\x6a\x0b
EOF

# Verify Enarx installation
/usr/local/bin/enarx --version

echo "Enarx installation and configuration complete for $NODE_TYPE node"
