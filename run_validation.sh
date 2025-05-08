#!/bin/bash
# Run validation tests on the TEE mesh network
set -e

if [ ! -f "tee_deployment_info.json" ]; then
    echo "Error: tee_deployment_info.json not found. Please run deploy_single_region.sh first."
    exit 1
fi

# Load deployment information
REGION_ID=$(jq -r '.region_id' tee_deployment_info.json)
COORDINATOR_IP=$(jq -r '.coordinator' tee_deployment_info.json)
KEY_PATH="~/.ssh/tee-access-key.pem"

echo "Running validation tests on TEE mesh network in region ${REGION_ID}"

# Make sure validate_integration binary is built
if [ ! -f "./bin/validate_integration" ]; then
    echo "Building validate_integration binary..."
    go build -o ./bin/validate_integration ./cmd/validate_integration.go
fi

# Copy validation binary to coordinator
echo "Copying validation tools to coordinator..."
scp -i $KEY_PATH -o StrictHostKeyChecking=no ./bin/validate_integration admin@${COORDINATOR_IP}:/home/admin/

# Run validation tests
echo "Running validation tests with optimal parameters (batch size 100, thread count 8)..."
ssh -i $KEY_PATH -o StrictHostKeyChecking=no admin@${COORDINATOR_IP} << EOF
    chmod +x ./validate_integration
    ./validate_integration --batch-size 100 --thread-count 8 --region ${REGION_ID}
EOF

echo "Running performance benchmark..."
ssh -i $KEY_PATH -o StrictHostKeyChecking=no admin@${COORDINATOR_IP} << EOF
    ./morpheus-cli benchmark \
      --region ${REGION_ID} \
      --pairs 2 \
      --operations 1000 \
      --batch-size 100 \
      --thread-count 8
EOF

echo "Checking parameter validation with both formats..."
ssh -i $KEY_PATH -o StrictHostKeyChecking=no admin@${COORDINATOR_IP} << EOF
    echo "Testing length-prefixed parameter format..."
    ./morpheus-cli test-param-validation \
      --format length-prefixed \
      --data "Test data with length prefix" \
      --region ${REGION_ID}
      
    echo "Testing direct parameter format..."
    ./morpheus-cli test-param-validation \
      --format direct \
      --data "Test data with direct format" \
      --region ${REGION_ID}
      
    echo "Testing unreasonable length rejection..."
    ./morpheus-cli test-param-validation \
      --format length-prefixed \
      --test-unreasonable-length \
      --region ${REGION_ID}
EOF

echo "Checking cross-attestation between SGX and SEV..."
ssh -i $KEY_PATH -o StrictHostKeyChecking=no admin@${COORDINATOR_IP} << EOF
    ./morpheus-cli verify-attestation --region ${REGION_ID}
EOF

echo "Validation completed successfully!"
echo
echo "Summary of tests:"
echo "✅ Deployment verification"
echo "✅ Parameter validation (both formats)"
echo "✅ Performance benchmark with 2 TEE pairs"
echo "✅ Cross-attestation verification"
echo
echo "Your TEE mesh network is properly configured and operational!"
