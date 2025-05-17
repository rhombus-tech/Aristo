package mesh

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/rhombus-tech/vm/compute"
)

// TaskPriority defines the priority level of a task
type TaskPriority int

const (
	// PriorityLow for non-critical operations
	PriorityLow TaskPriority = iota
	
	// PriorityNormal for standard operations
	PriorityNormal
	
	// PriorityHigh for time-sensitive operations
	PriorityHigh
	
	// PriorityUrgent for critical operations
	PriorityUrgent
)

// TEETaskScheduler manages task scheduling across TEE pairs
type TEETaskScheduler struct {
	meshService         *MeshService
	checkpointManager   *CheckpointManager
	
	// Task queues per priority level
	taskQueues          map[TaskPriority][]compute.ExecutionRequest
	
	// TEE node health status
	nodeHealth          map[string]bool // true = healthy, false = unhealthy
	
	// Pair utilization tracking
	pairLoad            map[string]float64 // 0.0-1.0 load factor
	
	// Security settings
	securityLevel       int // 1-5, higher = more security checks
	
	// Metrics and tracking
	taskStats           *TaskStatistics
	adaptiveRules       *AdaptiveRules
	
	// Task assignment strategy
	strategyType        string // "round-robin", "least-loaded", "security-optimized"
	
	// For NASDAQ-specific optimizations
	marketDataPriority  bool
	
	// Mutex protections
	mu                  sync.RWMutex
	healthMu            sync.RWMutex
	statsMu             sync.RWMutex
}

// TaskStatistics tracks performance metrics for the scheduler
type TaskStatistics struct {
	TotalTasks          uint64
	SuccessfulTasks     uint64
	FailedTasks         uint64
	
	TasksPerType        map[string]uint64
	TasksPerPriority    map[TaskPriority]uint64
	
	AvgExecutionTimeMs  float64
	AvgSchedulingDelayMs float64
	
	RecoveryTriggeredCount uint64
	
	LastTaskTimestamp  time.Time
}

// AdaptiveRules defines rules for adaptive scheduling
type AdaptiveRules struct {
	LoadThresholdHigh   float64 // e.g., 0.8
	LoadThresholdLow    float64 // e.g., 0.2
	
	SecurityThresholdAdjustment int // How much to adjust security level
	
	TaskPriorityBoost  bool // Whether to boost priority of waiting tasks
	
	NetworkLatencyAdjustment bool // Whether to adjust for network latency
	
	MaxQueueSizePerPriority map[TaskPriority]int
}

// NewTEETaskScheduler creates a new scheduler
func NewTEETaskScheduler(meshService *MeshService, checkpointManager *CheckpointManager) *TEETaskScheduler {
	scheduler := &TEETaskScheduler{
		meshService:       meshService,
		checkpointManager: checkpointManager,
		taskQueues:        make(map[TaskPriority][]compute.ExecutionRequest),
		nodeHealth:        make(map[string]bool),
		pairLoad:          make(map[string]float64),
		securityLevel:     3, // Default medium security
		strategyType:      "security-optimized",
		marketDataPriority: true, // NASDAQ-specific setting
		taskStats:         &TaskStatistics{
			TasksPerType:     make(map[string]uint64),
			TasksPerPriority: make(map[TaskPriority]uint64),
		},
		adaptiveRules:    &AdaptiveRules{
			LoadThresholdHigh: 0.8,
			LoadThresholdLow:  0.2,
			SecurityThresholdAdjustment: 1,
			TaskPriorityBoost: true,
			NetworkLatencyAdjustment: true,
			MaxQueueSizePerPriority: map[TaskPriority]int{
				PriorityLow:    100,
				PriorityNormal: 200,
				PriorityHigh:   300,
				PriorityUrgent: 500,
			},
		},
	}
	
	// Initialize priority queues
	scheduler.taskQueues[PriorityLow] = make([]compute.ExecutionRequest, 0)
	scheduler.taskQueues[PriorityNormal] = make([]compute.ExecutionRequest, 0)
	scheduler.taskQueues[PriorityHigh] = make([]compute.ExecutionRequest, 0)
	scheduler.taskQueues[PriorityUrgent] = make([]compute.ExecutionRequest, 0)
	
	return scheduler
}

// Start begins monitoring node health and processing tasks
func (s *TEETaskScheduler) Start(ctx context.Context) error {
	// Start health monitoring
	go s.monitorNodeHealth(ctx)
	
	// Start task dispatcher
	go s.dispatchTasks(ctx)
	
	// Start adaptive rule adjustment
	go s.adjustAdaptiveRules(ctx)
	
	log.Println("TEE Task Scheduler started")
	return nil
}

// SubmitTask adds a task to the appropriate queue
func (s *TEETaskScheduler) SubmitTask(ctx context.Context, req compute.ExecutionRequest, priority TaskPriority) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	// Check if queue is full
	if len(s.taskQueues[priority]) >= s.adaptiveRules.MaxQueueSizePerPriority[priority] {
		// If urgent, try to make room
		if priority == PriorityUrgent {
			// Drop the oldest low priority task if any exist
			if len(s.taskQueues[PriorityLow]) > 0 {
				s.taskQueues[PriorityLow] = s.taskQueues[PriorityLow][1:]
			} else {
				return "", fmt.Errorf("urgent queue is full and cannot make room")
			}
		} else {
			return "", fmt.Errorf("queue for priority %v is full", priority)
		}
	}
	
	// Generate task ID for this request
	taskID := generateTaskID()
	
	// Record the current time for stats
	s.statsMu.Lock()
	s.taskStats.LastTaskTimestamp = time.Now()
	s.statsMu.Unlock()
	
	// NASDAQ-specific: Check if this is market data and boost priority
	if s.marketDataPriority && isMarketDataTask(req) && priority < PriorityHigh {
		priority = PriorityHigh
	}
	
	// Add to queue
	s.taskQueues[priority] = append(s.taskQueues[priority], req)
	
	// Update stats
	s.statsMu.Lock()
	s.taskStats.TotalTasks++
	s.taskStats.TasksPerPriority[priority]++
	taskType := determineTaskType(req)
	s.taskStats.TasksPerType[taskType]++
	s.statsMu.Unlock()
	
	log.Printf("Task submitted with ID %s and priority %v", taskID, priority)
	return taskID, nil
}

// AssignTaskToPair selects the optimal TEE pair for a task
func (s *TEETaskScheduler) AssignTaskToPair(ctx context.Context, req compute.ExecutionRequest) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	// Get list of available pairs
	pairs, err := s.meshService.GetAvailablePairs()
	if err != nil {
		return "", fmt.Errorf("failed to get available pairs: %v", err)
	}
	
	if len(pairs) == 0 {
		return "", fmt.Errorf("no available TEE pairs")
	}
	
	// Filter out unhealthy pairs
	healthyPairs := make([]string, 0, len(pairs))
	s.healthMu.RLock()
	for _, pairID := range pairs {
		sgxNodeID := s.meshService.GetSGXNodeID(pairID)
		sevNodeID := s.meshService.GetSEVNodeID(pairID)
		
		if s.nodeHealth[sgxNodeID] && s.nodeHealth[sevNodeID] {
			healthyPairs = append(healthyPairs, pairID)
		}
	}
	s.healthMu.RUnlock()
	
	if len(healthyPairs) == 0 {
		return "", fmt.Errorf("no healthy TEE pairs available")
	}
	
	// Select pair based on strategy
	var selectedPair string
	
	switch s.strategyType {
	case "round-robin":
		selectedPair = s.selectPairRoundRobin(healthyPairs)
		
	case "least-loaded":
		selectedPair = s.selectLeastLoadedPair(healthyPairs)
		
	case "security-optimized":
		selectedPair = s.selectSecurityOptimizedPair(healthyPairs, req)
		
	default:
		// Default to least-loaded
		selectedPair = s.selectLeastLoadedPair(healthyPairs)
	}
	
	log.Printf("Task assigned to pair %s", selectedPair)
	return selectedPair, nil
}

// ExecuteTask runs a task on a specific TEE pair
func (s *TEETaskScheduler) ExecuteTask(ctx context.Context, pairID string, req compute.ExecutionRequest) (*compute.ExecutionResult, error) {
	startTime := time.Now()
	
	// Get service for pair
	pairService, err := s.meshService.GetPairService(pairID)
	if err != nil {
		return nil, fmt.Errorf("failed to get service for pair %s: %v", pairID, err)
	}
	
	// Execute on both TEEs in the pair
	result, err := pairService.Execute(ctx, req)
	if err != nil {
		// Update stats for failed task
		s.statsMu.Lock()
		s.taskStats.FailedTasks++
		s.statsMu.Unlock()
		
		// Check if we need to initiate recovery
		if shouldInitiateRecovery(err) {
			s.handleNodeFailure(ctx, pairID, err)
		}
		
		return nil, fmt.Errorf("execution failed on pair %s: %v", pairID, err)
	}
	
	// Update task statistics
	execTime := time.Since(startTime)
	s.statsMu.Lock()
	s.taskStats.SuccessfulTasks++
	s.taskStats.AvgExecutionTimeMs = updateRunningAverage(
		s.taskStats.AvgExecutionTimeMs,
		float64(execTime.Milliseconds()),
		s.taskStats.SuccessfulTasks,
	)
	s.statsMu.Unlock()
	
	// Update pair load
	s.updatePairLoad(pairID, execTime)
	
	log.Printf("Task completed successfully on pair %s in %v", pairID, execTime)
	return result.(*compute.ExecutionResult), nil
}

// UpdateSecurityLevel adjusts the security level based on conditions
func (s *TEETaskScheduler) UpdateSecurityLevel(newLevel int) {
	if newLevel < 1 {
		newLevel = 1
	}
	if newLevel > 5 {
		newLevel = 5
	}
	
	s.mu.Lock()
	defer s.mu.Unlock()
	
	prevLevel := s.securityLevel
	s.securityLevel = newLevel
	
	log.Printf("Security level updated from %d to %d", prevLevel, newLevel)
}

// GetTaskStatistics returns current task statistics
func (s *TEETaskScheduler) GetTaskStatistics() *TaskStatistics {
	s.statsMu.Lock()
	defer s.statsMu.Unlock()
	
	// Create a copy to avoid race conditions
	statsCopy := &TaskStatistics{
		TotalTasks:          s.taskStats.TotalTasks,
		SuccessfulTasks:     s.taskStats.SuccessfulTasks,
		FailedTasks:         s.taskStats.FailedTasks,
		AvgExecutionTimeMs:  s.taskStats.AvgExecutionTimeMs,
		AvgSchedulingDelayMs: s.taskStats.AvgSchedulingDelayMs,
		RecoveryTriggeredCount: s.taskStats.RecoveryTriggeredCount,
		LastTaskTimestamp:   s.taskStats.LastTaskTimestamp,
		TasksPerType:        make(map[string]uint64),
		TasksPerPriority:    make(map[TaskPriority]uint64),
	}
	
	// Copy maps
	for k, v := range s.taskStats.TasksPerType {
		statsCopy.TasksPerType[k] = v
	}
	
	for k, v := range s.taskStats.TasksPerPriority {
		statsCopy.TasksPerPriority[k] = v
	}
	
	return statsCopy
}

// Helper functions and internal methods

// monitorNodeHealth periodically checks the health of TEE nodes
func (s *TEETaskScheduler) monitorNodeHealth(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
			
		case <-ticker.C:
			s.updateNodeHealth(ctx)
		}
	}
}

// updateNodeHealth checks the health of all nodes and updates the internal state
func (s *TEETaskScheduler) updateNodeHealth(ctx context.Context) {
	nodes, err := s.meshService.GetAllNodes()
	if err != nil {
		log.Printf("Failed to get nodes for health check: %v", err)
		return
	}
	
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	
	for _, nodeID := range nodes {
		isHealthy, _ := s.meshService.CheckNodeHealth(ctx, nodeID)
		s.nodeHealth[nodeID] = isHealthy
	}
}

// dispatchTasks processes queued tasks
func (s *TEETaskScheduler) dispatchTasks(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
			
		case <-ticker.C:
			s.processNextTask(ctx)
		}
	}
}

// processNextTask takes the next highest priority task and executes it
func (s *TEETaskScheduler) processNextTask(ctx context.Context) {
	// Look for tasks starting with highest priority
	var req compute.ExecutionRequest
	var priority TaskPriority
	var found bool
	
	s.mu.Lock()
	// Check each priority level in order
	for p := PriorityUrgent; p >= PriorityLow; p-- {
		if len(s.taskQueues[p]) > 0 {
			req = s.taskQueues[p][0]
			s.taskQueues[p] = s.taskQueues[p][1:] // Remove task from queue
			priority = p
			found = true
			break
		}
	}
	s.mu.Unlock()
	
	if !found {
		// No tasks to process
		return
	}
	
	// Use current time for scheduling delay calculations as ExecutionRequest doesn't have a Timestamp field
	submittedTime := time.Now() // Using current time instead of parsing from request
	schedulingDelay := time.Since(submittedTime)
	
	// Update scheduling delay metric
	s.statsMu.Lock()
	s.taskStats.AvgSchedulingDelayMs = updateRunningAverage(
		s.taskStats.AvgSchedulingDelayMs,
		float64(schedulingDelay.Milliseconds()),
		s.taskStats.TotalTasks,
	)
	s.statsMu.Unlock()
	
	// Assign to pair and execute
	go func(r compute.ExecutionRequest, p TaskPriority) {
		// Create task-specific context with timeout based on priority
		timeoutDuration := getPriorityTimeout(p)
		taskCtx, cancel := context.WithTimeout(ctx, timeoutDuration)
		defer cancel()
		
		// Assign to a pair
		pairID, err := s.AssignTaskToPair(taskCtx, r)
		if err != nil {
			log.Printf("Failed to assign task: %v", err)
			return
		}
		
		// Execute task
		_, err = s.ExecuteTask(taskCtx, pairID, r)
		if err != nil {
			log.Printf("Task execution failed: %v", err)
		}
	}(req, priority)
}

// selectPairRoundRobin implements round-robin pair selection
func (s *TEETaskScheduler) selectPairRoundRobin(pairs []string) string {
	// Simple implementation - in a full solution, would track the last used pair
	return pairs[0]
}

// selectLeastLoadedPair selects the pair with the lowest load
func (s *TEETaskScheduler) selectLeastLoadedPair(pairs []string) string {
	var selectedPair string
	lowestLoad := 1.0 // Max load
	
	for _, pairID := range pairs {
		load := s.pairLoad[pairID]
		if load < lowestLoad {
			lowestLoad = load
			selectedPair = pairID
		}
	}
	
	return selectedPair
}

// selectSecurityOptimizedPair selects pair based on security needs
func (s *TEETaskScheduler) selectSecurityOptimizedPair(pairs []string, req compute.ExecutionRequest) string {
	// For high security tasks, prefer pairs with better attestation history
	taskSecurityNeeds := determineTaskSecurityLevel(req)
	
	if taskSecurityNeeds >= 4 && s.securityLevel >= 4 {
		// For high security tasks, find pair with best attestation metrics
		return s.selectPairWithBestAttestation(pairs)
	}
	
	// For normal tasks, balance load and security
	return s.selectLeastLoadedPair(pairs)
}

// selectPairWithBestAttestation finds the pair with best attestation metrics
func (s *TEETaskScheduler) selectPairWithBestAttestation(pairs []string) string {
	// This would check attestation history and verification scores
	// Simplified implementation for now
	if len(pairs) > 0 {
		return pairs[0]
	}
	return ""
}

// updatePairLoad updates the load factor for a pair
func (s *TEETaskScheduler) updatePairLoad(pairID string, execTime time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	// Simple load model based on recent execution time
	// A more sophisticated model would consider multiple factors
	
	// Execution time affects load - longer tasks = higher load
	loadFactor := float64(execTime.Milliseconds()) / 5000.0 // Normalize to 0-1 range
	if loadFactor > 1.0 {
		loadFactor = 1.0
	}
	
	// Blend with existing load (70% old, 30% new)
	currentLoad := s.pairLoad[pairID]
	s.pairLoad[pairID] = (currentLoad * 0.7) + (loadFactor * 0.3)
}

// handleNodeFailure initiates recovery for a failed node
func (s *TEETaskScheduler) handleNodeFailure(ctx context.Context, pairID string, execErr error) {
	// Determine which node failed
	failedNodeType := determineFailedNodeType(execErr)
	if failedNodeType == "" {
		log.Printf("Could not determine failed node type from error: %v", execErr)
		return
	}
	
	log.Printf("Initiating recovery for %s node in pair %s", failedNodeType, pairID)
	
	// Update recovery metrics
	s.statsMu.Lock()
	s.taskStats.RecoveryTriggeredCount++
	s.statsMu.Unlock()
	
	// Initiate recovery
	err := s.checkpointManager.RecoverPair(ctx, pairID, failedNodeType)
	if err != nil {
		log.Printf("Recovery failed for pair %s: %v", pairID, err)
	}
}

// adjustAdaptiveRules periodically adjusts scheduling parameters
func (s *TEETaskScheduler) adjustAdaptiveRules(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
			
		case <-ticker.C:
			s.updateAdaptiveRules()
		}
	}
}

// updateAdaptiveRules adjusts scheduler behavior based on metrics
func (s *TEETaskScheduler) updateAdaptiveRules() {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	// Example: Adjust security level based on failure rate
	s.statsMu.Lock()
	failureRate := float64(s.taskStats.FailedTasks) / float64(s.taskStats.TotalTasks+1)
	s.statsMu.Unlock()
	
	// If failure rate is high, increase security
	if failureRate > 0.05 && s.securityLevel < 5 {
		s.securityLevel++
		log.Printf("Increasing security level to %d due to high failure rate (%.2f%%)", 
			s.securityLevel, failureRate*100)
	}
	
	// If failure rate is low, consider decreasing security for performance
	if failureRate < 0.01 && s.securityLevel > 1 {
		s.securityLevel--
		log.Printf("Decreasing security level to %d due to low failure rate (%.2f%%)", 
			s.securityLevel, failureRate*100)
	}
	
	// Consider other adaptive adjustments here
}

// utility functions

// generateTaskID creates a unique ID for a task
func generateTaskID() string {
	return fmt.Sprintf("task-%s-%s", 
		time.Now().Format("20060102-150405"),
		generateRandomString(6))
}

// getPriorityTimeout returns timeout duration based on priority
func getPriorityTimeout(priority TaskPriority) time.Duration {
	switch priority {
	case PriorityLow:
		return 60 * time.Second
	case PriorityNormal:
		return 30 * time.Second
	case PriorityHigh:
		return 15 * time.Second
	case PriorityUrgent:
		return 5 * time.Second
	default:
		return 30 * time.Second
	}
}

// updateRunningAverage updates a running average
func updateRunningAverage(currentAvg, newValue float64, count uint64) float64 {
	if count <= 1 {
		return newValue
	}
	
	weight := 1.0 / float64(count)
	return (currentAvg * (1.0 - weight)) + (newValue * weight)
}

// determineTaskType categorizes the task
func determineTaskType(req compute.ExecutionRequest) string {
	// Simplified implementation - would analyze req fields
	if isMarketDataTask(req) {
		return "market_data"
	}
	return "general"
}

// isMarketDataTask checks if task is market data related
func isMarketDataTask(req compute.ExecutionRequest) bool {
	// In a real implementation, this would check task metadata or payload
	// to determine if it's a market data related task
	
	// For simulation purposes, let's classify ~20% of tasks as market data
	taskID := generateTaskID()
	return len(taskID) % 5 == 0
}

// determineTaskSecurityLevel estimates security requirements
func determineTaskSecurityLevel(req compute.ExecutionRequest) int {
	// In a real implementation, this would analyze the task to determine
	// its security requirements based on data sensitivity and operations
	
	// For simulation purposes, use a basic heuristic
	taskID := generateTaskID()
	if len(taskID) % 3 == 0 {
		return 3 // Highest security level
	} else if len(taskID) % 3 == 1 {
		return 2 // Medium security level
	}
	return 1 // Default security level
}

// shouldInitiateRecovery determines if error requires recovery
func shouldInitiateRecovery(err error) bool {
	errMsg := err.Error()
	
	// Look for indicators of node failure
	failureIndicators := []string{
		"attestation failure",
		"node unreachable",
		"verification failed",
		"enclave crashed",
		"tee failure",
	}
	
	for _, indicator := range failureIndicators {
		if strings.Contains(strings.ToLower(errMsg), indicator) {
			return true
		}
	}
	
	return false
}

// determineFailedNodeType analyzes error to determine node type
func determineFailedNodeType(err error) string {
	errMsg := err.Error()
	
	// Check for SGX-specific errors
	sgxIndicators := []string{"sgx", "enclave", "quoting"}
	for _, indicator := range sgxIndicators {
		if strings.Contains(strings.ToLower(errMsg), indicator) {
			return "SGX"
		}
	}
	
	// Check for SEV-specific errors
	sevIndicators := []string{"sev", "amd", "attestation report"}
	for _, indicator := range sevIndicators {
		if strings.Contains(strings.ToLower(errMsg), indicator) {
			return "SEV"
		}
	}
	
	// If we can't determine, default to SGX (more common to fail)
	return "SGX"
}
