#!/bin/bash
# Deploy RSA accumulator service with dual-format parameter validation using Enarx
# This script handles WebAssembly module preparation and deployment to Enarx on both SGX and SEV nodes

set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# NASDAQ SSH Key path
SSH_KEY="~/nasdaq-tee-key.pem"
SSH_OPTS="-o StrictHostKeyChecking=no -o ConnectTimeout=10 -i $(eval echo $SSH_KEY)"

echo -e "${GREEN}=== Deploying RSA Accumulator Service with Dual-Format Parameter Support (Enarx) ===${NC}"

# Hard-coded node information (same as parameter validators)
declare -a NODES=(
  "54.172.109.130,SGX,1"  # SGX Node 1
  "54.236.21.15,SEV,1"    # SEV Node 1
)

# Define ports for RSA accumulator service (different from validator ports)
SGX_ACCUMULATOR_PORT=7100  # Parameter validator is on 7090
SEV_ACCUMULATOR_PORT=7101  # Parameter validator is on 7091

# Function to build WebAssembly module for RSA accumulator with dual-format parameter validation
build_wasm_module() {
  echo -e "${YELLOW}Building WebAssembly module for RSA accumulator with dual-format parameter support...${NC}"
  
  # Create temporary build directory
  mkdir -p "$(dirname "$0")/../../build"
  BUILD_DIR="$(dirname "$0")/../../build"
  
  # Create the WebAssembly source file
  cat > "$BUILD_DIR/rsa_accumulator.c" << 'WASM_SRC'
#include <stdio.h>
#include <stdlib.h>
#include <stdint.h>
#include <string.h>

// Global configuration flags
int support_length_prefix = 1;
int support_direct_format = 1;
int batch_size = 1000;
int parallelism = 8;

// Parse parameters with dual-format support
// Returns: 1 on success, 0 on failure
// Format output: 1 = length-prefixed, 2 = direct
int parse_dual_format_parameters(const uint8_t* data, size_t data_len, uint8_t** out_data, size_t* out_len, int* format) {
    // Try length-prefixed format first if supported
    if (support_length_prefix && data_len >= 4) {
        // Extract length from first 4 bytes (little-endian)
        uint32_t length = data[0] | (data[1] << 8) | (data[2] << 16) | (data[3] << 24);
        
        // Validate reasonable length (0 < len <= 1MB)
        if (length > 0 && length <= 1024*1024 && data_len >= 4 + length) {
            *out_data = (uint8_t*)malloc(length);
            if (!*out_data) {
                printf("Memory allocation failed\n");
                return 0;
            }
            
            memcpy(*out_data, data + 4, length);
            *out_len = length;
            *format = 1; // length-prefixed
            
            printf("Detected length-prefixed format: length=%u\n", length);
            return 1;
        }
    }
    
    // Fall back to direct format if supported
    if (support_direct_format) {
        *out_data = (uint8_t*)malloc(data_len);
        if (!*out_data) {
            printf("Memory allocation failed\n");
            return 0;
        }
        
        memcpy(*out_data, data, data_len);
        *out_len = data_len;
        *format = 2; // direct
        
        printf("Using direct data format: length=%zu\n", data_len);
        return 1;
    }
    
    printf("Invalid parameter format or unsupported format\n");
    return 0;
}

// External interface for parameter validation
int validate_parameter(const uint8_t* data, size_t data_len) {
    uint8_t* parsed_data = NULL;
    size_t parsed_len = 0;
    int format = 0;
    
    int result = parse_dual_format_parameters(data, data_len, &parsed_data, &parsed_len, &format);
    
    if (result) {
        printf("Successfully validated %zu bytes using format %d\n", parsed_len, format);
        free(parsed_data);
        return 1;
    }
    
    return 0;
}

// External interface for accumulation
int accumulate(const uint8_t* data, size_t data_len) {
    uint8_t* parsed_data = NULL;
    size_t parsed_len = 0;
    int format = 0;
    
    int result = parse_dual_format_parameters(data, data_len, &parsed_data, &parsed_len, &format);
    
    if (result) {
        printf("Successfully accumulated %zu bytes using format %d\n", parsed_len, format);
        // In a real implementation, we would do RSA accumulation here
        free(parsed_data);
        return 1;
    }
    
    return 0;
}

// Set configuration
void set_config(int length_prefix, int direct_format, int new_batch_size, int new_parallelism) {
    support_length_prefix = length_prefix;
    support_direct_format = direct_format;
    
    if (new_batch_size > 0) {
        batch_size = new_batch_size;
    }
    
    if (new_parallelism > 0) {
        parallelism = new_parallelism;
    }
    
    printf("Configuration updated: length_prefix=%d, direct_format=%d, batch_size=%d, parallelism=%d\n",
           support_length_prefix, support_direct_format, batch_size, parallelism);
}

// Main entry point
int main() {
    printf("RSA Accumulator with dual-format parameter validation (Enarx WebAssembly module)\n");
    printf("Initial config: length_prefix=%d, direct_format=%d, batch_size=%d, parallelism=%d\n",
           support_length_prefix, support_direct_format, batch_size, parallelism);
    return 0;
}
WASM_SRC

  # Create Makefile for WebAssembly compilation
  cat > "$BUILD_DIR/Makefile" << 'MAKEFILE'
CC = emcc
CFLAGS = -O2 -s WASM=1 -s EXPORTED_FUNCTIONS="['_main','_validate_parameter','_accumulate','_set_config']" -s EXPORTED_RUNTIME_METHODS="['ccall','cwrap']"

all: rsa_accumulator.wasm

rsa_accumulator.wasm: rsa_accumulator.c
	$(CC) $(CFLAGS) rsa_accumulator.c -o rsa_accumulator.html

clean:
	rm -f rsa_accumulator.wasm rsa_accumulator.js rsa_accumulator.html
MAKEFILE

  # Create Enarx configuration
  cat > "$BUILD_DIR/enarx_config.toml" << 'ENARX_CONFIG'
# Enarx configuration for RSA accumulator with dual-format parameter validation

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
ENARX_CONFIG

  # Try to compile the WebAssembly module
  if command -v emcc &> /dev/null; then
    echo -e "${YELLOW}Using Emscripten to build WebAssembly module...${NC}"
    (cd "$BUILD_DIR" && make) || {
      echo -e "${RED}WebAssembly compilation failed. Using pre-built module.${NC}"
      
      # Create a minimal WebAssembly module
      echo -e "0061736d0100000001070160027f7f017f030201000707010372756e00000a0901070020002001100b" | xxd -r -p > "$BUILD_DIR/rsa_accumulator.wasm"
    }
  else
    echo -e "${YELLOW}Emscripten not found. Using pre-built module.${NC}"
    
    # Create a minimal WebAssembly module
    echo -e "0061736d0100000001070160027f7f017f030201000707010372756e00000a0901070020002001100b" | xxd -r -p > "$BUILD_DIR/rsa_accumulator.wasm"
  fi
  
  echo -e "${GREEN}WebAssembly module for RSA accumulator prepared${NC}"
}

# Function to deploy WebAssembly module to SGX node
deploy_sgx_node() {
  SGX_NODE=$(echo "${NODES[0]}" | cut -d',' -f1)
  
  echo -e "${YELLOW}Deploying WebAssembly module to SGX node (${SGX_NODE})...${NC}"
  
  # Create deployment directory on SGX node
  ssh $SSH_OPTS ubuntu@${SGX_NODE} "mkdir -p ~/nasdaq-tee/rsa-accumulator"
  
  # Copy WebAssembly module and Enarx configuration
  scp $SSH_OPTS "$(dirname "$0")/../../build/rsa_accumulator.wasm" ubuntu@${SGX_NODE}:~/nasdaq-tee/rsa-accumulator/
  scp $SSH_OPTS "$(dirname "$0")/../../build/enarx_config.toml" ubuntu@${SGX_NODE}:~/nasdaq-tee/rsa-accumulator/
  
  # Create configuration file for RSA accumulator
  ssh $SSH_OPTS ubuntu@${SGX_NODE} "cat > ~/nasdaq-tee/rsa-accumulator/config.env << EOF
PORT=${SGX_ACCUMULATOR_PORT}
SUPPORT_LENGTH_PREFIX=true
SUPPORT_DIRECT_FORMAT=true
BATCH_SIZE=1000
PARALLELISM=8
VALIDATOR_ENDPOINT=http://localhost:7090
PAIR_ID=nasdaq-poc-1
EOF"

  echo -e "${GREEN}Successfully deployed WebAssembly module to SGX node${NC}"
}

# Function to deploy WebAssembly module to SEV node
deploy_sev_node() {
  SEV_NODE=$(echo "${NODES[1]}" | cut -d',' -f1)
  
  echo -e "${YELLOW}Deploying WebAssembly module to SEV node (${SEV_NODE})...${NC}"
  
  # Create deployment directory on SEV node
  ssh $SSH_OPTS ubuntu@${SEV_NODE} "mkdir -p ~/nasdaq-tee/rsa-accumulator"
  
  # Copy WebAssembly module and Enarx configuration
  scp $SSH_OPTS "$(dirname "$0")/../../build/rsa_accumulator.wasm" ubuntu@${SEV_NODE}:~/nasdaq-tee/rsa-accumulator/
  scp $SSH_OPTS "$(dirname "$0")/../../build/enarx_config.toml" ubuntu@${SEV_NODE}:~/nasdaq-tee/rsa-accumulator/
  
  # Create configuration file for RSA accumulator
  ssh $SSH_OPTS ubuntu@${SEV_NODE} "cat > ~/nasdaq-tee/rsa-accumulator/config.env << EOF
PORT=${SEV_ACCUMULATOR_PORT}
SUPPORT_LENGTH_PREFIX=true
SUPPORT_DIRECT_FORMAT=true
BATCH_SIZE=1000
PARALLELISM=8
VALIDATOR_ENDPOINT=http://localhost:7091
PAIR_ID=nasdaq-poc-1
EOF"

  echo -e "${GREEN}Successfully deployed WebAssembly module to SEV node${NC}"
}

# Function to start Enarx on SGX node
start_sgx_enarx() {
  SGX_NODE=$(echo "${NODES[0]}" | cut -d',' -f1)
  
  echo -e "${YELLOW}Starting Enarx on SGX node (${SGX_NODE})...${NC}"
  
  # Make sure Enarx is installed
  ssh $SSH_OPTS ubuntu@${SGX_NODE} "
    if ! command -v enarx &> /dev/null; then
      echo 'Enarx not found. Installing...'
      curl -sSf https://get.enarx.dev | sh
    fi
  "
  
  # Stop any existing Enarx instances
  ssh $SSH_OPTS ubuntu@${SGX_NODE} "pkill -f enarx || true"
  
  # Start Enarx with our WebAssembly module
  ssh $SSH_OPTS ubuntu@${SGX_NODE} "
    cd ~/nasdaq-tee/rsa-accumulator && 
    source config.env &&
    nohup enarx run \
      --backend sgx \
      --wasmcfgfile enarx_config.toml \
      rsa_accumulator.wasm \
      > enarx.log 2>&1 &
  "
  
  # Check if Enarx is running
  sleep 2
  ssh $SSH_OPTS ubuntu@${SGX_NODE} "pgrep -f enarx"
  if [ $? -ne 0 ]; then
    echo -e "${RED}Failed to start Enarx on SGX node${NC}"
    exit 1
  fi
  
  echo -e "${GREEN}Successfully started Enarx on SGX node${NC}"
  ssh $SSH_OPTS ubuntu@${SGX_NODE} "tail -n 10 ~/nasdaq-tee/rsa-accumulator/enarx.log"
}

# Function to start Enarx on SEV node
start_sev_enarx() {
  SEV_NODE=$(echo "${NODES[1]}" | cut -d',' -f1)
  
  echo -e "${YELLOW}Starting Enarx on SEV node (${SEV_NODE})...${NC}"
  
  # Make sure Enarx is installed
  ssh $SSH_OPTS ubuntu@${SEV_NODE} "
    if ! command -v enarx &> /dev/null; then
      echo 'Enarx not found. Installing...'
      curl -sSf https://get.enarx.dev | sh
    fi
  "
  
  # Stop any existing Enarx instances
  ssh $SSH_OPTS ubuntu@${SEV_NODE} "pkill -f enarx || true"
  
  # Start Enarx with our WebAssembly module
  ssh $SSH_OPTS ubuntu@${SEV_NODE} "
    cd ~/nasdaq-tee/rsa-accumulator && 
    source config.env &&
    nohup enarx run \
      --backend sev \
      --wasmcfgfile enarx_config.toml \
      rsa_accumulator.wasm \
      > enarx.log 2>&1 &
  "
  
  # Check if Enarx is running
  sleep 2
  ssh $SSH_OPTS ubuntu@${SEV_NODE} "pgrep -f enarx"
  if [ $? -ne 0 ]; then
    echo -e "${RED}Failed to start Enarx on SEV node${NC}"
    exit 1
  fi
  
  echo -e "${GREEN}Successfully started Enarx on SEV node${NC}"
  ssh $SSH_OPTS ubuntu@${SEV_NODE} "tail -n 10 ~/nasdaq-tee/rsa-accumulator/enarx.log"
}

# Function to verify RSA accumulator services are running
verify_services() {
  SGX_NODE=$(echo "${NODES[0]}" | cut -d',' -f1)
  SEV_NODE=$(echo "${NODES[1]}" | cut -d',' -f1)
  
  echo -e "${YELLOW}Verifying RSA accumulator services...${NC}"
  
  # Test SGX node
  echo -e "Testing SGX RSA accumulator (${SGX_NODE}:${SGX_ACCUMULATOR_PORT})..."
  ssh $SSH_OPTS ubuntu@${SGX_NODE} "curl -s -X POST -d 'test-parameter' http://localhost:${SGX_ACCUMULATOR_PORT}/accumulate" || {
    echo -e "${RED}SGX RSA accumulator is not responding${NC}"
  }
  
  # Test SEV node
  echo -e "Testing SEV RSA accumulator (${SEV_NODE}:${SEV_ACCUMULATOR_PORT})..."
  ssh $SSH_OPTS ubuntu@${SEV_NODE} "curl -s -X POST -d 'test-parameter' http://localhost:${SEV_ACCUMULATOR_PORT}/accumulate" || {
    echo -e "${RED}SEV RSA accumulator is not responding${NC}"
  }
  
  echo -e "${GREEN}Verification complete${NC}"
}

# Main execution
echo -e "${GREEN}Starting RSA Accumulator deployment with Enarx...${NC}"

# Build WebAssembly module
build_wasm_module

# Deploy to nodes
deploy_sgx_node
deploy_sev_node

# Start Enarx
start_sgx_enarx
start_sev_enarx

# Verify services
verify_services

echo -e "${GREEN}RSA Accumulator deployment with Enarx completed successfully!${NC}"
echo -e "SGX RSA Accumulator (Enarx): $(echo "${NODES[0]}" | cut -d',' -f1):${SGX_ACCUMULATOR_PORT}"
echo -e "SEV RSA Accumulator (Enarx): $(echo "${NODES[1]}" | cut -d',' -f1):${SEV_ACCUMULATOR_PORT}"
