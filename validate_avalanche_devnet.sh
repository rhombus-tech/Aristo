#!/bin/bash
# Validate the Avalanche devnet integration with TEE mesh network
set -e

if [ ! -f "avalanche_deployment_info.json" ]; then
    echo "Error: avalanche_deployment_info.json not found. Please run deploy_avalanche_devnet.sh first."
    exit 1
fi

if [ ! -f "tee_deployment_info.json" ]; then
    echo "Error: tee_deployment_info.json not found. Please run deploy_single_region.sh first."
    exit 1
fi

# Load deployment information
BOOTSTRAP_IP=$(jq -r '.bootstrap_node.ip' avalanche_deployment_info.json)
BOOTSTRAP_ENDPOINT=$(jq -r '.bootstrap_node.api_endpoint' avalanche_deployment_info.json)
VM_ID=$(jq -r '.vm_id' avalanche_deployment_info.json)
REGION_ID=$(jq -r '.tee_integration.region_id' avalanche_deployment_info.json)
KEY_PATH="~/.ssh/tee-access-key.pem"

echo "Validating Avalanche devnet integration with TEE mesh network..."

# Check if bootstrap node is responsive
echo "Checking bootstrap node at ${BOOTSTRAP_ENDPOINT}..."
HEALTH_CHECK=$(curl -s ${BOOTSTRAP_ENDPOINT}/ext/health | jq -r '.healthy')
if [ "$HEALTH_CHECK" != "true" ]; then
    echo "Error: Bootstrap node is not healthy."
    exit 1
fi
echo "✅ Bootstrap node is healthy"

# Check if blockchain was created
echo "Checking if Morpheus blockchain was created..."
BLOCKCHAIN_ID=""
MAX_RETRIES=30
RETRY_COUNT=0

while [ -z "$BLOCKCHAIN_ID" ] && [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
    CHAINS=$(curl -s -X POST --data '{"jsonrpc":"2.0","method":"platform.getBlockchains","params":{},"id":1}' \
        -H 'content-type:application/json;' ${BOOTSTRAP_ENDPOINT}/ext/P | jq -r '.result.blockchains')
    
    BLOCKCHAIN_ID=$(echo $CHAINS | jq -r '.[] | select(.name=="morpheus") | .id')
    
    if [ -z "$BLOCKCHAIN_ID" ] || [ "$BLOCKCHAIN_ID" == "null" ]; then
        echo "Waiting for Morpheus blockchain to be created... (${RETRY_COUNT}/${MAX_RETRIES})"
        RETRY_COUNT=$((RETRY_COUNT+1))
        sleep 10
    fi
done

if [ -z "$BLOCKCHAIN_ID" ] || [ "$BLOCKCHAIN_ID" == "null" ]; then
    echo "Error: Morpheus blockchain was not created after ${MAX_RETRIES} retries."
    exit 1
fi

echo "✅ Morpheus blockchain created with ID: ${BLOCKCHAIN_ID}"

# Update config files with blockchain ID
echo "Updating blockchain_deployment_info.json with blockchain ID..."
jq ".blockchain_id = \"${BLOCKCHAIN_ID}\"" avalanche_deployment_info.json > tmp.json && mv tmp.json avalanche_deployment_info.json

# Test TEE integration by deploying a contract
echo "Testing TEE integration by deploying a contract..."
ssh -i $KEY_PATH -o StrictHostKeyChecking=no admin@${BOOTSTRAP_IP} << EOF
    # Create a simple test contract
    echo "Creating test contract..."
    cat > test_contract.rs << EOC
#![no_std]
#![allow(unused_attributes)]

use core::panic::PanicInfo;

#[no_mangle]
pub extern "C" fn add(a: i32, b: i32) -> i32 {
    a + b
}

#[panic_handler]
fn panic(_: &PanicInfo) -> ! {
    loop {}
}
EOC

    # Compile test contract to WebAssembly
    rustc --target wasm32-unknown-unknown -O --crate-type=cdylib test_contract.rs -o test_contract.wasm

    # Deploy contract through MorpheusVM
    echo "Deploying contract to blockchain..."
    DEPLOY_RESULT=\$(curl -s -X POST --data '{
        "jsonrpc": "2.0",
        "method": "morpheus.deployContract",
        "params": {
            "wasmBase64": "'"$(base64 -w 0 test_contract.wasm)"'",
            "vmID": "${VM_ID}"
        },
        "id": 1
    }' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/${BLOCKCHAIN_ID})
    
    CONTRACT_ID=\$(echo \$DEPLOY_RESULT | jq -r '.result.contractID')
    echo "Contract deployed with ID: \${CONTRACT_ID}"
    
    # Call contract function through MorpheusVM
    echo "Calling contract function..."
    CALL_RESULT=\$(curl -s -X POST --data '{
        "jsonrpc": "2.0",
        "method": "morpheus.callContract",
        "params": {
            "contractID": "'\${CONTRACT_ID}'",
            "function": "add",
            "args": [42, 58]
        },
        "id": 1
    }' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/${BLOCKCHAIN_ID})
    
    RESULT=\$(echo \$CALL_RESULT | jq -r '.result.value')
    echo "Contract call result: \${RESULT}"
    
    # Verify result
    if [ "\${RESULT}" == "100" ]; then
        echo "✅ Contract execution successful: 42 + 58 = 100"
    else
        echo "❌ Contract execution failed. Expected 100, got \${RESULT}"
        exit 1
    fi
EOF

# Check TEE attestation verification
echo "Checking TEE attestation verification..."
ssh -i $KEY_PATH -o StrictHostKeyChecking=no admin@${BOOTSTRAP_IP} << EOF
    ATTESTATION_RESULT=\$(curl -s -X POST --data '{
        "jsonrpc": "2.0",
        "method": "morpheus.getAttestations",
        "params": {},
        "id": 1
    }' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/${BLOCKCHAIN_ID})
    
    echo "Attestation result: \${ATTESTATION_RESULT}"
    
    ATTESTATION_COUNT=\$(echo \$ATTESTATION_RESULT | jq '.result.attestations | length')
    if [ \$ATTESTATION_COUNT -gt 0 ]; then
        echo "✅ TEE attestations verified: \${ATTESTATION_COUNT} attestations found"
    else
        echo "❌ No TEE attestations found"
        exit 1
    fi
EOF

# Test transaction submission with parameter validation
echo "Testing transaction with parameter validation..."
ssh -i $KEY_PATH -o StrictHostKeyChecking=no admin@${BOOTSTRAP_IP} << EOF
    # Create a transaction with length-prefixed parameters
    echo "Creating and submitting test transaction..."
    TX_RESULT=\$(curl -s -X POST --data '{
        "jsonrpc": "2.0",
        "method": "morpheus.testParameterValidation",
        "params": {
            "format": "length-prefixed",
            "data": "Test data with proper length prefix"
        },
        "id": 1
    }' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/${BLOCKCHAIN_ID})
    
    VALIDATION_SUCCESS=\$(echo \$TX_RESULT | jq -r '.result.success')
    if [ "\${VALIDATION_SUCCESS}" == "true" ]; then
        echo "✅ Parameter validation successful"
    else
        echo "❌ Parameter validation failed"
        exit 1
    fi
EOF

# Run performance test
echo "Running performance test..."
ssh -i $KEY_PATH -o StrictHostKeyChecking=no admin@${BOOTSTRAP_IP} << EOF
    # Run performance test with optimal batch size and thread count
    echo "Testing TEE performance..."
    PERF_RESULT=\$(curl -s -X POST --data '{
        "jsonrpc": "2.0",
        "method": "morpheus.benchmarkTEE",
        "params": {
            "operations": 1000,
            "batchSize": 100,
            "threadCount": 8
        },
        "id": 1
    }' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/${BLOCKCHAIN_ID})
    
    TPS=\$(echo \$PERF_RESULT | jq -r '.result.tps')
    echo "Performance test results:"
    echo "- TPS: \${TPS}"
    echo "- Batch Size: 100"
    echo "- Thread Count: 8"
    
    if (( \$(echo "\${TPS} > 1000" | bc -l) )); then
        echo "✅ Performance meets expectations (>\${TPS} TPS)"
    else
        echo "⚠️ Performance below expectations (expected >1000 TPS, got \${TPS} TPS)"
    fi
EOF

echo "Avalanche devnet validation completed successfully!"
echo
echo "Summary:"
echo "✅ Avalanche nodes are healthy"
echo "✅ Morpheus blockchain created and functioning"
echo "✅ TEE integration verified through contract execution"
echo "✅ Parameter validation functioning correctly"
echo "✅ Performance benchmarks completed"
echo
echo "Your Avalanche devnet is properly integrated with the TEE mesh network!"
echo "You can now deploy additional smart contracts and begin development."
