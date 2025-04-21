#!/bin/bash
# Build WebAssembly module for RSA accumulator with dual-format parameter validation

# Exit on error
set -e

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${YELLOW}Building WebAssembly module for Enarx...${NC}"

# Check if necessary tools are installed
if ! command -v tinygo &>/dev/null; then
  echo -e "${RED}TinyGo is not installed. Please install it first.${NC}"
  echo "Visit https://tinygo.org/getting-started/install/ for installation instructions."
  exit 1
fi

# Navigate to accumulator directory
cd "$(dirname "$0")/../../accumulator"

# Build WebAssembly module with TinyGo
tinygo build -o accumulator.wasm -target wasm ./accumulator_wasm.go

if [ $? -ne 0 ]; then
  echo -e "${RED}Failed to build WebAssembly module${NC}"
  exit 1
fi

echo -e "${GREEN}Successfully built WebAssembly module for RSA accumulator${NC}"
echo -e "${YELLOW}Module location: $(pwd)/accumulator.wasm${NC}"

# Create Enarx configuration files
cat > enarx_config.toml << 'EOF'
[[files]]
kind = "stdin"
content = """
{
  "supportLengthPrefix": true,
  "supportDirectFormat": true,
  "batchSize": 1000,
  "parallelism": 8
}
"""
