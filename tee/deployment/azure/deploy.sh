#!/bin/bash
# Deploy Aristo TEE Pairs to Azure
# This script helps deploy the dual TEE infrastructure to Azure

set -e

# Default values
RESOURCE_GROUP="aristo-tee"
LOCATION="eastus"
DEPLOYMENT_NAME="aristo-tee-deployment"
PREFIX="aristotee"
ADMIN_USERNAME="aristoadmin"
SGX_VM_SIZE="Standard_DC2s_v2"
SEV_VM_SIZE="Standard_NV4as_v4" 
NUMBER_OF_TEE_PAIRS=3
REGIONS=("eastus" "westeurope" "southeastasia")

# Function to display usage
usage() {
    echo "Usage: $0 [options]"
    echo "Options:"
    echo "  -g, --resource-group    Resource group name (default: $RESOURCE_GROUP)"
    echo "  -l, --location          Primary location (default: $LOCATION)"
    echo "  -n, --name              Deployment name (default: $DEPLOYMENT_NAME)"
    echo "  -p, --prefix            Resource prefix (default: $PREFIX)"
    echo "  -u, --username          Admin username (default: $ADMIN_USERNAME)"
    echo "  -s, --sgx-vm-size       SGX VM size (default: $SGX_VM_SIZE)"
    echo "  -v, --sev-vm-size       SEV VM size (default: $SEV_VM_SIZE)"
    echo "  -c, --count             Number of TEE pairs (default: $NUMBER_OF_TEE_PAIRS)"
    echo "  -r, --regions           Comma-separated list of regions"
    echo "  -h, --help              Display this help message"
    exit 1
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        -g|--resource-group)
            RESOURCE_GROUP="$2"
            shift 2
            ;;
        -l|--location)
            LOCATION="$2"
            shift 2
            ;;
        -n|--name)
            DEPLOYMENT_NAME="$2"
            shift 2
            ;;
        -p|--prefix)
            PREFIX="$2"
            shift 2
            ;;
        -u|--username)
            ADMIN_USERNAME="$2"
            shift 2
            ;;
        -s|--sgx-vm-size)
            SGX_VM_SIZE="$2"
            shift 2
            ;;
        -v|--sev-vm-size)
            SEV_VM_SIZE="$2"
            shift 2
            ;;
        -c|--count)
            NUMBER_OF_TEE_PAIRS="$2"
            shift 2
            ;;
        -r|--regions)
            IFS=',' read -r -a REGIONS <<< "$2"
            shift 2
            ;;
        -h|--help)
            usage
            ;;
        *)
            echo "Unknown option: $1"
            usage
            ;;
    esac
done

# Check if Azure CLI is installed
if ! command -v az &> /dev/null; then
    echo "Azure CLI not found. Please install it first."
    echo "Visit: https://docs.microsoft.com/cli/azure/install-azure-cli"
    exit 1
fi

# Check if we're logged in
echo "Checking Azure CLI login status..."
SUBSCRIPTION_ID=$(az account show --query id --output tsv 2>/dev/null || echo "")
if [ -z "$SUBSCRIPTION_ID" ]; then
    echo "Not logged in to Azure. Please run 'az login' first."
    exit 1
fi
echo "Using subscription: $SUBSCRIPTION_ID"

# Prompt for the admin password (don't store it in variables for security)
echo -n "Enter admin password for VMs (will not be echoed): "
read -s ADMIN_PASSWORD
echo ""

if [ -z "$ADMIN_PASSWORD" ]; then
    echo "Error: Admin password cannot be empty"
    exit 1
fi

# Check if the password meets Azure requirements
PW_LENGTH=${#ADMIN_PASSWORD}
if [ $PW_LENGTH -lt 12 ]; then
    echo "Error: Password must be at least 12 characters long"
    exit 1
fi

# Create resource group if it doesn't exist
echo "Creating/checking resource group '$RESOURCE_GROUP' in location '$LOCATION'..."
az group create --name "$RESOURCE_GROUP" --location "$LOCATION"

# Format regions array for ARM template
REGIONS_JSON=$(printf '%s\n' "${REGIONS[@]}" | jq -R . | jq -s .)

# Deploy ARM template
echo "Deploying ARM template for $NUMBER_OF_TEE_PAIRS TEE pairs..."
az deployment group create \
    --resource-group "$RESOURCE_GROUP" \
    --name "$DEPLOYMENT_NAME" \
    --template-file "$(dirname "$0")/azuredeploy-fixed.json" \
    --parameters \
        location="$LOCATION" \
        prefix="$PREFIX" \
        adminUsername="$ADMIN_USERNAME" \
        adminPassword="$ADMIN_PASSWORD" \
        sgxVmSize="$SGX_VM_SIZE" \
        sevVmSize="$SEV_VM_SIZE" \
        numberOfTeePairs="$NUMBER_OF_TEE_PAIRS" \
        regions="$REGIONS_JSON"

echo "Deployment initiated. This may take 15-20 minutes to complete."
echo "You can monitor the deployment progress in the Azure Portal or by running:"
echo "az deployment group show --resource-group $RESOURCE_GROUP --name $DEPLOYMENT_NAME --query properties.provisioningState"

# After deployment, get VM information
echo "Wait for deployment to complete, then run the following to get VM information:"
echo "az vm list-ip-addresses --resource-group $RESOURCE_GROUP --output table"
