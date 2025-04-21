#!/bin/bash
# NASDAQ Market Data Simulation with Optimized TEE Accumulator
# Run a high-throughput simulation to validate performance targets

set -e  # Exit on error

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Default configuration
NUM_MESSAGES=500000  # Half a million messages
BATCH_SIZE=1000      # Optimal batch size from our testing
NUM_NODES=4          # 4 nodes (2 SGX, 2 SEV)
WORKER_THREADS=8     # 8 worker threads
TARGET_TPS=50000     # 50k TPS target
OUTPUT_FILE="simulation_results_$(date +%Y%m%d_%H%M%S).json"
REALTIME_MODE=""     # Empty by default, set to "--simulate-realtime" for real-time mode

# Parse command line arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    --messages)
      NUM_MESSAGES="$2"
      shift 2
      ;;
    --batch-size)
      BATCH_SIZE="$2"
      shift 2
      ;;
    --nodes)
      NUM_NODES="$2"
      shift 2
      ;;
    --threads)
      WORKER_THREADS="$2"
      shift 2
      ;;
    --target)
      TARGET_TPS="$2"
      shift 2
      ;;
    --output)
      OUTPUT_FILE="$2"
      shift 2
      ;;
    --realtime)
      REALTIME_MODE="--simulate-realtime"
      shift
      ;;
    *)
      echo "Unknown option: $1"
      exit 1
      ;;
  esac
done

echo "===== NASDAQ Market Data Simulation ====="
echo "Messages:       $NUM_MESSAGES"
echo "Batch Size:     $BATCH_SIZE"
echo "Nodes:          $NUM_NODES"
echo "Worker Threads: $WORKER_THREADS"
echo "Target TPS:     $TARGET_TPS"
echo "Output File:    $OUTPUT_FILE"
echo "Real-time Mode: ${REALTIME_MODE:-disabled}"
echo "========================================"

# Check if Python is available
if ! command -v python3 &> /dev/null; then
    echo "Error: Python 3 is required but not found"
    exit 1
fi

# Run the simulation
echo "Starting simulation..."
python3 run_optimized_simulation.py \
  --num-messages "$NUM_MESSAGES" \
  --batch-size "$BATCH_SIZE" \
  --num-nodes "$NUM_NODES" \
  --worker-threads "$WORKER_THREADS" \
  --target-tps "$TARGET_TPS" \
  --output "$OUTPUT_FILE" \
  $REALTIME_MODE

echo "Simulation complete. Results saved to $OUTPUT_FILE"

# Display quick summary
if [ -f "$OUTPUT_FILE" ]; then
    echo ""
    echo "===== Quick Summary ====="
    OVERALL_TPS=$(grep -o '"overall_tps": [0-9.]*' "$OUTPUT_FILE" | cut -d ' ' -f 2)
    TARGET_MET=$(grep -o '"target_met": [a-z]*' "$OUTPUT_FILE" | cut -d ' ' -f 2)
    
    if [ "$TARGET_MET" == "true" ]; then
        echo "✅ Performance target achieved: $OVERALL_TPS TPS"
    else
        echo "❌ Performance target not met: $OVERALL_TPS TPS"
    fi
    
    # Print node-specific stats
    echo ""
    echo "Node performance:"
    python3 -c "
import json
with open('$OUTPUT_FILE', 'r') as f:
    data = json.load(f)
    for i, node in enumerate(data.get('node_stats', [])):
        print(f\"  Node {i+1} ({node['tee_type']}): {node['transactions_per_second']:.2f} TPS\")
"
    echo ""
    echo "For detailed results, see $OUTPUT_FILE"
fi
