#!/bin/bash

# Script to run the NASDAQ integration using the credentials config file
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONFIG_FILE="${SCRIPT_DIR}/nasdaq_credentials.env"

# Check if config file exists
if [ ! -f "$CONFIG_FILE" ]; then
    echo "Error: Credentials file not found at $CONFIG_FILE"
    exit 1
fi

# Source the configuration
source "$CONFIG_FILE"

# Run the integration using environment variables
"${SCRIPT_DIR}/run_nasdaq_integration.sh"
