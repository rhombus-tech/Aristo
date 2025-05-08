#!/bin/bash
# Deploy single region with 2 TEE pairs (2 SGX + 2 SEV instances)
set -e

# Configuration
REGION_ID="east-1"
STACK_NAME="tee-mesh-${REGION_ID}"
KEY_NAME="tee-access-key"
SGX_INSTANCE_TYPE="c5a.xlarge"
SEV_INSTANCE_TYPE="c6a.2xlarge"
COORDINATOR_INSTANCE_TYPE="m5.large"

echo "Deploying TEE mesh network to region ${REGION_ID}"

# Create security group for TEE mesh
echo "Creating security group for TEE mesh network..."
SG_ID=$(aws ec2 create-security-group \
  --group-name tee-mesh-sg \
  --description "Security group for TEE mesh network" \
  --output text \
  --query 'GroupId')

# Allow SSH and TEE service ports
aws ec2 authorize-security-group-ingress \
  --group-id ${SG_ID} \
  --protocol tcp \
  --port 22 \
  --cidr 0.0.0.0/0

aws ec2 authorize-security-group-ingress \
  --group-id ${SG_ID} \
  --protocol tcp \
  --port 7070-7080 \
  --cidr 0.0.0.0/0

# Deploy coordinator instance
echo "Deploying coordinator instance..."
COORDINATOR_ID=$(aws ec2 run-instances \
  --image-id ami-030f04819b19327fc \
  --instance-type ${COORDINATOR_INSTANCE_TYPE} \
  --key-name ${KEY_NAME} \
  --security-group-ids ${SG_ID} \
  --tag-specifications "ResourceType=instance,Tags=[{Key=Name,Value=coordinator-${REGION_ID}}]" \
  --output text \
  --query 'Instances[0].InstanceId')

# Deploy SGX instances (2)
echo "Deploying SGX instances..."
SGX_IDS=($(aws ec2 run-instances \
  --image-id ami-030f04819b19327fc \
  --count 2 \
  --instance-type ${SGX_INSTANCE_TYPE} \
  --key-name ${KEY_NAME} \
  --security-group-ids ${SG_ID} \
  --tag-specifications "ResourceType=instance,Tags=[{Key=Name,Value=sgx-${REGION_ID}}]" \
  --output text \
  --query 'Instances[*].InstanceId'))

# Deploy SEV instances (2)
echo "Deploying SEV instances..."
SEV_IDS=($(aws ec2 run-instances \
  --image-id ami-030f04819b19327fc \
  --count 2 \
  --instance-type ${SEV_INSTANCE_TYPE} \
  --key-name ${KEY_NAME} \
  --security-group-ids ${SG_ID} \
  --tag-specifications "ResourceType=instance,Tags=[{Key=Name,Value=sev-${REGION_ID}}]" \
  --output text \
  --query 'Instances[*].InstanceId'))

echo "Waiting for instances to be ready..."
aws ec2 wait instance-running --instance-ids ${COORDINATOR_ID} ${SGX_IDS[0]} ${SGX_IDS[1]} ${SEV_IDS[0]} ${SEV_IDS[1]}

# Get public IPs
COORDINATOR_IP=$(aws ec2 describe-instances --instance-ids ${COORDINATOR_ID} --query 'Reservations[0].Instances[0].PublicIpAddress' --output text)
SGX_IP_1=$(aws ec2 describe-instances --instance-ids ${SGX_IDS[0]} --query 'Reservations[0].Instances[0].PublicIpAddress' --output text)
SGX_IP_2=$(aws ec2 describe-instances --instance-ids ${SGX_IDS[1]} --query 'Reservations[0].Instances[0].PublicIpAddress' --output text)
SEV_IP_1=$(aws ec2 describe-instances --instance-ids ${SEV_IDS[0]} --query 'Reservations[0].Instances[0].PublicIpAddress' --output text)
SEV_IP_2=$(aws ec2 describe-instances --instance-ids ${SEV_IDS[1]} --query 'Reservations[0].Instances[0].PublicIpAddress' --output text)

echo "Infrastructure deployed successfully:"
echo "Coordinator: ${COORDINATOR_IP}"
echo "SGX Node 1: ${SGX_IP_1}"
echo "SGX Node 2: ${SGX_IP_2}"
echo "SEV Node 1: ${SEV_IP_1}"
echo "SEV Node 2: ${SEV_IP_2}"

# Save information for later use
cat > tee_deployment_info.json << EOF
{
  "region_id": "${REGION_ID}",
  "coordinator": "${COORDINATOR_IP}",
  "tee_pairs": [
    {
      "sgx": "${SGX_IP_1}",
      "sev": "${SEV_IP_1}"
    },
    {
      "sgx": "${SGX_IP_2}",
      "sev": "${SEV_IP_2}"
    }
  ]
}
EOF

echo "Deployment information saved to tee_deployment_info.json"
echo "Run ./configure_mesh.sh to configure the TEE mesh network"
