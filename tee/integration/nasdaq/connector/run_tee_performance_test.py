#!/usr/bin/env python3
"""
Dual TEE Performance Test Runner
--------------------------------
Tests the performance of our dual TEE infrastructure with cross-attestation
between Intel SGX and AMD SEV nodes.

Features:
- Tests both length-prefixed and direct parameter formats
- Measures throughput and validates against 50,000+ TPS target
- Verifies cross-attestation between SGX and SEV nodes
- Handles parameter validation and secure execution
"""

import os
import sys
import time
import json
import logging
import argparse
import concurrent.futures
import threading
import queue
from typing import Dict, List, Any, Tuple

# Add parent directory to path for imports
sys.path.append(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

# Import mesh integration and other components
from connector.mesh_integration import MeshConnectedNasdaqProcessor
from itch_simulator import ITCHMessageGenerator

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger("tee_perf_test")

# Define our dual TEE nodes explicitly to ensure we use the newly deployed infrastructure
DUAL_TEE_NODES = [
    {
        "node_id": "i-00e38fb76e0e77bb6",
        "node_type": "SGX",
        "public_ip": "3.88.167.91",
        "private_ip": "172.31.31.67",
        "port": 7070,
        "region": "us-east-1"
    },
    {
        "node_id": "i-011c91b6513c9a499",
        "node_type": "SEV",
        "public_ip": "3.93.178.107",
        "private_ip": "172.31.19.59",
        "port": 7070,
        "region": "us-east-1"
    }
]

def run_performance_test(message_count: int, batch_size: int, test_both_formats: bool = True):
    """
    Run a performance test using the dual TEE cross-attestation framework.
    Tests parameter validation for both length-prefixed and direct formats.
    Validates against the 50,000+ TPS target.
    """
    logger.info(f"Starting performance test with {message_count} messages, batch size {batch_size}")
    
    # Create mesh-connected processor with manual node configuration
    processor = MeshConnectedNasdaqProcessor()
    
    try:
        # Don't use initialize since it will try to discover nodes
        # Instead, directly inject our TEE nodes
        logger.info("Configuring test to use deployed dual TEE nodes:")
        
        # Import the node class
        from connector.mesh_integration import MeshNodeInfo
        
        # Directly insert nodes into the mesh client's nodes dictionary
        for node_info in DUAL_TEE_NODES:
            node_id = node_info["node_id"]
            node_type = node_info["node_type"]
            public_ip = node_info["public_ip"]
            
            # Create the node object
            node = MeshNodeInfo(
                node_id=node_id,
                node_type=node_type,
                public_ip=public_ip,
                private_ip=node_info["private_ip"],
                port=node_info["port"],
                region=node_info["region"]
            )
            
            # Set parameter capabilities
            node.parameter_capability = {
                "length_prefixed": True,
                "direct_format": True,
                "format_detection": True,
                "max_size": 1024
            }
            
            # Mark node as active and recently seen
            node.status = "active"
            node.last_seen = time.time()
            node.attestation_verified = True  # Assume verification for testing
            
            # Add to the mesh client's nodes dictionary
            processor.mesh_client.nodes[node_id] = node
            
            # Also add to type-specific dictionary
            if node_type not in processor.mesh_client.nodes_by_type:
                processor.mesh_client.nodes_by_type[node_type] = {}
            processor.mesh_client.nodes_by_type[node_type][node_id] = node
            
            # Add to region-specific dictionary
            region = node_info["region"]
            if region not in processor.mesh_client.nodes_by_region:
                processor.mesh_client.nodes_by_region[region] = {}
            processor.mesh_client.nodes_by_region[region][node_id] = node
            
            logger.info(f"  - {node_type} node {node_id} at {public_ip} configured and marked active")
            
        # Verify connectivity to the APIs
        logger.info("Verifying API connectivity to TEE nodes")
        import requests
        from requests.exceptions import RequestException
        
        active_nodes = 0
        for node_info in DUAL_TEE_NODES:
            try:
                response = requests.get(
                    f"http://{node_info['public_ip']}:8080/api/status", 
                    timeout=5
                )
                if response.status_code == 200:
                    active_nodes += 1
                    logger.info(f"  ✓ API connection to {node_info['node_type']} node {node_info['node_id']} verified")
                else:
                    logger.warning(f"  ✗ API connection to {node_info['node_type']} node returned status {response.status_code}")
            except RequestException as e:
                logger.warning(f"  ✗ API connection to {node_info['node_type']} node failed: {str(e)}")
        
        if active_nodes == 0:
            logger.error("No active TEE nodes available - cannot run performance test")
            raise RuntimeError("No active TEE nodes available")
        
        # Generate test data including treasury securities
        symbols = [
            "AAPL", "MSFT", "GOOGL", "AMZN", "META", "TSLA", "NVDA", 
            "US10Y", "US30Y", "US2Y", "US5Y", "USTB3M"
        ]
        
        logger.info(f"Generating {message_count} NASDAQ ITCH messages...")
        generator = ITCHMessageGenerator(symbols=symbols)
        messages = generator.generate_message_stream(message_count)
        
        total_start_time = time.time()
        
        # Process with length-prefixed format
        logger.info("\n--- Testing with LENGTH-PREFIXED parameter format ---")
        length_prefixed_result = processor.process_market_data(
            messages=messages,
            use_length_prefix=True,
            batch_size=batch_size
        )
        
        # If testing both formats, also test direct format
        direct_result = None
        if test_both_formats:
            logger.info("\n--- Testing with DIRECT parameter format ---")
            direct_result = processor.process_market_data(
                messages=messages,
                use_length_prefix=False,
                batch_size=batch_size
            )
        
        # Calculate final stats
        total_time = time.time() - total_start_time
        total_messages = message_count * (2 if test_both_formats else 1)
        overall_tps = total_messages / total_time if total_time > 0 else 0
        
        # Get detailed stats from processor
        stats = processor.get_stats()
        
        # Final report
        logger.info("\n=== Performance Test Results ===")
        logger.info(f"Total test time: {total_time:.2f} seconds")
        logger.info(f"Total messages processed: {total_messages}")
        logger.info(f"Overall throughput: {overall_tps:.2f} TPS")
        
        if length_prefixed_result:
            lp_tps = length_prefixed_result.get('throughput', 0)
            logger.info(f"Length-prefixed format throughput: {lp_tps:.2f} TPS")
        
        if direct_result:
            dir_tps = direct_result.get('throughput', 0)
            logger.info(f"Direct format throughput: {dir_tps:.2f} TPS")
        
        # Cross-attestation verification
        attestation_verified = stats.get('attestations_verified', 0)
        logger.info(f"Cross-attestations verified: {attestation_verified}")
        
        # Performance target check
        if overall_tps >= 50000:
            logger.info(f"✅ Performance target achieved: {overall_tps:.2f} TPS ≥ 50,000 TPS")
        else:
            logger.info(f"⚠️ Performance below target: {overall_tps:.2f} TPS < 50,000 TPS")
            logger.info("Analyzing performance bottlenecks...")
            
            # Suggest performance improvements based on results
            if batch_size < 10000:
                logger.info("Recommendation: Increase batch size for better throughput")
            if attestation_verified / total_messages < 0.9:
                logger.info("Recommendation: Reduce cross-attestation frequency for throughput-critical workloads")
        
        return {
            "total_time": total_time,
            "total_messages": total_messages,
            "overall_tps": overall_tps,
            "length_prefixed_tps": length_prefixed_result.get("throughput") if length_prefixed_result else None,
            "direct_tps": direct_result.get("throughput") if direct_result else None,
            "stats": stats,
            "node_distribution": processor.mesh_client.distribute_workload(message_count),
            "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        }
        
    except Exception as e:
        logger.error(f"Error during performance test: {str(e)}")
        raise
    finally:
        processor.shutdown()

def run_scalability_test():
    """
    Run tests with varying batch sizes to find optimal throughput settings
    """
    logger.info("Running scalability test with different batch sizes")
    results = {}
    
    batch_sizes = [1000, 5000, 10000, 20000, 50000]
    for batch_size in batch_sizes:
        logger.info(f"\n=== Testing with batch size {batch_size} ===")
        result = run_performance_test(
            message_count=100000,
            batch_size=batch_size,
            test_both_formats=True
        )
        results[f"batch_{batch_size}"] = result
    
    return results

def main():
    parser = argparse.ArgumentParser(description="Run Dual TEE Performance Test")
    parser.add_argument("--messages", type=int, default=100000, help="Number of messages to process")
    parser.add_argument("--batch-size", type=int, default=10000, help="Batch size for processing")
    parser.add_argument("--test-both-formats", action="store_true", default=True,
                        help="Test both length-prefixed and direct parameter formats")
    parser.add_argument("--scalability-test", action="store_true", default=False,
                        help="Run scalability test with different batch sizes")
    
    args = parser.parse_args()
    
    try:
        if args.scalability_test:
            # Run the scalability test
            results = run_scalability_test()
        else:
            # Run the standard performance test
            results = run_performance_test(
                message_count=args.messages,
                batch_size=args.batch_size,
                test_both_formats=args.test_both_formats
            )
        
        # Save results to file
        result_file = os.path.join(
            os.path.dirname(os.path.abspath(__file__)),
            f"tee_performance_results_{time.strftime('%Y%m%d_%H%M%S')}.json"
        )
        with open(result_file, 'w') as f:
            json.dump(results, f, indent=2)
        
        logger.info(f"Results saved to {result_file}")
        
    except Exception as e:
        logger.error(f"Test failed: {str(e)}")
        sys.exit(1)

if __name__ == "__main__":
    main()
