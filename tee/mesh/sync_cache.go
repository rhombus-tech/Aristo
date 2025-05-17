package mesh

import (
	"sync"
	"time"
)

// SyncResponseCacheV2 provides enhanced caching for sync responses to avoid redundant operations
type SyncResponseCacheV2 struct {
	items      map[string]*syncCacheItem
	mutex      sync.RWMutex
	maxItems   int
	expiration time.Duration
}

// syncCacheItem represents a cached sync response with expiration
type syncCacheItem struct {
	value      interface{}
	expiration time.Time
}

// NewSyncResponseCacheV2 creates a new cache with the specified capacity and expiration time
func NewSyncResponseCacheV2(maxItems int, expiration time.Duration) *SyncResponseCacheV2 {
	return &SyncResponseCacheV2{
		items:      make(map[string]*syncCacheItem),
		maxItems:   maxItems,
		expiration: expiration,
	}
}

// Get retrieves a value from the cache if it exists and hasn't expired
func (c *SyncResponseCacheV2) Get(key string) (interface{}, bool) {
	// Null pointer check
	if c == nil || c.items == nil {
		return nil, false
	}
	
	c.mutex.RLock()
	item, found := c.items[key]
	c.mutex.RUnlock()
	
	if !found {
		return nil, false
	}
	
	// Check if the item has expired
	if time.Now().After(item.expiration) {
		// Item has expired, remove it
		c.mutex.Lock()
		delete(c.items, key)
		c.mutex.Unlock()
		return nil, false
	}
	
	return item.value, true
}

// Set adds a value to the cache with the specified key
func (c *SyncResponseCacheV2) Set(key string, value interface{}) {
	// Null pointer check
	if c == nil {
		return
	}
	
	// Initialize the items map if it's nil
	if c.items == nil {
		c.items = make(map[string]*syncCacheItem)
	}
	
	c.mutex.Lock()
	defer c.mutex.Unlock()
	
	// Check if we need to evict something to make room
	if len(c.items) >= c.maxItems {
		c.evictOldest()
	}
	
	c.items[key] = &syncCacheItem{
		value:      value,
		expiration: time.Now().Add(c.expiration),
	}
}

// evictOldest removes the item with the earliest expiration time
func (c *SyncResponseCacheV2) evictOldest() {
	var oldestKey string
	var oldestTime time.Time
	
	// Find the oldest item
	first := true
	for key, item := range c.items {
		if first || item.expiration.Before(oldestTime) {
			oldestKey = key
			oldestTime = item.expiration
			first = false
		}
	}
	
	// Remove the oldest item
	if oldestKey != "" {
		delete(c.items, oldestKey)
	}
}

// Clear empties the cache
func (c *SyncResponseCacheV2) Clear() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	
	c.items = make(map[string]*syncCacheItem)
}

// Size returns the number of items in the cache
func (c *SyncResponseCacheV2) Size() int {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	
	return len(c.items)
}

// StoreResponse is a convenience method for storing sync responses
func (c *SyncResponseCacheV2) StoreResponse(requestID string, response interface{}) {
	c.Set(requestID, response)
}
