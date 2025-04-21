#!/bin/bash
# Update security groups to allow port 8080 for benchmarking
# This enables remote testing of the optimized TEE implementation

# Security Group IDs (from our deployment)
SGX_SG="sg-0b1e9b046c283f7f3"
SEV_SG="sg-0f19769de3e4e01cc"
REGION="us-east-1"

echo "Updating security groups to allow port 8080 access..."

# Add rule to SGX security group
aws ec2 authorize-security-group-ingress \
    --group-id $SGX_SG \
    --protocol tcp \
    --port 8080 \
    --cidr 0.0.0.0/0 \
    --region $REGION

# Add rule to SEV security group  
aws ec2 authorize-security-group-ingress \
    --group-id $SEV_SG \
    --protocol tcp \
    --port 8080 \
    --cidr 0.0.0.0/0 \
    --region $REGION

echo "Security group update complete."
echo "Verifying that nodes are accessible..."

# Sample IPs from our deployment
SGX_NODES=(
  "54.158.85.194"
  "54.91.177.223"
  "18.234.109.248"
  "3.208.29.137"
)

SEV_NODES=(
  "35.172.181.241"
  "3.91.64.154"
  "107.20.15.79" 
  "35.175.221.87"
)

# Function to check if port is open
check_port() {
  local host=$1
  local port=$2
  timeout 3 bash -c "echo > /dev/tcp/$host/$port" 2>/dev/null
  return $?
}

# Check SSH access (port 22)
echo "Testing SSH connectivity (port 22)..."
for node in "${SGX_NODES[@]}" "${SEV_NODES[@]}"; do
  if check_port $node 22; then
    echo "  ✅ SSH port open on $node"
  else
    echo "  ❌ SSH port closed on $node"
  fi
done

# Wait for security group changes to propagate
echo "Waiting 30 seconds for security group changes to propagate..."
sleep 30

# Check new port 8080 access
echo "Testing port 8080 connectivity..."
for node in "${SGX_NODES[@]}" "${SEV_NODES[@]}"; do
  if check_port $node 8080; then
    echo "  ✅ Port 8080 open on $node"
  else
    echo "  ❌ Port 8080 closed on $node"
  fi
done

echo "Security group update process complete."
