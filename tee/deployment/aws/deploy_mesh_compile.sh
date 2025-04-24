#!/bin/bash

# Deploy TEE mesh network by compiling binaries directly on AWS instances
# This addresses the architecture mismatch issue

set -e

# Configuration
REGION="us-east-1"
KEY_NAME="nasdaq-tee-key"

# Get AWS instance IPs
SGX_IPS=(54.226.83.253 3.80.113.74)
SEV_IPS=(54.209.198.69 52.206.202.214)

# Choose the first SGX node as our coordinator
COORDINATOR_IP=${SGX_IPS[0]}

echo "Preparing repositories and building binaries..."

# Configure the SGX node with coordinator
ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${COORDINATOR_IP} "
sudo apt-get update
sudo apt-get install -y git curl build-essential pkg-config libssl-dev
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
source \$HOME/.cargo/env

# Clone the repository if needed (we'll use an existing copy if available)
if [ ! -d \"/home/ubuntu/aristo\" ]; then
    echo 'Cloning repository...'
    git clone https://github.com/rhombus-tech/aristo.git /home/ubuntu/aristo
fi

cd /home/ubuntu/aristo/execution
echo 'Building TEE controller and coordinator...'
cargo build --release --bin coordinator_mock --bin tee-controller

echo 'Moving binaries to /opt/rhombus...'
sudo cp /home/ubuntu/aristo/execution/target/release/coordinator_mock /opt/rhombus/coordinator
sudo cp /home/ubuntu/aristo/execution/target/release/tee-controller /opt/rhombus/tee-controller
sudo chmod +x /opt/rhombus/coordinator /opt/rhombus/tee-controller
"

# Now deploy the tee-controller to all TEE nodes
for i in {0..1}; do
  # Skip the coordinator node as we already built binaries there
  if [ "${SGX_IPS[$i]}" != "$COORDINATOR_IP" ]; then
    # Deploy to SGX node
    echo "Building TEE controller on SGX node ${SGX_IPS[$i]}..."
    ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SGX_IPS[$i]} "
    sudo apt-get update
    sudo apt-get install -y git curl build-essential pkg-config libssl-dev
    curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
    source \$HOME/.cargo/env
    
    # Clone the repository if needed
    if [ ! -d \"/home/ubuntu/aristo\" ]; then
        git clone https://github.com/rhombus-tech/aristo.git /home/ubuntu/aristo
    fi
    
    cd /home/ubuntu/aristo/execution
    cargo build --release --bin tee-controller
    
    sudo cp /home/ubuntu/aristo/execution/target/release/tee-controller /opt/rhombus/tee-controller
    sudo chmod +x /opt/rhombus/tee-controller
    "
  fi
  
  # Deploy to SEV node
  echo "Building TEE controller on SEV node ${SEV_IPS[$i]}..."
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SEV_IPS[$i]} "
  sudo apt-get update
  sudo apt-get install -y git curl build-essential pkg-config libssl-dev
  curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
  source \$HOME/.cargo/env
  
  # Clone the repository if needed
  if [ ! -d \"/home/ubuntu/aristo\" ]; then
      git clone https://github.com/rhombus-tech/aristo.git /home/ubuntu/aristo
  fi
  
  cd /home/ubuntu/aristo/execution
  cargo build --release --bin tee-controller
  
  sudo cp /home/ubuntu/aristo/execution/target/release/tee-controller /opt/rhombus/tee-controller
  sudo chmod +x /opt/rhombus/tee-controller
  "
done

# Create coordinator service file
ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${COORDINATOR_IP} "sudo bash -c 'cat > /etc/systemd/system/coordinator.service << EOT
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

# Create TEE controller service files
for i in {0..1}; do
  # SGX node
  echo "Creating TEE controller service on SGX node ${SGX_IPS[$i]}..."
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SGX_IPS[$i]} "sudo bash -c 'cat > /etc/systemd/system/tee-controller.service << EOT
[Unit]
Description=TEE Controller Service (SGX)
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/rhombus
Environment=RUST_LOG=info
Environment=COORDINATOR_URL=http://${COORDINATOR_IP}:8080
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

  # SEV node
  echo "Creating TEE controller service on SEV node ${SEV_IPS[$i]}..."
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SEV_IPS[$i]} "sudo bash -c 'cat > /etc/systemd/system/tee-controller.service << EOT
[Unit]
Description=TEE Controller Service (SEV)
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/rhombus
Environment=RUST_LOG=info
Environment=COORDINATOR_URL=http://${COORDINATOR_IP}:8080
Environment=NODE_TYPE=SEV
Environment=PAIR_ID=$i
Environment=PARTNER_IP=${SGX_IPS[$i]}
Environment=WORKER_ID=sev-node-$i
Environment=REGION_ID=us-east
Environment=BATCH_SIZE=100
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
echo "Starting coordinator service on ${COORDINATOR_IP}..."
ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${COORDINATOR_IP} "sudo systemctl daemon-reload && sudo systemctl enable coordinator && sudo systemctl start coordinator"

# Start TEE controllers
for i in {0..1}; do
  echo "Starting TEE controller on SGX node ${SGX_IPS[$i]}..."
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SGX_IPS[$i]} "sudo systemctl daemon-reload && sudo systemctl enable tee-controller && sudo systemctl start tee-controller"
  
  echo "Starting TEE controller on SEV node ${SEV_IPS[$i]}..."
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SEV_IPS[$i]} "sudo systemctl daemon-reload && sudo systemctl enable tee-controller && sudo systemctl start tee-controller"
done

echo "Deployment of mesh binaries complete!"
echo "Waiting for services to initialize (30 seconds)..."
sleep 30

# Verify services are running
echo "Verifying coordinator service..."
ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${COORDINATOR_IP} "sudo systemctl status coordinator | grep 'Active:'"

echo "Verifying TEE controller services..."
for i in {0..1}; do
  echo "SGX node ${SGX_IPS[$i]}:"
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SGX_IPS[$i]} "sudo systemctl status tee-controller | grep 'Active:'"
  
  echo "SEV node ${SEV_IPS[$i]}:"
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SEV_IPS[$i]} "sudo systemctl status tee-controller | grep 'Active:'"
done

# Create a helper script to check mesh status and run benchmarks
ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${COORDINATOR_IP} "sudo bash -c 'cat > /opt/rhombus/check_mesh.sh << EOT
#!/bin/bash
echo "Checking mesh status..."
curl -s http://localhost:7071/mesh-status
echo -e "\n\nRunning parameter validation test..."
/opt/rhombus/test_parameter_validation.sh
EOT'"

ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${COORDINATOR_IP} "sudo chmod +x /opt/rhombus/check_mesh.sh"

echo "====================================="
echo "Mesh network deployment complete!"
echo "====================================="
echo "To check mesh status, run: ssh -i ~/${KEY_NAME}.pem ubuntu@${COORDINATOR_IP} 'sudo /opt/rhombus/check_mesh.sh'"
echo "To run benchmarks, use: ssh -i ~/${KEY_NAME}.pem ubuntu@${COORDINATOR_IP} 'sudo /opt/rhombus/run_benchmark.sh'"
echo "====================================="
