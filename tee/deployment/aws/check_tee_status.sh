#!/bin/bash
# Simple TEE status check script
# Focuses on service status and connectivity

set -e

# Configuration
KEY_NAME="nasdaq-tee-key"
SSH_OPTS="-o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Hard-code node IPs based on previous deployment
SGX_NODES=("54.172.109.130" "54.224.222.120")
SEV_NODES=("54.236.21.15" "3.91.253.73")
PAIR_IDS=("2" "1")

echo -e "${BLUE}=== Checking TEE Controller Status ===${NC}"

for i in {0..1}; do
  SGX_IP=${SGX_NODES[$i]}
  SEV_IP=${SEV_NODES[$i]}
  PAIR_ID=${PAIR_IDS[$i]}
  
  echo -e "${YELLOW}Pair $PAIR_ID:${NC}"
  
  # Check SGX node
  echo -n "  SGX Controller ($SGX_IP): "
  if ssh $SSH_OPTS ubuntu@$SGX_IP "sudo systemctl status controller" > /dev/null 2>&1; then
    echo -e "${GREEN}Running${NC}"
    ssh $SSH_OPTS ubuntu@$SGX_IP "sudo systemctl status controller | grep Active"
  else
    echo -e "${RED}Not Running${NC}"
    ssh $SSH_OPTS ubuntu@$SGX_IP "sudo systemctl status controller || true"
  fi
  
  # Check SEV node
  echo -n "  SEV Controller ($SEV_IP): "
  if ssh $SSH_OPTS ubuntu@$SEV_IP "sudo systemctl status controller" > /dev/null 2>&1; then
    echo -e "${GREEN}Running${NC}"
    ssh $SSH_OPTS ubuntu@$SEV_IP "sudo systemctl status controller | grep Active"
  else
    echo -e "${RED}Not Running${NC}"
    ssh $SSH_OPTS ubuntu@$SEV_IP "sudo systemctl status controller || true"
  fi
  
  # Check port connectivity
  echo -e "\n  Testing connectivity:"
  echo -n "    SGX -> SEV: "
  if ssh $SSH_OPTS ubuntu@$SGX_IP "nc -z -w 5 $SEV_IP 7070" > /dev/null 2>&1; then
    echo -e "${GREEN}Connected${NC}"
  else
    echo -e "${RED}Failed${NC}"
  fi
  
  echo -n "    SEV -> SGX: "
  if ssh $SSH_OPTS ubuntu@$SEV_IP "nc -z -w 5 $SGX_IP 7070" > /dev/null 2>&1; then
    echo -e "${GREEN}Connected${NC}"
  else
    echo -e "${RED}Failed${NC}"
  fi
  
  echo ""
done

echo -e "${BLUE}=== Checking TEE Parameter Validation Config ===${NC}"
for i in {0..1}; do
  SGX_IP=${SGX_NODES[$i]}
  SEV_IP=${SEV_NODES[$i]}
  PAIR_ID=${PAIR_IDS[$i]}
  
  echo -e "${YELLOW}Pair $PAIR_ID:${NC}"
  
  # Check SGX config
  echo -e "  SGX Controller Config:"
  ssh $SSH_OPTS ubuntu@$SGX_IP "cat /opt/rhombus/execution/config/controller_config.json | grep -A5 parameter_validation"
  
  # Check SEV config
  echo -e "  SEV Controller Config:"
  ssh $SSH_OPTS ubuntu@$SEV_IP "cat /opt/rhombus/execution/config/controller_config.json | grep -A5 parameter_validation"
  
  echo ""
done

echo -e "${BLUE}=== Checking Controller Logs ===${NC}"
for i in {0..1}; do
  SGX_IP=${SGX_NODES[$i]}
  SEV_IP=${SEV_NODES[$i]}
  PAIR_ID=${PAIR_IDS[$i]}
  
  echo -e "${YELLOW}Pair $PAIR_ID:${NC}"
  
  # Check SGX logs
  echo -e "  SGX Controller Logs (last 10 lines):"
  ssh $SSH_OPTS ubuntu@$SGX_IP "sudo journalctl -u controller -n 10 --no-pager" || echo "Could not get logs"
  
  # Check SEV logs
  echo -e "  SEV Controller Logs (last 10 lines):"
  ssh $SSH_OPTS ubuntu@$SEV_IP "sudo journalctl -u controller -n 10 --no-pager" || echo "Could not get logs"
  
  echo ""
done

echo -e "${BLUE}=== TEE Status Check Complete ===${NC}"
