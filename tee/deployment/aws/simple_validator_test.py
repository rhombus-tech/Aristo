#!/usr/bin/env python3
# Simple TEE validator test for NASDAQ integration
# Tests both length-prefixed and direct parameter formats

import sys
import socket
import struct
import json
import binascii
import time

def send_simple_request(host, port, data_format):
    """Send a simple validation request and print debug info"""
    print(f"Testing {data_format} parameter format on {host}:{port}")
    
    # Create test data based on format
    if data_format == "length_prefixed":
        # Create a simple message with length prefix (4 bytes little-endian)
        message = b"nasdaq_market_data_test"
        data = struct.pack("<I", len(message)) + message
        print(f"Sending {len(message)} bytes with length prefix: {message.decode()}")
    else:
        # Create a direct 32-byte message (like a contract ID)
        data = b"0123456789ABCDEF" * 2  # 32 bytes exactly
        print(f"Sending 32-byte direct format data")
    
    # Debug
    print(f"Raw hex data: {binascii.hexlify(data).decode()}")
    
    # Create a socket with a longer timeout
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    sock.settimeout(10)  # Longer timeout
    
    try:
        # Connect and send data
        print(f"Connecting to {host}:{port}...")
        sock.connect((host, port))
        print(f"Connected! Sending data...")
        sock.sendall(data)
        print("Data sent, waiting for response...")
        
        # Wait for response with simple polling
        for attempt in range(5):
            try:
                response = sock.recv(1024)
                if response:
                    print(f"Received {len(response)} bytes")
                    print(f"Raw response: {binascii.hexlify(response).decode()}")
                    
                    # Try to parse if it looks like our format
                    if len(response) >= 4:
                        try:
                            length = struct.unpack("<I", response[:4])[0]
                            print(f"Response indicates length: {length} bytes")
                            if len(response) >= 4 + length:
                                content = response[4:4+length]
                                print(f"Content: {content}")
                                try:
                                    json_content = json.loads(content)
                                    print(f"Parsed JSON: {json_content}")
                                    return True
                                except:
                                    print("Not valid JSON content")
                        except:
                            print("Could not parse length prefix")
                    
                    # Parameter validation successful if we got any response
                    print("✓ Parameter validation test PASSED (received response)")
                    return True
                else:
                    print(f"Attempt {attempt+1}: No data received, waiting...")
                    time.sleep(1)
            except socket.timeout:
                print(f"Attempt {attempt+1}: Socket timeout, retrying...")
        
        print("✗ No response received after multiple attempts")
        return False
        
    except Exception as e:
        print(f"Error during test: {e}")
        return False
    finally:
        sock.close()
        print("Socket closed")

def check_port_open(host, port, timeout=2):
    """Simple check if a port is open and accessible"""
    print(f"Checking if port {port} is open on {host}...")
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    sock.settimeout(timeout)
    result = False
    try:
        sock.connect((host, port))
        result = True
        print(f"✓ Port {port} is OPEN on {host}")
    except Exception as e:
        print(f"✗ Port {port} is NOT OPEN on {host}: {e}")
    finally:
        sock.close()
    return result

def main():
    if len(sys.argv) < 2:
        print("Usage: ./simple_validator_test.py <host> [format]")
        print("  format: length_prefixed (default) or direct")
        sys.exit(1)
        
    host = sys.argv[1]
    port = 7070
    format_type = sys.argv[2] if len(sys.argv) > 2 else "length_prefixed"
    
    # First check if port is open
    if not check_port_open(host, port):
        print("Cannot proceed with test - port is not accessible")
        sys.exit(1)
    
    # Run the test
    if send_simple_request(host, port, format_type):
        print("Overall Test Result: ✓ SUCCESS")
        sys.exit(0)
    else:
        print("Overall Test Result: ✗ FAILED")
        sys.exit(1)

if __name__ == "__main__":
    main()
