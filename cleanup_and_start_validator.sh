#!/bin/bash
# Script to clean up existing validators and start a new one with dual-format support

# Stop any existing validators
sudo pkill -f high_perf_validator || true
sudo pkill -f "python.*validator" || true
sudo pkill -f rsa_accumulator || true

# Check if port is in use and kill the process if needed
PORT_PROCESS=$(sudo lsof -t -i:7090)
if [ ! -z "$PORT_PROCESS" ]; then
  echo "Killing process using port 7090: $PORT_PROCESS"
  sudo kill -9 $PORT_PROCESS
else
  echo "No process using port 7090"
fi

# Determine which validator to start based on node type
if [ "$1" == "SGX" ]; then
  echo "Starting Go validator for SGX node with dual-format support..."
  cd /home/ubuntu
  nohup ./high_perf_validator.sh 7090 > validator.log 2>&1 &
  echo "Go validator started with PID: $!"
elif [ "$1" == "SEV" ]; then
  echo "Starting Rust validator for SEV node with dual-format support..."
  cd /home/ubuntu
  nohup ./rsa_accumulator.sh 7090 > validator.log 2>&1 &
  echo "Rust validator started with PID: $!"
else
  echo "Unknown node type: $1"
  exit 1
fi

# Verify the validator is running
sleep 2
if curl -s http://localhost:7090/metrics > /dev/null; then
  echo "Validator successfully started and responding on port 7090"
else
  echo "Warning: Validator not responding on port 7090"
  echo "Check validator.log for details"
fi
