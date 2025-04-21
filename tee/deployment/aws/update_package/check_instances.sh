#!/bin/bash

# Simple script to verify access to instances and check what's running
set -e

# Configuration
REGION="us-east-1"

# Instance IDs
SGX_INSTANCES="i-00e38fb76e0e77bb6 i-0b24b97ad7d7922aa"
SEV_INSTANCES="i-011c91b6513c9a499 i-0b2ebde88de87aaca"

echo "=== Checking SGX Nodes ==="
for INSTANCE in $SGX_INSTANCES; do
  IP=$(aws ec2 describe-instances --instance-ids $INSTANCE --region $REGION --query "Reservations[0].Instances[0].PublicIpAddress" --output text)
  echo "Checking SGX Node: $INSTANCE ($IP)"
  ssh -o StrictHostKeyChecking=no -i ~/.ssh/tee-access-key ubuntu@$IP "echo 'Connected to $IP'; hostname; uptime; ls -la /opt/tee 2>/dev/null || echo '/opt/tee not found'; ps aux | grep accumulator | grep -v grep || echo 'No accumulator process running'; sudo systemctl list-units --type=service | grep tee || echo 'No tee services found'"
done

echo "=== Checking SEV Nodes ==="
for INSTANCE in $SEV_INSTANCES; do
  IP=$(aws ec2 describe-instances --instance-ids $INSTANCE --region $REGION --query "Reservations[0].Instances[0].PublicIpAddress" --output text)
  echo "Checking SEV Node: $INSTANCE ($IP)"
  ssh -o StrictHostKeyChecking=no -i ~/.ssh/tee-access-key ubuntu@$IP "echo 'Connected to $IP'; hostname; uptime; ls -la /opt/tee 2>/dev/null || echo '/opt/tee not found'; ps aux | grep accumulator | grep -v grep || echo 'No accumulator process running'; sudo systemctl list-units --type=service | grep tee || echo 'No tee services found'"
done

echo "=== Check Complete ==="
