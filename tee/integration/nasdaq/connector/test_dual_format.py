#!/usr/bin/env python3
"""
TEE Dual-Format Parameter Validation Tester
-------------------------------------------
This script tests the dual-format parameter validation functionality
across both SGX (Go) and SEV (Rust) implementations. It verifies both
length-prefixed and direct data formats work properly.

The test:
1. Connects to both SGX and SEV nodes
2. Sends payloads in both formats to each node
3. Verifies correct handling and error conditions
4. Measures performance between formats
"""

import sys
import os
import json
import time
import logging
import socket
import struct
import hashlib
import argparse
import statistics
import random
import subprocess
import concurrent.futures
from typing import Dict, List, Any, Tuple, Optional, Union
import requests

# Add parent directory to path for imports
sys.path.append(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

# Import existing process_payload function from real_tee_perf.py
from real_tee_perf import process_payload, find_nodes_by_type

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger("dual-format-tester")

# Configuration constants
DEFAULT_PORT = 7090
MAX_PAYLOAD_SIZE = 1024 * 1024  # 1MB max payload size
TEST_ITERATIONS = 10
VALIDATOR_TIMEOUT = 30  # seconds

class DualFormatTester:
    def __init__(self, sgx_nodes: List[Dict], sev_nodes: List[Dict], port: int = DEFAULT_PORT):
        """Initialize the tester with SGX and SEV nodes"""
        self.sgx_nodes = sgx_nodes
        self.sev_nodes = sev_nodes
        self.port = port
        
    def test_node(self, node_info: Dict, test_data: bytes, use_length_prefix: bool) -> Dict:
        """Test parameter validation on a single node with either format"""
        start_time = time.time()
        node_type = node_info.get('type', 'unknown')
        node_ip = node_info.get('ip', 'unknown')
        implementation = "Go" if node_type == "SGX" else "Rust"
        
        # Use node-specific port if available, otherwise use default
        node_port = node_info.get('port', self.port)
        
        logger.info(f"Testing {node_type} node ({implementation}) at {node_ip}:{node_port} with "
                    f"{'length-prefixed' if use_length_prefix else 'direct'} format")
        
        # Prepare the payload with appropriate format
        payload = process_payload(test_data, use_length_prefix=use_length_prefix)
        
        try:
            # Try HTTP request first (more reliable)
            format_param = "length-prefixed" if use_length_prefix else "direct"
            url = f"http://{node_ip}:{node_port}/validate?format={format_param}"
            
            response = requests.post(
                url,
                data=payload,
                headers={"Content-Type": "application/octet-stream"},
                timeout=VALIDATOR_TIMEOUT
            )
            
            if response.status_code == 200:
                result = response.json()
                elapsed = time.time() - start_time
                return {
                    "success": True,
                    "node_type": node_type,
                    "implementation": implementation,
                    "format": format_param,
                    "elapsed_ms": round(elapsed * 1000, 2),
                    "result": result
                }
            else:
                return {
                    "success": False,
                    "node_type": node_type,
                    "implementation": implementation,
                    "format": format_param,
                    "error": f"HTTP Error: {response.status_code} - {response.text}"
                }
        except requests.RequestException as e:
            # Fall back to direct socket communication
            logger.warning(f"HTTP request failed, trying socket connection: {str(e)}")
            return self._test_socket_fallback(node_ip, payload, use_length_prefix, node_type, implementation, start_time)
    
    def _test_socket_fallback(self, ip: str, payload: bytes, use_length_prefix: bool, 
                             node_type: str, implementation: str, start_time: float) -> Dict:
        """Socket-based fallback for validator testing"""
        try:
            # Determine port based on node type
            port = self.port + 1 if node_type == "SEV" else self.port
            
            # Create a socket connection
            with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
                s.settimeout(VALIDATOR_TIMEOUT)
                s.connect((ip, port))
                
                # Construct HTTP-like request for compatibility
                format_param = "length-prefixed" if use_length_prefix else "direct"
                port = self.port + 1 if node_type == "SEV" else self.port
                http_request = (
                    f"POST /validate?format={format_param} HTTP/1.1\r\n"
                    f"Host: {ip}:{port}\r\n"
                    f"Content-Length: {len(payload)}\r\n"
                    f"Content-Type: application/octet-stream\r\n"
                    f"\r\n"
                )
                s.sendall(http_request.encode('utf-8') + payload)
                
                # Read response
                response = b""
                while True:
                    chunk = s.recv(4096)
                    if not chunk:
                        break
                    response += chunk
                    # Look for the end of HTTP headers and complete JSON
                    if b"\r\n\r\n" in response and response.endswith(b"}"):
                        break
                
                # Parse HTTP response
                if b"\r\n\r\n" in response:
                    headers, body = response.split(b"\r\n\r\n", 1)
                    try:
                        result = json.loads(body.decode('utf-8'))
                        elapsed = time.time() - start_time
                        return {
                            "success": True,
                            "node_type": node_type,
                            "implementation": implementation,
                            "format": format_param,
                            "elapsed_ms": round(elapsed * 1000, 2),
                            "result": result
                        }
                    except json.JSONDecodeError:
                        return {
                            "success": False,
                            "node_type": node_type,
                            "implementation": implementation,
                            "format": format_param,
                            "error": f"Invalid JSON response: {body.decode('utf-8', errors='replace')}"
                        }
                else:
                    return {
                        "success": False,
                        "node_type": node_type,
                        "implementation": implementation,
                        "format": format_param,
                        "error": f"Invalid HTTP response: {response.decode('utf-8', errors='replace')}"
                    }
        except Exception as e:
            return {
                "success": False,
                "node_type": node_type,
                "implementation": implementation, 
                "format": "length-prefixed" if use_length_prefix else "direct",
                "error": str(e)
            }

    def test_all_nodes(self, test_data: bytes) -> List[Dict]:
        """Test all nodes with both parameter formats"""
        results = []
        
        # Generate test payloads of various sizes
        test_payloads = [
            # Small payload (32 bytes)
            os.urandom(32),
            # Medium payload (1KB)
            os.urandom(1024),
            # Large payload (100KB) 
            os.urandom(102400)
        ]
        
        # Test each node with both formats
        for nodes in [self.sgx_nodes, self.sev_nodes]:
            for node in nodes:
                node_results = []
                
                # Test with both formats
                for use_length_prefix in [True, False]:
                    # Test with different payload sizes
                    for payload in test_payloads:
                        for _ in range(TEST_ITERATIONS):
                            result = self.test_node(node, payload, use_length_prefix)
                            if result["success"]:
                                node_results.append(result)
                
                # Calculate statistics if we have successful results
                if node_results:
                    # Group by format
                    length_prefixed_times = [r["elapsed_ms"] for r in node_results 
                                            if r["format"] == "length-prefixed"]
                    direct_times = [r["elapsed_ms"] for r in node_results 
                                   if r["format"] == "direct"]
                    
                    # Calculate statistics
                    if length_prefixed_times:
                        length_stats = {
                            "min": min(length_prefixed_times),
                            "max": max(length_prefixed_times),
                            "avg": statistics.mean(length_prefixed_times),
                            "median": statistics.median(length_prefixed_times)
                        }
                    else:
                        length_stats = {"error": "No successful length-prefixed tests"}
                    
                    if direct_times:
                        direct_stats = {
                            "min": min(direct_times),
                            "max": max(direct_times),
                            "avg": statistics.mean(direct_times),
                            "median": statistics.median(direct_times)
                        }
                    else:
                        direct_stats = {"error": "No successful direct format tests"}
                    
                    # Add summary to results
                    results.append({
                        "node_type": node.get("type"),
                        "ip": node.get("ip"),
                        "implementation": "Go" if node.get("type") == "SGX" else "Rust",
                        "length_prefixed_stats": length_stats,
                        "direct_format_stats": direct_stats,
                        "raw_results": node_results[:2]  # Just include a couple of raw results as examples
                    })
        
        return results
    
    def run_cross_platform_test(self) -> Dict:
        """Run cross-platform validation test between SGX and SEV nodes"""
        if not self.sgx_nodes or not self.sev_nodes:
            return {
                "success": False,
                "error": "Need at least one SGX and one SEV node for cross-platform testing"
            }
        
        logger.info("Running cross-platform parameter validation test")
        
        # Select one node of each type
        sgx_node = self.sgx_nodes[0]
        sev_node = self.sev_nodes[0]
        
        # Generate test data
        test_data = {
            "test_id": hashlib.md5(os.urandom(16)).hexdigest(),
            "timestamp": time.time(),
            "validation": "cross-platform",
            "payload": [random.randint(0, 255) for _ in range(32)]
        }
        payload = json.dumps(test_data).encode('utf-8')
        
        # Test with both formats on both platforms
        results = {}
        for use_length_prefix in [True, False]:
            format_name = "length-prefixed" if use_length_prefix else "direct"
            logger.info(f"Testing {format_name} format across platforms")
            
            # Test on SGX node
            sgx_result = self.test_node(sgx_node, payload, use_length_prefix)
            
            # Test on SEV node with same data
            sev_result = self.test_node(sev_node, payload, use_length_prefix)
            
            # Compare results
            results[format_name] = {
                "sgx": sgx_result,
                "sev": sev_result,
                "match": self._compare_results(sgx_result, sev_result)
            }
        
        # Summarize results
        return {
            "success": True,
            "cross_platform_validation": results,
            "summary": {
                "length_prefixed_match": results["length-prefixed"]["match"],
                "direct_match": results["direct"]["match"],
                "overall_success": results["length-prefixed"]["match"] and results["direct"]["match"]
            }
        }
    
    def _compare_results(self, result1: Dict, result2: Dict) -> bool:
        """Compare results between two nodes"""
        # If either result failed, they don't match
        if not result1.get("success") or not result2.get("success"):
            return False
        
        # Compare the validation results
        try:
            result1_validated = result1.get("result", {}).get("validation") == "passed"
            result2_validated = result2.get("result", {}).get("validation") == "passed"
            return result1_validated and result2_validated
        except (KeyError, TypeError):
            return False

def load_nodes() -> Tuple[List[Dict], List[Dict]]:
    """Load node information from config files or environment"""
    try:
        # Try to load from multi_tee_pairs.json
        config_path = os.path.join(
            os.path.dirname(os.path.dirname(os.path.dirname(os.path.dirname(
                os.path.abspath(__file__))))), 
            "deployment/aws/multi_tee_pairs.json"
        )
        
        if os.path.exists(config_path):
            with open(config_path, 'r') as f:
                config = json.load(f)
            
            sgx_nodes = []
            sev_nodes = []
            
            for pair in config:
                if 'public_sgx_ip' in pair:
                    sgx_nodes.append({
                        "ip": pair['public_sgx_ip'],
                        "type": "SGX",
                        "pair_id": pair.get('id', 'unknown')
                    })
                if 'public_sev_ip' in pair:
                    sev_nodes.append({
                        "ip": pair['public_sev_ip'],
                        "type": "SEV",
                        "pair_id": pair.get('id', 'unknown')
                    })
            
            if sgx_nodes and sev_nodes:
                return sgx_nodes, sev_nodes
    except Exception as e:
        logger.warning(f"Error loading config from file: {str(e)}")
    
    # Fallback to finding nodes through existing functions
    try:
        sgx_nodes = find_nodes_by_type("SGX")
        sev_nodes = find_nodes_by_type("SEV")
        return sgx_nodes, sev_nodes
    except Exception as e:
        logger.warning(f"Error finding nodes: {str(e)}")
    
    # Last resort - localhost for testing
    logger.warning("No nodes found, using localhost for testing")
    return [{"ip": "localhost", "type": "SGX"}], [{"ip": "localhost", "type": "SEV"}]

def main():
    """Main entry point for the dual-format parameter validation tester"""
    parser = argparse.ArgumentParser(description='Test dual-format parameter validation across TEE platforms')
    parser.add_argument('--port', type=int, default=DEFAULT_PORT, help='Port for validator services')
    parser.add_argument('--sgx-ip', type=str, help='Specific SGX node IP to test')
    parser.add_argument('--sev-ip', type=str, help='Specific SEV node IP to test')
    parser.add_argument('--local', action='store_true', help='Use localhost for testing (SGX=7090, SEV=7091)')
    args = parser.parse_args()
    
    # Load node information
    sgx_nodes, sev_nodes = load_nodes()
    
    # Override with command line args if provided
    if args.sgx_ip:
        sgx_nodes = [{"ip": args.sgx_ip, "type": "SGX"}]
    if args.sev_ip:
        sev_nodes = [{"ip": args.sev_ip, "type": "SEV"}]
    
    # Use localhost for local testing
    if args.local:
        print("Using localhost for testing...")
        sgx_port = args.port
        sev_port = args.port + 1
        sgx_nodes = [{"ip": "localhost", "type": "SGX", "pair_id": "local", "port": sgx_port}]
        sev_nodes = [{"ip": "localhost", "type": "SEV", "pair_id": "local", "port": sev_port}]
        # For testing, run a local sgx and sev validator
        try:
            # Get project root directory
            project_root = os.path.dirname(os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))
            
            # Start a Go validator for SGX testing
            sgx_cmd = f"cd {project_root}/tee/accumulator && ./high_perf_validator.sh {args.port} > /dev/null 2>&1 &"
            subprocess.Popen(sgx_cmd, shell=True)
            print(f"Started local Go validator on port {args.port}")
            
            # Start a Rust validator for SEV testing on a different port
            sev_port = args.port + 1
            sev_cmd = f"cd {project_root}/execution/accumulator && ./rsa_accumulator.sh {sev_port} > /dev/null 2>&1 &"
            subprocess.Popen(sev_cmd, shell=True)
            print(f"Started local Rust validator on port {sev_port}")
            
            # Give validators time to start
            time.sleep(2)
        except Exception as e:
            print(f"Error starting local validators: {e}")
    
    # Initialize tester
    tester = DualFormatTester(sgx_nodes, sev_nodes, port=args.port)
    
    # Run basic tests
    logger.info("Starting dual-format parameter validation tests...")
    test_data = os.urandom(64)  # 64 bytes of random data
    
    # Run tests on all nodes
    results = tester.test_all_nodes(test_data)
    
    # Run cross-platform test
    cross_platform_results = tester.run_cross_platform_test()
    
    # Combine all results
    full_results = {
        "node_tests": results,
        "cross_platform_test": cross_platform_results,
        "timestamp": time.time(),
        "summary": {
            "sgx_nodes_tested": len(sgx_nodes),
            "sev_nodes_tested": len(sev_nodes),
            "all_formats_supported": all(
                "error" not in r.get("length_prefixed_stats", {}) and 
                "error" not in r.get("direct_format_stats", {})
                for r in results
            ),
            "cross_platform_validation": cross_platform_results.get("summary", {}).get("overall_success", False)
        }
    }
    
    # Print results summary
    print("\n====== DUAL-FORMAT PARAMETER VALIDATION TEST RESULTS ======")
    print(f"SGX Nodes Tested: {len(sgx_nodes)}")
    print(f"SEV Nodes Tested: {len(sev_nodes)}")
    print("\nFormat Support Summary:")
    
    for r in results:
        node_type = r.get("node_type")
        impl = r.get("implementation")
        print(f"\n{node_type} Node ({impl}) at {r.get('ip')}:")
        
        # Length-prefixed stats
        lp_stats = r.get("length_prefixed_stats", {})
        if "error" not in lp_stats:
            print(f"  Length-Prefixed Format: ✓ SUPPORTED")
            print(f"    - Avg: {lp_stats.get('avg', 'N/A'):.2f}ms, " 
                  f"Min: {lp_stats.get('min', 'N/A'):.2f}ms, "
                  f"Max: {lp_stats.get('max', 'N/A'):.2f}ms")
        else:
            print(f"  Length-Prefixed Format: ✗ NOT SUPPORTED - {lp_stats.get('error')}")
        
        # Direct format stats
        dir_stats = r.get("direct_format_stats", {})
        if "error" not in dir_stats:
            print(f"  Direct Format: ✓ SUPPORTED")
            print(f"    - Avg: {dir_stats.get('avg', 'N/A'):.2f}ms, "
                  f"Min: {dir_stats.get('min', 'N/A'):.2f}ms, "
                  f"Max: {dir_stats.get('max', 'N/A'):.2f}ms")
        else:
            print(f"  Direct Format: ✗ NOT SUPPORTED - {dir_stats.get('error')}")
    
    # Cross-platform results
    cross_summary = full_results.get("summary", {})
    print("\nCross-Platform Validation:")
    if cross_summary.get("cross_platform_validation"):
        print("  ✓ PASSED - Both parameter formats work consistently across platforms")
    else:
        print("  ✗ FAILED - Parameter validation inconsistent across platforms")
    
    # Overall assessment
    all_formats = cross_summary.get("all_formats_supported", False)
    if all_formats and cross_summary.get("cross_platform_validation"):
        print("\n✅ SUCCESS: Dual-format parameter validation working correctly across platforms")
    else:
        print("\n❌ FAILURE: Issues detected with dual-format parameter validation")
    
    # Save full results to file
    results_file = "dual_format_validation_results.json"
    with open(results_file, 'w') as f:
        json.dump(full_results, f, indent=2)
    print(f"\nDetailed results saved to: {results_file}")

if __name__ == "__main__":
    main()
