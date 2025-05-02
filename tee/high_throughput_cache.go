package tee

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Cache performance constants
const (
	// Sharded cache configuration
	DefaultShardCount = 16              // Number of shards for reduced lock contention
	ShardHashSize     = 8               // Bytes of hash to use for sharding

	// Certificate chain caching
	MaxChainLength = 10                 // Maximum length of a certificate chain to cache

	// Adaptive TTL configuration
	MinTTL          = 1 * time.Hour     // Minimum TTL for any certificate
	MaxTTL          = 24 * time.Hour    // Maximum TTL for any certificate
	PremiumCertTTL  = 48 * time.Hour    // Premium certificates have longer TTL
	TTLScaleFactor  = 0.1               // Scale factor for TTL adjustments

	// Memory pooling
	PoolSize        = 100               // Number of certificate structures to pre-allocate
	
	// Cache size limits
	DefaultCacheSize = 1000             // Default size per shard
	DefaultMaxBytes  = 100 * 1024 * 1024 // 100MB limit to prevent memory explosion
	EstimatedCertSize = 2 * 1024        // Estimated certificate size (2KB)
	
	// Performance mode environment variable
	HighThroughputEnv = "TDX_PCS_HIGH_THROUGHPUT"
)

// Metrics - defined in pcs_certificates.go
var (
	// Using existing metrics from pcs_certificates.go, but these would be declared here
	// if they weren't already defined
)

//
// Advanced Certificate Cache with High-Performance Optimizations
//

// HTCertChain represents a validated chain of certificates
type HTCertChain struct {
	Certs       []*x509.Certificate // Chain of certificates (leaf to root)
	ValidUntil  time.Time           // When chain validation expires
	Fingerprint string              // Fingerprint of the leaf certificate
}

// HTCachedCert represents a certificate with usage metadata for adaptive TTL
type HTCachedCert struct {
	Cert        *x509.Certificate  // The actual certificate
	LoadedAt    time.Time          // When the certificate was loaded
	ExpiresAt   time.Time          // Certificate expiration
	AccessCount uint64             // Access counter for popularity
	Source      string             // "memory", "disk", "network"
	Fingerprint string             // Certificate fingerprint (SHA-256)
	IsPremium   bool               // Whether this is a high-priority certificate
	LastAccess  time.Time          // Last time the certificate was accessed
	CurrentTTL  time.Duration      // Current adaptive TTL
}

// HTCacheShard represents a single shard in the sharded cache
type HTCacheShard struct {
	cache       map[string]*HTCachedCert // Certificate cache by key
	accessList  *HTLinkedList            // LRU tracking list
	maxSize     int                      // Max entries in this shard
	maxBytes    int64                    // Max bytes in this shard
	currentSize int                      // Current number of entries
	currentBytes int64                   // Current bytes used
	mutex       sync.RWMutex             // Mutex for this shard
}

// HTShardedCache is a thread-safe, high-performance cache with multiple shards
type HTShardedCache struct {
	shards      []*HTCacheShard        // Array of cache shards
	shardCount  int                    // Number of shards
	shardMask   uint32                 // Bit mask for fast shard lookup
}

// HTCertPool implements memory pooling for certificates
type HTCertPool struct {
	pool        sync.Pool              // Memory pool
	used        atomic.Int64           // Number of certificates allocated
}

// HighThroughputCache is a high-performance certificate cache with multiple optimizations
type HighThroughputCache struct {
	shardedCache   *HTShardedCache       // Primary memory cache (sharded)
	chainCache     map[string]*HTCertChain // Cache for validated chains
	chainMutex     sync.RWMutex           // Lock for chain cache
	certPool       *HTCertPool            // Memory pool for certificates
	diskPath       string                 // Path to disk cache
	prewarmedCerts map[string]bool        // Tracking for prewarmed certificates
	refreshTime    time.Time              // Last refresh time
	isRefreshing   atomic.Bool            // Prevents concurrent refreshes
	refreshDue     atomic.Bool            // Indicates refresh is needed
}

// HTLinkedList implements a simple doubly-linked list for LRU tracking
type HTLinkedList struct {
	head        *HTListNode
	tail        *HTListNode
	size        int
}

// HTListNode is a node in the HTLinkedList
type HTListNode struct {
	key         string
	next        *HTListNode
	prev        *HTListNode
}

//
// Implementation of ShardedCache
//

// NewHTShardedCache creates a new sharded certificate cache
func NewHTShardedCache(shardCount, shardSize int, maxBytes int64) *HTShardedCache {
	// Ensure shard count is a power of 2 for efficient masking
	if shardCount & (shardCount - 1) != 0 {
		// Round up to next power of 2
		shardCount--
		for i := 1; i < 32; i *= 2 {
			shardCount |= shardCount >> i
		}
		shardCount++
	}
	
	shards := make([]*HTCacheShard, shardCount)
	for i := 0; i < shardCount; i++ {
		shards[i] = &HTCacheShard{
			cache:       make(map[string]*HTCachedCert),
			accessList:  NewHTLinkedList(),
			maxSize:     shardSize,
			maxBytes:    maxBytes / int64(shardCount),
			currentSize: 0,
			currentBytes: 0,
		}
	}
	
	return &HTShardedCache{
		shards:     shards,
		shardCount: shardCount,
		shardMask:  uint32(shardCount - 1),
	}
}

// getShard returns the appropriate shard for a key
func (c *HTShardedCache) getShard(key string) *HTCacheShard {
	// Fast hash-based sharding
	hash := sha256.Sum256([]byte(key))
	// Use first 4 bytes of hash as uint32, then mask to get shard index
	shardIndex := uint32(hash[0]) | uint32(hash[1])<<8 | uint32(hash[2])<<16 | uint32(hash[3])<<24
	shardIndex = shardIndex & c.shardMask
	return c.shards[shardIndex]
}

// Get retrieves a certificate from the cache
func (c *HTShardedCache) Get(key string) (*HTCachedCert, bool) {
	shard := c.getShard(key)
	
	// Read lock for lookup
	shard.mutex.RLock()
	cert, exists := shard.cache[key]
	if !exists {
		shard.mutex.RUnlock()
		return nil, false
	}
	
	// Mark as accessed for LRU (must release read lock and acquire write lock)
	shard.mutex.RUnlock()
	
	// Update access stats under write lock
	shard.mutex.Lock()
	
	// Make sure it still exists after getting the write lock
	cert, exists = shard.cache[key]
	if !exists {
		shard.mutex.Unlock()
		return nil, false
	}
	
	// Move to front of access list
	shard.accessList.MoveToFront(key)
	
	// Update access stats
	atomic.AddUint64(&cert.AccessCount, 1)
	cert.LastAccess = time.Now()
	
	shard.mutex.Unlock()
	return cert, true
}

// Add adds a certificate to the cache
func (c *HTShardedCache) Add(key string, cert *HTCachedCert) {
	if cert == nil || cert.Cert == nil {
		return // Defensive programming
	}
	
	// Create fingerprint if not set
	if cert.Fingerprint == "" {
		hash := sha256.Sum256(cert.Cert.Raw)
		cert.Fingerprint = hex.EncodeToString(hash[:])
	}
	
	// Set default TTL if not set
	if cert.CurrentTTL == 0 {
		if cert.IsPremium {
			cert.CurrentTTL = PremiumCertTTL
		} else {
			cert.CurrentTTL = MaxTTL
		}
	}
	
	// Initialize LastAccess if not set
	if cert.LastAccess.IsZero() {
		cert.LastAccess = time.Now()
	}
	
	// Calculate size
	certSize := int64(EstimatedCertSize)
	if len(cert.Cert.Raw) > 0 {
		certSize = int64(len(cert.Cert.Raw))
	}
	
	shard := c.getShard(key)
	shard.mutex.Lock()
	defer shard.mutex.Unlock()
	
	// Check if already exists
	existingCert, exists := shard.cache[key]
	if exists {
		// Update existing entry
		shard.currentBytes = shard.currentBytes - int64(len(existingCert.Cert.Raw)) + certSize
		shard.cache[key] = cert
		shard.accessList.MoveToFront(key)
		return
	}
	
	// Evict if necessary
	for shard.currentSize >= shard.maxSize || shard.currentBytes+certSize > shard.maxBytes {
		if !shard.evictOne() {
			// Nothing to evict, can't add more
			return
		}
	}
	
	// Add new entry
	shard.cache[key] = cert
	shard.accessList.AddToFront(key)
	shard.currentSize++
	shard.currentBytes += certSize
}

// Remove removes a certificate from the cache
func (c *HTShardedCache) Remove(key string) {
	shard := c.getShard(key)
	shard.mutex.Lock()
	defer shard.mutex.Unlock()
	
	cert, exists := shard.cache[key]
	if !exists {
		return
	}
	
	delete(shard.cache, key)
	shard.accessList.Remove(key)
	shard.currentSize--
	
	if cert.Cert != nil && len(cert.Cert.Raw) > 0 {
		shard.currentBytes -= int64(len(cert.Cert.Raw))
	} else {
		shard.currentBytes -= EstimatedCertSize
	}
}

// evictOne evicts the least recently used item from the shard
func (s *HTCacheShard) evictOne() bool {
	if s.currentSize == 0 {
		return false
	}
	
	// Get LRU key
	key := s.accessList.RemoveFromBack()
	if key == "" {
		return false
	}
	
	// Remove from cache
	cert, exists := s.cache[key]
	if !exists {
		return true // Already gone, but we did evict something
	}
	
	delete(s.cache, key)
	s.currentSize--
	
	if cert.Cert != nil && len(cert.Cert.Raw) > 0 {
		s.currentBytes -= int64(len(cert.Cert.Raw))
	} else {
		s.currentBytes -= EstimatedCertSize
	}
	
	return true
}

// Clear removes all items from the cache
func (c *HTShardedCache) Clear() {
	for _, shard := range c.shards {
		shard.mutex.Lock()
		shard.cache = make(map[string]*HTCachedCert)
		shard.accessList = NewHTLinkedList()
		shard.currentSize = 0
		shard.currentBytes = 0
		shard.mutex.Unlock()
	}
}

// Size returns the total number of certificates in the cache
func (c *HTShardedCache) Size() int {
	size := 0
	for _, shard := range c.shards {
		shard.mutex.RLock()
		size += shard.currentSize
		shard.mutex.RUnlock()
	}
	return size
}

// ForEach executes a function on each certificate in the cache
func (c *HTShardedCache) ForEach(fn func(key string, cert *HTCachedCert) bool) {
	for _, shard := range c.shards {
		shard.mutex.RLock()
		for k, v := range shard.cache {
			if !fn(k, v) {
				shard.mutex.RUnlock()
				return
			}
		}
		shard.mutex.RUnlock()
	}
}

//
// Implementation of LinkedList for LRU tracking
//

// NewHTLinkedList creates a new linked list
func NewHTLinkedList() *HTLinkedList {
	return &HTLinkedList{}
}

// AddToFront adds a node to the front of the list
func (l *HTLinkedList) AddToFront(key string) {
	node := &HTListNode{key: key}
	
	if l.head == nil {
		l.head = node
		l.tail = node
	} else {
		node.next = l.head
		l.head.prev = node
		l.head = node
	}
	
	l.size++
}

// MoveToFront moves a node to the front of the list
func (l *HTLinkedList) MoveToFront(key string) {
	// Find the node
	current := l.head
	for current != nil {
		if current.key == key {
			// Already at front
			if current == l.head {
				return
			}
			
			// Remove from current position
			if current.prev != nil {
				current.prev.next = current.next
			}
			
			if current.next != nil {
				current.next.prev = current.prev
			} else {
				// This was the tail
				l.tail = current.prev
			}
			
			// Move to front
			current.next = l.head
			current.prev = nil
			l.head.prev = current
			l.head = current
			
			return
		}
		current = current.next
	}
	
	// If not found, add it
	l.AddToFront(key)
}

// Remove removes a node from the list
func (l *HTLinkedList) Remove(key string) {
	current := l.head
	for current != nil {
		if current.key == key {
			// Remove from list
			if current.prev != nil {
				current.prev.next = current.next
			} else {
				// This was the head
				l.head = current.next
			}
			
			if current.next != nil {
				current.next.prev = current.prev
			} else {
				// This was the tail
				l.tail = current.prev
			}
			
			l.size--
			return
		}
		current = current.next
	}
}

// RemoveFromBack removes and returns the key of the least recently used node
func (l *HTLinkedList) RemoveFromBack() string {
	if l.tail == nil {
		return ""
	}
	
	key := l.tail.key
	
	// Remove tail
	if l.head == l.tail {
		// Only one node
		l.head = nil
		l.tail = nil
	} else {
		l.tail = l.tail.prev
		l.tail.next = nil
	}
	
	l.size--
	return key
}

//
// Implementation of CertificatePool for memory optimization
//

// NewHTCertPool creates a new certificate memory pool
func NewHTCertPool() *HTCertPool {
	return &HTCertPool{
		pool: sync.Pool{
			New: func() interface{} {
				return &HTCachedCert{}
			},
		},
	}
}

// Get retrieves a certificate from the pool
func (p *HTCertPool) Get() *HTCachedCert {
	p.used.Add(1)
	return p.pool.Get().(*HTCachedCert)
}

// Put returns a certificate to the pool
func (p *HTCertPool) Put(cert *HTCachedCert) {
	if cert == nil {
		return
	}
	
	// Clear fields to prevent memory leaks
	cert.Cert = nil
	cert.Fingerprint = ""
	cert.Source = ""
	cert.AccessCount = 0
	cert.IsPremium = false
	cert.CurrentTTL = 0
	
	p.pool.Put(cert)
	p.used.Add(-1)
}

// InUse returns the number of certificates currently allocated
func (p *HTCertPool) InUse() int64 {
	return p.used.Load()
}

//
// Implementation of OptimizedCertCache
//

// NewHighThroughputCache creates a new optimized certificate cache
func NewHighThroughputCache(diskPath string) (*HighThroughputCache, error) {
	// Create disk path if it doesn't exist
	if err := os.MkdirAll(diskPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create disk cache directory: %w", err)
	}
	
	cache := &HighThroughputCache{
		shardedCache:   NewHTShardedCache(DefaultShardCount, DefaultCacheSize, DefaultMaxBytes),
		chainCache:     make(map[string]*HTCertChain),
		certPool:       NewHTCertPool(),
		diskPath:       diskPath,
		prewarmedCerts: make(map[string]bool),
		refreshTime:    time.Now(),
	}
	
	// Start background refresh worker if not in test mode
	if os.Getenv("TDX_PCS_TEST_MODE") != "true" {
		go cache.refreshWorker()
		
		// Prewarm the cache
		go cache.prewarmCache()
	}
	
	return cache, nil
}

// prewarmCache loads critical certificates into memory at startup
func (c *HighThroughputCache) prewarmCache() {
	// Premium certificates that should be prewarmed
	premiumCerts := []string{
		defaultRootCAFile,
		defaultTCBSigningCertFile,
		defaultPCKCertFile,
	}
	
	for _, certName := range premiumCerts {
		certPath := filepath.Join(c.diskPath, certName)
		certData, err := ioutil.ReadFile(certPath)
		if err != nil {
			continue // Skip unavailable certs
		}
		
		// Parse certificate
		var cert *x509.Certificate
		// First try PEM format
		certPEM, _ := pem.Decode(certData)
		if certPEM != nil {
			cert, err = x509.ParseCertificate(certPEM.Bytes)
		} else {
			// Try DER format
			cert, err = x509.ParseCertificate(certData)
		}
		
		if err == nil && cert != nil {
			// Add to cache as premium cert
			cached := c.certPool.Get()
			cached.Cert = cert
			cached.LoadedAt = time.Now()
			cached.ExpiresAt = cert.NotAfter
			cached.Source = "prewarm"
			cached.IsPremium = true
			cached.LastAccess = time.Now()
			cached.CurrentTTL = PremiumCertTTL
			
			// Calculate fingerprint
			hash := sha256.Sum256(cert.Raw)
			cached.Fingerprint = hex.EncodeToString(hash[:])
			
			// Add to cache
			c.shardedCache.Add(certName, cached)
			c.prewarmedCerts[certName] = true
		}
	}
}

// refreshWorker periodically refreshes certificates
func (c *HighThroughputCache) refreshWorker() {
	ticker := time.NewTicker(autoRefreshInterval)
	defer ticker.Stop()
	
	for range ticker.C {
		if !c.isRefreshing.Load() {
			c.refreshCertificates()
		}
	}
}

// refreshCertificates checks and refreshes certificates as needed
func (c *HighThroughputCache) refreshCertificates() {
	// Avoid concurrent refreshes
	if !c.isRefreshing.CompareAndSwap(false, true) {
		return
	}
	defer c.isRefreshing.Store(false)
	
	startTime := time.Now()
	defer func() {
		certRefreshLatency.Observe(time.Since(startTime).Seconds())
	}()
	
	// Collect certificates needing refresh
	var toRefresh []string
	
	c.shardedCache.ForEach(func(key string, cert *HTCachedCert) bool {
		if cert == nil || cert.Cert == nil || cert.ExpiresAt.IsZero() {
			return true // Continue
		}
		
		timeToExpiry := time.Until(cert.ExpiresAt)
		
		// Warn about certificates close to expiry
		if timeToExpiry < ttlWarningThreshold {
			certExpiryWarnings.Inc()
		}
		
		// Calculate refresh threshold based on premium status
		refreshThreshold := proactiveRefreshAt
		if cert.IsPremium {
			// Premium certs refresh even earlier
			refreshThreshold = proactiveRefreshAt * 2
		}
		
		// Queue for refresh if approaching expiry
		if timeToExpiry < refreshThreshold {
			toRefresh = append(toRefresh, key)
		}
		
		return true // Continue iteration
	})
	
	// Refresh certificates
	for _, key := range toRefresh {
		// Just remove from cache, will be reloaded on next access
		c.shardedCache.Remove(key)
		
		// Also remove associated chain if it exists
		c.chainMutex.Lock()
		delete(c.chainCache, key)
		c.chainMutex.Unlock()
	}
	
	// Update last refresh time
	c.refreshTime = time.Now()
	c.refreshDue.Store(false)
}

// GetCertificate retrieves a certificate with all optimizations
func (c *HighThroughputCache) GetCertificate(name string) (*x509.Certificate, error) {
	// Fast path for high-throughput mode
	isHighThroughput := os.Getenv(HighThroughputEnv) == "true"
	if isHighThroughput {
		certHighThroughputOps.Inc()
		
		// Try memory cache only
		certObj, found := c.shardedCache.Get(name)
		if found && certObj.Cert != nil {
			certCacheHits.Inc()
			return certObj.Cert, nil
		}
		
		// Even in high-throughput mode, trigger background load if missing
		if !c.isRefreshing.Load() {
			go c.ensureCertificate(name)
		}
		
		certCacheMisses.Inc()
		return nil, fmt.Errorf("certificate %s not found in high-throughput cache", name)
	}
	
	// Normal tiered path
	return c.ensureCertificate(name)
}

// ensureCertificate ensures a certificate is available, loading from disk if needed
func (c *HighThroughputCache) ensureCertificate(name string) (*x509.Certificate, error) {
	// Check memory cache first
	certObj, found := c.shardedCache.Get(name)
	if found && certObj.Cert != nil {
		certCacheHits.Inc()
		
		// If using adaptive TTL, adjust based on access patterns
		if usageBasedTTLEnabled && !certObj.LastAccess.IsZero() && time.Since(certObj.LastAccess) < MinTTL {
			// Certificate is frequently used, increase TTL up to max
			newTTL := certObj.CurrentTTL + time.Duration(float64(certObj.CurrentTTL)*TTLScaleFactor)
			if newTTL > MaxTTL {
				newTTL = MaxTTL
			}
			certObj.CurrentTTL = newTTL
		}
		
		return certObj.Cert, nil
	}
	
	certCacheMisses.Inc()
	
	// Need to load from disk
	certPath := filepath.Join(c.diskPath, name)
	certData, err := ioutil.ReadFile(certPath)
	if err != nil {
		return nil, err
	}
	
	// Parse certificate
	var cert *x509.Certificate
	// First try PEM format
	certPEM, _ := pem.Decode(certData)
	if certPEM != nil {
		cert, err = x509.ParseCertificate(certPEM.Bytes)
	} else {
		// Try DER format
		cert, err = x509.ParseCertificate(certData)
	}
	
	if err != nil || cert == nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}
	
	// Add to cache
	cached := c.certPool.Get()
	cached.Cert = cert
	cached.LoadedAt = time.Now()
	cached.ExpiresAt = cert.NotAfter
	cached.Source = "disk"
	cached.LastAccess = time.Now()
	
	// Check if this is a premium certificate
	cached.IsPremium = c.prewarmedCerts[name]
	
	// Set initial TTL
	if cached.IsPremium {
		cached.CurrentTTL = PremiumCertTTL
	} else {
		cached.CurrentTTL = MaxTTL
	}
	
	// Add to cache
	c.shardedCache.Add(name, cached)
	
	return cert, nil
}

// StoreCertificateChain stores a validated certificate chain
func (c *HighThroughputCache) StoreCertificateChain(leaf *x509.Certificate, chain []*x509.Certificate, validUntil time.Time) {
	if leaf == nil || len(chain) == 0 || len(chain) > MaxChainLength {
		return
	}
	
	// Generate fingerprint for the leaf certificate
	hash := sha256.Sum256(leaf.Raw)
	fingerprint := hex.EncodeToString(hash[:])
	
	// Store in chain cache
	c.chainMutex.Lock()
	defer c.chainMutex.Unlock()
	
	c.chainCache[fingerprint] = &HTCertChain{
		Certs:       chain,
		ValidUntil:  validUntil,
		Fingerprint: fingerprint,
	}
}

// GetCertificateChain retrieves a validated certificate chain
func (c *HighThroughputCache) GetCertificateChain(leaf *x509.Certificate) ([]*x509.Certificate, bool) {
	if leaf == nil {
		return nil, false
	}
	
	// Generate fingerprint
	hash := sha256.Sum256(leaf.Raw)
	fingerprint := hex.EncodeToString(hash[:])
	
	// Check cache
	c.chainMutex.RLock()
	defer c.chainMutex.RUnlock()
	
	chain, exists := c.chainCache[fingerprint]
	if !exists || time.Now().After(chain.ValidUntil) {
		return nil, false
	}
	
	// Return a copy to prevent modification
	result := make([]*x509.Certificate, len(chain.Certs))
	copy(result, chain.Certs)
	
	return result, true
}

// Clear clears all caches
func (c *HighThroughputCache) Clear() {
	c.shardedCache.Clear()
	
	c.chainMutex.Lock()
	c.chainCache = make(map[string]*HTCertChain)
	c.chainMutex.Unlock()
}

// Size returns the total size of the cache
func (c *HighThroughputCache) Size() int {
	return c.shardedCache.Size()
}
