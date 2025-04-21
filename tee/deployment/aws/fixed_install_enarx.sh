#!/bin/bash
set -e

echo "Installing Enarx with all dependencies..."

# Install required dependencies including musl-tools which provides musl-gcc
sudo apt-get update
sudo apt-get install -y pkg-config libssl-dev curl git build-essential musl musl-tools musl-dev

# Install Rust if not already installed
if ! command -v cargo &> /dev/null; then
    echo "Installing Rust..."
    curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
    source $HOME/.cargo/env
fi

# Add target for musl
rustup target add x86_64-unknown-linux-musl

# Clone Enarx repo if not already cloned
if [ ! -d "$HOME/enarx" ]; then
    echo "Cloning Enarx repository..."
    cd $HOME
    git clone https://github.com/enarx/enarx.git
fi

# Build Enarx
cd $HOME/enarx
echo "Building Enarx (this may take a while)..."
cargo build --release

# Create a symlink
sudo ln -sf $HOME/enarx/target/release/enarx /usr/local/bin/enarx

# Verify installation
echo "Verifying Enarx installation..."
enarx --version

echo "Enarx installation completed successfully"
