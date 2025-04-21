#!/bin/bash

# Optimized TEE Deployment Script for AWS
# This script deploys a high-performance TEE architecture with Intel SGX and AMD SEV nodes
# for NASDAQ performance benchmarking with the optimized RSA accumulator

set -e

# Configuration
STACK_NAME="optimized-tee-architecture"
REGION="us-east-1"  # Region where NASDAQ Kafka is hosted and AMD SEV is available
KEY_NAME=""
VPC_ID=""
SUBNET_ID=""
SGX_INSTANCE_TYPE="c5a.xlarge"
SEV_INSTANCE_TYPE="c6a.2xlarge"
SGX_NODE_COUNT=6   # Optimized for 6 SGX nodes
SEV_NODE_COUNT=6   # Optimized for 6 SEV nodes
BATCH_SIZE=1000    # Optimized batch size

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
    --batch-size)
      BATCH_SIZE="$2"
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
echo "=== Optimized TEE AWS Deployment ==="
echo "Stack Name: $STACK_NAME"
echo "Region: $REGION"
echo "Key Name: $KEY_NAME"
echo "VPC ID: $VPC_ID"
echo "Subnet ID: $SUBNET_ID"
echo "SGX Instance Type: $SGX_INSTANCE_TYPE"
echo "SEV Instance Type: $SEV_INSTANCE_TYPE"
echo "SGX Node Count: $SGX_NODE_COUNT"
echo "SEV Node Count: $SEV_NODE_COUNT"
echo "Batch Size: $BATCH_SIZE"
echo "==================================="

# Confirm AMD SEV availability in the target region
echo "Confirming AMD SEV availability in $REGION..."
SEV_INSTANCES=$(aws ec2 describe-instance-types --filters "Name=processor-info.supported-features,Values=amd-sev-snp" --region $REGION --query "length(InstanceTypes[?starts_with(InstanceType, 'c6a.')])" --output text)

if [ "$SEV_INSTANCES" -eq "0" ]; then
  echo "Error: AMD SEV instances not available in $REGION. Please choose a different region."
  exit 1
fi

echo "Confirmed AMD SEV is available in $REGION ($SEV_INSTANCES instance types)"

# Prepare optimized package
echo "Preparing optimized Go implementation package..."
TEMP_DIR=$(mktemp -d)
mkdir -p "$TEMP_DIR/go/src/github.com/rhombus-tech/optimized-tee"
cp -r $(dirname "$0")/../../accumulator "$TEMP_DIR/go/src/github.com/rhombus-tech/optimized-tee/"
cp -r $(dirname "$0")/../../integration/nasdaq "$TEMP_DIR/go/src/github.com/rhombus-tech/optimized-tee/"

# Modify batchSize in optimized_rsa_client.go
echo "Setting optimized batch size to $BATCH_SIZE..."
sed -i.bak "s/defaultBatchSize = [0-9]\+/defaultBatchSize = $BATCH_SIZE/" "$TEMP_DIR/go/src/github.com/rhombus-tech/optimized-tee/accumulator/optimized_rsa_client.go"

# Create user data script that includes optimized deployment
cat > "$TEMP_DIR/user_data.sh" <<EOF
#!/bin/bash
# Optimized TEE node setup script

# Install dependencies
apt-get update
apt-get install -y golang git

# Set up Go environment
mkdir -p /opt/rhombus/tee
export GOPATH=/opt/rhombus/tee/go
export PATH=\$PATH:\$GOPATH/bin

# Copy optimized implementation
mkdir -p \$GOPATH/src/github.com/rhombus-tech/optimized-tee
cp -r /tmp/optimized-tee/* \$GOPATH/src/github.com/rhombus-tech/optimized-tee/

# Build optimized TEE service
cd \$GOPATH/src/github.com/rhombus-tech/optimized-tee/accumulator
go build -o /opt/rhombus/tee/bin/optimized_tee_service

# Create systemd service for optimized TEE
cat > /etc/systemd/system/optimized-tee.service <<SEOF
[Unit]
Description=Optimized TEE Accumulator Service
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/rhombus/tee
ExecStart=/opt/rhombus/tee/bin/optimized_tee_service
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
SEOF

# Enable and start the service
systemctl daemon-reload
systemctl enable optimized-tee
systemctl start optimized-tee

# Create a status endpoint
cat > /opt/rhombus/tee/status.sh <<SEOF
#!/bin/bash
ID=\$(curl -s http://169.254.169.254/latest/meta-data/instance-id)
IP=\$(curl -s http://169.254.169.254/latest/meta-data/local-ipv4)
INSTANCE_TYPE=\$(curl -s http://169.254.169.254/latest/meta-data/instance-type)

if [[ "\$INSTANCE_TYPE" == c5a* ]]; then
  NODE_TYPE="SGX"
elif [[ "\$INSTANCE_TYPE" == c6a* ]]; then
  NODE_TYPE="SEV"
else
  NODE_TYPE="UNKNOWN"
fi

echo "{\\"node_type\\":\\"\$NODE_TYPE\\",\\"id\\":\\"\$ID\\",\\"ip\\":\\"\$IP\\",\\"status\\":\\"running\\",\\"timestamp\\":\\"\$(date -u +"%Y-%m-%dT%H:%M:%SZ\\")"}" > /opt/rhombus/tee/status.json
SEOF

chmod +x /opt/rhombus/tee/status.sh
EOF

# Create a custom parameters file for CloudFormation
cat > "$TEMP_DIR/parameters.json" <<EOF
[
  {
    "ParameterKey": "KeyName",
    "ParameterValue": "$KEY_NAME"
  },
  {
    "ParameterKey": "VpcId",
    "ParameterValue": "$VPC_ID"
  },
  {
    "ParameterKey": "SubnetId",
    "ParameterValue": "$SUBNET_ID"
  },
  {
    "ParameterKey": "SGXCount",
    "ParameterValue": "$SGX_NODE_COUNT"
  },
  {
    "ParameterKey": "SEVCount",
    "ParameterValue": "$SEV_NODE_COUNT"
  }
]
EOF

# Deploy CloudFormation stack
echo "Deploying CloudFormation stack..."
aws cloudformation create-stack \
  --stack-name $STACK_NAME \
  --template-body file://$(dirname "$0")/multi_tee_cluster.json \
  --parameters file://$TEMP_DIR/parameters.json \
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

# Upload optimized package to instances
echo "=== Uploading optimized implementation ==="
for INSTANCE in $SGX_INSTANCES $SEV_INSTANCES; do
  IP=$(aws ec2 describe-instances --instance-ids $INSTANCE --region $REGION --query "Reservations[0].Instances[0].PublicIpAddress" --output text)
  echo "Uploading to $INSTANCE ($IP)..."
  scp -o StrictHostKeyChecking=no -i "$KEY_NAME.pem" -r "$TEMP_DIR/go/src/github.com/rhombus-tech/optimized-tee/" ubuntu@$IP:/tmp/optimized-tee/
  ssh -o StrictHostKeyChecking=no -i "$KEY_NAME.pem" ubuntu@$IP "sudo bash /tmp/optimized-tee/user_data.sh"
done

# Clean up temp directory
rm -rf "$TEMP_DIR"

echo "=== Next Steps ==="
echo "1. SSH into the nodes to verify deployment: ssh -i $KEY_NAME.pem ubuntu@<ip-address>"
echo "2. Verify all 12 nodes are running the optimized TEE service"
echo "3. Run performance benchmark tests against the cluster"
echo "4. Generate performance report with actual hardware results"

exit 0
