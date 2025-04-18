#!/bin/bash

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Default values (these will be replaced with secure environment variables)
CLIENT_ID=""
CLIENT_SECRET=""
TOKEN_ENDPOINT=""
BOOTSTRAP_SERVER=""
TEE_ENDPOINT="http://localhost:8080/api/v1/nasdaq-data"
REGION="us-east"

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Function to display help
function show_help {
    echo -e "${YELLOW}NASDAQ Cloud Data Service Integration for TEE Platform${NC}"
    echo "=========================================================="
    echo "This script runs the NASDAQ data integration service"
    echo ""
    echo "Usage:"
    echo "  $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  -h, --help                 Show this help message"
    echo "  -i, --client-id ID         Set the OAuth2 Client ID"
    echo "  -s, --client-secret SECRET Set the OAuth2 Client Secret"
    echo "  -t, --token-endpoint URL   Set the OAuth2 Token Endpoint URL"
    echo "  -b, --bootstrap-server URL Set the Kafka Bootstrap Server"
    echo "  -e, --tee-endpoint URL     Set the TEE Mesh API Endpoint (default: $TEE_ENDPOINT)"
    echo "  -r, --region ID            Set the TEE Region ID (default: $REGION)"
    echo ""
    echo "Environment Variables:"
    echo "  NASDAQ_CLIENT_ID           OAuth2 Client ID"
    echo "  NASDAQ_CLIENT_SECRET       OAuth2 Client Secret"
    echo "  NASDAQ_TOKEN_ENDPOINT      OAuth2 Token Endpoint URL"
    echo "  NASDAQ_BOOTSTRAP_SERVER    Kafka Bootstrap Server"
    echo "  TEE_ENDPOINT               TEE Mesh API Endpoint"
    echo "  TEE_REGION                 TEE Region ID"
    echo ""
    echo "Example:"
    echo "  $0 --client-id myid --client-secret mysecret \\"
    echo "     --token-endpoint https://auth.example.com/token \\"
    echo "     --bootstrap-server kafka.example.com:9093 \\"
    echo "     --region us-east"
    echo ""
}

# Function to check for dependencies
function check_dependencies {
    # Check for Python 3
    if ! command -v python3 &> /dev/null; then
        echo -e "${RED}Error: Python 3 is required but not installed.${NC}"
        exit 1
    fi
    
    # Check for pip
    if ! command -v pip3 &> /dev/null; then
        echo -e "${RED}Error: pip3 is required but not installed.${NC}"
        exit 1
    fi
    
    # Install required Python packages
    echo -e "${YELLOW}Checking required Python packages...${NC}"
    pip3 install confluent-kafka requests
}

# Function to validate required parameters
function validate_params {
    local missing_params=false
    
    if [ -z "$CLIENT_ID" ]; then
        echo -e "${RED}Error: OAuth2 Client ID is required${NC}"
        missing_params=true
    fi
    
    if [ -z "$CLIENT_SECRET" ]; then
        echo -e "${RED}Error: OAuth2 Client Secret is required${NC}"
        missing_params=true
    fi
    
    if [ -z "$TOKEN_ENDPOINT" ]; then
        echo -e "${RED}Error: OAuth2 Token Endpoint URL is required${NC}"
        missing_params=true
    fi
    
    if [ -z "$BOOTSTRAP_SERVER" ]; then
        echo -e "${RED}Error: Kafka Bootstrap Server is required${NC}"
        missing_params=true
    fi
    
    if [ "$missing_params" = true ]; then
        echo ""
        show_help
        exit 1
    fi
}

# Function to start the NASDAQ integration
function start_integration {
    echo -e "${YELLOW}Starting NASDAQ Cloud Data Service Integration...${NC}"
    echo "Region: $REGION"
    echo "TEE Endpoint: $TEE_ENDPOINT"
    
    # Build the command
    local cmd="python3 ${SCRIPT_DIR}/nasdaq_kafka_service.py"
    cmd+=" --client-id \"$CLIENT_ID\""
    cmd+=" --client-secret \"$CLIENT_SECRET\""
    cmd+=" --token-endpoint \"$TOKEN_ENDPOINT\""
    cmd+=" --bootstrap-server \"$BOOTSTRAP_SERVER\""
    cmd+=" --tee-endpoint \"$TEE_ENDPOINT\""
    cmd+=" --region \"$REGION\""
    
    # Run the command
    echo -e "${GREEN}Integration started!${NC}"
    echo "Press Ctrl+C to stop"
    echo "=============================================="
    eval $cmd
}

# Load environment variables if present
[ -n "$NASDAQ_CLIENT_ID" ] && CLIENT_ID="$NASDAQ_CLIENT_ID"
[ -n "$NASDAQ_CLIENT_SECRET" ] && CLIENT_SECRET="$NASDAQ_CLIENT_SECRET"
[ -n "$NASDAQ_TOKEN_ENDPOINT" ] && TOKEN_ENDPOINT="$NASDAQ_TOKEN_ENDPOINT"
[ -n "$NASDAQ_BOOTSTRAP_SERVER" ] && BOOTSTRAP_SERVER="$NASDAQ_BOOTSTRAP_SERVER"
[ -n "$TEE_ENDPOINT" ] && TEE_ENDPOINT="$TEE_ENDPOINT"
[ -n "$TEE_REGION" ] && REGION="$TEE_REGION"

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            show_help
            exit 0
            ;;
        -i|--client-id)
            CLIENT_ID="$2"
            shift 2
            ;;
        -s|--client-secret)
            CLIENT_SECRET="$2"
            shift 2
            ;;
        -t|--token-endpoint)
            TOKEN_ENDPOINT="$2"
            shift 2
            ;;
        -b|--bootstrap-server)
            BOOTSTRAP_SERVER="$2"
            shift 2
            ;;
        -e|--tee-endpoint)
            TEE_ENDPOINT="$2"
            shift 2
            ;;
        -r|--region)
            REGION="$2"
            shift 2
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            show_help
            exit 1
            ;;
    esac
done

# Check dependencies
check_dependencies

# Validate required parameters
validate_params

# Start the integration
start_integration
