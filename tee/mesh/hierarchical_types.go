// Package mesh provides a mesh network for TEE-to-TEE communication
// with hierarchical verification capabilities.
package mesh

import (
	"sync"
	"time"
)

// HierarchicalConfig defines configuration for the hierarchical accumulator
type HierarchicalConfig struct {
	// Names of the accumulator levels (e.g., "Regional", "CrossRegion", "Global")
	LevelNames []string
	
	// Map of level name to update frequency in milliseconds
	UpdateFrequency map[string]int64
}

// HierarchicalFederationCoordinator manages federation of regions with hierarchical accumulator capabilities
type HierarchicalFederationCoordinator struct {
	// Base federation coordinator
	FederationCoordinator  *FederationCoordinator
	
	// Hierarchical accumulator adapter - exported for cross-module access
	AccumulatorAdapter     *HierarchicalAccumulatorAdapter
	
	// Cross-region verifier for attestation verification
	CrossVerifier          *CrossVerifier
	
	// Region identification
	RegionID               string
	
	// Connected regions for cross-region verification
	ConnectedRegions       map[string]*FederationRegion
	
	// Synchronization
	RegionMutex            sync.RWMutex
	SyncMutex              sync.RWMutex
	
	// Last synchronization timestamp
	LastSyncTime           map[string]time.Time
	
	// Sync interval in seconds
	SyncInterval           time.Duration
	
	// Configuration
	Config                 *FederationPolicy
}

// CrossRegionOperation is defined in coordinator_federation.go

// FederationRegion represents a connected region in the federation
type FederationRegion struct {
	RegionID     string
	Status       string
	LastSyncTime time.Time
	Endpoint     string
	Priority     int
}

// Note: RegionVerifier is already defined in cross_verify.go
