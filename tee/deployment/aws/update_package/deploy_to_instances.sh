#!/bin/bash

# Simplified deployment script for specific instances
set -e

# Configuration
REGION="us-east-1"
BUILD_DIR="./build"
LOCAL_REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)/tee"

# Instance IDs
SGX_INSTANCES="i-00e38fb76e0e77bb6 i-0b24b97ad7d7922aa"
SEV_INSTANCES="i-011c91b6513c9a499 i-0b2ebde88de87aaca"

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

echo "=== Deploying to SGX Nodes ==="
for INSTANCE in $SGX_INSTANCES; do
  IP=$(aws ec2 describe-instances --instance-ids $INSTANCE --region $REGION --query "Reservations[0].Instances[0].PublicIpAddress" --output text)
  echo "Deploying to SGX Node: $INSTANCE ($IP)"
  
  # Create a temporary deployment package for this node
  NODE_DIR="$BUILD_DIR/sgx-$INSTANCE"
  mkdir -p "$NODE_DIR/accumulator"
  cp -r "$BUILD_DIR/"*.go "$NODE_DIR/accumulator/"
  cp -r "$BUILD_DIR/nasdaq" "$NODE_DIR/"
  cp "$BUILD_DIR/node_deploy.sh" "$NODE_DIR/"
  
  # Use SCP to copy files to the node
  echo "Copying files to $IP..."
  ssh -o StrictHostKeyChecking=no -i "~/.ssh/tee-access-key" ubuntu@$IP "mkdir -p ~/tee-update"
  scp -o StrictHostKeyChecking=no -i "~/.ssh/tee-access-key" -r "$NODE_DIR/"* ubuntu@$IP:~/tee-update/
  
  # SSH to run the deployment script
  echo "Running deployment script on $IP..."
  ssh -o StrictHostKeyChecking=no -i "~/.ssh/tee-access-key" ubuntu@$IP "cd ~/tee-update && chmod +x node_deploy.sh && ./node_deploy.sh"
done

echo "=== Deploying to SEV Nodes ==="
for INSTANCE in $SEV_INSTANCES; do
  IP=$(aws ec2 describe-instances --instance-ids $INSTANCE --region $REGION --query "Reservations[0].Instances[0].PublicIpAddress" --output text)
  echo "Deploying to SEV Node: $INSTANCE ($IP)"
  
  # Create a temporary deployment package for this node
  NODE_DIR="$BUILD_DIR/sev-$INSTANCE"
  mkdir -p "$NODE_DIR/accumulator"
  cp -r "$BUILD_DIR/"*.go "$NODE_DIR/accumulator/"
  cp -r "$BUILD_DIR/nasdaq" "$NODE_DIR/"
  cp "$BUILD_DIR/node_deploy.sh" "$NODE_DIR/"
  
  # Use SCP to copy files to the node
  echo "Copying files to $IP..."
  ssh -o StrictHostKeyChecking=no -i "~/.ssh/tee-access-key" ubuntu@$IP "mkdir -p ~/tee-update"
  scp -o StrictHostKeyChecking=no -i "~/.ssh/tee-access-key" -r "$NODE_DIR/"* ubuntu@$IP:~/tee-update/
  
  # SSH to run the deployment script
  echo "Running deployment script on $IP..."
  ssh -o StrictHostKeyChecking=no -i "~/.ssh/tee-access-key" ubuntu@$IP "cd ~/tee-update && chmod +x node_deploy.sh && ./node_deploy.sh"
done

echo "=== Deployment Complete ==="
echo "Optimized TEE Accumulator has been deployed to all AWS nodes"

# Clean up
rm -rf $BUILD_DIR

exit 0
