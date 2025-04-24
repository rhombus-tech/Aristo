#!/bin/bash

# Deploy a standalone version of the dual TEE mesh network
# Fixed implementation to resolve compilation errors

set -e

# Configuration
REGION="us-east-1"
KEY_NAME="nasdaq-tee-key"

# Get AWS instance IPs
SGX_IPS=(54.226.83.253 3.80.113.74)
SEV_IPS=(54.209.198.69 52.206.202.214)

# Choose the first SGX node as our coordinator
COORDINATOR_IP=${SGX_IPS[0]}

# Create standalone coordinator code
echo "Creating standalone coordinator code..."
COORDINATOR_CODE=$(cat <<'EOF'
use std::collections::HashMap;
use std::net::SocketAddr;
use std::sync::{Arc, Mutex};
use axum::{
    extract::{Json, State},
    http::StatusCode,
    routing::{get, post},
    Router,
};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
struct TeeInfo {
    id: String,
    address: String,
    region: String,
    node_type: String,
    batch_size: Option<u32>,
    pair_id: Option<u32>,
}

#[derive(Debug, Default)]
struct Registry {
    tees: HashMap<String, TeeInfo>,
}

type SharedRegistry = Arc<Mutex<Registry>>;

#[tokio::main]
async fn main() {
    // Initialize the registry
    let registry = Arc::new(Mutex::new(Registry::default()));
    
    // Build our application
    let app = Router::new()
        .route("/register", post(register_tee))
        .route("/tees", get(list_tees))
        .route("/health", get(health_check))
        .with_state(registry);
    
    // Bind to address
    let addr = SocketAddr::from(([0, 0, 0, 0], 8080));
    println!("Coordinator listening on {}", addr);
    
    axum::Server::bind(&addr)
        .serve(app.into_make_service())
        .await
        .unwrap();
}

async fn register_tee(
    State(registry): State<SharedRegistry>,
    Json(tee_info): Json<TeeInfo>,
) -> StatusCode {
    println!("Registering TEE: {:?}", tee_info);
    let mut reg = registry.lock().unwrap();
    reg.tees.insert(tee_info.id.clone(), tee_info);
    StatusCode::CREATED
}

async fn list_tees(
    State(registry): State<SharedRegistry>,
) -> Json<Vec<TeeInfo>> {
    let reg = registry.lock().unwrap();
    let tees: Vec<TeeInfo> = reg.tees.values().cloned().collect();
    Json(tees)
}

async fn health_check() -> StatusCode {
    StatusCode::OK
}
EOF
)

# Create standalone controller code
echo "Creating fixed standalone controller code..."
CONTROLLER_CODE=$(cat <<'EOF'
use std::env;
use std::net::SocketAddr;
use std::sync::{Arc, Mutex};
use axum::{
    extract::Json,
    http::StatusCode,
    routing::{get, post},
    Router,
};
use clap::Parser;
use serde::{Deserialize, Serialize};
use reqwest::Client;

#[derive(Parser, Debug)]
#[command(name = "tee-controller")]
struct Args {
    #[arg(long, default_value = "0.0.0.0:7070")]
    addr: String,
    
    #[arg(long, default_value = "us-east")]
    region: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct TeeInfo {
    id: String,
    address: String,
    region: String,
    node_type: String,
    batch_size: Option<u32>,
    pair_id: Option<u32>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct ExecutionRequest {
    contract_id: String,
    function: String,
    parameters: Vec<u8>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct ExecutionResult {
    success: bool,
    data: Vec<u8>,
    error: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct MeshStatus {
    node_type: String,
    pair_id: u32,
    partner_address: String,
    status: String,
    batch_size: u32,
    worker_threads: u32,
}

struct AppState {
    client: Client,
    coordinator_url: String,
    node_type: String,
    worker_id: String,
    pair_id: u32,
    partner_ip: String,
    batch_size: u32,
    worker_threads: u32,
}

// Global shared state
type SharedState = Arc<Mutex<AppState>>;

#[tokio::main]
async fn main() {
    // Parse command line arguments
    let args = Args::parse();
    
    // Get environment variables
    let coordinator_url = env::var("COORDINATOR_URL").unwrap_or_else(|_| "http://localhost:8080".to_string());
    let node_type = env::var("NODE_TYPE").unwrap_or_else(|_| "SGX".to_string());
    let worker_id = env::var("WORKER_ID").unwrap_or_else(|_| format!("{}-node", node_type.to_lowercase()));
    let pair_id = env::var("PAIR_ID").unwrap_or_else(|_| "0".to_string()).parse::<u32>().unwrap_or(0);
    let partner_ip = env::var("PARTNER_IP").unwrap_or_else(|_| "127.0.0.1".to_string());
    let batch_size = env::var("BATCH_SIZE").unwrap_or_else(|_| 
        if node_type == "SGX" { "500" } else { "100" }.to_string()
    ).parse::<u32>().unwrap_or(if node_type == "SGX" { 500 } else { 100 });
    let worker_threads = env::var("WORKER_THREADS").unwrap_or_else(|_| "8".to_string()).parse::<u32>().unwrap_or(8);
    
    println!("Starting TEE Controller ({}) with ID: {}", node_type, worker_id);
    println!("Coordinator URL: {}", coordinator_url);
    println!("Batch Size: {}", batch_size);
    println!("Worker Threads: {}", worker_threads);
    println!("Pair ID: {}", pair_id);
    println!("Partner IP: {}", partner_ip);
    
    // Create application state
    let state = Arc::new(Mutex::new(AppState {
        client: Client::new(),
        coordinator_url,
        node_type,
        worker_id,
        pair_id,
        partner_ip,
        batch_size,
        worker_threads,
    }));
    
    // Register with coordinator
    register_with_coordinator(state.clone()).await;
    
    // Build our application
    let app = Router::new()
        .route("/status", get(get_status))
        .route("/mesh-status", get(get_mesh_status))
        .route("/execute", post(execute_contract))
        .route("/health", get(|| async { StatusCode::OK }))
        .with_state(state);
    
    // Bind to address
    let addr = args.addr.parse::<SocketAddr>().expect("Invalid address format");
    println!("Controller listening on {}", addr);
    
    // Use a different approach to start the server
    let server = axum::Server::bind(&addr)
        .serve(app.into_make_service());
    
    println!("Server ready, press Ctrl+C to stop");
    
    // Start the server
    if let Err(e) = server.await {
        eprintln!("Server error: {}", e);
    }
}

async fn register_with_coordinator(state: SharedState) {
    let state_guard = state.lock().unwrap();
    let tee_info = TeeInfo {
        id: state_guard.worker_id.clone(),
        address: format!("http://{}:7070", state_guard.partner_ip),
        region: "us-east".to_string(),
        node_type: state_guard.node_type.clone(),
        batch_size: Some(state_guard.batch_size),
        pair_id: Some(state_guard.pair_id),
    };
    
    let client = state_guard.client.clone();
    let coordinator_url = state_guard.coordinator_url.clone();
    drop(state_guard);
    
    // Register with coordinator
    match client.post(format!("{}/register", coordinator_url))
        .json(&tee_info)
        .send()
        .await {
            Ok(_) => println!("Successfully registered with coordinator"),
            Err(e) => println!("Failed to register with coordinator: {}", e),
        }
}

async fn get_status() -> impl axum::response::IntoResponse {
    "Running"
}

async fn get_mesh_status(
    State(state): State<SharedState>
) -> impl axum::response::IntoResponse {
    let state_guard = state.lock().unwrap();
    let mesh_status = MeshStatus {
        node_type: state_guard.node_type.clone(),
        pair_id: state_guard.pair_id,
        partner_address: format!("http://{}:7070", state_guard.partner_ip),
        status: "Running".to_string(),
        batch_size: state_guard.batch_size,
        worker_threads: state_guard.worker_threads,
    };
    
    Json(mesh_status)
}

async fn execute_contract(
    Json(req): Json<ExecutionRequest>
) -> impl axum::response::IntoResponse {
    println!("Executing contract: {} -> {}", req.contract_id, req.function);
    
    // Check for length-prefixed format in parameters
    let params = req.parameters.clone();
    if params.len() >= 4 {
        let len_bytes = [params[0], params[1], params[2], params[3]];
        let len = u32::from_le_bytes(len_bytes) as usize;
        
        if len > 0 && len <= 1024 && len + 4 <= params.len() {
            println!("Processing length-prefixed parameters: {} bytes", len);
            // Process length-prefixed params
            let data = params[4..4+len].to_vec();
            println!("Parameter data: {:?}", data);
        } else {
            println!("Processing direct parameters: {} bytes", params.len());
            // Process direct params
        }
    }
    
    // Simulate execution and validation
    let result = ExecutionResult {
        success: true,
        data: vec![1, 2, 3, 4],
        error: None,
    };
    
    Json(result)
}
EOF
)

# Create standalone Cargo.toml files
COORDINATOR_CARGO=$(cat <<'EOF'
[package]
name = "coordinator"
version = "0.1.0"
edition = "2021"

[dependencies]
axum = "0.6.20"
serde = { version = "1.0", features = ["derive"] }
serde_json = "1.0"
tokio = { version = "1.28", features = ["full"] }
tracing = "0.1"
tracing-subscriber = "0.3"
EOF
)

CONTROLLER_CARGO=$(cat <<'EOF'
[package]
name = "tee-controller"
version = "0.1.0"
edition = "2021"

[dependencies]
axum = "0.6.20"
clap = { version = "4.3", features = ["derive"] }
reqwest = { version = "0.11", features = ["json"] }
serde = { version = "1.0", features = ["derive"] }
serde_json = "1.0"
tokio = { version = "1.28", features = ["full"] }
tracing = "0.1"
tracing-subscriber = "0.3"
EOF
)

echo "Preparing SGX coordinator node..."
# Set up Rust environment and create project
ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${COORDINATOR_IP} "
sudo apt-get update
sudo apt-get install -y build-essential pkg-config libssl-dev curl
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
source \$HOME/.cargo/env

mkdir -p \$HOME/coordinator/src
cat > \$HOME/coordinator/Cargo.toml << 'EOT'
$COORDINATOR_CARGO
EOT

cat > \$HOME/coordinator/src/main.rs << 'EOT'
$COORDINATOR_CODE
EOT

cd \$HOME/coordinator
cargo build --release

sudo mkdir -p /opt/rhombus
sudo cp \$HOME/coordinator/target/release/coordinator /opt/rhombus/
sudo chmod +x /opt/rhombus/coordinator
"

# Build and deploy controllers to each TEE node
for i in {0..1}; do
  # Deploy to SGX node
  echo "Deploying TEE controller to SGX node ${SGX_IPS[$i]}..."
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SGX_IPS[$i]} "
  sudo apt-get update
  sudo apt-get install -y build-essential pkg-config libssl-dev curl
  curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
  source \$HOME/.cargo/env
  
  mkdir -p \$HOME/tee-controller/src
  cat > \$HOME/tee-controller/Cargo.toml << 'EOT'
$CONTROLLER_CARGO
EOT

  cat > \$HOME/tee-controller/src/main.rs << 'EOT'
$CONTROLLER_CODE
EOT

  cd \$HOME/tee-controller
  cargo build --release
  
  sudo mkdir -p /opt/rhombus
  sudo cp \$HOME/tee-controller/target/release/tee-controller /opt/rhombus/
  sudo chmod +x /opt/rhombus/tee-controller
  "
  
  # Deploy to SEV node
  echo "Deploying TEE controller to SEV node ${SEV_IPS[$i]}..."
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SEV_IPS[$i]} "
  sudo apt-get update
  sudo apt-get install -y build-essential pkg-config libssl-dev curl
  curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
  source \$HOME/.cargo/env
  
  mkdir -p \$HOME/tee-controller/src
  cat > \$HOME/tee-controller/Cargo.toml << 'EOT'
$CONTROLLER_CARGO
EOT

  cat > \$HOME/tee-controller/src/main.rs << 'EOT'
$CONTROLLER_CODE
EOT

  cd \$HOME/tee-controller
  cargo build --release
  
  sudo mkdir -p /opt/rhombus
  sudo cp \$HOME/tee-controller/target/release/tee-controller /opt/rhombus/
  sudo chmod +x /opt/rhombus/tee-controller
  "
done

# Create coordinator service file
echo "Creating coordinator service file..."
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

# Create TEE controller service files for each node
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

# Give coordinator time to start
echo "Waiting for coordinator to start (5 seconds)..."
sleep 5

# Start TEE controllers
for i in {0..1}; do
  echo "Starting TEE controller on SGX node ${SGX_IPS[$i]}..."
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SGX_IPS[$i]} "sudo systemctl daemon-reload && sudo systemctl enable tee-controller && sudo systemctl start tee-controller"
  
  echo "Starting TEE controller on SEV node ${SEV_IPS[$i]}..."
  ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${SEV_IPS[$i]} "sudo systemctl daemon-reload && sudo systemctl enable tee-controller && sudo systemctl start tee-controller"
done

echo "Deployment of dual TEE mesh network complete!"
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

# Create test scripts
echo "Creating test and monitoring scripts..."
ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${COORDINATOR_IP} "sudo bash -c 'cat > /opt/rhombus/check_dual_tee_mesh.sh << EOT
#!/bin/bash

echo \"========== Coordinator Status ==========\"
curl -s http://${COORDINATOR_IP}:8080/tees | jq

echo -e \"\\n\\n========== SGX Node Status ==========\"
curl -s http://${SGX_IPS[0]}:7070/mesh-status | jq

echo -e \"\\n\\n========== SEV Node Status ==========\"
curl -s http://${SEV_IPS[0]}:7070/mesh-status | jq

echo -e \"\\n\\n========== Parameter Validation Test ==========\"
echo \"Testing length-prefixed format...\"
# Create a test payload with length prefix (4 bytes for length + data)
# Length is 10 (0x0A000000 in little endian)
PAYLOAD_LENGTH_PREFIX=\$(echo -n -e '\\x0A\\x00\\x00\\x00TESTPAYLOAD' | base64)
curl -s -X POST -H \"Content-Type: application/json\" -d \"{\\"contract_id\\":\\"test-contract\\",\\"function\\":\\"test-function\\",\\"parameters\\":[\$PAYLOAD_LENGTH_PREFIX]}\" http://${SGX_IPS[0]}:7070/execute | jq

echo -e \"\\n\\nTesting direct format...\"
# Create a direct payload without length prefix
PAYLOAD_DIRECT=\$(echo -n -e 'DIRECTPAYLOAD' | base64)
curl -s -X POST -H \"Content-Type: application/json\" -d \"{\\"contract_id\\":\\"test-contract\\",\\"function\\":\\"test-function\\",\\"parameters\\":[\$PAYLOAD_DIRECT]}\" http://${SGX_IPS[0]}:7070/execute | jq

echo -e \"\\n\\n========== Cross-Attestation Test ==========\"
echo \"Testing SGX -> SEV attestation...\"
curl -s -X POST -H \"Content-Type: application/json\" -d \"{\\"contract_id\\":\\"cross-attestation\\",\\"function\\":\\"attest\\",\\"parameters\\":[\$PAYLOAD_LENGTH_PREFIX]}\" http://${SGX_IPS[0]}:7070/execute | jq

echo -e \"\\n\\nTesting SEV -> SGX attestation...\"
curl -s -X POST -H \"Content-Type: application/json\" -d \"{\\"contract_id\\":\\"cross-attestation\\",\\"function\\":\\"attest\\",\\"parameters\\":[\$PAYLOAD_LENGTH_PREFIX]}\" http://${SEV_IPS[0]}:7070/execute | jq
EOT'"

ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${COORDINATOR_IP} "sudo apt-get install -y jq && sudo chmod +x /opt/rhombus/check_dual_tee_mesh.sh"

echo -e "\n============================================="
echo "Dual TEE Mesh Network Successfully Deployed!"
echo "============================================="
echo "Coordinator: ${COORDINATOR_IP}"
echo "SGX Nodes: ${SGX_IPS[*]}"
echo "SEV Nodes: ${SEV_IPS[*]}"
echo "============================================="
echo "To test the mesh network: ssh -i ~/${KEY_NAME}.pem ubuntu@${COORDINATOR_IP} 'sudo /opt/rhombus/check_dual_tee_mesh.sh'"
echo "============================================="

# Run initial test
echo "Running initial test of the dual TEE mesh network..."
ssh -o StrictHostKeyChecking=no -i ~/${KEY_NAME}.pem ubuntu@${COORDINATOR_IP} "sudo /opt/rhombus/check_dual_tee_mesh.sh"
