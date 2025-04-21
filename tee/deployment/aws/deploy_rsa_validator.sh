#!/bin/bash
# Deploy enhanced TEE validator with RSA accumulator integration

set -e

# Configuration
SGX_NODE_IP=${SGX_NODE_IP:-"<SGX_NODE_IP>"}
SEV_NODE_IP=${SEV_NODE_IP:-"<SEV_NODE_IP>"}
VALIDATOR_FILE="rsa_enhanced_validator.py"
CONFIG_PATH="parameter_config.json"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== Deploying Enhanced TEE Validator with RSA Accumulator ===${NC}"

# Ensure validator file exists
if [ ! -f "$VALIDATOR_FILE" ]; then
    echo -e "${RED}Validator file not found: $VALIDATOR_FILE${NC}"
    exit 1
fi

# Create parameter configuration files
cat > sgx_parameter_config.json <<EOL
{
  "tee_type": "SGX",
  "node_id": "sgx-1",
  "parameter_validation": {
    "length_prefixed": true,
    "direct_format": true,
    "max_size": 1024,
    "format_detection": true
  },
  "partner_tee": {
    "tee_type": "SEV",
    "node_id": "sev-1"
  },
  "accumulator": {
    "size_bytes": 32,
    "batch_size": 1000,
    "threads": 8
  },
  "listen_address": "0.0.0.0:7070"
}
EOL

cat > sev_parameter_config.json <<EOL
{
  "tee_type": "SEV",
  "node_id": "sev-1",
  "parameter_validation": {
    "length_prefixed": true,
    "direct_format": true,
    "max_size": 1024,
    "format_detection": true
  },
  "partner_tee": {
    "tee_type": "SGX",
    "node_id": "sgx-1"
  },
  "accumulator": {
    "size_bytes": 32,
    "batch_size": 1000,
    "threads": 8
  },
  "listen_address": "0.0.0.0:7070"
}
EOL

# Print validator code stats
echo -e "${YELLOW}Validator information:${NC}"
echo -e "  Size: $(wc -c < $VALIDATOR_FILE) bytes"
echo -e "  Parameters: Length-prefixed and Direct format, with 1024 byte max"
echo -e "  RSA Accumulator: 32-byte with 1000 element batch size"
echo -e "  Target: 8-thread parallel execution"

# Deploy to SGX node
if [ "$SGX_NODE_IP" != "<SGX_NODE_IP>" ]; then
    echo -e "${GREEN}Deploying to SGX node: $SGX_NODE_IP${NC}"
    ssh -o StrictHostKeyChecking=no ubuntu@$SGX_NODE_IP "mkdir -p ~/tee_validator"
    scp $VALIDATOR_FILE ubuntu@$SGX_NODE_IP:~/tee_validator/
    scp sgx_parameter_config.json ubuntu@$SGX_NODE_IP:~/tee_validator/parameter_config.json
    
    # Install dependencies if needed
    ssh ubuntu@$SGX_NODE_IP "
        cd ~/tee_validator && 
        chmod +x $VALIDATOR_FILE &&
        sudo apt-get update &&
        sudo apt-get install -y python3 python3-pip &&
        pip3 install --user typing &&
        echo 'Starting validator service...' &&
        python3 $VALIDATOR_FILE --config parameter_config.json > validator.log 2>&1 &
        echo \$! > validator.pid
    "
    echo -e "${GREEN}SGX Validator deployed and started${NC}"
else
    echo -e "${YELLOW}Skipping SGX node deployment (IP not specified)${NC}"
fi

# Deploy to SEV node
if [ "$SEV_NODE_IP" != "<SEV_NODE_IP>" ]; then
    echo -e "${GREEN}Deploying to SEV node: $SEV_NODE_IP${NC}"
    ssh -o StrictHostKeyChecking=no ubuntu@$SEV_NODE_IP "mkdir -p ~/tee_validator"
    scp $VALIDATOR_FILE ubuntu@$SEV_NODE_IP:~/tee_validator/
    scp sev_parameter_config.json ubuntu@$SEV_NODE_IP:~/tee_validator/parameter_config.json
    
    # Install dependencies if needed
    ssh ubuntu@$SEV_NODE_IP "
        cd ~/tee_validator && 
        chmod +x $VALIDATOR_FILE &&
        sudo apt-get update &&
        sudo apt-get install -y python3 python3-pip &&
        pip3 install --user typing &&
        echo 'Starting validator service...' &&
        python3 $VALIDATOR_FILE --config parameter_config.json > validator.log 2>&1 &
        echo \$! > validator.pid
    "
    echo -e "${GREEN}SEV Validator deployed and started${NC}"
else
    echo -e "${YELLOW}Skipping SEV node deployment (IP not specified)${NC}"
fi

echo -e "${GREEN}=== Deployment Complete ===${NC}"
echo "To test cross-attestation, use the simple_validator_test.py script"

# Clean up temporary files
rm -f sgx_parameter_config.json sev_parameter_config.json
