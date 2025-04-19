package mesh

import (
	"sync"
	"time"

	"github.com/rhombus-tech/vm/tee/observability/metrics"
)

// MetricsCollector collects TEE mesh metrics and forwards them to the Prometheus exporter
type MetricsCollector struct {
	teeMetrics *metrics.TEEMetrics
	regionID   string
	teeType    string
	teeID      string
	mu         sync.RWMutex
}

// NewMetricsCollector creates a new metrics collector for the mesh network
func NewMetricsCollector(teeMetrics *metrics.TEEMetrics, regionID, teeType, teeID string) *MetricsCollector {
	return &MetricsCollector{
		teeMetrics: teeMetrics,
		regionID:   regionID,
		teeType:    teeType,
		teeID:      teeID,
	}
}

// RecordSnapshotCreation records metrics for snapshot creation
func (c *MetricsCollector) RecordSnapshotCreation(snapshotType string, sizeBytes int) {
	c.teeMetrics.RecordTransaction(c.regionID, c.teeType, "snapshot_creation")
	c.teeMetrics.RecordRegionalStateSize(c.regionID, snapshotType, float64(sizeBytes))
}

// RecordSnapshotVerification records metrics for snapshot verification
func (c *MetricsCollector) RecordSnapshotVerification(sourceRegion, targetRegion string, latencyMs float64, success bool) {
	result := "success"
	if !success {
		result = "failure"
	}
	
	c.teeMetrics.RecordCrossRegionalOperation(sourceRegion, targetRegion, "snapshot_verification", result)
	c.teeMetrics.RecordCrossRegionalLatency(sourceRegion, targetRegion, "snapshot_verification", latencyMs)
}

// RecordAttestationVerification records metrics for attestation verification
func (c *MetricsCollector) RecordAttestationVerification(attestationType string, latencyMs float64, success bool) {
	result := "success"
	if !success {
		result = "failure"
	}
	
	c.teeMetrics.RecordAttestation(c.regionID, c.teeType, result)
	c.teeMetrics.RecordVerificationLatency(c.regionID, attestationType, latencyMs)
}

// RecordAccumulatorUpdate records metrics for accumulator updates
func (c *MetricsCollector) RecordAccumulatorUpdate(updateType string) {
	c.teeMetrics.RecordAccumulatorUpdate(c.regionID, updateType)
}

// RecordPolicyEnforcement records metrics for policy enforcement
func (c *MetricsCollector) RecordPolicyEnforcement(policyType string, success bool) {
	result := "success"
	if !success {
		result = "failure"
	}
	
	c.teeMetrics.RecordPolicyVerification(c.regionID, policyType, result)
}

// RecordResourceUtilization records TEE resource utilization metrics
func (c *MetricsCollector) RecordResourceUtilization(resourceType string, value float64) {
	c.teeMetrics.RecordTEEResourceUtilization(c.regionID, c.teeID, resourceType, value)
}

// MeasureOperation executes an operation and records its latency
func (c *MetricsCollector) MeasureOperation(opType string, fn func() (bool, error)) (bool, error) {
	startTime := time.Now()
	
	success, err := fn()
	
	latencyMs := float64(time.Since(startTime).Milliseconds())
	c.teeMetrics.RecordVerificationLatency(c.regionID, opType, latencyMs)
	
	return success, err
}

// MeasureCrossRegionalOperation executes a cross-regional operation and records its latency
func (c *MetricsCollector) MeasureCrossRegionalOperation(sourceRegion, targetRegion, opType string, fn func() (bool, error)) (bool, error) {
	startTime := time.Now()
	
	success, err := fn()
	
	latencyMs := float64(time.Since(startTime).Milliseconds())
	
	result := "success"
	if !success || err != nil {
		result = "failure"
	}
	
	c.teeMetrics.RecordCrossRegionalOperation(sourceRegion, targetRegion, opType, result)
	c.teeMetrics.RecordCrossRegionalLatency(sourceRegion, targetRegion, opType, latencyMs)
	
	return success, err
}
