#!/bin/bash
# Configure TEE mesh network for a single region with 2 TEE pairs
set -e

if [ ! -f "tee_deployment_info.json" ]; then
    echo "Error: tee_deployment_info.json not found. Please run deploy_single_region.sh first."
    exit 1
fi

# Load deployment information
REGION_ID=$(jq -r '.region_id' tee_deployment_info.json)
COORDINATOR_IP=$(jq -r '.coordinator' tee_deployment_info.json)
SGX_IP_1=$(jq -r '.tee_pairs[0].sgx' tee_deployment_info.json)
SEV_IP_1=$(jq -r '.tee_pairs[0].sev' tee_deployment_info.json)
SGX_IP_2=$(jq -r '.tee_pairs[1].sgx' tee_deployment_info.json)
SEV_IP_2=$(jq -r '.tee_pairs[1].sev' tee_deployment_info.json)

TIMEOUT=10
KEY_PATH="/Users/talzisckind/.ssh/tee_access_key"

echo "Configuring TEE mesh network for region ${REGION_ID}"

# Wait a bit for SSH to be available
echo "Waiting for SSH to be available on all instances..."
sleep 30

# Create a temporary directory for all necessary binaries
mkdir -p ./tmp_deployment

# Copy binaries to the temporary directory
cp ./devnet/sgx1/tee-controller ./tmp_deployment/
cp ./execution/target/debug/coordinator_mock ./tmp_deployment/coordinator-service
cp ./morpheus_cli_bin ./tmp_deployment/morpheus-cli

# Create a simplified mesh config file
cat > ./tmp_deployment/mesh-config.json << EOF
{
    "regionId": "${REGION_ID}",
    "parameterValidation": {
        "enableLengthPrefixChecks": true,
        "enableDirectFormatChecks": true
    },
    "attestation": {
        "enableCrossAttestation": true
    }
}
EOF

# Upload binaries and configs to all nodes
for IP in $COORDINATOR_IP $SGX_IP_1 $SGX_IP_2 $SEV_IP_1 $SEV_IP_2; do
    echo "Uploading binaries and configs to ${IP}..."
    scp -i $KEY_PATH -o StrictHostKeyChecking=no -o ConnectTimeout=$TIMEOUT \
        ./tmp_deployment/* ubuntu@${IP}:/home/ubuntu/
done

# Configure coordinator
echo "Configuring coordinator at ${COORDINATOR_IP}..."
ssh -i $KEY_PATH -o StrictHostKeyChecking=no -o ConnectTimeout=$TIMEOUT ubuntu@${COORDINATOR_IP} << EOF
    chmod +x ./coordinator-service
    cat > coordinator-config.json << EOC
{
    "regionId": "${REGION_ID}",
    "discoveryPort": 7071,
    "servicePort": 7070,
    "teePairs": [
        {
            "sgx": "${SGX_IP_1}:7080",
            "sev": "${SEV_IP_1}:7080"
        },
        {
            "sgx": "${SGX_IP_2}:7080",
            "sev": "${SEV_IP_2}:7080"
        }
    ],
    "circuitBreakerThreshold": 5,
    "peerRefreshInterval": 60
}
EOC
    nohup ./coordinator-service --config coordinator-config.json > coordinator.log 2>&1 &
    echo "Coordinator service started"
EOF

# Configure TEE nodes - create pairs with enhanced parameter validation
configure_tee_node() {
    local IP=$1
    local TYPE=$2
    local PAIR_NUM=$3
    
    echo "Configuring ${TYPE} node ${PAIR_NUM} at ${IP}..."
    ssh -i $KEY_PATH -o StrictHostKeyChecking=no -o ConnectTimeout=$TIMEOUT ubuntu@${IP} << EOF
        chmod +x ./tee-controller ./morpheus-cli
        cat > tee-config.json << EOC
{
    "regionId": "${REGION_ID}",
    "teeType": "${TYPE}",
    "pairNumber": ${PAIR_NUM},
    "coordinatorEndpoint": "${COORDINATOR_IP}:7070",
    "discoveryEndpoint": "${COORDINATOR_IP}:7071",
    "meshEnabled": true,
    "parameterValidation": {
        "maxSizeBytes": 1024,
        "enableLengthPrefixChecks": true,
        "enableDirectFormatChecks": true,
        "rejectUnreasonableLength": true
    },
    "circuitBreakerThreshold": 5,
    "peerRefreshInterval": 30,
    "attestation": {
        "enableCrossAttestation": true,
        "useAccumulator": true,
        "accumulatorEndpoint": "${COORDINATOR_IP}:7072"
    }
}
EOC
    # Use coordinator_mock for execution instead of the WebAssembly module
    nohup ./morpheus-cli compute start ${REGION_ID} 7080 \
        --controller ./tee-controller \
        --config tee-config.json > tee-${TYPE}-${PAIR_NUM}.log 2>&1 &
    echo "${TYPE} node ${PAIR_NUM} started"
EOF
}

# Configure TEE nodes
configure_tee_node $SGX_IP_1 "SGX" 1
configure_tee_node $SEV_IP_1 "SEV" 1
configure_tee_node $SGX_IP_2 "SGX" 2
configure_tee_node $SEV_IP_2 "SEV" 2

echo "Waiting for mesh network to initialize..."
sleep 20

# Run validation script on coordinator
echo "Running validation test with integration script..."
ssh -i $KEY_PATH -o StrictHostKeyChecking=no -o ConnectTimeout=$TIMEOUT ubuntu@${COORDINATOR_IP} << EOF
    chmod +x ./morpheus-cli
    ./morpheus-cli validate --batch-size 100 --thread-count 8 --region ${REGION_ID}
EOF

echo "TEE mesh network configuration completed successfully!"
echo
echo "To monitor the system:"
echo "  - Check coordinator logs: ssh -i $KEY_PATH ubuntu@${COORDINATOR_IP} 'tail -f coordinator.log'"
echo "  - Check TEE logs: ssh -i $KEY_PATH ubuntu@${SGX_IP_1} 'tail -f tee-SGX-1.log'"
echo
echo "To run performance tests:"
echo "  - ssh -i $KEY_PATH ubuntu@${COORDINATOR_IP}"
echo "  - ./morpheus-cli benchmark --pairs 2 --operations 1000 --batch-size 100"
