#!/bin/bash

# Optimized TEE Accumulator Deployment Script
# This script updates the existing AWS TEE nodes with the optimized accumulator implementation

set -e

# Configuration
REGION="us-east-1"  # Region where the nodes are deployed
STACK_NAME="dual-tee-stack"  # The CloudFormation stack name used for deployment
BUILD_DIR="./build"  # Temporary build directory
LOCAL_REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)/tee"  # Root of the tee directory

# Parse command line arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    --region)
      REGION="$2"
      shift 2
      ;;
    --stack-name)
      STACK_NAME="$2"
      shift 2
      ;;
    *)
      echo "Unknown option: $1"
      exit 1
      ;;
  esac
done

# Create build directory
mkdir -p $BUILD_DIR

echo "=== Building Optimized TEE Accumulator ==="

# Copy required files to build directory
cp "$LOCAL_REPO_DIR/accumulator/optimized_rsa_client.go" "$BUILD_DIR/"
cp "$LOCAL_REPO_DIR/accumulator/rsa_client.go" "$BUILD_DIR/"
cp -r "$LOCAL_REPO_DIR/integration/nasdaq" "$BUILD_DIR/"

# Create a simple Go build wrapper
cat > "$BUILD_DIR/build.go" << 'EOF'
package main

import (
    "fmt"
    "os"
    "github.com/rhombus-tech/vm/tee/accumulator"
)

func main() {
    // Simple test to verify the optimized client can be instantiated
    client, err := accumulator.NewOptimizedRsaClient(
        "test-client",
        "SGX",
        accumulator.OptimizedRsaOptions{
            BatchSize:   1000,
            EnableAsync: true,
            Parallelism: 8,
        },
    )
    if err != nil {
        fmt.Printf("Error creating client: %v\n", err)
        os.Exit(1)
    }
    defer client.Close()
    
    fmt.Println("Optimized RSA client successfully built and tested!")
}
EOF

# Create a deployment script that will run on each node
cat > "$BUILD_DIR/node_deploy.sh" << 'EOF'
#!/bin/bash
set -e

# Stop the existing accumulator service if running
if systemctl is-active --quiet tee-accumulator; then
    sudo systemctl stop tee-accumulator
fi

# Backup existing implementation
if [ -d "/opt/tee/accumulator/backup" ]; then
    sudo rm -rf /opt/tee/accumulator/backup
fi

sudo mkdir -p /opt/tee/accumulator/backup
sudo cp -r /opt/tee/accumulator/* /opt/tee/accumulator/backup/

# Copy new optimized implementation
sudo cp -r ./accumulator/* /opt/tee/accumulator/
sudo cp -r ./nasdaq /opt/tee/integration/

# Update configuration for optimized performance
sudo cat > /opt/tee/accumulator/config.json << EOCFG
{
    "batch_size": 1000,
    "enable_async": true,
    "parallelism": 8,
    "verify_timeout_ms": 100,
    "benchmark_mode": false
}
EOCFG

# Start the updated service
sudo systemctl start tee-accumulator

# Verify the service is running
if systemctl is-active --quiet tee-accumulator; then
    echo "TEE Accumulator service successfully updated and running"
else
    echo "Error: TEE Accumulator service failed to start"
    exit 1
fi

# Run a simple benchmark to verify performance
/opt/tee/accumulator/benchmark --batch-size 1000 --duration 10
EOF

chmod +x "$BUILD_DIR/node_deploy.sh"

# Create an update script for the NASDAQ connector
cat > "$BUILD_DIR/update_nasdaq_connector.sh" << 'EOF'
#!/bin/bash
set -e

# Stop the existing NASDAQ connector if running
if systemctl is-active --quiet nasdaq-connector; then
    sudo systemctl stop nasdaq-connector
fi

# Back up existing connector
sudo mkdir -p /opt/tee/integration/nasdaq/backup
sudo cp -r /opt/tee/integration/nasdaq/* /opt/tee/integration/nasdaq/backup/

# Copy updated connector files
sudo cp -r ./nasdaq/* /opt/tee/integration/nasdaq/

# Update configuration
sudo cat > /opt/tee/integration/nasdaq/config.json << EOCFG
{
    "batch_size": 1000,
    "batch_timeout_ms": 50,
    "worker_threads": 16,
    "target_tps": 50000,
    "sgx_nodes": ["sgx-node-1", "sgx-node-2", "sgx-node-3", "sgx-node-4", "sgx-node-5", "sgx-node-6"],
    "sev_nodes": ["sev-node-1", "sev-node-2", "sev-node-3", "sev-node-4", "sev-node-5", "sev-node-6"]
}
EOCFG

# Start the connector
sudo systemctl start nasdaq-connector

# Verify the connector is running
if systemctl is-active --quiet nasdaq-connector; then
    echo "NASDAQ connector successfully updated and running"
else
    echo "Error: NASDAQ connector failed to start"
    exit 1
fi
EOF

chmod +x "$BUILD_DIR/update_nasdaq_connector.sh"

echo "=== Retrieving Node Information ==="

# Get all TEE node instances from the CloudFormation stack
SGX_INSTANCES=$(aws cloudformation describe-stack-resources \
  --stack-name $STACK_NAME \
  --region $REGION \
  --query "StackResources[?ResourceType=='AWS::EC2::Instance' && contains(LogicalResourceId, 'SGXNode')].PhysicalResourceId" \
  --output text)

SEV_INSTANCES=$(aws cloudformation describe-stack-resources \
  --stack-name $STACK_NAME \
  --region $REGION \
  --query "StackResources[?ResourceType=='AWS::EC2::Instance' && contains(LogicalResourceId, 'SEVNode')].PhysicalResourceId" \
  --output text)

NASDAQ_CONNECTOR=$(aws cloudformation describe-stack-resources \
  --stack-name $STACK_NAME \
  --region $REGION \
  --query "StackResources[?ResourceType=='AWS::EC2::Instance' && contains(LogicalResourceId, 'NasdaqConnector')].PhysicalResourceId" \
  --output text)

if [ -z "$SGX_INSTANCES" ] || [ -z "$SEV_INSTANCES" ] || [ -z "$NASDAQ_CONNECTOR" ]; then
  echo "Error: Could not find all required instances in the CloudFormation stack"
  exit 1
fi

echo "=== Deploying to SGX Nodes ==="
for INSTANCE in $SGX_INSTANCES; do
  IP=$(aws ec2 describe-instances --instance-ids $INSTANCE --region $REGION --query "Reservations[0].Instances[0].PrivateIpAddress" --output text)
  echo "Deploying to SGX Node: $INSTANCE ($IP)"
  
  # Create a temporary deployment package for this node
  NODE_DIR="$BUILD_DIR/sgx-$INSTANCE"
  mkdir -p "$NODE_DIR/accumulator"
  cp -r "$BUILD_DIR/"*.go "$NODE_DIR/accumulator/"
  cp -r "$BUILD_DIR/nasdaq" "$NODE_DIR/"
  cp "$BUILD_DIR/node_deploy.sh" "$NODE_DIR/"
  
  # Use SCP to copy files to the node
  scp -r "$NODE_DIR/"* ec2-user@$IP:~/tee-update/
  
  # SSH to run the deployment script
  ssh ec2-user@$IP "cd ~/tee-update && ./node_deploy.sh"
done

echo "=== Deploying to SEV Nodes ==="
for INSTANCE in $SEV_INSTANCES; do
  IP=$(aws ec2 describe-instances --instance-ids $INSTANCE --region $REGION --query "Reservations[0].Instances[0].PrivateIpAddress" --output text)
  echo "Deploying to SEV Node: $INSTANCE ($IP)"
  
  # Create a temporary deployment package for this node
  NODE_DIR="$BUILD_DIR/sev-$INSTANCE"
  mkdir -p "$NODE_DIR/accumulator"
  cp -r "$BUILD_DIR/"*.go "$NODE_DIR/accumulator/"
  cp -r "$BUILD_DIR/nasdaq" "$NODE_DIR/"
  cp "$BUILD_DIR/node_deploy.sh" "$NODE_DIR/"
  
  # Use SCP to copy files to the node
  scp -r "$NODE_DIR/"* ec2-user@$IP:~/tee-update/
  
  # SSH to run the deployment script
  ssh ec2-user@$IP "cd ~/tee-update && ./node_deploy.sh"
done

echo "=== Deploying to NASDAQ Connector ==="
IP=$(aws ec2 describe-instances --instance-ids $NASDAQ_CONNECTOR --region $REGION --query "Reservations[0].Instances[0].PrivateIpAddress" --output text)
echo "Deploying to NASDAQ Connector: $NASDAQ_CONNECTOR ($IP)"

# Create a temporary deployment package for the connector
CONNECTOR_DIR="$BUILD_DIR/nasdaq-connector"
mkdir -p "$CONNECTOR_DIR"
cp -r "$BUILD_DIR/nasdaq" "$CONNECTOR_DIR/"
cp "$BUILD_DIR/update_nasdaq_connector.sh" "$CONNECTOR_DIR/"

# Use SCP to copy files to the node
scp -r "$CONNECTOR_DIR/"* ec2-user@$IP:~/tee-update/

# SSH to run the deployment script
ssh ec2-user@$IP "cd ~/tee-update && ./update_nasdaq_connector.sh"

echo "=== Deployment Complete ==="
echo "Optimized TEE Accumulator has been deployed to all AWS nodes"
echo "NASDAQ Connector has been updated with the optimized implementation"
echo "The system is now ready for the NASDAQ proof of concept"

# Clean up
rm -rf $BUILD_DIR

exit 0
