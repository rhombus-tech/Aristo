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

# Set the TEE endpoint to use localhost:8081 (our mock server)
export TEE_ENDPOINT="http://localhost:8081/api/v1/nasdaq-data"

# Activate virtual environment
source "${SCRIPT_DIR}/venv/bin/activate"

# Make sure scripts are executable
chmod +x "${SCRIPT_DIR}/nasdaq_api_consumer.py"
chmod +x "${SCRIPT_DIR}/mock_tee_server.py"

echo -e "${YELLOW}Starting Mock TEE Server...${NC}"
# Start the mock TEE server in the background
"${SCRIPT_DIR}/mock_tee_server.py" --port 8081 > mock_tee_server.log 2>&1 &
MOCK_SERVER_PID=$!

# Give the server a moment to start
sleep 2

echo -e "${GREEN}Mock TEE Server running with PID ${MOCK_SERVER_PID}${NC}"

echo -e "${YELLOW}Starting NASDAQ Cloud Data Service Integration${NC}"
echo "==============================================="
echo "Client ID: $NASDAQ_CLIENT_ID"
echo "Token Endpoint: $NASDAQ_TOKEN_ENDPOINT"
echo "Bootstrap Server: $NASDAQ_BOOTSTRAP_SERVER"
echo "TEE Endpoint: $TEE_ENDPOINT"
echo "Region: $TEE_REGION"
echo "==============================================="
echo -e "${GREEN}Connecting to NASDAQ Cloud Data Service...${NC}"
echo "This will stream live market data through the mock TEE server"
echo "Press Ctrl+C to stop both the consumer and mock server"
echo ""

# Define trap to handle Ctrl+C and kill the mock server
trap cleanup INT TERM
cleanup() {
    echo -e "${YELLOW}Shutting down...${NC}"
    # Kill the mock server
    kill $MOCK_SERVER_PID 2>/dev/null
    echo "Mock TEE Server stopped"
    exit 0
}

# Run the NASDAQ API consumer with our credentials and improved parameters
"${SCRIPT_DIR}/nasdaq_api_consumer.py" \
  --client-id "$NASDAQ_CLIENT_ID" \
  --client-secret "$NASDAQ_CLIENT_SECRET" \
  --token-endpoint "$NASDAQ_TOKEN_ENDPOINT" \
  --bootstrap-server "$NASDAQ_BOOTSTRAP_SERVER" \
  --tee-endpoint "$TEE_ENDPOINT" \
  --region "$TEE_REGION" \
  --log-level INFO \
  --queue-size 500 \
  --rate-limit 2000 \
  --batch-size 5

# If we reach here, the consumer has terminated, so we should stop the mock server
cleanup
