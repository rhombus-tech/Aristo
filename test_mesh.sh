#!/bin/bash
set -e

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

RUST_CONTROLLER="/Users/talzisckind/Downloads/aristo-fresh 2/execution/target/debug/tee-controller"
BASE_DIR="/Users/talzisckind/Downloads/aristo-fresh 2"
WASM_MODULE="$BASE_DIR/test_data/sample_module.wasm"
INPUT_FILE="$BASE_DIR/test_data/test_input.json"

# Ensure controller is built
echo -e "${BLUE}Building Rust controller...${NC}"
cd "$BASE_DIR/execution/controller"
cargo build

# Setup test data directory if it doesn't exist
mkdir -p "$BASE_DIR/test_data"

# Create sample WASM module if it doesn't exist (sample binary data)
if [ ! -f "$WASM_MODULE" ]; then
    echo -e "${BLUE}Creating sample WASM module...${NC}"
    echo "Sample WASM module" > "$WASM_MODULE"
fi

# Create test input file
echo -e "${BLUE}Creating test input file...${NC}"
cat << EOF > "$INPUT_FILE"
{
    "data": "test input",
    "timestamp": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
}
EOF

echo -e "${BLUE}====== Testing Direct Mesh Functionality ======${NC}"

# Test 1: Test paired execution with mesh
echo -e "${BLUE}Test 1: Testing paired execution with mesh...${NC}"
cd "$BASE_DIR/execution/controller"
"$RUST_CONTROLLER" --simulate --mesh-enabled --region-id test-region execute-paired \
    --wasm-module "$WASM_MODULE" \
    --input "$INPUT_FILE" \
    --contract-id "test-contract-001" \
    --operation-id "test-op-001" \
    --timeout 5000

# Test 2: Test paired execution with mesh cache
echo -e "${BLUE}Test 2: Testing paired execution with mesh cache...${NC}"
"$RUST_CONTROLLER" --simulate --mesh-enabled --region-id test-region execute-paired \
    --wasm-module "$WASM_MODULE" \
    --input "$INPUT_FILE" \
    --contract-id "test-contract-001" \
    --operation-id "test-op-002" \
    --timeout 5000 \
    --use-cache

echo -e "${GREEN}====== Mesh functionality tests completed ======${NC}"
