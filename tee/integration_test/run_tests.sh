#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CONTRACT_DIR="$REPO_ROOT/execution/controller/tests/contracts/simple_add"

# Build the test contract
echo "Building test contract..."

# Check if the contract already exists in testdata
if [ -f "$SCRIPT_DIR/../testdata/simple_add.wasm" ]; then
    echo "Test contract already exists, skipping build."
else
    # Get the path to the simple_add contract
    CONTRACT_PATH="$CONTRACT_DIR"
    RELEASE_PATH="$CONTRACT_PATH/target/wasm32-unknown-unknown/release"
    
    # Check if the contract already exists
    if [ ! -f "$RELEASE_PATH/simple_add.wasm" ]; then
        echo "Building simple_add contract..."
        
        # Navigate to the contract directory
        pushd $CONTRACT_PATH > /dev/null
        
        # Build the contract
        cargo build --target wasm32-unknown-unknown --release
        
        # Go back to the original directory
        popd > /dev/null
    else
        echo "Contract already built, using existing wasm."
    fi
    
    # Copy the contract to the testdata directory
    mkdir -p "$SCRIPT_DIR/../testdata"
    cp $RELEASE_PATH/simple_add.wasm "$SCRIPT_DIR/../testdata/"
    
    echo "Contract copied to testdata directory."
fi

# Run the integration tests
echo "Running integration tests..."
go test -v
