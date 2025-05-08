#!/bin/bash
set -e

echo "Setting up local TEE mesh network development environment..."

# Build the TEE controller binary if not already done
echo "Building TEE controller binaries if needed..."
if [ ! -f ./execution/target/debug/tee-controller ]; then
  cd execution && cargo build --bin tee-controller --bin coordinator_mock
  cd ..
fi

# Create devnet directories
echo "Creating devnet directories..."
mkdir -p ./devnet
mkdir -p ./devnet/sgx1 ./devnet/sgx2
mkdir -p ./devnet/sev1 ./devnet/sev2
mkdir -p ./devnet/coordinator
mkdir -p ./devnet/configs
mkdir -p ./devnet/avalanche
mkdir -p ./devnet/contracts
mkdir -p ./devnet/metrics

# Copy TEE controller to appropriate directories
echo "Copying TEE controller binaries..."
cp ./execution/target/debug/tee-controller ./devnet/sgx1/
cp ./execution/target/debug/tee-controller ./devnet/sgx2/
cp ./execution/target/debug/tee-controller ./devnet/sev1/
cp ./execution/target/debug/tee-controller ./devnet/sev2/
cp ./execution/target/debug/coordinator_mock ./devnet/coordinator/

# Copy WebAssembly contracts for testing
echo "Copying WebAssembly test contracts..."
cp ./execution/controller/tests/contracts/simple_add/target/wasm32-unknown-unknown/release/simple_add.wasm ./devnet/contracts/ 2>/dev/null || echo "Simple add contract not found, skipping"
cp ./execution/controller/tests/contracts/market_data_consumer/target/wasm32-unknown-unknown/release/market_data_consumer.wasm ./devnet/contracts/ 2>/dev/null || echo "Market data consumer contract not found, skipping"

# Create configuration files
echo "Creating configuration files..."

# Coordinator config with parameter validation settings
cat > ./devnet/configs/coordinator.json << EOF
{
  "port": 8080,
  "host": "127.0.0.1",
  "region_id": "local-region-1",
  "log_level": "debug",
  "attestation_verification": "simulation",
  "parameter_validation": {
    "enabled": true,
    "max_parameter_size": 1048576,
    "support_length_prefix": true,
    "support_direct_format": true,
    "default_format": "length_prefix"
  },
  "performance": {
    "batch_size": 100,
    "thread_count": 8,
    "metrics_enabled": true
  }
}
EOF

# SGX1 config with enhanced mesh settings
cat > ./devnet/configs/sgx1.json << EOF
{
  "tee_type": "sgx",
  "tee_id": "sgx1",
  "port": 8081,
  "coordinator_url": "http://127.0.0.1:8080",
  "log_level": "debug",
  "region_id": "local-region-1",
  "mesh_enabled": true,
  "mesh_peers": ["http://127.0.0.1:8082", "http://127.0.0.1:8083", "http://127.0.0.1:8084"],
  "attestation_type": "simulation",
  "parameter_validation": {
    "enabled": true,
    "max_parameter_size": 1048576,
    "support_length_prefix": true,
    "support_direct_format": true
  },
  "performance": {
    "batch_size": 100,
    "thread_count": 8,
    "metrics_enabled": true
  },
  "dual_execution": {
    "enabled": true,
    "cross_attestation": true,
    "fallback_on_mismatch": true
  }
}
EOF

# SGX2 config
cat > ./devnet/configs/sgx2.json << EOF
{
  "tee_type": "sgx",
  "tee_id": "sgx2",
  "port": 8082,
  "coordinator_url": "http://127.0.0.1:8080",
  "log_level": "debug",
  "region_id": "local-region-1",
  "mesh_enabled": true,
  "mesh_peers": ["http://127.0.0.1:8081", "http://127.0.0.1:8083", "http://127.0.0.1:8084"],
  "attestation_type": "simulation",
  "parameter_validation": {
    "enabled": true,
    "max_parameter_size": 1048576,
    "support_length_prefix": true,
    "support_direct_format": true
  },
  "performance": {
    "batch_size": 100,
    "thread_count": 8,
    "metrics_enabled": true
  }
}
EOF

# SEV1 config
cat > ./devnet/configs/sev1.json << EOF
{
  "tee_type": "sev",
  "tee_id": "sev1",
  "port": 8083,
  "coordinator_url": "http://127.0.0.1:8080",
  "log_level": "debug",
  "region_id": "local-region-1",
  "mesh_enabled": true,
  "mesh_peers": ["http://127.0.0.1:8081", "http://127.0.0.1:8082", "http://127.0.0.1:8084"],
  "attestation_type": "simulation",
  "parameter_validation": {
    "enabled": true,
    "max_parameter_size": 1048576,
    "support_length_prefix": true,
    "support_direct_format": true
  },
  "performance": {
    "batch_size": 100,
    "thread_count": 8,
    "metrics_enabled": true
  }
}
EOF

# SEV2 config
cat > ./devnet/configs/sev2.json << EOF
{
  "tee_type": "sev",
  "tee_id": "sev2",
  "port": 8084,
  "coordinator_url": "http://127.0.0.1:8080",
  "log_level": "debug",
  "region_id": "local-region-1",
  "mesh_enabled": true, 
  "mesh_peers": ["http://127.0.0.1:8081", "http://127.0.0.1:8082", "http://127.0.0.1:8083"],
  "attestation_type": "simulation",
  "parameter_validation": {
    "enabled": true,
    "max_parameter_size": 1048576,
    "support_length_prefix": true,
    "support_direct_format": true
  },
  "performance": {
    "batch_size": 100,
    "thread_count": 8,
    "metrics_enabled": true
  }
}
EOF

# Avalanche devnet configuration
cat > ./devnet/configs/avalanche.json << EOF
{
  "network-id": 1337,
  "http-host": "127.0.0.1",
  "http-port": 9650,
  "public-ip": "127.0.0.1",
  "staking-port": 9651,
  "db-dir": "./db",
  "log-level": "debug",
  "chain-config-dir": "./configs/chains",
  "tee-integration": {
    "coordinator-url": "http://127.0.0.1:8080",
    "primary-tee": "sgx1",
    "secondary-tee": "sev1",
    "verification-level": "dual",
    "batch-size": 100,
    "allowed-formats": ["length_prefix", "direct"]
  }
}
EOF

# Create script to start the local devnet
cat > ./devnet/start_local_devnet.sh << EOF
#!/bin/bash
set -e

echo "Starting local TEE mesh network with Avalanche integration..."

# Start coordinator
echo "Starting coordinator..."
cd coordinator
./coordinator_mock --config ../configs/coordinator.json > coordinator.log 2>&1 &
COORDINATOR_PID=\$!
echo "Coordinator started with PID \$COORDINATOR_PID"

# Wait for coordinator to initialize
sleep 2

# Start SGX1
echo "Starting SGX1 TEE node..."
cd ../sgx1
./tee-controller --config ../configs/sgx1.json > sgx1.log 2>&1 &
SGX1_PID=\$!
echo "SGX1 started with PID \$SGX1_PID"

# Start SGX2
echo "Starting SGX2 TEE node..."
cd ../sgx2
./tee-controller --config ../configs/sgx2.json > sgx2.log 2>&1 &
SGX2_PID=\$!
echo "SGX2 started with PID \$SGX2_PID"

# Start SEV1
echo "Starting SEV1 TEE node..."
cd ../sev1
./tee-controller --config ../configs/sev1.json > sev1.log 2>&1 &
SEV1_PID=\$!
echo "SEV1 started with PID \$SEV1_PID"

# Start SEV2
echo "Starting SEV2 TEE node..."
cd ../sev2
./tee-controller --config ../configs/sev2.json > sev2.log 2>&1 &
SEV2_PID=\$!
echo "SEV2 started with PID \$SEV2_PID"

# Wait for TEE nodes to initialize
sleep 3

# Start Prometheus metrics collector
echo "Starting metrics collector..."
cd ../metrics
if [ -f prometheus.yml ]; then
  prometheusPath=\$(which prometheus 2>/dev/null)
  if [ -n "\$prometheusPath" ]; then
    prometheus --config.file=prometheus.yml > metrics.log 2>&1 &
    METRICS_PID=\$!
    echo "Metrics collector started with PID \$METRICS_PID"
  else
    echo "Prometheus not found, skipping metrics collection"
  fi
fi

echo "All TEE nodes started successfully!"
echo "Coordinator: http://127.0.0.1:8080"
echo "SGX1: http://127.0.0.1:8081"
echo "SGX2: http://127.0.0.1:8082"
echo "SEV1: http://127.0.0.1:8083"
echo "SEV2: http://127.0.0.1:8084"
echo "Metrics Dashboard (if installed): http://127.0.0.1:9090"

# Create a file with PIDs for stopping
cat > ./stop_pids.txt << EOT
\$COORDINATOR_PID
\$SGX1_PID
\$SGX2_PID
\$SEV1_PID
\$SEV2_PID
\${METRICS_PID:-}
EOT

echo "To stop the devnet, run: ./stop_local_devnet.sh"
EOF

# Create script to stop the local devnet
cat > ./devnet/stop_local_devnet.sh << EOF
#!/bin/bash

echo "Stopping local TEE mesh network..."

if [ -f ./stop_pids.txt ]; then
  while read pid; do
    if [ -n "\$pid" ] && ps -p \$pid > /dev/null 2>&1; then
      echo "Stopping process with PID \$pid"
      kill \$pid
    fi
  done < ./stop_pids.txt
  rm ./stop_pids.txt
  echo "All TEE nodes stopped successfully!"
else
  echo "No PID file found. Devnet may not be running."
fi
EOF

# Create Prometheus config for metrics collection
cat > ./devnet/metrics/prometheus.yml << EOF
global:
  scrape_interval: 10s

scrape_configs:
  - job_name: 'tee_mesh'
    static_configs:
      - targets: ['127.0.0.1:8081', '127.0.0.1:8082', '127.0.0.1:8083', '127.0.0.1:8084']
  - job_name: 'coordinator'
    static_configs:
      - targets: ['127.0.0.1:8080']
EOF

# Create comprehensive validation test script
cat > ./devnet/run_validation.sh << EOF
#!/bin/bash
set -e

echo "Running validation tests on local TEE mesh network..."

# Parameter validation test with length-prefixed format
echo "Testing parameter validation with length-prefixed format..."
curl -X POST -H "Content-Type: application/json" -d '{
  "payload": "0x00000004AABBCCDD",
  "tee_type": "sgx",
  "target_id": "sgx1",
  "validation_level": "strict",
  "parameter_format": "length_prefix"
}' http://127.0.0.1:8080/execute

echo

# Parameter validation test with direct format
echo "Testing parameter validation with direct format..."
curl -X POST -H "Content-Type: application/json" -d '{
  "payload": "0xAABBCCDD",
  "tee_type": "sgx",
  "target_id": "sgx1",
  "validation_level": "strict",
  "parameter_format": "direct"
}' http://127.0.0.1:8080/execute

echo

# Cross-attestation test between SGX and SEV
echo "Testing cross-attestation between SGX and SEV..."
curl -X POST -H "Content-Type: application/json" -d '{
  "payload": "0x00000004AABBCCDD",
  "tee_type": "dual",
  "primary_id": "sgx1",
  "secondary_id": "sev1",
  "validation_level": "strict",
  "parameter_format": "length_prefix"
}' http://127.0.0.1:8080/execute_dual

echo

# Mesh network direct communication test
echo "Testing mesh network direct communication..."
curl -X POST -H "Content-Type: application/json" -d '{
  "target_tee": "sgx2",
  "operation": "discover_peers",
  "region_id": "local-region-1"
}' http://127.0.0.1:8081/mesh/execute

echo

# Performance benchmark test with batch size 100 and 8 threads
echo "Running performance benchmark with optimized parameters (batch size: 100, threads: 8)..."
curl -X POST -H "Content-Type: application/json" -d '{
  "operation": "benchmark",
  "batch_size": 100,
  "thread_count": 8,
  "duration_seconds": 5,
  "parameter_format": "length_prefix"
}' http://127.0.0.1:8080/benchmark

echo

# WebAssembly contract test
if [ -f ../contracts/simple_add.wasm ]; then
  echo "Testing WebAssembly contract execution..."
  # First deploy the contract
  CONTRACT_ID=\$(curl -s -X POST -H "Content-Type: application/json" -d '{
    "operation": "deploy_contract",
    "contract_path": "../contracts/simple_add.wasm",
    "tee_type": "dual",
    "primary_id": "sgx1",
    "secondary_id": "sev1"
  }' http://127.0.0.1:8080/contract | jq -r '.contract_id')
  
  # Then execute the contract
  if [ -n "\$CONTRACT_ID" ]; then
    echo "Contract deployed with ID: \$CONTRACT_ID"
    echo "Executing contract..."
    curl -X POST -H "Content-Type: application/json" -d "{
      \"operation\": \"execute_contract\",
      \"contract_id\": \"\$CONTRACT_ID\",
      \"function\": \"add\",
      \"parameters\": \"0x0000000800000019000000272A\",
      \"tee_type\": \"dual\",
      \"primary_id\": \"sgx1\",
      \"secondary_id\": \"sev1\"
    }" http://127.0.0.1:8080/contract
    echo
  else
    echo "Failed to deploy contract"
  fi
fi

echo

echo "Validation tests completed!"
EOF

# Make the scripts executable
chmod +x ./devnet/start_local_devnet.sh
chmod +x ./devnet/stop_local_devnet.sh
chmod +x ./devnet/run_validation.sh

echo "Local TEE mesh development environment setup complete!"
echo "To start the local devnet: cd devnet && ./start_local_devnet.sh"
echo "To stop the local devnet: cd devnet && ./stop_local_devnet.sh"
echo "To run validation tests: cd devnet && ./run_validation.sh"
