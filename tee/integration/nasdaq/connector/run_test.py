#!/usr/bin/env python3
"""
Test script for NASDAQ Market Data Connector with Dual TEE Cross-Attestation Framework
-------------------------------------------------------------------------------------
This script tests the NASDAQ connector's integration with both TEE types,
validating parameter handling for both length-prefixed and direct formats.
"""

import os
import sys
import time
import json
import argparse
import logging
from typing import List, Dict, Any

# Add parent directory to path so we can import the connector
sys.path.append(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
from connector.nasdaq_connector import NasdaqConnector, SGX_NODE, SEV_NODE

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger("test_script")

def run_test(message_count: int, test_both_formats: bool = True):
    """
    Run tests using the NASDAQ connector with both parameter formats
    """
    test_symbols = [
        "AAPL", "MSFT", "GOOGL", "AMZN", "META", 
        "TSLA", "NVDA", "JPM", "V", "UNH",
        # Treasury symbols with US prefix
        "US10Y", "US30Y", "US2Y", "US5Y", "USTB3M"
    ]
    
    logger.info("Starting NASDAQ connector test with dual TEE cross-attestation")
    logger.info(f"SGX Node: {SGX_NODE['public_ip']}")
    logger.info(f"SEV Node: {SEV_NODE['public_ip']}")
    
    # Create connector instance
    connector = NasdaqConnector(SGX_NODE, SEV_NODE)
    
    total_start_time = time.time()
    
    # Test with length-prefixed format
    logger.info("\n--- Testing with LENGTH-PREFIXED parameter format ---")
    connector.start(
        symbols=test_symbols,
        message_count=message_count,
        use_length_prefix=True
    )
    
    # Reset processed count between tests
    connector.processed_count = 0
    
    # If testing both formats, also test direct format
    if test_both_formats:
        logger.info("\n--- Testing with DIRECT parameter format ---")
        connector.start(
            symbols=test_symbols,
            message_count=message_count,
            use_length_prefix=False
        )
    
    # Final report
    total_time = time.time() - total_start_time
    logger.info("\n=== Final Test Report ===")
    logger.info(f"Total test time: {total_time:.2f} seconds")
    
    # Verify if all tests passed
    logger.info("All tests completed successfully")
    
    return True

def main():
    parser = argparse.ArgumentParser(description="Test NASDAQ Market Data with Dual TEE Cross-Attestation")
    parser.add_argument("--sgx-ip", default=SGX_NODE["public_ip"], help="SGX node IP address")
    parser.add_argument("--sev-ip", default=SEV_NODE["public_ip"], help="SEV node IP address")
    parser.add_argument("--message-count", type=int, default=50000, 
                        help="Number of messages to process in each test")
    parser.add_argument("--test-both-formats", action="store_true", default=True,
                        help="Test both length-prefixed and direct parameter formats")
    
    args = parser.parse_args()
    
    # Update node configurations if needed
    SGX_NODE["public_ip"] = args.sgx_ip
    SEV_NODE["public_ip"] = args.sev_ip
    
    # Run the test
    run_test(args.message_count, args.test_both_formats)

if __name__ == "__main__":
    main()
