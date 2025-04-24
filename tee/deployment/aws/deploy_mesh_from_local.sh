#!/bin/bash

# Deploy TEE mesh network by copying local source code to AWS instances and building there
# This ensures architecture compatibility with the AWS instances

set -e

# Configuration
REGION="us-east-1"
KEY_NAME="nasdaq-tee-key"
LOCAL_SOURCE_DIR="/Users/talzisckind/Downloads/aristo-fresh 2"

# Get AWS instance IPs
SGX_IPS=(54.226.83.253 3.80.113.74)
SEV_IPS=(54.209.198.69 52.206.202.214)

# Choose the first SGX node as our coordinator
COORDINATOR_IP=${SGX_IPS[0]}

echo "Creating temporary source package for transfer..."
# Create a temporary directory for our source code
TEMP_DIR=$(mktemp -d)
mkdir -p $TEMP_DIR/source/execution/controller/src

# Copy only the essential source code files
cp -r "$LOCAL_SOURCE_DIR/execution/controller/src" "$TEMP_DIR/source/execution/controller/"
cp -r "$LOCAL_SOURCE_DIR/execution/controller/Cargo.toml" "$TEMP_DIR/source/execution/controller/" 2>/dev/null || true
cp -r "$LOCAL_SOURCE_DIR/execution/Cargo.toml" "$TEMP_DIR/source/execution/" 2>/dev/null || true

# Create minimal Cargo.toml files if they don't exist
if [ ! -f "$TEMP_DIR/source/execution/controller/Cargo.toml" ]; then
    echo "Creating controller Cargo.toml..."
    cat > "$TEMP_DIR/source/execution/controller/Cargo.toml" << EOT
[package]
name = "hypertee-controller"
version = "0.1.0"
edition = "2021"

[[bin]]
name = "tee-controller"
path = "src/main.rs"

[[bin]]
name = "coordinator_mock"
path = "src/bin/coordinator_mock.rs"

[dependencies]
tokio = { version = "1.28", features = ["full"] }
tonic = "0.9"
prost = "0.11"
clap = { version = "4.3", features = ["derive"] }
tracing = "0.1"
tracing-subscriber = "0.3"
async-trait = "0.1"
futures = "0.3"
serde = { version = "1.0", features = ["derive"] }
serde_json = "1.0"
axum = "0.6"
EOT
fi

if [ ! -f "$TEMP_DIR/source/execution/Cargo.toml" ]; then
    echo "Creating execution workspace Cargo.toml..."
    cat > "$TEMP_DIR/source/execution/Cargo.toml" << EOT
[workspace]
members = [
    "controller",
]
resolver = "2"
EOT
fi

# Create a mock coordinator file if it doesn't exist
if [ ! -f "$TEMP_DIR/source/execution/controller/src/bin/coordinator_mock.rs" ]; then
    echo "Creating mock coordinator implementation..."
    mkdir -p "$TEMP_DIR/source/execution/controller/src/bin"
    cat > "$TEMP_DIR/source/execution/controller/src/bin/coordinator_mock.rs" << EOT
use axum::{
    routing::{get, post},
    Router, Json, http::StatusCode, extract::State,
};
use serde::{Deserialize, Serialize};
use std::sync::{Arc, RwLock};
use std::collections::HashMap;
use std::net::SocketAddr;

#[derive(Debug, Clone, Serialize, Deserialize)]
struct TeeInfo {
    id: String,
    address: String,
    region: String,
    node_type: String,
}

#[derive(Debug, Default)]
struct Registry {
    tees: HashMap<String, TeeInfo>,
}

#[tokio::main]
async fn main() {
    // Initialize tracing
    tracing_subscriber::fmt::init();
    
    // Create shared state
    let registry = Arc::new(RwLock::new(Registry::default()));
    
    // Build our application with routes
    let app = Router::new()
        .route("/register", post(register_tee))
        .route("/tees", get(list_tees))
        .route("/health", get(health_check))
        .with_state(registry);

    // Run our app
    let addr = SocketAddr::from(([0, 0, 0, 0], 8080));
    println!("Coordinator listening on {}", addr);
    axum::Server::bind(&addr)
        .serve(app.into_make_service())
        .await
        .unwrap();
}

async fn register_tee(
    State(registry): State<Arc<RwLock<Registry>>>,
    Json(tee_info): Json<TeeInfo>,
) -> StatusCode {
    println!("Registering TEE: {:?}", tee_info);
    let mut reg = registry.write().unwrap();
    reg.tees.insert(tee_info.id.clone(), tee_info);
    StatusCode::CREATED
}

async fn list_tees(
    State(registry): State<Arc<RwLock<Registry>>>,
) -> Json<Vec<TeeInfo>> {
    let reg = registry.read().unwrap();
    let tees: Vec<TeeInfo> = reg.tees.values().cloned().collect();
    Json(tees)
}

async fn health_check() -> StatusCode {
    StatusCode::OK
}
EOT
fi

# Create a tarball of our source code
echo "Creating source tarball..."
pushd $TEMP_DIR > /dev/null
tar czf source.tar.gz source/
popd > /dev/null

echo "Preparing SGX coordinator node..."

# Copy source code to coordinator node and build
ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${COORDINATOR_IP} "
sudo apt-get update
sudo apt-get install -y build-essential pkg-config libssl-dev curl
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
source \$HOME/.cargo/env
echo 'export PATH=\$PATH:\$HOME/.cargo/bin' >> \$HOME/.bashrc
mkdir -p \$HOME/execution_src
"

# Copy the source tarball to the coordinator node
scp -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem $TEMP_DIR/source.tar.gz ubuntu@${COORDINATOR_IP}:~/source.tar.gz

# Extract and build the source
ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${COORDINATOR_IP} "
tar xzf ~/source.tar.gz -C \$HOME
cd \$HOME/source/execution
source \$HOME/.cargo/env
echo 'Building TEE controller and coordinator...'
cargo build --release
sudo mkdir -p /opt/rhombus
sudo cp \$HOME/source/execution/target/release/tee-controller /opt/rhombus/tee-controller
sudo cp \$HOME/source/execution/target/release/coordinator_mock /opt/rhombus/coordinator
sudo chmod +x /opt/rhombus/tee-controller /opt/rhombus/coordinator
"

# Do the same for the remaining nodes
for i in {0..1}; do
  # Skip coordinator node if we're at the first SGX node
  if [ $i -eq 0 ]; then
    SGX_NODE=${SGX_IPS[1]}
  else
    SGX_NODE=${SGX_IPS[0]}
  fi
  
  SEV_NODE=${SEV_IPS[$i]}
  
  # Set up SGX node
  echo "Setting up SGX node ${SGX_NODE}..."
  if [ "${SGX_NODE}" != "${COORDINATOR_IP}" ]; then
    ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SGX_NODE} "
    sudo apt-get update
    sudo apt-get install -y build-essential pkg-config libssl-dev curl
    curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
    source \$HOME/.cargo/env
    echo 'export PATH=\$PATH:\$HOME/.cargo/bin' >> \$HOME/.bashrc
    mkdir -p \$HOME/execution_src
    "
    
    # Copy the source tarball
    scp -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem $TEMP_DIR/source.tar.gz ubuntu@${SGX_NODE}:~/source.tar.gz
    
    # Extract and build
    ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SGX_NODE} "
    tar xzf ~/source.tar.gz -C \$HOME
    cd \$HOME/source/execution
    source \$HOME/.cargo/env
    echo 'Building TEE controller...'
    cargo build --release --bin tee-controller
    sudo mkdir -p /opt/rhombus
    sudo cp \$HOME/source/execution/target/release/tee-controller /opt/rhombus/tee-controller
    sudo chmod +x /opt/rhombus/tee-controller
    "
  fi
  
  # Set up SEV node
  echo "Setting up SEV node ${SEV_NODE}..."
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SEV_NODE} "
  sudo apt-get update
  sudo apt-get install -y build-essential pkg-config libssl-dev curl
  curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
  source \$HOME/.cargo/env
  echo 'export PATH=\$PATH:\$HOME/.cargo/bin' >> \$HOME/.bashrc
  mkdir -p \$HOME/execution_src
  "
  
  # Copy the source tarball
  scp -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem $TEMP_DIR/source.tar.gz ubuntu@${SEV_NODE}:~/source.tar.gz
  
  # Extract and build
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SEV_NODE} "
  tar xzf ~/source.tar.gz -C \$HOME
  cd \$HOME/source/execution
  source \$HOME/.cargo/env
  echo 'Building TEE controller...'
  cargo build --release --bin tee-controller
  sudo mkdir -p /opt/rhombus
  sudo cp \$HOME/source/execution/target/release/tee-controller /opt/rhombus/tee-controller
  sudo chmod +x /opt/rhombus/tee-controller
  "
done

# Clean up temporary files
echo "Cleaning up temporary files..."
rm -rf $TEMP_DIR

# Create coordinator service file
echo "Creating service files..."
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

sleep 5  # Give coordinator time to start

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

# Create helper scripts
echo "Creating helper scripts for mesh management and testing..."
ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${COORDINATOR_IP} "sudo bash -c 'cat > /opt/rhombus/check_mesh.sh << EOT
#!/bin/bash
echo \"Checking coordinator status...\"
curl -s http://localhost:8080/tees

echo -e \"\\n\\nChecking SGX controller status...\"
curl -s http://localhost:7070/status

echo -e \"\\n\\nChecking parameter validation...\"
/opt/rhombus/test_parameter_validation.sh
EOT'"

ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${COORDINATOR_IP} "sudo bash -c 'cat > /opt/rhombus/run_mesh_benchmark.sh << EOT
#!/bin/bash
echo \"Running dual TEE mesh benchmark...\"
cd /opt/rhombus/benchmark
python3 paired_node_performance_test.py --primary-ip ${SGX_IPS[0]} --secondary-ip ${SEV_IPS[0]} --mode=secure-cross-attested
EOT'"

ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${COORDINATOR_IP} "sudo chmod +x /opt/rhombus/check_mesh.sh /opt/rhombus/run_mesh_benchmark.sh"

echo "====================================="
echo "Dual TEE Mesh Network Deployment Complete!"
echo "====================================="
echo "To check mesh status: ssh -i ~/${KEY_NAME}.pem ubuntu@${COORDINATOR_IP} 'sudo /opt/rhombus/check_mesh.sh'"
echo "To run benchmarks: ssh -i ~/${KEY_NAME}.pem ubuntu@${COORDINATOR_IP} 'sudo /opt/rhombus/run_mesh_benchmark.sh'"
echo "====================================="
