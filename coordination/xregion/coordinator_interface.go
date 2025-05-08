package xregion

import (
	"context"
	"time"
)

// CrossRegionRLNCProvider defines the interface for RLNC-enhanced cross-region operations
type CrossRegionRLNCProvider interface {
	// RequestRangeProofWithRLNC requests a range proof from another region with RLNC resilience
	RequestRangeProofWithRLNC(ctx context.Context, req *RangeRequest) (*RangeResponse, error)

	// ExchangeAttestationWithRLNC exchanges TEE attestation data with another region using RLNC
	ExchangeAttestationWithRLNC(ctx context.Context, targetRegion string, attestation *AttestationDataWithRLNC) (*AttestationResponseWithRLNC, error)

	// RegisterCrossRegionVerifierWithRLNC registers a TEE pair as a cross-region verifier with RLNC resilience
	RegisterCrossRegionVerifierWithRLNC(ctx context.Context, targetRegion string, verifierInfo *VerifierInfoWithRLNC) error

	// BroadcastStateTransitionWithRLNC broadcasts a state transition to all connected regions using RLNC
	BroadcastStateTransitionWithRLNC(ctx context.Context, transition *StateTransitionWithRLNC) error

	// GetRLNCMetrics returns the current RLNC metrics
	GetRLNCMetrics() *RLNCMetrics

	// UpdateRLNCConfig updates the RLNC configuration
	UpdateRLNCConfig(config *RLNCTransportConfig)
}

// RLNCNetworkMonitor defines the interface for monitoring RLNC network health
type RLNCNetworkMonitor interface {
	// UpdateNetworkHealth updates the network health metrics for a region
	UpdateNetworkHealth(region string, health float64)

	// GetNetworkHealth returns the network health metrics for all regions
	GetNetworkHealth() map[string]float64
}

// CoordinatorState represents a coordinator's shared state with network health monitoring
type CoordinatorState struct {
	// Existing fields...

	// RLNC-specific fields
	rlncEnabled       bool
	rlncNetworkHealth map[string]float64
	rlncLastUpdated   map[string]time.Time
}

// NewCoordinatorState creates a new coordinator state
func NewCoordinatorState() *CoordinatorState {
	return &CoordinatorState{
		rlncNetworkHealth: make(map[string]float64),
		rlncLastUpdated:   make(map[string]time.Time),
	}
}

// UpdateRLNCNetworkHealth updates the RLNC network health for a region
func (s *CoordinatorState) UpdateRLNCNetworkHealth(region string, health float64) {
	s.rlncNetworkHealth[region] = health
	s.rlncLastUpdated[region] = time.Now()
}

// GetRLNCNetworkHealth returns the RLNC network health for a region
func (s *CoordinatorState) GetRLNCNetworkHealth(region string) float64 {
	health, ok := s.rlncNetworkHealth[region]
	if !ok {
		return 0.5 // Default to 50% health if unknown
	}
	return health
}

// IsRLNCEnabled returns whether RLNC is enabled for cross-region communication
func (s *CoordinatorState) IsRLNCEnabled() bool {
	return s.rlncEnabled
}

// SetRLNCEnabled sets whether RLNC is enabled for cross-region communication
func (s *CoordinatorState) SetRLNCEnabled(enabled bool) {
	s.rlncEnabled = enabled
}

// GetStaleRegions returns a list of regions whose health metrics are stale
func (s *CoordinatorState) GetStaleRegions(staleDuration time.Duration) []string {
	now := time.Now()
	var staleRegions []string

	for region, lastUpdated := range s.rlncLastUpdated {
		if now.Sub(lastUpdated) > staleDuration {
			staleRegions = append(staleRegions, region)
		}
	}

	return staleRegions
}

// CrossRegionOperationType represents the type of cross-region operation being performed
type CrossRegionOperationType int

const (
	// OperationTypeStateSync represents a state synchronization operation
	OperationTypeStateSync CrossRegionOperationType = iota
	// OperationTypeAttestation represents an attestation exchange operation
	OperationTypeAttestation
	// OperationTypeTransaction represents a transaction broadcast operation
	OperationTypeTransaction
	// OperationTypeVerifierRegistration represents a verifier registration operation
	OperationTypeVerifierRegistration
)

// ShouldUseRLNC determines whether RLNC should be used for a cross-region operation
// based on operation type, target region, and current network conditions
func ShouldUseRLNC(
	state *CoordinatorState,
	opType CrossRegionOperationType,
	targetRegion string,
	config *RLNCTransportConfig,
) bool {
	// If RLNC is disabled globally, don't use it
	if !state.IsRLNCEnabled() || config == nil || !config.Enabled {
		return false
	}

	// Get network health for the target region
	health := state.GetRLNCNetworkHealth(targetRegion)

	// For critical operations, use RLNC if network health is below 80%
	if opType == OperationTypeAttestation || opType == OperationTypeVerifierRegistration {
		return health < 0.8
	}

	// For state sync operations, use RLNC if network health is below 60%
	if opType == OperationTypeStateSync {
		return health < 0.6
	}

	// For transaction operations, use RLNC if network health is below 70%
	if opType == OperationTypeTransaction {
		return health < 0.7
	}

	// Default to using RLNC for unknown operation types if health is below 75%
	return health < 0.75
}
