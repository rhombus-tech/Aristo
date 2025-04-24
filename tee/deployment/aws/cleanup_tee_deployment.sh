#!/bin/bash
# cleanup_tee_deployment.sh
# Performs a clean shutdown of existing TEE deployment before new deployment
set -e

# Configuration - update with the same settings as your deployment script
KEY_NAME="nasdaq-tee-key"  # Default key name used in our deployment
TEE_PAIR_COUNT=3  # Number of TEE pairs in the deployment

# AWS Region for CloudFormation resources
REGION="us-east-1"  # Default region used in deployment

# Initialize empty arrays for IPs
SGX_IPS=()
SEV_IPS=()

# Dynamically get IP addresses from CloudFormation stacks
echo "Discovering deployed TEE nodes from CloudFormation..."
for i in $(seq 1 $TEE_PAIR_COUNT); do
  PAIR_STACK_NAME="nasdaq-tee-pair-$i"
  
  echo "Checking CloudFormation stack: $PAIR_STACK_NAME"
  if aws cloudformation describe-stacks --stack-name $PAIR_STACK_NAME --region $REGION &>/dev/null; then
    SGX_IP=$(aws cloudformation describe-stacks --stack-name $PAIR_STACK_NAME --region $REGION \
      --query "Stacks[0].Outputs[?OutputKey=='SGXNodePublicIP'].OutputValue" --output text)
    SEV_IP=$(aws cloudformation describe-stacks --stack-name $PAIR_STACK_NAME --region $REGION \
      --query "Stacks[0].Outputs[?OutputKey=='SEVNodePublicIP'].OutputValue" --output text)
    
    if [ -n "$SGX_IP" ] && [ -n "$SEV_IP" ]; then
      echo "Found TEE pair $i: SGX=$SGX_IP, SEV=$SEV_IP"
      SGX_IPS+=($SGX_IP)
      SEV_IPS+=($SEV_IP)
    else
      echo "Warning: Could not find IP addresses for pair $i"
    fi
  else
    echo "CloudFormation stack $PAIR_STACK_NAME not found, skipping..."
  fi
done

# Check if we found any instances
if [ ${#SGX_IPS[@]} -eq 0 ] || [ ${#SEV_IPS[@]} -eq 0 ]; then
  echo "Error: No TEE nodes found in CloudFormation. Check if deployment exists."
  echo "You can proceed with a new deployment without cleanup."
  exit 1
fi

echo "Found ${#SGX_IPS[@]} TEE pairs to clean up:"

echo "=== Starting TEE Deployment Cleanup ==="
echo "This will stop all services and prepare for a clean redeployment."
echo "TEE Pair Count: $TEE_PAIR_COUNT"

# Function to clean up a node
cleanup_node() {
  local ip=$1
  local node_type=$2
  local pair_id=$3
  
  echo "Cleaning up $node_type node (Pair $pair_id) at $ip..."
  
  # Stop any running Node.js processes and services
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$ip << EOF
    echo "Stopping services on $node_type node (Pair $pair_id)..."
    
    # Stop Node.js processes
    sudo pkill -f "node.*wasm_accumulator_handler.js" || true
    sudo pkill -f "node.*wasm_sampling_handler.js" || true
    
    # Stop any systemd services we might have created
    sudo systemctl stop rhombus-tee-handler.service || true
    sudo systemctl disable rhombus-tee-handler.service || true
    
    # Remove existing configuration
    echo "Cleaning configuration directories..."
    sudo rm -rf /opt/rhombus/mesh
    sudo rm -f /opt/rhombus/tee_config.json
    
    # Keep the WASM modules and core files, but remove handlers
    sudo rm -f /opt/rhombus/wasm_accumulator_handler.js
    
    echo "Cleanup complete on $node_type node (Pair $pair_id)"
EOF
  
  echo "$node_type node (Pair $pair_id) cleanup completed"
}

# Clean up all nodes
for i in $(seq 1 $TEE_PAIR_COUNT); do
  IDX=$((i-1))
  
  # Skip if IP isn't set
  if [ -z "${SGX_IPS[$IDX]}" ] || [ -z "${SEV_IPS[$IDX]}" ]; then
    echo "Warning: Missing IP information for pair $i, skipping..."
    continue
  fi
  
  SGX_IP=${SGX_IPS[$IDX]}
  SEV_IP=${SEV_IPS[$IDX]}
  
  echo "Cleaning up TEE pair $i..."
  cleanup_node $SGX_IP "SGX" $i
  cleanup_node $SEV_IP "SEV" $i
  echo "TEE pair $i cleanup completed"
done

echo "=== Cleanup Complete ==="
echo "All TEE nodes have been cleaned and are ready for redeployment."
echo "You can now run the deploy_nasdaq_e2e_poc.sh script for a fresh deployment."
