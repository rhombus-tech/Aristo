#!/bin/bash
# Update Avalanche and MorpheusVM configurations to point to AWS-deployed TEE mesh network
set -e

if [ ! -f "tee_deployment_info.json" ]; then
    echo "Error: tee_deployment_info.json not found. Please run deploy_single_region.sh first."
    exit 1
fi

# Load coordinator IP from deployment info
COORDINATOR_IP=$(jq -r '.coordinator' tee_deployment_info.json)
REGION_ID=$(jq -r '.region_id' tee_deployment_info.json)
SGX_IP_1=$(jq -r '.tee_pairs[0].sgx' tee_deployment_info.json)
SEV_IP_1=$(jq -r '.tee_pairs[0].sev' tee_deployment_info.json)

echo "Updating Avalanche VM configurations to use AWS coordinator at ${COORDINATOR_IP}"

# Update Avalanche config
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
    "coordinator-url": "http://${COORDINATOR_IP}:7070",
    "primary-tee": "sgx1",
    "secondary-tee": "sev1",
    "verification-level": "dual",
    "batch-size": 100,
    "allowed-formats": ["length_prefix", "direct"]
  }
}
EOF

# Update MorpheusVM config
mkdir -p ./devnet/configs/chains/M
cat > ./devnet/configs/chains/M/config.json << EOF
{
  "tee-integration": {
    "enabled": true,
    "coordinator-url": "http://${COORDINATOR_IP}:7070",
    "primary-worker": "sgx1",
    "secondary-worker": "sev1",
    "verification-level": "dual",
    "supported-formats": ["length_prefix", "direct"],
    "max-batch-size": 100,
    "target-latency-ms": 100
  },
  "vm-config": {
    "hypersdk-config": {
      "log-level": "debug",
      "tee-module-enabled": true
    },
    "state-sync-enabled": true,
    "continuous-profiling-enabled": false,
    "metrics-enabled": true,
    "index-transactions": true
  }
}
EOF

echo "Configuration files updated to use AWS-deployed TEE mesh network"
echo "To run Avalanche with the updated configs, execute:"
echo "  ./run_local_avalanche_devnet.sh"
