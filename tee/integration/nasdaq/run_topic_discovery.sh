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

# Activate virtual environment
source "${SCRIPT_DIR}/venv/bin/activate"

# Output file for results
OUTPUT_FILE="${SCRIPT_DIR}/discovered_topics.json"

echo -e "${YELLOW}NASDAQ Cloud Data Service Topic Discovery${NC}"
echo "=================================================="
echo "Client ID: $NASDAQ_CLIENT_ID"
echo "Token Endpoint: $NASDAQ_TOKEN_ENDPOINT"
echo "Bootstrap Server: $NASDAQ_BOOTSTRAP_SERVER"
echo "Output File: $OUTPUT_FILE"
echo "=================================================="
echo -e "${GREEN}Discovering accessible NASDAQ topics...${NC}"
echo "This tool will:
  1. Authenticate with NASDAQ CDS using your credentials
  2. Try to discover the exact topic names you have access to
  3. Test different topic naming patterns and prefixes
  4. Save results to $OUTPUT_FILE"
echo ""

# Make the script executable
chmod +x "${SCRIPT_DIR}/topic_discovery.py"

# Run the topic discovery with credentials
"${SCRIPT_DIR}/topic_discovery.py" \
  --client-id "$NASDAQ_CLIENT_ID" \
  --client-secret "$NASDAQ_CLIENT_SECRET" \
  --token-endpoint "$NASDAQ_TOKEN_ENDPOINT" \
  --bootstrap-server "$NASDAQ_BOOTSTRAP_SERVER" \
  --output "$OUTPUT_FILE" \
  --log-level INFO
