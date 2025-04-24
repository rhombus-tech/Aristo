#!/bin/bash

# Deploy TEE mesh binaries to AWS instances
# This script deploys the TEE controller and coordinator binaries to enable the full mesh network

set -e

# Configuration
REGION="us-east-1"
KEY_NAME="nasdaq-tee-key"

# Get AWS instance IPs
SGX_IPS=(54.226.83.253 3.80.113.74)
SEV_IPS=(54.209.198.69 52.206.202.214)

# Define binary locations - use path with space properly escaped for shell
PROJECT_DIR="/Users/talzisckind/Downloads/aristo-fresh 2"
TEE_CONTROLLER="${PROJECT_DIR}/execution/target/release/tee-controller"
COORDINATOR_MOCK="${PROJECT_DIR}/execution/target/release/coordinator_mock"
MULTI_TEE_INTEGRATION="${PROJECT_DIR}/execution/target/release/multi_tee_integration"

# Deploy coordinator to first SGX node (will act as regional coordinator)
echo "Deploying coordinator to SGX node (${SGX_IPS[0]})..."
scp -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem "$COORDINATOR_MOCK" ubuntu@${SGX_IPS[0]}:/tmp/coordinator_mock
ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SGX_IPS[0]} "sudo mv /tmp/coordinator_mock /opt/rhombus/coordinator && sudo chmod +x /opt/rhombus/coordinator"

# Create coordinator service file
ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SGX_IPS[0]} "sudo bash -c 'cat > /etc/systemd/system/coordinator.service << EOT
[Unit]
Description=TEE Mesh Coordinator Service
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/rhombus
ExecStart=/opt/rhombus/coordinator
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOT'"

# Deploy tee-controller to all nodes
for i in {0..1}; do
  # Deploy to SGX node
  echo "Deploying TEE controller to SGX node ${SGX_IPS[$i]}..."
  scp -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem "$TEE_CONTROLLER" ubuntu@${SGX_IPS[$i]}:/tmp/tee-controller
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SGX_IPS[$i]} "sudo mv /tmp/tee-controller /opt/rhombus/tee-controller && sudo chmod +x /opt/rhombus/tee-controller"
  
  # Create TEE controller service file for SGX
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SGX_IPS[$i]} "sudo bash -c 'cat > /etc/systemd/system/tee-controller.service << EOT
[Unit]
Description=TEE Controller Service (SGX)
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/rhombus
Environment=RUST_LOG=info
Environment=COORDINATOR_URL=http://${SGX_IPS[0]}:8080
Environment=NODE_TYPE=SGX
Environment=PAIR_ID=$i
Environment=PARTNER_IP=${SEV_IPS[$i]}
Environment=WORKER_ID=sgx-node-$i
Environment=REGION_ID=us-east
Environment=BATCH_SIZE=500
Environment=WORKER_THREADS=8
ExecStart=/opt/rhombus/tee-controller --addr 0.0.0.0:7070 --region us-east
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOT'"

  # Deploy to SEV node
  echo "Deploying TEE controller to SEV node ${SEV_IPS[$i]}..."
  scp -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem "$TEE_CONTROLLER" ubuntu@${SEV_IPS[$i]}:/tmp/tee-controller
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SEV_IPS[$i]} "sudo mv /tmp/tee-controller /opt/rhombus/tee-controller && sudo chmod +x /opt/rhombus/tee-controller"
  
  # Create TEE controller service file for SEV
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SEV_IPS[$i]} "sudo bash -c 'cat > /etc/systemd/system/tee-controller.service << EOT
[Unit]
Description=TEE Controller Service (SEV)
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/rhombus
Environment=RUST_LOG=info
Environment=COORDINATOR_URL=http://${SGX_IPS[0]}:8080
Environment=NODE_TYPE=SEV
Environment=PAIR_ID=$i
Environment=PARTNER_IP=${SGX_IPS[$i]}
Environment=WORKER_ID=sev-node-$i
Environment=REGION_ID=us-east
Environment=BATCH_SIZE=250
Environment=WORKER_THREADS=8
Environment=SAMPLING_RATIO=0.2
ExecStart=/opt/rhombus/tee-controller --addr 0.0.0.0:7070 --region us-east
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOT'"
done

# Start services
echo "Starting coordinator service on ${SGX_IPS[0]}..."
ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SGX_IPS[0]} "sudo systemctl daemon-reload && sudo systemctl enable coordinator && sudo systemctl start coordinator"

# Start TEE controllers
for i in {0..1}; do
  echo "Starting TEE controller on SGX node ${SGX_IPS[$i]}..."
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SGX_IPS[$i]} "sudo systemctl daemon-reload && sudo systemctl enable tee-controller && sudo systemctl start tee-controller"
  
  echo "Starting TEE controller on SEV node ${SEV_IPS[$i]}..."
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SEV_IPS[$i]} "sudo systemctl daemon-reload && sudo systemctl enable tee-controller && sudo systemctl start tee-controller"
done

echo "Deployment of mesh binaries complete!"
echo "Waiting for services to initialize (15 seconds)..."
sleep 15

# Verify services are running
echo "Verifying coordinator service..."
ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SGX_IPS[0]} "sudo systemctl status coordinator | grep 'Active:'"

echo "Verifying TEE controller services..."
for i in {0..1}; do
  echo "SGX node ${SGX_IPS[$i]}:"
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SGX_IPS[$i]} "sudo systemctl status tee-controller | grep 'Active:'"
  
  echo "SEV node ${SEV_IPS[$i]}:"
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SEV_IPS[$i]} "sudo systemctl status tee-controller | grep 'Active:'"
done

echo "Mesh network deployment complete!"
echo "To run benchmarks, use: sudo /opt/rhombus/run_benchmark.sh"
