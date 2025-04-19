#!/bin/bash
# Script to add new SSH key to authorized_keys for both ubuntu and ec2-user

# The SSH public key to add
SSH_KEY="ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAACAQDnADyvywBOjv1ZFJkVqibCSTLVxo+cQAcU/WXmWGOA8N2L5MOJiDBd/TJoqtZYBXapoSLPCVsps8EMf086lL1K05Xg6F9hFV8GbAwMrDMGTaPbR5KdNnK9OXafGGUi4dZViK5y1SgpFam+YXdqSNc5opv4ZBFt0HIXnbug+/LXHSYY1FMPfwErP4KIzuw5Jloh9AW9MUDuhGnE6AACvr4YhmcZ9ZsTYm1H7k0J0+HqZQ4Vv1bFrsR6o5lP8By4nBTqv/Yuj0qE2X4lF9Ga5mxVwX1zNVlUqHD5aLpHVLM28BEv9SGl5IFT5C5/rEwQaZDB4TRaTSJmUyDnp1g+Khe49zVR5S0xLrMnTer4hoKNP98BY2zeGTgSNRQonr4aU3pj4XODfuJ5F1trY/l8a69go2spsM7ngn6cHBRMm19mYrzk7XrHEGIqZ1IOH6/ZJBLBBE/W5cq5KcHzFJWB6Z3x3LVnQTMcZ6HgyQaZSAMb+aMVuCGsTdxUKd/japuN24E55W52GIIntJZrRFsQNp5brqf6zvrvowysQjsUTkPwhjmjlBCB6a9O59svZpCiIthSPUjaLUwL9B3foKFyW59e0YzuUoQQr8HJAdSIC9TdwDwEcEbVWBebRm+QdooxtmN8mqrZgn6qKc2fJm16JmxKjgTOPNsJtEPwWxU3jDq3ew== TEE Access Key"

# Try for Ubuntu user
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

# Also try for root (just in case)
mkdir -p /root/.ssh
echo "$SSH_KEY" >> /root/.ssh/authorized_keys
chmod 700 /root/.ssh
chmod 600 /root/.ssh/authorized_keys

# Start the TEE service if not running (we'll add this logic once we know what service to look for)
echo "Checking for TEE service status..."
service_name="tee-service"
if systemctl is-active --quiet $service_name; then
  echo "TEE service is already running."
else
  echo "Attempting to start TEE service..."
  systemctl start $service_name || echo "Failed to start TEE service - may not be installed correctly"
fi

# Check if port 7070 is listening
netstat -tulpn | grep 7070 || echo "Warning: No service listening on port 7070"

echo "SSH key installation complete."
