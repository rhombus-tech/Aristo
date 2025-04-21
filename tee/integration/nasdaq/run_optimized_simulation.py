#!/usr/bin/env python3
"""
NASDAQ Market Data Simulation with Optimized TEE Accumulator
-----------------------------------------------------------
Simulates high-volume NASDAQ market data flow through the 
optimized TEE accumulator to validate performance targets.
"""

import os
import sys
import time
import json
import argparse
import logging
from typing import Dict, List, Any
import threading
import concurrent.futures

# Add parent directory to path for imports
sys.path.append(os.path.dirname(os.path.abspath(__file__)))

# Import NASDAQ components
from itch_simulator import ITCHMessageGenerator
from connector.optimized_rsa_connector import OptimizedRsaConnector, AccumulatorElement

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger("nasdaq_simulation")

def run_simulation(config: Dict[str, Any]):
    """
    Run the NASDAQ market data simulation
    
    Args:
        config: Simulation configuration
    """
    logger.info(f"Starting NASDAQ market data simulation with configuration:")
    for key, value in config.items():
        logger.info(f"  {key}: {value}")
    
    # Initialize ITCH message generator
    symbols = config.get("symbols", ["AAPL", "MSFT", "AMZN", "GOOG", "FB", "TSLA", "NVDA", "PYPL", "INTC", "AMD"])
    itch_generator = ITCHMessageGenerator(
        symbols=symbols,
        primary_tee_id=f"sgx-node-1",
        secondary_tee_id=f"sev-node-1",
        region_id=config.get("region", "us-east-1")
    )
    
    # Create TEE accumulator connectors (one for each simulated node)
    connectors = []
    for i in range(config.get("num_nodes", 4)):
        tee_type = "SGX" if i % 2 == 0 else "SEV"
        connector = OptimizedRsaConnector(
            node_id=f"nasdaq-node-{i+1}",
            tee_type=tee_type,
            region=f"region-{i//2 + 1}",
            batch_size=config.get("batch_size", 1000),
            batch_timeout_ms=config.get("batch_timeout_ms", 50)
        )
        connectors.append(connector)
        connector.start()
    
    try:
        # Instead of generating all messages at once, we'll use a streaming approach
        # This is more realistic for market data and avoids memory issues
        logger.info(f"Setting up streaming of {config.get('num_messages', 100000)} ITCH messages...")
        
        # Configure batch size for message generation
        streaming_batch_size = 10000  # Process 10,000 messages at a time for higher throughput
        remaining_messages = config.get("num_messages", 100000)
        total_messages = remaining_messages
        
        # Process start time
        start_time = time.time()
        processed_count = 0
        futures_list = []
        
        # Distribute symbols to nodes (sharding)
        symbol_to_node = {}
        for i, symbol in enumerate(symbols):
            node_idx = i % len(connectors)
            symbol_to_node[symbol] = node_idx
        
        # Create processing thread pool
        with concurrent.futures.ThreadPoolExecutor(max_workers=config.get("worker_threads", 8)) as executor:
            batch_num = 0
            
            # Process in batches until all messages are processed
            while remaining_messages > 0:
                batch_num += 1
                batch_size = min(streaming_batch_size, remaining_messages)
                
                # Generate a batch of messages - use more threads for message generation
                # This ensures we can generate messages fast enough to saturate our accumulator
                logger.info(f"Generating message batch {batch_num}: {batch_size} messages...")
                message_batch = itch_generator.generate_message_stream(batch_size)
                
                # Add random message IDs for routing
                for i, msg in enumerate(message_batch):
                    msg["message_id"] = f"batch-{batch_num}-{i}-{int(time.time()*1000000)}"
                
                # Process the batch
                futures = []
                for msg in message_batch:
                        # Get symbol from the ITCH message format
                    symbol = msg.get("stock", "UNKNOWN") 
                    
                    # Use deterministic but evenly distributed routing to nodes
                    # This ensures all nodes get their fair share of messages
                    if isinstance(symbol, str):
                        # Hash the symbol or message ID and distribute across all nodes
                        msg_id = msg.get("message_id", f"{symbol}-{batch_num}-{processed_count}")
                        
                        # Ensure even distribution to all available nodes
                        # This is critical for demonstrating total system capacity
                        node_idx = hash(msg_id) % len(connectors)
                    else:
                        # Fall back to round-robin assignment for even distribution
                        node_idx = processed_count % len(connectors)
                    
                    # Get the appropriate connector
                    connector = connectors[node_idx]
                    
                    # Add message to connector and track the future
                    future = executor.submit(connector.add_market_event, msg)
                    futures.append(future)
                    
                    # Update count
                    processed_count += 1
                
                # Add batch futures to our tracking list
                futures_list.extend(futures)
                
                # Report progress after each batch
                elapsed = time.time() - start_time
                if elapsed > 0:
                    current_tps = processed_count / elapsed
                    progress_pct = (processed_count / total_messages) * 100
                    logger.info(f"Progress: {processed_count}/{total_messages} messages "
                               f"({progress_pct:.1f}%), {current_tps:.2f} TPS")
                
                # Update remaining message count
                remaining_messages -= batch_size
            
            # Wait for all futures to complete
            logger.info(f"All batches generated. Waiting for processing to complete...")
            concurrent.futures.wait(futures_list)
            
        # Wait for final processing
        logger.info("Waiting for final batch processing...")
        time.sleep(2.0)
        
        # Calculate overall performance
        elapsed = time.time() - start_time
        overall_tps = processed_count / elapsed if elapsed > 0 else 0
        
        logger.info(f"Simulation complete: {processed_count} messages in {elapsed:.2f}s")
        logger.info(f"Overall throughput: {overall_tps:.2f} TPS")
        
        # Get stats from each connector
        for i, connector in enumerate(connectors):
            stats = connector.get_stats()
            logger.info(f"Node {i+1} ({connector.tee_type}) stats: "
                       f"{stats['processed_count']} msgs, "
                       f"{stats['transactions_per_second']:.2f} TPS")
        
        # Check if performance target was met
        target_tps = config.get("target_tps", 50000)
        if overall_tps >= target_tps:
            logger.info(f"✅ Performance target achieved: {overall_tps:.2f} TPS ≥ {target_tps} TPS")
        else:
            logger.warning(f"❌ Performance target not met: {overall_tps:.2f} TPS < {target_tps} TPS")
            
        # Return simulation results
        return {
            "success": True,
            "processed_count": processed_count,
            "elapsed_seconds": elapsed,
            "overall_tps": overall_tps,
            "target_met": overall_tps >= target_tps,
            "node_stats": [connector.get_stats() for connector in connectors]
        }
        
    except Exception as e:
        logger.error(f"Error in simulation: {str(e)}")
        return {
            "success": False,
            "error": str(e)
        }
        
    finally:
        # Clean up connectors
        for connector in connectors:
            connector.stop()


def main():
    """Main entry point"""
    parser = argparse.ArgumentParser(description="NASDAQ Market Data Simulation")
    parser.add_argument("--num-messages", type=int, default=100000, 
                        help="Number of messages to generate")
    parser.add_argument("--batch-size", type=int, default=1000,
                        help="Batch size for accumulator")
    parser.add_argument("--num-nodes", type=int, default=4,
                        help="Number of nodes to simulate")
    parser.add_argument("--worker-threads", type=int, default=8,
                        help="Number of worker threads")
    parser.add_argument("--target-tps", type=int, default=50000,
                        help="Target transactions per second")
    parser.add_argument("--output", type=str, default="simulation_results.json",
                        help="Output file for simulation results")
    parser.add_argument("--simulate-realtime", action="store_true",
                        help="Simulate real-time data flow")
    args = parser.parse_args()
    
    # Run simulation with configuration from args
    config = vars(args)
    results = run_simulation(config)
    
    # Save results
    if args.output:
        with open(args.output, 'w') as f:
            json.dump(results, f, indent=2)
            logger.info(f"Results saved to {args.output}")


if __name__ == "__main__":
    main()
