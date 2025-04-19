#!/usr/bin/env python3
"""
Deploy additional TEE nodes to complete the dual TEE cross-attestation infrastructure.
This script creates EC2 instances for both SGX and SEV nodes and configures
them with the appropriate TEE services.
"""

import os
import sys
import time
import json
import boto3
import logging
import argparse
from typing import Dict, Any, List

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger("deploy_tee_nodes")

# Define target node configuration to match existing node references
TARGET_NODES = [
    {
        "node_id": "sgx-node-1",
        "node_type": "SGX",
        "target_ip": "3.82.138.122",  # Note: actual IP will be different
        "instance_type": "c5a.xlarge",
        "ami": "ami-030f04819b19327fc"  # Same AMI as in CloudFormation template
    },
    {
        "node_id": "sev-node-1",
        "node_type": "SEV",
        "target_ip": "44.203.182.22",  # Note: actual IP will be different
        "instance_type": "c6a.2xlarge",
        "ami": "ami-030f04819b19327fc"  # Same AMI as in CloudFormation template
    }
]

# User data script for SGX node initialization
SGX_USER_DATA = """#!/bin/bash -xe
exec > >(tee /var/log/user-data.log|logger -t user-data -s 2>/dev/console) 2>&1
echo "Starting SGX node setup"

# Install required packages
apt-get update
apt-get install -y curl jq git build-essential python3-pip

# Create TEE service directories
mkdir -p /opt/rhombus/tee/bin
mkdir -p /opt/rhombus/tee/contracts

# Clone Enarx repository for SGX support
cd /tmp
git clone https://github.com/enarx/enarx.git
cd enarx
cargo build --release
cp target/release/enarx /usr/local/bin/

# Install TEE client
cd /opt/rhombus/tee/bin
curl -L -o tee-client https://github.com/rhombus-tech/tee-client/releases/latest/download/tee-client-linux-amd64
chmod +x tee-client

# Create systemd service for TEE
cat > /etc/systemd/system/tee-service.service << 'EOF'
[Unit]
Description=TEE Service
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/rhombus/tee
ExecStart=/opt/rhombus/tee/bin/tee-client --service --port 8080
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

# Enable and start the service
systemctl daemon-reload
systemctl enable tee-service
systemctl start tee-service

# Set SGX-specific variables
echo "SGX_ENABLED=1" >> /etc/environment
echo "SGX_MODE=HW" >> /etc/environment

# Create status script
cat > /opt/rhombus/tee/status.sh << 'EOF'
#!/bin/bash
ID=$(curl -s http://169.254.169.254/latest/meta-data/instance-id)
IP=$(curl -s http://169.254.169.254/latest/meta-data/public-ipv4)
echo "{\\"node_type\\":\\"SGX\\",\\"id\\":\\"$ID\\",\\"ip\\":\\"$IP\\",\\"status\\":\\"running\\",\\"timestamp\\":\\"`date -u +"%Y-%m-%dT%H:%M:%SZ"`\\"}" > /var/www/html/status.json
EOF
chmod +x /opt/rhombus/tee/status.sh

# Set up simple web server to expose status
apt-get install -y apache2
echo '*/5 * * * * root /opt/rhombus/tee/status.sh' > /etc/cron.d/tee-status

# Initialize status immediately
/opt/rhombus/tee/status.sh

echo "SGX node setup complete"
"""

# User data script for SEV node initialization
SEV_USER_DATA = """#!/bin/bash -xe
exec > >(tee /var/log/user-data.log|logger -t user-data -s 2>/dev/console) 2>&1
echo "Starting SEV node setup"

# Install required packages
apt-get update
apt-get install -y curl jq git build-essential python3-pip

# Create TEE service directories
mkdir -p /opt/rhombus/tee/bin
mkdir -p /opt/rhombus/tee/contracts

# Clone Enarx repository for SEV support
cd /tmp
git clone https://github.com/enarx/enarx.git
cd enarx
cargo build --release --features=sev
cp target/release/enarx /usr/local/bin/

# Install TEE client
cd /opt/rhombus/tee/bin
curl -L -o tee-client https://github.com/rhombus-tech/tee-client/releases/latest/download/tee-client-linux-amd64
chmod +x tee-client

# Create systemd service for TEE
cat > /etc/systemd/system/tee-service.service << 'EOF'
[Unit]
Description=TEE Service
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/rhombus/tee
ExecStart=/opt/rhombus/tee/bin/tee-client --service --port 8080
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

# Enable and start the service
systemctl daemon-reload
systemctl enable tee-service
systemctl start tee-service

# Set SEV-specific variables
echo "SEV_ENABLED=1" >> /etc/environment
echo "SEV_SNP_ENABLED=1" >> /etc/environment

# Create status script
cat > /opt/rhombus/tee/status.sh << 'EOF'
#!/bin/bash
ID=$(curl -s http://169.254.169.254/latest/meta-data/instance-id)
IP=$(curl -s http://169.254.169.254/latest/meta-data/public-ipv4)
echo "{\\"node_type\\":\\"SEV\\",\\"id\\":\\"$ID\\",\\"ip\\":\\"$IP\\",\\"status\\":\\"running\\",\\"timestamp\\":\\"`date -u +"%Y-%m-%dT%H:%M:%SZ"`\\"}" > /var/www/html/status.json
EOF
chmod +x /opt/rhombus/tee/status.sh

# Set up simple web server to expose status
apt-get install -y apache2
echo '*/5 * * * * root /opt/rhombus/tee/status.sh' > /etc/cron.d/tee-status

# Initialize status immediately
/opt/rhombus/tee/status.sh

echo "SEV node setup complete"
"""

def deploy_node(node_config: Dict[str, Any], key_name: str, security_group_id: str, subnet_id: str) -> Dict[str, Any]:
    """
    Deploy a TEE node as an EC2 instance with the specified configuration
    """
    logger.info(f"Deploying {node_config['node_type']} node: {node_config['node_id']}")
    
    # Select user data script based on node type
    user_data = SGX_USER_DATA if node_config['node_type'] == 'SGX' else SEV_USER_DATA
    
    # Create EC2 client
    ec2 = boto3.resource('ec2')
    
    # Create tags for the instance
    tags = [
        {
            'Key': 'Name',
            'Value': node_config['node_id']
        },
        {
            'Key': 'TEEType',
            'Value': node_config['node_type']
        },
        {
            'Key': 'Project',
            'Value': 'RhombusTech-TEE'
        }
    ]
    
    # Create instance
    instances = ec2.create_instances(
        ImageId=node_config['ami'],
        InstanceType=node_config['instance_type'],
        MinCount=1,
        MaxCount=1,
        KeyName=key_name,
        UserData=user_data,
        SecurityGroupIds=[security_group_id],
        SubnetId=subnet_id,
        TagSpecifications=[
            {
                'ResourceType': 'instance',
                'Tags': tags
            }
        ]
    )
    
    instance = instances[0]
    logger.info(f"Created instance {instance.id} for {node_config['node_type']} node")
    
    # Wait for the instance to be running
    logger.info(f"Waiting for instance {instance.id} to be running...")
    instance.wait_until_running()
    
    # Reload instance to get updated information
    instance.reload()
    
    # Return instance details
    return {
        "instance_id": instance.id,
        "node_id": node_config['node_id'],
        "node_type": node_config['node_type'],
        "public_ip": instance.public_ip_address,
        "private_ip": instance.private_ip_address,
        "status": "running"
    }

def create_security_group(vpc_id: str, node_type: str) -> str:
    """
    Create a security group for the TEE node
    """
    ec2 = boto3.client('ec2')
    
    group_name = f"TEE-{node_type}-SecurityGroup"
    logger.info(f"Creating security group {group_name}")
    
    # Create security group
    response = ec2.create_security_group(
        GroupName=group_name,
        Description=f"Security group for {node_type} TEE node",
        VpcId=vpc_id
    )
    
    group_id = response['GroupId']
    
    # Add ingress rules
    ec2.authorize_security_group_ingress(
        GroupId=group_id,
        IpPermissions=[
            # SSH
            {
                'IpProtocol': 'tcp',
                'FromPort': 22,
                'ToPort': 22,
                'IpRanges': [{'CidrIp': '0.0.0.0/0'}]
            },
            # TEE Service
            {
                'IpProtocol': 'tcp',
                'FromPort': 8080,
                'ToPort': 8080,
                'IpRanges': [{'CidrIp': '0.0.0.0/0'}]
            },
            # Status Web Server
            {
                'IpProtocol': 'tcp',
                'FromPort': 80,
                'ToPort': 80,
                'IpRanges': [{'CidrIp': '0.0.0.0/0'}]
            }
        ]
    )
    
    logger.info(f"Created security group {group_id}")
    return group_id

def check_instance_status(instance_id: str) -> Dict[str, Any]:
    """
    Check the status of an instance and its TEE service
    """
    ec2 = boto3.resource('ec2')
    instance = ec2.Instance(instance_id)
    
    # Reload to get current status
    instance.reload()
    
    # Check if instance is running
    if instance.state['Name'] != 'running':
        return {
            "instance_id": instance_id,
            "status": instance.state['Name'],
            "tee_service": "unknown"
        }
    
    # Try to check TEE service status
    import requests
    try:
        response = requests.get(f"http://{instance.public_ip_address}:8080/api/status", timeout=5)
        tee_service = "running" if response.status_code == 200 else "error"
    except Exception as e:
        tee_service = f"error: {str(e)}"
    
    return {
        "instance_id": instance_id,
        "status": instance.state['Name'],
        "public_ip": instance.public_ip_address,
        "private_ip": instance.private_ip_address,
        "tee_service": tee_service
    }

def main():
    parser = argparse.ArgumentParser(description="Deploy additional TEE nodes")
    parser.add_argument("--key-name", type=str, required=True, 
                        help="Name of the EC2 key pair to use")
    parser.add_argument("--vpc-id", type=str, required=True,
                        help="ID of the VPC to deploy to")
    parser.add_argument("--subnet-id", type=str, required=True,
                        help="ID of the subnet to deploy to")
    parser.add_argument("--nodes", type=str, choices=["all", "sgx", "sev"], default="all",
                        help="Which nodes to deploy")
    
    args = parser.parse_args()
    
    # Filter nodes based on argument
    nodes_to_deploy = []
    if args.nodes == "all":
        nodes_to_deploy = TARGET_NODES
    elif args.nodes == "sgx":
        nodes_to_deploy = [n for n in TARGET_NODES if n["node_type"] == "SGX"]
    else:  # sev
        nodes_to_deploy = [n for n in TARGET_NODES if n["node_type"] == "SEV"]
    
    # Create security groups
    security_groups = {}
    for node_type in set(n["node_type"] for n in nodes_to_deploy):
        security_groups[node_type] = create_security_group(args.vpc_id, node_type)
    
    # Deploy each node
    deployed_nodes = []
    for node_config in nodes_to_deploy:
        security_group_id = security_groups[node_config["node_type"]]
        deployed_node = deploy_node(node_config, args.key_name, security_group_id, args.subnet_id)
        deployed_nodes.append(deployed_node)
    
    # Wait for TEE services to initialize
    logger.info("Waiting for TEE services to initialize (60 seconds)...")
    time.sleep(60)
    
    # Check status of all deployed nodes
    logger.info("Checking status of deployed nodes...")
    final_statuses = []
    for node in deployed_nodes:
        status = check_instance_status(node["instance_id"])
        final_statuses.append(status)
    
    # Save deployment results
    timestamp = time.strftime("%Y%m%d_%H%M%S")
    result_file = f"tee_nodes_deployment_{timestamp}.json"
    with open(result_file, "w") as f:
        json.dump({
            "deployed_nodes": deployed_nodes,
            "final_status": final_statuses
        }, f, indent=2)
    
    logger.info(f"Deployment results saved to {result_file}")
    
    # Update real_tee_perf.py configuration
    update_perf_config(deployed_nodes)
    
    return 0

def update_perf_config(deployed_nodes: List[Dict[str, Any]]):
    """
    Update the real_tee_perf.py configuration with the newly deployed nodes
    """
    # Path to real_tee_perf.py
    perf_script_path = os.path.join(os.path.dirname(__file__), "real_tee_perf.py")
    
    # Read the current file
    with open(perf_script_path, "r") as f:
        content = f.read()
    
    # Find the fallback_nodes list
    import re
    fallback_nodes_pattern = r"fallback_nodes\s*=\s*\[(.*?)\]"
    fallback_nodes_match = re.search(fallback_nodes_pattern, content, re.DOTALL)
    
    if not fallback_nodes_match:
        logger.warning("Could not find fallback_nodes list in real_tee_perf.py")
        return
    
    # Generate new fallback_nodes content
    new_fallback_nodes = "[\n"
    for node in deployed_nodes:
        new_fallback_nodes += f"""        {{
            "node_id": "{node['instance_id']}",
            "node_type": "{node['node_type']}",
            "public_ip": "{node['public_ip']}",
            "private_ip": "{node['private_ip']}",
            "port": 8080,
            "region": "us-east-1",
            "status": "active",
            "attestation_supported": True,
            "api_version": "1.0",
            "allow_format_fallback": True
        }},\n"""
    
    new_fallback_nodes = new_fallback_nodes.rstrip(",\n") + "\n    ]"
    
    # Replace the fallback_nodes list
    new_content = content[:fallback_nodes_match.start(1)] + new_fallback_nodes[1:-1] + content[fallback_nodes_match.end(1):]
    
    # Write updated content back
    with open(perf_script_path, "w") as f:
        f.write(new_content)
    
    logger.info("Updated real_tee_perf.py with new node configurations")

if __name__ == "__main__":
    sys.exit(main())
