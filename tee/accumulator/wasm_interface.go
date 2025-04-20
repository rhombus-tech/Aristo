package accumulator

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"time"
	"log"
	"os"
	
	"github.com/bytecodealliance/wasmtime-go"
)

// WebAssemblyInstance represents a minimal interface to WebAssembly functionality
type WebAssemblyInstance interface {
	ExecuteFunction(functionName string, data []byte, useLengthPrefix bool) ([]byte, error)
	Close() error
}

// WasmTeeInterface implements the WasmInterface for interacting with WebAssembly TEEs
type WasmTeeInterface struct {
	teeID      string
	teeType    string
	vmInstance WebAssemblyInstance
	mutex      sync.Mutex
	region     string
}

// NewWasmTeeInterface creates a new interface to the WebAssembly TEE using the production runtime
func NewWasmTeeInterface(teeID, teeType, region string, wasmBytes []byte) (*WasmTeeInterface, error) {
	// Create logger
	logger := log.New(os.Stderr, fmt.Sprintf("[%s-%s] ", teeType, teeID), log.LstdFlags)
	
	// Validate TEE type
	if teeType != "SGX" && teeType != "SEV" {
		return nil, fmt.Errorf("invalid TEE type: %s (must be 'SGX' or 'SEV')", teeType)
	}
	
	// Initialize the WebAssembly runtime with security-focused configuration
	config := wasmtime.NewConfig()
	
	// Configure security settings
	config.SetDebugInfo(true) // Enable debug info for better diagnostics
	config.SetWasmThreads(false) // Disable threading for security reasons
	config.SetConsumeFuel(true) // Enable fuel consumption to prevent infinite loops
	
	// Create engine and store
	engine := wasmtime.NewEngineWithConfig(config)
	store := wasmtime.NewStore(engine)
	
	// Add fuel for execution (500 million units)
	store.AddFuel(500_000_000)
	
	// Compile and instantiate the module
	module, err := wasmtime.NewModule(store.Engine, wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to compile WebAssembly module: %v", err)
	}
	
	// Set up the TEE environment with imports
	// In a production TEE, we would configure imports for TEE-specific functionality
	var imports []wasmtime.AsExtern
	
	// Instantiate the module
	instance, err := wasmtime.NewInstance(store, module, imports)
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate WebAssembly module: %v", err)
	}
	
	// Get memory export
	memoryExport := instance.GetExport(store, "memory")
	if memoryExport == nil {
		return nil, fmt.Errorf("WebAssembly module does not export 'memory'")
	}
	
	memory := memoryExport.Memory()
	if memory == nil {
		return nil, fmt.Errorf("'memory' export is not a memory")
	}
	
	// Get exported functions
	neededFunctions := []string{
		"alloc",             // Memory allocation
		"free",              // Memory deallocation
		"verify_attestation", // Verify attestation
		"batch_verify_attestation", // Batch verify
		"register_attestation", // Register attestation
	}
	
	functions := make(map[string]*wasmtime.Func)
	
	for _, funcName := range neededFunctions {
		export := instance.GetExport(store, funcName)
		if export == nil {
			logger.Printf("Warning: Function '%s' not exported by module", funcName)
			continue
		}
		
		func_ := export.Func()
		if func_ == nil {
			logger.Printf("Warning: Export '%s' is not a function", funcName)
			continue
		}
		
		functions[funcName] = func_
	}
	
	// Check if required functions are available
	if functions["verify_attestation"] == nil {
		return nil, fmt.Errorf("required function 'verify_attestation' not found in module")
	}
	
	// Create the environment
	env := &ProductionWasmEnvironment{
		engine:    engine,
		store:     store,
		instance:  instance,
		memory:    memory,
		functions: functions,
		logger:    logger,
		callCount: 0,
		totalTime: 0,
	}
	
	return &WasmTeeInterface{
		teeID:      teeID,
		teeType:    teeType,
		vmInstance: env,
		region:     region,
	}, nil
}

// ProductionWasmEnvironment is the production implementation of WebAssembly runtime
type ProductionWasmEnvironment struct {
	engine    *wasmtime.Engine
	store     *wasmtime.Store
	instance  *wasmtime.Instance
	memory    *wasmtime.Memory
	functions map[string]*wasmtime.Func
	logger    *log.Logger
	// Performance tracking
	callCount  int64
	totalTime  time.Duration
}

// ExecuteFunction implements the WebAssemblyInstance interface
func (w *ProductionWasmEnvironment) ExecuteFunction(functionName string, data []byte, useLengthPrefix bool) ([]byte, error) {
	start := time.Now()
	defer func() {
		w.callCount++
		w.totalTime += time.Since(start)
	}()
	
	// Ensure function exists
	func_, exists := w.functions[functionName]
	if !exists {
		return nil, fmt.Errorf("function %s not exported by WebAssembly module", functionName)
	}
	
	// Prepare the data with or without length prefix based on the parameter format
	var inputData []byte
	if useLengthPrefix {
		// Format 1: Length-prefixed format (common WebAssembly convention)
		// Add 4-byte length prefix (little-endian u32)
		lengthBytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(lengthBytes, uint32(len(data)))
		inputData = append(lengthBytes, data...)
	} else {
		// Format 2: Direct data format (used in Go tests)
		inputData = data
	}
	
	// Allocate memory for the input
	dataSize := len(inputData)
	alloc := w.functions["alloc"]
	if alloc == nil {
		return nil, fmt.Errorf("alloc function not exported by WebAssembly module")
	}
	
	// Call alloc function to get pointer
	val, err := alloc.Call(w.store, dataSize)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate memory: %v", err)
	}
	ptr := val.(int32)
	
	// Copy data to WebAssembly memory using UnsafeData
	memory := w.memory
	memoryData := memory.UnsafeData(w.store)
	for i := 0; i < dataSize; i++ {
		memoryData[ptr+int32(i)] = inputData[i]
	}
	
	// Call the actual function with appropriate parameters
	var resultPtr interface{}
	if functionName == "register_attestation" || functionName == "verify_attestation" {
		// These functions take just a pointer and length
		resultPtr, err = func_.Call(w.store, ptr, dataSize)
	} else if functionName == "batch_verify_attestation" {
		// Batch functions may take additional parameters
		resultPtr, err = func_.Call(w.store, ptr, dataSize, int32(0)) // 0 is a flag for batch processing
	} else {
		// Default function signature
		resultPtr, err = func_.Call(w.store, ptr, dataSize)
	}
	
	if err != nil {
		return nil, fmt.Errorf("failed to call function %s: %v", functionName, err)
	}
	
	// Get result from memory
	resultPtrVal := resultPtr.(int32)
	
	// Check for null pointer (error condition)
	if resultPtrVal == 0 {
		return nil, fmt.Errorf("WebAssembly function returned null pointer")
	}
	
	// First 4 bytes are length (little endian u32)
	resultPtrInt := int(resultPtrVal)
	resultLength := binary.LittleEndian.Uint32(memoryData[resultPtrInt : resultPtrInt+4])
	
	// Sanity check the result length
	if resultLength > 1024*1024 { // 1MB max result size
		return nil, fmt.Errorf("result too large: %d bytes", resultLength)
	}
	
	// Extract result data
	result := make([]byte, resultLength)
	for i := uint32(0); i < resultLength; i++ {
		result[i] = memoryData[resultPtrInt+4+int(i)]
	}
	
	// Free allocated memory
	free := w.functions["free"]
	if free != nil {
		_, err = free.Call(w.store, ptr)
		if err != nil {
			w.logger.Printf("Warning: failed to free pointer %d: %v", ptr, err)
		}
		
		_, err = free.Call(w.store, resultPtrVal)
		if err != nil {
			w.logger.Printf("Warning: failed to free result pointer %d: %v", resultPtrVal, err)
		}
	}
	
	// Log performance for slow operations
	elapsed := time.Since(start)
	if elapsed > 100*time.Millisecond {
		w.logger.Printf("SLOW WASM EXECUTION: %s took %v", functionName, elapsed)
	}
	
	return result, nil
}

// Close implements the WebAssemblyInstance interface
func (w *ProductionWasmEnvironment) Close() error {
	// Clean up resources
	if w.store != nil {
		w.store = nil
	}
	
	if w.engine != nil {
		w.engine = nil
	}
	
	w.functions = nil
	w.memory = nil
	w.instance = nil
	
	// Runtime will be garbage collected
	return nil
}

// GetTeeType returns the type of TEE (SGX or SEV)
func (w *WasmTeeInterface) GetTeeType() string {
	return w.teeType
}

// GetTeeID returns the identifier for the TEE
func (w *WasmTeeInterface) GetTeeID() string {
	return w.teeID
}

// ExecuteInTee executes a function inside the WebAssembly TEE
func (w *WasmTeeInterface) ExecuteInTee(
	ctx context.Context,
	function string,
	params []byte,
	useLengthPrefix bool,
) ([]byte, error) {
	// Lock to ensure exclusive access to the WebAssembly instance
	w.mutex.Lock()
	defer w.mutex.Unlock()
	
	// Check for context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		// Continue execution
	}
	
	// Execute the function, passing the useLengthPrefix parameter to the WebAssembly interface
	startTime := time.Now()
	result, err := w.vmInstance.ExecuteFunction(function, params, useLengthPrefix)
	execTime := time.Since(startTime)
	
	if err != nil {
		return nil, fmt.Errorf("WebAssembly execution failed: %v", err)
	}
	
	// Log execution time for performance tracking
	if execTime > 100*time.Millisecond {
		// This is a slow operation, log it
		fmt.Printf("SLOW TEE EXECUTION: %s took %v\n", function, execTime)
	}
	
	return result, nil
}

// Close releases resources used by the WebAssembly interface
func (w *WasmTeeInterface) Close() error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	
	return w.vmInstance.Close()
}
