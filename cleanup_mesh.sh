#!/bin/bash
# Script to clean up services and files before redeployment

set -e

# Configuration
KEY_NAME="nasdaq-tee-key"

# Get AWS instance IPs
SGX_IPS=(54.226.83.253 3.80.113.74)
SEV_IPS=(54.209.198.69 52.206.202.214)

for i in {0..1}; do
  # Stop and clean up SGX node
  echo "Cleaning up SGX node ${SGX_IPS[$i]}..."
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SGX_IPS[$i]} "
  sudo systemctl stop tee-controller 2>/dev/null || true
  sudo systemctl disable tee-controller 2>/dev/null || true
  sudo rm -f /opt/rhombus/tee-controller 2>/dev/null || true
  "
  
  # Stop and clean up SEV node
  echo "Cleaning up SEV node ${SEV_IPS[$i]}..."
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SEV_IPS[$i]} "
  sudo systemctl stop tee-controller 2>/dev/null || true
  sudo systemctl disable tee-controller 2>/dev/null || true
  sudo rm -f /opt/rhombus/tee-controller 2>/dev/null || true
  "
done

# Clean up coordinator node
echo "Cleaning up coordinator node ${SGX_IPS[0]}..."
ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SGX_IPS[0]} "
sudo systemctl stop coordinator 2>/dev/null || true
sudo systemctl disable coordinator 2>/dev/null || true
sudo rm -f /opt/rhombus/coordinator 2>/dev/null || true
"

echo "Cleanup complete!"
