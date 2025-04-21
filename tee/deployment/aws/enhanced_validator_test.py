#!/usr/bin/env python3
# Enhanced test script for dual-format parameter validator with RSA accumulator

import socket
import struct
import json
import binascii
import time
import argparse
import random
import os
import sys
import logging
from typing import Dict, Any, List, Tuple, Optional

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger("ValidatorTest")

def create_length_prefixed_param(data: bytes) -> bytes:
    """Create a length-prefixed parameter (4-byte little-endian length + data)."""
    return struct.pack("<I", len(data)) + data

def create_direct_param(data: bytes) -> bytes:
    """Create a direct format parameter (raw data, no prefix)."""
    return data

def send_request(host: str, port: int, data: bytes) -> Dict[str, Any]:
    """Send request to validator and receive response."""
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    sock.settimeout(10)
    
    try:
        sock.connect((host, port))
        sock.sendall(data)
        
        # Read response (4-byte length + json data)
        resp_len_bytes = sock.recv(4)
        if len(resp_len_bytes) != 4:
            raise Exception("Failed to read response length")
            
        resp_len = struct.unpack("<I", resp_len_bytes)[0]
        resp_data = b""
        
        # Read in chunks
        bytes_remaining = resp_len
        while bytes_remaining > 0:
            chunk = sock.recv(min(4096, bytes_remaining))
            if not chunk:
                break
            resp_data += chunk
            bytes_remaining -= len(chunk)
            
        # Parse JSON response
        if len(resp_data) != resp_len:
            logger.warning(f"Expected {resp_len} bytes, got {len(resp_data)}")
            
        return json.loads(resp_data.decode())
        
    finally:
        sock.close()

def test_length_prefixed_format(host: str, port: int) -> bool:
    """Test length-prefixed parameter format validation."""
    logger.info("Testing length-prefixed format validation")
    
    # Create test data with proper length prefix
    test_data = json.dumps({
        "contract_id": "0x" + binascii.hexlify(os.urandom(32)).decode(),
        "method": "test_method",
        "parameters": {
            "key1": "value1",
            "key2": 12345
        }
    }).encode()
    
    # Add length prefix
    request_data = create_length_prefixed_param(test_data)
    
    # Log the request
    logger.info(f"Sending length-prefixed data: {len(test_data)} bytes")
    logger.debug(f"Length prefix: {struct.unpack('<I', request_data[:4])[0]}")
    
    try:
        response = send_request(host, port, request_data)
        
        if response.get("success"):
            validation = response.get("validation", {})
            if validation.get("format") == "length_prefixed":
                logger.info("Length-prefixed format test: SUCCESS")
                logger.info(f"Validated length: {validation.get('length')} bytes")
                return True
            else:
                logger.error(f"Wrong format detected: {validation.get('format')}")
                return False
        else:
            logger.error(f"Validation failed: {response.get('error')}")
            return False
            
    except Exception as e:
        logger.error(f"Test failed with error: {e}")
        return False

def test_direct_format(host: str, port: int) -> bool:
    """Test direct parameter format validation (32-byte contract ID)."""
    logger.info("Testing direct format validation (contract ID)")
    
    # Create 32-byte contract ID (direct format without length prefix)
    contract_id = os.urandom(32)
    request_data = create_direct_param(contract_id)
    
    # Log the request
    logger.info(f"Sending direct format data: {len(contract_id)} bytes")
    logger.debug(f"Contract ID: {binascii.hexlify(contract_id).decode()}")
    
    try:
        response = send_request(host, port, request_data)
        
        if response.get("success"):
            validation = response.get("validation", {})
            if validation.get("format") == "direct":
                logger.info("Direct format test: SUCCESS")
                logger.info(f"Validated length: {validation.get('length')} bytes")
                return True
            else:
                logger.error(f"Wrong format detected: {validation.get('format')}")
                return False
        else:
            logger.error(f"Validation failed: {response.get('error')}")
            return False
            
    except Exception as e:
        logger.error(f"Test failed with error: {e}")
        return False

def test_overflow_protection(host: str, port: int) -> bool:
    """Test protection against unreasonable parameter lengths."""
    logger.info("Testing overflow protection")
    
    # Create data with unreasonable length
    bad_length = 0x7FFFFFFF  # ~2GB, clearly unreasonable
    request_data = struct.pack("<I", bad_length) + b"abc"  # Just a few bytes of actual data
    
    # Log the request
    logger.info(f"Sending data with unreasonable length: {bad_length}")
    
    try:
        response = send_request(host, port, request_data)
        
        # Should fail validation
        if not response.get("success"):
            logger.info("Overflow protection test: SUCCESS")
            logger.info(f"Error: {response.get('error')}")
            return True
        else:
            logger.error("Validation incorrectly succeeded for unreasonable length")
            return False
            
    except Exception as e:
        # Network error or connection closed is also acceptable
        logger.info(f"Overflow protection test: SUCCESS (connection error: {e})")
        return True

def test_cross_attestation(sgx_host: str, sgx_port: int, sev_host: str, sev_port: int) -> bool:
    """Test cross-attestation between SGX and SEV nodes."""
    if not sgx_host or not sev_host:
        logger.warning("Cross-attestation test skipped - need both SGX and SEV hosts")
        return False
        
    logger.info("Testing cross-attestation between SGX and SEV nodes")
    
    # Create attestation request to SGX node
    attestation_req = json.dumps({
        "request_type": "attestation",
        "target_tee": "SEV",
        "timestamp": int(time.time()),
        "data": binascii.hexlify(os.urandom(32)).decode()
    }).encode()
    
    # Add length prefix
    request_data = create_length_prefixed_param(attestation_req)
    
    # Send to SGX node
    logger.info(f"Sending cross-attestation request to SGX node")
    try:
        sgx_response = send_request(sgx_host, sgx_port, request_data)
        
        if not sgx_response.get("success"):
            logger.error(f"SGX attestation failed: {sgx_response.get('error')}")
            return False
            
        if "attestation" not in sgx_response:
            logger.error("No attestation data in SGX response")
            return False
            
        sgx_attestation = sgx_response.get("attestation", {})
        logger.info(f"SGX attestation received: partner={sgx_attestation.get('partner_type')}")
        
        # Get witness from SGX node
        sgx_witness = sgx_attestation.get("witness", {})
        if not sgx_witness:
            logger.error("No witness in SGX attestation")
            return False
            
        # Now test attestation to SEV node
        sev_attestation_req = json.dumps({
            "request_type": "attestation",
            "target_tee": "SGX",
            "timestamp": int(time.time()),
            "data": binascii.hexlify(os.urandom(32)).decode(),
            "sgx_witness": sgx_witness
        }).encode()
        
        # Add length prefix
        sev_request_data = create_length_prefixed_param(sev_attestation_req)
        
        # Send to SEV node
        logger.info(f"Sending cross-attestation request to SEV node")
        sev_response = send_request(sev_host, sev_port, sev_request_data)
        
        if not sev_response.get("success"):
            logger.error(f"SEV attestation failed: {sev_response.get('error')}")
            return False
            
        if "attestation" not in sev_response:
            logger.error("No attestation data in SEV response")
            return False
            
        sev_attestation = sev_response.get("attestation", {})
        logger.info(f"SEV attestation received: partner={sev_attestation.get('partner_type')}")
        
        # Cross-attestation succeeded
        logger.info("Cross-attestation test: SUCCESS")
        return True
        
    except Exception as e:
        logger.error(f"Cross-attestation test failed with error: {e}")
        return False

def test_rsa_accumulator_performance(host: str, port: int) -> Dict[str, Any]:
    """Test RSA accumulator performance with batch operations."""
    logger.info("Testing RSA accumulator performance")
    
    # Generate batch of random elements
    batch_size = 1000  # Optimal batch size from benchmarks
    batch_elements = []
    total_bytes = 0
    
    for i in range(batch_size):
        # Random size between 32 and 128 bytes
        size = random.randint(32, 128)
        element = os.urandom(size)
        batch_elements.append(element)
        total_bytes += size
    
    # Create batch request
    batch_req = json.dumps({
        "request_type": "batch_accumulate",
        "count": batch_size,
        "total_bytes": total_bytes,
        "elements": [binascii.hexlify(e).decode() for e in batch_elements]
    }).encode()
    
    # Add length prefix
    request_data = create_length_prefixed_param(batch_req)
    
    # Log the request
    logger.info(f"Sending batch of {batch_size} elements ({total_bytes} bytes)")
    
    # Measure time
    start_time = time.time()
    try:
        response = send_request(host, port, request_data)
        duration = time.time() - start_time
        
        logger.info(f"Batch processed in {duration:.4f} seconds")
        logger.info(f"Throughput: {batch_size/duration:.2f} items/sec")
        logger.info(f"Bandwidth: {total_bytes/duration/1024:.2f} KB/sec")
        
        return {
            "success": response.get("success", False),
            "batch_size": batch_size,
            "duration_seconds": duration,
            "throughput_items_per_sec": batch_size/duration,
            "bandwidth_bytes_per_sec": total_bytes/duration
        }
            
    except Exception as e:
        logger.error(f"Performance test failed with error: {e}")
        return {
            "success": False,
            "error": str(e)
        }

def main():
    parser = argparse.ArgumentParser(description='Test TEE Parameter Validator')
    parser.add_argument('--sgx-host', default=None, help='SGX node hostname/IP')
    parser.add_argument('--sgx-port', type=int, default=7070, help='SGX node port')
    parser.add_argument('--sev-host', default=None, help='SEV node hostname/IP')
    parser.add_argument('--sev-port', type=int, default=7070, help='SEV node port')
    parser.add_argument('--log-level', choices=['DEBUG', 'INFO', 'WARNING', 'ERROR'], 
                      default='INFO', help='Logging level')
    parser.add_argument('--test', choices=['all', 'length-prefixed', 'direct', 'overflow', 
                       'cross-attestation', 'performance'], default='all', help='Test to run')
    
    args = parser.parse_args()
    
    # Set log level
    logging.getLogger().setLevel(getattr(logging, args.log_level))
    
    # Determine which host to use for basic tests
    test_host = args.sgx_host if args.sgx_host else args.sev_host
    test_port = args.sgx_port if args.sgx_host else args.sev_port
    
    if not test_host:
        logger.error("No host specified. Please provide --sgx-host or --sev-host")
        sys.exit(1)
    
    # Run the specified test or all tests
    results = {}
    
    if args.test in ['all', 'length-prefixed']:
        results['length_prefixed'] = test_length_prefixed_format(test_host, test_port)
        
    if args.test in ['all', 'direct']:
        results['direct'] = test_direct_format(test_host, test_port)
        
    if args.test in ['all', 'overflow']:
        results['overflow'] = test_overflow_protection(test_host, test_port)
        
    if args.test in ['all', 'cross-attestation'] and args.sgx_host and args.sev_host:
        results['cross_attestation'] = test_cross_attestation(
            args.sgx_host, args.sgx_port, args.sev_host, args.sev_port)
            
    if args.test in ['all', 'performance']:
        results['performance'] = test_rsa_accumulator_performance(test_host, test_port)
    
    # Print summary
    logger.info("\n=== Test Results ===")
    for test, result in results.items():
        if test != 'performance':
            status = "PASSED" if result else "FAILED"
            logger.info(f"{test.replace('_', ' ').title()}: {status}")
        else:
            if isinstance(result, dict) and result.get('success'):
                logger.info(f"Performance: PASSED - {result.get('throughput_items_per_sec', 0):.2f} items/sec")
            else:
                logger.info(f"Performance: FAILED")
    
    # Exit with status code based on results
    # Exclude performance from failure criteria
    non_perf_results = [v for k, v in results.items() if k != 'performance' and isinstance(v, bool)]
    success = all(non_perf_results) if non_perf_results else False
    sys.exit(0 if success else 1)

if __name__ == "__main__":
    main()
