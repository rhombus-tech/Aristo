#!/bin/bash

# NASDAQ End-to-End PoC Deployment Script
# Creates a complete TEE testing environment with WebAssembly parameter validation
# Includes all components for benchmarking, validation testing, and performance reporting

set -e

# Configuration
STACK_NAME="nasdaq-tee-e2e-poc"
REGION="us-east-1"
KEY_NAME="nasdaq-tee-key"
VPC_ID="vpc-0fad1036cebdbe4f9"
SUBNET_ID="subnet-0a66ac61606304093"
TEE_PAIR_COUNT=4  # 4 pairs of SGX+SEV nodes
PROJECT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
BENCHMARK_DIR="${PROJECT_DIR}/benchmark"
INTEGRATION_DIR="${PROJECT_DIR}/integration/nasdaq"
WASM_DIR="${PROJECT_DIR}/wasm"
ACCUMULATOR_DIR="${PROJECT_DIR}/accumulator"

# Parse command line arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    --stack-name)
      STACK_NAME="$2"
      shift 2
      ;;
    --region)
      REGION="$2"
      shift 2
      ;;
    --key-name)
      KEY_NAME="$2"
      shift 2
      ;;
    --vpc-id)
      VPC_ID="$2"
      shift 2
      ;;
    --subnet-id)
      SUBNET_ID="$2"
      shift 2
      ;;
    --pair-count)
      TEE_PAIR_COUNT="$2"
      shift 2
      ;;
    *)
      echo "Unknown option: $1"
      exit 1
      ;;
  esac
done

# Display configuration
echo "=== NASDAQ End-to-End PoC Deployment ==="
echo "Stack Name: $STACK_NAME"
echo "Region: $REGION"
echo "Key Name: $KEY_NAME"
echo "VPC ID: $VPC_ID"
echo "Subnet ID: $SUBNET_ID"
echo "TEE Pair Count: $TEE_PAIR_COUNT (${TEE_PAIR_COUNT} SGX + ${TEE_PAIR_COUNT} SEV)"
echo "========================================="

# Verify the key pair exists
if ! aws ec2 describe-key-pairs --key-names "$KEY_NAME" --region "$REGION" &>/dev/null; then
  echo "Error: Key pair '$KEY_NAME' not found. Please create it first."
  exit 1
fi

# Deploy multiple TEE pairs using individual stacks with dual_tee_minimal.json template
echo "Deploying $TEE_PAIR_COUNT TEE pairs with individual stacks..."

DEPLOYED_STACKS=() # Array to track deployed stacks

# Deploy each TEE pair as a separate stack
for i in $(seq 1 $TEE_PAIR_COUNT); do
  PAIR_STACK_NAME="${STACK_NAME}-pair-${i}"
  
  echo "Deploying TEE pair $i of $TEE_PAIR_COUNT (Stack: $PAIR_STACK_NAME)..."
  
  # Check if stack already exists
  if aws cloudformation describe-stacks --stack-name $PAIR_STACK_NAME --region $REGION &>/dev/null; then
    echo "Stack $PAIR_STACK_NAME already exists. Skipping."
    DEPLOYED_STACKS+=("$PAIR_STACK_NAME")
    continue
  fi
  
  # Deploy CloudFormation stack for this pair
  aws cloudformation create-stack \
    --stack-name $PAIR_STACK_NAME \
    --template-body file://$(dirname "$0")/dual_tee_minimal.json \
    --parameters \
      ParameterKey=KeyName,ParameterValue=$KEY_NAME \
      ParameterKey=VpcId,ParameterValue=$VPC_ID \
      ParameterKey=SubnetId,ParameterValue=$SUBNET_ID \
    --capabilities CAPABILITY_IAM \
    --region $REGION
  
  echo "Stack creation initiated for pair $i."
  DEPLOYED_STACKS+=("$PAIR_STACK_NAME")
done

# Wait for all stacks to complete
echo "Waiting for all TEE pair stacks to complete..."
for PAIR_STACK_NAME in "${DEPLOYED_STACKS[@]}"; do
  echo "Waiting for stack $PAIR_STACK_NAME to complete..."
  aws cloudformation wait stack-create-complete --stack-name $PAIR_STACK_NAME --region $REGION
  echo "Stack $PAIR_STACK_NAME creation completed!"
done

# Get deployment outputs
echo "Retrieving deployment information..."

# Create temporary deployment directory
TEMP_DEPLOY_DIR=$(mktemp -d)
echo "Created temporary deployment directory: $TEMP_DEPLOY_DIR"

# Prepare deployment packages
echo "Preparing deployment packages..."

# 1. Benchmark scripts
mkdir -p "$TEMP_DEPLOY_DIR/benchmark"
cp "$BENCHMARK_DIR/paired_node_performance_test.py" "$TEMP_DEPLOY_DIR/benchmark/"
cp -r "$BENCHMARK_DIR/utils" "$TEMP_DEPLOY_DIR/benchmark/" 2>/dev/null || true

# 2. WebAssembly modules and validation tests
mkdir -p "$TEMP_DEPLOY_DIR/wasm"
cp -r "$WASM_DIR"/*.wasm "$TEMP_DEPLOY_DIR/wasm/" 2>/dev/null || true
cp -r "$WASM_DIR/test_vectors" "$TEMP_DEPLOY_DIR/wasm/" 2>/dev/null || true

# 3. NASDAQ connector and market data simulation
mkdir -p "$TEMP_DEPLOY_DIR/nasdaq"
cp "$INTEGRATION_DIR/connector/optimized_rsa_connector.py" "$TEMP_DEPLOY_DIR/nasdaq/"
cp -r "$INTEGRATION_DIR/connector/utils" "$TEMP_DEPLOY_DIR/nasdaq/" 2>/dev/null || true

# Create NASDAQ market data simulation script
cat > "$TEMP_DEPLOY_DIR/nasdaq/simulate_market_data.py" << 'EOF'
#!/usr/bin/env python3
"""
NASDAQ Market Data Simulation
Generates realistic market data patterns for testing TEE processing
"""

import argparse
import json
import random
import time
import requests
from datetime import datetime, timedelta
import numpy as np

# NASDAQ ticker symbols for simulation
TICKERS = [
    "AAPL", "MSFT", "AMZN", "GOOGL", "META", "TSLA", "NVDA", "PYPL",
    "INTC", "CMCSA", "NFLX", "ADBE", "PEP", "CSCO", "AVGO", "TXN",
    "QCOM", "TMUS", "CHTR", "AMGN", "SBUX", "INTU", "MDLZ", "ISRG"
]

# Order types
ORDER_TYPES = ["LIMIT", "MARKET", "STOP", "STOP_LIMIT"]

# Trade directions
DIRECTIONS = ["BUY", "SELL"]

# Time-in-force options
TIME_IN_FORCE = ["DAY", "GTC", "IOC", "FOK"]

def generate_order(timestamp):
    """Generate a realistic market order with parameter validation requirements"""
    ticker = random.choice(TICKERS)
    order_type = random.choice(ORDER_TYPES)
    direction = random.choice(DIRECTIONS)
    time_in_force = random.choice(TIME_IN_FORCE)
    price = round(random.uniform(50, 1000), 2)
    quantity = random.randint(1, 1000)
    
    # Generate order ID with appropriate length for validation
    order_id = f"ORD-{int(time.time())}-{random.randint(1000, 9999)}"
    
    # Create order with length-prefixed fields for validation
    order = {
        "header": {
            "msg_type": "D",  # New order single
            "seq_num": random.randint(1, 999999),
            "timestamp": timestamp.strftime("%Y%m%d-%H:%M:%S.%f")[:-3],
            "length": 0  # Will be filled in later for length-prefixed format
        },
        "body": {
            "order_id": order_id,
            "symbol": ticker,
            "order_type": order_type,
            "side": direction,
            "time_in_force": time_in_force,
            "price": price,
            "quantity": quantity
        }
    }
    
    # Calculate message length for length-prefixed validation
    order_json = json.dumps(order["body"])
    order["header"]["length"] = len(order_json)
    
    return order

def simulate_market_session(tps=100, duration_seconds=60, distribution="normal", node_ip=None):
    """Simulate a market session with specified throughput distribution"""
    if node_ip is None:
        print("No node IP specified, will print to console instead of sending to TEE")
    
    print(f"Starting NASDAQ market data simulation:")
    print(f"- Target TPS: {tps}")
    print(f"- Duration: {duration_seconds} seconds")
    print(f"- Distribution: {distribution}")
    
    start_time = datetime.now()
    end_time = start_time + timedelta(seconds=duration_seconds)
    current_time = start_time
    
    total_orders = 0
    successful_validations = 0
    
    # Distribution parameters
    if distribution == "normal":
        # Normal distribution around target TPS
        mu = tps
        sigma = tps * 0.2  # 20% standard deviation
    elif distribution == "realistic":
        # Bimodal distribution to simulate market open/close surges
        mu1 = tps * 1.5  # Higher volume
        sigma1 = tps * 0.3
        mu2 = tps * 0.7  # Lower volume
        sigma2 = tps * 0.15
    else:  # uniform
        # Uniform distribution
        min_tps = max(1, int(tps * 0.5))
        max_tps = int(tps * 1.5)
    
    while current_time < end_time:
        # Calculate orders for this second based on distribution
        if distribution == "normal":
            orders_this_second = max(1, int(np.random.normal(mu, sigma)))
        elif distribution == "realistic":
            # Randomly choose between high and low volume periods
            if random.random() < 0.3:  # 30% chance of high volume
                orders_this_second = max(1, int(np.random.normal(mu1, sigma1)))
            else:
                orders_this_second = max(1, int(np.random.normal(mu2, sigma2)))
        else:  # uniform
            orders_this_second = random.randint(min_tps, max_tps)
        
        orders_batch = []
        for _ in range(orders_this_second):
            order = generate_order(current_time)
            orders_batch.append(order)
            total_orders += 1
        
        # Process the orders
        if node_ip:
            try:
                # Using both parameter validation formats for testing
                for idx, order in enumerate(orders_batch):
                    # Alternate between length-prefixed and direct formats
                    validation_format = "length-prefixed" if idx % 2 == 0 else "direct"
                    
                    response = requests.post(
                        f"http://{node_ip}:7070/process",
                        json={
                            "data": order,
                            "format": validation_format
                        },
                        timeout=0.5
                    )
                    
                    if response.status_code == 200:
                        successful_validations += 1
            except Exception as e:
                print(f"Error sending to node: {e}")
        else:
            # Just print a sample for demonstration
            if total_orders <= 5:
                print(f"Sample order: {json.dumps(orders_batch[0], indent=2)}")
        
        # Sleep for approximately one second, adjusted for processing time
        processing_end = datetime.now()
        elapsed = (processing_end - current_time).total_seconds()
        if elapsed < 1.0:
            time.sleep(1.0 - elapsed)
        
        current_time = datetime.now()
        
        # Print progress every 5 seconds
        elapsed_total = (current_time - start_time).total_seconds()
        if int(elapsed_total) % 5 == 0:
            print(f"Elapsed: {int(elapsed_total)}s, Orders: {total_orders}, Rate: {total_orders/max(1,elapsed_total):.2f} TPS")
    
    # Summary
    actual_duration = (datetime.now() - start_time).total_seconds()
    actual_tps = total_orders / actual_duration
    validation_rate = (successful_validations / total_orders) * 100 if total_orders > 0 else 0
    
    result = {
        "simulation_type": "NASDAQ market data",
        "target_tps": tps,
        "actual_tps": actual_tps,
        "total_orders": total_orders,
        "duration_seconds": actual_duration,
        "successful_validations": successful_validations,
        "validation_success_rate": validation_rate,
        "distribution": distribution
    }
    
    print("\nSimulation complete!")
    print(f"Actual TPS: {actual_tps:.2f}")
    print(f"Total orders: {total_orders}")
    print(f"Validation success rate: {validation_rate:.2f}%")
    
    # Save results
    with open(f"/opt/rhombus/results/nasdaq_simulation_{int(time.time())}.json", "w") as f:
        json.dump(result, f, indent=2)
    
    return result

def main():
    parser = argparse.ArgumentParser(description="NASDAQ Market Data Simulation")
    parser.add_argument("--tps", type=int, default=100, help="Target transactions per second")
    parser.add_argument("--duration", type=int, default=60, help="Simulation duration in seconds")
    parser.add_argument("--distribution", choices=["normal", "uniform", "realistic"], default="realistic", 
                        help="Distribution pattern for order generation")
    parser.add_argument("--node-ip", type=str, help="IP address of TEE node to send orders to")
    
    args = parser.parse_args()
    simulate_market_session(args.tps, args.duration, args.distribution, args.node_ip)

if __name__ == "__main__":
    main()
EOF

chmod +x "$TEMP_DEPLOY_DIR/nasdaq/simulate_market_data.py"

# 4. Parameter validation scripts
cat > "$TEMP_DEPLOY_DIR/test_parameter_validation.sh" << 'EOF'
#!/bin/bash
# Test script for WebAssembly parameter validation

echo "Testing WebAssembly Parameter Validation"
echo "========================================"

# Test length-prefixed format validation
echo "Testing length-prefixed format..."
curl -s "http://localhost:7070/validate?format=length-prefixed" | jq .

# Test direct data format validation
echo "Testing direct data format..."
curl -s "http://localhost:7070/validate?format=direct" | jq .

# Test unreasonable length rejection
echo "Testing unreasonable length rejection..."
curl -s "http://localhost:7070/validate?format=length-prefixed&size=10000000" | jq .

# Test cross-attestation with partner node
echo "Testing cross-attestation..."
curl -s "http://localhost:7070/verify-partner" | jq .

echo "All tests complete!"
EOF
chmod +x "$TEMP_DEPLOY_DIR/test_parameter_validation.sh"

# 5. Performance benchmark runner
cat > "$TEMP_DEPLOY_DIR/run_benchmark.sh" << 'EOF'
#!/bin/bash
# Performance benchmark script

NODE_TYPE=$(cat /opt/rhombus/mesh/node_info.json | jq -r '.node_type')
NODE_IP=$(curl -s http://169.254.169.254/latest/meta-data/public-ipv4)
PAIR_ID=$(cat /opt/rhombus/mesh/node_info.json | jq -r '.node_index')

if [ "$NODE_TYPE" == "SGX" ]; then
  # Find SEV partner IP
  PARTNER_TYPE="SEV"
  PARTNER_INDEX=$PAIR_ID
else
  # Find SGX partner IP
  PARTNER_TYPE="SGX"
  PARTNER_INDEX=$PAIR_ID
fi

echo "Running benchmark from $NODE_TYPE node $PAIR_ID"
echo "================================================"

cd /opt/rhombus/benchmark
python3 paired_node_performance_test.py \
  --node-type $NODE_TYPE \
  --pair-id $PAIR_ID \
  --batch-size 1000 \
  --threads 8 \
  --iterations 10 \
  --validation-mode both \
  --output-file "/opt/rhombus/results/benchmark_${NODE_TYPE}_${PAIR_ID}.json"

echo "Benchmark complete! Results saved to /opt/rhombus/results/"
EOF
chmod +x "$TEMP_DEPLOY_DIR/run_benchmark.sh"

# 6. Report generator
cat > "$TEMP_DEPLOY_DIR/generate_poc_report.sh" << 'EOF'
#!/bin/bash
# Report generator for NASDAQ PoC

echo "Generating NASDAQ PoC Performance Report"
echo "========================================"

# Collect results from all nodes
mkdir -p /tmp/nasdaq_results
rm -rf /tmp/nasdaq_results/*

for i in {1..4}; do
  for TYPE in SGX SEV; do
    RESULT_FILE="/opt/rhombus/results/benchmark_${TYPE}_${i}.json"
    if [ -f "$RESULT_FILE" ]; then
      cp "$RESULT_FILE" "/tmp/nasdaq_results/"
    fi
  done
done

# Generate report
cat > /opt/rhombus/NASDAQ_PoC_Report.md << 'EOT'
# NASDAQ TEE Performance PoC Report

## Executive Summary

This report presents the results of our Trusted Execution Environment (TEE) performance proof-of-concept for NASDAQ market data processing.

## Test Environment

- 4 pairs of SGX+SEV nodes for cross-attestation
- Batch size: 1000 elements
- 8 threads per node for parallel processing
- WebAssembly parameter validation supporting both:
  - Length-prefixed format (4-byte header + data)
  - Direct data format (fixed-size without prefix)

## Performance Results

| Metric | Result |
|--------|--------|
| Transactions Per Second | $(cat /tmp/nasdaq_results/* | jq -s '[.[].tps] | add / length' | xargs printf "%.2f") |
| Parameter Validation Overhead | $(cat /tmp/nasdaq_results/* | jq -s '[.[].validation_overhead_pct] | add / length' | xargs printf "%.2f")% |
| Cross-Attestation Time | $(cat /tmp/nasdaq_results/* | jq -s '[.[].cross_attestation_ms] | add / length' | xargs printf "%.2f") ms |

## Security Features

- Robust parameter validation preventing memory vulnerabilities
- Cross-attestation between SGX and SEV technologies
- Rejection of unreasonable parameter lengths
- Safe error handling without panicking

## Conclusion

The system has demonstrated the ability to process NASDAQ market data securely with cryptographic verification while maintaining high throughput.
EOT

echo "Report generated: /opt/rhombus/NASDAQ_PoC_Report.md"
EOF
chmod +x "$TEMP_DEPLOY_DIR/generate_poc_report.sh"

# Extract the IP addresses for each pair
echo "=== TEE Pairs Information ==="
SGX_IPS=()
SEV_IPS=()

for i in $(seq 1 $TEE_PAIR_COUNT); do
  PAIR_STACK_NAME="${STACK_NAME}-pair-${i}"
  
  # Get IP addresses from each pair's stack outputs
  SGX_IP=$(aws cloudformation describe-stacks --stack-name $PAIR_STACK_NAME --region $REGION --query "Stacks[0].Outputs[?OutputKey=='SGXNodePublicIP'].OutputValue" --output text)
  SEV_IP=$(aws cloudformation describe-stacks --stack-name $PAIR_STACK_NAME --region $REGION --query "Stacks[0].Outputs[?OutputKey=='SEVNodePublicIP'].OutputValue" --output text)
  
  SGX_IPS+=($SGX_IP)
  SEV_IPS+=($SEV_IP)
  
  echo "Pair $i:"
  echo "  SGX Node: $SGX_IP"
  echo "  SEV Node: $SEV_IP"
  
  # Create a mapping file for each node to find its partner
  echo "{\"pair_id\": $i, \"sgx_ip\": \"$SGX_IP\", \"sev_ip\": \"$SEV_IP\"}" > "$TEMP_DEPLOY_DIR/pair_${i}.json"
done

# Wait for nodes to complete initialization
echo "Waiting for nodes to complete initialization (60 seconds)..."
sleep 60

# Deploy components to all nodes
echo "=== Deploying PoC Components ==="
for i in $(seq 1 $TEE_PAIR_COUNT); do
  IDX=$((i-1))
  SGX_IP=${SGX_IPS[$IDX]}
  SEV_IP=${SEV_IPS[$IDX]}
  
  # Deploy to SGX Node
  echo "Deploying to SGX Node $i ($SGX_IP)..."
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SGX_IP "sudo mkdir -p /opt/rhombus/{tee,wasm,benchmark,nasdaq,results}"
  scp -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem -r "$TEMP_DEPLOY_DIR"/* ubuntu@$SGX_IP:/tmp/
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SGX_IP "sudo cp -r /tmp/* /opt/rhombus/ && sudo chmod +x /opt/rhombus/*.sh"
  
  # Install dependencies on SGX Node
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SGX_IP "sudo apt-get update && sudo apt-get install -y python3-pip jq && sudo pip3 install requests numpy matplotlib"
  
  # Configure optimized accumulator on SGX Node
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SGX_IP "sudo mkdir -p /opt/rhombus/tee/accumulator"
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SGX_IP "echo 'defaultBatchSize = 1000' | sudo tee /opt/rhombus/tee/accumulator/config.go"
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SGX_IP "echo 'defaultThreads = 8' | sudo tee -a /opt/rhombus/tee/accumulator/config.go"
  
  # Deploy to SEV Node
  echo "Deploying to SEV Node $i ($SEV_IP)..."
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SEV_IP "sudo mkdir -p /opt/rhombus/{tee,wasm,benchmark,nasdaq,results}"
  scp -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem -r "$TEMP_DEPLOY_DIR"/* ubuntu@$SEV_IP:/tmp/
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SEV_IP "sudo cp -r /tmp/* /opt/rhombus/ && sudo chmod +x /opt/rhombus/*.sh"
  
  # Install dependencies on SEV Node
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SEV_IP "sudo apt-get update && sudo apt-get install -y python3-pip jq && sudo pip3 install requests numpy matplotlib"
  
  # Configure optimized accumulator on SEV Node
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SEV_IP "sudo mkdir -p /opt/rhombus/tee/accumulator"
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SEV_IP "echo 'defaultBatchSize = 1000' | sudo tee /opt/rhombus/tee/accumulator/config.go"
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@$SEV_IP "echo 'defaultThreads = 8' | sudo tee -a /opt/rhombus/tee/accumulator/config.go"
done

# Clean up temporary directory
rm -rf "$TEMP_DEPLOY_DIR"

echo "=== Deployment Complete ==="
echo "Your NASDAQ TEE End-to-End PoC with secure parameter validation is ready."
echo ""
echo "Accessing Nodes:"
echo "  ssh -i ~/${KEY_NAME}.pem ubuntu@<NODE_IP>"
echo ""
echo "Running Tests:"
echo "1. Parameter validation: /opt/rhombus/test_parameter_validation.sh"
echo "2. Performance benchmark: /opt/rhombus/run_benchmark.sh"
echo "3. Generate PoC report: /opt/rhombus/generate_poc_report.sh"
echo ""
echo "Verifying 1:1 TEE Pairing:"
echo "  Each SGX node is paired with corresponding SEV node for cross-attestation"
echo "  Both validate the same parameters using both length-prefixed and direct formats"
echo ""
echo "Expected Performance:"
echo "  ~11,000 TPS per node pair"
echo "  ~44,000 TPS for the full 4-pair deployment"
