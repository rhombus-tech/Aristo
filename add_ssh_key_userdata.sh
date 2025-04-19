#!/bin/bash
# Add our SSH key to authorized_keys and check/start the TEE service

# The SSH public key to add
SSH_KEY="ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAACAQDnADyvywBOjv1ZFJkVqibCSTLVxo+cQAcU/WXmWGOA8N2L5MOJiDBd/TJoqtZYBXapoSLPCVsps8EMf086lL1K05Xg6F9hFV8GbAwMrDMGTaPbR5KdNnK9OXafGGUi4dZViK5y1SgpFam+YXdqSNc5opv4ZBFt0HIXnbug+/LXHSYY1FMPfwErP4KIzuw5Jloh9AW9MUDuhGnE6AACvr4YhmcZ9ZsTYm1H7k0J0+HqZQ4Vv1bFrsR6o5lP8By4nBTqv/Yuj0qE2X4lF9Ga5mxVwX1zNVlUqHD5aLpHVLM28BEv9SGl5IFT5C5/rEwQaZDB4TRaTSJmUyDnp1g+Khe49zVR5S0xLrMnTer4hoKNP98BY2zeGTgSNRQonr4aU3pj4XODfuJ5F1trY/l8a69go2spsM7ngn6cHBRMm19mYrzk7XrHEGIqZ1IOH6/ZJBLBBE/W5cq5KcHzFJWB6Z3x3LVnQTMcZ6HgyQaZSAMb+aMVuCGsTdxUKd/japuN24E55W52GIIntJZrRFsQNp5brqf6zvrvowysQjsUTkPwhjmjlBCB6a9O59svZpCiIthSPUjaLUwL9B3foKFyW59e0YzuUoQQr8HJAdSIC9TdwDwEcEbVWBebRm+QdooxtmN8mqrZgn6qKc2fJm16JmxKjgTOPNsJtEPwWxU3jDq3ew== TEE Access Key"

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

# Find and restart any TEE-related services
echo "Looking for TEE-related services..."
for service in $(systemctl list-units --type=service | grep -i "tee\|enarx\|sgx\|sev" | awk '{print $1}'); do
  echo "Found TEE service: $service"
  systemctl restart $service
  systemctl status $service
done

# Start the gRPC service on port 7070 if it's not running
if ! ss -tuln | grep -q ':7070 '; then
  echo "No service found on port 7070, checking common TEE service locations..."
  
  # Check common locations for the TEE service
  if [ -f /opt/rhombus/tee/tee-service ]; then
    echo "Found TEE service at /opt/rhombus/tee/tee-service"
    nohup /opt/rhombus/tee/tee-service --port 7070 > /var/log/tee-service.log 2>&1 &
  elif [ -f /usr/local/bin/tee-service ]; then
    echo "Found TEE service at /usr/local/bin/tee-service"
    nohup /usr/local/bin/tee-service --port 7070 > /var/log/tee-service.log 2>&1 &
  elif [ -f /opt/rhombus/mesh/mesh-service ]; then
    echo "Found mesh service at /opt/rhombus/mesh/mesh-service"
    nohup /opt/rhombus/mesh/mesh-service > /var/log/mesh-service.log 2>&1 &
  fi
  
  # Check if any service is now running on port 7070
  sleep 5
  if ss -tuln | grep -q ':7070 '; then
    echo "TEE service started successfully!"
  else
    echo "Could not start TEE service automatically."
    echo "You may need to start it manually after connecting via SSH."
  fi
fi

echo "SSH key added and services checked. You should now be able to connect via SSH."
