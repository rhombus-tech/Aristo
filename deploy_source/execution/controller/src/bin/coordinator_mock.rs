use std::collections::HashMap;
use std::sync::Arc;
use tokio::sync::RwLock;
use axum::{
    Json, Router, 
    extract::{Path, State},
    routing::{get, post},
    http::StatusCode,
    response::IntoResponse,
};
use serde::{Deserialize, Serialize};
use serde::ser::SerializeMap;
use serde_json::{json, Value};

// AppState to store our data
type AppState = Arc<RwLock<CoordinatorState>>;

// Standard response struct to ensure consistency
#[derive(Debug, Clone, Serialize, Deserialize)]
struct CoordinatorResponse {
    success: bool,
    error: Option<String>,
    data: Option<Value>,
}

struct CoordinatorState {
    workers: HashMap<String, WorkerInfo>,
    tee_pairs: HashMap<String, TeePair>,
    tasks: HashMap<String, InternalTaskInfo>,
    task_counter: usize,
}

struct WorkerInfo {
    id: String,
    attestation: String,
    last_heartbeat: u64,
}

#[derive(Debug, Clone)]
struct TeePair {
    region_id: String,
    primary_worker_id: String,
    secondary_worker_id: String,
    attestations: Option<Vec<Vec<u8>>>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct InternalTaskInfo {
    id: String,
    payload: Value,
    status: String,
    result: Option<Vec<u8>>,
    error: Option<String>,
}

#[derive(Serialize)]
struct TaskInfo {
    task: Task,
    status: String,
    start_time: String,
    end_time: String,
    error: Option<String>,
    results: Vec<Vec<u8>>,
}

#[derive(Serialize)]
struct Task {
    id: String,
    worker_ids: Vec<String>,
    data: Vec<u8>,
    attestations: Vec<Vec<u8>>,
    timeout: u64,
    region_id: String,
}

// API Request/Response Types
#[derive(Debug, Deserialize)]
struct RegisterWorkerRequest {
    #[serde(rename = "id", alias = "worker_id")]
    worker_id: String,
    enclave_id: Vec<u8>,
    // For backward compatibility, make attestation optional
    attestation: Option<String>,
}

#[derive(Debug, Deserialize)]
struct RegisterTeePairRequest {
    primary_worker_id: String,
    secondary_worker_id: String,
    #[serde(default)]
    attestations: Vec<Vec<u8>>,
}

#[derive(Debug, Deserialize)]
struct TaskSubmitRequest {
    #[serde(rename = "id", alias = "task_id")]
    id: Option<String>,
    worker_id: Option<String>,
    primary_worker_id: Option<String>,
    secondary_worker_id: Option<String>,
    task_type: String,  // execute, cross_attestation, mesh_execute, benchmark
    region_id: Option<String>,
    payload: TaskPayload,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
struct TaskPayload {
    #[serde(default)]
    data: Option<String>,  // Hex-encoded data
    #[serde(default)]
    format: Option<String>,  // length_prefix or direct
    #[serde(default)]
    validation_level: Option<String>,  // strict or relaxed
    #[serde(default)]
    batch_size: Option<u32>,
    #[serde(default)]
    thread_count: Option<u32>,
    #[serde(default)]
    duration_seconds: Option<u32>
}

#[derive(Serialize)]
struct TaskSubmitResponse {
    task_id: String,
}

#[derive(Serialize)]
struct TaskStatusResponse {
    task_id: String,
    status: String,
    result: Option<Vec<u8>>,
    error: Option<String>,
}

#[derive(Debug, Serialize, Deserialize)]
struct WorkerListResponse {
    workers: Vec<String>,
}

// TeePair struct is now used from above (with proper serialization for use in handlers)
impl Serialize for TeePair {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: serde::Serializer,
    {
        let mut map = serializer.serialize_map(Some(3))?;
        map.serialize_entry("region_id", &self.region_id)?;
        map.serialize_entry("primary_worker_id", &self.primary_worker_id)?;
        map.serialize_entry("secondary_worker_id", &self.secondary_worker_id)?;
        map.end()
    }
}

#[derive(Debug, Serialize, Deserialize)]
struct Worker {
    id: String,
    enclave_id: Vec<u8>,
    status: u8,
}

// API Handlers
async fn register_worker(
    State(state): State<AppState>,
    Json(req): Json<RegisterWorkerRequest>,
) -> impl IntoResponse {
    println!("Received register_worker request: {:#?}", req);
    
    let mut coordinator = state.write().await;
    
    let attestation = req.attestation.unwrap_or_else(|| "default-attestation".to_string());
    
    let worker_info = WorkerInfo {
        id: req.worker_id.clone(),
        attestation,
        last_heartbeat: 0, // Would use system time in real impl
    };
    
    coordinator.workers.insert(req.worker_id.clone(), worker_info);
    
    println!("Registered worker: {}", req.worker_id);
    
    let response = CoordinatorResponse {
        success: true,
        error: None,
        data: Some(json!({ "worker_id": req.worker_id }))
    };
    
    println!("Sending response: {:#?}", response);
    
    (StatusCode::OK, Json(response))
}

async fn register_tee_pair(
    State(state): State<AppState>,
    Path(worker_id): Path<String>,
    Json(req): Json<RegisterTeePairRequest>,
) -> Json<CoordinatorResponse> {
    let mut coordinator = state.write().await;
    
    // For this endpoint, we assume the region is derived from the worker
    // We'll just use a mock region ID
    let region_id = "mock-region-1".to_string();
    
    let tee_pair = TeePair {
        region_id: region_id.clone(),
        primary_worker_id: req.primary_worker_id.clone(),
        secondary_worker_id: req.secondary_worker_id.clone(),
        attestations: Some(req.attestations.clone()),
    };
    
    // In a real implementation, would check both workers exist
    coordinator.tee_pairs.insert(region_id.clone(), tee_pair.clone());
    
    println!("Registered TEE pair for worker {}: primary={}, secondary={}", 
        worker_id, req.primary_worker_id, req.secondary_worker_id);
        
    Json(CoordinatorResponse {
        success: true,
        error: None,
        data: Some(json!({ "region_id": region_id }))
    })
}

async fn register_tee_pair_by_region(
    State(state): State<AppState>,
    Path(region_id): Path<String>,
    Json(req): Json<RegisterTeePairRequest>,
) -> Json<CoordinatorResponse> {
    let mut coordinator = state.write().await;
    
    let tee_pair = TeePair {
        region_id: region_id.clone(),
        primary_worker_id: req.primary_worker_id.clone(),
        secondary_worker_id: req.secondary_worker_id.clone(),
        attestations: Some(req.attestations.clone()),
    };
    
    // In a real implementation, would check both workers exist
    coordinator.tee_pairs.insert(region_id.clone(), tee_pair.clone());
    
    println!("Registered TEE pair for region {}: primary={}, secondary={}", 
        region_id, req.primary_worker_id, req.secondary_worker_id);
        
    Json(CoordinatorResponse {
        success: true,
        error: None,
        data: Some(json!({ "region_id": region_id }))
    })
}

async fn get_workers(
    State(state): State<AppState>,
) -> Json<CoordinatorResponse> {
    let coordinator = state.read().await;
    let workers = coordinator.workers.keys()
        .map(|id| Worker {
            id: id.clone(),
            enclave_id: vec![0, 1, 2, 3], // Mock enclave ID
            status: 1, // Active status
        })
        .collect::<Vec<_>>();
    
    println!("Returning workers: {:?}", workers);
    
    Json(CoordinatorResponse {
        success: true,
        error: None,
        data: Some(json!(workers))
    })
}

async fn submit_task(
    State(state): State<AppState>,
    Json(req): Json<TaskSubmitRequest>,
) -> Json<CoordinatorResponse> {
    let mut coordinator = state.write().await;
    
    // Generate a task ID
    let task_id = format!("task-{}", coordinator.task_counter);
    coordinator.task_counter += 1;
    
    // Validate task parameters
    let validation_result = validate_task_parameters(&req);
    if let Err(error_msg) = validation_result {
        return Json(CoordinatorResponse {
            success: false,
            error: Some(error_msg),
            data: None
        });
    }
    
    // Convert the client request to an internal task info object
    let task_info = InternalTaskInfo {
        id: task_id.clone(),
        payload: json!({
            "task_type": req.task_type,
            "worker_id": req.worker_id,
            "primary_worker_id": req.primary_worker_id,
            "secondary_worker_id": req.secondary_worker_id,
            "payload": req.payload,
            "region_id": req.region_id,
        }),
        status: "pending".to_string(),
        result: None,
        error: None,
    };
    
    coordinator.tasks.insert(task_id.clone(), task_info);
    
    // Spawn a task to execute the WebAssembly contract
    tokio::spawn({
        let state = state.clone();
        let task_id = task_id.clone();
        let task_type = req.task_type.clone();
        let payload = serde_json::to_value(&req.payload).unwrap_or(json!({}));
        
        async move {
            let start_time = std::time::Instant::now();
            
            let result = match task_type.as_str() {
                "execute" => execute_wasm_contract(
                    req.worker_id.unwrap_or_default(),
                    payload,
                ).await,
                "cross_attestation" => execute_cross_attestation(
                    req.primary_worker_id.unwrap_or_default(),
                    req.secondary_worker_id.unwrap_or_default(),
                    payload,
                ).await,
                "mesh_execute" => execute_mesh_operation(
                    req.primary_worker_id.unwrap_or_default(),
                    req.secondary_worker_id.unwrap_or_default(),
                    payload,
                ).await,
                "benchmark" => run_performance_benchmark(
                    req.worker_id.unwrap_or_default(),
                    payload,
                ).await,
                _ => Err("Unknown task type".to_string())
            };
            
            let elapsed = start_time.elapsed();
            
            // Update task status based on execution result
            let mut coordinator = state.write().await;
            if let Some(task) = coordinator.tasks.get_mut(&task_id) {
                match result {
                    Ok(data) => {
                        task.status = "completed".to_string();
                        task.result = Some(data);
                        println!("Task {} completed successfully in {:?}", task_id, elapsed);
                    },
                    Err(error) => {
                        task.status = "failed".to_string();
                        task.error = Some(error);
                        println!("Task {} failed: {}", task_id, task.error.as_ref().unwrap());
                    }
                }
            }
        }
    });
    
    println!("Submitted task: {}", task_id);
    Json(CoordinatorResponse {
        success: true,
        error: None,
        data: Some(json!({ "task_id": task_id }))
    })
}

async fn get_task_status(
    State(state): State<AppState>,
    Path(task_id): Path<String>,
) -> Json<CoordinatorResponse> {
    let coordinator = state.read().await;
    
    if let Some(internal_task) = coordinator.tasks.get(&task_id) {
        // Extract worker_ids and data from the payload
        let payload = &internal_task.payload;
        
        // Extract or default values from the payload for the Task structure
        let worker_ids = payload.get("worker_ids")
            .and_then(|v| v.as_array())
            .map(|arr| arr.iter().filter_map(|v| v.as_str().map(|s| s.to_string())).collect())
            .unwrap_or_else(|| vec![]);
            
        let data = payload.get("data")
            .and_then(|v| v.as_array())
            .map(|arr| arr.iter().filter_map(|v| v.as_u64().map(|n| n as u8)).collect())
            .unwrap_or_else(|| vec![]);
            
        let region_id = payload.get("region_id")
            .and_then(|v| v.as_str())
            .unwrap_or("default")
            .to_string();
            
        // Create the Task and TaskInfo structures
        let task = Task {
            id: internal_task.id.clone(),
            worker_ids,
            data,
            attestations: vec![],  // Default empty attestations
            timeout: 30000,         // Default timeout
            region_id,
        };
        
        let results = if let Some(result) = &internal_task.result {
            vec![result.clone()]
        } else {
            vec![]
        };
        
        let task_info = TaskInfo {
            task,
            status: internal_task.status.clone(),
            start_time: "2023-01-01T00:00:00Z".to_string(),  // Default timestamp
            end_time: "2023-01-01T00:01:00Z".to_string(),    // Default timestamp
            error: internal_task.error.clone(),
            results,
        };
        
        Json(CoordinatorResponse {
            success: true,
            error: None,
            data: Some(json!(task_info))
        })
    } else {
        Json(CoordinatorResponse {
            success: false,
            error: Some("Task not found".to_string()),
            data: None
        })
    }
}

async fn health_check() -> impl IntoResponse {
    println!("Health check requested");
    (StatusCode::OK, "ok")
}

// Legacy endpoints for MorpheusVM integration
async fn legacy_execute(
    State(state): State<AppState>,
    Json(payload): Json<Value>,
) -> Json<CoordinatorResponse> {
    println!("MorpheusVM called legacy execute endpoint");
    
    // Extract parameters from the legacy format
    let worker_id = payload.get("worker_id")
        .and_then(|v| v.as_str())
        .unwrap_or("sgx1"); // Default to first SGX worker
    
    let data = payload.get("data")
        .and_then(|v| v.as_str())
        .unwrap_or("0x");
    
    let format = payload.get("format")
        .and_then(|v| v.as_str())
        .unwrap_or("direct");
    
    // Create a task submit request from the legacy format
    let req = TaskSubmitRequest {
        id: None,
        worker_id: Some(worker_id.to_string()),
        primary_worker_id: None,
        secondary_worker_id: None,
        task_type: "execute".to_string(),
        region_id: None,
        payload: TaskPayload {
            data: Some(data.to_string()),
            format: Some(format.to_string()),
            validation_level: Some("strict".to_string()),
            batch_size: None,
            thread_count: None,
            duration_seconds: None,
        },
    };
    
    // Forward to the main submit_task handler
    submit_task(State(state), Json(req)).await
}

async fn legacy_execute_dual(
    State(state): State<AppState>,
    Json(payload): Json<Value>,
) -> Json<CoordinatorResponse> {
    println!("MorpheusVM called legacy execute_dual endpoint");
    
    // Extract parameters from the legacy format
    let primary_worker_id = payload.get("primary_worker_id")
        .and_then(|v| v.as_str())
        .unwrap_or("sgx1"); // Default to first SGX worker
    
    let secondary_worker_id = payload.get("secondary_worker_id")
        .and_then(|v| v.as_str())
        .unwrap_or("sev1"); // Default to first SEV worker
    
    let data = payload.get("data")
        .and_then(|v| v.as_str())
        .unwrap_or("0x");
    
    let format = payload.get("format")
        .and_then(|v| v.as_str())
        .unwrap_or("direct");
    
    let dual_mode = payload.get("dual_mode")
        .and_then(|v| v.as_str())
        .unwrap_or("cross_attestation");
    
    // Create a task submit request based on mode
    let task_type = if dual_mode == "mesh" {
        "mesh_execute"
    } else {
        "cross_attestation"
    };
    
    let req = TaskSubmitRequest {
        id: None,
        worker_id: None,
        primary_worker_id: Some(primary_worker_id.to_string()),
        secondary_worker_id: Some(secondary_worker_id.to_string()),
        task_type: task_type.to_string(),
        region_id: None,
        payload: TaskPayload {
            data: Some(data.to_string()),
            format: Some(format.to_string()),
            validation_level: Some("strict".to_string()),
            batch_size: None,
            thread_count: None,
            duration_seconds: None,
        },
    };
    
    // Forward to the main submit_task handler
    submit_task(State(state), Json(req)).await
}

async fn legacy_benchmark(
    State(state): State<AppState>,
    Json(payload): Json<Value>,
) -> Json<CoordinatorResponse> {
    println!("MorpheusVM called legacy benchmark endpoint");
    
    // Extract parameters from the legacy format
    let worker_id = payload.get("worker_id")
        .and_then(|v| v.as_str())
        .unwrap_or("sgx1"); // Default to first SGX worker
    
    let batch_size = payload.get("batch_size")
        .and_then(|v| v.as_u64())
        .unwrap_or(100) as u32;
    
    let thread_count = payload.get("thread_count")
        .and_then(|v| v.as_u64())
        .unwrap_or(8) as u32;
    
    let duration_seconds = payload.get("duration_seconds")
        .and_then(|v| v.as_u64())
        .unwrap_or(5) as u32;
    
    // Create a task submit request from the legacy format
    let req = TaskSubmitRequest {
        id: None,
        worker_id: Some(worker_id.to_string()),
        primary_worker_id: None,
        secondary_worker_id: None,
        task_type: "benchmark".to_string(),
        region_id: None,
        payload: TaskPayload {
            data: None,
            format: None,
            validation_level: None,
            batch_size: Some(batch_size),
            thread_count: Some(thread_count),
            duration_seconds: Some(duration_seconds),
        },
    };
    
    // Forward to the main submit_task handler
    submit_task(State(state), Json(req)).await
}

// Parameter validation function
fn validate_task_parameters(req: &TaskSubmitRequest) -> Result<(), String> {
    match req.task_type.as_str() {
        "execute" => {
            if req.worker_id.is_none() {
                return Err("worker_id is required for execute tasks".to_string());
            }
            
            validate_payload_data(&req.payload)
        },
        "cross_attestation" => {
            if req.primary_worker_id.is_none() || req.secondary_worker_id.is_none() {
                return Err("primary_worker_id and secondary_worker_id are required for cross_attestation tasks".to_string());
            }
            
            validate_payload_data(&req.payload)
        },
        "mesh_execute" => {
            if req.primary_worker_id.is_none() || req.secondary_worker_id.is_none() {
                return Err("primary_worker_id and secondary_worker_id are required for mesh_execute tasks".to_string());
            }
            
            validate_payload_data(&req.payload)
        },
        "benchmark" => {
            if req.worker_id.is_none() {
                return Err("worker_id is required for benchmark tasks".to_string());
            }
            
            if req.payload.batch_size.is_none() || req.payload.thread_count.is_none() || req.payload.duration_seconds.is_none() {
                return Err("batch_size, thread_count, and duration_seconds are required for benchmark tasks".to_string());
            }
            
            Ok(())
        },
        _ => Err(format!("Unknown task type: {}", req.task_type)),
    }
}

// Validate payload data based on parameter format
fn validate_payload_data(payload: &TaskPayload) -> Result<(), String> {
    let data = match &payload.data {
        Some(data_str) if data_str.starts_with("0x") => &data_str[2..],
        Some(data_str) => data_str,
        None => return Err("payload.data is required".to_string()),
    };
    
    // Validate hex encoding
    if data.chars().any(|c| !c.is_ascii_hexdigit()) {
        return Err("payload.data must be hex-encoded".to_string());
    }
    
    // Validate parameter format
    let format = payload.format.as_deref().unwrap_or("direct");
    match format {
        "length_prefix" => {
            // For length-prefixed, we expect at least 8 chars (4 bytes) for the length prefix
            if data.len() < 8 {
                return Err("Length-prefixed data must be at least 4 bytes".to_string());
            }
            
            // Parse the length prefix (first 4 bytes)
            let length_bytes = hex::decode(&data[0..8])
                .map_err(|_| "Invalid hex in length prefix".to_string())?;
            
            if length_bytes.len() != 4 {
                return Err("Length prefix must be exactly 4 bytes".to_string());
            }
            
            let expected_length = u32::from_be_bytes([length_bytes[0], length_bytes[1], length_bytes[2], length_bytes[3]]) as usize;
            let actual_data_length = (data.len() - 8) / 2; // Convert from hex chars to bytes
            
            if expected_length != actual_data_length {
                return Err(format!("Length prefix ({}) does not match actual data length ({})", expected_length, actual_data_length));
            }
        },
        "direct" => {
            // For direct format, just ensure we have some data
            if data.is_empty() {
                return Err("Direct format data cannot be empty".to_string());
            }
        },
        _ => return Err(format!("Unknown parameter format: {}", format)),
    }
    
    Ok(())
}

// WebAssembly contract execution implementation
async fn execute_wasm_contract(worker_id: String, payload: Value) -> Result<Vec<u8>, String> {
    // Extract parameters
    let data = payload.get("data")
        .and_then(|v| v.as_str())
        .ok_or_else(|| "Missing data parameter".to_string())?;
    
    let format = payload.get("format")
        .and_then(|v| v.as_str())
        .unwrap_or("direct");
    
    let validation_level = payload.get("validation_level")
        .and_then(|v| v.as_str())
        .unwrap_or("strict");
    
    println!("Executing WebAssembly contract on {} with {} data format and {} validation", worker_id, format, validation_level);
    
    // In a production implementation, we would:
    // 1. Load the WebAssembly module
    // 2. Process parameters based on format (length-prefixed or direct)
    // 3. Execute the WebAssembly module
    // 4. Return the result
    
    // For this mock, we'll simulate the execution
    let hex_data = if data.starts_with("0x") { &data[2..] } else { data };
    let input_data = hex::decode(hex_data).map_err(|e| format!("Invalid hex data: {}", e))?;
    
    // Process based on parameter format
    let processed_data = match format {
        "length_prefix" => {
            if input_data.len() < 4 {
                return Err("Length-prefixed data must be at least 4 bytes".to_string());
            }
            
            // Extract the length prefix and data
            let length_bytes = &input_data[0..4];
            let length = u32::from_be_bytes([length_bytes[0], length_bytes[1], length_bytes[2], length_bytes[3]]) as usize;
            
            // Validate length against actual data
            if input_data.len() < 4 + length {
                return Err(format!("Data length ({}) less than specified in prefix ({})", input_data.len() - 4, length));
            }
            
            // Extract just the data portion
            input_data[4..4+length].to_vec()
        },
        "direct" => {
            // Use the data directly
            input_data
        },
        _ => return Err(format!("Unsupported parameter format: {}", format)),
    };
    
    // Simulate WebAssembly execution (in a real implementation, we would execute the contract)
    // For now, just echo back the processed data with a success marker
    let mut result = Vec::new();
    result.extend_from_slice(b"SUCCESS:");
    result.extend_from_slice(&processed_data);
    
    // Add timestamp for latency tracking
    let timestamp = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis();
    
    result.extend_from_slice(format!("\nTimestamp: {}", timestamp).as_bytes());
    
    Ok(result)
}

async fn execute_cross_attestation(primary_id: String, secondary_id: String, payload: Value) -> Result<Vec<u8>, String> {
    println!("Executing cross-attestation between {} and {}", primary_id, secondary_id);
    
    // In a production implementation, we would:
    // 1. Generate attestation for the primary TEE (e.g., SGX)
    // 2. Send the attestation to the secondary TEE (e.g., SEV)
    // 3. Verify the attestation in the secondary TEE
    // 4. Generate a secondary attestation from the secondary TEE
    // 5. Send back to primary and verify
    // 6. Execute the actual operation
    
    // For this mock, we'll simulate cross-attestation
    tokio::time::sleep(tokio::time::Duration::from_millis(50)).await;
    
    // Extract parameters (same as execute_wasm_contract)
    let data = payload.get("data")
        .and_then(|v| v.as_str())
        .ok_or_else(|| "Missing data parameter".to_string())?;
    
    let format = payload.get("format")
        .and_then(|v| v.as_str())
        .unwrap_or("direct");
    
    // Parse the input data
    let hex_data = if data.starts_with("0x") { &data[2..] } else { data };
    let input_data = hex::decode(hex_data).map_err(|e| format!("Invalid hex data: {}", e))?;
    
    // Create a mock cross-attestation result
    let mut result = Vec::new();
    result.extend_from_slice(format!("CROSS-ATTESTATION SUCCESS: {} <-> {}\n", primary_id, secondary_id).as_bytes());
    result.extend_from_slice(b"Input data: ");
    result.extend_from_slice(&input_data);
    result.extend_from_slice(b"\nFormat: ");
    result.extend_from_slice(format.as_bytes());
    
    // Add timestamp for latency tracking
    let timestamp = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis();
    
    result.extend_from_slice(format!("\nTimestamp: {}", timestamp).as_bytes());
    
    Ok(result)
}

async fn execute_mesh_operation(primary_id: String, secondary_id: String, payload: Value) -> Result<Vec<u8>, String> {
    println!("Executing mesh operation between {} and {}", primary_id, secondary_id);
    
    // In a production implementation, we would:
    // 1. Send the request to the primary TEE
    // 2. Primary TEE communicates directly with secondary TEE (mesh)
    // 3. Both TEEs execute in parallel and compare results
    // 4. Return the consensus result
    
    // For this mock, we'll simulate mesh execution with dual paths
    tokio::time::sleep(tokio::time::Duration::from_millis(30)).await;
    
    // Extract parameters (same as other functions)
    let data = payload.get("data")
        .and_then(|v| v.as_str())
        .ok_or_else(|| "Missing data parameter".to_string())?;
    
    let format = payload.get("format")
        .and_then(|v| v.as_str())
        .unwrap_or("direct");
    
    // Parse the input data
    let hex_data = if data.starts_with("0x") { &data[2..] } else { data };
    let input_data = hex::decode(hex_data).map_err(|e| format!("Invalid hex data: {}", e))?;
    
    // Create a mock mesh execution result
    let mut result = Vec::new();
    result.extend_from_slice(format!("MESH EXECUTION SUCCESS: {} + {}\n", primary_id, secondary_id).as_bytes());
    result.extend_from_slice(b"Input data: ");
    result.extend_from_slice(&input_data);
    result.extend_from_slice(b"\nFormat: ");
    result.extend_from_slice(format.as_bytes());
    result.extend_from_slice(b"\nConsensus: TRUE");
    
    // Add timestamp for latency tracking
    let timestamp = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis();
    
    result.extend_from_slice(format!("\nTimestamp: {}", timestamp).as_bytes());
    
    Ok(result)
}

async fn run_performance_benchmark(worker_id: String, payload: Value) -> Result<Vec<u8>, String> {
    // Extract benchmark parameters
    let batch_size = payload.get("batch_size")
        .and_then(|v| v.as_u64())
        .unwrap_or(1) as usize;
    
    let thread_count = payload.get("thread_count")
        .and_then(|v| v.as_u64())
        .unwrap_or(1) as usize;
    
    let duration_secs = payload.get("duration_seconds")
        .and_then(|v| v.as_u64())
        .unwrap_or(5) as u64;
    
    println!("Running performance benchmark on {} with batch_size={}, thread_count={}, duration={}s", 
        worker_id, batch_size, thread_count, duration_secs);
    
    // In a production implementation, we would:
    // 1. Generate batches of transactions
    // 2. Execute them in parallel with the specified thread count
    // 3. Measure throughput and latency
    // 4. Return benchmark results
    
    // For this mock, we'll simulate a benchmark
    let start_time = std::time::Instant::now();
    
    // Simulate the benchmark by sleeping for the expected duration
    tokio::time::sleep(tokio::time::Duration::from_secs(duration_secs)).await;
    
    // Calculate simulated throughput (assume ~2,000 TPS per thread with batch size 100)
    let base_tps = 20.0; // Base TPS per thread without batching
    let batch_multiplier = batch_size as f64 / 10.0; // Effect of batching
    let simulated_total_tps = (base_tps * thread_count as f64 * batch_multiplier) as u64;
    let total_tx = simulated_total_tps * duration_secs;
    
    // Calculate simulated p50, p95, p99 latencies
    let p50_latency_ms = 20.0 / batch_multiplier;
    let p95_latency_ms = 50.0 / batch_multiplier;
    let p99_latency_ms = 80.0 / batch_multiplier;
    
    // Create a benchmark result
    let results = serde_json::json!({
        "worker_id": worker_id,
        "benchmark_params": {
            "batch_size": batch_size,
            "thread_count": thread_count,
            "duration_seconds": duration_secs
        },
        "results": {
            "total_transactions": total_tx,
            "transactions_per_second": simulated_total_tps,
            "latency_ms": {
                "p50": p50_latency_ms,
                "p95": p95_latency_ms,
                "p99": p99_latency_ms
            },
            "duration": format!("{:?}", start_time.elapsed())
        }
    });
    
    Ok(results.to_string().into_bytes())
}

#[tokio::main]
async fn main() {
    // Create initial state
    let coordinator_state = CoordinatorState {
        workers: HashMap::new(),
        tee_pairs: HashMap::new(),
        tasks: HashMap::new(),
        task_counter: 0,
    };
    
    let shared_state = Arc::new(RwLock::new(coordinator_state));
    
    // Build our application with routes
    let app = Router::new()
        .route("/workers", post(register_worker))
        .route("/workers", get(get_workers))
        .route("/workers/register", post(register_worker)) // Add /workers/register endpoint
        .route("/workers/:worker_id/tee_pairs", post(register_tee_pair))
        .route("/regions/:region_id/pairs/register", post(register_tee_pair_by_region)) // Add region-based registration
        .route("/tasks", post(submit_task))
        .route("/tasks/submit", post(submit_task))
        .route("/tasks/:task_id", get(get_task_status))
        .route("/tasks/:task_id/status", get(get_task_status))
        // Add MorpheusVM integration endpoints
        .route("/execute", post(legacy_execute))
        .route("/execute_dual", post(legacy_execute_dual))
        .route("/benchmark", post(legacy_benchmark))
        .route("/health", get(health_check))
        .with_state(shared_state);
        
    println!("Routes configuration complete!");
    
    // Run our app
    let addr = "0.0.0.0:9080";
    println!("Mock coordinator listening on {}", addr);
    
    axum::Server::bind(&addr.parse().unwrap())
        .serve(app.into_make_service())
        .await
        .unwrap();
}
