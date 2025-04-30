#!/usr/bin/env python3
"""
Verify that all NASDAQ configuration files have the correct consumer group ID
"""

import os
import sys
import re

# The expected group ID from NASDAQ
EXPECTED_GROUP_ID = "rhombustechnologies-tal-zisckindt"

def check_file(filename):
    """Check if a file contains the expected group ID"""
    print(f"Checking {filename}...")
    
    try:
        with open(filename, 'r') as f:
            content = f.read()
            
        # Look for the group ID in various formats
        patterns = [
            # Direct configuration assignment
            f"'group.id': '{EXPECTED_GROUP_ID}'",
            f"group_id = '{EXPECTED_GROUP_ID}'",
            f"group_id = \"{EXPECTED_GROUP_ID}\"",
            f"'group.id': \"{EXPECTED_GROUP_ID}\"",
            
            # As part of an array
            f"f\"{EXPECTED_GROUP_ID}\"",   # f"rhombustechnologies-tal-zisckindt"
            f"\"{EXPECTED_GROUP_ID}\"",     # "rhombustechnologies-tal-zisckindt"
            f"'{EXPECTED_GROUP_ID}'",        # 'rhombustechnologies-tal-zisckindt'
        ]
        
        found = False
        for pattern in patterns:
            if pattern in content:
                print(f"  ✅ Found correct group ID: {pattern}")
                found = True
                break
                
        if not found:
            print(f"  ❌ Did NOT find correct group ID")
            return False
            
        return True
        
    except Exception as e:
        print(f"  ❌ Error checking file: {str(e)}")
        return False

def main():
    """Check all relevant NASDAQ integration files"""
    base_dir = os.path.dirname(os.path.abspath(__file__))
    
    files_to_check = [
        os.path.join(base_dir, "test_basic_consumer.py"),
        os.path.join(base_dir, "fixed_basic_consumer.py"),
        os.path.join(base_dir, "nasdaq_api_consumer.py"),
        os.path.join(base_dir, "nasdaq_kafka_service.py")
    ]
    
    all_good = True
    for file in files_to_check:
        if not check_file(file):
            all_good = False
    
    if all_good:
        print("\n✅ All files have the correct NASDAQ group ID configuration")
        print(f"Group ID: {EXPECTED_GROUP_ID}")
    else:
        print("\n❌ Some files may not have the correct NASDAQ group ID configuration")
        print("Please review the output above for details")
    
if __name__ == "__main__":
    main()
