package policy

import (
	"fmt"
	"io/ioutil"
	"reflect"
	"sync"
	"unsafe"

	"github.com/bytecodealliance/wasmtime-go"
)

// Helper functions for WebAssembly integration

// getOrLoadWasmModule gets a cached module or loads it from disk
func (e *PolicyEngine) getOrLoadWasmModule(wasmPath string) (*wasmtime.Module, error) {
	e.policyMu.RLock()
	module, exists := e.wasmCache[wasmPath]
	e.policyMu.RUnlock()
	
	if exists {
		return module, nil
	}
	
	// Module not in cache, load it
	e.policyMu.Lock()
	defer e.policyMu.Unlock()
	
	// Check again in case it was loaded by another goroutine
	if module, exists := e.wasmCache[wasmPath]; exists {
		return module, nil
	}
	
	// Read wasm bytes
	wasmBytes, err := ioutil.ReadFile(wasmPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read WebAssembly file: %w", err)
	}
	
	// Compile module
	module, err = wasmtime.NewModule(e.engine, wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to compile WebAssembly module: %w", err)
	}
	
	// Cache module
	e.wasmCache[wasmPath] = module
	
	return module, nil
}

// setupWasmImports creates the necessary imports for WebAssembly modules
func setupWasmImports(store *wasmtime.Store, memory *wasmtime.Memory) []wasmtime.AsExtern {
	// Create a list of imports
	var imports []wasmtime.AsExtern
	
	// Add memory as an import
	imports = append(imports, memory)
	
	// You can add more imports here if needed
	// For example, host functions for logging, getting time, etc.
	
	return imports
}

// readNullTerminatedString reads a null-terminated string from WebAssembly memory
func readNullTerminatedString(mem *wasmtime.Memory, store *wasmtime.Store, offset uint32) string {
	// Start building the string
	var bytes []byte
	
	// Get memory size
	size := mem.DataSize(store)
	
	// Get the raw memory pointer and convert to byte slice
	ptr := mem.Data(store)
	data := ptrToByteSlice(ptr, uint64(size))
	
	// Read until we find a null terminator or hit a reasonable limit
	for i := uint32(0); i < 10000 && uint64(offset+i) < uint64(size); i++ { // Limit to prevent infinite loops
		b := data[offset+i]
		if b == 0 {
			break
		}
		bytes = append(bytes, b)
	}
	
	return string(bytes)
}

// ptrToByteSlice converts an unsafe pointer to a byte slice of the given size
func ptrToByteSlice(ptr unsafe.Pointer, size uint64) []byte {
	// Use reflection to create a slice backed by the memory pointed to by ptr
	var slice []byte
	sliceHeader := (*reflect.SliceHeader)(unsafe.Pointer(&slice))
	sliceHeader.Data = uintptr(ptr)
	sliceHeader.Len = int(size)
	sliceHeader.Cap = int(size)
	return slice
}

// wasmMemoryManager provides simple memory management for WebAssembly modules
type wasmMemoryManager struct {
	memory       *wasmtime.Memory
	store        *wasmtime.Store
	allocations  map[uint32]uint32 // Map of offsets to sizes
	nextOffset   uint32
	mutex        sync.Mutex
}

// newWasmMemoryManager creates a new memory manager for a given WebAssembly memory
func newWasmMemoryManager(memory *wasmtime.Memory, store *wasmtime.Store) *wasmMemoryManager {
	return &wasmMemoryManager{
		memory:      memory,
		store:       store,
		allocations: make(map[uint32]uint32),
		nextOffset:  0,
		mutex:       sync.Mutex{},
	}
}

// allocate reserves a block of memory in the WebAssembly linear memory
func (m *wasmMemoryManager) allocate(size int) (uint32, error) {
	if size <= 0 {
		return 0, fmt.Errorf("allocation size must be positive")
	}
	
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	offset := m.nextOffset
	sizeBytes := uint32(size)
	m.allocations[offset] = sizeBytes
	m.nextOffset += sizeBytes
	
	// Get current memory size in bytes
	pages := m.memory.Size(m.store)
	currentSize := pages * 65536 // Page size is 64KB
	
	// Check if we need to grow memory
	if uint64(m.nextOffset) > currentSize {
		// Calculate needed pages
		neededPages := (uint64(m.nextOffset) + 65535) / 65536 // Round up to page boundary
		pagesToGrow := neededPages - pages
		
		// Grow the memory
		_, err := m.memory.Grow(m.store, pagesToGrow)
		if err != nil {
			return 0, fmt.Errorf("failed to grow WebAssembly memory: %w", err)
		}
	}
	
	return offset, nil
}

// free releases a previously allocated memory block
func (m *wasmMemoryManager) free(offset uint32) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	delete(m.allocations, offset)
}

// writeBytes writes data to WebAssembly memory at the given offset
func (m *wasmMemoryManager) writeBytes(offset uint32, data []byte) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	// Get memory size
	memSize := m.memory.DataSize(m.store)
	
	// Check bounds
	if uint64(offset)+uint64(len(data)) > uint64(memSize) {
		return fmt.Errorf("data exceeds memory bounds: offset=%d, data_size=%d, memory_size=%d", 
			offset, len(data), memSize)
	}
	
	// Get the raw memory data
	ptr := m.memory.Data(m.store)
	memData := ptrToByteSlice(ptr, uint64(memSize))
	
	// Write the data
	copy(memData[offset:], data)
	
	return nil
}

// readBytes reads data from WebAssembly memory at the given offset
func (m *wasmMemoryManager) readBytes(offset uint32, size uint32) ([]byte, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	// Get memory size
	memSize := m.memory.DataSize(m.store)
	
	// Check bounds
	if uint64(offset)+uint64(size) > uint64(memSize) {
		return nil, fmt.Errorf("read exceeds memory bounds: offset=%d, size=%d, memory_size=%d", 
			offset, size, memSize)
	}
	
	// Get the raw memory data
	ptr := m.memory.Data(m.store)
	memData := ptrToByteSlice(ptr, uint64(memSize))
	
	// Copy the data
	result := make([]byte, size)
	copy(result, memData[offset:offset+size])
	
	return result, nil
}
