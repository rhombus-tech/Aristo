#!/bin/bash

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONFIG_FILE="${SCRIPT_DIR}/nasdaq_credentials.env"

# Load configuration
source "$CONFIG_FILE"

echo -e "${YELLOW}Testing NASDAQ Integration with Mock TEE Endpoint${NC}"
echo "==========================================================="
echo "Client ID: $NASDAQ_CLIENT_ID"
echo "Token Endpoint: $NASDAQ_TOKEN_ENDPOINT"
echo "Bootstrap Server: $NASDAQ_BOOTSTRAP_SERVER"
echo "TEE Endpoint: $TEE_ENDPOINT"
echo "Region: $TEE_REGION"
echo "==========================================================="

# Activate virtual environment
source "${SCRIPT_DIR}/venv/bin/activate"

# Run the NASDAQ Kafka integration directly
echo -e "${GREEN}Starting NASDAQ Kafka Integration...${NC}"
python3 "${SCRIPT_DIR}/nasdaq_kafka_service.py" \
  --client-id "$NASDAQ_CLIENT_ID" \
  --client-secret "$NASDAQ_CLIENT_SECRET" \
  --token-endpoint "$NASDAQ_TOKEN_ENDPOINT" \
  --bootstrap-server "$NASDAQ_BOOTSTRAP_SERVER" \
  --tee-endpoint "$TEE_ENDPOINT" \
  --region "$TEE_REGION"
