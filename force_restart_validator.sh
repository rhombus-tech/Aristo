#!/bin/bash
# Script to forcefully stop any running validators and restart with dual-format support

# The port to use for the validator
PORT=7090

# Function to forcefully clean the port and processes
function cleanup_node() {
  NODE_IP=$1
  NODE_TYPE=$2
  
  echo "Cleaning up $NODE_TYPE node at $NODE_IP..."
  
  # SSH with sudo to forcefully kill any processes using the port
  ssh -i ~/nasdaq-tee-key.pem ubuntu@$NODE_IP "
    echo 'Stopping existing validators...'
    sudo pkill -f high_perf_validator || true
    sudo pkill -f rsa_accumulator || true
    sudo pkill -f 'python.*validator' || true
    
    echo 'Forcefully freeing port $PORT...'
    sudo fuser -k $PORT/tcp || true
    
    # Double-check if port is still in use
    PORT_PROCESS=\$(sudo lsof -t -i:$PORT || true)
    if [ ! -z \"\$PORT_PROCESS\" ]; then
      echo 'Killing processes using port $PORT: \$PORT_PROCESS'
      sudo kill -9 \$PORT_PROCESS || true
    fi
    
    # Wait to ensure port is free
    sleep 2
    
    echo 'Checking if port is free...'
    if sudo lsof -i:$PORT; then
      echo 'ERROR: Port $PORT is still in use. Manual intervention required.'
      exit 1
    else
      echo 'Port $PORT is now free.'
    fi
  "
  
  if [ $? -ne 0 ]; then
    echo "Failed to clean up $NODE_TYPE node at $NODE_IP"
    return 1
  fi
  
  return 0
}

# Function to start the validator based on node type
function start_validator() {
  NODE_IP=$1
  NODE_TYPE=$2
  
  echo "Starting $NODE_TYPE validator on $NODE_IP..."
  
  if [ "$NODE_TYPE" == "SGX" ]; then
    # Start Go validator for SGX
    ssh -i ~/nasdaq-tee-key.pem ubuntu@$NODE_IP "
      cd ~/high_perf_validator
      nohup sudo ./high_perf_validator.sh $PORT > validator.log 2>&1 &
      echo \$! > validator.pid
      sleep 3
      if curl -s http://localhost:$PORT/metrics > /dev/null; then
        echo 'SGX validator started successfully'
      else
        echo 'Warning: Validator not responding. Check validator.log'
        tail -20 validator.log
      fi
    "
  elif [ "$NODE_TYPE" == "SEV" ]; then
    # Start Rust validator for SEV
    ssh -i ~/nasdaq-tee-key.pem ubuntu@$NODE_IP "
      cd ~/high_perf_validator
      nohup sudo ./rsa_accumulator.sh $PORT > validator.log 2>&1 &
      echo \$! > validator.pid
      sleep 3
      if curl -s http://localhost:$PORT/metrics > /dev/null; then
        echo 'SEV validator started successfully'
      else
        echo 'Warning: Validator not responding. Check validator.log'
        tail -20 validator.log
      fi
    "
  fi
  
  if [ $? -ne 0 ]; then
    echo "Failed to start $NODE_TYPE validator on $NODE_IP"
    return 1
  fi
  
  return 0
}

# Clean up and start validators on both nodes
echo "=== Forcefully Restarting Validators with Dual-Format Support ==="

# SGX Node
SGX_IP="54.172.109.130"
cleanup_node "$SGX_IP" "SGX" && start_validator "$SGX_IP" "SGX"

# SEV Node
SEV_IP="54.236.21.15"
cleanup_node "$SEV_IP" "SEV" && start_validator "$SEV_IP" "SEV"

echo "=== Validation Complete ==="
echo "Running test script to verify dual-format parameter validation..."

# Run test script
cd /Users/talzisckind/Downloads/aristo-fresh\ 2
python3 tee/integration/nasdaq/connector/test_dual_format.py --sgx-ip $SGX_IP --sev-ip $SEV_IP
