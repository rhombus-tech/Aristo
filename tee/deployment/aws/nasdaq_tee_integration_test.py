#!/usr/bin/env python3
# NASDAQ TEE Integration Test with Dual-format Parameter Validation
# Tests cross-attestation between SGX and SEV nodes and RSA accumulator performance

import os
import sys
import time
import json
import struct
import socket
import binascii
import hashlib
import argparse
import logging
import random
from typing import Dict, Any, List, Tuple, Optional
import threading

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger("NASDAQTest")

class TEEClient:
    """Client for interacting with TEE validator nodes with dual-format parameter validation."""
    
    def __init__(self, host: str, port: int, tee_type: str):
        self.host = host
        self.port = port
        self.tee_type = tee_type
        
    def send_length_prefixed(self, data: Dict[str, Any]) -> Dict[str, Any]:
        """Send data in length-prefixed format (4-byte little-endian length + JSON data)."""
        json_data = json.dumps(data).encode()
        request = struct.pack("<I", len(json_data)) + json_data
        return self._send_request(request)
    
    def send_direct(self, data: bytes) -> Dict[str, Any]:
        """Send data in direct format (raw bytes, typically 32-byte contract ID)."""
        return self._send_request(data)
        
    def _send_request(self, data: bytes) -> Dict[str, Any]:
        """Send request to TEE validator and receive response."""
        sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        sock.settimeout(10)
        
        try:
            sock.connect((self.host, self.port))
            sock.sendall(data)
            
            # Receive raw response data with a longer timeout
            raw_response = b""
            sock.settimeout(10)
            
            # First read 4 bytes for length
            length_bytes = sock.recv(4)
            if len(length_bytes) != 4:
                raise Exception("Failed to read response length")
                
            response_length = struct.unpack("<I", length_bytes)[0]
            logger.debug(f"Expected response length: {response_length} bytes")
            
            # Read the full response body
            bytes_remaining = response_length
            while bytes_remaining > 0:
                chunk = sock.recv(min(4096, bytes_remaining))
                if not chunk:
                    break
                raw_response += chunk
                bytes_remaining -= len(chunk)
                
            # Parse JSON response
            try:
                return json.loads(raw_response.decode())
            except Exception as e:
                logger.error(f"Failed to parse response: {e}")
                logger.debug(f"Raw response: {binascii.hexlify(raw_response[:100]).decode()}")
                return {"success": False, "error": str(e)}
                
        except Exception as e:
            logger.error(f"Request error: {e}")
            return {"success": False, "error": str(e)}
        finally:
            sock.close()

class NASDAQMarketDataSimulator:
    """Simulates NASDAQ ITCH market data messages for TEE testing."""
    
    def __init__(self):
        self.message_types = [
            "system_event", "stock_directory", "stock_trading_action",
            "reg_sho_restriction", "market_participant_position", "mwcb_decline",
            "mwcb_status", "ipo_quoting_period_update", "add_order", "add_order_mpid",
            "execute_order", "execute_order_with_price", "reduce_order", "modify_order",
            "delete_order", "trade_non_cross", "trade_cross", "broken_trade",
            "net_order_imbalance", "retail_price_improvement"
        ]
        
    def generate_message(self, msg_type: Optional[str] = None) -> Dict[str, Any]:
        """Generate a simulated NASDAQ ITCH message."""
        if not msg_type:
            msg_type = random.choice(self.message_types)
            
        # Generate message based on type
        msg = {
            "type": msg_type,
            "timestamp": int(time.time() * 1000000),  # microseconds
            "contract_id": binascii.hexlify(os.urandom(32)).decode(),
        }
        
        # Add type-specific fields
        if msg_type == "add_order":
            msg.update({
                "order_id": random.randint(1, 999999999),
                "side": random.choice(["buy", "sell"]),
                "shares": random.randint(1, 10000),
                "stock": ''.join(random.choices('ABCDEFGHIJKLMNOPQRSTUVWXYZ', k=4)),
                "price": round(random.uniform(1.0, 1000.0), 2)
            })
        elif msg_type == "execute_order":
            msg.update({
                "order_id": random.randint(1, 999999999),
                "executed_shares": random.randint(1, 10000),
                "execution_id": random.randint(1, 999999999)
            })
            
        return msg
        
    def generate_batch(self, count: int) -> List[Dict[str, Any]]:
        """Generate a batch of NASDAQ ITCH messages."""
        return [self.generate_message() for _ in range(count)]
        
    def generate_contract_id(self) -> bytes:
        """Generate a random 32-byte contract ID for direct format testing."""
        return os.urandom(32)

class RSAAccumulatorTest:
    """Tests RSA accumulator performance with batched operations."""
    
    def __init__(self, sgx_client: TEEClient, sev_client: TEEClient):
        self.sgx_client = sgx_client
        self.sev_client = sev_client
        self.simulator = NASDAQMarketDataSimulator()
        
    def test_length_prefixed_format(self) -> bool:
        """Test Length-prefixed parameter format validation."""
        logger.info("Testing length-prefixed parameter format with NASDAQ market data")
        
        # Generate a market data message and send in length-prefixed format
        test_message = self.simulator.generate_message("add_order")
        logger.info(f"Sending length-prefixed market data message: {test_message['type']}")
        
        response = self.sgx_client.send_length_prefixed(test_message)
        if response and response.get("success"):
            validation = response.get("validation", {})
            logger.info(f"Success: validated as {validation.get('format')} format")
            logger.info(f"Length: {validation.get('length')} bytes")
            return True
        else:
            logger.error(f"Failed to validate length-prefixed format: {response.get('error', 'Unknown error')}")
            return False
            
    def test_direct_format(self) -> bool:
        """Test direct parameter format validation (32-byte contract ID)."""
        logger.info("Testing direct format parameter validation with 32-byte contract ID")
        
        # Generate a 32-byte contract ID and send in direct format
        contract_id = self.simulator.generate_contract_id()
        logger.info(f"Sending 32-byte contract ID: {binascii.hexlify(contract_id).decode()}")
        
        response = self.sgx_client.send_direct(contract_id)
        if response and response.get("success"):
            validation = response.get("validation", {})
            logger.info(f"Success: validated as {validation.get('format')} format")
            logger.info(f"Received contract ID: {validation.get('contract_id', '')}")
            return True
        else:
            logger.error(f"Failed to validate direct format: {response.get('error', 'Unknown error')}")
            return False
            
    def test_cross_attestation(self) -> bool:
        """Test cross-attestation between SGX and SEV nodes."""
        logger.info("Testing cross-attestation between SGX and SEV nodes")
        
        attestation_request = {
            "request_type": "attestation",
            "target_tee": "SEV",
            "nonce": binascii.hexlify(os.urandom(16)).decode(),
            "timestamp": int(time.time())
        }
        
        logger.info(f"Sending attestation request from SGX to SEV: {attestation_request}")
        response = self.sgx_client.send_length_prefixed(attestation_request)
        
        if response and response.get("success") and "attestation" in response:
            attestation = response.get("attestation", {})
            logger.info(f"Received attestation from TEE pair")
            for key, value in attestation.items():
                logger.info(f"  {key}: {value[:30]}..." if isinstance(value, str) and len(value) > 30 else f"  {key}: {value}")
            return True
        else:
            logger.error(f"Cross-attestation failed: {response.get('error', 'Unknown error')}")
            return False
            
    def test_accumulator_performance(self, message_count: int, batch_size: int) -> Tuple[bool, float]:
        """Test RSA accumulator performance with batched operations."""
        logger.info(f"Testing RSA accumulator performance with {message_count} messages in batches of {batch_size}")
        
        # Generate messages
        messages = []
        total_size = 0
        
        for _ in range(message_count):
            msg = self.simulator.generate_message()
            messages.append(msg)
            total_size += len(json.dumps(msg).encode())
            
        batches = [messages[i:i+batch_size] for i in range(0, len(messages), batch_size)]
        logger.info(f"Created {len(batches)} batches with total size {total_size/1024:.2f} KB")
        
        # Start timing
        start_time = time.time()
        success_count = 0
        
        for i, batch in enumerate(batches):
            batch_request = {
                "request_type": "accumulator_batch",
                "batch_id": f"batch-{i}",
                "messages": batch
            }
            
            response = self.sgx_client.send_length_prefixed(batch_request)
            if response and response.get("success"):
                success_count += 1
                
        # Calculate performance metrics
        duration = time.time() - start_time
        messages_per_second = message_count / duration if duration > 0 else 0
        
        logger.info(f"Performance test completed in {duration:.2f} seconds")
        logger.info(f"Processed {message_count} messages ({success_count}/{len(batches)} successful batches)")
        logger.info(f"Performance: {messages_per_second:.2f} messages/sec")
        
        return success_count == len(batches), messages_per_second

def main():
    parser = argparse.ArgumentParser(description="NASDAQ TEE Integration Test")
    parser.add_argument("--sgx-host", required=True, help="SGX node hostname/IP")
    parser.add_argument("--sgx-port", type=int, default=7090, help="SGX node port")
    parser.add_argument("--sev-host", required=True, help="SEV node hostname/IP")
    parser.add_argument("--sev-port", type=int, default=7090, help="SEV node port")
    parser.add_argument("--message-count", type=int, default=1000, help="Number of messages for performance test")
    parser.add_argument("--batch-size", type=int, default=100, help="Batch size for accumulator test")
    parser.add_argument("--test", choices=["all", "length-prefixed", "direct", "cross-attestation", "performance"], 
                        default="all", help="Test to run")
    
    args = parser.parse_args()
    
    # Initialize clients
    sgx_client = TEEClient(args.sgx_host, args.sgx_port, "SGX")
    sev_client = TEEClient(args.sev_host, args.sev_port, "SEV")
    
    # Initialize test suite
    test_suite = RSAAccumulatorTest(sgx_client, sev_client)
    
    # Run tests
    test_results = {}
    
    if args.test in ["all", "length-prefixed"]:
        test_results["length_prefixed"] = test_suite.test_length_prefixed_format()
        
    if args.test in ["all", "direct"]:
        test_results["direct"] = test_suite.test_direct_format()
        
    if args.test in ["all", "cross-attestation"]:
        test_results["cross_attestation"] = test_suite.test_cross_attestation()
        
    if args.test in ["all", "performance"]:
        success, tps = test_suite.test_accumulator_performance(args.message_count, args.batch_size)
        test_results["performance"] = {
            "success": success,
            "transactions_per_second": tps
        }
    
    # Print overall results
    logger.info("\n=== Test Results ===")
    for test_name, result in test_results.items():
        if isinstance(result, dict):
            status = "PASSED" if result.get("success") else "FAILED"
            logger.info(f"{test_name.replace('_', ' ').title()}: {status}")
            if "transactions_per_second" in result:
                logger.info(f"  Performance: {result['transactions_per_second']:.2f} TPS")
        else:
            status = "PASSED" if result else "FAILED"
            logger.info(f"{test_name.replace('_', ' ').title()}: {status}")
    
if __name__ == "__main__":
    main()
