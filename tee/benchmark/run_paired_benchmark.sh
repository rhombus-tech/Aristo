#!/bin/bash
# Run the paired node performance test against our deployed infrastructure
# This executes the test across the 4 SGX+SEV node pairs we have operational

# Activate virtual environment where requests is installed
SCRIPT_DIR=$(dirname "$0")
source "$SCRIPT_DIR/venv/bin/activate"

# SGX Nodes (taking the first 4 since we have 4 pairs)
SGX_NODES=(
  "54.158.85.194"
  "54.91.177.223"
  "18.234.109.248"
  "3.208.29.137"
)

# SEV Nodes (all 4 that are running)
SEV_NODES=(
  "35.172.181.241"
  "3.91.64.154"
  "107.20.15.79"
  "35.175.221.87"
)

# Make the test script executable
chmod +x "$SCRIPT_DIR/paired_node_performance_test.py"

# Run the benchmark
echo "Starting paired node performance benchmark..."
python3 "$SCRIPT_DIR/paired_node_performance_test.py" \
  --sgx-nodes "${SGX_NODES[@]}" \
  --sev-nodes "${SEV_NODES[@]}" \
  --batch-size 1000 \
  --thread-count 8 \
  --duration 300 \
  --pairs 4 \
  --output "nasdaq_paired_node_results_$(date +%Y%m%d_%H%M%S).json"

echo "Benchmark complete!"
# Deactivate virtual environment
deactivate
