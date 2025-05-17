package mesh

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestObject represents a generic object to be processed by the worker pool
type TestObject struct {
	ID        int
	Data      []byte
	ProcessMs int
}

// SerialProcessObjects processes objects sequentially
func SerialProcessObjects(objects []*TestObject) []bool {
	results := make([]bool, len(objects))
	
	for i, obj := range objects {
		// Simulate processing time
		if obj.ProcessMs > 0 {
			time.Sleep(time.Duration(obj.ProcessMs) * time.Millisecond)
		}
		
		// Simple processing logic - objects with even IDs succeed
		results[i] = obj.ID%2 == 0
	}
	
	return results
}

// WorkerPoolProcessObjects processes objects in parallel using a worker pool
func WorkerPoolProcessObjects(objects []*TestObject) []bool {
	if len(objects) == 0 {
		return []bool{}
	}
	
	if len(objects) == 1 {
		return SerialProcessObjects(objects)
	}
	
	// Define job structure
	type ProcessJob struct {
		index  int
		object *TestObject
	}
	
	// Initialize result tracking
	results := make([]bool, len(objects))
	var resultMutex sync.Mutex
	
	// Create optimal worker pool
	numWorkers := runtime.NumCPU() * 2 // Use 2x CPU count for IO-bound operations
	if numWorkers > len(objects) {
		numWorkers = len(objects) // Don't create more workers than needed
	}
	
	// Create job channel and worker coordination
	jobs := make(chan ProcessJob, len(objects))
	var wg sync.WaitGroup
	
	// Start workers
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			
			for job := range jobs {
				// Simulate processing time
				if job.object.ProcessMs > 0 {
					time.Sleep(time.Duration(job.object.ProcessMs) * time.Millisecond)
				}
				
				// Simple processing logic - objects with even IDs succeed
				resultMutex.Lock()
				results[job.index] = job.object.ID%2 == 0
				resultMutex.Unlock()
			}
		}()
	}
	
	// Add objects to job queue
	for i, obj := range objects {
		jobs <- ProcessJob{
			index:  i,
			object: obj,
		}
	}
	
	// Close jobs channel to signal no more work
	close(jobs)
	
	// Wait for all workers to finish
	wg.Wait()
	
	return results
}

// createTestObjects creates a batch of test objects with the specified size
func createTestObjects(size int, processTimeMs int) []*TestObject {
	objects := make([]*TestObject, size)
	for i := 0; i < size; i++ {
		objects[i] = &TestObject{
			ID:        i,
			Data:      []byte(fmt.Sprintf("test-data-%d", i)),
			ProcessMs: processTimeMs,
		}
	}
	return objects
}

// TestWorkerPoolVsSerial tests the worker pool implementation against serial processing
func TestWorkerPoolVsSerial(t *testing.T) {
	// Define test cases
	testCases := []struct {
		name         string
		objectCount  int
		processTimeMs int
	}{
		{"SmallBatch_FastProcessing", 10, 1},
		{"MediumBatch_FastProcessing", 100, 1},
		{"LargeBatch_FastProcessing", 1000, 1},
		{"SmallBatch_SlowProcessing", 10, 5},
		{"MediumBatch_SlowProcessing", 50, 5},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			objects := createTestObjects(tc.objectCount, tc.processTimeMs)
			
			// Test serial processing
			serialStart := time.Now()
			serialResults := SerialProcessObjects(objects)
			serialDuration := time.Since(serialStart)
			
			// Test worker pool processing
			poolStart := time.Now()
			poolResults := WorkerPoolProcessObjects(objects)
			poolDuration := time.Since(poolStart)
			
			// Verify results match
			require.Equal(t, len(serialResults), len(poolResults), "Result count should match")
			for i := range serialResults {
				require.Equal(t, serialResults[i], poolResults[i], "Results should match for object %d", i)
			}
			
			// Log performance comparison
			t.Logf("Serial processing took: %v", serialDuration)
			t.Logf("Worker pool processing took: %v", poolDuration)
			t.Logf("Speed improvement: %.2fx", float64(serialDuration)/float64(poolDuration))
			
			// For slow processing, worker pool should be significantly faster with larger batches
			if tc.processTimeMs > 1 && tc.objectCount > 20 {
				// With CPU*2 workers, we expect at least 1.5x speedup for IO-bound tasks
				require.Greater(t, float64(serialDuration)/float64(poolDuration), 1.5, 
					"Worker pool should be significantly faster for larger batches with slow processing")
			}
		})
	}
}

// BenchmarkWorkerPoolProcessing benchmarks the worker pool implementation
func BenchmarkWorkerPoolProcessing(b *testing.B) {
	// Batch sizes to benchmark
	batchSizes := []int{10, 100, 1000}
	processingTimes := []int{0, 1, 5}
	
	for _, batchSize := range batchSizes {
		for _, processTimeMs := range processingTimes {
			b.Run(fmt.Sprintf("Size_%d_ProcessTime_%dms", batchSize, processTimeMs), func(b *testing.B) {
				// Create test objects once
				objects := createTestObjects(batchSize, processTimeMs)
				
				// Reset timer before the benchmark loop
				b.ResetTimer()
				
				// Run benchmark
				for i := 0; i < b.N; i++ {
					_ = WorkerPoolProcessObjects(objects)
				}
			})
		}
	}
}

// BenchmarkSerialProcessing benchmarks the serial processing implementation
func BenchmarkSerialProcessing(b *testing.B) {
	// Batch sizes to benchmark
	batchSizes := []int{10, 100, 1000}
	processingTimes := []int{0, 1, 5}
	
	for _, batchSize := range batchSizes {
		for _, processTimeMs := range processingTimes {
			b.Run(fmt.Sprintf("Size_%d_ProcessTime_%dms", batchSize, processTimeMs), func(b *testing.B) {
				// Create test objects once
				objects := createTestObjects(batchSize, processTimeMs)
				
				// Reset timer before the benchmark loop
				b.ResetTimer()
				
				// Run benchmark
				for i := 0; i < b.N; i++ {
					_ = SerialProcessObjects(objects)
				}
			})
		}
	}
}

// BenchmarkComparisonTest directly compares serial vs worker pool performance
func BenchmarkComparisonTest(b *testing.B) {
	// Test cases to compare
	testCases := []struct {
		name         string
		objectCount  int
		processTimeMs int
	}{
		{"Small_Fast", 10, 1},
		{"Medium_Fast", 100, 1},
		{"Large_Fast", 1000, 1},
		{"Small_Slow", 10, 5},
		{"Medium_Slow", 50, 5},
		{"BatchAttestation", 500, 2}, // Similar to attestation verification workload
	}
	
	for _, tc := range testCases {
		// First benchmark serial processing
		b.Run(fmt.Sprintf("Serial_%s", tc.name), func(b *testing.B) {
			objects := createTestObjects(tc.objectCount, tc.processTimeMs)
			b.ResetTimer()
			
			for i := 0; i < b.N; i++ {
				_ = SerialProcessObjects(objects)
			}
		})
		
		// Then benchmark worker pool processing
		b.Run(fmt.Sprintf("WorkerPool_%s", tc.name), func(b *testing.B) {
			objects := createTestObjects(tc.objectCount, tc.processTimeMs)
			b.ResetTimer()
			
			for i := 0; i < b.N; i++ {
				_ = WorkerPoolProcessObjects(objects)
			}
		})
	}
}
