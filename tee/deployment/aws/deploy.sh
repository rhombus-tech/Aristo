#!/bin/bash

# Dual TEE Deployment Script for AWS
# This script deploys a dual TEE architecture with Intel SGX and AMD SEV nodes
# for cross-attestation in the same region as NASDAQ Kafka

set -e

# Configuration
STACK_NAME="dual-tee-architecture"
REGION="us-east-1"  # Region where NASDAQ Kafka is hosted and AMD SEV is available
KEY_NAME=""
VPC_ID=""
SUBNET_ID=""
SGX_INSTANCE_TYPE="c5a.xlarge"
SEV_INSTANCE_TYPE="c6a.2xlarge"
SGX_NODE_COUNT=2
SEV_NODE_COUNT=2

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
    --sgx-instance-type)
      SGX_INSTANCE_TYPE="$2"
      shift 2
      ;;
    --sev-instance-type)
      SEV_INSTANCE_TYPE="$2"
      shift 2
      ;;
    --sgx-count)
      SGX_NODE_COUNT="$2"
      shift 2
      ;;
    --sev-count)
      SEV_NODE_COUNT="$2"
      shift 2
      ;;
    *)
      echo "Unknown option: $1"
      exit 1
      ;;
  esac
done

# Validate required parameters
if [ -z "$KEY_NAME" ]; then
  echo "Error: --key-name is required"
  exit 1
fi

if [ -z "$VPC_ID" ]; then
  echo "Error: --vpc-id is required"
  exit 1
fi

if [ -z "$SUBNET_ID" ]; then
  echo "Error: --subnet-id is required"
  exit 1
fi

# Display configuration
echo "=== Dual TEE AWS Deployment ==="
echo "Stack Name: $STACK_NAME"
echo "Region: $REGION"
echo "Key Name: $KEY_NAME"
echo "VPC ID: $VPC_ID"
echo "Subnet ID: $SUBNET_ID"
echo "SGX Instance Type: $SGX_INSTANCE_TYPE"
echo "SEV Instance Type: $SEV_INSTANCE_TYPE"
echo "SGX Node Count: $SGX_NODE_COUNT"
echo "SEV Node Count: $SEV_NODE_COUNT"
echo "==============================="

# Confirm AMD SEV availability in the target region
echo "Confirming AMD SEV availability in $REGION..."
SEV_INSTANCES=$(aws ec2 describe-instance-types --filters "Name=processor-info.supported-features,Values=amd-sev-snp" --region $REGION --query "length(InstanceTypes[?starts_with(InstanceType, 'c6a.')])" --output text)

if [ "$SEV_INSTANCES" -eq "0" ]; then
  echo "Error: AMD SEV instances not available in $REGION. Please choose a different region."
  exit 1
fi

echo "Confirmed AMD SEV is available in $REGION ($SEV_INSTANCES instance types)"

# Deploy CloudFormation stack
echo "Deploying CloudFormation stack..."
aws cloudformation create-stack \
  --stack-name $STACK_NAME \
  --template-body file://$(dirname "$0")/cloudformation.json \
  --parameters \
    ParameterKey=KeyName,ParameterValue=$KEY_NAME \
    ParameterKey=VpcId,ParameterValue=$VPC_ID \
    ParameterKey=SubnetId,ParameterValue=$SUBNET_ID \
    ParameterKey=SGXInstanceType,ParameterValue=$SGX_INSTANCE_TYPE \
    ParameterKey=SEVInstanceType,ParameterValue=$SEV_INSTANCE_TYPE \
    ParameterKey=SGXNodeCount,ParameterValue=$SGX_NODE_COUNT \
    ParameterKey=SEVNodeCount,ParameterValue=$SEV_NODE_COUNT \
  --capabilities CAPABILITY_IAM \
  --region $REGION

echo "Stack deployment initiated. Waiting for completion..."
aws cloudformation wait stack-create-complete --stack-name $STACK_NAME --region $REGION

# Get deployment outputs
echo "Retrieving deployment information..."
SGX_GROUP=$(aws cloudformation describe-stacks --stack-name $STACK_NAME --region $REGION --query "Stacks[0].Outputs[?OutputKey=='SGXNodeGroupName'].OutputValue" --output text)
SEV_GROUP=$(aws cloudformation describe-stacks --stack-name $STACK_NAME --region $REGION --query "Stacks[0].Outputs[?OutputKey=='SEVNodeGroupName'].OutputValue" --output text)
NASDAQ_CONNECTOR_IP=$(aws cloudformation describe-stacks --stack-name $STACK_NAME --region $REGION --query "Stacks[0].Outputs[?OutputKey=='NASDAQConnectorIP'].OutputValue" --output text)

echo "=== Deployment Complete ==="
echo "SGX Node Group: $SGX_GROUP"
echo "SEV Node Group: $SEV_GROUP"
echo "NASDAQ Connector IP: $NASDAQ_CONNECTOR_IP"

# Get instance information
echo "=== SGX Node Instances ==="
SGX_INSTANCES=$(aws autoscaling describe-auto-scaling-groups --auto-scaling-group-names $SGX_GROUP --region $REGION --query "AutoScalingGroups[0].Instances[*].InstanceId" --output text)
for INSTANCE in $SGX_INSTANCES; do
  IP=$(aws ec2 describe-instances --instance-ids $INSTANCE --region $REGION --query "Reservations[0].Instances[0].PrivateIpAddress" --output text)
  echo "Instance ID: $INSTANCE, IP: $IP"
done

echo "=== SEV Node Instances ==="
SEV_INSTANCES=$(aws autoscaling describe-auto-scaling-groups --auto-scaling-group-names $SEV_GROUP --region $REGION --query "AutoScalingGroups[0].Instances[*].InstanceId" --output text)
for INSTANCE in $SEV_INSTANCES; do
  IP=$(aws ec2 describe-instances --instance-ids $INSTANCE --region $REGION --query "Reservations[0].Instances[0].PrivateIpAddress" --output text)
  echo "Instance ID: $INSTANCE, IP: $IP"
done

echo "=== Next Steps ==="
echo "1. SSH into the nodes to verify deployment"
echo "2. Configure cross-attestation between SGX and SEV nodes"
echo "3. Set up NASDAQ market data connection"
echo "4. Run parameter validation tests for WebAssembly contracts"

exit 0
