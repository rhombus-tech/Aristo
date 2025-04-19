#!/bin/bash

# Instance to fix
INSTANCE_ID="i-0600a681a918ebcb7"  # SGX Node 1
INSTANCE_IP="3.82.138.122"

echo "=== Simple SSH Fixer for TEE Instance ==="
echo "Working with instance $INSTANCE_ID ($INSTANCE_IP)"

# Stop the instance
echo "Stopping instance..."
aws ec2 stop-instances --instance-ids $INSTANCE_ID
echo "Waiting for instance to stop..."
aws ec2 wait instance-stopped --instance-ids $INSTANCE_ID

# Create a very simple user-data script focused just on adding the SSH key
echo "Creating simplified user data..."
cat > /tmp/simple_add_key.sh << 'EOF'
#!/bin/bash
# Simple key adder that tries all common users

# The SSH key to add
SSH_KEY="ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAACAQDnADyvywBOjv1ZFJkVqibCSTLVxo+cQAcU/WXmWGOA8N2L5MOJiDBd/TJoqtZYBXapoSLPCVsps8EMf086lL1K05Xg6F9hFV8GbAwMrDMGTaPbR5KdNnK9OXafGGUi4dZViK5y1SgpFam+YXdqSNc5opv4ZBFt0HIXnbug+/LXHSYY1FMPfwErP4KIzuw5Jloh9AW9MUDuhGnE6AACvr4YhmcZ9ZsTYm1H7k0J0+HqZQ4Vv1bFrsR6o5lP8By4nBTqv/Yuj0qE2X4lF9Ga5mxVwX1zNVlUqHD5aLpHVLM28BEv9SGl5IFT5C5/rEwQaZDB4TRaTSJmUyDnp1g+Khe49zVR5S0xLrMnTer4hoKNP98BY2zeGTgSNRQonr4aU3pj4XODfuJ5F1trY/l8a69go2spsM7ngn6cHBRMm19mYrzk7XrHEGIqZ1IOH6/ZJBLBBE/W5cq5KcHzFJWB6Z3x3LVnQTMcZ6HgyQaZSAMb+aMVuCGsTdxUKd/japuN24E55W52GIIntJZrRFsQNp5brqf6zvrvowysQjsUTkPwhjmjlBCB6a9O59svZpCiIthSPUjaLUwL9B3foKFyW59e0YzuUoQQr8HJAdSIC9TdwDwEcEbVWBebRm+QdooxtmN8mqrZgn6qKc2fJm16JmxKjgTOPNsJtEPwWxU3jDq3ew== TEE Access Key"

# Try all common Linux users for EC2 instances
for USER in ubuntu ec2-user admin centos fedora root; do
  # Try to find home directory
  if [ -d "/home/$USER" ] || [ "$USER" = "root" ]; then
    HOMEDIR=$(eval echo ~$USER)
    echo "Adding SSH key for user $USER (home: $HOMEDIR)..."
    
    # Create .ssh directory and set proper permissions
    mkdir -p $HOMEDIR/.ssh
    echo "$SSH_KEY" >> $HOMEDIR/.ssh/authorized_keys
    
    # Set permissions
    if [ "$USER" != "root" ]; then
      chown -R $USER:$USER $HOMEDIR/.ssh
    fi
    chmod 700 $HOMEDIR/.ssh
    chmod 600 $HOMEDIR/.ssh/authorized_keys
    echo "Key added for $USER"
  fi
done

# Make sure sshd is enabled and running
systemctl enable sshd || systemctl enable ssh
systemctl restart sshd || systemctl restart ssh

# Log validation
echo "Current users with home directories:"
ls -la /home/
echo "Current authorized_keys status:"
find /home -name "authorized_keys" -exec ls -la {} \;
if [ -f /root/.ssh/authorized_keys ]; then
  echo "Root keys: $(wc -l /root/.ssh/authorized_keys)"
fi
EOF

# Convert to base64
USER_DATA=$(cat /tmp/simple_add_key.sh | base64)

# Update instance user data
echo "Updating instance user data..."
aws ec2 modify-instance-attribute --instance-id $INSTANCE_ID --attribute userData --value "$USER_DATA"

# Start instance
echo "Starting instance..."
aws ec2 start-instances --instance-ids $INSTANCE_ID
echo "Waiting for instance to start..."
aws ec2 wait instance-running --instance-ids $INSTANCE_ID

# Wait for SSH to be available 
echo "Waiting for SSH service to start (2 minutes)..."
echo "Instance is booting up and applying user data..."
echo "This includes adding the SSH key to all possible users."
for i in {1..12}; do
  echo -n "."
  sleep 10
done
echo

# Try connecting to the instance with different users
echo "Trying to connect to the instance with different users..."
for USER in ubuntu ec2-user admin centos fedora root; do
  echo "Testing SSH connection with user: $USER"
  ssh -v -i ~/.ssh/tee_access_key -o StrictHostKeyChecking=no -o ConnectTimeout=5 $USER@$INSTANCE_IP echo "SSH connection test for $USER" || echo "Failed to connect with $USER"
  echo "---"
done

echo "SSH connection testing complete."
echo "If any of these tests succeeded, you now have SSH access to the TEE instance."
echo "You can try connecting directly with:"
echo "  ssh -i ~/.ssh/tee_access_key ubuntu@$INSTANCE_IP"
echo "or another username that showed a successful connection above."
