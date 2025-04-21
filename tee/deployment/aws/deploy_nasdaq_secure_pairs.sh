#!/bin/bash

# NASDAQ Secure TEE Pairs Deployment Script
# Creates 4 pairs of SGX+SEV nodes with cross-attestation and secure parameter validation
# Optimized for WebAssembly parameter validation across TEE technologies

set -e

# Configuration
STACK_NAME="nasdaq-tee-secure-pairs"
REGION="us-east-1"
KEY_NAME="nasdaq-tee-key"
VPC_ID="vpc-0fad1036cebdbe4f9"
SUBNET_ID="subnet-0a66ac61606304093"
TEE_PAIR_COUNT=4  # 4 pairs of SGX+SEV nodes

# Parse command line arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    --stack-name)
      STACK_NAME="$2"
      shift 2
      ;;
    --region)
      REGION="$2"
      shift 2
      ;;
    --key-name)
      KEY_NAME="$2"
      shift 2
      ;;
    --vpc-id)
      VPC_ID="$2"
      shift 2
      ;;
    --subnet-id)
      SUBNET_ID="$2"
      shift 2
      ;;
    --pair-count)
      TEE_PAIR_COUNT="$2"
      shift 2
      ;;
    *)
      echo "Unknown option: $1"
      exit 1
      ;;
  esac
done

# Display configuration
echo "=== NASDAQ Secure TEE Pairs Deployment ==="
echo "Stack Name: $STACK_NAME"
echo "Region: $REGION"
echo "Key Name: $KEY_NAME"
echo "VPC ID: $VPC_ID"
echo "Subnet ID: $SUBNET_ID"
echo "TEE Pair Count: $TEE_PAIR_COUNT (${TEE_PAIR_COUNT} SGX + ${TEE_PAIR_COUNT} SEV)"
echo "========================================="

# Verify the key pair exists
if ! aws ec2 describe-key-pairs --key-names "$KEY_NAME" --region "$REGION" &>/dev/null; then
  echo "Error: Key pair '$KEY_NAME' not found. Please create it first."
  exit 1
fi

# Deploy CloudFormation stack using the proven multi_tee_pairs.json template
echo "Deploying CloudFormation stack with 1:1 TEE pairing..."
aws cloudformation create-stack \
  --stack-name $STACK_NAME \
  --template-body file://$(dirname "$0")/multi_tee_pairs.json \
  --parameters \
    ParameterKey=KeyName,ParameterValue=$KEY_NAME \
    ParameterKey=VpcId,ParameterValue=$VPC_ID \
    ParameterKey=SubnetId,ParameterValue=$SUBNET_ID \
    ParameterKey=PairCount,ParameterValue=$TEE_PAIR_COUNT \
  --capabilities CAPABILITY_IAM \
  --region $REGION

echo "Stack deployment initiated. Waiting for completion..."
aws cloudformation wait stack-create-complete --stack-name $STACK_NAME --region $REGION

# Get deployment outputs
echo "Retrieving deployment information..."

# Extract the IP addresses for each pair
echo "=== TEE Pairs Information ==="
for i in $(seq 1 $TEE_PAIR_COUNT); do
  SGX_IP=$(aws cloudformation describe-stacks --stack-name $STACK_NAME --region $REGION --query "Stacks[0].Outputs[?OutputKey=='SGXIP${i}'].OutputValue" --output text)
  SEV_IP=$(aws cloudformation describe-stacks --stack-name $STACK_NAME --region $REGION --query "Stacks[0].Outputs[?OutputKey=='SEVIP${i}'].OutputValue" --output text)
  echo "Pair $i:"
  echo "  SGX Node: $SGX_IP"
  echo "  SEV Node: $SEV_IP"
done

# Deploy the optimized accumulator to all nodes
echo "=== Deploying Optimized Accumulator ==="
echo "Waiting for nodes to complete initialization (60 seconds)..."
sleep 60

# Update the nodes with optimized accumulator configuration
for i in $(seq 1 $TEE_PAIR_COUNT); do
  SGX_IP=$(aws cloudformation describe-stacks --stack-name $STACK_NAME --region $REGION --query "Stacks[0].Outputs[?OutputKey=='SGXIP${i}'].OutputValue" --output text)
  SEV_IP=$(aws cloudformation describe-stacks --stack-name $STACK_NAME --region $REGION --query "Stacks[0].Outputs[?OutputKey=='SEVIP${i}'].OutputValue" --output text)
  
  echo "Configuring SGX Node $i ($SGX_IP)..."
  ssh -o StrictHostKeyChecking=no -i ~/nasdaq-tee-key.pem ubuntu@$SGX_IP "sudo mkdir -p /opt/rhombus/tee/accumulator"
  ssh -o StrictHostKeyChecking=no -i ~/nasdaq-tee-key.pem ubuntu@$SGX_IP "echo 'defaultBatchSize = 1000' | sudo tee /opt/rhombus/tee/accumulator/config.go"
  ssh -o StrictHostKeyChecking=no -i ~/nasdaq-tee-key.pem ubuntu@$SGX_IP "echo 'defaultThreads = 8' | sudo tee -a /opt/rhombus/tee/accumulator/config.go"
  
  echo "Configuring SEV Node $i ($SEV_IP)..."
  ssh -o StrictHostKeyChecking=no -i ~/nasdaq-tee-key.pem ubuntu@$SEV_IP "sudo mkdir -p /opt/rhombus/tee/accumulator"
  ssh -o StrictHostKeyChecking=no -i ~/nasdaq-tee-key.pem ubuntu@$SEV_IP "echo 'defaultBatchSize = 1000' | sudo tee /opt/rhombus/tee/accumulator/config.go"
  ssh -o StrictHostKeyChecking=no -i ~/nasdaq-tee-key.pem ubuntu@$SEV_IP "echo 'defaultThreads = 8' | sudo tee -a /opt/rhombus/tee/accumulator/config.go"
done

echo "=== Deployment Complete ==="
echo "Your NASDAQ TEE infrastructure with secure parameter validation is ready."
echo "Access your nodes using: ssh -i ~/nasdaq-tee-key.pem ubuntu@<NODE_IP>"
echo ""
echo "Testing WebAssembly Parameter Validation:"
echo "1. SSH into any node"
echo "2. Run validation test with length-prefixed format:"
echo "   curl http://localhost:7070/validate?format=length-prefixed"
echo "3. Run validation test with direct data format:"
echo "   curl http://localhost:7070/validate?format=direct"
echo "4. Test cross-attestation between paired nodes:"
echo "   curl http://localhost:7070/verify-partner"
echo ""
echo "For performance benchmarking, use the paired_node_performance_test.py script."
