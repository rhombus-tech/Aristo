package coordination

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/ava-labs/avalanchego/x/merkledb"
)

// MemoryStorage implements Storage using in-memory maps
type MemoryStorage struct {
	workers            map[string]*Worker
	channels           map[string]*SecureChannel
	regions            map[string]*Region
	teePairs           map[string]*TEEPairInfo
	teeMetrics         map[string]*TEEPairMetrics
	coordinatorState   map[string]interface{}
	data               map[string][]byte
	mu                 sync.RWMutex
}

// NewInMemoryStorage creates a new in-memory storage
func NewInMemoryStorage() BaseStorage {
	return &MemoryStorage{
		workers:            make(map[string]*Worker),
		channels:           make(map[string]*SecureChannel),
		regions:            make(map[string]*Region),
		teePairs:           make(map[string]*TEEPairInfo),
		teeMetrics:         make(map[string]*TEEPairMetrics),
		coordinatorState:   make(map[string]interface{}),
		data:               make(map[string][]byte),
	}
}

// SaveRegion saves a region to memory storage
func (ms *MemoryStorage) SaveRegion(ctx context.Context, region *Region) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.regions[region.ID] = region
	return nil
}

// LoadRegion loads a region from memory storage
func (ms *MemoryStorage) LoadRegion(ctx context.Context, id string) (*Region, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	
	region, exists := ms.regions[id]
	if !exists {
		return nil, fmt.Errorf("region not found: %s", id)
	}
	return region, nil
}

// DeleteRegion deletes a region from memory storage
func (ms *MemoryStorage) DeleteRegion(ctx context.Context, id string) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	
	delete(ms.regions, id)
	return nil
}

// SaveTEEPairInfo saves TEE pair info to memory storage
func (ms *MemoryStorage) SaveTEEPairInfo(ctx context.Context, info *TEEPairInfo) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	
	ms.teePairs[info.ID] = info
	return nil
}

// GetTEEPairInfo gets TEE pair info from memory storage
func (ms *MemoryStorage) GetTEEPairInfo(ctx context.Context, pairID string) (*TEEPairInfo, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	
	pair, exists := ms.teePairs[pairID]
	if !exists {
		return nil, fmt.Errorf("TEE pair not found: %s", pairID)
	}
	return pair, nil
}

// ListTEEPairInfo lists all TEE pairs from memory storage
func (ms *MemoryStorage) ListTEEPairInfo(ctx context.Context) ([]*TEEPairInfo, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	
	pairs := make([]*TEEPairInfo, 0, len(ms.teePairs))
	for _, pair := range ms.teePairs {
		pairs = append(pairs, pair)
	}
	return pairs, nil
}

// SaveTEEMetrics saves TEE metrics to memory storage
func (ms *MemoryStorage) SaveTEEMetrics(ctx context.Context, pairID string, metrics *TEEPairMetrics) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	
	ms.teeMetrics[pairID] = metrics
	return nil
}

// GetTEEMetrics gets TEE metrics from memory storage
func (ms *MemoryStorage) GetTEEMetrics(ctx context.Context, pairID string) (*TEEPairMetrics, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	
	metrics, exists := ms.teeMetrics[pairID]
	if !exists {
		return nil, fmt.Errorf("TEE metrics not found: %s", pairID)
	}
	return metrics, nil
}

// SaveCoordinatorState saves coordinator state to memory storage
func (ms *MemoryStorage) SaveCoordinatorState(ctx context.Context, state map[string]interface{}) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	
	ms.coordinatorState = state
	return nil
}

// LoadCoordinatorState loads coordinator state from memory storage
func (ms *MemoryStorage) LoadCoordinatorState(ctx context.Context) (map[string]interface{}, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	
	if len(ms.coordinatorState) == 0 {
		return make(map[string]interface{}), nil
	}
	return ms.coordinatorState, nil
}

// Put stores a value in memory storage
func (ms *MemoryStorage) Put(ctx context.Context, key []byte, value []byte) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	
	ms.data[string(key)] = value
	return nil
}

// Get retrieves a value from memory storage
func (ms *MemoryStorage) Get(ctx context.Context, key []byte) ([]byte, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	
	value, exists := ms.data[string(key)]
	if !exists {
		return nil, fmt.Errorf("key not found: %s", string(key))
	}
	return value, nil
}

// Delete removes a value from memory storage
func (ms *MemoryStorage) Delete(ctx context.Context, key []byte) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	
	delete(ms.data, string(key))
	return nil
}

// GetByPrefix retrieves all values with keys starting with the given prefix
func (ms *MemoryStorage) GetByPrefix(ctx context.Context, prefix []byte) ([][]byte, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	
	prefixStr := string(prefix)
	var results [][]byte
	
	for key, value := range ms.data {
		if strings.HasPrefix(key, prefixStr) {
			results = append(results, value)
		}
	}
	
	return results, nil
}

// channelKey creates a consistent key for a channel between two workers
func channelKey(worker1, worker2 WorkerID) string {
	if worker1 < worker2 {
		return fmt.Sprintf("%s-%s", worker1, worker2)
	}
	return fmt.Sprintf("%s-%s", worker2, worker1)
}

// SaveChannel persists channel state in memory
func (ms *MemoryStorage) SaveChannel(ctx context.Context, channel *SecureChannel) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	
	key := channelKey(channel.Worker1, channel.Worker2)
	ms.channels[key] = channel
	return nil
}

// LoadChannel loads channel state from memory
func (ms *MemoryStorage) LoadChannel(ctx context.Context, worker1, worker2 WorkerID) (*SecureChannel, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	
	key := channelKey(worker1, worker2)
	channel, exists := ms.channels[key]
	if !exists {
		return nil, ErrNotFound
	}
	return channel, nil
}

// DeleteChannel removes channel state from memory
func (ms *MemoryStorage) DeleteChannel(ctx context.Context, worker1, worker2 WorkerID) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	
	key := channelKey(worker1, worker2)
	delete(ms.channels, key)
	return nil
}

// SaveWorker persists worker state in memory
func (ms *MemoryStorage) SaveWorker(ctx context.Context, worker *Worker) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	
	ms.workers[string(worker.ID)] = worker
	return nil
}

// LoadWorker loads worker state from memory
func (ms *MemoryStorage) LoadWorker(ctx context.Context, id WorkerID) (*Worker, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	
	worker, exists := ms.workers[string(id)]
	if !exists {
		return nil, ErrNotFound
	}
	return worker, nil
}

// DeleteWorker removes worker state from memory
func (ms *MemoryStorage) DeleteWorker(ctx context.Context, id WorkerID) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	
	delete(ms.workers, string(id))
	return nil
}

// NewView is a no-op for memory storage as it doesn't support views
func (ms *MemoryStorage) NewView(ctx context.Context, changes merkledb.ViewChanges) error {
	// No-op for memory storage
	return nil
}

// Close is a no-op for memory storage
func (ms *MemoryStorage) Close() error {
	// No-op for memory storage
	return nil
}
