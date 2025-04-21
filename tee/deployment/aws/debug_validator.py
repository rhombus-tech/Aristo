#!/usr/bin/env python3
# Validator debug script for dual-format parameter validation

import socket
import struct
import json
import binascii
import time
import logging
import sys

# Configure logging
logging.basicConfig(
    level=logging.DEBUG,
    format='%(asctime)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger("ValidatorDebug")

def create_length_prefixed_param(data: bytes) -> bytes:
    """Create a length-prefixed parameter (4-byte little-endian length + data)."""
    return struct.pack("<I", len(data)) + data

def send_raw_request(host, port, data, debug=True):
    """Send raw request and log binary response for detailed debugging."""
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    sock.settimeout(10)
    
    logger.info(f"Connecting to {host}:{port}")
    
    try:
        sock.connect((host, port))
        logger.info(f"Connected, sending {len(data)} bytes")
        if debug:
            logger.debug(f"Sending raw: {binascii.hexlify(data).decode()}")
        
        sock.sendall(data)
        logger.info("Data sent, waiting for response")
        
        # Read raw response for debugging
        response = b""
        start_time = time.time()
        try:
            while time.time() - start_time < 5:  # 5 second timeout
                try:
                    chunk = sock.recv(4096)
                    if not chunk:
                        break
                    response += chunk
                    logger.info(f"Received chunk: {len(chunk)} bytes")
                except socket.timeout:
                    logger.info("Socket timeout, no more data")
                    break
        except Exception as e:
            logger.error(f"Error receiving data: {e}")
        
        # Analyze what we received
        if not response:
            logger.error("No response received")
            return None
            
        logger.info(f"Received total: {len(response)} bytes")
        logger.debug(f"Raw response: {binascii.hexlify(response).decode()}")
        
        # Try to parse as length-prefixed
        if len(response) >= 4:
            try:
                resp_len = struct.unpack("<I", response[:4])[0]
                logger.info(f"Detected length prefix: {resp_len} bytes")
                
                if len(response) >= resp_len + 4:
                    json_data = response[4:4+resp_len]
                    logger.info(f"Extracted JSON data ({len(json_data)} bytes)")
                    try:
                        parsed = json.loads(json_data.decode())
                        logger.info("Successfully parsed JSON response:")
                        logger.info(json.dumps(parsed, indent=2))
                        return parsed
                    except json.JSONDecodeError as e:
                        logger.error(f"Failed to parse JSON: {e}")
                        logger.debug(f"JSON attempt: {json_data.decode()}")
                else:
                    logger.error(f"Incomplete response: expected {resp_len} bytes after prefix, got {len(response)-4}")
            except struct.error:
                logger.error("Failed to parse length prefix")
        
        # Try to parse as direct JSON
        try:
            parsed = json.loads(response.decode())
            logger.info("Parsed as direct JSON (no length prefix):")
            logger.info(json.dumps(parsed, indent=2))
            return parsed
        except json.JSONDecodeError:
            logger.error("Not valid JSON without prefix either")
            
        return None
        
    finally:
        sock.close()
        logger.info("Connection closed")

def test_length_prefixed():
    """Test length-prefixed parameter format."""
    host = sys.argv[1] if len(sys.argv) > 1 else "54.172.109.130"
    port = int(sys.argv[2]) if len(sys.argv) > 2 else 7090
    
    logger.info(f"Testing length-prefixed format validation on {host}:{port}")
    
    # Create test data with proper length prefix
    test_data = json.dumps({
        "contract_id": "0x" + binascii.hexlify(b"A" * 32).decode(),
        "method": "test_method",
        "parameters": {"key1": "value1", "key2": 12345}
    }).encode()
    
    # Add length prefix
    request_data = create_length_prefixed_param(test_data)
    
    logger.info(f"Sending length-prefixed data: {len(test_data)} bytes")
    logger.debug(f"Length prefix: {struct.unpack('<I', request_data[:4])[0]}")
    
    result = send_raw_request(host, port, request_data)
    if result and result.get("success"):
        logger.info("✅ TEST PASSED")
    else:
        logger.error("❌ TEST FAILED")

def test_direct_format():
    """Test direct parameter format."""
    host = sys.argv[1] if len(sys.argv) > 1 else "54.172.109.130"  
    port = int(sys.argv[2]) if len(sys.argv) > 2 else 7090
    
    logger.info(f"Testing direct format validation on {host}:{port}")
    
    # Create 32-byte test data (typical contract ID)
    test_data = b"B" * 32  # 32-byte direct data
    
    logger.info(f"Sending direct format data: {len(test_data)} bytes")
    logger.debug(f"Data: {binascii.hexlify(test_data).decode()}")
    
    result = send_raw_request(host, port, test_data)
    if result and result.get("success"):
        logger.info("✅ TEST PASSED")
    else:
        logger.error("❌ TEST FAILED")

if __name__ == "__main__":
    logger.info("=== Validator Debug Tool ===")
    logger.info("1. Testing Length-Prefixed Format")
    test_length_prefixed()
    
    logger.info("\n\n2. Testing Direct Format")
    test_direct_format()
