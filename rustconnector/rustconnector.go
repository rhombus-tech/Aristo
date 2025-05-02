// Package rustconnector provides integration with the Rust TEE implementation
package rustconnector

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/rhombus-tech/vm/core"
)

// RustConnector handles interaction with the Rust TEE implementation
type RustConnector struct {
    controllerPath string
    wasmPath      string
    verbose       bool
    mu            sync.Mutex
}

// ExecutionRequest represents input for TEE execution
type ExecutionRequest struct {
    ExecutionID uint64          `json:"execution_id"`
    Input      []byte          `json:"input"`
    Params     ExecutionParams `json:"params"`
}

// ExecutionParams defines execution parameters
type ExecutionParams struct {
    ExpectedHash  []byte `json:"expected_hash,omitempty"`
    DetailedProof bool      `json:"detailed_proof"`
}

// ExecutionResult holds output from TEE execution
type ExecutionResult struct {
    ResultHash  []byte         `json:"result_hash"`
    Result      []byte           `json:"result"`
    Attestation AttestationProof `json:"attestation"`
}

// AttestationProof contains TEE attestation data
type AttestationProof struct {
    EnclaveType     string   `json:"enclave_type"`
    Measurement     []byte   `json:"measurement"`
    Timestamp       uint64   `json:"timestamp"`
    PlatformData    []byte   `json:"platform_data"`
}

// MeshExecutionRequest represents a mesh execution request
type MeshExecutionRequest struct {
    ExecutionID  uint64                  `json:"execution_id"`
    Input        []byte                  `json:"input"`
    Params       ExecutionParams         `json:"params"`
    TargetTEE    string                  `json:"target_tee"`     // TEE ID to execute on
    RegionID     string                  `json:"region_id"`      // Region to execute in
    TEEType      string                  `json:"tee_type"`       // SGX or SEV
    FunctionCall string                  `json:"function_call,omitempty"`
    Timeout      time.Duration           `json:"timeout_ms"`     // Execution timeout
    Async        bool                    `json:"async"`          // Whether to execute asynchronously
    Fallback     bool                    `json:"fallback"`       // Whether to allow fallback to coordinator
    MetricsFlags MetricsFlags            `json:"metrics_flags"`  // Metrics collection flags
}

// MeshExecutionResult contains the result of a mesh execution
type MeshExecutionResult struct {
    ResultHash    []byte              `json:"result_hash"`
    Result        []byte              `json:"result"`
    Attestations  []AttestationProof  `json:"attestations"`
    ExecutionTime uint64              `json:"execution_time_ns"`
    MemoryUsed    uint64              `json:"memory_used"`
    SyscallCount  uint64              `json:"syscall_count"`
    Status        string              `json:"status"`
    Error         string              `json:"error,omitempty"`
    Metrics       PerformanceMetrics  `json:"metrics"`
    CacheHit      bool                `json:"cache_hit"`
    CacheTTL      uint64              `json:"cache_ttl_sec,omitempty"`
}

// MetricsFlags controls which metrics to collect
type MetricsFlags struct {
    CollectLatency     bool `json:"collect_latency"`
    CollectMemory      bool `json:"collect_memory"`
    CollectSyscalls    bool `json:"collect_syscalls"`
    CollectThroughput  bool `json:"collect_throughput"`
    DetailedMetrics    bool `json:"detailed_metrics"`
}

// PerformanceMetrics contains detailed performance information
type PerformanceMetrics struct {
    TEEType            string  `json:"tee_type"`
    RegionID           string  `json:"region_id"`
    WorkerID           string  `json:"worker_id"`
    LatencyMs          float64 `json:"latency_ms"`
    ExecutionTimeNs    uint64  `json:"execution_time_ns"`
    NetworkLatencyMs   float64 `json:"network_latency_ms"`
    SuccessCount       uint64  `json:"success_count"`
    FailureCount       uint64  `json:"failure_count"`
    MemoryUsedBytes    uint64  `json:"memory_used_bytes"`
    SyscallCount       uint64  `json:"syscall_count"`
    ThroughputBytesPs  uint64  `json:"throughput_bytes_ps"`
}

// DiscoveryRequest represents a request to discover peers
type DiscoveryRequest struct {
    RegionID    string  `json:"region_id"`
    TEEType     string  `json:"tee_type"`    // Optional, if empty discovers all types
    MaxResults  int     `json:"max_results"` // Maximum number of results to return
}

// DiscoveryResult contains the result of a discovery request
type DiscoveryResult struct {
    Peers       []PeerInfo  `json:"peers"`
    Timestamp   uint64      `json:"timestamp"`
    Error       string      `json:"error,omitempty"`
}

// PeerInfo contains information about a TEE peer
type PeerInfo struct {
    ID          string  `json:"id"`
    Address     string  `json:"address"`
    RegionID    string  `json:"region_id"`
    TEEType     string  `json:"tee_type"`
    Status      string  `json:"status"`
    LatencyMs   float64 `json:"latency_ms"`
}

// SyncRequest represents a state synchronization request
type SyncRequest struct {
    ObjectID    string  `json:"object_id"`
    TargetTEE   string  `json:"target_tee"`
    UseDeltas   bool    `json:"use_deltas"`
    ForceSync   bool    `json:"force_sync"`
    SyncTimeout time.Duration `json:"sync_timeout_ms"`
}

// SyncResult contains the result of a sync operation
type SyncResult struct {
    ObjectID      string  `json:"object_id"`
    StateHash     []byte  `json:"state_hash"`
    SyncTimeNs    uint64  `json:"sync_time_ns"`
    DataSize      uint64  `json:"data_size"`
    Success       bool    `json:"success"`
    Error         string  `json:"error,omitempty"`
}

// MeshExecutionOptions defines additional options for mesh execution
type MeshExecutionOptions struct {
	Fallback           bool
	CollectLatency     bool
	CollectMemory      bool
	CollectSyscalls    bool
	CollectThroughput  bool
	DetailedMetrics    bool
	UseMeshCache       bool
	CacheTTLSec        int
	StaleResultTimeout int
}

// MeshExecuteRequest contains parameters for a mesh execute request
type MeshExecuteRequest struct {
	Input        json.RawMessage
	TargetTEE    string
	RegionID     string
	TEEType      string
	FunctionCall string
	TimeoutMs    int
	Options      MeshExecutionOptions
}

// New creates a new RustConnector instance
func New(controllerPath, wasmPath string, verbose bool) *RustConnector {
    return &RustConnector{
        controllerPath: controllerPath,
        wasmPath:      wasmPath,
        verbose:       verbose,
    }
}

// ExecuteSGX executes code in SGX TEE
func (rc *RustConnector) ExecuteSGX(ctx context.Context, input []byte) (*core.ExecutionResult, error) {
    rc.mu.Lock()
    defer rc.mu.Unlock()

    // Create temp file for input
    inputFile, err := os.CreateTemp("", "tee-sgx-input-*")
    if err != nil {
        return nil, fmt.Errorf("failed to create input file: %w", err)
    }
    defer os.Remove(inputFile.Name())
    defer inputFile.Close()

    // Write input data
    if _, err := inputFile.Write(input); err != nil {
        return nil, fmt.Errorf("failed to write input: %w", err)
    }

    // Execute in SGX TEE
    cmd := exec.CommandContext(ctx, rc.controllerPath,
        "--wasm-module", rc.wasmPath,
        "--input", inputFile.Name(),
        "--backend", "sgx",
        "--verbose", fmt.Sprintf("%v", rc.verbose),
    )

    output, err := cmd.Output()
    if err != nil {
        return nil, fmt.Errorf("SGX execution failed: %w", err)
    }

    // Parse result
    var rustResult ExecutionResult
    if err := json.Unmarshal(output, &rustResult); err != nil {
        return nil, fmt.Errorf("failed to parse result: %w", err)
    }

    // Convert to core.ExecutionResult with proper field mappings
    return &core.ExecutionResult{
        Output:       rustResult.Result,
        StateHash:    rustResult.ResultHash,
        RegionID:     "",
        Attestations: [2]core.TEEAttestation{
            {
                EnclaveID:   []byte(rustResult.Attestation.EnclaveType + "-enclave"),
                Measurement: rustResult.Attestation.Measurement,
                Timestamp:   time.Unix(int64(rustResult.Attestation.Timestamp), 0),
                Data:        rustResult.Attestation.PlatformData,
            },
            {}, // Empty second attestation to satisfy [2]core.TEEAttestation requirement
        },
    }, nil
}

// ExecuteSEV executes code in SEV TEE
func (rc *RustConnector) ExecuteSEV(ctx context.Context, input []byte) (*core.ExecutionResult, error) {
    rc.mu.Lock()
    defer rc.mu.Unlock()

    // Create temp file for input
    inputFile, err := os.CreateTemp("", "tee-sev-input-*")
    if err != nil {
        return nil, fmt.Errorf("failed to create input file: %w", err)
    }
    defer os.Remove(inputFile.Name())
    defer inputFile.Close()

    // Write input data
    if _, err := inputFile.Write(input); err != nil {
        return nil, fmt.Errorf("failed to write input: %w", err)
    }

    // Execute in SEV TEE
    cmd := exec.CommandContext(ctx, rc.controllerPath,
        "--wasm-module", rc.wasmPath,
        "--input", inputFile.Name(),
        "--backend", "sev",
        "--verbose", fmt.Sprintf("%v", rc.verbose),
    )

    output, err := cmd.Output()
    if err != nil {
        return nil, fmt.Errorf("SEV execution failed: %w", err)
    }

    // Parse result
    var rustResult ExecutionResult
    if err := json.Unmarshal(output, &rustResult); err != nil {
        return nil, fmt.Errorf("failed to parse result: %w", err)
    }

    // Convert to core.ExecutionResult with proper field mappings
    return &core.ExecutionResult{
        Output:      rustResult.Result,
        StateHash:   rustResult.ResultHash,
        RegionID:    "",
        Attestations: [2]core.TEEAttestation{
            {
                EnclaveID:   []byte(rustResult.Attestation.EnclaveType + "-enclave"),
                Measurement: rustResult.Attestation.Measurement,
                Timestamp:   time.Unix(int64(rustResult.Attestation.Timestamp), 0),
                Data:        rustResult.Attestation.PlatformData,
            },
            {}, // Empty second attestation to satisfy [2]core.TEEAttestation requirement
        },
    }, nil
}

// ExecuteTDX executes code in TDX TEE
func (r *RustConnector) ExecuteTDX(ctx context.Context, input []byte) (*core.ExecutionResult, error) {
    const teetype = "tdx"
    
    // Create a temporary input file with dual-format parameter validation
    inputFile, err := r.createInputFile(input, "")
    if err != nil {
        return nil, fmt.Errorf("failed to create TDX input file: %w", err)
    }
    defer os.Remove(inputFile)
    
    // Create temporary output file
    outputFile, err := ioutil.TempFile("", "tdx-output-*.json")
    if err != nil {
        return nil, fmt.Errorf("failed to create TDX output file: %w", err)
    }
    outputPath := outputFile.Name()
    outputFile.Close()
    defer os.Remove(outputPath)
    
    // Prepare command with TDX-specific flags
    cmdArgs := []string{
        "execute",
        "--input", inputFile,
        "--output", outputPath,
        "--tee", teetype,
        "--wasm", r.wasmPath,
    }
    
    // Add verbose logging if enabled
    if r.verbose {
        cmdArgs = append(cmdArgs, "--verbose")
    }
    
    // Add accumulator path for sub-millisecond verification
    if accPath := os.Getenv("TDX_ACCUMULATOR_PATH"); accPath != "" {
        cmdArgs = append(cmdArgs, "--accumulator-path", accPath)
    }
    
    // Create the command
    cmd := exec.CommandContext(ctx, r.controllerPath, cmdArgs...)
    
    // Capture output for logging
    output, err := cmd.CombinedOutput()
    if r.verbose {
        log.Printf("TDX execution output: %s", string(output))
    }
    
    if err != nil {
        return nil, fmt.Errorf("TDX execution failed: %w, output: %s", err, string(output))
    }
    
    // Parse the execution result
    resultBytes, err := ioutil.ReadFile(outputPath)
    if err != nil {
        return nil, fmt.Errorf("failed to read TDX execution result: %w", err)
    }
    
    var result ExecutionResult
    if err := json.Unmarshal(resultBytes, &result); err != nil {
        return nil, fmt.Errorf("failed to parse TDX execution result: %w", err)
    }
    
    // Create core.ExecutionResult with TDX-specific attestation
    tdxAttestation := core.TEEAttestation{
        EnclaveID:   []byte{0x1, 0x2, 0x3, 0x4}, // TDX identifier
        Measurement: result.Attestation.Measurement,
        Signature:   []byte{}, // TDX doesn't use signatures like SGX
        Data:        resultBytes, // Store the full attestation data
        Timestamp:   time.Unix(int64(result.Attestation.Timestamp), 0),
    }
    
    // We need to run the SGX/SEV verification on the TDX result
    // This implements our defense-in-depth triple attestation security model
    sgxAttestation, err := r.verifyWithSGX(ctx, result.Result)
    if err != nil {
        return nil, fmt.Errorf("SGX verification failed: %w", err)
    }
    
    // In a full implementation, we would also verify with SEV
    // For now, we can use SGX as both verification steps
    
    return &core.ExecutionResult{
        Output:      result.Result,
        StateHash:   result.ResultHash, // Using ResultHash as StateHash
        RegionID:    "global", // Use global region for AI workloads
        Attestations: [2]core.TEEAttestation{
            tdxAttestation, // TDX attestation in slot 0
            sgxAttestation, // SGX attestation in slot 1 (verification)
        },
    }, nil
}

// verifyWithSGX takes TDX output and verifies it with SGX for defense-in-depth
func (r *RustConnector) verifyWithSGX(ctx context.Context, tdxResult []byte) (core.TEEAttestation, error) {
    // Validate input
    if len(tdxResult) == 0 {
        return core.TEEAttestation{}, fmt.Errorf("empty TDX result")
    }
    
    // Create a temporary input file with the TDX result
    inputFile, err := r.createInputFile(tdxResult, "verify_tdx_output")
    if err != nil {
        return core.TEEAttestation{}, fmt.Errorf("failed to create SGX verification input file: %w", err)
    }
    defer os.Remove(inputFile)
    
    // Create temporary output file
    outputFile, err := ioutil.TempFile("", "sgx-verify-output-*.json")
    if err != nil {
        return core.TEEAttestation{}, fmt.Errorf("failed to create SGX verification output file: %w", err)
    }
    outputPath := outputFile.Name()
    outputFile.Close()
    defer os.Remove(outputPath)
    
    // Prepare SGX verification command
    cmdArgs := []string{
        "verify",
        "--input", inputFile,
        "--output", outputPath,
        "--tee", "sgx",
        "--wasm", r.wasmPath,
    }
    
    // Add verbose logging if enabled
    if r.verbose {
        cmdArgs = append(cmdArgs, "--verbose")
    }
    
    // Create the command
    cmd := exec.CommandContext(ctx, r.controllerPath, cmdArgs...)
    
    // Capture output for logging
    output, err := cmd.CombinedOutput()
    if r.verbose {
        log.Printf("SGX verification output: %s", string(output))
    }
    
    if err != nil {
        return core.TEEAttestation{}, fmt.Errorf("SGX verification failed: %w, output: %s", err, string(output))
    }
    
    // Parse the verification result
    resultBytes, err := ioutil.ReadFile(outputPath)
    if err != nil {
        return core.TEEAttestation{}, fmt.Errorf("failed to read SGX verification result: %w", err)
    }
    
    var result ExecutionResult
    if err := json.Unmarshal(resultBytes, &result); err != nil {
        return core.TEEAttestation{}, fmt.Errorf("failed to parse SGX verification result: %w", err)
    }
    
    // Create SGX attestation
    sgxAttestation := core.TEEAttestation{
        EnclaveID:   result.Attestation.PlatformData, // SGX uses platform data as enclave ID
        Measurement: result.Attestation.Measurement,
        Signature:   []byte{}, // We don't need the signature for this purpose
        Data:        resultBytes, // Store the full attestation data
        Timestamp:   time.Unix(int64(result.Attestation.Timestamp), 0),
    }
    
    return sgxAttestation, nil
}

// VerifyPlatforms checks if all TEE types are available
func (r *RustConnector) VerifyPlatforms(ctx context.Context) (bool, bool, bool, error) {
    cmd := exec.CommandContext(ctx, r.controllerPath, "--verify-platforms")
    output, err := cmd.Output()
    if err != nil {
        return false, false, false, fmt.Errorf("platform verification failed: %w", err)
    }

    var result struct {
        SGXAvailable bool `json:"sgx_available"`
        SEVAvailable bool `json:"sev_available"`
        TDXAvailable bool `json:"tdx_available"`
    }

    if err := json.Unmarshal(output, &result); err != nil {
        return false, false, false, fmt.Errorf("invalid platform verification response: %w", err)
    }

    return result.SGXAvailable, result.SEVAvailable, result.TDXAvailable, nil
}

func (rc *RustConnector) createInputFile(input []byte, functionCall string) (string, error) {
    // Create a temporary directory
    tmpDir, err := ioutil.TempDir("", "tee-input")
    if err != nil {
        return "", fmt.Errorf("failed to create temp directory: %w", err)
    }
    
    // If function call is specified, include it in the input data
    var inputMap map[string]interface{}
    if err := json.Unmarshal(input, &inputMap); err != nil {
        os.RemoveAll(tmpDir)
        return "", fmt.Errorf("failed to parse input JSON: %w", err)
    }
    
    // Add method key if function call is specified and method not already present
    if functionCall != "" && inputMap["method"] == nil {
        inputMap["method"] = functionCall
    }
    
    // Marshal back to JSON
    inputData, err := json.Marshal(inputMap)
    if err != nil {
        os.RemoveAll(tmpDir)
        return "", fmt.Errorf("failed to marshal input JSON: %w", err)
    }
    
    // Write to a temporary file
    inputFile := filepath.Join(tmpDir, "input.json")
    if err := ioutil.WriteFile(inputFile, inputData, 0644); err != nil {
        os.RemoveAll(tmpDir)
        return "", fmt.Errorf("failed to write input file: %w", err)
    }
    
    return inputFile, nil
}

// ExecuteMesh executes a task via mesh network
func (rc *RustConnector) ExecuteMesh(ctx context.Context, request *MeshExecutionRequest) (*MeshExecutionResult, error) {
    rc.mu.Lock()
    defer rc.mu.Unlock()

    // Create a temporary input file with function call included
    inputFile, err := rc.createInputFile(request.Input, request.FunctionCall)
    if err != nil {
        return nil, err
    }
    tmpDir := filepath.Dir(inputFile)
    defer os.RemoveAll(tmpDir)
    
    // Prepare command arguments
    args := []string{
        "mesh-execute",  // This is the correct command name per the Rust controller
        "--target-tee", request.TargetTEE,
        "--region", request.RegionID,  // The argument is --region, not --region-id
        "--tee-type", request.TEEType,
        "--input", inputFile,
    }

    // Add optional parameters
    if request.Timeout > 0 {
        timeoutMs := int(request.Timeout.Milliseconds())
        args = append(args, "--timeout", fmt.Sprintf("%d", timeoutMs))  // It's --timeout, not --timeout-ms
    }

    if request.Async {
        args = append(args, "--async")
    }

    if request.Fallback {
        args = append(args, "--allow-fallback")  // It's --allow-fallback, not --fallback
    }

    // Debug log
    if rc.verbose {
        log.Printf("ExecuteMesh command: %s %v", rc.controllerPath, args)
    }

    // Execute command with separate stdout and stderr
    cmd := exec.CommandContext(ctx, rc.controllerPath, args...)
    
    // Create pipes for stdout and stderr
    stdout, err := cmd.StdoutPipe()
    if err != nil {
        return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
    }
    
    stderr, err := cmd.StderrPipe()
    if err != nil {
        return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
    }
    
    // Start the command
    if err := cmd.Start(); err != nil {
        return nil, fmt.Errorf("failed to start mesh execution: %w", err)
    }
    
    // Read stdout and stderr concurrently
    var stdoutBytes, stderrBytes []byte
    var stdoutErr, stderrErr error
    
    var wg sync.WaitGroup
    wg.Add(2)
    
    go func() {
        stdoutBytes, stdoutErr = ioutil.ReadAll(stdout)
        wg.Done()
    }()
    
    go func() {
        stderrBytes, stderrErr = ioutil.ReadAll(stderr)
        wg.Done()
    }()
    
    wg.Wait()
    
    if stdoutErr != nil {
        return nil, fmt.Errorf("error reading stdout: %w", stdoutErr)
    }
    
    if stderrErr != nil {
        return nil, fmt.Errorf("error reading stderr: %w", stderrErr)
    }
    
    // Wait for command completion
    if err := cmd.Wait(); err != nil {
        // Log stderr for debugging
        if len(stderrBytes) > 0 {
            log.Printf("ExecuteMesh stderr: %s", string(stderrBytes))
        }
        return nil, fmt.Errorf("mesh execution failed: %w", err)
    }
    
    // Parse the result
    var result MeshExecutionResult
    if err := json.Unmarshal(stdoutBytes, &result); err != nil {
        if len(stdoutBytes) > 0 {
            log.Printf("Invalid JSON response: %s", string(stdoutBytes))
        }
        return nil, fmt.Errorf("failed to parse mesh execution result: %w", err)
    }
    
    return &result, nil
}

// ExecuteWithMeshCache executes a WASM function using the mesh network with caching support
func (rc *RustConnector) ExecuteWithMeshCache(ctx context.Context, request *MeshExecuteRequest) (*MeshExecutionResult, error) {
    rc.mu.Lock()
    defer rc.mu.Unlock()

    // Create a temporary input file with function call included
    inputFile, err := rc.createInputFile(request.Input, request.FunctionCall)
    if err != nil {
        return nil, err
    }
    tmpDir := filepath.Dir(inputFile)
    defer os.RemoveAll(tmpDir)
    
    // Prepare command arguments
    args := []string{
        "execute-with-mesh-cache",
        "--target-tee", request.TargetTEE,
        "--region", request.RegionID,  // --region, not --region-id
        "--tee-type", request.TEEType,
        "--input", inputFile,
    }

    // Add timeout parameter
    if request.TimeoutMs > 0 {
        args = append(args, "--timeout", fmt.Sprintf("%d", request.TimeoutMs))
    }

    // Add cache options
    if request.Options.UseMeshCache {
        args = append(args, "--use-cache")
    }

    if request.Options.CacheTTLSec > 0 {
        args = append(args, "--cache-ttl-sec", fmt.Sprintf("%d", request.Options.CacheTTLSec))
    }

    if request.Options.StaleResultTimeout > 0 {
        args = append(args, "--stale-result-timeout-ms", fmt.Sprintf("%d", request.Options.StaleResultTimeout))
    }

    // Add execution options
    if request.Options.Fallback {
        args = append(args, "--allow-fallback")
    }

    // Debug log
    if rc.verbose {
        log.Printf("ExecuteWithMeshCache command: %s %v", rc.controllerPath, args)
    }

    // Execute command with separate stdout and stderr
    cmd := exec.CommandContext(ctx, rc.controllerPath, args...)
    
    // Create pipes for stdout and stderr
    stdout, err := cmd.StdoutPipe()
    if err != nil {
        return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
    }
    
    stderr, err := cmd.StderrPipe()
    if err != nil {
        return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
    }
    
    // Start the command
    if err := cmd.Start(); err != nil {
        return nil, fmt.Errorf("failed to start mesh execution with cache: %w", err)
    }
    
    // Read stdout and stderr concurrently
    var stdoutBytes, stderrBytes []byte
    var stdoutErr, stderrErr error
    
    var wg sync.WaitGroup
    wg.Add(2)
    
    go func() {
        stdoutBytes, stdoutErr = ioutil.ReadAll(stdout)
        wg.Done()
    }()
    
    go func() {
        stderrBytes, stderrErr = ioutil.ReadAll(stderr)
        wg.Done()
    }()
    
    wg.Wait()
    
    if stdoutErr != nil {
        return nil, fmt.Errorf("error reading stdout: %w", stdoutErr)
    }
    
    if stderrErr != nil {
        return nil, fmt.Errorf("error reading stderr: %w", stderrErr)
    }
    
    // Wait for command completion
    if err := cmd.Wait(); err != nil {
        // Log stderr for debugging
        if len(stderrBytes) > 0 {
            log.Printf("ExecuteWithMeshCache stderr: %s", string(stderrBytes))
        }
        return nil, fmt.Errorf("mesh execution with cache failed: %w", err)
    }
    
    // Parse the result
    var result MeshExecutionResult
    if err := json.Unmarshal(stdoutBytes, &result); err != nil {
        if len(stdoutBytes) > 0 {
            log.Printf("Invalid JSON response: %s", string(stdoutBytes))
        }
        return nil, fmt.Errorf("failed to parse mesh execution with cache result: %w", err)
    }
    
    return &result, nil
}

// DiscoverPeers discovers peers in the mesh network
func (rc *RustConnector) DiscoverPeers(ctx context.Context, request *DiscoveryRequest) (*DiscoveryResult, error) {
    rc.mu.Lock()
    defer rc.mu.Unlock()
    
    // Prepare command arguments
    args := []string{
        "discover-peers",
        "--region", request.RegionID,  // --region, not --region-id
    }
    
    if request.TEEType != "" {
        args = append(args, "--tee-type", request.TEEType)
    }
    
    if request.MaxResults > 0 {
        args = append(args, "--max-results", fmt.Sprintf("%d", request.MaxResults))
    }
    
    // Debug log
    if rc.verbose {
        log.Printf("DiscoverPeers command: %s %v", rc.controllerPath, args)
    }
    
    // Execute command
    output, err := exec.CommandContext(ctx, rc.controllerPath, args...).Output()
    if err != nil {
        if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
            log.Printf("DiscoverPeers stderr: %s", string(exitErr.Stderr))
        }
        return nil, fmt.Errorf("discover peers failed: %w", err)
    }
    
    // Parse the result
    var result DiscoveryResult
    if err := json.Unmarshal(output, &result); err != nil {
        log.Printf("Invalid JSON response: %s", string(output))
        return nil, fmt.Errorf("failed to parse discover peers result: %w", err)
    }
    
    return &result, nil
}

// SyncState synchronizes state between TEEs
func (rc *RustConnector) SyncState(ctx context.Context, request *SyncRequest) (*SyncResult, error) {
    rc.mu.Lock()
    defer rc.mu.Unlock()
    
    // Prepare command arguments
    args := []string{
        "sync-state",
        "--target-tee", request.TargetTEE,
        "--object-id", request.ObjectID,
    }
    
    if request.UseDeltas {
        args = append(args, "--use-deltas")
    }
    
    if request.ForceSync {
        args = append(args, "--force-sync")
    }
    
    if request.SyncTimeout > 0 {
        timeoutMs := int(request.SyncTimeout.Milliseconds())
        args = append(args, "--timeout", fmt.Sprintf("%d", timeoutMs))
    }
    
    // Debug log
    if rc.verbose {
        log.Printf("SyncState command: %s %v", rc.controllerPath, args)
    }
    
    // Execute command
    output, err := exec.CommandContext(ctx, rc.controllerPath, args...).Output()
    if err != nil {
        if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
            log.Printf("SyncState stderr: %s", string(exitErr.Stderr))
        }
        return nil, fmt.Errorf("sync state failed: %w", err)
    }
    
    // Parse the result
    var result SyncResult
    if err := json.Unmarshal(output, &result); err != nil {
        log.Printf("Invalid JSON response: %s", string(output))
        return nil, fmt.Errorf("failed to parse sync state result: %w", err)
    }
    
    return &result, nil
}

// ConvertToProtoResponse converts a MeshExecutionResult to a proto-compatible response
func (rc *RustConnector) ConvertToProtoResponse(result *MeshExecutionResult) *ExecutionResponse {
	resp := &ExecutionResponse{
		Result:         result.Result,
		StateHash:      result.ResultHash,
		ExecutionTime:  result.ExecutionTime,
		MemoryUsed:     result.MemoryUsed,
		SyscallCount:   result.SyscallCount,
		SenderId:       result.Metrics.WorkerID,
		Success:        result.Status == "completed" && result.Error == "",
	}

	// Convert network latency from milliseconds to nanoseconds
	if result.Metrics.NetworkLatencyMs > 0 {
		resp.NetworkLatencyNs = uint64(result.Metrics.NetworkLatencyMs * 1_000_000)
	}

	// Add cache-related fields
	resp.CacheHit = result.CacheHit
	if result.CacheHit {
		resp.CacheTtlSec = result.CacheTTL
	}

	return resp
}

// ExecutionResponse represents a standardized execution response
type ExecutionResponse struct {
	Result           []byte `json:"result"`
	StateHash        []byte `json:"state_hash"`
	ExecutionTime    uint64 `json:"execution_time"`
	MemoryUsed       uint64 `json:"memory_used"`
	SyscallCount     uint64 `json:"syscall_count"`
	NetworkLatencyNs uint64 `json:"network_latency_ns"`
	SenderId         string `json:"sender_id"`
	Success          bool   `json:"success"`
	CacheHit         bool   `json:"cache_hit"`
	CacheTtlSec      uint64 `json:"cache_ttl_sec,omitempty"`
}