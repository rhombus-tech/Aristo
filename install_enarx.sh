#!/bin/bash
# Enarx Installation and Configuration Script for Dual TEE Infrastructure
# ==============================================================
# This script installs and configures Enarx on Intel SGX and AMD SEV nodes
# for our dual TEE market data processing infrastructure.

set -e # Exit on any error

echo "=========================================================="
echo "Enarx Installation for Dual TEE Market Data Infrastructure"
echo "=========================================================="

# Check if running as root
if [ "$(id -u)" -ne 0 ]; then
    echo "Error: This script must be run as root or with sudo."
    exit 1
fi

# Detect TEE type (SGX or SEV)
if [ -d /sys/firmware/sev ]; then
    TEE_TYPE="SEV"
    echo "Detected AMD SEV platform"
elif [ -e /dev/sgx_enclave ] || [ -e /dev/sgx/enclave ] || [ -e /dev/isgx ]; then
    TEE_TYPE="SGX"
    echo "Detected Intel SGX platform"
else
    echo "Warning: No TEE hardware detected. Will install Enarx anyway."
    TEE_TYPE="UNKNOWN"
fi

# Create directories
mkdir -p /opt/tee/bin
mkdir -p /opt/tee/contracts
mkdir -p /opt/tee/logs
mkdir -p /opt/tee/config

# Install dependencies
echo "Installing dependencies..."
yum update -y
yum install -y curl wget git gcc make pkg-config openssl-devel

# Install Rust if not already installed
if ! command -v rustc &> /dev/null; then
    echo "Installing Rust..."
    curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
    source $HOME/.cargo/env
fi

# Install Enarx
echo "Installing Enarx..."
if ! command -v enarx &> /dev/null; then
    # Clone and build Enarx from source
    cd /tmp
    git clone https://github.com/enarx/enarx.git
    cd enarx
    # Use a specific version tag for consistency
    git checkout v0.7.1
    cargo install --path .
    
    # Copy to system location for all users
    cp $HOME/.cargo/bin/enarx /usr/local/bin/
    
    echo "Enarx installed to /usr/local/bin/enarx"
else
    echo "Enarx is already installed"
fi

# Verify installation
enarx --version

# Create Enarx service unit file
cat > /etc/systemd/system/enarx-service.service << EOF
[Unit]
Description=Enarx TEE Service for Market Data Processing
After=network.target

[Service]
Type=simple
User=enarx
Group=enarx
WorkingDirectory=/opt/tee
ExecStart=/usr/local/bin/enarx run --backend=${TEE_TYPE,,} /opt/tee/contracts/market_data_processor.wasm
Restart=on-failure
RestartSec=5
StandardOutput=syslog
StandardError=syslog
SyslogIdentifier=enarx-service
Environment=RUST_LOG=info

[Install]
WantedBy=multi-user.target
EOF

# Create enarx user if it doesn't exist
if ! id -u enarx &>/dev/null; then
    echo "Creating enarx user"
    useradd -r -m -d /opt/tee -s /bin/bash enarx
fi

# Set proper permissions
chown -R enarx:enarx /opt/tee

# Create a basic WebAssembly contract for testing
cat > /opt/tee/contracts/test_contract.wasm << EOF
(module
  (type (;0;) (func (param i32 i32) (result i32)))
  (func (;0;) (type 0) (param i32 i32) (result i32)
    local.get 0
    local.get 1
    i32.add)
  (export "add" (func 0))
)
EOF

# Download our market data processor WASM contract
echo "Downloading market data processor WASM contract..."
curl -sL -o /opt/tee/contracts/market_data_processor.wasm https://github.com/internal-assets/tee/releases/market_data_processor.wasm || echo "Failed to download WASM contract - this is just a placeholder URL"

# Create a basic test script
cat > /opt/tee/bin/test_enarx.sh << EOF
#!/bin/bash
echo "Testing Enarx functionality"
/usr/local/bin/enarx info
echo "Testing WASM contract execution"
echo -n '{"method":"add","params":[40,60]}' > /tmp/test_input.json
/usr/local/bin/enarx run --backend=${TEE_TYPE,,} /opt/tee/contracts/test_contract.wasm < /tmp/test_input.json
EOF

chmod +x /opt/tee/bin/test_enarx.sh

# Configure sysctl for SGX if needed
if [ "$TEE_TYPE" = "SGX" ]; then
    echo "Configuring system for SGX..."
    echo "vm.mmap_min_addr = 0" > /etc/sysctl.d/enarx-sgx.conf
    sysctl -p /etc/sysctl.d/enarx-sgx.conf
fi

# Configure for SEV if needed
if [ "$TEE_TYPE" = "SEV" ]; then
    echo "Configuring system for SEV..."
    # Any SEV-specific configuration would go here
fi

# Enable service
systemctl daemon-reload
systemctl enable enarx-service
systemctl start enarx-service || echo "Warning: Failed to start enarx-service. You may need to debug the service."

# Add enarx to PATH for all users
echo 'export PATH=$PATH:/usr/local/bin' > /etc/profile.d/enarx.sh

echo "Testing Enarx installation..."
sudo -u enarx /opt/tee/bin/test_enarx.sh || echo "Enarx test failed, but installation will continue"

echo "=========================================================="
echo "Enarx installation complete!"
echo "TEE Type: $TEE_TYPE"
echo "WASM contracts directory: /opt/tee/contracts"
echo "Service status:"
systemctl status enarx-service --no-pager || true
echo "=========================================================="

# Print instructions
echo "To check if Enarx is running: systemctl status enarx-service"
echo "To start/stop/restart: systemctl start/stop/restart enarx-service"
echo "Logs can be viewed with: journalctl -u enarx-service"
echo "=========================================================="
