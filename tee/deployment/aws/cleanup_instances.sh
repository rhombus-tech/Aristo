#!/bin/bash
# Cleanup script for TEE instances
# Frees up disk space for deployment of the enhanced RSA validator

set -e

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# NASDAQ SSH Key path
SSH_KEY="~/nasdaq-tee-key.pem"

# Check if SSH key exists
if [ ! -f "$(eval echo $SSH_KEY)" ]; then
    echo -e "${RED}NASDAQ SSH key not found: $SSH_KEY${NC}"
    echo -e "${YELLOW}Please ensure ~/nasdaq-tee-key.pem exists${NC}"
    exit 1
fi

# SSH options
SSH_OPTS="-o StrictHostKeyChecking=no -o ConnectTimeout=10 -i $(eval echo $SSH_KEY)"

echo -e "${GREEN}=== Cleaning Up Disk Space on TEE Instances ===${NC}"

# Get instance data directly using AWS CLI
echo -e "${YELLOW}Looking for running EC2 instances...${NC}"

# Get instance IDs and IPs in a simple format
INSTANCE_DATA=$(aws ec2 describe-instances --filters "Name=instance-state-name,Values=running" --query "Reservations[*].Instances[*].[InstanceId,PublicIpAddress]" --output text)

# Parse the INSTANCE_DATA into separate arrays
INSTANCE_IDS=()
INSTANCE_IPS=()

while read -r ID IP; do
    INSTANCE_IDS+=("$ID")
    INSTANCE_IPS+=("$IP")
done <<< "$INSTANCE_DATA"

# Count number of running instances
INSTANCE_COUNT=${#INSTANCE_IDS[@]}

echo -e "${GREEN}Found $INSTANCE_COUNT running instances.${NC}"

# Display found instances
for ((i=0; i<INSTANCE_COUNT; i++)); do
    echo -e "${YELLOW}Instance ${INSTANCE_IDS[$i]} has IP ${INSTANCE_IPS[$i]}${NC}"
done

# Function to clean up an instance
cleanup_instance() {
    local IP=$1
    local ID=$2
    
    echo -e "${GREEN}Cleaning up instance $ID ($IP)...${NC}"
    
    # Run cleanup commands
    ssh $SSH_OPTS ubuntu@$IP "
        # Print disk usage before cleanup
        echo 'Disk usage before cleanup:'
        df -h /
        
        # Clean apt cache
        echo 'Cleaning apt cache...'
        sudo apt-get clean
        sudo apt-get autoclean
        
        # Remove old downloaded archive files
        echo 'Removing old package archives...'
        sudo apt-get autoremove -y
        
        # Clean logs
        echo 'Cleaning log files...'
        sudo find /var/log -type f -name '*.gz' -delete
        sudo find /var/log -type f -name '*.log.*' -delete
        sudo truncate -s 0 /var/log/*.log
        
        # Clean temp files
        echo 'Cleaning temp files...'
        sudo rm -rf /tmp/*
        sudo rm -rf /var/tmp/*
        
        # Clean old HyperSDK artifacts if any
        echo 'Cleaning old HyperSDK artifacts...'
        rm -rf ~/hyper_src.tar.gz
        
        # Remove old TEE validator files if any
        echo 'Cleaning old TEE validator files...'
        rm -rf ~/tee_validator
        
        # Clean home directory
        echo 'Cleaning home directory...'
        rm -rf ~/.cache/*
        
        # Clean old enarx files if any
        echo 'Cleaning old Enarx build files...'
        if [ -d ~/enarx ]; then
            rm -rf ~/enarx/target
        fi
        
        # Docker cleanup if installed
        if command -v docker &> /dev/null; then
            echo 'Cleaning Docker artifacts...'
            sudo docker system prune -f
        fi
        
        # Print disk usage after cleanup
        echo 'Disk usage after cleanup:'
        df -h /
    "
    
    echo -e "${GREEN}Cleanup completed for instance $ID ($IP)${NC}"
    return 0
}

# Clean up each instance
for ((i=0; i<INSTANCE_COUNT; i++)); do
    cleanup_instance "${INSTANCE_IPS[$i]}" "${INSTANCE_IDS[$i]}" || true
done

echo -e "${GREEN}=== Cleanup Complete ===${NC}"
echo -e "${YELLOW}You can now proceed with deploying the enhanced RSA validator${NC}"
