package tee

import (
	"context"
	"fmt"
	"log"
	"math"
	"sync"
	"time"
)

// SecurityLevel represents different security requirement levels
type SecurityLevel int

const (
	SecurityLevelStandard SecurityLevel = iota // Standard security (default)
	SecurityLevelElevated                      // Elevated security (more attestations)
	SecurityLevelMaximum                       // Maximum security (highest attestation requirements)
)

// NetworkCondition represents network performance conditions
type NetworkCondition int

const (
	NetworkConditionNormal   NetworkCondition = iota // Normal network conditions
	NetworkConditionDegraded                         // Degraded network conditions
	NetworkConditionSevere                           // Severely degraded network conditions
)

// AdaptiveQuorumConfig provides configuration for adaptive quorum
type AdaptiveQuorumConfig struct {
	// Base quorum requirements
	BaseSGXQuorum int
	BaseSEVQuorum int

	// Maximum quorum sizes (based on total available nodes)
	MaxSGXQuorum int
	MaxSEVQuorum int

	// Minimum quorum sizes (absolute floor, regardless of conditions)
	MinSGXQuorum int
	MinSEVQuorum int

	// Latency thresholds for adjustment (in milliseconds)
	LatencyThresholdMs float64 // Threshold at which to start reducing quorum requirements
	CriticalLatencyMs  float64 // Threshold for minimum quorum requirements

	// Adaptation time windows
	AdaptationWindowDuration    time.Duration // How long to consider for conditions
	QuorumChangeStabilityWindow time.Duration // Minimum time between quorum changes

	// Security boost factors (percentage increase in quorum size)
	ElevatedSecurityFactor float64 // Factor for elevated security (e.g., 1.5 = 50% more nodes)
	MaximumSecurityFactor  float64 // Factor for maximum security (e.g., 2.0 = 100% more nodes)

	// Network condition factors (percentage decrease in quorum size)
	DegradedNetworkFactor float64 // Factor for degraded network (e.g., 0.8 = 20% fewer nodes)
	SevereNetworkFactor   float64 // Factor for severe network conditions (e.g., 0.6 = 40% fewer nodes)

	// Instrumentation
	CollectMetrics bool
}

// DefaultAdaptiveQuorumConfig returns default configuration
func DefaultAdaptiveQuorumConfig() *AdaptiveQuorumConfig {
	return &AdaptiveQuorumConfig{
		BaseSGXQuorum:               3,
		BaseSEVQuorum:               2,
		MaxSGXQuorum:                5,
		MaxSEVQuorum:                3,
		MinSGXQuorum:                2,
		MinSEVQuorum:                1,
		LatencyThresholdMs:          50.0,
		CriticalLatencyMs:           150.0,
		AdaptationWindowDuration:    1 * time.Minute,
		QuorumChangeStabilityWindow: 10 * time.Second,
		ElevatedSecurityFactor:      1.5,
		MaximumSecurityFactor:       2.0,
		DegradedNetworkFactor:       0.8,
		SevereNetworkFactor:         0.6,
		CollectMetrics:              true,
	}
}

// AdaptiveQuorumController dynamically adjusts quorum requirements based on
// current conditions, balancing security and performance
type AdaptiveQuorumController struct {
	config *AdaptiveQuorumConfig

	// Current state
	currentSGXQuorum int
	currentSEVQuorum int
	securityLevel    SecurityLevel
	networkCondition NetworkCondition

	// Performance history
	latencyHistory   []float64
	lastQuorumChange time.Time

	// Metrics
	metrics *AdaptiveQuorumMetrics

	// Mutex for thread safety
	mu sync.RWMutex
}

// AdaptiveQuorumMetrics tracks statistics about quorum adjustments
type AdaptiveQuorumMetrics struct {
	TotalAdaptations    int
	UpwardAdaptations   int
	DownwardAdaptations int
	LatencyTriggered    int
	SecurityTriggered   int
	NetworkTriggered    int

	AverageLatencyMs float64
	AverageSGXQuorum float64
	AverageSEVQuorum float64

	AvgSGXQuorumBySecLevel map[SecurityLevel]float64
	AvgSEVQuorumBySecLevel map[SecurityLevel]float64

	AvgSGXQuorumByNetCond map[NetworkCondition]float64
	AvgSEVQuorumByNetCond map[NetworkCondition]float64

	// Used for calculating averages
	totalLatencyMs   float64
	totalSGXQuorum   int
	totalSEVQuorum   int
	measurementCount int

	quorumHistory []struct {
		timestamp        time.Time
		sgxQuorum        int
		sevQuorum        int
		securityLevel    SecurityLevel
		networkCondition NetworkCondition
		latencyMs        float64
	}
}

// NewAdaptiveQuorumController creates a new controller with the given configuration
func NewAdaptiveQuorumController(config *AdaptiveQuorumConfig) *AdaptiveQuorumController {
	if config == nil {
		config = DefaultAdaptiveQuorumConfig()
	}

	return &AdaptiveQuorumController{
		config:           config,
		currentSGXQuorum: config.BaseSGXQuorum,
		currentSEVQuorum: config.BaseSEVQuorum,
		securityLevel:    SecurityLevelStandard,
		networkCondition: NetworkConditionNormal,
		latencyHistory:   make([]float64, 0, 100),
		lastQuorumChange: time.Now().Add(-24 * time.Hour), // Set to past so first change can happen immediately
		metrics: &AdaptiveQuorumMetrics{
			AvgSGXQuorumBySecLevel: make(map[SecurityLevel]float64),
			AvgSEVQuorumBySecLevel: make(map[SecurityLevel]float64),
			AvgSGXQuorumByNetCond:  make(map[NetworkCondition]float64),
			AvgSEVQuorumByNetCond:  make(map[NetworkCondition]float64),
			quorumHistory: make([]struct {
				timestamp        time.Time
				sgxQuorum        int
				sevQuorum        int
				securityLevel    SecurityLevel
				networkCondition NetworkCondition
				latencyMs        float64
			}, 0, 1000),
		},
	}
}

// SetSecurityLevel sets the current security level
func (a *AdaptiveQuorumController) SetSecurityLevel(level SecurityLevel) {
	a.mu.Lock()
	defer a.mu.Unlock()

	oldLevel := a.securityLevel
	a.securityLevel = level

	// Only recalculate if security level actually changed
	if oldLevel != level {
		a.recalculateQuorumRequirements()

		if a.config.CollectMetrics {
			a.metrics.SecurityTriggered++
			if a.currentSGXQuorum > a.config.BaseSGXQuorum || a.currentSEVQuorum > a.config.BaseSEVQuorum {
				a.metrics.UpwardAdaptations++
			} else {
				a.metrics.DownwardAdaptations++
			}
			a.metrics.TotalAdaptations++
		}
	}
}

// SetNetworkCondition sets the current network condition
func (a *AdaptiveQuorumController) SetNetworkCondition(condition NetworkCondition) {
	a.mu.Lock()
	defer a.mu.Unlock()

	oldCondition := a.networkCondition
	a.networkCondition = condition

	// Only recalculate if network condition actually changed
	if oldCondition != condition {
		a.recalculateQuorumRequirements()

		if a.config.CollectMetrics {
			a.metrics.NetworkTriggered++
			if a.currentSGXQuorum < a.config.BaseSGXQuorum || a.currentSEVQuorum < a.config.BaseSEVQuorum {
				a.metrics.DownwardAdaptations++
			} else {
				a.metrics.UpwardAdaptations++
			}
			a.metrics.TotalAdaptations++
		}
	}
}

// RecordLatency records an attestation latency measurement and potentially triggers adjustment
func (a *AdaptiveQuorumController) RecordLatency(latencyMs float64) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Add to history
	a.latencyHistory = append(a.latencyHistory, latencyMs)

	// Trim history if it gets too long
	// Keep the most recent readings within our adaptation window
	maxHistorySize := 1000 // Hard cap to prevent memory growth
	if len(a.latencyHistory) > maxHistorySize {
		a.latencyHistory = a.latencyHistory[len(a.latencyHistory)-maxHistorySize:]
	}

	// Update metrics
	if a.config.CollectMetrics {
		a.metrics.totalLatencyMs += latencyMs
		a.metrics.measurementCount++
		a.metrics.AverageLatencyMs = a.metrics.totalLatencyMs / float64(a.metrics.measurementCount)
	}

	// Check if we should adapt based on latency
	// Only adapt if we've waited long enough since last change (for stability)
	if time.Since(a.lastQuorumChange) > a.config.QuorumChangeStabilityWindow {
		// Calculate recent average latency
		recentLatencySum := 0.0
		recentReadings := 0

		// Look at recent readings within our history
		historySize := len(a.latencyHistory)

		// Take the most recent readings that fit within our window
		// This is an approximation since we don't store timestamps with each reading
		for i := historySize - 1; i >= 0 && recentReadings < 20; i-- {
			recentLatencySum += a.latencyHistory[i]
			recentReadings++
		}

		if recentReadings > 0 {
			avgLatency := recentLatencySum / float64(recentReadings)

			// Check if latency is high enough to warrant action
			if avgLatency > a.config.LatencyThresholdMs {
				// Latency is high, consider reducing quorum requirements
				oldSGXQuorum := a.currentSGXQuorum
				oldSEVQuorum := a.currentSEVQuorum

				// Calculate reduction factor based on how far over threshold we are
				latencyFactor := math.Min(1.0,
					(a.config.CriticalLatencyMs-avgLatency)/
						(a.config.CriticalLatencyMs-a.config.LatencyThresholdMs))

				if latencyFactor < 0 {
					// We're beyond critical latency, use minimum quorums
					a.currentSGXQuorum = a.config.MinSGXQuorum
					a.currentSEVQuorum = a.config.MinSEVQuorum
				} else {
					// Scale between base and minimum based on latency
					sgxRange := float64(a.currentSGXQuorum - a.config.MinSGXQuorum)
					sevRange := float64(a.currentSEVQuorum - a.config.MinSEVQuorum)

					// Apply gradual reduction based on latency factor
					a.currentSGXQuorum = int(math.Max(
						float64(a.config.MinSGXQuorum),
						float64(a.config.MinSGXQuorum)+sgxRange*latencyFactor))

					a.currentSEVQuorum = int(math.Max(
						float64(a.config.MinSEVQuorum),
						float64(a.config.MinSEVQuorum)+sevRange*latencyFactor))
				}

				// Only consider this an adaptation if quorum actually changed
				if oldSGXQuorum != a.currentSGXQuorum || oldSEVQuorum != a.currentSEVQuorum {
					a.lastQuorumChange = time.Now()

					if a.config.CollectMetrics {
						a.metrics.LatencyTriggered++
						a.metrics.DownwardAdaptations++
						a.metrics.TotalAdaptations++
					}

					log.Printf("Adaptive quorum reduced due to high latency (%.2fms): SGX %d->%d, SEV %d->%d",
						avgLatency, oldSGXQuorum, a.currentSGXQuorum, oldSEVQuorum, a.currentSEVQuorum)
				}
			} else if avgLatency < a.config.LatencyThresholdMs*0.7 &&
				(a.currentSGXQuorum < a.config.BaseSGXQuorum || a.currentSEVQuorum < a.config.BaseSEVQuorum) {
				// Latency is low, consider returning to base requirements if we're currently reduced
				// Only do this if we're significantly below threshold to prevent oscillation
				oldSGXQuorum := a.currentSGXQuorum
				oldSEVQuorum := a.currentSEVQuorum

				// Recalculate quorum requirements using original factors
				a.recalculateQuorumRequirements()

				// Only consider this an adaptation if quorum actually changed
				if oldSGXQuorum != a.currentSGXQuorum || oldSEVQuorum != a.currentSEVQuorum {
					a.lastQuorumChange = time.Now()

					if a.config.CollectMetrics {
						a.metrics.LatencyTriggered++
						a.metrics.UpwardAdaptations++
						a.metrics.TotalAdaptations++
					}

					log.Printf("Adaptive quorum increased due to low latency (%.2fms): SGX %d->%d, SEV %d->%d",
						avgLatency, oldSGXQuorum, a.currentSGXQuorum, oldSEVQuorum, a.currentSEVQuorum)
				}
			}
		}
	}

	// Update metrics
	if a.config.CollectMetrics {
		a.metrics.totalSGXQuorum += a.currentSGXQuorum
		a.metrics.totalSEVQuorum += a.currentSEVQuorum
		a.metrics.AverageSGXQuorum = float64(a.metrics.totalSGXQuorum) / float64(a.metrics.measurementCount)
		a.metrics.AverageSEVQuorum = float64(a.metrics.totalSEVQuorum) / float64(a.metrics.measurementCount)

		// Track by security level
		levelCount := a.metrics.AvgSGXQuorumBySecLevel[a.securityLevel]
		a.metrics.AvgSGXQuorumBySecLevel[a.securityLevel] =
			(levelCount*float64(a.metrics.measurementCount-1) + float64(a.currentSGXQuorum)) /
				float64(a.metrics.measurementCount)

		levelCount = a.metrics.AvgSEVQuorumBySecLevel[a.securityLevel]
		a.metrics.AvgSEVQuorumBySecLevel[a.securityLevel] =
			(levelCount*float64(a.metrics.measurementCount-1) + float64(a.currentSEVQuorum)) /
				float64(a.metrics.measurementCount)

		// Track by network condition
		condCount := a.metrics.AvgSGXQuorumByNetCond[a.networkCondition]
		a.metrics.AvgSGXQuorumByNetCond[a.networkCondition] =
			(condCount*float64(a.metrics.measurementCount-1) + float64(a.currentSGXQuorum)) /
				float64(a.metrics.measurementCount)

		condCount = a.metrics.AvgSEVQuorumByNetCond[a.networkCondition]
		a.metrics.AvgSEVQuorumByNetCond[a.networkCondition] =
			(condCount*float64(a.metrics.measurementCount-1) + float64(a.currentSEVQuorum)) /
				float64(a.metrics.measurementCount)

		// Add to history
		a.metrics.quorumHistory = append(a.metrics.quorumHistory, struct {
			timestamp        time.Time
			sgxQuorum        int
			sevQuorum        int
			securityLevel    SecurityLevel
			networkCondition NetworkCondition
			latencyMs        float64
		}{
			timestamp:        time.Now(),
			sgxQuorum:        a.currentSGXQuorum,
			sevQuorum:        a.currentSEVQuorum,
			securityLevel:    a.securityLevel,
			networkCondition: a.networkCondition,
			latencyMs:        latencyMs,
		})

		// Trim history if it gets too long
		if len(a.metrics.quorumHistory) > 1000 {
			a.metrics.quorumHistory = a.metrics.quorumHistory[len(a.metrics.quorumHistory)-1000:]
		}
	}
}

// GetQuorumRequirements returns the current quorum requirements for SGX and SEV
func (a *AdaptiveQuorumController) GetQuorumRequirements() (sgxQuorum, sevQuorum int) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.currentSGXQuorum, a.currentSEVQuorum
}

// GetMetrics returns a copy of the current metrics
func (a *AdaptiveQuorumController) GetMetrics() AdaptiveQuorumMetrics {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.config.CollectMetrics {
		return *a.metrics
	}

	return AdaptiveQuorumMetrics{}
}

// ResetMetrics resets all metrics
func (a *AdaptiveQuorumController) ResetMetrics() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.metrics = &AdaptiveQuorumMetrics{
		AvgSGXQuorumBySecLevel: make(map[SecurityLevel]float64),
		AvgSEVQuorumBySecLevel: make(map[SecurityLevel]float64),
		AvgSGXQuorumByNetCond:  make(map[NetworkCondition]float64),
		AvgSEVQuorumByNetCond:  make(map[NetworkCondition]float64),
		quorumHistory: make([]struct {
			timestamp        time.Time
			sgxQuorum        int
			sevQuorum        int
			securityLevel    SecurityLevel
			networkCondition NetworkCondition
			latencyMs        float64
		}, 0, 1000),
	}
}

// recalculateQuorumRequirements recalculates quorum requirements based on current
// security level and network conditions
func (a *AdaptiveQuorumController) recalculateQuorumRequirements() {
	// Start with base requirements
	sgxQuorum := float64(a.config.BaseSGXQuorum)
	sevQuorum := float64(a.config.BaseSEVQuorum)

	// Apply security level factor
	securityFactor := 1.0
	switch a.securityLevel {
	case SecurityLevelStandard:
		securityFactor = 1.0
	case SecurityLevelElevated:
		securityFactor = a.config.ElevatedSecurityFactor
	case SecurityLevelMaximum:
		securityFactor = a.config.MaximumSecurityFactor
	}

	sgxQuorum *= securityFactor
	sevQuorum *= securityFactor

	// Apply network condition factor
	networkFactor := 1.0
	switch a.networkCondition {
	case NetworkConditionNormal:
		networkFactor = 1.0
	case NetworkConditionDegraded:
		networkFactor = a.config.DegradedNetworkFactor
	case NetworkConditionSevere:
		networkFactor = a.config.SevereNetworkFactor
	}

	sgxQuorum *= networkFactor
	sevQuorum *= networkFactor

	// Apply min/max bounds
	sgxQuorum = math.Min(float64(a.config.MaxSGXQuorum), math.Max(float64(a.config.MinSGXQuorum), sgxQuorum))
	sevQuorum = math.Min(float64(a.config.MaxSEVQuorum), math.Max(float64(a.config.MinSEVQuorum), sevQuorum))

	// Round to nearest integer
	a.currentSGXQuorum = int(math.Round(sgxQuorum))
	a.currentSEVQuorum = int(math.Round(sevQuorum))
}

// GetDetailedQuorumReport generates a detailed report on quorum adaptations
func (a *AdaptiveQuorumController) GetDetailedQuorumReport() map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if !a.config.CollectMetrics {
		return map[string]interface{}{
			"error": "Metrics collection is disabled",
		}
	}

	// Calculate current adaptation level compared to base
	sgxAdaptationLevel := float64(a.currentSGXQuorum) / float64(a.config.BaseSGXQuorum)
	sevAdaptationLevel := float64(a.currentSEVQuorum) / float64(a.config.BaseSEVQuorum)

	// Calculate average response time by quorum level
	sgxQuorumToLatency := make(map[int]float64)
	sevQuorumToLatency := make(map[int]float64)
	quorumCounts := make(map[int]int)

	for _, entry := range a.metrics.quorumHistory {
		sgxQuorumToLatency[entry.sgxQuorum] += entry.latencyMs
		sevQuorumToLatency[entry.sevQuorum] += entry.latencyMs
		quorumCounts[entry.sgxQuorum]++
		quorumCounts[entry.sevQuorum]++
	}

	for q, sum := range sgxQuorumToLatency {
		if count := quorumCounts[q]; count > 0 {
			sgxQuorumToLatency[q] = sum / float64(count)
		}
	}

	for q, sum := range sevQuorumToLatency {
		if count := quorumCounts[q]; count > 0 {
			sevQuorumToLatency[q] = sum / float64(count)
		}
	}

	return map[string]interface{}{
		"current_sgx_quorum":          a.currentSGXQuorum,
		"current_sev_quorum":          a.currentSEVQuorum,
		"base_sgx_quorum":             a.config.BaseSGXQuorum,
		"base_sev_quorum":             a.config.BaseSEVQuorum,
		"sgx_adaptation_level":        sgxAdaptationLevel,
		"sev_adaptation_level":        sevAdaptationLevel,
		"security_level":              a.securityLevel,
		"network_condition":           a.networkCondition,
		"average_latency_ms":          a.metrics.AverageLatencyMs,
		"total_adaptations":           a.metrics.TotalAdaptations,
		"upward_adaptations":          a.metrics.UpwardAdaptations,
		"downward_adaptations":        a.metrics.DownwardAdaptations,
		"latency_triggered":           a.metrics.LatencyTriggered,
		"security_triggered":          a.metrics.SecurityTriggered,
		"network_triggered":           a.metrics.NetworkTriggered,
		"average_sgx_quorum":          a.metrics.AverageSGXQuorum,
		"average_sev_quorum":          a.metrics.AverageSEVQuorum,
		"sgx_quorum_latency_map":      sgxQuorumToLatency,
		"sev_quorum_latency_map":      sevQuorumToLatency,
		"avg_sgx_quorum_by_sec_level": a.metrics.AvgSGXQuorumBySecLevel,
		"avg_sev_quorum_by_sec_level": a.metrics.AvgSEVQuorumBySecLevel,
		"avg_sgx_quorum_by_net_cond":  a.metrics.AvgSGXQuorumByNetCond,
		"avg_sev_quorum_by_net_cond":  a.metrics.AvgSEVQuorumByNetCond,
	}
}

// AdaptiveQuorumAdapter is an adapter component that integrates adaptive quorum
// with existing TEE verification logic
type AdaptiveQuorumAdapter struct {
	controller *AdaptiveQuorumController
}

// NewAdaptiveQuorumAdapter creates a new adapter with the given controller
func NewAdaptiveQuorumAdapter(controller *AdaptiveQuorumController) *AdaptiveQuorumAdapter {
	if controller == nil {
		controller = NewAdaptiveQuorumController(nil)
	}

	return &AdaptiveQuorumAdapter{
		controller: controller,
	}
}

// GetRequiredQuorum returns the current adaptive quorum requirements
func (a *AdaptiveQuorumAdapter) GetRequiredQuorum(ctx context.Context, teeType string) (int, error) {
	sgxQuorum, sevQuorum := a.controller.GetQuorumRequirements()

	switch teeType {
	case "SGX":
		return sgxQuorum, nil
	case "SEV":
		return sevQuorum, nil
	default:
		return 0, fmt.Errorf("unknown TEE type: %s", teeType)
	}
}

// RecordVerificationLatency records a verification latency measurement
func (a *AdaptiveQuorumAdapter) RecordVerificationLatency(teeType string, latencyMs float64) {
	a.controller.RecordLatency(latencyMs)
}

// DetectNetworkCondition automatically detects network conditions based on recent history
func (a *AdaptiveQuorumAdapter) DetectNetworkCondition(ctx context.Context) NetworkCondition {
	metrics := a.controller.GetMetrics()

	// Simple heuristic based on average latency
	if metrics.AverageLatencyMs > 100.0 {
		return NetworkConditionSevere
	} else if metrics.AverageLatencyMs > 50.0 {
		return NetworkConditionDegraded
	}

	return NetworkConditionNormal
}

// AutoAdjustForNetworkConditions automatically monitors and adjusts for network conditions
func (a *AdaptiveQuorumAdapter) AutoAdjustForNetworkConditions(ctx context.Context, monitorInterval time.Duration) {
	ticker := time.NewTicker(monitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			condition := a.DetectNetworkCondition(ctx)
			a.controller.SetNetworkCondition(condition)
		}
	}
}

// IsQuorumSatisfied checks if a quorum size satisfies current requirements
func (a *AdaptiveQuorumAdapter) IsQuorumSatisfied(teeType string, quorumSize int) bool {
	requiredQuorum, err := a.GetRequiredQuorum(context.Background(), teeType)
	if err != nil {
		// If we can't determine required quorum, default to the standard base values
		switch teeType {
		case "SGX":
			requiredQuorum = a.controller.config.BaseSGXQuorum
		case "SEV":
			requiredQuorum = a.controller.config.BaseSEVQuorum
		default:
			// If unknown TEE type, require at least 1 attestation
			requiredQuorum = 1
		}
	}

	return quorumSize >= requiredQuorum
}
