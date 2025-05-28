// Package mesh provides a mesh network for TEE-to-TEE communication
// with hierarchical verification capabilities.
package mesh

import (
	"context"
	"fmt"
	"time"
)

// CreateHierarchicalFederationCoordinator creates a new federation coordinator with hierarchical capabilities
func CreateHierarchicalFederationCoordinator(
	federationID string,
	regionID string,
	policy *FederationPolicy,
	snapshotCoordinator *RegionalSnapshotCoordinator,
	stateManager StateManager,
) (*HierarchicalFederationCoordinator, error) {
	// Create base federation coordinator
	baseFC := NewFederationCoordinator(federationID, regionID, policy, snapshotCoordinator, stateManager)
	
	// Create hierarchical federation coordinator with the correct field names
	hfc := &HierarchicalFederationCoordinator{
		FederationCoordinator: baseFC,
		RegionID:              regionID,
		ConnectedRegions:      make(map[string]*FederationRegion),
		LastSyncTime:          make(map[string]time.Time),
		SyncInterval:          60 * time.Second, // Default to 60 seconds
		Config:                policy,
		// Mutexes are initialized automatically by Go
	}
	
	// Initialize the LastSyncTime for the current region
	hfc.LastSyncTime[regionID] = time.Now()
	
	return hfc, nil
}

// InitializeAccumulatorAdapter initializes the accumulator adapter for a hierarchical federation coordinator
func (hfc *HierarchicalFederationCoordinator) InitializeAccumulatorAdapter(crossVerifier *CrossVerifier) {
	hfc.CrossVerifier = crossVerifier
	
	// Create a new accumulator adapter for hierarchical federation
	hfc.AccumulatorAdapter = NewHierarchicalAccumulatorAdapter(
		hfc.FederationCoordinator,
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
}

// AddRegion adds a new region to the federation
func (hfc *HierarchicalFederationCoordinator) AddRegion(regionID string, verifier *RegionVerifier) {
	hfc.RegionMutex.Lock()
	defer hfc.RegionMutex.Unlock()
	
	// Add region to connected regions map
	hfc.ConnectedRegions[regionID] = &FederationRegion{
		RegionID: regionID,
		Status:   "Connected",
	}
}

// RemoveRegion removes a region from the federation
func (hfc *HierarchicalFederationCoordinator) RemoveRegion(regionID string) {
	hfc.RegionMutex.Lock()
	defer hfc.RegionMutex.Unlock()
	
	// Remove the region from the connected regions map
	delete(hfc.ConnectedRegions, regionID)
}

// GetRegion retrieves a federation region by ID
func (hfc *HierarchicalFederationCoordinator) GetRegion(regionID string) (*FederationRegion, error) {
	hfc.RegionMutex.Lock()
	defer hfc.RegionMutex.Unlock()
	
	// Check if region exists
	region, exists := hfc.ConnectedRegions[regionID]
	if !exists {
		return nil, fmt.Errorf("region %s not found in federation", regionID)
	}
	
	// Return the region
	return region, nil
}

// ShouldSynchronize checks if the federation should synchronize
func (hfc *HierarchicalFederationCoordinator) ShouldSynchronize() bool {
	hfc.SyncMutex.RLock()
	defer hfc.SyncMutex.RUnlock()
	
	// Check if enough time has passed since the last sync
	lastSync, exists := hfc.LastSyncTime[hfc.RegionID]
	if !exists {
		return true
	}
	
	return time.Since(lastSync) >= hfc.SyncInterval
}

// SynchronizeHierarchy synchronizes the hierarchical accumulator state across regions
func (hfc *HierarchicalFederationCoordinator) SynchronizeHierarchy(ctx context.Context) error {
	// Check if we should synchronize
	if !hfc.ShouldSynchronize() {
		return nil
	}
	
	// Update the last sync time
	hfc.SyncMutex.Lock()
	hfc.LastSyncTime[hfc.RegionID] = time.Now()
	hfc.SyncMutex.Unlock()
	
	// Synchronize the accumulators
	if hfc.AccumulatorAdapter == nil {
		return fmt.Errorf("accumulator adapter not initialized")
	}
	return hfc.AccumulatorAdapter.SynchronizeAccumulators(ctx)
}

// VerifyRegionalElement verifies an element against a region's accumulator
func (hfc *HierarchicalFederationCoordinator) VerifyRegionalElement(
	ctx context.Context,
	regionID string,
	element []byte,
) (bool, error) {
	if hfc.AccumulatorAdapter == nil {
		return false, fmt.Errorf("accumulator adapter not initialized")
	}
	
	// Use the accumulator adapter to verify the element
	return hfc.AccumulatorAdapter.VerifyRegionalElement(regionID, element)
}

// VerifyCrossRegionOperation verifies a cross-region operation
func (hfc *HierarchicalFederationCoordinator) VerifyCrossRegionOperation(
	ctx context.Context,
	operation *CrossRegionOperation,
) (bool, error) {
	if hfc.AccumulatorAdapter == nil {
		return false, fmt.Errorf("accumulator adapter not initialized")
	}
	
	// Use the accumulator adapter to verify the operation
	return hfc.AccumulatorAdapter.VerifyCrossRegionOperation(ctx, operation)
}

// GetHierarchicalMetrics returns metrics about the hierarchical federation
func (hfc *HierarchicalFederationCoordinator) GetHierarchicalMetrics() map[string]interface{} {
	metrics := make(map[string]interface{})
	
	// Add metrics from the accumulator adapter if available
	if hfc.AccumulatorAdapter != nil {
		for k, v := range hfc.AccumulatorAdapter.GetVerificationMetrics() {
			metrics[k] = v
		}
	}
	
	// Add federation coordinator metrics
	metrics["RegionCount"] = len(hfc.ConnectedRegions)
	
	// Format the last sync time for the current region
	if lastSync, ok := hfc.LastSyncTime[hfc.RegionID]; ok {
		metrics["LastSyncTime"] = lastSync.Format(time.RFC3339)
	}
	
	return metrics
}

// GetFederationCoordinator returns the underlying federation coordinator
func (hfc *HierarchicalFederationCoordinator) GetFederationCoordinator() *FederationCoordinator {
	return hfc.FederationCoordinator
}

// GetLatestRegionalSnapshot returns the latest regional snapshot
func (hfc *HierarchicalFederationCoordinator) GetLatestRegionalSnapshot() (*RegionalSnapshot, error) {
	// Get the snapshot directly from the snapshot coordinator
	if hfc.FederationCoordinator != nil && hfc.FederationCoordinator.snapshotCoordinator != nil {
		return hfc.FederationCoordinator.snapshotCoordinator.GetLatestRegionalSnapshot()
	}
	return nil, fmt.Errorf("snapshot coordinator not initialized")
}

// GetRegisteredTEEs returns the registered TEEs
func (hfc *HierarchicalFederationCoordinator) GetRegisteredTEEs() map[string]string {
	// Return an empty map if not implemented in the underlying coordinator
	// This is a placeholder - implement proper TEE registration tracking if needed
	return make(map[string]string)
}

// SynchronizeHierarchical synchronizes the hierarchical structure for integration compatibility
func (hfc *HierarchicalFederationCoordinator) SynchronizeHierarchical(ctx context.Context) error {
	return hfc.SynchronizeHierarchy(ctx)
}

// UpdateLastSyncTime updates the last synchronization timestamp for integration compatibility
func (hfc *HierarchicalFederationCoordinator) UpdateLastSyncTime() {
	hfc.SyncMutex.Lock()
	hfc.LastSyncTime[hfc.RegionID] = time.Now()
	hfc.SyncMutex.Unlock()
}
