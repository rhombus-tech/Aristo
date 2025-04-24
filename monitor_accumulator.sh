#!/bin/bash
# monitor_accumulator.sh - Monitor RSA Accumulator performance
# Tracks dual-format parameter validation performance across TEE nodes

set -e

# Configuration
SGX_NODES=("ec2-54-225-41-220.compute-1.amazonaws.com" "ec2-52-54-181-245.compute-1.amazonaws.com")
SEV_NODES=("ec2-3-81-65-148.compute-1.amazonaws.com" "ec2-54-161-2-145.compute-1.amazonaws.com")
ACCUMULATOR_PORT=7101
MONITOR_INTERVAL=60  # seconds

# ANSI color codes
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to check node health
check_health() {
  local node=$1
  
  if curl -s --max-time 5 "http://$node:$ACCUMULATOR_PORT/health" | grep -q "ok"; then
    echo -e "${GREEN}✓${NC} $node"
    return 0
  else
    echo -e "${RED}✗${NC} $node"
    return 1
  fi
}

# Function to get node stats
get_stats() {
  local node=$1
  local stats=$(curl -s --max-time 5 "http://$node:$ACCUMULATOR_PORT/stats")
  
  if [ -z "$stats" ]; then
    echo -e "${RED}Failed to get stats from $node${NC}"
    return
  fi
  
  # Extract key metrics
  local successful=$(echo $stats | grep -o '"successful_requests":[0-9]*' | cut -d':' -f2)
  local failed=$(echo $stats | grep -o '"failed_requests":[0-9]*' | cut -d':' -f2)
  local length_prefixed=$(echo $stats | grep -o '"length_prefixed":[0-9]*' | cut -d':' -f2)
  local direct_format=$(echo $stats | grep -o '"direct_format":[0-9]*' | cut -d':' -f2)
  local cross_validations=$(echo $stats | grep -o '"cross_validations":[0-9]*' | cut -d':' -f2)
  local cross_val_matches=$(echo $stats | grep -o '"cross_val_matches":[0-9]*' | cut -d':' -f2)
  local avg_latency=$(echo $stats | grep -o '"avg_latency_ms":[0-9.]*' | cut -d':' -f2)
  local peak_tps=$(echo $stats | grep -o '"peak_tps":[0-9.]*' | cut -d':' -f2)
  local tee_type=$(echo $stats | grep -o '"tee_type":"[^"]*"' | cut -d':' -f2 | tr -d '"')
  
  # Calculate success rate
  local total=$((successful + failed))
  local success_rate=0
  if [ $total -gt 0 ]; then
    success_rate=$(echo "scale=2; 100 * $successful / $total" | bc)
  fi
  
  # Calculate cross-validation success rate
  local cross_val_rate=0
  if [ "$cross_validations" != "" ] && [ $cross_validations -gt 0 ]; then
    cross_val_rate=$(echo "scale=2; 100 * $cross_val_matches / $cross_validations" | bc)
  fi
  
  # Calculate format distribution
  local length_prefix_rate=0
  local direct_format_rate=0
  local format_total=$((length_prefixed + direct_format))
  if [ $format_total -gt 0 ]; then
    length_prefix_rate=$(echo "scale=2; 100 * $length_prefixed / $format_total" | bc)
    direct_format_rate=$(echo "scale=2; 100 * $direct_format / $format_total" | bc)
  fi
  
  # Print metrics
  echo -e "Node: ${BLUE}$node${NC} (TEE Type: ${YELLOW}$tee_type${NC})"
  echo -e "  Success Rate: ${success_rate}% (${successful}/${total})"
  echo -e "  Parameter Format: ${length_prefix_rate}% Length-Prefixed, ${direct_format_rate}% Direct"
  echo -e "  Cross-Validation Match Rate: ${cross_val_rate}% (${cross_val_matches}/${cross_validations})"
  echo -e "  Performance: ${BLUE}${avg_latency}ms${NC} avg latency, ${GREEN}${peak_tps}${NC} peak TPS"
  echo ""
}

# Monitor function
monitor() {
  while true; do
    clear
    echo -e "${BLUE}========== RSA ACCUMULATOR MONITORING ==========${NC}"
    echo -e "${BLUE}========== $(date) ==========${NC}"
    echo ""
    
    echo -e "${YELLOW}SGX NODES:${NC}"
    for node in "${SGX_NODES[@]}"; do
      check_health "$node" && get_stats "$node"
    done
    
    echo -e "${YELLOW}SEV NODES:${NC}"
    for node in "${SEV_NODES[@]}"; do
      check_health "$node" && get_stats "$node"
    done
    
    echo -e "${BLUE}===============================================${NC}"
    echo -e "Press Ctrl+C to exit. Refreshing in ${MONITOR_INTERVAL}s..."
    sleep $MONITOR_INTERVAL
  done
}

# Start monitoring
monitor
