#!/usr/bin/env python3
# Optimized TEE Performance Benchmark - Paired Node Test
# This script tests the performance of paired SGX+SEV nodes
# NASDAQ Proof of Concept - April 2025

import argparse
import concurrent.futures
import json
import statistics
import time
import requests
import sys
import os
from datetime import datetime

# Default configuration
DEFAULT_BATCH_SIZE = 1000
DEFAULT_THREAD_COUNT = 8
DEFAULT_TEST_DURATION = 300  # 5 minutes
DEFAULT_PAIRS = 4  # 4 SGX+SEV pairs

def get_args():
    parser = argparse.ArgumentParser(description='TEE Paired Node Performance Benchmark')
    parser.add_argument('--sgx-nodes', nargs='+', required=True, help='List of SGX node IPs')
    parser.add_argument('--sev-nodes', nargs='+', required=True, help='List of SEV node IPs')
    parser.add_argument('--batch-size', type=int, default=DEFAULT_BATCH_SIZE, help=f'Batch size (default: {DEFAULT_BATCH_SIZE})')
    parser.add_argument('--thread-count', type=int, default=DEFAULT_THREAD_COUNT, help=f'Thread count per node (default: {DEFAULT_THREAD_COUNT})')
    parser.add_argument('--duration', type=int, default=DEFAULT_TEST_DURATION, help=f'Test duration in seconds (default: {DEFAULT_TEST_DURATION})')
    parser.add_argument('--pairs', type=int, default=DEFAULT_PAIRS, help=f'Number of node pairs to test (default: {DEFAULT_PAIRS})')
    parser.add_argument('--output', type=str, default='performance_results.json', help='Output file for results')
    return parser.parse_args()

def generate_test_data(batch_size):
    """Generate test transaction data for a batch"""
    return [f"tx_{i}_{time.time()}" for i in range(batch_size)]

def run_paired_test(sgx_ip, sev_ip, batch_size, thread_count, duration):
    """Run performance test on a SGX+SEV node pair"""
    print(f"Starting paired test: SGX ({sgx_ip}) + SEV ({sev_ip})")
    
    # Test parameters
    start_time = time.time()
    end_time = start_time + duration
    transaction_count = 0
    batch_count = 0
    latencies = []
    
    # Configure the nodes
    sgx_config = {
        'batch_size': batch_size,
        'thread_count': thread_count,
        'pair_ip': sev_ip,
        'test_mode': 'paired'
    }
    
    sev_config = {
        'batch_size': batch_size,
        'thread_count': thread_count,
        'pair_ip': sgx_ip,
        'test_mode': 'paired'
    }
    
    try:
        # Configure the nodes
        requests.post(f"http://{sgx_ip}:8080/configure", json=sgx_config, timeout=5)
        requests.post(f"http://{sev_ip}:8080/configure", json=sev_config, timeout=5)
        
        # Run the test
        while time.time() < end_time:
            test_data = generate_test_data(batch_size)
            
            # Time the transaction processing
            batch_start = time.time()
            
            # Send to SGX node which coordinates with its SEV pair
            response = requests.post(
                f"http://{sgx_ip}:8080/process_batch",
                json={'transactions': test_data},
                timeout=30
            )
            
            if response.status_code == 200:
                batch_end = time.time()
                batch_latency = (batch_end - batch_start) * 1000  # convert to ms
                
                batch_count += 1
                transaction_count += batch_size
                latencies.append(batch_latency)
                
                # Print progress every 10 batches
                if batch_count % 10 == 0:
                    elapsed = time.time() - start_time
                    tps = transaction_count / elapsed
                    print(f"Pair SGX:{sgx_ip} + SEV:{sev_ip} - Processed {transaction_count} transactions, Current TPS: {tps:.2f}")
            else:
                print(f"Error processing batch: {response.text}")
        
        # Calculate results
        test_duration = time.time() - start_time
        tps = transaction_count / test_duration
        avg_latency = statistics.mean(latencies) if latencies else 0
        p95_latency = statistics.quantiles(latencies, n=20)[18] if len(latencies) >= 20 else 0
        p99_latency = statistics.quantiles(latencies, n=100)[98] if len(latencies) >= 100 else 0
        
        result = {
            'sgx_node': sgx_ip,
            'sev_node': sev_ip,
            'batch_size': batch_size,
            'thread_count': thread_count,
            'test_duration_sec': test_duration,
            'transaction_count': transaction_count,
            'batch_count': batch_count,
            'tps': tps,
            'avg_latency_ms': avg_latency,
            'p95_latency_ms': p95_latency,
            'p99_latency_ms': p99_latency
        }
        
        print(f"Paired test complete: SGX ({sgx_ip}) + SEV ({sev_ip})")
        print(f"  - TPS: {tps:.2f}")
        print(f"  - Avg Latency: {avg_latency:.2f} ms")
        print(f"  - P95 Latency: {p95_latency:.2f} ms")
        print(f"  - P99 Latency: {p99_latency:.2f} ms")
        
        return result
    
    except Exception as e:
        print(f"Error in paired test {sgx_ip}+{sev_ip}: {str(e)}")
        return {
            'sgx_node': sgx_ip,
            'sev_node': sev_ip,
            'error': str(e)
        }

def main():
    args = get_args()
    
    # Validate inputs
    if len(args.sgx_nodes) < args.pairs:
        print(f"Error: Need at least {args.pairs} SGX nodes for paired testing")
        sys.exit(1)
    if len(args.sev_nodes) < args.pairs:
        print(f"Error: Need at least {args.pairs} SEV nodes for paired testing")
        sys.exit(1)
    
    # Select the number of pairs to test
    sgx_nodes = args.sgx_nodes[:args.pairs]
    sev_nodes = args.sev_nodes[:args.pairs]
    
    print(f"=== TEE Paired Node Performance Benchmark ===")
    print(f"Starting benchmark with {args.pairs} SGX+SEV node pairs")
    print(f"Configuration:")
    print(f"  - Batch Size: {args.batch_size}")
    print(f"  - Thread Count: {args.thread_count}")
    print(f"  - Test Duration: {args.duration} seconds")
    print(f"  - SGX Nodes: {sgx_nodes}")
    print(f"  - SEV Nodes: {sev_nodes}")
    print(f"=============================================")
    
    # Run tests in parallel
    results = []
    with concurrent.futures.ThreadPoolExecutor(max_workers=args.pairs) as executor:
        futures = []
        for i in range(args.pairs):
            futures.append(
                executor.submit(
                    run_paired_test,
                    sgx_nodes[i],
                    sev_nodes[i],
                    args.batch_size,
                    args.thread_count,
                    args.duration
                )
            )
        
        # Collect results
        for future in concurrent.futures.as_completed(futures):
            result = future.result()
            results.append(result)
    
    # Calculate aggregate results
    successful_results = [r for r in results if 'error' not in r]
    if successful_results:
        total_tps = sum(r['tps'] for r in successful_results)
        avg_latency = statistics.mean(r['avg_latency_ms'] for r in successful_results)
        total_tx = sum(r['transaction_count'] for r in successful_results)
        
        # Create summary
        summary = {
            'timestamp': datetime.now().isoformat(),
            'configuration': {
                'pairs': args.pairs,
                'batch_size': args.batch_size,
                'thread_count': args.thread_count,
                'test_duration': args.duration
            },
            'aggregate_results': {
                'total_tps': total_tps,
                'avg_latency_ms': avg_latency,
                'total_transactions': total_tx,
                'successful_pairs': len(successful_results),
                'projected_12_node_tps': (total_tps / len(successful_results)) * 6 if successful_results else 0
            },
            'pair_results': results
        }
        
        # Print summary
        print("\n=== Benchmark Results ===")
        print(f"Total TPS: {total_tps:.2f}")
        print(f"Average Latency: {avg_latency:.2f} ms")
        print(f"Total Transactions: {total_tx}")
        print(f"Successful Pairs: {len(successful_results)} of {args.pairs}")
        print(f"Projected 12-Node TPS: {summary['aggregate_results']['projected_12_node_tps']:.2f}")
        
        # Save results
        with open(args.output, 'w') as f:
            json.dump(summary, f, indent=2)
        print(f"Results saved to {args.output}")
    else:
        print("No successful test results")

if __name__ == "__main__":
    main()
