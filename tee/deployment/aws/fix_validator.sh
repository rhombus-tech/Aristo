#!/bin/bash
# Script to fix and restart the validator service on both nodes

# SSH key path
SSH_KEY="~/nasdaq-tee-key.pem"

# Node information
SGX_IP="54.172.109.130"
SEV_IP="54.236.21.15"

echo "Restarting validator on SGX node..."
ssh -i $SSH_KEY ubuntu@$SGX_IP "cd ~/tee_validator && pkill -f rsa_enhanced_validator.py || true"
ssh -i $SSH_KEY ubuntu@$SGX_IP "cd ~/tee_validator && sed -i 's/\"listen_address\": \"0.0.0.0:7070\"/\"listen_address\": \"0.0.0.0:7090\"/' node_*.json"
ssh -i $SSH_KEY ubuntu@$SGX_IP "cd ~/tee_validator && nohup python3 rsa_enhanced_validator.py --config node_*.json > validator.log 2>&1 &"
ssh -i $SSH_KEY ubuntu@$SGX_IP "cd ~/tee_validator && echo \$! > validator.pid"
ssh -i $SSH_KEY ubuntu@$SGX_IP "ps aux | grep rsa_enhanced_validator.py"

echo "Restarting validator on SEV node..."
ssh -i $SSH_KEY ubuntu@$SEV_IP "cd ~/tee_validator && pkill -f rsa_enhanced_validator.py || true"
ssh -i $SSH_KEY ubuntu@$SEV_IP "cd ~/tee_validator && sed -i 's/\"listen_address\": \"0.0.0.0:7070\"/\"listen_address\": \"0.0.0.0:7090\"/' node_*.json"
ssh -i $SSH_KEY ubuntu@$SEV_IP "cd ~/tee_validator && nohup python3 rsa_enhanced_validator.py --config node_*.json > validator.log 2>&1 &"
ssh -i $SSH_KEY ubuntu@$SEV_IP "cd ~/tee_validator && echo \$! > validator.pid"
ssh -i $SSH_KEY ubuntu@$SEV_IP "ps aux | grep rsa_enhanced_validator.py"

echo "Waiting for services to start..."
sleep 3

echo "Checking if validator service is running on SGX node..."
ssh -i $SSH_KEY ubuntu@$SGX_IP "netstat -tulpn | grep 7090 || echo 'Service not binding to port 7090'"

echo "Checking if validator service is running on SEV node..."
ssh -i $SSH_KEY ubuntu@$SEV_IP "netstat -tulpn | grep 7090 || echo 'Service not binding to port 7090'"

echo "Validator services restarted on port 7090"
