#!/usr/bin/env python3
"""
Integrated Test Runner for NASDAQ Market Data with Mesh Network
--------------------------------------------------------------
Demonstrates the complete integration between the NASDAQ market data connector
and the dual TEE cross-attestation framework using the mesh network.

Features:
- Tests both length-prefixed and direct parameter formats
- Verifies cross-attestation between SGX and SEV nodes
- Measures throughput for performance benchmarking
- Implements optimized batch processing for higher performance
"""

import os
import sys
import time
import json
import logging
import argparse
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
logger = logging.getLogger("tee_mesh_test")

def run_performance_test(message_count: int, batch_size: int, test_both_formats: bool = True):
    """
    Run a performance test using the mesh network with the dual TEE cross-attestation framework.
    Tests parameter validation for both length-prefixed and direct formats.
    """
    logger.info(f"Starting performance test with {message_count} messages, batch size {batch_size}")
    
    # Create mesh-connected processor
    processor = MeshConnectedNasdaqProcessor()
    
    try:
        # Initialize the processor and discover nodes
        nodes = processor.initialize()
        logger.info(f"Discovered {len(nodes)} TEE nodes in mesh network:")
        
        for node in nodes:
            logger.info(f"  - {node.node_type} node {node.node_id} at {node.public_ip}")
        
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
            logger.info(f"Length-prefixed format throughput: {length_prefixed_result['throughput']:.2f} TPS")
        
        if direct_result:
            logger.info(f"Direct format throughput: {direct_result['throughput']:.2f} TPS")
        
        # Performance target check
        if overall_tps >= 50000:
            logger.info(f"✅ Performance target achieved: {overall_tps:.2f} TPS ≥ 50,000 TPS")
        else:
            logger.info(f"⚠️ Performance below target: {overall_tps:.2f} TPS < 50,000 TPS")
            logger.info("Consider increasing batch size or optimizing node performance")
        
        return {
            "total_time": total_time,
            "total_messages": total_messages,
            "overall_tps": overall_tps,
            "length_prefixed_tps": length_prefixed_result.get("throughput") if length_prefixed_result else None,
            "direct_tps": direct_result.get("throughput") if direct_result else None,
            "stats": stats
        }
        
    except Exception as e:
        logger.error(f"Error during performance test: {str(e)}")
        raise
    finally:
        processor.shutdown()

def main():
    parser = argparse.ArgumentParser(description="Run NASDAQ Market Data Mesh Network Test")
    parser.add_argument("--messages", type=int, default=100000, help="Number of messages to process")
    parser.add_argument("--batch-size", type=int, default=5000, help="Batch size for processing")
    parser.add_argument("--test-both-formats", action="store_true", default=True,
                        help="Test both length-prefixed and direct parameter formats")
    
    args = parser.parse_args()
    
    try:
        # Run the performance test
        results = run_performance_test(
            message_count=args.messages,
            batch_size=args.batch_size,
            test_both_formats=args.test_both_formats
        )
        
        # Save results to file
        result_file = os.path.join(
            os.path.dirname(os.path.abspath(__file__)),
            "performance_results.json"
        )
        with open(result_file, 'w') as f:
            json.dump(results, f, indent=2)
        
        logger.info(f"Results saved to {result_file}")
        
    except Exception as e:
        logger.error(f"Test failed: {str(e)}")
        sys.exit(1)

if __name__ == "__main__":
    main()
