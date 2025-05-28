// Package mesh provides a mesh network for TEE-to-TEE communication
// with hierarchical verification capabilities.
package mesh

import (
	"context"
	"fmt"
	"time"
)

// HierarchicalSystem encapsulates all components of the hierarchical verification system
type HierarchicalSystem struct {
	// Federation coordinator with hierarchical capabilities
	FederationCoordinator *HierarchicalFederationCoordinator
	
	// Cross-region verifier
	CrossVerifier *CrossVerifier
	
	// Hierarchical accumulator adapter
	AccumulatorAdapter *HierarchicalAccumulatorAdapter
	
	// Region identification
	RegionID string
}

// CreateHierarchicalSystem creates a complete hierarchical verification system
func CreateHierarchicalSystem(
	federationID string,
	regionID string,
	policy *FederationPolicy,
	snapshotCoordinator *RegionalSnapshotCoordinator,
	stateManager StateManager,
) (*HierarchicalSystem, error) {
	
	// Create a hierarchical federation coordinator using baseFC
	// Note: Using the canonical struct definition from hierarchical_types.go
	hfc, err := CreateHierarchicalFederationCoordinator(
		federationID,
		regionID,
		policy,
		snapshotCoordinator,
		stateManager,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create hierarchical federation coordinator: %w", err)
	}
	
	// The LastSyncTime is already initialized in CreateHierarchicalFederationCoordinator
	
	// Create a cross-verifier
	cv, err := NewCrossVerifier(regionID, "hierarchical_accumulator", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create cross verifier: %w", err)
	}
	
	// Initialize the accumulator adapter
	hfc.InitializeAccumulatorAdapter(cv)
	
	// Return the complete system
	return &HierarchicalSystem{
		FederationCoordinator: hfc,
		CrossVerifier:         cv,
		AccumulatorAdapter:    hfc.AccumulatorAdapter,
		RegionID:              regionID,
	}, nil
}

// InitializeHierarchicalSystem initializes the complete hierarchical accumulator system
// This is the main entry point for setting up the hierarchical verification infrastructure
func InitializeHierarchicalSystem(
	ctx context.Context,
	federationID string,
	regionID string,
	accumulatorPath string,
	policy *FederationPolicy,
	snapshotCoordinator *RegionalSnapshotCoordinator,
	stateManager StateManager,
) (*HierarchicalSystem, error) {
	// We'll create a base federation coordinator and federation config
	baseFC := NewFederationCoordinator(
		federationID,
		regionID,
		policy,
		snapshotCoordinator,
		stateManager,
	)

	// Create a hierarchical federation coordinator directly
	hierarchicalFC := &HierarchicalFederationCoordinator{
		FederationCoordinator: baseFC,
		RegionID:              regionID,
		ConnectedRegions:      make(map[string]*FederationRegion),
		LastSyncTime:          make(map[string]time.Time),
		SyncInterval:          60 * time.Second,
		Config:                policy,
		AccumulatorAdapter:    nil, // Will be initialized later
		CrossVerifier:         nil, // Will be initialized later
	}
	
	// Initialize the LastSyncTime for the current region
	hierarchicalFC.LastSyncTime[regionID] = time.Now()
	
	// Create federation coordinator wrapper
	federationWrapper := &CoordinatorFederation{
		regionalCoordinator: hierarchicalFC,
		regionID:            regionID,
		options:             DefaultFederationOptions(),
		metadataCache:       make(map[string]*FederationMetadata),
		lastHeartbeat:       make(map[string]time.Time),
	}
	
	// Create cross verifier
	crossVerifier, err := NewCrossVerifier(
		regionID,
		accumulatorPath,
		federationWrapper,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create cross verifier: %v", err)
	}
	
	// Initialize the accumulator adapter in the hierarchical federation coordinator
	hierarchicalFC.AccumulatorAdapter = NewHierarchicalAccumulatorAdapter(
		hierarchicalFC.FederationCoordinator,
		crossVerifier,
		&HierarchicalConfig{
			LevelNames: []string{"Regional", "CrossRegion", "Global"},
			UpdateFrequency: map[string]int64{
				"Regional":    1000,  // Regional: 1 second
				"CrossRegion": 5000,  // Cross-region: 5 seconds
				"Global":      30000, // Global: 30 seconds
			},
		},
	)
	
	return &HierarchicalSystem{
		FederationCoordinator: hierarchicalFC,
		CrossVerifier:         crossVerifier,
		AccumulatorAdapter:    hierarchicalFC.AccumulatorAdapter,
		RegionID:              regionID,
	}, nil
}

// VerifyAttestation verifies an attestation using the hierarchical system
func (hs *HierarchicalSystem) VerifyAttestation(
	ctx context.Context,
	attestation []byte,
	regions []string,
) (bool, error) {
	return hs.CrossVerifier.VerifyCrossRegion(ctx, attestation, regions)
}

// GetFederationCoordinator returns the FederationCoordinator
func (hs *HierarchicalSystem) GetFederationCoordinator() *FederationCoordinator {
	return hs.FederationCoordinator.GetFederationCoordinator()
}

// SynchronizeState synchronizes state across regions using the hierarchical system
func (hs *HierarchicalSystem) SynchronizeState(ctx context.Context) error {
	return hs.FederationCoordinator.SynchronizeHierarchical(ctx)
}

// GetHierarchicalAccumulatorState returns the HierarchicalAccumulatorState
func (hs *HierarchicalSystem) GetHierarchicalAccumulatorState() map[string]interface{} {
	return hs.AccumulatorAdapter.GetHierarchicalAccumulatorState()
}

// GetVerificationMetrics returns metrics for the hierarchical verification system
func (hs *HierarchicalSystem) GetVerificationMetrics() map[string]interface{} {
	return hs.AccumulatorAdapter.GetVerificationMetrics()
}

// ResetSystem resets the hierarchical system
func (hs *HierarchicalSystem) ResetSystem() {
	// Reset the cross verifier metrics
	hs.CrossVerifier.ResetVerificationMetrics()
	
	// Reset the federation coordinator
	// No explicit reset method in HierarchicalFederationCoordinator yet,
	// but we could update lastSyncTime
	hs.FederationCoordinator.UpdateLastSyncTime()
}
