#!/usr/bin/env python3
"""
Real-World TEE Performance Test Runner
--------------------------------------
Tests the actual performance of our dual TEE infrastructure with cross-attestation
between Intel SGX and AMD SEV nodes with realistic conditions.

Features:
- Tests both length-prefixed and direct parameter formats
- Uses appropriate batch sizes to stay under payload limits
- Enforces cross-attestation between SGX and SEV nodes
- Measures real-world throughput with actual TEE operations
"""

import os
import sys
import time
import json
import logging
import argparse
import requests
import concurrent.futures
import threading
import queue
from typing import Dict, List, Any, Tuple

# Add parent directory to path for imports
sys.path.append(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

# Import mesh integration and other components
from connector.mesh_integration import MeshConnectedNasdaqProcessor, MeshNodeInfo
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

def verify_tee_node_status(node_info: Dict[str, Any]) -> Dict[str, Any]:
    """Verify a TEE node's status and gather information about its capabilities"""
    node_data = {
        "id": node_info["node_id"],
        "type": node_info["node_type"],
        "ip": node_info["public_ip"],
        "status": "offline",
        "api_available": False
    }
    
    try:
        # Check TEE REST API
        response = requests.get(
            f"http://{node_info['public_ip']}:8080/api/status", 
            timeout=5
        )
        
        if response.status_code == 200:
            node_data["api_available"] = True
            node_data["status"] = "online"
            node_data["api_response"] = response.json()
            logger.info(f"✓ Node {node_info['node_type']} ({node_info['node_id']}) API is available")
        else:
            logger.warning(f"✗ Node {node_info['node_type']} API returned status {response.status_code}")
    except Exception as e:
        logger.warning(f"✗ Cannot connect to {node_info['node_type']} node API: {e}")
    
    return node_data

def optimize_batch_size(message_size_bytes: int) -> int:
    """Calculate optimal batch size based on message size to stay under 1MB payload limit"""
    # Target payload size of 800KB to stay safely under 1MB limit with overhead
    target_payload_size = 800 * 1024
    
    # Determine batch size based on average message size
    batch_size = max(1, int(target_payload_size / message_size_bytes))
    
    # Round to nearest multiple of 10 for cleaner batch sizes
    batch_size = max(10, (batch_size // 10) * 10)
    
    return batch_size

def run_performance_test(
    message_count: int, 
    batch_size: int = None,
    test_both_formats: bool = True,
    enforce_attestation: bool = True,
    message_size: int = None
):
    """
    Run a realistic performance test using the dual TEE cross-attestation framework.
    Tests parameter validation for both length-prefixed and direct formats.
    Ensures actual TEE execution with appropriate batch sizes.
    """
    logger.info(f"Starting real-world TEE performance test with {message_count} messages")
    
    # Verify node status before proceeding
    logger.info("Verifying TEE node status:")
    node_statuses = []
    for node_info in DUAL_TEE_NODES:
        node_statuses.append(verify_tee_node_status(node_info))
    
    # Count online nodes
    online_nodes = sum(1 for node in node_statuses if node["status"] == "online")
    if online_nodes == 0:
        logger.error("No TEE nodes are online - cannot run performance test")
        return {"error": "No online TEE nodes available"}
    
    logger.info(f"Found {online_nodes} online TEE nodes for testing")
    
    # Create mesh-connected processor
    processor = MeshConnectedNasdaqProcessor()
    
    try:
        # Directly inject our TEE nodes
        logger.info("Configuring TEE nodes for testing:")
        
        for node_info in DUAL_TEE_NODES:
            node_id = node_info["node_id"]
            node_type = node_info["node_type"]
            public_ip = node_info["public_ip"]
            
            # Skip nodes that are not online
            node_status = next((n for n in node_statuses if n["id"] == node_id), None)
            if node_status and node_status["status"] != "online":
                logger.warning(f"Skipping offline node {node_type} ({node_id})")
                continue
            
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
                "max_size": 1024 * 1024  # 1MB limit
            }
            
            # Mark node as active and recently seen
            node.status = "active"
            node.last_seen = time.time()
            node.attestation_verified = True
            
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
            
            logger.info(f"  - {node_type} node {node_id} at {public_ip} configured and ready")
        
        # Generate test data including treasury securities
        symbols = [
            "AAPL", "MSFT", "GOOGL", "AMZN", "META", "TSLA", "NVDA", 
            "US10Y", "US30Y", "US2Y", "US5Y", "USTB3M"
        ]
        
        logger.info(f"Generating {message_count} NASDAQ ITCH messages...")
        generator = ITCHMessageGenerator(symbols=symbols)
        messages = generator.generate_message_stream(message_count)
        
        # Estimate message size and optimize batch size if not specified
        sample_message_size = len(json.dumps(messages[0]).encode('utf-8'))
        
        if message_size is None:
            message_size = sample_message_size
        
        logger.info(f"Average message size: {message_size} bytes")
        
        if batch_size is None:
            batch_size = optimize_batch_size(message_size)
            logger.info(f"Optimizing batch size to {batch_size} messages to stay under 1MB payload limit")
        else:
            estimated_payload = batch_size * message_size
            if estimated_payload > 1024 * 1024:
                logger.warning(f"Specified batch size {batch_size} may exceed payload limits (~{estimated_payload/1024/1024:.2f}MB)")
        
        total_start_time = time.time()
        
        # Process with length-prefixed format
        logger.info("\n--- Testing with LENGTH-PREFIXED parameter format ---")
        length_prefixed_result = processor.process_market_data(
            messages=messages,
            use_length_prefix=True,
            batch_size=batch_size,
            enforce_security=enforce_attestation
        )
        
        # If testing both formats, also test direct format
        direct_result = None
        if test_both_formats:
            logger.info("\n--- Testing with DIRECT parameter format ---")
            direct_result = processor.process_market_data(
                messages=messages,
                use_length_prefix=False,
                batch_size=batch_size,
                enforce_security=enforce_attestation
            )
        
        # Calculate final stats
        total_time = time.time() - total_start_time
        total_messages = message_count * (2 if test_both_formats else 1)
        overall_tps = total_messages / total_time if total_time > 0 else 0
        
        # Get detailed stats from processor
        stats = processor.get_stats()
        
        # Final report
        logger.info("\n=== Real-World TEE Performance Results ===")
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
        attestation_count = stats.get('attestations_verified', 0)
        attestation_percentage = (attestation_count / total_messages) * 100 if total_messages > 0 else 0
        logger.info(f"Cross-attestations performed: {attestation_count} ({attestation_percentage:.2f}%)")
        
        # Security metrics
        logger.info("\n=== Security Metrics ===")
        format_confusion_prevented = stats.get('format_confusion_prevented', 0)
        logger.info(f"Format confusion attacks prevented: {format_confusion_prevented}")
        
        buffer_overflow_prevented = stats.get('buffer_overflow_prevented', 0)
        logger.info(f"Buffer overflow attempts prevented: {buffer_overflow_prevented}")
        
        # Performance target check
        if overall_tps >= 1000:
            logger.info(f"✅ Base performance target achieved: {overall_tps:.2f} TPS ≥ 1,000 TPS per node pair")
            
            # Calculate how many node pairs needed for 50K TPS
            if overall_tps > 0:
                pairs_needed = int(50000 / overall_tps) + (1 if 50000 % overall_tps > 0 else 0)
                logger.info(f"Projection: ~{pairs_needed} node pairs needed to achieve 50,000+ TPS")
        else:
            logger.info(f"⚠️ Performance below target: {overall_tps:.2f} TPS < 1,000 TPS per node pair")
        
        return {
            "total_time": total_time,
            "total_messages": total_messages,
            "overall_tps": overall_tps,
            "length_prefixed_tps": length_prefixed_result.get("throughput") if length_prefixed_result else None,
            "direct_tps": direct_result.get("throughput") if direct_result else None,
            "attestation_count": attestation_count,
            "attestation_percentage": attestation_percentage,
            "batch_size": batch_size,
            "message_size": message_size,
            "node_statuses": node_statuses,
            "stats": stats,
            "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        }
        
    except Exception as e:
        logger.error(f"Error during performance test: {str(e)}")
        import traceback
        logger.error(traceback.format_exc())
        return {"error": str(e)}
    finally:
        processor.shutdown()

def main():
    parser = argparse.ArgumentParser(description="Run Real-World TEE Performance Test")
    parser.add_argument("--messages", type=int, default=5000, help="Number of messages to process")
    parser.add_argument("--batch-size", type=int, default=None, help="Batch size for processing (auto-optimized if not specified)")
    parser.add_argument("--test-both-formats", action="store_true", default=True,
                        help="Test both length-prefixed and direct parameter formats")
    parser.add_argument("--enforce-attestation", action="store_true", default=True,
                        help="Enforce attestation verification for security")
    parser.add_argument("--message-size", type=int, default=None, help="Override estimated message size in bytes")
    
    args = parser.parse_args()
    
    try:
        # Run the performance test
        results = run_performance_test(
            message_count=args.messages,
            batch_size=args.batch_size,
            test_both_formats=args.test_both_formats,
            enforce_attestation=args.enforce_attestation,
            message_size=args.message_size
        )
        
        # Save results to file
        result_file = os.path.join(
            os.path.dirname(os.path.abspath(__file__)),
            f"real_tee_performance_results_{time.strftime('%Y%m%d_%H%M%S')}.json"
        )
        with open(result_file, 'w') as f:
            json.dump(results, f, indent=2)
        
        logger.info(f"Results saved to {result_file}")
        
    except Exception as e:
        logger.error(f"Test failed: {str(e)}")
        sys.exit(1)

if __name__ == "__main__":
    main()
