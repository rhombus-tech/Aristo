#!/usr/bin/env python3
import json
import sys

def load_json_file(filename):
    try:
        with open(filename, 'r') as f:
            return json.load(f)
    except Exception as e:
        print(f"Error loading {filename}: {e}")
        return None

def verify_cross_attestation(sgx_file, sev_file):
    sgx_data = load_json_file(sgx_file)
    sev_data = load_json_file(sev_file)
    
    if not sgx_data or not sev_data:
        return False
    
    # Check basic structure
    if len(sgx_data) != len(sev_data):
        print(f"Mismatch in result count: SGX={len(sgx_data)}, SEV={len(sev_data)}")
        return False
    
    # Verify each processed message
    mismatches = 0
    for i, (sgx_msg, sev_msg) in enumerate(zip(sgx_data, sev_data)):
        # Check key fields match (prices, symbols, etc.)
        if 'stock' in sgx_msg and 'stock' in sev_msg:
            if sgx_msg['stock'] != sev_msg['stock']:
                mismatches += 1
                continue
                
        if 'price' in sgx_msg and 'price' in sev_msg:
            if sgx_msg['price'] != sev_msg['price']:
                mismatches += 1
                continue
    
    match_percentage = 100 - (mismatches / len(sgx_data) * 100)
    print(f"Cross-attestation verification: {match_percentage:.2f}% match")
    
    # Consider it successful if at least 95% match
    return match_percentage >= 95.0

if __name__ == "__main__":
    if len(sys.argv) != 3:
        print("Usage: verify_cross_attestation.py <sgx_file> <sev_file>")
        sys.exit(1)
        
    sgx_file = sys.argv[1]
    sev_file = sys.argv[2]
    
    if verify_cross_attestation(sgx_file, sev_file):
        print("Cross-attestation verification PASSED")
        sys.exit(0)
    else:
        print("Cross-attestation verification FAILED")
        sys.exit(1)
