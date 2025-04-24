#!/bin/bash
# Build and deploy script for RSA accumulator with HTTP server in Enarx

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
BUILD_DIR="$PROJECT_DIR/bin"
ACCUMULATOR_DIR="$PROJECT_DIR/execution/accumulator"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}Building RSA Accumulator with HTTP Server for Enarx...${NC}"

# Ensure build directory exists
mkdir -p "$BUILD_DIR"

# Check Rust and Enarx are installed
if ! command -v rustc &> /dev/null; then
    echo -e "${RED}Error: Rust not found. Please install Rust.${NC}"
    exit 1
fi

if ! command -v enarx &> /dev/null; then
    echo -e "${YELLOW}Warning: Enarx not found. You may need to install it for deployment.${NC}"
fi

# Check WebAssembly target is installed
if ! rustup target list --installed | grep -q "wasm32-wasi"; then
    echo -e "${YELLOW}Adding WebAssembly target...${NC}"
    rustup target add wasm32-wasi
fi

# Build the RSA accumulator with HTTP server
echo -e "${GREEN}Compiling RSA accumulator to WebAssembly...${NC}"
cd "$ACCUMULATOR_DIR"
RUSTFLAGS="-C target-feature=+crt-static" cargo build --release --target wasm32-wasi

# Copy WebAssembly module and configuration
echo -e "${GREEN}Copying files to bin directory...${NC}"
cp "$ACCUMULATOR_DIR/target/wasm32-wasi/release/rsa_accumulator.wasm" "$BUILD_DIR/"
cp "$ACCUMULATOR_DIR/enarx_config.toml" "$BUILD_DIR/"

# Generate systemd service file
cat > "$BUILD_DIR/enarx-accumulator.service" << EOL
[Unit]
Description=RSA Accumulator with HTTP Server in Enarx TEE
After=network.target

[Service]
Type=simple
User=ubuntu
WorkingDirectory=/home/ubuntu
ExecStart=/home/ubuntu/.enarx/bin/enarx run \\
  --backend nil \\
  --wasmcfgfile /home/ubuntu/enarx_config.toml \\
  /home/ubuntu/rsa_accumulator.wasm
Environment=PORT=7101
Environment=TEE_TYPE=sgx
Environment=ENABLE_CROSS_VAL=true
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOL

echo -e "${GREEN}Build completed successfully!${NC}"
echo -e "${YELLOW}Files in $BUILD_DIR:${NC}"
ls -la "$BUILD_DIR" | grep -E "\.wasm|\.toml|\.service"

echo -e "${GREEN}To run locally:${NC}"
echo -e "enarx run --backend nil --wasmcfgfile $BUILD_DIR/enarx_config.toml $BUILD_DIR/rsa_accumulator.wasm"

echo -e "${GREEN}To deploy:${NC}"
echo -e "Use the Makefile targets: make deploy-sgx-proxy deploy-sev-proxy"
