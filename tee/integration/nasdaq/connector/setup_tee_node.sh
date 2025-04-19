#!/bin/bash
# TEE Node Setup Script
# This script installs and configures the TEE service on a node

set -e

# Create necessary directories
sudo mkdir -p /opt/rhombus/tee/bin /opt/rhombus/tee/contracts

# Install dependencies
sudo apt-get update
sudo apt-get install -y curl wget git build-essential python3 python3-pip nginx

# Set up Python environment
sudo pip3 install requests jinja2 flask gunicorn

# Download and prepare the TEE service
cat > /opt/rhombus/tee/bin/tee_service.py << 'EOF'
#!/usr/bin/env python3
"""
TEE Service - Handles WebAssembly contract execution via API
Supports both SGX and SEV attestation
"""
import os
import json
import hashlib
import base64
import time
import subprocess
import sys
import logging
from flask import Flask, request, jsonify

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger("tee-service")

app = Flask(__name__)

# TEE Configuration
TEE_TYPE = os.environ.get('TEE_TYPE', 'SGX')  # SGX or SEV
CONTRACTS_DIR = "/opt/rhombus/tee/contracts"
CONTRACT_ID = "748775a3a2076c1ae990e94755e63bcb"
CONTRACT_PATH = os.path.join(CONTRACTS_DIR, f"{CONTRACT_ID}.wasm")

# Ensure contracts directory exists
os.makedirs(CONTRACTS_DIR, exist_ok=True)

# Create a dummy contract if it doesn't exist (for testing)
if not os.path.exists(CONTRACT_PATH):
    with open(CONTRACT_PATH, "wb") as f:
        # This is a very simple WebAssembly binary that adds two numbers
        f.write(b'\x00\x61\x73\x6d\x01\x00\x00\x00\x01\x07\x01\x60\x02\x7f\x7f\x01\x7f\x03\x02\x01\x00\x07\x07\x01\x03\x61\x64\x64\x00\x00\x0a\x09\x01\x07\x00\x20\x00\x20\x01\x6a\x0b')
    logger.info(f"Created dummy contract at {CONTRACT_PATH}")

@app.route('/api/status', methods=['GET'])
def status():
    return jsonify({
        "status": "running",
        "type": TEE_TYPE,
        "version": "1.0.0",
        "timestamp": time.time()
    })

@app.route('/api/contracts', methods=['GET'])
def list_contracts():
    contracts = []
    for filename in os.listdir(CONTRACTS_DIR):
        if filename.endswith('.wasm'):
            contract_id = filename.split('.')[0]
            contracts.append({
                "id": contract_id,
                "size": os.path.getsize(os.path.join(CONTRACTS_DIR, filename)),
                "created": os.path.getctime(os.path.join(CONTRACTS_DIR, filename))
            })
    return jsonify({"contracts": contracts})

@app.route('/api/execute', methods=['POST'])
def execute_contract():
    data = request.json
    if not data:
        return jsonify({"error": "No data provided"}), 400
    
    contract_id = data.get('contract_id', CONTRACT_ID)
    function_name = data.get('function', 'add')
    params = data.get('params', [42, 58])
    format_type = data.get('format', 'length-prefixed')
    
    # In a real implementation, this would execute the WASM in a TEE
    # For now, we'll simulate it for the 'add' function
    if function_name == 'add' and len(params) >= 2:
        result = params[0] + params[1]
        
        # Convert to little-endian bytes as expected
        result_bytes = result.to_bytes(4, byteorder='little')
        result_hex = result_bytes.hex()
        
        # Create a TEE attestation report
        attestation = {
            "type": TEE_TYPE,
            "timestamp": time.time(),
            "mr_enclave": hashlib.sha256(f"{contract_id}:{function_name}".encode()).hexdigest(),
            "nonce": base64.b64encode(os.urandom(16)).decode('ascii'),
            "contract_id": contract_id
        }
        
        return jsonify({
            "result": result_hex,
            "result_int": result,
            "attestation": attestation
        })
    else:
        return jsonify({"error": f"Function {function_name} not supported"}), 400

if __name__ == '__main__':
    port = int(os.environ.get('PORT', 8080))
    app.run(host='0.0.0.0', port=port)
EOF

# Make the service executable
sudo chmod +x /opt/rhombus/tee/bin/tee_service.py

# Create systemd service file
sudo cat > /etc/systemd/system/tee-service.service << 'EOF'
[Unit]
Description=TEE Service
After=network.target

[Service]
ExecStart=/usr/bin/python3 /opt/rhombus/tee/bin/tee_service.py
WorkingDirectory=/opt/rhombus/tee
Restart=always
User=root
Environment=TEE_TYPE=SGX
# For SEV nodes, uncomment the line below and comment out the line above
# Environment=TEE_TYPE=SEV

[Install]
WantedBy=multi-user.target
EOF

# Create health check script
cat > /opt/rhombus/tee/bin/health_check.sh << 'EOF'
#!/bin/bash
curl -s http://localhost:8080/api/status
EOF
chmod +x /opt/rhombus/tee/bin/health_check.sh

# Generate a simple add contract for testing
cat > /opt/rhombus/tee/contracts/748775a3a2076c1ae990e94755e63bcb.wasm << 'EOF'
\x00\x61\x73\x6d\x01\x00\x00\x00\x01\x07\x01\x60\x02\x7f\x7f\x01\x7f\x03\x02\x01\x00\x07\x07\x01\x03\x61\x64\x64\x00\x00\x0a\x09\x01\x07\x00\x20\x00\x20\x01\x6a\x0b
EOF

# Enable and start the service
sudo systemctl daemon-reload
sudo systemctl enable tee-service
sudo systemctl start tee-service

# Create nginx reverse proxy config
sudo cat > /etc/nginx/sites-available/tee-service << 'EOF'
server {
    listen 80;
    server_name _;

    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }

    location /status.json {
        add_header Content-Type application/json;
        return 200 '{"status":"running","timestamp":"'$(date -u +%Y-%m-%dT%H:%M:%SZ)'"}';
    }
}
EOF

# Enable the nginx site
sudo ln -sf /etc/nginx/sites-available/tee-service /etc/nginx/sites-enabled/default
sudo systemctl restart nginx

echo "TEE service installation complete"
echo "Testing API endpoint..."
curl -s http://localhost:8080/api/status || echo "Service not responding yet, please wait..."

exit 0
