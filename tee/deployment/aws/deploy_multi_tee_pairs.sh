#!/bin/bash

# Script to deploy multiple TEE pairs using the dual_tee_minimal.json template
# Each pair is deployed as a separate CloudFormation stack

set -e

# Configuration
BASE_STACK_NAME="nasdaq-tee-poc"
REGION="us-east-1"
KEY_NAME="nasdaq-tee-key"
VPC_ID="vpc-0fad1036cebdbe4f9"
SUBNET_ID="subnet-0a66ac61606304093"
PAIR_COUNT=4

# Parse command line arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    --base-stack-name)
      BASE_STACK_NAME="$2"
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
      PAIR_COUNT="$2"
      shift 2
      ;;
    *)
      echo "Unknown option: $1"
      exit 1
      ;;
  esac
done

# Display configuration
echo "=== NASDAQ Multi-TEE Deployment ==="
echo "Base Stack Name: $BASE_STACK_NAME"
echo "Region: $REGION"
echo "Key Name: $KEY_NAME"
echo "VPC ID: $VPC_ID"
echo "Subnet ID: $SUBNET_ID"
echo "TEE Pair Count: $PAIR_COUNT"
echo "=============================="

# Verify the key pair exists
if ! aws ec2 describe-key-pairs --key-names "$KEY_NAME" --region "$REGION" &>/dev/null; then
  echo "Error: Key pair '$KEY_NAME' not found. Please create it first."
  exit 1
fi

# Deploy each TEE pair
for i in $(seq 1 $PAIR_COUNT); do
  STACK_NAME="${BASE_STACK_NAME}-pair-${i}"
  
  echo "Deploying TEE pair $i of $PAIR_COUNT (Stack: $STACK_NAME)..."
  
  # Check if stack already exists
  if aws cloudformation describe-stacks --stack-name $STACK_NAME --region $REGION &>/dev/null; then
    echo "Stack $STACK_NAME already exists. Skipping."
    continue
  fi
  
  # Deploy CloudFormation stack
  aws cloudformation create-stack \
    --stack-name $STACK_NAME \
    --template-body file://$(dirname "$0")/dual_tee_minimal.json \
    --parameters \
      ParameterKey=KeyName,ParameterValue=$KEY_NAME \
      ParameterKey=VpcId,ParameterValue=$VPC_ID \
      ParameterKey=SubnetId,ParameterValue=$SUBNET_ID \
    --capabilities CAPABILITY_IAM \
    --region $REGION
    
  echo "Stack creation initiated for pair $i. Waiting for stack to be created..."
done

# Wait for all stacks to complete
echo "Waiting for all stacks to complete..."
for i in $(seq 1 $PAIR_COUNT); do
  STACK_NAME="${BASE_STACK_NAME}-pair-${i}"
  aws cloudformation wait stack-create-complete --stack-name $STACK_NAME --region $REGION
  echo "Stack $STACK_NAME creation completed!"
done

# Display TEE pair information
echo -e "\n=== TEE Pair Information ==="
for i in $(seq 1 $PAIR_COUNT); do
  STACK_NAME="${BASE_STACK_NAME}-pair-${i}"
  
  # Get output values for the stack
  SGX_IP=$(aws cloudformation describe-stacks --stack-name $STACK_NAME --region $REGION --query "Stacks[0].Outputs[?OutputKey=='SGXNodePublicIP'].OutputValue" --output text)
  SEV_IP=$(aws cloudformation describe-stacks --stack-name $STACK_NAME --region $REGION --query "Stacks[0].Outputs[?OutputKey=='SEVNodePublicIP'].OutputValue" --output text)
  
  echo "Pair $i:"
  echo "  SGX Node: $SGX_IP"
  echo "  SEV Node: $SEV_IP"
done

echo -e "\nAll $PAIR_COUNT TEE pairs have been deployed successfully!"
