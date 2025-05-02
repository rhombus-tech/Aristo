// wasi_executor.go - WASI module execution in TDX VMs using Enarx
package tee

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Constants for WASI execution
const (
	// Command maximums for security
	maxCommandLength     = 4096
	maxArgumentLength    = 1024
	maxArgumentsCount    = 64
	maxEnvironmentCount  = 128
	maxEnvironmentLength = 1024
	
	// Execution constraints
	maxExecutionTimeSeconds = 60 * 5  // 5 minutes max execution time
	maxMemoryMB             = 1024    // 1GB max memory
	
	// WASM format constraints
	maxWasmSize            = 100 * 1024 * 1024 // 100MB max WebAssembly module size
	
	// WASI module detection markers
	wasiMagicNumber    = "\x00asm"
	wasiSectionIDType  = 1
	wasiSectionIDImport = 2
	
	// Return code handling
	wasiSuccessCode    = 0
	wasiTimeoutCode    = 124
	wasiSignalOffset   = 128 // Standard convention: signal X = 128+X
)

// Performance metrics
var (
	wasiExecutionCount = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "wasi_execution_count",
			Help: "Number of WASI module executions",
		},
	)
	
	wasiExecutionTime = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "wasi_execution_time_seconds",
			Help:    "Time taken to execute WASI modules in seconds",
			Buckets: []float64{0.1, 0.5, 1.0, 5.0, 10.0, 30.0},
		},
	)
	
	wasiExecutionErrors = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "wasi_execution_errors",
			Help: "Number of WASI module execution errors",
		},
	)
	
	wasiExecutionTimeouts = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "wasi_execution_timeouts",
			Help: "Number of WASI module executions that timed out",
		},
	)
)

func init() {
	// Register metrics
	prometheus.MustRegister(wasiExecutionCount)
	prometheus.MustRegister(wasiExecutionTime)
	prometheus.MustRegister(wasiExecutionErrors)
	prometheus.MustRegister(wasiExecutionTimeouts)
}

// WASIExecutionResult contains the result of a WASI module execution
type WASIExecutionResult struct {
	ExitCode    int
	Stdout      []byte
	Stderr      []byte
	ExecutionMs int64
	Error       error
}

// EnarxWASIConfig contains configuration for WASI execution in Enarx
type EnarxWASIConfig struct {
	EnarxPath      string            // Path to enarx binary
	TEEType        string            // "tdx", "sgx", "sev", etc.
	WasiModulePath string            // Path to the WASI module
	Args           []string          // Command-line arguments
	Env            map[string]string // Environment variables
	Timeout        time.Duration     // Execution timeout
	WorkDir        string            // Working directory
	StdioSize      int               // Maximum size for stdout/stderr (bytes)
}

// DetectWASIModule checks if a file is a valid WASI module
func DetectWASIModule(data []byte) (bool, error) {
	// Parameter validation
	if len(data) < 8 {
		return false, fmt.Errorf("file too small to be a valid WASM module")
	}
	
	// Support dual-format parameters - check for length-prefixed format
	moduleData := data
	if len(data) >= 4 {
		prefixLen := binary.LittleEndian.Uint32(data[:4])
		// Validate reasonable size
		if prefixLen > 0 && prefixLen <= maxWasmSize && (int(prefixLen)+4) <= len(data) {
			// Extract actual module data from length-prefixed format
			moduleData = data[4:int(prefixLen)+4]
		}
	}
	
	// Check for WASM magic (direct format validation)
	wasmMagic := []byte(wasiMagicNumber)
	if len(moduleData) < 8 || !bytes.Equal(moduleData[0:4], wasmMagic) {
		return false, fmt.Errorf("invalid WASM module: missing magic")
	}
	
	// Look for WASI imports in the binary
	// Start from byte 8 (after header)
	offset := 8
	
	// Absolute maximum section size for security
	maxSectionSize := 10 * 1024 * 1024 // 10MB max
	
	// Look for import section with proper bounds checking
	for offset < len(moduleData) - 1 { // Need at least 1 byte for section ID
		// Check if we have an import section
		if moduleData[offset] == wasiSectionIDImport {
			// Need at least 4 more bytes for section size
			if offset+5 > len(moduleData) {
				return false, fmt.Errorf("malformed WASM: truncated import section header")
			}
			
			// Get section size with bounds validation
			sectionSize := int(binary.LittleEndian.Uint32(moduleData[offset+1:offset+5]))
			
			// Security check: validate reasonable section size
			if sectionSize <= 0 || sectionSize > maxSectionSize {
				return false, fmt.Errorf("malformed WASM: unreasonable import section size: %d", sectionSize)
			}
			
			// Ensure the full section is within bounds
			if offset+5+sectionSize > len(moduleData) {
				return false, fmt.Errorf("malformed WASM: import section exceeds module bounds")
			}
			
			// Extract section data safely
			sectionData := moduleData[offset+5:offset+5+sectionSize]
			
			// Search for WASI-specific imports
			wasiStrings := []string{
				"wasi_snapshot_preview1",
				"wasi_unstable",
			}
			
			for _, wasiString := range wasiStrings {
				if bytes.Contains(sectionData, []byte(wasiString)) {
					return true, nil
				}
			}
			
			// Skip past this section
			offset += 5 + sectionSize
		} else {
			// Move to next byte
			offset++
		}
	}
	
	// No WASI imports found
	return false, nil
}

// NewDefaultEnarxConfig creates a default Enarx configuration
func NewDefaultEnarxConfig() *EnarxWASIConfig {
	// Find enarx binary in PATH
	enarxPath, err := exec.LookPath("enarx")
	if err != nil {
		// Use a default location if not found in PATH
		enarxPath = "/usr/local/bin/enarx"
	}
	
	return &EnarxWASIConfig{
		EnarxPath:   enarxPath,
		TEEType:     "tdx", // Default to TDX
		Args:        []string{},
		Env:         make(map[string]string),
		Timeout:     time.Duration(maxExecutionTimeSeconds) * time.Second,
		StdioSize:   1 * 1024 * 1024, // 1MB default for stdout/stderr
	}
}

// ExecuteWASIModuleInTDX executes a WASI module inside a TDX VM using Enarx
func ExecuteWASIModuleInTDX(ctx context.Context, config *EnarxWASIConfig) (*WASIExecutionResult, error) {
	start := time.Now()
	wasiExecutionCount.Inc()
	
	// 1. Parameter validation first (security-first architecture)
	if config == nil {
		wasiExecutionErrors.Inc()
		return nil, fmt.Errorf("nil configuration")
	}
	
	if config.WasiModulePath == "" {
		wasiExecutionErrors.Inc()
		return nil, fmt.Errorf("empty WASI module path")
	}
	
	// Validate args for length and count
	if len(config.Args) > maxArgumentsCount {
		wasiExecutionErrors.Inc()
		return nil, fmt.Errorf("too many arguments: %d > %d", len(config.Args), maxArgumentsCount)
	}
	
	for i, arg := range config.Args {
		if len(arg) > maxArgumentLength {
			wasiExecutionErrors.Inc()
			return nil, fmt.Errorf("argument %d too long: %d > %d", i, len(arg), maxArgumentLength)
		}
	}
	
	// Validate environment variables
	if len(config.Env) > maxEnvironmentCount {
		wasiExecutionErrors.Inc()
		return nil, fmt.Errorf("too many environment variables: %d > %d", len(config.Env), maxEnvironmentCount)
	}
	
	for k, v := range config.Env {
		if len(k) + len(v) + 1 > maxEnvironmentLength {
			wasiExecutionErrors.Inc()
			return nil, fmt.Errorf("environment variable too long: %s=%s", k, v)
		}
	}
	
	// 2. Check if module exists and is a valid WASI module
	moduleData, err := os.ReadFile(config.WasiModulePath)
	if err != nil {
		wasiExecutionErrors.Inc()
		return nil, fmt.Errorf("failed to read WASI module: %w", err)
	}
	
	isWasi, err := DetectWASIModule(moduleData)
	if err != nil {
		wasiExecutionErrors.Inc()
		return nil, fmt.Errorf("failed to check if module is WASI: %w", err)
	}
	
	if !isWasi {
		wasiExecutionErrors.Inc()
		return nil, fmt.Errorf("not a valid WASI module")
	}
	
	// 3. Prepare the execution command
	// Base command: enarx run --backend=tdx path/to/module.wasm
	args := []string{"run", "--backend=" + config.TEEType}
	
	// Add memory limit if specified
	args = append(args, "--memory-size=" + fmt.Sprintf("%dM", maxMemoryMB))
	
	// Add the WASI module path
	args = append(args, config.WasiModulePath)
	
	// Add module arguments
	args = append(args, config.Args...)
	
	// Create the command
	cmd := exec.CommandContext(ctx, config.EnarxPath, args...)
	
	// Set environment variables
	for k, v := range config.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	
	// Set working directory if specified
	if config.WorkDir != "" {
		cmd.Dir = config.WorkDir
	}
	
	// 4. Prepare stdout and stderr capture
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	
	// 5. Create a timeout context if not already provided
	var cancel context.CancelFunc
	if ctx == nil {
		ctx, cancel = context.WithTimeout(context.Background(), config.Timeout)
		defer cancel()
	}
	
	// 6. Execute the WASI module
	err = cmd.Start()
	if err != nil {
		wasiExecutionErrors.Inc()
		return nil, fmt.Errorf("failed to start WASI module: %w", err)
	}
	
	// Create a channel for command completion
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	
	// Wait for command to complete or timeout
	var cmdErr error
	select {
	case <-ctx.Done():
		// Command timed out
		wasiExecutionTimeouts.Inc()
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		cmdErr = fmt.Errorf("execution timed out after %v", config.Timeout)
	case cmdErr = <-done:
		// Command completed
	}
	
	// 7. Capture execution result
	executionTime := time.Since(start)
	wasiExecutionTime.Observe(executionTime.Seconds())
	
	result := &WASIExecutionResult{
		ExitCode:    0,
		Stdout:      stdout.Bytes(),
		Stderr:      stderr.Bytes(),
		ExecutionMs: executionTime.Milliseconds(),
		Error:       cmdErr,
	}
	
	// Set exit code based on error
	if cmdErr != nil {
		if exitErr, ok := cmdErr.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
	}
	
	// If timeout, set special exit code
	if ctx.Err() == context.DeadlineExceeded {
		result.ExitCode = wasiTimeoutCode
	}
	
	return result, nil
}

// Singleton pattern for WASI executor
var (
	defaultExecutor     *WASIExecutor
	defaultExecutorOnce sync.Once
)

// WASIExecutor provides a reusable executor for WASI modules
type WASIExecutor struct {
	defaultConfig *EnarxWASIConfig
	mutex         sync.Mutex
}

// GetWASIExecutor returns the singleton WASI executor
func GetWASIExecutor() *WASIExecutor {
	defaultExecutorOnce.Do(func() {
		defaultExecutor = &WASIExecutor{
			defaultConfig: NewDefaultEnarxConfig(),
		}
	})
	return defaultExecutor
}

// ExecuteModule executes a WASI module with the given parameters
func (e *WASIExecutor) ExecuteModule(
	modulePath string,
	args []string,
	env map[string]string,
	timeout time.Duration,
) (*WASIExecutionResult, error) {
	// Create a config based on the default config
	config := *e.defaultConfig
	config.WasiModulePath = modulePath
	config.Args = args
	config.Env = env
	config.Timeout = timeout
	
	// Execute the module
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	
	return ExecuteWASIModuleInTDX(ctx, &config)
}

// ExecuteWasiFunction provides a high-level API for executing WASI functions
func ExecuteWasiFunction(
	modulePath string,
	functionName string,
	params []string,
	timeoutSecs int,
) (string, error) {
	// Parameter validation using our security-first architecture approach
	if modulePath == "" {
		return "", fmt.Errorf("empty module path")
	}
	
	if functionName == "" {
		return "", fmt.Errorf("empty function name")
	}
	
	if timeoutSecs <= 0 {
		timeoutSecs = 30 // Default timeout
	} else if timeoutSecs > maxExecutionTimeSeconds {
		return "", fmt.Errorf("timeout too large: %d > %d", timeoutSecs, maxExecutionTimeSeconds)
	}
	
	// Create execution args: function name followed by params
	args := make([]string, 0, len(params)+1)
	args = append(args, functionName)
	args = append(args, params...)
	
	// Get the executor
	executor := GetWASIExecutor()
	
	// Execute the module
	result, err := executor.ExecuteModule(
		modulePath,
		args,
		nil, // No custom environment variables
		time.Duration(timeoutSecs)*time.Second,
	)
	
	if err != nil {
		return "", fmt.Errorf("execution error: %w", err)
	}
	
	// Check exit code
	if result.ExitCode != wasiSuccessCode {
		stderrStr := string(result.Stderr)
		if stderrStr == "" {
			stderrStr = "unknown error"
		}
		return "", fmt.Errorf("execution failed with code %d: %s", result.ExitCode, stderrStr)
	}
	
	// Return stdout as string
	return string(result.Stdout), nil
}
