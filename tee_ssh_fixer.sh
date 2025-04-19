#!/bin/bash
# TEE SSH Connection Fixer - Diagnoses and fixes SSH connection issues

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Set to false to disable verbose SSH debugging
VERBOSE_SSH=true

# Instance IDs and their IPs - edit as needed
declare -A INSTANCE_IPS
INSTANCE_IPS["i-0600a681a918ebcb7"]="3.82.138.122"     # SGX Node 1
INSTANCE_IPS["i-066491a2fc0856d81"]="52.207.116.18"    # SGX Node 2
INSTANCE_IPS["i-020abb3c8b81087a5"]="44.203.182.22"    # SEV Node 1
INSTANCE_IPS["i-081ae726b8eebc482"]="34.205.69.30"     # SEV Node 2

# SSH key path
SSH_KEY_PATH=~/.ssh/tee_access_key

# Test if an instance is responsive
test_instance() {
    local instance_id=$1
    local ip=${INSTANCE_IPS[$instance_id]}
    echo -e "${BLUE}Testing SSH connectivity to Instance: $instance_id ($ip)${NC}"

    # Step 1: Basic ping test to check network connectivity
    echo -e "${YELLOW}Step 1: Checking network connectivity with ping...${NC}"
    ping -c 3 -W 2 $ip
    local ping_status=$?
    
    if [ $ping_status -ne 0 ]; then
        echo -e "${RED}ERROR: Network unreachable. Ping failed.${NC}"
    else
        echo -e "${GREEN}SUCCESS: Network is reachable.${NC}"
    fi

    # Step 2: TCP port check for SSH (port 22)
    echo -e "\n${YELLOW}Step 2: Checking if SSH port (22) is open...${NC}"
    nc -z -v -w 5 $ip 22
    local port_status=$?
    
    if [ $port_status -ne 0 ]; then
        echo -e "${RED}ERROR: SSH port 22 is not reachable. Security group issue or SSH not running.${NC}"
    else
        echo -e "${GREEN}SUCCESS: SSH port 22 is open.${NC}"
    fi

    # Step 3: Basic SSH connection attempt
    echo -e "\n${YELLOW}Step 3: Basic SSH connection test (password-less)...${NC}"
    timeout 5 ssh -o BatchMode=yes -o StrictHostKeyChecking=no -o ConnectTimeout=5 $ip echo "SSH Connectivity Test" 2>&1
    local ssh_basic_status=$?
    
    if [ $ssh_basic_status -eq 255 ]; then
        echo -e "${RED}ERROR: SSH connection failed - authentication issues.${NC}"
    elif [ $ssh_basic_status -eq 124 ]; then
        echo -e "${RED}ERROR: SSH connection timed out.${NC}"
    elif [ $ssh_basic_status -ne 0 ]; then
        echo -e "${RED}ERROR: SSH connection failed with status $ssh_basic_status.${NC}"
    else
        echo -e "${GREEN}SUCCESS: Basic SSH connection successful!${NC}"
    fi
    
    # Step 4: Try SSH with our key
    echo -e "\n${YELLOW}Step 4: Testing SSH with our TEE access key...${NC}"
    if [ $VERBOSE_SSH = true ]; then
        ssh -v -i $SSH_KEY_PATH -o StrictHostKeyChecking=no -o ConnectTimeout=5 ubuntu@$ip echo "SSH Key Test" 2>&1
    else
        ssh -i $SSH_KEY_PATH -o StrictHostKeyChecking=no -o ConnectTimeout=5 ubuntu@$ip echo "SSH Key Test" 2>&1
    fi
    local ssh_status=$?
    
    if [ $ssh_status -ne 0 ]; then
        echo -e "${RED}ERROR: SSH with key failed with status $ssh_status.${NC}"
        echo -e "${YELLOW}Trying with ec2-user instead of ubuntu...${NC}"
        
        if [ $VERBOSE_SSH = true ]; then
            ssh -v -i $SSH_KEY_PATH -o StrictHostKeyChecking=no -o ConnectTimeout=5 ec2-user@$ip echo "SSH Key Test" 2>&1
        else
            ssh -i $SSH_KEY_PATH -o StrictHostKeyChecking=no -o ConnectTimeout=5 ec2-user@$ip echo "SSH Key Test" 2>&1
        fi
        
        local ssh_alt_status=$?
        if [ $ssh_alt_status -ne 0 ]; then
            echo -e "${RED}ERROR: SSH with ec2-user also failed.${NC}"
        else
            echo -e "${GREEN}SUCCESS: SSH with ec2-user successful!${NC}"
        fi
    else
        echo -e "${GREEN}SUCCESS: SSH with ubuntu user successful!${NC}"
    fi
}

# Check SSH key permissions
fix_key_permissions() {
    echo -e "${BLUE}Checking and fixing SSH key permissions...${NC}"
    if [ ! -f "$SSH_KEY_PATH" ]; then
        echo -e "${RED}ERROR: SSH key not found at $SSH_KEY_PATH${NC}"
        return 1
    fi
    
    chmod 600 $SSH_KEY_PATH
    echo -e "${GREEN}Key permissions set to 600 (user read/write only)${NC}"
    ls -la $SSH_KEY_PATH
}

# Get instance status
check_instance_status() {
    local instance_id=$1
    echo -e "${BLUE}Checking status of Instance: $instance_id${NC}"
    aws ec2 describe-instances --instance-ids $instance_id --query "Reservations[].Instances[].[InstanceId, State.Name, PublicIpAddress, PublicDnsName]" --output table
}

# Try to fix SSH by resetting the user data and restarting the instance
fix_ssh_with_restart() {
    local instance_id=$1
    echo -e "${BLUE}Attempting to fix SSH for Instance: $instance_id${NC}"
    
    # Stop the instance
    echo -e "${YELLOW}Stopping the instance...${NC}"
    aws ec2 stop-instances --instance-ids $instance_id
    
    # Wait for the instance to stop
    echo -e "${YELLOW}Waiting for instance to stop...${NC}"
    aws ec2 wait instance-stopped --instance-ids $instance_id
    
    # Create user data script
    echo -e "${YELLOW}Preparing user data script...${NC}"
    cat > /tmp/tee_add_key.sh << 'EOF'
#!/bin/bash
# Add SSH key to authorized_keys

# The SSH public key to add
SSH_KEY="ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAACAQDnADyvywBOjv1ZFJkVqibCSTLVxo+cQAcU/WXmWGOA8N2L5MOJiDBd/TJoqtZYBXapoSLPCVsps8EMf086lL1K05Xg6F9hFV8GbAwMrDMGTaPbR5KdNnK9OXafGGUi4dZViK5y1SgpFam+YXdqSNc5opv4ZBFt0HIXnbug+/LXHSYY1FMPfwErP4KIzuw5Jloh9AW9MUDuhGnE6AACvr4YhmcZ9ZsTYm1H7k0J0+HqZQ4Vv1bFrsR6o5lP8By4nBTqv/Yuj0qE2X4lF9Ga5mxVwX1zNVlUqHD5aLpHVLM28BEv9SGl5IFT5C5/rEwQaZDB4TRaTSJmUyDnp1g+Khe49zVR5S0xLrMnTer4hoKNP98BY2zeGTgSNRQonr4aU3pj4XODfuJ5F1trY/l8a69go2spsM7ngn6cHBRMm19mYrzk7XrHEGIqZ1IOH6/ZJBLBBE/W5cq5KcHzFJWB6Z3x3LVnQTMcZ6HgyQaZSAMb+aMVuCGsTdxUKd/japuN24E55W52GIIntJZrRFsQNp5brqf6zvrvowysQjsUTkPwhjmjlBCB6a9O59svZpCiIthSPUjaLUwL9B3foKFyW59e0YzuUoQQr8HJAdSIC9TdwDwEcEbVWBebRm+QdooxtmN8mqrZgn6qKc2fJm16JmxKjgTOPNsJtEPwWxU3jDq3ew== TEE Access Key"

# Add SSH key to root user first (for diagnosis)
echo "Adding SSH key for root user..."
mkdir -p /root/.ssh
echo "$SSH_KEY" >> /root/.ssh/authorized_keys
chmod 700 /root/.ssh
chmod 600 /root/.ssh/authorized_keys

# Try for Ubuntu user (most likely on these instances)
if id ubuntu >/dev/null 2>&1; then
  echo "Adding SSH key for ubuntu user..."
  mkdir -p /home/ubuntu/.ssh
  echo "$SSH_KEY" >> /home/ubuntu/.ssh/authorized_keys
  chown -R ubuntu:ubuntu /home/ubuntu/.ssh
  chmod 700 /home/ubuntu/.ssh
  chmod 600 /home/ubuntu/.ssh/authorized_keys
fi

# Try for ec2-user (Amazon Linux)
if id ec2-user >/dev/null 2>&1; then
  echo "Adding SSH key for ec2-user..."
  mkdir -p /home/ec2-user/.ssh
  echo "$SSH_KEY" >> /home/ec2-user/.ssh/authorized_keys
  chown -R ec2-user:ec2-user /home/ec2-user/.ssh
  chmod 700 /home/ec2-user/.ssh
  chmod 600 /home/ec2-user/.ssh/authorized_keys
fi

# Enable password authentication temporarily (for diagnosis)
if [ -f /etc/ssh/sshd_config ]; then
  echo "Enabling password authentication temporarily..."
  sed -i 's/PasswordAuthentication no/PasswordAuthentication yes/g' /etc/ssh/sshd_config
  systemctl restart sshd
fi

# List all users that could be used for SSH login
echo "Available system users:"
cat /etc/passwd | grep -E "/bin/bash|/bin/sh" | cut -d: -f1

# Debug SSH configuration
echo "SSH server configuration:"
cat /etc/ssh/sshd_config

# Check SSH service status
echo "SSH service status:"
systemctl status sshd || service ssh status
EOF

    # Convert to base64
    echo -e "${YELLOW}Encoding user data as base64...${NC}"
    USER_DATA=$(cat /tmp/tee_add_key.sh | base64)
    
    # Update the instance user data
    echo -e "${YELLOW}Updating instance user data...${NC}"
    aws ec2 modify-instance-attribute --instance-id $instance_id --attribute userData --value "$USER_DATA"
    
    # Start the instance
    echo -e "${YELLOW}Starting the instance...${NC}"
    aws ec2 start-instances --instance-ids $instance_id
    
    # Wait for the instance to start
    echo -e "${YELLOW}Waiting for instance to start...${NC}"
    aws ec2 wait instance-running --instance-ids $instance_id
    
    # Wait a bit more for SSH to be available
    echo -e "${YELLOW}Waiting for SSH service to start (60 seconds)...${NC}"
    sleep 60
    
    # Update the IP address
    local new_ip=$(aws ec2 describe-instances --instance-ids $instance_id --query "Reservations[].Instances[].PublicIpAddress" --output text)
    INSTANCE_IPS[$instance_id]=$new_ip
    echo -e "${GREEN}Instance restarted with IP: $new_ip${NC}"
    
    # Test SSH again
    test_instance $instance_id
}

# Main menu
main_menu() {
    while true; do
        echo
        echo -e "${BLUE}=== TEE SSH Connection Fixer ===${NC}"
        echo "1. Check/Fix SSH Key Permissions"
        echo "2. List All Instances and Status"
        echo "3. Test SSH to All Instances"
        echo "4. Test SSH to SGX Node 1 (i-0600a681a918ebcb7)"
        echo "5. Test SSH to SGX Node 2 (i-066491a2fc0856d81)"
        echo "6. Test SSH to SEV Node 1 (i-020abb3c8b81087a5)"
        echo "7. Test SSH to SEV Node 2 (i-081ae726b8eebc482)"
        echo "8. Fix SSH on SGX Node 1 (restart required)"
        echo "9. Fix SSH on SGX Node 2 (restart required)"
        echo "10. Fix SSH on SEV Node 1 (restart required)"
        echo "11. Fix SSH on SEV Node 2 (restart required)"
        echo "12. Exit"
        echo
        read -p "Enter your choice: " choice
        
        case $choice in
            1) fix_key_permissions ;;
            2) 
                for instance_id in "${!INSTANCE_IPS[@]}"; do
                    check_instance_status $instance_id
                done
                ;;
            3)
                for instance_id in "${!INSTANCE_IPS[@]}"; do
                    test_instance $instance_id
                done
                ;;
            4) test_instance "i-0600a681a918ebcb7" ;;
            5) test_instance "i-066491a2fc0856d81" ;;
            6) test_instance "i-020abb3c8b81087a5" ;;
            7) test_instance "i-081ae726b8eebc482" ;;
            8) fix_ssh_with_restart "i-0600a681a918ebcb7" ;;
            9) fix_ssh_with_restart "i-066491a2fc0856d81" ;;
            10) fix_ssh_with_restart "i-020abb3c8b81087a5" ;;
            11) fix_ssh_with_restart "i-081ae726b8eebc482" ;;
            12) echo "Exiting."; exit 0 ;;
            *) echo -e "${RED}Invalid choice. Please try again.${NC}" ;;
        esac
    done
}

# Start the script
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}     TEE SSH Connection Fixer Tool     ${NC}"
echo -e "${BLUE}========================================${NC}"
echo -e "${YELLOW}This tool will help diagnose and fix SSH connection issues${NC}"
echo -e "${YELLOW}to your TEE instances in AWS.${NC}"
echo

main_menu
