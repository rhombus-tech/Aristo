#!/usr/bin/env python3
# NASDAQ Market Data Simulation for Dual TEE Mesh
# Adapted for our deployed SGX+SEV TEE mesh network
# April 2025

import argparse
import concurrent.futures
import json
import statistics
import time
import requests
import sys
import os
import base64
import random
import struct
from datetime import datetime

# Default configuration
DEFAULT_BATCH_SIZE = 500  # SGX nodes configured with 500-element batches
DEFAULT_TEST_DURATION = 300  # 5 minutes
DEFAULT_ITERATIONS = 5000  # High iteration count for throughput testing
DEFAULT_CONCURRENT_CLIENTS = 8  # Match the 8-thread parallel execution model

# Our deployed node IPs
SGX_NODES = ["54.226.83.253", "3.80.113.74"]
SEV_NODES = ["54.209.198.69", "52.206.202.214"]
COORDINATOR_IP = "54.226.83.253"

def get_args():
    parser = argparse.ArgumentParser(description='NASDAQ Market Data Simulation for Dual TEE Mesh')
    parser.add_argument('--batch-size', type=int, default=DEFAULT_BATCH_SIZE, 
                        help=f'Batch size (default: {DEFAULT_BATCH_SIZE})')
    parser.add_argument('--duration', type=int, default=DEFAULT_TEST_DURATION, 
                        help=f'Test duration in seconds (default: {DEFAULT_TEST_DURATION})')
    parser.add_argument('--iterations', type=int, default=DEFAULT_ITERATIONS, 
                        help=f'Number of iterations (default: {DEFAULT_ITERATIONS})')
    parser.add_argument('--concurrent', type=int, default=DEFAULT_CONCURRENT_CLIENTS, 
                        help=f'Number of concurrent clients (default: {DEFAULT_CONCURRENT_CLIENTS})')
    parser.add_argument('--output', type=str, default=f'nasdaq_simulation_{datetime.now().strftime("%Y%m%d_%H%M%S")}.json', 
                        help='Output file for results')
    parser.add_argument('--mode', type=str, choices=['sgx', 'sev', 'both'], default='both',
                        help='Test mode: SGX only, SEV only, or both (default: both)')
    return parser.parse_args()

def generate_market_data(batch_size):
    """Generate synthetic NASDAQ market data for testing with proper length-prefixed format"""
    market_data = []
    
    # Create a batch of market data events with realistic trade characteristics
    for i in range(batch_size):
        # Format similar to NASDAQ TotalView-ITCH market data
        symbol = random.choice(["AAPL", "MSFT", "AMZN", "GOOGL", "META", "TSLA", "NVDA", "AMD", "INTC", "CSCO"])
        price = round(random.uniform(50, 2000), 2)  # Stock price $50-$2000
        qty = random.randint(1, 1000)  # Trade quantity 1-1000 shares
        side = random.choice(["BUY", "SELL"])
        venue = random.choice(["NYSE", "NASDAQ", "IEX", "ARCA"])
        timestamp = int(time.time() * 1000)  # Millisecond timestamp
        
        # Format the data as length-prefixed binary data
        event_data = f"{symbol},{price},{qty},{side},{venue},{timestamp}".encode()
        length = len(event_data)
        
        # Length-prefixed format: 4-byte length prefix (little-endian) + data
        # This matches our WebAssembly contract expectations
        length_prefix = struct.pack("<I", length)  # 4-byte little-endian unsigned int
        market_event = length_prefix + event_data
        
        # Convert to array of integers for JSON serialization
        market_data.append([b for b in market_event])
    
    return market_data

def register_nodes_with_coordinator():
    """Register all nodes with the coordinator to ensure proper mesh configuration"""
    print("Registering TEE nodes with coordinator...")
    
    try:
        # Register SGX nodes
        for i, ip in enumerate(SGX_NODES):
            print(f"Registering SGX node {i}: {ip}")
            requests.post(
                f"http://{COORDINATOR_IP}:8080/register",
                json={
                    'id': f'sgx-node-{i}',
                    'address': f'http://{ip}:7070',
                    'region': 'us-east',
                    'node_type': 'SGX',
                    'batch_size': 500,
                    'pair_id': i
                },
                timeout=5
            )
        
        # Register SEV nodes
        for i, ip in enumerate(SEV_NODES):
            print(f"Registering SEV node {i}: {ip}")
            requests.post(
                f"http://{COORDINATOR_IP}:8080/register",
                json={
                    'id': f'sev-node-{i}',
                    'address': f'http://{ip}:7070',
                    'region': 'us-east',
                    'node_type': 'SEV',
                    'batch_size': 100,
                    'pair_id': i
                },
                timeout=5
            )
            
        # Verify registration
        response = requests.get(f"http://{COORDINATOR_IP}:8080/tees", timeout=5)
        if response.status_code == 200:
            nodes = response.json()
            print(f"Successfully registered {len(nodes)} nodes with coordinator")
    except Exception as e:
        print(f"Warning: Error registering nodes with coordinator: {str(e)}")

def configure_nodes_for_benchmark(batch_size):
    """Configure nodes with optimal settings for benchmark"""
    print("Configuring nodes for high-throughput benchmark...")
    
    try:
        # Configure SGX nodes for high throughput
        for ip in SGX_NODES:
            requests.post(
                f"http://{ip}:7070/status",
                json={
                    'batch_size': batch_size,
                    'worker_threads': 8,
                    'mode': 'high-throughput'
                },
                timeout=5
            )
        
        # Configure SEV nodes
        for ip in SEV_NODES:
            requests.post(
                f"http://{ip}:7070/status",
                json={
                    'batch_size': min(batch_size, 100),  # SEV uses smaller batches
                    'worker_threads': 8,
                    'mode': 'secure-verification'
                },
                timeout=5
            )
    except Exception as e:
        print(f"Warning: Error configuring nodes: {str(e)}")

def run_market_simulation(node_ip, batch_size, iterations, is_sgx=True):
    """Run market data simulation on a node with high throughput"""
    node_type = "SGX" if is_sgx else "SEV"
    
    # Configure batch size according to node type - SGX can handle larger batches
    actual_batch_size = batch_size if is_sgx else min(batch_size, 100)  # SEV nodes use smaller batches
    
    print(f"Starting market data simulation on {node_type} node: {node_ip} with batch size {actual_batch_size}")
    
    # Test metrics
    transaction_count = 0
    batch_count = 0
    latencies = []
    errors = 0
    start_time = time.time()
    
    # Create a thread pool for parallel requests within this simulation
    # This mimics the 8-thread parallel execution model of our architecture
    thread_count = 8 if is_sgx else 2  # SGX optimized for parallel processing
    
    # Use a local thread pool to simulate high throughput
    with concurrent.futures.ThreadPoolExecutor(max_workers=thread_count) as local_executor:
        # Submit initial batch of requests
        futures = []
        for _ in range(min(thread_count * 2, iterations)):
            futures.append(local_executor.submit(
                process_batch, node_ip, actual_batch_size, node_type
            ))
        
        completed = 0
        while completed < iterations:
            # As futures complete, submit new ones to maintain parallelism
            for future in concurrent.futures.as_completed(futures):
                result = future.result()
                completed += 1
                
                if result['success']:
                    batch_count += 1
                    transaction_count += actual_batch_size  # Each batch processes actual_batch_size transactions
                    latencies.append(result['latency_ms'])
                else:
                    errors += 1
                
                # Report progress periodically
                if completed % 50 == 0 or completed == 1:
                    elapsed = time.time() - start_time
                    current_tps = transaction_count / elapsed if elapsed > 0 else 0
                    print(f"  {node_type} {node_ip} - Completed {completed}/{iterations} batches, " +
                          f"Current TPS: {current_tps:.2f}, " +
                          f"Transactions: {transaction_count}")
                
                # Submit a new job if we haven't reached the iteration limit
                if completed < iterations:
                    futures.append(local_executor.submit(
                        process_batch, node_ip, actual_batch_size, node_type
                    ))
    
    # Calculate final results
    test_duration = time.time() - start_time
    tps = transaction_count / test_duration if test_duration > 0 else 0
    avg_latency = statistics.mean(latencies) if latencies else 0
    p95_latency = statistics.quantiles(latencies, n=20)[18] if len(latencies) >= 20 else 0
    p99_latency = statistics.quantiles(latencies, n=100)[98] if len(latencies) >= 100 else 0
    
    result = {
        'node_ip': node_ip,
        'node_type': node_type,
        'batch_size': actual_batch_size,
        'transaction_count': transaction_count,
        'batch_count': batch_count,
        'test_duration_sec': test_duration,
        'error_count': errors,
        'tps': tps,
        'avg_latency_ms': avg_latency,
        'p95_latency_ms': p95_latency,
        'p99_latency_ms': p99_latency
    }
    
    print(f"Simulation complete on {node_type} {node_ip}:")
    print(f"  - Transactions: {transaction_count}")
    print(f"  - TPS: {tps:.2f}")
    print(f"  - Avg Latency: {avg_latency:.2f} ms")
    print(f"  - P95 Latency: {p95_latency:.2f} ms")
    print(f"  - Error count: {errors}")
    
    return result

def process_batch(node_ip, batch_size, node_type):
    """Process a single batch of transactions on a node"""
    try:
        # Generate market data batch - full batch with multiple events
        market_data = generate_market_data(batch_size)
        
        # Time the transaction processing
        batch_start = time.time()
        
        # Each event in the batch is correctly length-prefixed
        # In a real scenario, the entire batch would be sent together
        # Here we send all events in a single batch to maximize throughput
        response = requests.post(
            f"http://{node_ip}:7070/execute",
            json={
                'contract_id': 'nasdaq-market-data',
                'function': 'process_batch',
                'parameters': [b for event in market_data for b in event]  # Flatten all events into a single array
            },
            timeout=30  # Longer timeout for large batches
        )
        
        batch_end = time.time()
        batch_latency = (batch_end - batch_start) * 1000  # convert to ms
        
        if response.status_code == 200:
            return {
                'success': True,
                'latency_ms': batch_latency
            }
        else:
            return {
                'success': False,
                'error': f"HTTP {response.status_code}: {response.text}"
            }
            
    except Exception as e:
        return {
            'success': False,
            'error': str(e)
        }
    
    # Calculate results
    tps = transaction_count / (sum(latencies) / 1000) if latencies else 0
    avg_latency = statistics.mean(latencies) if latencies else 0
    p95_latency = statistics.quantiles(latencies, n=20)[18] if len(latencies) >= 20 else 0
    p99_latency = statistics.quantiles(latencies, n=100)[98] if len(latencies) >= 100 else 0
    
    result = {
        'node_ip': node_ip,
        'node_type': node_type,
        'batch_size': batch_size,
        'transaction_count': transaction_count,
        'batch_count': batch_count,
        'error_count': errors,
        'tps': tps,
        'avg_latency_ms': avg_latency,
        'p95_latency_ms': p95_latency,
        'p99_latency_ms': p99_latency
    }
    
    print(f"Simulation complete on {node_type} {node_ip}:")
    print(f"  - TPS: {tps:.2f}")
    print(f"  - Avg Latency: {avg_latency:.2f} ms")
    print(f"  - P95 Latency: {p95_latency:.2f} ms")
    print(f"  - Error count: {errors}")
    
    return result

def main():
    args = get_args()
    
    print(f"=== High-Throughput NASDAQ Market Data Simulation for Dual TEE Mesh ===")
    print(f"Configuration:")
    print(f"  - Batch Size: {args.batch_size}")
    print(f"  - Test Duration: {args.duration} seconds")
    print(f"  - Iterations: {args.iterations}")
    print(f"  - Concurrent Clients: {args.concurrent}")
    print(f"  - Mode: {args.mode}")
    print(f"  - SGX Nodes: {SGX_NODES}")
    print(f"  - SEV Nodes: {SEV_NODES}")
    print(f"=============================================")
    
    # Determine which nodes to test based on mode
    test_nodes = []
    if args.mode in ['sgx', 'both']:
        for ip in SGX_NODES:
            test_nodes.append((ip, True))  # (ip, is_sgx)
    
    if args.mode in ['sev', 'both']:
        for ip in SEV_NODES:
            test_nodes.append((ip, False))  # (ip, is_sgx)
    
    # For high throughput testing, we need a reasonable number of iterations
    # that will give SGX nodes a chance to reach their throughput potential
    # SGX nodes can scale to 5000+ TPS, so we need enough iterations
    iterations = min(args.iterations, 2000)  # Cap at 2000 for reasonable test duration
    
    # Register TEE nodes with coordinator to ensure mesh is properly configured
    register_nodes_with_coordinator()
    
    # Configure nodes with the right batch sizes and thread counts
    configure_nodes_for_benchmark(args.batch_size)
    
    # Run tests in parallel
    results = []
    start_time = time.time()
    
    with concurrent.futures.ThreadPoolExecutor(max_workers=args.concurrent) as executor:
        futures = []
        for node_ip, is_sgx in test_nodes:
            futures.append(
                executor.submit(
                    run_market_simulation,
                    node_ip,
                    args.batch_size,
                    iterations,
                    is_sgx
                )
            )
        
        # Collect results
        for future in concurrent.futures.as_completed(futures):
            result = future.result()
            results.append(result)
    
    total_duration = time.time() - start_time
    
    # Calculate aggregate results
    sgx_results = [r for r in results if r['node_type'] == 'SGX']
    sev_results = [r for r in results if r['node_type'] == 'SEV']
    
    # Create summary
    summary = {
        'timestamp': datetime.now().isoformat(),
        'configuration': {
            'batch_size': args.batch_size,
            'iterations': iterations,
            'concurrent_clients': args.concurrent,
            'mode': args.mode,
            'total_duration_sec': total_duration
        },
        'sgx_aggregate': {
            'node_count': len(sgx_results),
            'total_transactions': sum(r['transaction_count'] for r in sgx_results),
            'total_errors': sum(r['error_count'] for r in sgx_results),
            'avg_tps': statistics.mean(r['tps'] for r in sgx_results) if sgx_results else 0,
            'avg_latency_ms': statistics.mean(r['avg_latency_ms'] for r in sgx_results) if sgx_results else 0
        },
        'sev_aggregate': {
            'node_count': len(sev_results),
            'total_transactions': sum(r['transaction_count'] for r in sev_results),
            'total_errors': sum(r['error_count'] for r in sev_results),
            'avg_tps': statistics.mean(r['tps'] for r in sev_results) if sev_results else 0,
            'avg_latency_ms': statistics.mean(r['avg_latency_ms'] for r in sev_results) if sev_results else 0
        },
        'node_results': results
    }
    
    # Print summary
    print("\n=== Simulation Summary ===")
    print(f"Total Duration: {total_duration:.2f} seconds")
    
    if sgx_results:
        print("\nSGX Nodes:")
        print(f"  - Nodes: {len(sgx_results)}")
        print(f"  - Total Transactions: {summary['sgx_aggregate']['total_transactions']}")
        print(f"  - Avg TPS: {summary['sgx_aggregate']['avg_tps']:.2f}")
        print(f"  - Avg Latency: {summary['sgx_aggregate']['avg_latency_ms']:.2f} ms")
        print(f"  - Total Errors: {summary['sgx_aggregate']['total_errors']}")
    
    if sev_results:
        print("\nSEV Nodes:")
        print(f"  - Nodes: {len(sev_results)}")
        print(f"  - Total Transactions: {summary['sev_aggregate']['total_transactions']}")
        print(f"  - Avg TPS: {summary['sev_aggregate']['avg_tps']:.2f}")
        print(f"  - Avg Latency: {summary['sev_aggregate']['avg_latency_ms']:.2f} ms")
        print(f"  - Total Errors: {summary['sev_aggregate']['total_errors']}")
    
    # Save results to file
    with open(args.output, 'w') as f:
        json.dump(summary, f, indent=2)
    
    print(f"\nResults saved to: {args.output}")
    print("=============================================")

if __name__ == "__main__":
    main()
