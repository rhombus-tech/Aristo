#!/usr/bin/env python3
# Mock TEE endpoint for testing NASDAQ integration
# This simulates the TEE mesh network's API endpoint

import http.server
import socketserver
import json
import logging
import threading
import time
import argparse
from datetime import datetime

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger("MOCK-TEE-Endpoint")

# Storage for received data
received_data = {
    "nasdaq_basic": [],
    "nls_plus": []
}

class MockTEEHandler(http.server.BaseHTTPRequestHandler):
    def _set_response(self, status=200):
        self.send_response(status)
        self.send_header('Content-type', 'application/json')
        self.end_headers()
        
    def do_POST(self):
        content_length = int(self.headers['Content-Length'])
        post_data = self.rfile.read(content_length)
        
        logger.info(f"POST request received on path: {self.path}")
        
        if self.path == '/api/v1/nasdaq-data':
            try:
                data = json.loads(post_data.decode('utf-8'))
                
                # Extract and log relevant information
                data_type = data.get('tee_metadata', {}).get('data_type', 'unknown')
                source_region = data.get('tee_metadata', {}).get('source_region', 'unknown')
                
                logger.info(f"Received {data_type} data from region {source_region}")
                
                # Store the data
                if data_type == 'nasdaq_basic' and len(received_data['nasdaq_basic']) < 100:
                    received_data['nasdaq_basic'].append(data)
                elif data_type == 'nls_plus' and len(received_data['nls_plus']) < 100:
                    received_data['nls_plus'].append(data)
                
                # Simulate cross-regional verification (approximately 50-90ms)
                verification_time = 70  # milliseconds
                time.sleep(verification_time / 1000)
                
                # Send success response
                self._set_response()
                response = {
                    "status": "success",
                    "message": f"Data processed by mock TEE (verification: {verification_time}ms)",
                    "timestamp": datetime.now().isoformat(),
                    "region_id": "us-east"
                }
                self.wfile.write(json.dumps(response).encode('utf-8'))
                
            except json.JSONDecodeError:
                logger.error("Failed to parse request data as JSON")
                self._set_response(400)
                response = {
                    "status": "error",
                    "message": "Invalid JSON data"
                }
                self.wfile.write(json.dumps(response).encode('utf-8'))
        else:
            logger.warning(f"Unrecognized path: {self.path}")
            self._set_response(404)
            response = {
                "status": "error",
                "message": f"Path not found: {self.path}"
            }
            self.wfile.write(json.dumps(response).encode('utf-8'))
    
    def log_message(self, format, *args):
        # Override to prevent duplicate logging
        return

class MockTEEServer:
    def __init__(self, port=8081):
        self.port = port
        self.httpd = None
        self.server_thread = None
        self.running = False
    
    def start(self):
        self.httpd = socketserver.TCPServer(("", self.port), MockTEEHandler)
        self.running = True
        
        logger.info(f"Starting mock TEE endpoint on port {self.port}")
        
        self.server_thread = threading.Thread(target=self.httpd.serve_forever)
        self.server_thread.daemon = True
        self.server_thread.start()
        
        logger.info(f"Mock TEE endpoint is running on port {self.port}")
    
    def stop(self):
        if self.httpd:
            logger.info("Stopping mock TEE endpoint")
            self.running = False
            self.httpd.shutdown()
            self.httpd.server_close()
            self.server_thread.join(timeout=2.0)
            logger.info("Mock TEE endpoint stopped")

def print_stats():
    """Print statistics about received data every 10 seconds"""
    while True:
        nasdaq_basic_count = len(received_data['nasdaq_basic'])
        nls_plus_count = len(received_data['nls_plus'])
        
        logger.info(f"Received data stats - Nasdaq Basic: {nasdaq_basic_count}, NLS Plus: {nls_plus_count}")
        
        if nasdaq_basic_count > 0 or nls_plus_count > 0:
            # Print a sample of the latest data
            if nasdaq_basic_count > 0:
                sample = received_data['nasdaq_basic'][-1]
                logger.info(f"Latest Nasdaq Basic sample: {json.dumps(sample)[:200]}...")
            
            if nls_plus_count > 0:
                sample = received_data['nls_plus'][-1]
                logger.info(f"Latest NLS Plus sample: {json.dumps(sample)[:200]}...")
        
        time.sleep(10)

if __name__ == "__main__":
    # Parse command-line arguments
    parser = argparse.ArgumentParser(description='Mock TEE Endpoint Server')
    parser.add_argument('--port', type=int, default=8081, help='Port to run the server on (default: 8081)')
    args = parser.parse_args()
    
    # Start the mock TEE endpoint
    server = MockTEEServer(port=args.port)
    server.start()
    
    # Start the stats thread
    stats_thread = threading.Thread(target=print_stats)
    stats_thread.daemon = True
    stats_thread.start()
    
    try:
        # Keep the main thread alive
        while True:
            time.sleep(1)
            
    except KeyboardInterrupt:
        logger.info("Shutting down mock TEE endpoint...")
        server.stop()
