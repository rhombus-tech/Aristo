//go:build testing
// +build testing

package mesh

import (
	"sync"
	"time"
)

// Mock TEEMetrics for testing
type mockTEEMetrics struct{}

// MetricsCollector mock for testing environments
type MetricsCollector struct {
	regionID string
	teeType  string
	teeID    string
	mu       sync.RWMutex
}

// NewMetricsCollector creates a mock metrics collector for testing
func NewMetricsCollector(teeMetrics interface{}, regionID, teeType, teeID string) *MetricsCollector {
	return &MetricsCollector{
		regionID: regionID,
		teeType:  teeType,
		teeID:    teeID,
	}
}

// RecordSnapshotCreation is a no-op in testing mode
func (m *MetricsCollector) RecordSnapshotCreation(snapshotType string, sizeBytes int) {}

// RecordSnapshotVerification is a no-op in testing mode
func (m *MetricsCollector) RecordSnapshotVerification(sourceRegion, targetRegion string, latencyMs float64, success bool) {}

// RecordAttestationVerification is a no-op in testing mode
func (m *MetricsCollector) RecordAttestationVerification(attestationType string, latencyMs float64, success bool) {}

// RecordAccumulatorUpdate is a no-op in testing mode
func (m *MetricsCollector) RecordAccumulatorUpdate(updateType string) {}

// RecordPolicyEnforcement is a no-op in testing mode
func (m *MetricsCollector) RecordPolicyEnforcement(policyType string, success bool) {}

// RecordResourceUtilization is a no-op in testing mode
func (m *MetricsCollector) RecordResourceUtilization(resourceType string, value float64) {}

// MeasureOperation executes an operation without measuring in test mode
func (m *MetricsCollector) MeasureOperation(opType string, fn func() (bool, error)) (bool, error) {
	return fn()
}

// MeasureCrossRegionalOperation executes a cross-regional operation without measuring in test mode
func (m *MetricsCollector) MeasureCrossRegionalOperation(sourceRegion, targetRegion, opType string, fn func() (bool, error)) (bool, error) {
	return fn()
}
