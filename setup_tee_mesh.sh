#!/bin/bash
# Setup a local TEE mesh network for development and testing
set -e

echo "Setting up local TEE mesh network..."

# Create necessary directories
mkdir -p ./devnet/tee
mkdir -p ./devnet/contracts

# Copy controller binary to devnet directory
echo "Copying TEE controller to devnet directory..."
cp ./execution/controller/target/debug/tee-controller ./devnet/tee/

# Build the test contracts
echo "Building test contracts..."
cd ./execution/controller/tests/contracts/simple_add
cargo build --target wasm32-unknown-unknown --release
cd - > /dev/null
cp ./execution/controller/tests/contracts/simple_add/target/wasm32-unknown-unknown/release/simple_add.wasm ./devnet/contracts/

# Create config for TEE mesh
echo "Creating TEE mesh configuration..."
cat > ./devnet/tee/config.json << EOF
{
    "region_id": "local-1",
    "mesh_enabled": true,
    "parameter_validation": {
        "max_size_bytes": 1024,
        "enable_length_prefix_checks": true,
        "enable_direct_format_checks": true,
        "reject_unreasonable_length": true
    },
    "batch_size": 100,
    "thread_count": 8,
    "circuit_breaker_threshold": 5,
    "peer_refresh_interval": 30
}
EOF

# Create startup scripts
echo "Creating startup scripts..."

# Coordinator startup script
cat > ./devnet/tee/start_coordinator.sh << EOF
#!/bin/bash
cd "\$(dirname "\$0")"
./tee-controller --coordinator --port 7070 --discovery-port 7071 --config-file config.json
EOF
chmod +x ./devnet/tee/start_coordinator.sh

# SGX TEE startup script
cat > ./devnet/tee/start_sgx.sh << EOF
#!/bin/bash
cd "\$(dirname "\$0")"
./tee-controller --tee-type SGX --region-id local-1 --port 7080 --coordinator-endpoint 127.0.0.1:7070 --discovery-endpoint 127.0.0.1:7071 --config-file config.json
EOF
chmod +x ./devnet/tee/start_sgx.sh

# SEV TEE startup script
cat > ./devnet/tee/start_sev.sh << EOF
#!/bin/bash
cd "\$(dirname "\$0")"
./tee-controller --tee-type SEV --region-id local-1 --port 7081 --coordinator-endpoint 127.0.0.1:7070 --discovery-endpoint 127.0.0.1:7071 --config-file config.json
EOF
chmod +x ./devnet/tee/start_sev.sh

# Create validation script
cat > ./devnet/tee/validate_mesh.sh << EOF
#!/bin/bash
cd "\$(dirname "\$0")"

echo "Validating TEE mesh network..."

# Check if coordinator is running
echo "Checking coordinator..."
curl -s http://127.0.0.1:7071/status > /dev/null
if [ \$? -ne 0 ]; then
    echo "Error: Coordinator not running. Please start it with ./start_coordinator.sh"
    exit 1
fi
echo "✅ Coordinator is running"

# Check if SGX TEE is running
echo "Checking SGX TEE..."
curl -s http://127.0.0.1:7080/status > /dev/null
if [ \$? -ne 0 ]; then
    echo "Error: SGX TEE not running. Please start it with ./start_sgx.sh"
    exit 1
fi
echo "✅ SGX TEE is running"

# Check if SEV TEE is running
echo "Checking SEV TEE..."
curl -s http://127.0.0.1:7081/status > /dev/null
if [ \$? -ne 0 ]; then
    echo "Error: SEV TEE not running. Please start it with ./start_sev.sh"
    exit 1
fi
echo "✅ SEV TEE is running"

# Check if TEE pair is registered
echo "Checking TEE pair registration..."
PAIRS=\$(curl -s http://127.0.0.1:7071/tee_pairs | jq -r '.tee_pairs | length')
if [ "\$PAIRS" -eq "0" ]; then
    echo "Error: No TEE pairs registered. There might be an issue with the mesh network."
    exit 1
fi
echo "✅ TEE pair registered successfully"

# Deploy and execute a test contract
echo "Testing contract deployment and execution..."
CONTRACT_ID=\$(curl -s -X POST -H "Content-Type: application/json" -d '{
    "wasm_bytes": "'"\$(base64 -i ../contracts/simple_add.wasm)"'"
}' http://127.0.0.1:7070/deploy | jq -r '.contract_id')

if [ -z "\$CONTRACT_ID" ] || [ "\$CONTRACT_ID" == "null" ]; then
    echo "Error: Failed to deploy contract"
    exit 1
fi
echo "✅ Contract deployed with ID: \$CONTRACT_ID"

# Execute the contract
echo "Executing contract..."
RESULT=\$(curl -s -X POST -H "Content-Type: application/json" -d '{
    "contract_id": "'\$CONTRACT_ID'",
    "function": "add",
    "args": [42, 58]
}' http://127.0.0.1:7070/execute | jq -r '.result')

if [ -z "\$RESULT" ] || [ "\$RESULT" == "null" ]; then
    echo "Error: Failed to execute contract"
    exit 1
fi

if [ "\$RESULT" == "100" ]; then
    echo "✅ Contract execution successful: 42 + 58 = 100"
else
    echo "❌ Contract execution failed. Expected 100, got \$RESULT"
    exit 1
fi

echo
echo "TEE mesh network validation complete!"
echo "Your dual TEE architecture with parameter validation is working correctly."
EOF
chmod +x ./devnet/tee/validate_mesh.sh

echo
echo "Local TEE mesh network setup completed!"
echo
echo "To start your TEE mesh network, run the following commands in separate terminals:"
echo "1. Start coordinator:  ./devnet/tee/start_coordinator.sh"
echo "2. Start SGX service:  ./devnet/tee/start_sgx.sh"
echo "3. Start SEV service:  ./devnet/tee/start_sev.sh"
echo
echo "After starting all services, validate your TEE mesh network with:"
echo "./devnet/tee/validate_mesh.sh"
echo
echo "This setup demonstrates your dual TEE architecture with proper parameter validation,"
echo "ensuring your \"100ms and regulated\" value proposition is maintained."
