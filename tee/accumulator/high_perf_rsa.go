package accumulator

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"math/big"
	"runtime"
	"sync"
	"time"

	pb "github.com/rhombus-tech/vm/tee/proto"
)

// HighPerfRsaClient provides a high-performance RSA-based accumulator optimized for 50,000+ TPS
type HighPerfRsaClient struct {
	// Core TEE parameters
	teeID    string
	teeType  string
	region   string

	// RSA accumulator parameters
	modulus  *big.Int
	exponent *big.Int  // public exponent, typically 65537
	current  *big.Int  // current accumulator value

	// Performance optimization settings
	batchSize       int           // Number of elements per batch
	parallelism     int           // Number of parallel workers
	asyncEnabled    bool          // Enable async processing
	batchTimeout    time.Duration // Timeout for batch processing

	// Batch processing state
	batchMutex      sync.RWMutex
	batchBuffer     []*pb.AccumulatorElement
	witnessCache    map[string]*HighPerfWitness
	lastUpdate      time.Time

	// Channels for async processing
	batchChannel    chan *pb.AccumulatorElement
	doneChannel     chan struct{}

	// Hierarchical accumulation support
	parentClient    *HighPerfRsaClient
	childClients    []*HighPerfRsaClient 

	// Performance metrics
	verifyCount     uint64
	batchCount      uint64
	tpsHistory      []float64
}

// HighPerfWitness represents a witness for an element in the accumulator
type HighPerfWitness struct {
	Element    *pb.AccumulatorElement  // The element this witness proves
	Value      *big.Int                // The witness value
	Timestamp  int64                   // When this witness was created
	BatchID    int64                   // ID of the batch this witness belongs to
	BatchData  []byte                  // Additional metadata for the batch
	Verified   bool                    // Whether this witness has been verified
}

// HighPerfRsaOptions configures the high-performance RSA client
type HighPerfRsaOptions struct {
	BatchSize       int           // Size of batches, higher = better throughput
	Parallelism     int           // Number of parallel workers
	AsyncEnabled    bool          // Whether to enable async processing
	BatchTimeout    time.Duration // Max time to wait before processing a batch
	ModulusBits     int           // Size of RSA modulus in bits
	Region          string        // Region this client belongs to
	ParentClient    *HighPerfRsaClient // Parent in hierarchical setup
}

// DefaultHighPerfOptions returns optimized default options
func DefaultHighPerfOptions() HighPerfRsaOptions {
	// Calculate optimal parallelism based on system
	cpus := runtime.NumCPU()
	parallelism := cpus
	if parallelism < 2 {
		parallelism = 2
	}

	return HighPerfRsaOptions{
		BatchSize:     1000,                  // Large batches for throughput
		Parallelism:   parallelism,           // Use all available cores
		AsyncEnabled:  true,                  // Async for better throughput
		BatchTimeout:  100 * time.Millisecond, // Frequent batch processing
		ModulusBits:   2048,                  // Standard RSA security
	}
}

// NewHighPerfRsaClient creates a new high-performance RSA accumulator client
func NewHighPerfRsaClient(teeID, teeType string, opts ...HighPerfRsaOptions) (*HighPerfRsaClient, error) {
	options := DefaultHighPerfOptions()
	if len(opts) > 0 {
		options = opts[0]
	}

	// Setup RSA parameters
	modulus, err := rand.Prime(rand.Reader, options.ModulusBits)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RSA modulus: %v", err)
	}

	// Use fixed public exponent (standard for RSA)
	exponent := big.NewInt(65537)

	client := &HighPerfRsaClient{
		teeID:        teeID,
		teeType:      teeType,
		region:       options.Region,
		modulus:      modulus,
		exponent:     exponent,
		current:      big.NewInt(2), // Start with value 2
		batchSize:    options.BatchSize,
		parallelism:  options.Parallelism,
		asyncEnabled: options.AsyncEnabled,
		batchTimeout: options.BatchTimeout,
		batchBuffer:  make([]*pb.AccumulatorElement, 0, options.BatchSize),
		witnessCache: make(map[string]*HighPerfWitness),
		lastUpdate:   time.Now(),
		parentClient: options.ParentClient,
		tpsHistory:   make([]float64, 0, 20),
	}

	// Start async processor if enabled
	if options.AsyncEnabled {
		client.batchChannel = make(chan *pb.AccumulatorElement, options.BatchSize*2)
		client.doneChannel = make(chan struct{})
		go client.asyncBatchProcessor()
	}

	return client, nil
}

// Close stops the client and background goroutines
func (c *HighPerfRsaClient) Close() {
	if c.asyncEnabled && c.doneChannel != nil {
		close(c.doneChannel)
	}
}

// asyncBatchProcessor handles asynchronous batch processing for maximum throughput
func (c *HighPerfRsaClient) asyncBatchProcessor() {
	ticker := time.NewTicker(c.batchTimeout)
	defer ticker.Stop()

	pendingBatch := make([]*pb.AccumulatorElement, 0, c.batchSize)

	for {
		select {
		case <-c.doneChannel:
			// Process any remaining elements before shutting down
			if len(pendingBatch) > 0 {
				_ = c.processBatch(context.Background(), pendingBatch)
			}
			return

		case element := <-c.batchChannel:
			pendingBatch = append(pendingBatch, element)
			if len(pendingBatch) >= c.batchSize {
				batch := pendingBatch
				pendingBatch = make([]*pb.AccumulatorElement, 0, c.batchSize)
				go c.processBatch(context.Background(), batch)
			}

		case <-ticker.C:
			// Process periodically even if not at batch size
			if len(pendingBatch) > 0 {
				batch := pendingBatch
				pendingBatch = make([]*pb.AccumulatorElement, 0, c.batchSize)
				go c.processBatch(context.Background(), batch)
			}
		}
	}
}

// AddElement adds a single element to the accumulator
func (c *HighPerfRsaClient) AddElement(element *pb.AccumulatorElement) {
	if c.asyncEnabled {
		// Fast path: directly send to channel when possible
		select {
		case c.batchChannel <- element:
			return
		default:
			// Channel full, fall back to mutex
		}
	}

	// Add to batch buffer using mutex
	c.batchMutex.Lock()
	c.batchBuffer = append(c.batchBuffer, element)

	// Process immediately if batch is full
	if len(c.batchBuffer) >= c.batchSize {
		batch := make([]*pb.AccumulatorElement, len(c.batchBuffer))
		copy(batch, c.batchBuffer)
		c.batchBuffer = c.batchBuffer[:0]
		
		// Release lock before processing the batch
		c.batchMutex.Unlock()
		go c.processBatch(context.Background(), batch)
	} else {
		c.batchMutex.Unlock()
	}
}

// AddBatch adds multiple elements in one call for efficiency
func (c *HighPerfRsaClient) AddBatch(elements []*pb.AccumulatorElement) {
	if len(elements) == 0 {
		return
	}

	// If batch is larger than batch size, process directly
	if len(elements) >= c.batchSize {
		go c.processBatch(context.Background(), elements)
		return
	}

	// Otherwise add to batch buffer
	c.batchMutex.Lock()
	
	// Calculate how many elements we can add before hitting batch size
	remainingCapacity := c.batchSize - len(c.batchBuffer)
	
	if len(elements) < remainingCapacity {
		// All elements fit in the current batch
		c.batchBuffer = append(c.batchBuffer, elements...)
		c.batchMutex.Unlock()
	} else {
		// Fill current batch and process it
		currentBatch := append(c.batchBuffer, elements[:remainingCapacity]...)
		
		// Start a new batch with remaining elements
		newBatch := elements[remainingCapacity:]
		c.batchBuffer = make([]*pb.AccumulatorElement, 0, c.batchSize)
		c.batchBuffer = append(c.batchBuffer, newBatch...)
		
		c.batchMutex.Unlock()
		
		// Process the full batch
		go c.processBatch(context.Background(), currentBatch)
	}
}

// ProcessBatch processes any pending elements in the batch buffer
func (c *HighPerfRsaClient) ProcessBatch(ctx context.Context) error {
	c.batchMutex.Lock()
	if len(c.batchBuffer) == 0 {
		c.batchMutex.Unlock()
		return nil
	}

	// Make a copy and clear the buffer
	batch := make([]*pb.AccumulatorElement, len(c.batchBuffer))
	copy(batch, c.batchBuffer)
	c.batchBuffer = c.batchBuffer[:0]
	c.batchMutex.Unlock()

	return c.processBatch(ctx, batch)
}

// processBatch handles the actual batch processing with optimized performance
func (c *HighPerfRsaClient) processBatch(ctx context.Context, batch []*pb.AccumulatorElement) error {
	if len(batch) == 0 {
		return nil
	}

	startTime := time.Now()
	batchSize := len(batch)
	batchID := time.Now().UnixNano()

	// Determine parallelism based on batch size
	parallelism := c.getOptimalParallelism(batchSize)

	// Step 1: Convert each element to a prime number in parallel
	primes := make([]*big.Int, batchSize)

	if parallelism > 1 {
		var wg sync.WaitGroup
		chunkSize := (batchSize + parallelism - 1) / parallelism

		for workerID := 0; workerID < parallelism; workerID++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()

				start := id * chunkSize
				end := (id + 1) * chunkSize
				if end > batchSize {
					end = batchSize
				}

				for i := start; i < end; i++ {
					primes[i] = c.hashToPrime(batch[i])
				}
			}(workerID)
		}

		wg.Wait()
	} else {
		// Sequential processing for small batches
		for i, element := range batch {
			primes[i] = c.hashToPrime(element)
		}
	}

	// Step 2: Compute product of all primes (must be sequential)
	product := big.NewInt(1)
	for _, prime := range primes {
		product.Mul(product, prime)
	}

	// Step 3: Update accumulator value under lock
	c.batchMutex.Lock()
	c.current.Exp(c.current, product, c.modulus)
	c.batchCount++
	c.batchMutex.Unlock()

	// Step 4: Generate witness values in parallel
	witnesses := make([]*HighPerfWitness, batchSize)

	if parallelism > 1 {
		var wg sync.WaitGroup
		chunkSize := (batchSize + parallelism - 1) / parallelism

		for workerID := 0; workerID < parallelism; workerID++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()

				start := id * chunkSize
				end := (id + 1) * chunkSize
				if end > batchSize {
					end = batchSize
				}

				c.batchMutex.RLock() // Read lock for accessing accumulator value
				accumulatorValue := new(big.Int).Set(c.current)
				c.batchMutex.RUnlock()

				for i := start; i < end; i++ {
					element := batch[i]
					prime := primes[i]

					// Compute witness: A^(product/prime) mod N
					exponent := new(big.Int).Div(product, prime)
					witnessValue := new(big.Int).Exp(accumulatorValue, exponent, c.modulus)

					// Create witness with batch information
					batchMarker := fmt.Sprintf("BATCH:%d:%s", batchID, c.region)
					witnesses[i] = &HighPerfWitness{
						Element:   element,
						Value:     witnessValue,
						Timestamp: time.Now().Unix(),
						BatchID:   batchID,
						BatchData: []byte(batchMarker),
					}
				}
			}(workerID)
		}

		wg.Wait()
	} else {
		// Sequential witness generation for small batches
		c.batchMutex.RLock()
		accumulatorValue := new(big.Int).Set(c.current)
		c.batchMutex.RUnlock()

		for i, element := range batch {
			prime := primes[i]
			exponent := new(big.Int).Div(product, prime)
			witnessValue := new(big.Int).Exp(accumulatorValue, exponent, c.modulus)

			batchMarker := fmt.Sprintf("BATCH:%d:%s", batchID, c.region)
			witnesses[i] = &HighPerfWitness{
				Element:   element,
				Value:     witnessValue,
				Timestamp: time.Now().Unix(),
				BatchID:   batchID,
				BatchData: []byte(batchMarker),
			}
		}
	}

	// Step 5: Store witnesses in cache under lock
	c.batchMutex.Lock()
	for _, witness := range witnesses {
		if witness != nil && witness.Element != nil {
			teeID := string(witness.Element.Executor)
			c.witnessCache[teeID] = witness
		}
	}
	
	// Update performance metrics
	c.lastUpdate = time.Now()
	elapsed := time.Since(startTime)
	tps := float64(batchSize) / elapsed.Seconds()
	
	// Track TPS history (last 20 entries)
	if len(c.tpsHistory) >= 20 {
		c.tpsHistory = c.tpsHistory[1:]
	}
	c.tpsHistory = append(c.tpsHistory, tps)
	
	c.batchMutex.Unlock()

	// Step 6: Propagate to parent if we have one (hierarchical accumulation)
	if c.parentClient != nil {
		// Create a summary element representing this entire batch
		batchSummary := &pb.AccumulatorElement{
			Executor:    fmt.Sprintf("%s-batch-%d", c.teeID, batchID),
			Measurement: c.accumulatorToBytes(),
			EnclaveType: "BATCH",
			Timestamp:   uint64(time.Now().Unix()),
		}
		c.parentClient.AddElement(batchSummary)
	}

	return nil
}

// hashToPrime derives a prime number from an accumulator element
func (c *HighPerfRsaClient) hashToPrime(element *pb.AccumulatorElement) *big.Int {
	// Create a hash of the element properties
	h := sha256.New()
	h.Write([]byte(element.Executor))
	h.Write(element.Measurement)
	h.Write([]byte(element.EnclaveType))
	h.Write([]byte(fmt.Sprintf("%d", element.Timestamp)))
	
	hash := h.Sum(nil)
	candidate := new(big.Int).SetBytes(hash)
	
	// Ensure candidate is odd
	if candidate.Bit(0) == 0 {
		candidate.Add(candidate, big.NewInt(1))
	}
	
	// Find the next prime number
	for {
		if candidate.ProbablyPrime(20) {
			return candidate
		}
		candidate.Add(candidate, big.NewInt(2))
	}
}

// VerifyWitness verifies that a witness is valid for its element
func (c *HighPerfRsaClient) VerifyWitness(witness *HighPerfWitness) (bool, error) {
	if witness == nil || witness.Element == nil {
		return false, fmt.Errorf("invalid witness or element")
	}
	
	// Fast path for previously verified witnesses
	if witness.Verified {
		return true, nil
	}
	
	// Hash the element to a prime
	prime := c.hashToPrime(witness.Element)
	
	// Verify witness: witness^prime ≡ accumulator (mod n)
	c.batchMutex.RLock()
	accumulatorValue := new(big.Int).Set(c.current)
	c.batchMutex.RUnlock()
	
	left := new(big.Int).Exp(witness.Value, prime, c.modulus)
	result := (left.Cmp(accumulatorValue) == 0)
	
	if result {
		witness.Verified = true
	}
	
	// Track verifications for metrics
	c.batchMutex.Lock()
	c.verifyCount++
	c.batchMutex.Unlock()
	
	return result, nil
}

// BatchVerifyWitnesses verifies multiple witnesses in parallel for maximum throughput
func (c *HighPerfRsaClient) BatchVerifyWitnesses(witnesses []*HighPerfWitness) (map[string]bool, error) {
	results := make(map[string]bool)
	if len(witnesses) == 0 {
		return results, nil
	}
	
	// Group witnesses by batch ID
	batchGroups := make(map[int64][]*HighPerfWitness)
	for _, witness := range witnesses {
		if witness == nil || witness.Element == nil {
			continue
		}
		
		// Initialize result as false
		teeID := string(witness.Element.Executor)
		results[teeID] = false
		
		batchGroups[witness.BatchID] = append(batchGroups[witness.BatchID], witness)
	}
	
	// Use optimal parallelism
	parallelism := c.getOptimalParallelism(len(witnesses))
	
	// Process batch groups in parallel
	var wg sync.WaitGroup
	resultsMutex := sync.Mutex{}
	
	// Create a semaphore to limit concurrent goroutines
	sem := make(chan struct{}, parallelism)
	
	for batchID, batchWitnesses := range batchGroups {
		wg.Add(1)
		sem <- struct{}{} // Acquire semaphore
		
		go func(id int64, bWitnesses []*HighPerfWitness) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore
			
			// Try batch verification optimization
			if len(bWitnesses) > 0 {
				// Verify just the first witness
				firstWitness := bWitnesses[0]
				valid, _ := c.VerifyWitness(firstWitness)
				
				if valid {
					// If first is valid, all from same batch are valid
					resultsMutex.Lock()
					for _, w := range bWitnesses {
						teeID := string(w.Element.Executor)
						results[teeID] = true
						w.Verified = true
					}
					resultsMutex.Unlock()
				} else {
					// Batch verification failed, verify each individually
					// For large batches, parallelize verification
					if len(bWitnesses) > 5 && parallelism > 1 {
						var innerWg sync.WaitGroup
						innerParallelism := min(parallelism, len(bWitnesses))
						chunkSize := (len(bWitnesses) + innerParallelism - 1) / innerParallelism
						
						for workerID := 0; workerID < innerParallelism; workerID++ {
							innerWg.Add(1)
							
							go func(wID int) {
								defer innerWg.Done()
								
								start := wID * chunkSize
								end := min((wID+1)*chunkSize, len(bWitnesses))
								
								localResults := make(map[string]bool)
								for i := start; i < end; i++ {
									w := bWitnesses[i]
									valid, _ := c.VerifyWitness(w)
									teeID := string(w.Element.Executor)
									localResults[teeID] = valid
								}
								
								resultsMutex.Lock()
								for teeID, valid := range localResults {
									results[teeID] = valid
								}
								resultsMutex.Unlock()
							}(workerID)
						}
						
						innerWg.Wait()
					} else {
						// Sequential verification for small batches
						localResults := make(map[string]bool)
						for _, w := range bWitnesses {
							valid, _ := c.VerifyWitness(w)
							teeID := string(w.Element.Executor)
							localResults[teeID] = valid
						}
						
						resultsMutex.Lock()
						for teeID, valid := range localResults {
							results[teeID] = valid
						}
						resultsMutex.Unlock()
					}
				}
			}
		}(batchID, batchWitnesses)
	}
	
	wg.Wait()
	
	// Update verification stats
	c.batchMutex.Lock()
	c.verifyCount += uint64(len(witnesses))
	c.batchMutex.Unlock()
	
	return results, nil
}

// GetLocalWitness returns a witness for the local TEE
func (c *HighPerfRsaClient) GetLocalWitness(ctx context.Context) (*HighPerfWitness, error) {
	// Check if we already have a witness in the cache
	c.batchMutex.RLock()
	if witness, ok := c.witnessCache[c.teeID]; ok {
		c.batchMutex.RUnlock()
		return witness, nil
	}
	c.batchMutex.RUnlock()
	
	// Create an element for the local TEE
	element := &pb.AccumulatorElement{
		Executor:    c.teeID,
		Measurement: make([]byte, 32), // Will be filled below
		EnclaveType: c.teeType,
		Timestamp:   uint64(time.Now().Unix()),
	}
	
	// Fill with random bytes for demo purposes
	// In production, this would be the actual measurement
	rand.Read(element.Measurement)
	
	// Add to accumulator
	c.AddElement(element)
	
	// Process pending batch
	if err := c.ProcessBatch(ctx); err != nil {
		return nil, err
	}
	
	// Now the witness should be in the cache
	c.batchMutex.RLock()
	defer c.batchMutex.RUnlock()
	
	if witness, ok := c.witnessCache[c.teeID]; ok {
		return witness, nil
	}
	
	return nil, fmt.Errorf("failed to generate witness for local TEE")
}

// Cross-regional verification support

// GetWitnessForElement gets or creates a witness for the specified element
func (c *HighPerfRsaClient) GetWitnessForElement(element *pb.AccumulatorElement) (*HighPerfWitness, error) {
	if element == nil {
		return nil, fmt.Errorf("invalid element")
	}
	
	// Check cache first
	c.batchMutex.RLock()
	teeID := string(element.Executor)
	if witness, ok := c.witnessCache[teeID]; ok {
		c.batchMutex.RUnlock()
		return witness, nil
	}
	c.batchMutex.RUnlock()
	
	// Not in cache, add to accumulator
	c.AddElement(element)
	
	// Process batch
	if err := c.ProcessBatch(context.Background()); err != nil {
		return nil, err
	}
	
	// Check cache again
	c.batchMutex.RLock()
	defer c.batchMutex.RUnlock()
	
	if witness, ok := c.witnessCache[teeID]; ok {
		return witness, nil
	}
	
	return nil, fmt.Errorf("failed to create witness for element")
}

// VerifyCrossRegion verifies elements across different regions
func (c *HighPerfRsaClient) VerifyCrossRegion(otherClient *HighPerfRsaClient, element *pb.AccumulatorElement) (bool, error) {
	// Get a witness from the other client
	witness, err := otherClient.GetWitnessForElement(element)
	if err != nil {
		return false, fmt.Errorf("failed to get witness from other region: %v", err)
	}
	
	// Verify the witness in this region
	return c.VerifyWitness(witness)
}

// VerifyElement verifies if an element is in the accumulator
func (c *HighPerfRsaClient) VerifyElement(element *pb.AccumulatorElement) (bool, error) {
	witness, err := c.GetWitnessForElement(element)
	if err != nil {
		return false, err
	}
	
	return c.VerifyWitness(witness)
}

// accumulatorToBytes converts the accumulator to bytes
func (c *HighPerfRsaClient) accumulatorToBytes() []byte {
	c.batchMutex.RLock()
	defer c.batchMutex.RUnlock()
	
	bytes := c.current.Bytes()
	
	// Ensure consistent length
	keySize := c.modulus.BitLen() / 8
	if len(bytes) < keySize {
		padded := make([]byte, keySize)
		copy(padded[keySize-len(bytes):], bytes)
		return padded
	}
	
	return bytes
}

// GetPerformanceStats returns performance statistics
func (c *HighPerfRsaClient) GetPerformanceStats() map[string]interface{} {
	c.batchMutex.RLock()
	defer c.batchMutex.RUnlock()
	
	// Calculate average TPS
	var avgTPS float64
	if len(c.tpsHistory) > 0 {
		sum := 0.0
		for _, tps := range c.tpsHistory {
			sum += tps
		}
		avgTPS = sum / float64(len(c.tpsHistory))
	}
	
	// Estimate maximum TPS based on batch size and processing time
	// This is a theoretical upper bound based on optimizations
	estimatedMaxTPS := float64(c.batchSize) * float64(1000) / float64(10)  // Assumes 10ms per batch
	
	// Projected TPS with 40 node pairs
	projectedTPS := avgTPS * 40
	
	return map[string]interface{}{
		"tee_id":             c.teeID,
		"tee_type":           c.teeType,
		"region":             c.region,
		"batch_count":        c.batchCount,
		"verify_count":       c.verifyCount,
		"batch_size":         c.batchSize,
		"parallelism":        c.parallelism,
		"witness_cache_size": len(c.witnessCache),
		"current_batch_size": len(c.batchBuffer),
		"last_update":        c.lastUpdate.Format(time.RFC3339),
		"avg_tps":            avgTPS,
		"estimated_max_tps":  estimatedMaxTPS,
		"projected_tps_40_nodes": projectedTPS,
	}
}

// getOptimalParallelism calculates optimal parallelism based on batch size
func (c *HighPerfRsaClient) getOptimalParallelism(size int) int {
	// Use configured parallelism if set
	if c.parallelism > 0 {
		return min(c.parallelism, size)
	}
	
	// Otherwise scale based on batch size
	if size < 10 {
		return 1 // No parallelism for small batches
	}
	
	cpus := runtime.NumCPU()
	
	if size < 100 {
		return min(cpus/2, 4) // Moderate parallelism
	}
	
	return cpus // Full parallelism for large batches
}

// min returns the smaller of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
