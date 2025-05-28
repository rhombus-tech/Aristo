// Package mesh provides a mesh network for TEE-to-TEE communication
// with enhanced federation capabilities using hierarchical accumulators.
package mesh

import (
	"context"
	"fmt"
	"time"
)

// InitializeHierarchicalFederation initializes the hierarchical federation system
func InitializeHierarchicalFederation(
	ctx context.Context,
	federationID string,
	regionID string,
	policy *FederationPolicy,
	snapshotCoordinator *RegionalSnapshotCoordinator,
	stateManager StateManager,
) (*HierarchicalFederationCoordinator, error) {
	// Create a base federation coordinator
	baseFC := NewFederationCoordinator(
		federationID,
		regionID,
		policy,
		snapshotCoordinator,
		stateManager,
	)

	// Create hierarchical federation coordinator with consistent field names
	hfc := &HierarchicalFederationCoordinator{
		FederationCoordinator: baseFC,
		RegionID:              regionID,
		ConnectedRegions:      make(map[string]*FederationRegion),
		LastSyncTime:          make(map[string]time.Time),
		SyncInterval:          60 * time.Second, // Default to 60 seconds
		Config:                policy,
	}
	
	// Initialize the LastSyncTime for the current region
	hfc.LastSyncTime[regionID] = time.Now()
	
	return hfc, nil
}

// ConnectToRegion connects to another region in the federation
func (hfc *HierarchicalFederationCoordinator) ConnectToRegion(
	ctx context.Context,
	targetRegionID string,
	endpoint string,
) error {
	// Ensure we're not connecting to ourselves
	if targetRegionID == hfc.RegionID {
		return fmt.Errorf("cannot connect to self")
	}
	
	// Check if already connected
	hfc.RegionMutex.RLock()
	_, exists := hfc.ConnectedRegions[targetRegionID]
	hfc.RegionMutex.RUnlock()
	
	if exists {
		return fmt.Errorf("already connected to region %s", targetRegionID)
	}
	
	// Register the region
	hfc.RegionMutex.Lock()
	hfc.ConnectedRegions[targetRegionID] = &FederationRegion{
		RegionID:     targetRegionID,
		Status:       "connected",
		LastSyncTime: time.Now(),
		Endpoint:     endpoint,
		Priority:     1, // Default priority
	}
	hfc.RegionMutex.Unlock()
	
	// Initialize synchronization for the region
	hfc.SyncMutex.Lock()
	hfc.LastSyncTime[targetRegionID] = time.Now()
	hfc.SyncMutex.Unlock()
	
	return nil
}

// SynchronizeWithRegion performs synchronization with a specific region
func (hfc *HierarchicalFederationCoordinator) SynchronizeWithRegion(
	ctx context.Context,
	targetRegionID string,
) error {
	// Check if region is connected
	hfc.RegionMutex.RLock()
	region, exists := hfc.ConnectedRegions[targetRegionID]
	hfc.RegionMutex.RUnlock()
	
	if !exists {
		return fmt.Errorf("region %s is not connected", targetRegionID)
	}
	
	// Update region status
	hfc.RegionMutex.Lock()
	region.Status = "synchronizing"
	hfc.RegionMutex.Unlock()
	
	// Perform synchronization
	// In a real implementation, this would involve communication with the remote region
	
	// Update region status and synchronization time
	hfc.RegionMutex.Lock()
	region.Status = "synchronized"
	region.LastSyncTime = time.Now()
	hfc.RegionMutex.Unlock()
	
	// Update last sync time
	hfc.SyncMutex.Lock()
	hfc.LastSyncTime[targetRegionID] = time.Now()
	hfc.SyncMutex.Unlock()
	
	return nil
}

// GetConnectedRegions returns a list of connected region IDs
func (hfc *HierarchicalFederationCoordinator) GetConnectedRegions() []string {
	hfc.RegionMutex.RLock()
	defer hfc.RegionMutex.RUnlock()
	
	regions := make([]string, 0, len(hfc.ConnectedRegions))
	for regionID := range hfc.ConnectedRegions {
		regions = append(regions, regionID)
	}
	
	return regions
}

// GetRegionStatus returns the status of a specific region
func (hfc *HierarchicalFederationCoordinator) GetRegionStatus(regionID string) (string, error) {
	hfc.RegionMutex.RLock()
	defer hfc.RegionMutex.RUnlock()
	
	region, exists := hfc.ConnectedRegions[regionID]
	if !exists {
		return "", fmt.Errorf("region %s is not connected", regionID)
	}
	
	return region.Status, nil
}

// GetLastSyncTime returns the last synchronization time for a region
func (hfc *HierarchicalFederationCoordinator) GetLastSyncTime(regionID string) (time.Time, error) {
	hfc.SyncMutex.RLock()
	defer hfc.SyncMutex.RUnlock()
	
	syncTime, exists := hfc.LastSyncTime[regionID]
	if !exists {
		return time.Time{}, fmt.Errorf("no sync history for region %s", regionID)
	}
	
	return syncTime, nil
}

// SetSyncInterval sets the synchronization interval
func (hfc *HierarchicalFederationCoordinator) SetSyncInterval(interval time.Duration) {
	hfc.SyncInterval = interval
}

// GetSyncInterval returns the current synchronization interval
func (hfc *HierarchicalFederationCoordinator) GetSyncInterval() time.Duration {
	return hfc.SyncInterval
}
