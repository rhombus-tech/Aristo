#!/bin/bash
# Script to shut down AWS resources for the TEE mesh network
set -e

echo "Reading deployment information..."
COORDINATOR_IP=$(cat tee_deployment_info.json | grep -o '"coordinator": "[^"]*"' | cut -d'"' -f4)
SGX_IP_1=$(cat tee_deployment_info.json | grep -o '"sgx": "[^"]*"' | head -1 | cut -d'"' -f4)
SGX_IP_2=$(cat tee_deployment_info.json | grep -o '"sgx": "[^"]*"' | tail -1 | cut -d'"' -f4)
SEV_IP_1=$(cat tee_deployment_info.json | grep -o '"sev": "[^"]*"' | head -1 | cut -d'"' -f4)
SEV_IP_2=$(cat tee_deployment_info.json | grep -o '"sev": "[^"]*"' | tail -1 | cut -d'"' -f4)

echo "Finding instance IDs based on IP addresses..."
COORDINATOR_ID=$(aws ec2 describe-instances --filters "Name=ip-address,Values=${COORDINATOR_IP}" --query "Reservations[].Instances[].InstanceId" --output text)
SGX_ID_1=$(aws ec2 describe-instances --filters "Name=ip-address,Values=${SGX_IP_1}" --query "Reservations[].Instances[].InstanceId" --output text)
SGX_ID_2=$(aws ec2 describe-instances --filters "Name=ip-address,Values=${SGX_IP_2}" --query "Reservations[].Instances[].InstanceId" --output text)
SEV_ID_1=$(aws ec2 describe-instances --filters "Name=ip-address,Values=${SEV_IP_1}" --query "Reservations[].Instances[].InstanceId" --output text)
SEV_ID_2=$(aws ec2 describe-instances --filters "Name=ip-address,Values=${SEV_IP_2}" --query "Reservations[].Instances[].InstanceId" --output text)

# Create a list of all instance IDs, filtering out empty strings
INSTANCE_IDS=()
for ID in "$COORDINATOR_ID" "$SGX_ID_1" "$SGX_ID_2" "$SEV_ID_1" "$SEV_ID_2"; do
    if [ ! -z "$ID" ]; then
        INSTANCE_IDS+=("$ID")
    fi
done

if [ ${#INSTANCE_IDS[@]} -eq 0 ]; then
    echo "No running instances found with the specified IP addresses."
    echo "It's possible they've already been terminated or the IP addresses are no longer valid."
    exit 0
fi

echo "Found ${#INSTANCE_IDS[@]} instances to terminate:"
for ID in "${INSTANCE_IDS[@]}"; do
    echo "  - $ID"
done

echo "Terminating instances..."
aws ec2 terminate-instances --instance-ids ${INSTANCE_IDS[@]}

echo "Waiting for instances to terminate..."
aws ec2 wait instance-terminated --instance-ids ${INSTANCE_IDS[@]}

echo "Cleaning up security groups..."
# Note: This assumes the security group name used in deploy_single_region.sh
SG_ID=$(aws ec2 describe-security-groups --group-names tee-mesh-sg --query "SecurityGroups[0].GroupId" --output text 2>/dev/null || echo "")

if [ ! -z "$SG_ID" ] && [ "$SG_ID" != "None" ]; then
    echo "Deleting security group $SG_ID..."
    aws ec2 delete-security-group --group-id $SG_ID || echo "Could not delete security group. It may be in use by other resources."
fi

echo "All AWS resources have been successfully shut down."
echo "Creating a backup of the deployment info..."
mv tee_deployment_info.json tee_deployment_info.json.bak

echo "Shutdown completed successfully."
