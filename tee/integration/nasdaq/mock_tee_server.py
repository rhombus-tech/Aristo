#!/usr/bin/env python3
# Mock TEE server for testing NASDAQ API Consumer
# This simulates a TEE mesh network endpoint for receiving market data

import os
import json
import base64
import logging
import argparse
from http.server import HTTPServer, BaseHTTPRequestHandler
from datetime import datetime

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger("Mock-TEE-Server")

class MockTEEHandler(BaseHTTPRequestHandler):
    """
    Mock TEE mesh network handler that supports both length-prefixed and direct data formats
    based on our dual TEE architecture requirements
    """
    
    def _set_response(self, status_code=200, content_type="application/json"):
        self.send_response(status_code)
        self.send_header('Content-type', content_type)
        self.end_headers()
    
    def do_GET(self):
        """Handle GET requests with a simple status page"""
        self._set_response(200, "text/html")
        self.wfile.write(b"""
        <html>
        <head><title>Mock TEE Server</title></head>
        <body>
        <h1>Mock TEE Server</h1>
        <p>This is a mock TEE mesh network endpoint for testing the NASDAQ API Consumer</p>
        <p>Server status: Running</p>
        <p>Server time: %s</p>
        </body>
        </html>
        """ % datetime.now().isoformat().encode('utf-8'))
    
    def do_POST(self):
        """Handle POST requests - simulating a TEE mesh network endpoint"""
        content_length = int(self.headers['Content-Length'])
        post_data = self.rfile.read(content_length)
        
        # Get headers for logging
        region_id = self.headers.get('X-TEE-Region-ID', 'unknown')
        source = self.headers.get('X-TEE-Source', 'unknown')
        format_type = self.headers.get('X-TEE-Format', 'unknown')
        batch_size = self.headers.get('X-TEE-Batch-Size', '0')
        
        logger.info(f"Received request from {source} in region {region_id} with format {format_type}")
        
        # Try to parse as JSON
        try:
            data = json.loads(post_data)
            
            # Check if this is a batch
            if 'batch' in data and 'batch_metadata' in data:
                batch = data['batch']
                metadata = data['batch_metadata']
                count = len(batch)
                
                logger.info(f"Received batch of {count} messages with metadata: {metadata}")
                
                # Process each item in the batch - in a real TEE, this would verify attestations
                # and execute WebAssembly contracts
                for idx, item in enumerate(batch[:3]):  # Log just first 3 for brevity
                    # Check if binary data is included
                    if 'nasdaq_basic' in item and isinstance(item['nasdaq_basic'], dict) and item['nasdaq_basic'].get('binary', False):
                        binary_len = item['nasdaq_basic'].get('byte_length', 0)
                        logger.info(f"Item {idx}: Binary data with {binary_len} bytes")
                    elif 'nls_plus' in item and isinstance(item['nls_plus'], dict) and item['nls_plus'].get('binary', False):
                        binary_len = item['nls_plus'].get('byte_length', 0)
                        logger.info(f"Item {idx}: Binary data with {binary_len} bytes")
                    else:
                        # Just log the data type
                        for key in item:
                            if key != 'tee_metadata':
                                logger.info(f"Item {idx}: {key} data received")
                
                if count > 3:
                    logger.info(f"... and {count - 3} more items")
            else:
                # Single item
                logger.info(f"Received single message: {str(data)[:200]}...")
            
            # Send successful response - in a real TEE, this would include
            # attestation proofs and execution results
            response = {
                "success": True,
                "timestamp": datetime.now().isoformat(),
                "message": f"Processed request from {source} in region {region_id}",
                "format_used": format_type
            }
            
            self._set_response(200)
            self.wfile.write(json.dumps(response).encode('utf-8'))
            
        except json.JSONDecodeError:
            # If it's not JSON, it might be binary data in direct format
            # In our dual TEE architecture, we handle both formats
            logger.warning(f"Received non-JSON data of {len(post_data)} bytes - treating as direct format")
            
            # Generate hex preview for logging
            hex_preview = post_data[:30].hex()
            logger.info(f"Binary data preview: {hex_preview}...")
            
            # Send response for binary data
            response = {
                "success": True,
                "timestamp": datetime.now().isoformat(),
                "message": f"Processed binary data from {source} in region {region_id}",
                "format_used": "direct",
                "data_length": len(post_data)
            }
            
            self._set_response(200)
            self.wfile.write(json.dumps(response).encode('utf-8'))
        
        except Exception as e:
            # Handle other errors
            logger.error(f"Error processing request: {str(e)}")
            
            response = {
                "success": False,
                "error": str(e),
                "timestamp": datetime.now().isoformat()
            }
            
            self._set_response(500)
            self.wfile.write(json.dumps(response).encode('utf-8'))

def run_server(port=8081, server_class=HTTPServer, handler_class=MockTEEHandler):
    """Run the mock TEE server"""
    server_address = ('', port)
    httpd = server_class(server_address, handler_class)
    logger.info(f"Starting mock TEE server on port {port}")
    logger.info(f"This simulates a TEE mesh network endpoint for NASDAQ data processing")
    logger.info(f"Press Ctrl+C to stop")
    
    try:
        httpd.serve_forever()
    except KeyboardInterrupt:
        logger.info("Keyboard interrupt received, shutting down")
    finally:
        httpd.server_close()
        logger.info("Server stopped")

def main():
    """Main entry point"""
    parser = argparse.ArgumentParser(description='Mock TEE Server for NASDAQ API Consumer testing')
    parser.add_argument('--port', type=int, default=8081, help='Port to run the server on')
    
    args = parser.parse_args()
    
    run_server(port=args.port)

if __name__ == "__main__":
    main()
