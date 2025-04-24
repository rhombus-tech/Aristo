#!/bin/bash

# Script to check logs from the dual TEE mesh network services
# This will help diagnose why the services are restarting

set -e

# Configuration
REGION="us-east-1"
KEY_NAME="nasdaq-tee-key"

# Get AWS instance IPs
SGX_IPS=(54.226.83.253 3.80.113.74)
SEV_IPS=(54.209.198.69 52.206.202.214)

# Check coordinator logs
echo "=================================="
echo "Checking coordinator logs on ${SGX_IPS[0]}..."
echo "=================================="
ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SGX_IPS[0]} "sudo journalctl -u coordinator -n 50 --no-pager"

# Check TEE controller logs
for i in {0..1}; do
  echo -e "\n=================================="
  echo "Checking TEE controller logs on SGX node ${SGX_IPS[$i]}..."
  echo "=================================="
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SGX_IPS[$i]} "sudo journalctl -u tee-controller -n 50 --no-pager"
  
  echo -e "\n=================================="
  echo "Checking TEE controller logs on SEV node ${SEV_IPS[$i]}..."
  echo "=================================="
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SEV_IPS[$i]} "sudo journalctl -u tee-controller -n 50 --no-pager"
done

echo -e "\n=================================="
echo "Checking file permissions and resources:"
echo "=================================="
ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SGX_IPS[0]} "ls -la /opt/rhombus/ && echo -e '\nFree Disk Space:' && df -h"
