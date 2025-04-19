// Package metrics provides monitoring capabilities for the regional TEE architecture
package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// TEEMetrics collects and exports performance and security metrics for the TEE architecture
type TEEMetrics struct {
	// Performance metrics
	transactionCounter    *prometheus.CounterVec
	verificationLatency   *prometheus.HistogramVec
	crossRegionalLatency  *prometheus.HistogramVec
	
	// Security metrics
	attestationCounter    *prometheus.CounterVec
	accumulatorUpdates    *prometheus.CounterVec
	policyVerifications   *prometheus.CounterVec
	
	// Resource metrics
	teeResourceUtilization *prometheus.GaugeVec
	
	// TEE-specific metrics
	sgxAttestations       *prometheus.CounterVec
	sevAttestations       *prometheus.CounterVec
	
	// Cross-regional metrics
	crossRegionalOps      *prometheus.CounterVec
	
	// Regional state metrics
	regionalStateSize     *prometheus.GaugeVec
	
	mu sync.RWMutex
}

// NewTEEMetrics creates a new metrics collector for the regional TEE architecture
func NewTEEMetrics(registry *prometheus.Registry) *TEEMetrics {
	m := &TEEMetrics{}
	
	// Performance metrics
	m.transactionCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "tee",
			Name:      "transactions_total",
			Help:      "Total number of transactions processed by TEE regions",
		},
		[]string{"region_id", "tee_type", "operation_type"},
	)
	
	m.verificationLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "tee",
			Name:      "verification_latency_ms",
			Help:      "Attestation verification latency in milliseconds",
			Buckets:   prometheus.ExponentialBuckets(0.1, 2, 15), // 0.1ms to 1.6s
		},
		[]string{"region_id", "verification_type"},
	)
	
	m.crossRegionalLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "tee",
			Name:      "cross_regional_latency_ms",
			Help:      "Cross-regional operation latency in milliseconds",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 10), // 1ms to 512ms
		},
		[]string{"source_region", "target_region", "operation_type"},
	)
	
	// Security metrics
	m.attestationCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "tee",
			Name:      "attestations_total",
			Help:      "Total number of attestations processed",
		},
		[]string{"region_id", "tee_type", "result"},
	)
	
	m.accumulatorUpdates = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "tee",
			Name:      "accumulator_updates_total",
			Help:      "Total number of cryptographic accumulator updates",
		},
		[]string{"region_id", "update_type"},
	)
	
	m.policyVerifications = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "tee",
			Name:      "policy_verifications_total",
			Help:      "Total number of regional policy verifications",
		},
		[]string{"region_id", "policy_type", "result"},
	)
	
	// Resource metrics
	m.teeResourceUtilization = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "tee",
			Name:      "resource_utilization",
			Help:      "Resource utilization metrics for TEEs",
		},
		[]string{"region_id", "tee_id", "resource_type"},
	)
	
	// TEE-specific metrics
	m.sgxAttestations = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "tee",
			Name:      "sgx_attestations_total",
			Help:      "Total number of Intel SGX attestations",
		},
		[]string{"region_id", "attestation_type", "result"},
	)
	
	m.sevAttestations = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "tee",
			Name:      "sev_attestations_total",
			Help:      "Total number of AMD SEV attestations",
		},
		[]string{"region_id", "attestation_type", "result"},
	)
	
	// Cross-regional metrics
	m.crossRegionalOps = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "tee",
			Name:      "cross_regional_operations_total",
			Help:      "Total number of cross-regional operations",
		},
		[]string{"source_region", "target_region", "operation_type", "result"},
	)
	
	// Regional state metrics
	m.regionalStateSize = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "tee",
			Name:      "regional_state_size_bytes",
			Help:      "Size of regional state in bytes",
		},
		[]string{"region_id", "state_type"},
	)
	
	// Register all metrics with the provided registry
	registry.MustRegister(
		m.transactionCounter,
		m.verificationLatency,
		m.crossRegionalLatency,
		m.attestationCounter,
		m.accumulatorUpdates,
		m.policyVerifications,
		m.teeResourceUtilization,
		m.sgxAttestations,
		m.sevAttestations,
		m.crossRegionalOps,
		m.regionalStateSize,
	)
	
	return m
}

// RecordTransaction records a transaction processed by a TEE
func (m *TEEMetrics) RecordTransaction(regionID, teeType, opType string) {
	m.transactionCounter.WithLabelValues(regionID, teeType, opType).Inc()
}

// RecordVerificationLatency records the latency of an attestation verification
func (m *TEEMetrics) RecordVerificationLatency(regionID, verificationType string, latencyMs float64) {
	m.verificationLatency.WithLabelValues(regionID, verificationType).Observe(latencyMs)
}

// RecordCrossRegionalLatency records the latency of a cross-regional operation
func (m *TEEMetrics) RecordCrossRegionalLatency(sourceRegion, targetRegion, opType string, latencyMs float64) {
	m.crossRegionalLatency.WithLabelValues(sourceRegion, targetRegion, opType).Observe(latencyMs)
}

// RecordAttestation records an attestation operation
func (m *TEEMetrics) RecordAttestation(regionID, teeType, result string) {
	m.attestationCounter.WithLabelValues(regionID, teeType, result).Inc()
	
	// Also record for specific TEE types
	switch teeType {
	case "sgx":
		m.sgxAttestations.WithLabelValues(regionID, "standard", result).Inc()
	case "sev":
		m.sevAttestations.WithLabelValues(regionID, "standard", result).Inc()
	}
}

// RecordAccumulatorUpdate records an update to the cryptographic accumulator
func (m *TEEMetrics) RecordAccumulatorUpdate(regionID, updateType string) {
	m.accumulatorUpdates.WithLabelValues(regionID, updateType).Inc()
}

// RecordPolicyVerification records a regional policy verification
func (m *TEEMetrics) RecordPolicyVerification(regionID, policyType, result string) {
	m.policyVerifications.WithLabelValues(regionID, policyType, result).Inc()
}

// RecordTEEResourceUtilization records resource utilization for a TEE
func (m *TEEMetrics) RecordTEEResourceUtilization(regionID, teeID, resourceType string, value float64) {
	m.teeResourceUtilization.WithLabelValues(regionID, teeID, resourceType).Set(value)
}

// RecordCrossRegionalOperation records a cross-regional operation
func (m *TEEMetrics) RecordCrossRegionalOperation(sourceRegion, targetRegion, opType, result string) {
	m.crossRegionalOps.WithLabelValues(sourceRegion, targetRegion, opType, result).Inc()
}

// RecordRegionalStateSize records the size of a regional state
func (m *TEEMetrics) RecordRegionalStateSize(regionID, stateType string, sizeBytes float64) {
	m.regionalStateSize.WithLabelValues(regionID, stateType).Set(sizeBytes)
}
