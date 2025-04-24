#!/bin/bash

# Script to check the actual repository structure on AWS instances
# This will help us understand where the Cargo.toml files are located

set -e

# Configuration
KEY_NAME="nasdaq-tee-key"
SGX_IP="54.226.83.253"  # First SGX node

echo "Checking git repository structure..."
ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SGX_IP} "
sudo apt-get install -y git

# Clone a fresh copy of the repository to analyze structure
git clone https://github.com/rhombus-tech/aristo.git /tmp/aristo-test 2>/dev/null || true

echo 'Finding all Cargo.toml files...'
find /tmp/aristo-test -name 'Cargo.toml' | sort

echo -e '\nChecking directory structure...'
find /tmp/aristo-test -type d -name execution -o -name controller | sort

echo -e '\nChecking if we can directly use the local copy from the deployment...'
find /opt/rhombus -type f -name '*.rs' | head -5
"
