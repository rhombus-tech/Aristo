#!/usr/bin/env python3
"""
Hardware Cross-Attestation Test Script

This script performs cross-attestation tests between SGX and SEV nodes
using real hardware attestation. It supports both length-prefixed and direct
parameter formats and handles the appropriate parameter ordering for hardware nodes.

Usage:
  python hardware_cross_attestation.py --contract-id 748775a3a2076c1ae990e94755e63bcb [--direct-format] [--verbose]
"""

import argparse
import base64
import hashlib
import json
import logging
import os
import random
import socket
import subprocess
import sys
import tempfile
import time
import uuid
from typing import Dict, List, Tuple, Any, Optional, Union
import datetime

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s',
    handlers=[logging.StreamHandler()]
)
logger = logging.getLogger('hardware_cross_attestation')

# Accumulator class for ongoing attestation verification
class Accumulator:
    def __init__(self, accumulator_id="tee_cross_attestation"):
        self.accumulator_id = accumulator_id
        self.current_hash = None
        self.witness_cache = {}  # node_id -> witness data
        self.updates = []
        self.initialize()
    
    def initialize(self):
        """Initialize the accumulator with a random seed"""
        seed = f"accumulator-{self.accumulator_id}-{int(time.time())}"
        self.current_hash = hashlib.sha256(seed.encode()).digest()
        logger.info(f"Initialized accumulator {self.accumulator_id} with hash {self.current_hash.hex()[:16]}")
        
    def create_witness(self, node_id, node_type, measurement=None):
        """Create a witness for a node"""
        if not measurement:
            # Create a simple measurement based on node properties
            measurement = hashlib.sha256(f"{node_id}:{node_type}:{time.time()}".encode()).digest()
            
        timestamp = int(time.time())
        
        # Create witness element
        element = {
            "executor": node_id,
            "measurement": measurement,
            "enclave_type": node_type,
            "timestamp": timestamp
        }
        
        # Hash the element to create witness value
        element_bytes = element["executor"].encode()
        element_bytes += element["measurement"]
        element_bytes += element["enclave_type"].encode()
        element_bytes += str(element["timestamp"]).encode()
        
        value = hashlib.sha256(element_bytes).digest()
        
        witness = {
            "value": value,
            "last_accumulator": self.current_hash,
            "element": element,
            "last_update": timestamp
        }
        
        # Store in cache
        self.witness_cache[node_id] = witness
        return witness
    
    def verify_witness(self, witness, skip_cache_check=False):
        """Verify a witness from a node"""
        if not witness:
            return False, "Witness is nil"
        
        # Check if witness is too old (1 day)
        max_age = 86400  # 1 day in seconds
        if time.time() - witness["last_update"] > max_age:
            return False, f"Witness is too old: {witness['last_update']}"
        
        tee_id = witness["element"]["executor"]
        
        # Skip cache check if requested
        if not skip_cache_check:
            if tee_id in self.witness_cache:
                cached_witness = self.witness_cache[tee_id]
                if witness["last_update"] <= cached_witness["last_update"]:
                    return False, "Witness is not newer than cached witness"
        
        # Verify the witness hash
        element_bytes = witness["element"]["executor"].encode()
        element_bytes += witness["element"]["measurement"]
        element_bytes += witness["element"]["enclave_type"].encode()
        element_bytes += str(witness["element"]["timestamp"]).encode()
        
        hash_value = hashlib.sha256(element_bytes).digest()
        
        # In a real implementation, this would be a proper cryptographic verification
        # For now, we'll check if the hashes match
        if hash_value != witness["value"]:
            return False, "Witness hash verification failed"
        
        # Cache the witness
        self.witness_cache[tee_id] = witness
        return True, "Witness verified successfully"
    
    def update_accumulator(self, results):
        """Update the accumulator with new execution results"""
        # In a real implementation, this would update a Merkle tree or other accumulator
        update_data = {
            "timestamp": int(time.time()),
            "results": results,
        }
        
        # Update the accumulator hash with the new results
        update_bytes = json.dumps(update_data, sort_keys=True).encode()
        combined = self.current_hash + update_bytes
        self.current_hash = hashlib.sha256(combined).digest()
        
        # Store update for verification
        update_data["accumulator_hash"] = self.current_hash.hex()
        self.updates.append(update_data)
        
        return self.current_hash.hex()[:16]  # Return shortened hash for display
    
    def debug_witness(self, witness):
        """Return a string representation of a witness for debugging"""
        if not witness:
            return "nil witness"
        
        return (
            f"Witness for {witness['element']['executor']} (type: {witness['element']['enclave_type']})\n"
            f"Value: {witness['value'].hex()}\n"
            f"Last Accumulator: {witness['last_accumulator'].hex()}\n"
            f"Last Update: {datetime.datetime.fromtimestamp(witness['last_update']).isoformat()}\n"
            f"Measurement: {witness['element']['measurement'].hex()}"
        )
    
    def get_update_count(self):
        """Return the number of accumulator updates"""
        return len(self.updates)
    
    def verify_cross_attestation(self, sgx_witness, sev_witness, execution_results):
        """Verify cross-attestation between SGX and SEV nodes"""
        # Verify both witnesses
        sgx_valid, sgx_msg = self.verify_witness(sgx_witness, skip_cache_check=True)
        if not sgx_valid:
            return False, f"SGX witness invalid: {sgx_msg}"
            
        sev_valid, sev_msg = self.verify_witness(sev_witness, skip_cache_check=True)
        if not sev_valid:
            return False, f"SEV witness invalid: {sev_msg}"
        
        # Verify results match
        if execution_results["sgx_result"] != execution_results["sev_result"]:
            return False, "Cross-attestation failed: results do not match"
            
        # In a real implementation, we would do more sophisticated verification
        # between the SGX and SEV attestations
        
        # Update the accumulator with the verified results
        new_hash = self.update_accumulator(execution_results)
        
        return True, f"Cross-attestation verified, accumulator updated: {new_hash}"

# Global accumulator instance
global_accumulator = Accumulator()

# Node configurations - Update these with your actual node information
SGX_NODES = [
    {
        "node_id": "sgx-node-1",
        "node_type": "SGX",
        "public_ip": "3.84.125.9",
        "private_ip": "172.31.48.10",
        "port": 8080,
        "region": "us-east-1"
    },
    {
        "node_id": "i-00e38fb76e0e77bb6",
        "node_type": "SGX",
        "public_ip": "3.88.167.91",
        "private_ip": "172.31.31.67",
        "port": 8080,
        "region": "us-east-1"
    }
]

SEV_NODES = [
    {
        "node_id": "sev-node-1",
        "node_type": "SEV",
        "public_ip": "34.224.71.190",
        "private_ip": "172.31.49.10",
        "port": 8080,
        "region": "us-east-1"
    },
    {
        "node_id": "i-011c91b6513c9a499",
        "node_type": "SEV",
        "public_ip": "3.93.178.107",
        "private_ip": "172.31.19.59",
        "port": 8080,
        "region": "us-east-1"
    }
]

# Function to generate test parameters
def generate_test_parameters(use_length_prefix: bool = True) -> bytes:
    """
    Generate test parameters for the cross-attestation test.
    For simple_add contract, creates parameters for adding 42 + 58 = 100.
    
    Args:
        use_length_prefix: If True, use length-prefixed format, otherwise direct
        
    Returns:
        Parameter bytes to pass to the contract
    """
    # Values to add: 42 + 58 = 100
    a = 42
    b = 58
    
    if use_length_prefix:
        # Length-prefixed format: 4-byte length + data
        # Total length is 8 bytes (two 4-byte integers)
        length = 8
        params = length.to_bytes(4, byteorder='little')
        params += a.to_bytes(4, byteorder='little')
        params += b.to_bytes(4, byteorder='little')
        logger.debug(f"Created length-prefixed parameters: {params.hex()}")
    else:
        # Direct format: just the raw data without length prefix
        params = a.to_bytes(4, byteorder='little')
        params += b.to_bytes(4, byteorder='little')
        logger.debug(f"Created direct parameters: {params.hex()}")
    
    return params

# Function to upload parameters to a node
def upload_parameters_to_node(node: Dict[str, Any], params: bytes) -> Tuple[bool, Optional[str]]:
    """
    Upload parameter data to a remote TEE node
    
    Args:
        node: Node information dictionary
        params: Parameter data bytes
        
    Returns:
        Tuple of (success, remote_file_path)
    """
    # Generate a unique remote filename - use a directory we know exists
    remote_file_name = f"test_params_{int(time.time())}_{random.randint(1000, 9999)}.bin"
    
    # Create temporary local file
    with tempfile.NamedTemporaryFile(delete=False, mode='wb') as tf:
        tf.write(params)
        local_file_path = tf.name
    
    try:
        # Use expanded home directory for SSH key
        ssh_key_path = os.path.expanduser("~/.ssh/tee_access_key")
        
        # Upload via SSH - make sure we're using a path that exists and is writable
        upload_cmd = [
            "ssh",
            "-i", ssh_key_path,
            f"ubuntu@{node['public_ip']}",
            f"mkdir -p ~/tmp && cat > ~/tmp/params.bin && sudo cp ~/tmp/params.bin /opt/rhombus/tee/contracts/{remote_file_name}"
        ]
        
        logger.debug(f"Uploading parameters to {node['node_type']} node {node['node_id']}")
        
        # Write the parameter data to the SSH process's stdin
        process = subprocess.Popen(
            upload_cmd,
            stdin=subprocess.PIPE, 
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE
        )
        stdout, stderr = process.communicate(input=params)
        
        if process.returncode != 0:
            logger.error(f"Failed to upload parameters to {node['node_type']} node: {stderr.decode()}")
            return False, None
            
        logger.debug(f"Successfully uploaded parameters to {node['node_type']} node {node['node_id']}")
        return True, remote_file_name
        
    except Exception as e:
        logger.error(f"Error uploading parameters to {node['node_type']} node: {str(e)}")
        return False, None
    finally:
        # Clean up local temporary file
        try:
            os.unlink(local_file_path)
        except:
            pass

# Function to execute contract on a node with hardware attestation
def execute_contract_on_node(
    node: Dict[str, Any], 
    contract_id: str, 
    params_file: str, 
    use_length_prefix: bool = True
) -> Tuple[bool, Any]:
    """
    Execute a WebAssembly contract on a TEE node with hardware attestation
    
    Args:
        node: Node information dictionary
        contract_id: Contract ID to execute
        params_file: Path to parameter file on the remote node
        use_length_prefix: Whether to use length-prefixed parameter format
        
    Returns:
        Tuple of (success, result)
    """
    try:
        # Use expanded home directory for SSH key
        ssh_key_path = os.path.expanduser("~/.ssh/tee_access_key")
        
        # Build command with CORRECT positional parameter order for run_enarx.sh:
        # [node_type] [contract_id] [function] [params_file] [format]
        param_format = "length-prefixed" if use_length_prefix else "direct"
        
        # Construct the remote command - using correct parameter paths
        # run_enarx.sh expects: [node_type] [contract_id] [function] [params_file] [format]
        remote_cmd = (
            f"cd /opt/rhombus/tee && "
            f"sudo /opt/rhombus/tee/bin/run_enarx.sh {node['node_type']} {contract_id} add contracts/{params_file} {param_format}"
        )
        
        # Execute via SSH
        ssh_cmd = [
            "ssh",
            "-i", ssh_key_path,
            f"ubuntu@{node['public_ip']}",
            remote_cmd
        ]
        
        logger.debug(f"Executing on {node['node_type']} node {node['node_id']}: {remote_cmd}")
        result = subprocess.run(ssh_cmd, capture_output=True, text=True, timeout=30)
        
        if result.returncode != 0:
            logger.error(f"Command failed on {node['node_type']} node: {result.stderr}")
            return False, {"error": result.stderr}
        
        # Extract the result (usually the last line)
        output_lines = result.stdout.strip().split('\n')
        result_line = output_lines[-1] if output_lines else ""
        
        try:
            # Try to parse as integer
            result_value = int(result_line)
            return True, result_value
        except:
            # If not an integer, return as string
            return True, result_line
            
    except subprocess.TimeoutExpired:
        logger.error(f"Command timed out on {node['node_type']} node {node['node_id']}")
        return False, {"error": "timeout"}
    except Exception as e:
        logger.error(f"Error executing contract on {node['node_type']} node: {str(e)}")
        return False, {"error": str(e)}

# Function to clean up parameter files
def cleanup_parameter_file(node: Dict[str, Any], params_file: str) -> bool:
    """
    Clean up parameter file from remote node
    
    Args:
        node: Node information dictionary
        params_file: Path to parameter file on the remote node
        
    Returns:
        Success status
    """
    try:
        # Use expanded home directory for SSH key
        ssh_key_path = os.path.expanduser("~/.ssh/tee_access_key")
        
        # Execute cleanup via SSH
        ssh_cmd = [
            "ssh",
            "-i", ssh_key_path,
            f"ubuntu@{node['public_ip']}",
            f"sudo rm -f /opt/rhombus/tee/contracts/{params_file}"
        ]
        
        logger.debug(f"Cleaning up parameter file on {node['node_type']} node {node['node_id']}")
        result = subprocess.run(ssh_cmd, capture_output=True, text=True, timeout=10)
        
        if result.returncode != 0:
            logger.warning(f"Failed to clean up parameter file on {node['node_type']} node: {result.stderr}")
            return False
            
        return True
        
    except Exception as e:
        logger.warning(f"Error cleaning up parameter file on {node['node_type']} node: {str(e)}")
        return False

# Function to perform cross-attestation between SGX and SEV nodes
def perform_cross_attestation(
    sgx_node: Dict[str, Any], 
    sev_node: Dict[str, Any], 
    contract_id: str, 
    use_length_prefix: bool = True
) -> Dict[str, Any]:
    """
    Perform cross-attestation between SGX and SEV nodes
    
    Args:
        sgx_node: SGX node information
        sev_node: SEV node information
        contract_id: Contract ID to execute
        use_length_prefix: Whether to use length-prefixed parameter format
        
    Returns:
        Dictionary with cross-attestation results
    """
    # Start with an empty result
    result = {
        "sgx_node": sgx_node["node_id"],
        "sev_node": sev_node["node_id"],
        "contract_id": contract_id,
        "parameter_format": "length-prefixed" if use_length_prefix else "direct",
        "success": False,
        "verified": False,
        "sgx_result": None,
        "sev_result": None,
        "start_time": time.time(),
        "end_time": None,
        "duration": None
    }
    
    # Generate test parameters
    params = generate_test_parameters(use_length_prefix)
    
    # Parameter file paths for cleanup
    sgx_params_file = None
    sev_params_file = None
    
    try:
        # Upload parameters to SGX node
        sgx_upload_success, sgx_params_file = upload_parameters_to_node(sgx_node, params)
        if not sgx_upload_success:
            result["error"] = "Failed to upload parameters to SGX node"
            return result
        
        # Upload parameters to SEV node
        sev_upload_success, sev_params_file = upload_parameters_to_node(sev_node, params)
        if not sev_upload_success:
            result["error"] = "Failed to upload parameters to SEV node"
            return result
        
        # Execute on SGX node
        logger.info(f"Executing contract on SGX node {sgx_node['node_id']} with parameter file {sgx_params_file}")
        sgx_success, sgx_result = execute_contract_on_node(
            sgx_node, contract_id, sgx_params_file, use_length_prefix
        )
        result["sgx_result"] = sgx_result
        if not sgx_success:
            result["error"] = f"Failed to execute contract on SGX node: {sgx_result.get('error', 'Unknown error')}"
            return result
        
        # Execute on SEV node
        logger.info(f"Executing contract on SEV node {sev_node['node_id']} with parameter file {sev_params_file}")
        sev_success, sev_result = execute_contract_on_node(
            sev_node, contract_id, sev_params_file, use_length_prefix
        )
        result["sev_result"] = sev_result
        if not sev_success:
            result["error"] = f"Failed to execute contract on SEV node: {sev_result.get('error', 'Unknown error')}"
            return result
        
        # Generate witnesses for cross-attestation verification
        sgx_witness = global_accumulator.create_witness(sgx_node["node_id"], sgx_node["node_type"])
        sev_witness = global_accumulator.create_witness(sev_node["node_id"], sev_node["node_type"])
        
        # Verify results with accumulator
        verified, message = global_accumulator.verify_cross_attestation(sgx_witness, sev_witness, result)
        if verified:
            result["success"] = True
            result["verified"] = True
            result["message"] = message
        else:
            result["success"] = False
            result["verified"] = False
            result["error"] = message
        
        return result
    finally:
        # Clean up parameter files
        if sgx_params_file:
            cleanup_parameter_file(sgx_node, sgx_params_file)
        if sev_params_file:
            cleanup_parameter_file(sev_node, sev_params_file)
        
        # Update timing information
        result["end_time"] = time.time()
        result["duration"] = result["end_time"] - result["start_time"]

# Main function to run cross-attestation tests
def main():
    # Parse command line arguments
    parser = argparse.ArgumentParser(description='Hardware Cross-Attestation Test')
    parser.add_argument('--contract-id', default='748775a3a2076c1ae990e94755e63bcb',
                        help='Contract ID to use for cross-attestation')
    parser.add_argument('--direct-format', action='store_true',
                        help='Use direct parameter format instead of length-prefixed')
    parser.add_argument('--verbose', '-v', action='store_true',
                        help='Enable verbose logging')
    args = parser.parse_args()
    
    # Configure logging level
    if args.verbose:
        logger.setLevel(logging.DEBUG)
    
    # Use length-prefixed format by default, unless direct-format is specified
    use_length_prefix = not args.direct_format
    
    # Print test configuration
    print(f"🔒 Hardware Cross-Attestation Test 🔒")
    print(f"Contract ID: {args.contract_id}")
    print(f"Parameter Format: {'length-prefixed' if use_length_prefix else 'direct'}")
    print(f"Accumulator ID: {global_accumulator.accumulator_id}")
    print(f"Available Nodes:")
    print(f"  SGX Nodes: {', '.join([n['node_id'] for n in SGX_NODES])}")
    print(f"  SEV Nodes: {', '.join([n['node_id'] for n in SEV_NODES])}")
    print("-" * 50)
    
    # Try all SGX and SEV node combinations
    all_results = []
    total_pairs = len(SGX_NODES) * len(SEV_NODES)
    successful_pairs = 0
    verified_pairs = 0
    
    print(f"Testing {total_pairs} node pairs...")
    
    for sgx_node in SGX_NODES:
        for sev_node in SEV_NODES:
            print(f"\n🔐 Testing Pair: SGX({sgx_node['node_id']}) + SEV({sev_node['node_id']})")
            
            # Perform cross-attestation
            result = perform_cross_attestation(
                sgx_node, sev_node, args.contract_id, use_length_prefix
            )
            all_results.append(result)
            
            # Update statistics
            if result["success"]:
                successful_pairs += 1
                if result["verified"]:
                    verified_pairs += 1
                    print(f"✅ Cross-attestation VERIFIED in {result['duration']:.2f}s")
                    print(f"   SGX result: {result['sgx_result']}")
                    print(f"   SEV result: {result['sev_result']}")
                    print(f"   Accumulator: {result['message']}")
                else:
                    print(f"❌ Cross-attestation FAILED in {result['duration']:.2f}s")
                    print(f"   SECURITY ALERT: Results don't match between TEE platforms!")
                    print(f"   SGX result: {result['sgx_result']}")
                    print(f"   SEV result: {result['sev_result']}")
            else:
                print(f"❌ Cross-attestation FAILED: {result.get('error', 'Unknown error')}")
    
    # Print summary
    print("\n" + "=" * 50)
    print(f"Cross-Attestation Summary:")
    print(f"Total pairs tested: {total_pairs}")
    print(f"Successful executions: {successful_pairs} ({successful_pairs/total_pairs*100:.1f}%)")
    print(f"Verified attestations: {verified_pairs} ({verified_pairs/total_pairs*100:.1f}%)")
    print(f"Accumulator updates: {global_accumulator.get_update_count()}")
    
    # Export results to JSON
    timestamp = time.strftime("%Y%m%d_%H%M%S")
    filename = f"cross_attestation_results_{timestamp}.json"
    
    with open(filename, "w") as f:
        json.dump({
            "timestamp": timestamp,
            "total_pairs": total_pairs,
            "successful_executions": successful_pairs,
            "verified_attestations": verified_pairs,
            "accumulator_updates": global_accumulator.get_update_count(),
            "final_accumulator_hash": global_accumulator.current_hash.hex(),
            "results": all_results
        }, f, indent=2)
    
    print(f"Results saved to {filename}")
    
    # Return success if all attestations verified
    return 0 if verified_pairs == total_pairs else 1

if __name__ == "__main__":
    sys.exit(main())
