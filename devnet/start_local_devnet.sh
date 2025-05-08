#!/bin/bash
set -e

echo "Starting local TEE mesh network with Avalanche integration..."

# Start coordinator
echo "Starting coordinator..."
./coordinator/coordinator_mock --config ./configs/coordinator.json > ./coordinator/coordinator.log 2>&1 &
COORDINATOR_PID=$!
echo "Coordinator started with PID $COORDINATOR_PID"

# Wait for coordinator to initialize
sleep 2

# Start SGX1
echo "Starting SGX1 TEE node..."
./sgx1/tee-controller \
  --port 8081 \
  --tee-id sgx1 \
  --tee-type sgx \
  --region-id local-region-1 \
  --mesh-enabled \
  --execution-mode auto \
  --verbose \
  --max-peers 10 \
  --circuit-breaker-threshold-ms 100 \
  --base-dir /tmp/tee-sgx1 \
  --discovery-endpoint http://127.0.0.1:9080 \
  --simulate > ./sgx1/sgx1.log 2>&1 &
SGX1_PID=$!
echo "SGX1 started with PID $SGX1_PID"

# Start SGX2
echo "Starting SGX2 TEE node..."
./sgx2/tee-controller \
  --port 8082 \
  --tee-id sgx2 \
  --tee-type sgx \
  --region-id local-region-1 \
  --mesh-enabled \
  --execution-mode auto \
  --verbose \
  --max-peers 10 \
  --circuit-breaker-threshold-ms 100 \
  --base-dir /tmp/tee-sgx2 \
  --discovery-endpoint http://127.0.0.1:9080 \
  --simulate > ./sgx2/sgx2.log 2>&1 &
SGX2_PID=$!
echo "SGX2 started with PID $SGX2_PID"

# Start SEV1
echo "Starting SEV1 TEE node..."
./sev1/tee-controller \
  --port 8083 \
  --tee-id sev1 \
  --tee-type sev \
  --region-id local-region-1 \
  --mesh-enabled \
  --execution-mode auto \
  --verbose \
  --max-peers 10 \
  --circuit-breaker-threshold-ms 100 \
  --base-dir /tmp/tee-sev1 \
  --discovery-endpoint http://127.0.0.1:9080 \
  --simulate > ./sev1/sev1.log 2>&1 &
SEV1_PID=$!
echo "SEV1 started with PID $SEV1_PID"

# Start SEV2
echo "Starting SEV2 TEE node..."
./sev2/tee-controller \
  --port 8084 \
  --tee-id sev2 \
  --tee-type sev \
  --region-id local-region-1 \
  --mesh-enabled \
  --execution-mode auto \
  --verbose \
  --max-peers 10 \
  --circuit-breaker-threshold-ms 100 \
  --base-dir /tmp/tee-sev2 \
  --discovery-endpoint http://127.0.0.1:9080 \
  --simulate > ./sev2/sev2.log 2>&1 &
SEV2_PID=$!
echo "SEV2 started with PID $SEV2_PID"

# Wait for TEE nodes to initialize
sleep 3

# Start metrics collector
echo "Starting metrics collector..."
if [ -f ./metrics/prometheus.yml ]; then
  prometheusPath=$(which prometheus 2>/dev/null)
  if [ -n "$prometheusPath" ]; then
    prometheus --config.file=./metrics/prometheus.yml > ./metrics/metrics.log 2>&1 &
    METRICS_PID=$!
    echo "Metrics collector started with PID $METRICS_PID"
  else
    echo "Prometheus not found, skipping metrics collection"
  fi
fi

echo "All TEE nodes started successfully!"
echo "Coordinator: http://127.0.0.1:9080"
echo "SGX1: http://127.0.0.1:8081"
echo "SGX2: http://127.0.0.1:8082"
echo "SEV1: http://127.0.0.1:8083"
echo "SEV2: http://127.0.0.1:8084"
echo "Metrics Dashboard (if installed): http://127.0.0.1:9090"

# Create a file with PIDs for stopping
cat > ./stop_pids.txt << EOT
$COORDINATOR_PID
$SGX1_PID
$SGX2_PID
$SEV1_PID
$SEV2_PID
${METRICS_PID:-}
EOT

echo "To stop the devnet, run: ./stop_local_devnet.sh"
