#!/bin/bash

# Optimized TEE Deployment Script for AWS
# This script deploys a high-performance TEE architecture with Intel SGX and AMD SEV nodes
# for NASDAQ performance benchmarking with the optimized RSA accumulator

set -e

# Configuration
STACK_NAME="optimized-tee-architecture"
REGION="us-east-1"  # Region where NASDAQ Kafka is hosted and AMD SEV is available
KEY_NAME="tee-access-key"
VPC_ID="vpc-0fad1036cebdbe4f9"
SUBNET_ID="subnet-0a66ac61606304093"
SGX_COUNT=6
SEV_COUNT=6

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
    --sgx-count)
      SGX_COUNT="$2"
      shift 2
      ;;
    --sev-count)
      SEV_COUNT="$2"
      shift 2
      ;;
    *)
      echo "Unknown option: $1"
      exit 1
      ;;
  esac
done

# Display configuration
echo "=== Optimized TEE AWS Deployment ==="
echo "Stack Name: $STACK_NAME"
echo "Region: $REGION"
echo "Key Name: $KEY_NAME"
echo "VPC ID: $VPC_ID"
echo "Subnet ID: $SUBNET_ID"
echo "SGX Node Count: $SGX_COUNT"
echo "SEV Node Count: $SEV_COUNT"
echo "==================================="

# Deploy CloudFormation stack
echo "Deploying CloudFormation stack..."
aws cloudformation create-stack \
  --stack-name $STACK_NAME \
  --template-body file://$(dirname "$0")/optimized_tee_cluster.json \
  --parameters \
    ParameterKey=KeyName,ParameterValue=$KEY_NAME \
    ParameterKey=VpcId,ParameterValue=$VPC_ID \
    ParameterKey=SubnetId,ParameterValue=$SUBNET_ID \
    ParameterKey=SGXCount,ParameterValue=$SGX_COUNT \
    ParameterKey=SEVCount,ParameterValue=$SEV_COUNT \
  --capabilities CAPABILITY_IAM \
  --region $REGION

echo "Stack deployment initiated. Waiting for completion..."
aws cloudformation wait stack-create-complete --stack-name $STACK_NAME --region $REGION

# Get deployment outputs
echo "Retrieving deployment information..."
SGX_GROUP=$(aws cloudformation describe-stacks --stack-name $STACK_NAME --region $REGION --query "Stacks[0].Outputs[?OutputKey=='SGXNodeGroupName'].OutputValue" --output text)
SEV_GROUP=$(aws cloudformation describe-stacks --stack-name $STACK_NAME --region $REGION --query "Stacks[0].Outputs[?OutputKey=='SEVNodeGroupName'].OutputValue" --output text)

echo "=== Deployment Complete ==="
echo "SGX Node Group: $SGX_GROUP"
echo "SEV Node Group: $SEV_GROUP"

# Get instance information
echo "=== SGX Node Instances ==="
SGX_INSTANCES=$(aws autoscaling describe-auto-scaling-groups --auto-scaling-group-names $SGX_GROUP --region $REGION --query "AutoScalingGroups[0].Instances[*].InstanceId" --output text)
for INSTANCE in $SGX_INSTANCES; do
  IP=$(aws ec2 describe-instances --instance-ids $INSTANCE --region $REGION --query "Reservations[0].Instances[0].PrivateIpAddress" --output text)
  PUBLIC_IP=$(aws ec2 describe-instances --instance-ids $INSTANCE --region $REGION --query "Reservations[0].Instances[0].PublicIpAddress" --output text)
  echo "Instance ID: $INSTANCE, Private IP: $IP, Public IP: $PUBLIC_IP"
done

echo "=== SEV Node Instances ==="
SEV_INSTANCES=$(aws autoscaling describe-auto-scaling-groups --auto-scaling-group-names $SEV_GROUP --region $REGION --query "AutoScalingGroups[0].Instances[*].InstanceId" --output text)
for INSTANCE in $SEV_INSTANCES; do
  IP=$(aws ec2 describe-instances --instance-ids $INSTANCE --region $REGION --query "Reservations[0].Instances[0].PrivateIpAddress" --output text)
  PUBLIC_IP=$(aws ec2 describe-instances --instance-ids $INSTANCE --region $REGION --query "Reservations[0].Instances[0].PublicIpAddress" --output text)
  echo "Instance ID: $INSTANCE, Private IP: $IP, Public IP: $PUBLIC_IP"
done

echo "=== Next Steps ==="
echo "1. SSH into the nodes to verify deployment: ssh -i ${KEY_NAME}.pem ubuntu@<public-ip>"
echo "2. Check service status: sudo systemctl status optimized-tee"
echo "3. Run performance benchmark tests on the cluster"
echo "4. Total expected performance: ~65,000 TPS with all 12 nodes"

exit 0
