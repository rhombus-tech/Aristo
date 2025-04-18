#!/bin/bash

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONFIG_FILE="${SCRIPT_DIR}/nasdaq_credentials.env"

# Check if config file exists
if [ ! -f "$CONFIG_FILE" ]; then
    echo -e "${RED}Error: Credentials file not found at $CONFIG_FILE${NC}"
    exit 1
fi

# Source the configuration
source "$CONFIG_FILE"

# Override the TEE endpoint to use port 8081
TEE_ENDPOINT="http://localhost:8081/api/v1/nasdaq-data"

# Activate virtual environment
source "${SCRIPT_DIR}/venv/bin/activate"

echo -e "${YELLOW}Starting NASDAQ Cloud Data Service Integration${NC}"
echo "==============================================="
echo "Client ID: $NASDAQ_CLIENT_ID"
echo "Token Endpoint: $NASDAQ_TOKEN_ENDPOINT"
echo "Bootstrap Server: $NASDAQ_BOOTSTRAP_SERVER"
echo "TEE Endpoint: $TEE_ENDPOINT"
echo "Region: $TEE_REGION"
echo "==============================================="
echo -e "${GREEN}Connecting to NASDAQ Cloud Data Service...${NC}"
echo "This will stream live market data through your TEE mesh network"
echo "Press Ctrl+C to stop"
echo ""

# Make the script executable
chmod +x "${SCRIPT_DIR}/nasdaq_api_consumer.py"

# Run the NASDAQ API consumer with our credentials
"${SCRIPT_DIR}/nasdaq_api_consumer.py" \
  --client-id "$NASDAQ_CLIENT_ID" \
  --client-secret "$NASDAQ_CLIENT_SECRET" \
  --token-endpoint "$NASDAQ_TOKEN_ENDPOINT" \
  --bootstrap-server "$NASDAQ_BOOTSTRAP_SERVER" \
  --tee-endpoint "$TEE_ENDPOINT" \
  --region "$TEE_REGION" \
  --log-level INFO
