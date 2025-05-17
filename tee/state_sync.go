package tee

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
)

// SyncMode defines how state should be synchronized
type SyncMode int

const (
	// SyncModeFull does complete state synchronization
	SyncModeFull SyncMode = iota
	
	// SyncModeDelta only synchronizes changes
	SyncModeDelta
	
	// SyncModeSmartDelta uses TEE-specific optimizations for delta sync
	SyncModeSmartDelta
)

// TEEType represents a type of Trusted Execution Environment
type TEEType string

// StateEntry represents a single entry in the state
type StateEntry struct {
	Key       string      `json:"key"`
	Value     interface{} `json:"value"`
	Version   uint64      `json:"version"`
	Timestamp time.Time   `json:"timestamp"`
	Hash      []byte      `json:"hash,omitempty"`
}

// StateDelta represents a change in state
type StateDelta struct {
	Entries    []StateEntry `json:"entries"`
	SourceType TEEType      `json:"source_type"`
	TargetType TEEType      `json:"target_type"`
	Timestamp  time.Time    `json:"timestamp"`
}

// StateSyncOptions provides configuration for state synchronization
type StateSyncOptions struct {
	// Default sync mode to use
	DefaultSyncMode SyncMode
	
	// Whether to use TEE-specific optimizations
	UseOptimizations bool
	
	// SGX-specific memory optimization (smaller chunks due to EPC limits)
	SGXMaxChunkSizeBytes int
	
	// SEV-specific memory optimization (larger chunks due to page granularity)
	SEVPageAlignBytes int
	
	// Compression settings
	UseCompression       bool
	CompressionThreshold int  // Size threshold for using compression
	
	// Performance tuning
	ConcurrentSyncs      int  // Number of concurrent sync operations
	SyncTimeoutMs        int  // Timeout for sync operations
	
	// Security settings
	VerifyIntegrity      bool // Verify integrity of state after sync
}

// DefaultStateSyncOptions returns default options
func DefaultStateSyncOptions() *StateSyncOptions {
	return &StateSyncOptions{
		DefaultSyncMode:      SyncModeSmartDelta,
		UseOptimizations:     true,
		SGXMaxChunkSizeBytes: 4096,       // 4KB chunks for SGX (EPC limit-friendly)
		SEVPageAlignBytes:    4096,       // 4KB page alignment for SEV
		UseCompression:       true,
		CompressionThreshold: 1024,       // Compress data larger than 1KB
		ConcurrentSyncs:      4,          // 4 concurrent sync operations
		SyncTimeoutMs:        200,        // 200ms timeout
		VerifyIntegrity:      true,       // Always verify integrity
	}
}

// StateSyncManager manages efficient state synchronization between TEE environments
type StateSyncManager struct {
	options     *StateSyncOptions
	stateStore  map[string]StateEntry  // In-memory state store
	versionMap  map[string]uint64      // Track versions for each key
	hashCache   map[string][]byte      // Cache of computed hashes
	metrics     *StateSyncMetrics
	mu          sync.RWMutex
}

// StateSyncMetrics tracks metrics for state synchronization
type StateSyncMetrics struct {
	TotalSyncOperations      uint64
	SuccessfulSyncs          uint64
	FailedSyncs              uint64
	TotalBytesSynced         uint64
	TotalBytesOptimized      uint64
	AvgSyncLatencyMs         float64
	
	TotalSGXToSEVSyncs       uint64
	TotalSEVToSGXSyncs       uint64
	
	FullSyncs                uint64
	DeltaSyncs               uint64
	SmartDeltaSyncs          uint64
	
	totalLatencyMs           float64  // Used for calculating average
	mu                       sync.Mutex
}

// NewStateSyncManager creates a new state synchronization manager
func NewStateSyncManager(options *StateSyncOptions) *StateSyncManager {
	if options == nil {
		options = DefaultStateSyncOptions()
	}
	
	return &StateSyncManager{
		options:    options,
		stateStore: make(map[string]StateEntry),
		versionMap: make(map[string]uint64),
		hashCache:  make(map[string][]byte),
		metrics:    &StateSyncMetrics{},
	}
}

// GetState retrieves a state entry by key
func (m *StateSyncManager) GetState(key string) (StateEntry, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	entry, ok := m.stateStore[key]
	return entry, ok
}

// SetState sets a state entry
func (m *StateSyncManager) SetState(key string, value interface{}) (StateEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// Get current version or start at 1
	version := m.versionMap[key] + 1
	
	// Create entry
	entry := StateEntry{
		Key:       key,
		Value:     value,
		Version:   version,
		Timestamp: time.Now(),
	}
	
	// Compute hash
	jsonData, err := json.Marshal(value)
	if err != nil {
		return StateEntry{}, fmt.Errorf("failed to marshal value: %w", err)
	}
	
	hash := sha256.Sum256(jsonData)
	entry.Hash = hash[:]
	
	// Store entry and update version
	m.stateStore[key] = entry
	m.versionMap[key] = version
	m.hashCache[key] = hash[:]
	
	return entry, nil
}

// calculateDelta computes the difference between local and remote state
func (m *StateSyncManager) calculateDelta(localKeys []string, remoteState map[string]StateEntry, targetType TEEType) StateDelta {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	delta := StateDelta{
		SourceType: TEEType(m.getLocalTEEType()),
		TargetType: targetType,
		Timestamp:  time.Now(),
		Entries:    make([]StateEntry, 0),
	}
	
	// Add entries that are newer in local state
	for _, key := range localKeys {
		localEntry, exists := m.stateStore[key]
		if !exists {
			continue
		}
		
		remoteEntry, remoteExists := remoteState[key]
		if !remoteExists || localEntry.Version > remoteEntry.Version {
			delta.Entries = append(delta.Entries, localEntry)
		}
	}
	
	return delta
}

// optimizeDeltaForTEE optimizes a delta for the specific target TEE type
func (m *StateSyncManager) optimizeDeltaForTEE(delta StateDelta) StateDelta {
	if !m.options.UseOptimizations {
		return delta
	}
	
	optimizedDelta := delta
	optimizedDelta.Entries = make([]StateEntry, 0, len(delta.Entries))
	
	// Apply TEE-specific optimizations
	switch delta.TargetType {
	case TEETypeSGX:
		// For SGX, break large entries into smaller chunks due to EPC memory limitations
		for _, entry := range delta.Entries {
			jsonData, err := json.Marshal(entry.Value)
			if err != nil {
				// If we can't marshal, just include the original entry
				optimizedDelta.Entries = append(optimizedDelta.Entries, entry)
				continue
			}
			
			// If entry is too large, break it down
			if len(jsonData) > m.options.SGXMaxChunkSizeBytes {
				// Create chunked entries
				chunks := m.chunkData(jsonData, m.options.SGXMaxChunkSizeBytes)
				for i, chunk := range chunks {
					chunkKey := fmt.Sprintf("%s_chunk_%d", entry.Key, i)
					chunkEntry := StateEntry{
						Key:       chunkKey,
						Value:     chunk,
						Version:   entry.Version,
						Timestamp: entry.Timestamp,
					}
					optimizedDelta.Entries = append(optimizedDelta.Entries, chunkEntry)
				}
				
				// Add metadata entry
				metaEntry := StateEntry{
					Key:       fmt.Sprintf("%s_chunks_meta", entry.Key),
					Value:     map[string]interface{}{
						"total_chunks": len(chunks),
						"original_key": entry.Key,
						"original_hash": entry.Hash,
					},
					Version:   entry.Version,
					Timestamp: entry.Timestamp,
				}
				optimizedDelta.Entries = append(optimizedDelta.Entries, metaEntry)
			} else {
				optimizedDelta.Entries = append(optimizedDelta.Entries, entry)
			}
		}
		
	case TEETypeSEV:
		// For SEV, align data to page boundaries for efficiency
		for _, entry := range delta.Entries {
			jsonData, err := json.Marshal(entry.Value)
			if err != nil {
				// If we can't marshal, just include the original entry
				optimizedDelta.Entries = append(optimizedDelta.Entries, entry)
				continue
			}
			
			// Page-align data for SEV's memory model
			if len(jsonData)%m.options.SEVPageAlignBytes != 0 {
				// Pad data to page boundary
				padding := m.options.SEVPageAlignBytes - (len(jsonData) % m.options.SEVPageAlignBytes)
				paddedData := make([]byte, len(jsonData)+padding)
				copy(paddedData, jsonData)
				
				// Create a new entry with padded data
				paddedEntry := entry
				paddedEntry.Value = map[string]interface{}{
					"data":           paddedData,
					"original_size":  len(jsonData),
					"padded_size":    len(paddedData),
					"original_hash":  entry.Hash,
					"is_page_aligned": true,
				}
				
				optimizedDelta.Entries = append(optimizedDelta.Entries, paddedEntry)
			} else {
				optimizedDelta.Entries = append(optimizedDelta.Entries, entry)
			}
		}
	}
	
	return optimizedDelta
}

// chunkData breaks data into chunks of specified size
func (m *StateSyncManager) chunkData(data []byte, chunkSize int) [][]byte {
	chunks := make([][]byte, 0)
	
	for i := 0; i < len(data); i += chunkSize {
		end := i + chunkSize
		if end > len(data) {
			end = len(data)
		}
		chunks = append(chunks, data[i:end])
	}
	
	return chunks
}

// SyncState synchronizes state with a remote TEE
func (m *StateSyncManager) SyncState(ctx context.Context, remoteState map[string]StateEntry, targetType TEEType, mode SyncMode) error {
	startTime := time.Now()
	
	if mode == SyncModeFull {
		// Full sync: overwrite all state
		err := m.performFullSync(remoteState)
		m.updateMetrics(startTime, err == nil, len(remoteState), 0, SyncModeFull, targetType)
		return err
	}
	
	// Get keys from local state
	localKeys := m.getLocalKeys()
	
	// For delta or smart delta sync
	if mode == SyncModeDelta || mode == SyncModeSmartDelta {
		// Calculate delta
		delta := m.calculateDelta(localKeys, remoteState, targetType)
		
		// For smart delta, apply TEE-specific optimizations
		if mode == SyncModeSmartDelta {
			originalSize := m.calculateDeltaSize(delta)
			delta = m.optimizeDeltaForTEE(delta)
			optimizedSize := m.calculateDeltaSize(delta)
			
			bytesOptimized := originalSize - optimizedSize
			if bytesOptimized < 0 {
				bytesOptimized = 0 // In case optimization increased size
			}
			
			err := m.applyDelta(delta)
			m.updateMetrics(startTime, err == nil, optimizedSize, bytesOptimized, SyncModeSmartDelta, targetType)
			return err
		}
		
		// Regular delta sync
		deltaSize := m.calculateDeltaSize(delta)
		err := m.applyDelta(delta)
		m.updateMetrics(startTime, err == nil, deltaSize, 0, SyncModeDelta, targetType)
		return err
	}
	
	return fmt.Errorf("unsupported sync mode: %v", mode)
}

// getLocalKeys returns all keys in the local state
func (m *StateSyncManager) getLocalKeys() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	keys := make([]string, 0, len(m.stateStore))
	for key := range m.stateStore {
		keys = append(keys, key)
	}
	
	return keys
}

// getLocalTEEType returns the local TEE type
// In a real implementation, this would query the actual TEE
func (m *StateSyncManager) getLocalTEEType() string {
	// For demo purposes, we'll default to SGX
	// In a real implementation, this would detect the actual TEE
	return string(TEETypeSGX)
}

// performFullSync performs a full state synchronization
func (m *StateSyncManager) performFullSync(remoteState map[string]StateEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// Clear current state
	m.stateStore = make(map[string]StateEntry)
	m.versionMap = make(map[string]uint64)
	m.hashCache = make(map[string][]byte)
	
	// Copy remote state
	for key, entry := range remoteState {
		m.stateStore[key] = entry
		m.versionMap[key] = entry.Version
		if entry.Hash != nil {
			m.hashCache[key] = entry.Hash
		}
	}
	
	return nil
}

// applyDelta applies a state delta
func (m *StateSyncManager) applyDelta(delta StateDelta) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// Apply each entry in the delta
	for _, entry := range delta.Entries {
		currentVersion, exists := m.versionMap[entry.Key]
		
		// Only apply if this is a new key or the version is newer
		if !exists || entry.Version > currentVersion {
			m.stateStore[entry.Key] = entry
			m.versionMap[entry.Key] = entry.Version
			if entry.Hash != nil {
				m.hashCache[entry.Key] = entry.Hash
			}
		}
	}
	
	// Check if we need to handle chunked entries
	m.rebuildChunkedEntries()
	
	return nil
}

// rebuildChunkedEntries looks for chunked entries and rebuilds them
func (m *StateSyncManager) rebuildChunkedEntries() {
	// Find all chunk metadata entries
	for key, entry := range m.stateStore {
		if !isMetaEntry(key) {
			continue
		}
		
		// Extract metadata
		meta, ok := entry.Value.(map[string]interface{})
		if !ok {
			continue
		}
		
		originalKey, ok := meta["original_key"].(string)
		if !ok {
			continue
		}
		
		totalChunks, ok := meta["total_chunks"].(float64) // JSON unmarshals numbers as float64
		if !ok {
			continue
		}
		
		// Collect all chunks
		chunks := make([][]byte, int(totalChunks))
		allChunksFound := true
		
		for i := 0; i < int(totalChunks); i++ {
			chunkKey := fmt.Sprintf("%s_chunk_%d", originalKey, i)
			chunkEntry, exists := m.stateStore[chunkKey]
			if !exists {
				allChunksFound = false
				break
			}
			
			chunkData, ok := chunkEntry.Value.([]byte)
			if !ok {
				allChunksFound = false
				break
			}
			
			chunks[i] = chunkData
		}
		
		// If we have all chunks, rebuild the original entry
		if allChunksFound {
			// Combine chunks
			combinedData := bytes.Join(chunks, nil)
			
			// Unmarshal to original value
			var originalValue interface{}
			if err := json.Unmarshal(combinedData, &originalValue); err != nil {
				log.Printf("Failed to unmarshal combined chunks for key %s: %v", originalKey, err)
				continue
			}
			
			// Create rebuilt entry
			originalHash, _ := meta["original_hash"].([]byte)
			rebuiltEntry := StateEntry{
				Key:       originalKey,
				Value:     originalValue,
				Version:   entry.Version,
				Timestamp: entry.Timestamp,
				Hash:      originalHash,
			}
			
			// Store rebuilt entry
			m.stateStore[originalKey] = rebuiltEntry
			m.versionMap[originalKey] = entry.Version
			if originalHash != nil {
				m.hashCache[originalKey] = originalHash
			}
			
			// Clean up chunk entries (optional)
			// We could delete the chunk entries and metadata here
		}
	}
}

// isMetaEntry checks if a key is a chunk metadata entry
func isMetaEntry(key string) bool {
	return len(key) > 12 && key[len(key)-12:] == "_chunks_meta"
}

// calculateDeltaSize estimates the size of a delta in bytes
func (m *StateSyncManager) calculateDeltaSize(delta StateDelta) int {
	size := 0
	
	for _, entry := range delta.Entries {
		jsonData, err := json.Marshal(entry)
		if err == nil {
			size += len(jsonData)
		}
	}
	
	return size
}

// updateMetrics updates the metrics after a sync operation
func (m *StateSyncManager) updateMetrics(startTime time.Time, success bool, bytesSynced int, bytesOptimized int, mode SyncMode, targetType TEEType) {
	latencyMs := float64(time.Since(startTime).Milliseconds())
	
	m.metrics.mu.Lock()
	defer m.metrics.mu.Unlock()
	
	m.metrics.TotalSyncOperations++
	
	if success {
		m.metrics.SuccessfulSyncs++
	} else {
		m.metrics.FailedSyncs++
	}
	
	m.metrics.TotalBytesSynced += uint64(bytesSynced)
	m.metrics.TotalBytesOptimized += uint64(bytesOptimized)
	
	m.metrics.totalLatencyMs += latencyMs
	m.metrics.AvgSyncLatencyMs = m.metrics.totalLatencyMs / float64(m.metrics.TotalSyncOperations)
	
	// Track by TEE type
	if targetType == TEETypeSEV {
		m.metrics.TotalSGXToSEVSyncs++
	} else if targetType == TEETypeSGX {
		m.metrics.TotalSEVToSGXSyncs++
	}
	
	// Track by sync mode
	switch mode {
	case SyncModeFull:
		m.metrics.FullSyncs++
	case SyncModeDelta:
		m.metrics.DeltaSyncs++
	case SyncModeSmartDelta:
		m.metrics.SmartDeltaSyncs++
	}
}

// GetMetrics returns a copy of the current metrics
func (m *StateSyncManager) GetMetrics() *StateSyncMetrics {
	m.metrics.mu.Lock()
	defer m.metrics.mu.Unlock()
	
	// Create a copy without the mutex
	copy := &StateSyncMetrics{
		TotalSyncOperations:     m.metrics.TotalSyncOperations,
		SuccessfulSyncs:         m.metrics.SuccessfulSyncs,
		FailedSyncs:             m.metrics.FailedSyncs,
		TotalBytesSynced:        m.metrics.TotalBytesSynced,
		TotalBytesOptimized:     m.metrics.TotalBytesOptimized,
		AvgSyncLatencyMs:        m.metrics.AvgSyncLatencyMs,
		TotalSGXToSEVSyncs:      m.metrics.TotalSGXToSEVSyncs,
		TotalSEVToSGXSyncs:      m.metrics.TotalSEVToSGXSyncs,
		FullSyncs:               m.metrics.FullSyncs,
		DeltaSyncs:              m.metrics.DeltaSyncs,
		SmartDeltaSyncs:         m.metrics.SmartDeltaSyncs,
		totalLatencyMs:          m.metrics.totalLatencyMs,
	}
	return copy
}
