#!/usr/bin/env python3
# Enhanced NASDAQ Market Data Simulator for Dual TEE Mesh Testing
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
from enum import Enum
from typing import List, Dict, Any
import time

# Import our order book implementation
from order_book import MarketSimulator, MessageType

# Default configuration
DEFAULT_BATCH_SIZE = 500  # SGX nodes configured with 500-element batches
DEFAULT_TEST_DURATION = 300  # 5 minutes
DEFAULT_ITERATIONS = 1000
DEFAULT_CONCURRENT_CLIENTS = 8
DEFAULT_VOLATILITY = 0.02  # Default market volatility (0.02 = 2%)

# Our deployed node IPs
SGX_NODES = ["54.226.83.253", "3.80.113.74"]
SEV_NODES = ["54.209.198.69", "52.206.202.214"]
COORDINATOR_IP = "54.226.83.253"

# Market scenarios
class MarketScenario(Enum):
    NORMAL = "normal"           # Regular trading
    OPEN = "open"               # Market open - high volume
    CLOSE = "close"             # Market close - high volume
    VOLATILE = "volatile"       # High volatility
    EARNINGS = "earnings"       # Earnings announcement - very bursty

def get_args():
    parser = argparse.ArgumentParser(description='Enhanced NASDAQ Market Data Simulation for Dual TEE Mesh')
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
    parser.add_argument('--scenario', type=str, choices=[s.value for s in MarketScenario], default=MarketScenario.NORMAL.value,
                        help='Market scenario to simulate (default: normal)')
    parser.add_argument('--volatility', type=float, default=DEFAULT_VOLATILITY,
                        help=f'Market volatility level (default: {DEFAULT_VOLATILITY})')
    return parser.parse_args()

def generate_realistic_market_data(batch_size, scenario, volatility=0.02):
    """Generate realistic NASDAQ-like market data based on scenario"""
    
    # Create a market simulator with order books for each symbol
    simulator = MarketSimulator()
    
    # Adjust message generation based on scenario
    messages = []
    
    if scenario == MarketScenario.NORMAL.value:
        # Normal trading - balanced mix of orders
        messages = simulator.generate_random_orders(batch_size, volatility=volatility)
        
    elif scenario == MarketScenario.OPEN.value:
        # Market open - high volume, more aggressive price discovery
        # Start with some volatility events
        for symbol in random.sample(simulator.symbols, min(3, len(simulator.symbols))):
            messages.extend(simulator.generate_market_volatility(symbol, magnitude=volatility*2))
        
        # Fill the rest with normal orders
        remaining = max(0, batch_size - len(messages))
        if remaining > 0:
            messages.extend(simulator.generate_random_orders(remaining, volatility=volatility*1.5))
            
    elif scenario == MarketScenario.CLOSE.value:
        # Market close - high volume, closing imbalances
        # Add some imbalance orders (mostly one-sided)
        for symbol in random.sample(simulator.symbols, min(5, len(simulator.symbols))):
            book = simulator.books[symbol]
            side = random.choice(["BUY", "SELL"])
            price = book.get_market_price() * (1 + random.normalvariate(0, volatility))
            price = max(0.01, round(price, 2))
            
            # Create multiple orders on the same side (imbalance)
            for _ in range(min(10, batch_size // 5)):
                quantity = int(max(1, random.lognormvariate(4, 1.2)))
                from order_book import Side
                _, new_messages = book.add_order(
                    Side.BUY if side == "BUY" else Side.SELL, 
                    price, 
                    quantity, 
                    "NASDAQ"
                )
                messages.extend(new_messages)
        
        # Fill the rest with normal orders
        remaining = max(0, batch_size - len(messages))
        if remaining > 0:
            messages.extend(simulator.generate_random_orders(remaining, volatility=volatility*1.2))
            
    elif scenario == MarketScenario.VOLATILE.value:
        # Volatile market - larger price swings, more message volume
        # Generate volatility events for several symbols
        for symbol in random.sample(simulator.symbols, min(4, len(simulator.symbols))):
            messages.extend(simulator.generate_market_volatility(symbol, magnitude=volatility*3))
            
        # Fill the rest with higher volatility normal orders
        remaining = max(0, batch_size - len(messages))
        if remaining > 0:
            messages.extend(simulator.generate_random_orders(remaining, volatility=volatility*2))
            
    elif scenario == MarketScenario.EARNINGS.value:
        # Earnings announcement - sudden price jumps on specific symbols
        # Select 1-2 symbols for "earnings surprise"
        for symbol in random.sample(simulator.symbols, min(2, len(simulator.symbols))):
            # Big price jump (up or down)
            direction = 1 if random.random() < 0.5 else -1
            messages.extend(simulator.generate_market_volatility(
                symbol, 
                magnitude=volatility*5 * direction
            ))
            
        # Fill the rest with higher volatility normal orders
        remaining = max(0, batch_size - len(messages))
        if remaining > 0:
            messages.extend(simulator.generate_random_orders(remaining, volatility=volatility*1.5))
    
    # Ensure we have exactly batch_size messages
    if len(messages) > batch_size:
        messages = messages[:batch_size]
    elif len(messages) < batch_size:
        # Pad with normal orders if needed
        messages.extend(simulator.generate_random_orders(batch_size - len(messages)))
    
    # Convert messages to length-prefixed binary format
    binary_messages = []
    for message in messages:
        # Serialize to JSON bytes
        msg_json = json.dumps(message).encode('utf-8')
        
        # Add length prefix (4-byte little-endian unsigned int)
        length = len(msg_json)
        length_prefix = struct.pack("<I", length)
        
        # Convert to list of integers for JSON transmission
        binary_message = list(length_prefix + msg_json)
        binary_messages.append(binary_message)
    
    return binary_messages

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

def process_batch(node_ip, batch_size, node_type, scenario, volatility):
    """Process a single batch of transactions on a node"""
    try:
        # Generate realistic market data for this batch and scenario
        market_data = generate_realistic_market_data(batch_size, scenario, volatility)
        
        # Time the transaction processing
        batch_start = time.time()
        
        # Send market data to TEE for processing
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
                'latency_ms': batch_latency,
                'batch_size': batch_size,
                'response': response.text[:100] if response.text else None  # Truncate long responses
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

def run_market_simulation(node_ip, batch_size, iterations, scenario, volatility, is_sgx=True):
    """Run enhanced market data simulation on a node"""
    node_type = "SGX" if is_sgx else "SEV"
    
    # Configure batch size according to node type - SGX can handle larger batches
    actual_batch_size = batch_size if is_sgx else min(batch_size, 100)  # SEV nodes use smaller batches
    
    print(f"Starting realistic market simulation on {node_type} node: {node_ip}")
    print(f"  - Batch size: {actual_batch_size}")
    print(f"  - Scenario: {scenario}")
    print(f"  - Volatility: {volatility}")
    
    # Test metrics
    transaction_count = 0
    batch_count = 0
    latencies = []
    errors = 0
    start_time = time.time()
    
    # Create a thread pool for parallel requests within this simulation
    thread_count = 8 if is_sgx else 2  # SGX optimized for parallel processing
    
    # Use a local thread pool to simulate high throughput
    with concurrent.futures.ThreadPoolExecutor(max_workers=thread_count) as local_executor:
        # Submit initial batch of requests
        futures = []
        for _ in range(min(thread_count * 2, iterations)):
            futures.append(local_executor.submit(
                process_batch, node_ip, actual_batch_size, node_type, scenario, volatility
            ))
        
        completed = 0
        while completed < iterations:
            # As futures complete, submit new ones to maintain parallelism
            for future in concurrent.futures.as_completed(futures):
                result = future.result()
                completed += 1
                
                if result.get('success'):
                    batch_count += 1
                    transaction_count += actual_batch_size  # Each batch processes actual_batch_size transactions
                    latencies.append(result['latency_ms'])
                else:
                    errors += 1
                    print(f"  Error on {node_type} {node_ip}: {result.get('error', 'Unknown error')}")
                
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
                        process_batch, node_ip, actual_batch_size, node_type, scenario, volatility
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
        'scenario': scenario,
        'volatility': volatility,
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

def main():
    args = get_args()
    
    print(f"=== Enhanced NASDAQ Market Data Simulation for Dual TEE Mesh ===")
    print(f"Configuration:")
    print(f"  - Batch Size: {args.batch_size}")
    print(f"  - Test Duration: {args.duration} seconds")
    print(f"  - Iterations: {args.iterations}")
    print(f"  - Concurrent Clients: {args.concurrent}")
    print(f"  - Mode: {args.mode}")
    print(f"  - Market Scenario: {args.scenario}")
    print(f"  - Volatility: {args.volatility}")
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
    
    # For high throughput testing, use a reasonable number of iterations
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
                    args.scenario,
                    args.volatility,
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
            'scenario': args.scenario,
            'volatility': args.volatility,
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
    print(f"Market Scenario: {args.scenario} (volatility: {args.volatility})")
    
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
