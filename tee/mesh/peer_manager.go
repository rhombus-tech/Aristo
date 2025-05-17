package mesh

import (
	"context"
	"math"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"
)

// PeerScore represents a peer's performance metrics used for selection decisions
type PeerScore struct {
	PeerID            string
	ResponseTime      time.Duration // Average response time
	SuccessRate       float64       // Success rate of sync operations (0.0-1.0)
	LastSyncTime      time.Time     // Last successful sync time
	NetworkProximity  float64       // Network proximity score (lower is better)
	DomainOverlap     float64       // Overlap in domains of interest (higher is better)
	ResourceAvailable float64       // Resource availability score (higher is better)
	HistoricalScore   float64       // Historical performance score
}

// PeerManagerV2 handles intelligent selection and management of sync peers
type PeerManagerV2 struct {
	// Core components
	logger         *zap.Logger
	metrics        *PeerManagerMetrics
	domainManager  *DomainManager
	peerMutex      sync.RWMutex
	configMutex    sync.RWMutex
	
	// Peer tracking
	peerScores     map[string]*PeerScore
	activePeers    map[string]bool
	regionPeers    map[string][]string  // Region -> peerIDs
	domainPeers    map[string][]string  // Domain -> peerIDs
	peerConnections map[string]int      // PeerID -> connection count
	
	// Configuration
	maxActivePeers     int
	minActivePeers     int
	scoreDecayFactor   float64
	syncInterval       time.Duration
	balancingInterval  time.Duration
	
	// Algorithms
	peerSelectionStrategy PeerSelectionStrategy
	loadBalancer          LoadBalancer
	topologyManager       TopologyManager
	
	// Management
	refreshTicker   *time.Ticker
	balancingTicker *time.Ticker
	stopChan        chan struct{}
}

// PeerManagerMetrics tracks performance of peer selection and connections
type PeerManagerMetrics struct {
	TotalPeerConnections   int64
	SuccessfulSyncs        int64
	FailedSyncs            int64
	AverageResponseTimeMs  float64
	PeerSelectionLatencyMs float64
	NetworkEfficiency      float64
	DomainSyncCoverage     map[string]float64
	ConnectionChangesTotal int64
}

// PeerManagerConfig contains configuration options for the peer manager
type PeerManagerConfig struct {
	MaxActivePeers        int
	MinActivePeers        int
	ScoreDecayFactor      float64
	SyncInterval          time.Duration
	BalancingInterval     time.Duration
	PeerSelectionStrategy string
	LoadBalancingStrategy string
	TopologyStrategy      string
}

// NewPeerManagerV2 creates a new peer manager with the given configuration
func NewPeerManagerV2(
	logger *zap.Logger,
	domainManager *DomainManager,
	config *PeerManagerConfig,
) *PeerManagerV2 {
	if config == nil {
		config = &PeerManagerConfig{
			MaxActivePeers:        15,
			MinActivePeers:        5,
			ScoreDecayFactor:      0.95,
			SyncInterval:          time.Minute * 5,
			BalancingInterval:     time.Minute * 30,
			PeerSelectionStrategy: "weighted",
			LoadBalancingStrategy: "domain-aware",
			TopologyStrategy:      "proximity",
		}
	}
	
	pm := &PeerManagerV2{
		logger:           logger,
		metrics:          &PeerManagerMetrics{DomainSyncCoverage: make(map[string]float64)},
		domainManager:    domainManager,
		peerScores:       make(map[string]*PeerScore),
		activePeers:      make(map[string]bool),
		regionPeers:      make(map[string][]string),
		domainPeers:      make(map[string][]string),
		peerConnections:  make(map[string]int),
		maxActivePeers:   config.MaxActivePeers,
		minActivePeers:   config.MinActivePeers,
		scoreDecayFactor: config.ScoreDecayFactor,
		syncInterval:     config.SyncInterval,
		balancingInterval: config.BalancingInterval,
		stopChan:         make(chan struct{}),
	}
	
	// Initialize the selection strategy
	switch config.PeerSelectionStrategy {
	case "random":
		pm.peerSelectionStrategy = &RandomSelectionStrategy{}
	case "round-robin":
		pm.peerSelectionStrategy = &RoundRobinSelectionStrategy{}
	case "domain-priority":
		pm.peerSelectionStrategy = &DomainPrioritySelectionStrategy{}
	case "weighted":
		fallthrough
	default:
		pm.peerSelectionStrategy = &WeightedSelectionStrategy{}
	}
	
	// Initialize the load balancer
	switch config.LoadBalancingStrategy {
	case "round-robin":
		pm.loadBalancer = &RoundRobinLoadBalancer{}
	case "domain-aware":
		fallthrough
	default:
		pm.loadBalancer = &DomainAwareLoadBalancer{}
	}
	
	// Initialize the topology manager
	switch config.TopologyStrategy {
	case "region-aware":
		pm.topologyManager = &RegionAwareTopologyManager{}
	case "proximity":
		fallthrough
	default:
		pm.topologyManager = &ProximityBasedTopologyManager{}
	}
	
	return pm
}

// Start begins the peer management background processes
func (pm *PeerManagerV2) Start(ctx context.Context) {
	pm.refreshTicker = time.NewTicker(pm.syncInterval)
	pm.balancingTicker = time.NewTicker(pm.balancingInterval)
	
	go func() {
		for {
			select {
			case <-pm.stopChan:
				pm.refreshTicker.Stop()
				pm.balancingTicker.Stop()
				return
			case <-pm.refreshTicker.C:
				pm.refreshPeerScores()
			case <-pm.balancingTicker.C:
				pm.rebalanceConnections()
			}
		}
	}()
}

// Stop halts all peer management background processes
func (pm *PeerManagerV2) Stop() {
	close(pm.stopChan)
}

// RegisterPeer adds a new peer to the manager with default scores
func (pm *PeerManagerV2) RegisterPeer(peerID, region string, domains []string) {
	// Null check for the peer manager
	if pm == nil {
		return
	}
	
	// Initialize maps if needed
	if pm.peerScores == nil {
		pm.peerScores = make(map[string]*PeerScore)
	}
	if pm.regionPeers == nil {
		pm.regionPeers = make(map[string][]string)
	}
	if pm.domainPeers == nil {
		pm.domainPeers = make(map[string][]string)
	}
	
	pm.peerMutex.Lock()
	defer pm.peerMutex.Unlock()
	
	// Initialize peer if not exists
	if _, exists := pm.peerScores[peerID]; !exists {
		pm.peerScores[peerID] = &PeerScore{
			PeerID:            peerID,
			ResponseTime:      time.Millisecond * 500, // Default starting value
			SuccessRate:       0.95,                  // Optimistic initial value
			LastSyncTime:      time.Now().Add(-time.Hour), // Start as needing sync
			NetworkProximity:  0.5,                   // Middle proximity score
			DomainOverlap:     0.0,                   // Will be calculated
			ResourceAvailable: 0.8,                   // Assume mostly available
			HistoricalScore:   0.5,                   // Neutral starting value
		}
	}
	
	// Add to region map
	if region != "" {
		pm.regionPeers[region] = append(pm.regionPeers[region], peerID)
	}
	
	// Add to domain maps
	for _, domain := range domains {
		pm.domainPeers[domain] = append(pm.domainPeers[domain], peerID)
	}
	
	// Calculate domain overlap
	pm.updateDomainOverlap(peerID, domains)
}

// UpdatePeerScore updates a peer's performance metrics
func (pm *PeerManagerV2) UpdatePeerScore(
	peerID string,
	responseTime time.Duration,
	success bool,
	resourceLevel float64,
) {
	pm.peerMutex.Lock()
	defer pm.peerMutex.Unlock()
	
	score, exists := pm.peerScores[peerID]
	if !exists {
		pm.logger.Warn("Attempted to update score for unknown peer", zap.String("peerID", peerID))
		return
	}
	
	// Update response time using exponential moving average
	alpha := 0.3 // Weight for new value
	score.ResponseTime = time.Duration(float64(score.ResponseTime)*(1-alpha) + float64(responseTime)*alpha)
	
	// Update success rate
	successValue := 0.0
	if success {
		successValue = 1.0
		score.LastSyncTime = time.Now()
		pm.metrics.SuccessfulSyncs++
	} else {
		pm.metrics.FailedSyncs++
	}
	score.SuccessRate = score.SuccessRate*0.9 + successValue*0.1
	
	// Update resource availability
	score.ResourceAvailable = resourceLevel
	
	// Update historical score (weighted combination of factors)
	score.HistoricalScore = pm.calculateHistoricalScore(score)
}

// calculateHistoricalScore combines factors into a single score
func (pm *PeerManagerV2) calculateHistoricalScore(score *PeerScore) float64 {
	// Weight factors according to importance
	responseTimeNorm := math.Min(1.0, float64(time.Second)/float64(score.ResponseTime))
	
	// Combine weighted factors
	weights := map[string]float64{
		"successRate":       0.4,
		"responseTime":      0.3,
		"resourceAvailable": 0.2,
		"domainOverlap":     0.1,
	}
	
	combined := weights["successRate"]*score.SuccessRate +
	            weights["responseTime"]*responseTimeNorm +
	            weights["resourceAvailable"]*score.ResourceAvailable +
	            weights["domainOverlap"]*score.DomainOverlap
	            
	return combined
}

// refreshPeerScores applies decay to all peer scores to favor recent data
func (pm *PeerManagerV2) refreshPeerScores() {
	pm.peerMutex.Lock()
	defer pm.peerMutex.Unlock()
	
	for _, score := range pm.peerScores {
		// Apply decay to historical score
		score.HistoricalScore = score.HistoricalScore * pm.scoreDecayFactor
		
		// Detect and handle stale peers
		if time.Since(score.LastSyncTime) > pm.syncInterval*3 {
			// If we haven't synced in a while, slightly penalize the score
			score.HistoricalScore = math.Max(0.1, score.HistoricalScore*0.9)
		}
	}
}

// updateDomainOverlap recalculates domain overlap scores
func (pm *PeerManagerV2) updateDomainOverlap(peerID string, peerDomains []string) {
	score, exists := pm.peerScores[peerID]
	if !exists {
		return
	}
	
	// Get our domains
	ourDomains := pm.domainManager.GetAllDomains()
	
	// Create sets for efficient lookup
	ourDomainSet := make(map[string]bool)
	peerDomainSet := make(map[string]bool)
	
	for _, domain := range ourDomains {
		ourDomainSet[domain] = true
	}
	
	for _, domain := range peerDomains {
		peerDomainSet[domain] = true
	}
	
	// Count overlaps
	overlapCount := 0
	for domain := range peerDomainSet {
		if ourDomainSet[domain] {
			overlapCount++
		}
	}
	
	// Calculate Jaccard similarity: |A ∩ B| / |A ∪ B|
	unionCount := len(ourDomainSet) + len(peerDomainSet) - overlapCount
	if unionCount > 0 {
		score.DomainOverlap = float64(overlapCount) / float64(unionCount)
	} else {
		score.DomainOverlap = 0.0
	}
}

// SelectPeersForDomain finds the best peers for syncing a specific domain
func (pm *PeerManagerV2) SelectPeersForDomain(domain string, count int) []string {
	// Null check for the peer manager
	if pm == nil {
		return []string{}
	}
	
	pm.peerMutex.RLock()
	defer pm.peerMutex.RUnlock()
	
	startTime := time.Now()
	
	// Get domain-specific peers with null check
	var domainPeers []string
	if pm.domainPeers != nil {
		domainPeers = pm.domainPeers[domain]
	}
	
	// Create selection context with necessary information
	ctx := &PeerSelectionContext{
		Domain:        domain,
		RequestedCount: count,
		AvailablePeers: domainPeers,
		PeerScores:     pm.peerScores,
		ActivePeers:    pm.activePeers,
	}
	
	// Use the strategy to select peers with null check
	var selectedPeers []string
	if pm.peerSelectionStrategy != nil {
		selectedPeers = pm.peerSelectionStrategy.SelectPeers(ctx)
	} else {
		// Fallback behavior if no strategy is set
		// Return a subset of available peers up to count
		if len(domainPeers) <= count {
			selectedPeers = domainPeers
		} else {
			selectedPeers = domainPeers[:count]
		}
	}
	
	// Update metrics
	pm.metrics.PeerSelectionLatencyMs = float64(time.Since(startTime).Microseconds()) / 1000.0
	
	return selectedPeers
}

// rebalanceConnections optimizes the active peer connections
func (pm *PeerManagerV2) rebalanceConnections() {
	pm.peerMutex.Lock()
	defer pm.peerMutex.Unlock()
	
	// Create a slice of peers sorted by score
	var peers []*PeerScore
	for _, score := range pm.peerScores {
		peers = append(peers, score)
	}
	
	// Sort by historical score (descending)
	sort.Slice(peers, func(i, j int) bool {
		return peers[i].HistoricalScore > peers[j].HistoricalScore
	})
	
	// Let the load balancer determine optimal connections
	balancingContext := &LoadBalancingContext{
		AllPeers:           peers,
		CurrentConnections: pm.peerConnections,
		MaxConnections:     pm.maxActivePeers,
		MinConnections:     pm.minActivePeers,
		DomainManager:      pm.domainManager,
	}
	
	newConnections := pm.loadBalancer.BalanceConnections(balancingContext)
	
	// Track connection changes
	changes := 0
	for peerID, count := range newConnections {
		if pm.peerConnections[peerID] != count {
			changes++
		}
	}
	
	// Update connections
	pm.peerConnections = newConnections
	
	// Update active peers set
	pm.activePeers = make(map[string]bool)
	for peerID, count := range pm.peerConnections {
		if count > 0 {
			pm.activePeers[peerID] = true
		}
	}
	
	// Update metrics
	pm.metrics.ConnectionChangesTotal += int64(changes)
	pm.metrics.TotalPeerConnections = 0
	for _, count := range pm.peerConnections {
		pm.metrics.TotalPeerConnections += int64(count)
	}
	
	// Ask topology manager to reorganize if needed
	if changes > pm.maxActivePeers/3 {
		topologyContext := &TopologyContext{
			AllPeers:        peers,
			ActivePeers:     pm.activePeers,
			RegionPeers:     pm.regionPeers,
			DomainPeers:     pm.domainPeers,
		}
		
		pm.topologyManager.OptimizeTopology(topologyContext)
	}
}

// GetPeerMetrics returns the current peer manager metrics
func (pm *PeerManagerV2) GetPeerMetrics() *PeerManagerMetrics {
	pm.peerMutex.RLock()
	defer pm.peerMutex.RUnlock()
	
	// Deep copy to avoid concurrent map access
	metricsCopy := &PeerManagerMetrics{
		TotalPeerConnections:   pm.metrics.TotalPeerConnections,
		SuccessfulSyncs:        pm.metrics.SuccessfulSyncs,
		FailedSyncs:            pm.metrics.FailedSyncs,
		AverageResponseTimeMs:  pm.metrics.AverageResponseTimeMs,
		PeerSelectionLatencyMs: pm.metrics.PeerSelectionLatencyMs,
		NetworkEfficiency:      pm.metrics.NetworkEfficiency,
		DomainSyncCoverage:     make(map[string]float64),
		ConnectionChangesTotal: pm.metrics.ConnectionChangesTotal,
	}
	
	for domain, coverage := range pm.metrics.DomainSyncCoverage {
		metricsCopy.DomainSyncCoverage[domain] = coverage
	}
	
	return metricsCopy
}

// GetActivePeersCount returns the current number of active peers
func (pm *PeerManagerV2) GetActivePeersCount() int {
	pm.peerMutex.RLock()
	defer pm.peerMutex.RUnlock()
	
	return len(pm.activePeers)
}

// GetPeerConnection returns the connection count for a peer
func (pm *PeerManagerV2) GetPeerConnection(peerID string) int {
	pm.peerMutex.RLock()
	defer pm.peerMutex.RUnlock()
	
	return pm.peerConnections[peerID]
}

// SelectPeersForSync selects a set of peers optimized for synchronizing the given domains
func (pm *PeerManagerV2) SelectPeersForSync(domains []string, count int) []string {
	pm.peerMutex.RLock()
	defer pm.peerMutex.RUnlock()
	
	if len(domains) == 1 {
		// For a single domain, simply use the domain-specific selection
		return pm.SelectPeersForDomain(domains[0], count)
	}
	
	// For multiple domains, we need to find peers that can serve all or most domains
	// This is a simplified implementation; a real one would be more sophisticated
	scores := make(map[string]float64)
	for _, domain := range domains {
		peers := pm.domainPeers[domain]
		for _, peerID := range peers {
			if score, ok := pm.peerScores[peerID]; ok {
				scores[peerID] += score.DomainOverlap * score.HistoricalScore
			}
		}
	}
	
	// Sort peers by score
	type scoredPeer struct {
		id    string
		score float64
	}
	
	peerList := make([]scoredPeer, 0, len(scores))
	for id, score := range scores {
		peerList = append(peerList, scoredPeer{id: id, score: score})
	}
	
	sort.Slice(peerList, func(i, j int) bool {
		return peerList[i].score > peerList[j].score
	})
	
	// Return the top peers
	result := make([]string, 0, count)
	for i := 0; i < len(peerList) && i < count; i++ {
		result = append(result, peerList[i].id)
	}
	
	return result
}

// IsPeerActive checks if a peer is currently active
func (pm *PeerManagerV2) IsPeerActive(peerID string) bool {
	pm.peerMutex.RLock()
	defer pm.peerMutex.RUnlock()
	
	active, exists := pm.activePeers[peerID]
	return exists && active
}

// GetPeerScore returns the performance score for a peer
func (pm *PeerManagerV2) GetPeerScore(peerID string) (*PeerScore, bool) {
	pm.peerMutex.RLock()
	defer pm.peerMutex.RUnlock()
	
	score, exists := pm.peerScores[peerID]
	return score, exists
}

// UpdatePeerMetrics updates a peer's performance metrics
func (pm *PeerManagerV2) UpdatePeerMetrics(peerID string, latency time.Duration, success bool, bytesTransferred int64) {
	pm.peerMutex.Lock()
	defer pm.peerMutex.Unlock()
	
	score, exists := pm.peerScores[peerID]
	if !exists {
		return
	}
	
	// Update response time with an exponential moving average
	alpha := 0.3 // Weighting factor for the moving average
	if score.ResponseTime == 0 {
		score.ResponseTime = latency
	} else {
		score.ResponseTime = time.Duration(float64(score.ResponseTime)*(1-alpha) + float64(latency)*alpha)
	}
	
	// Update success rate
	if success {
		score.SuccessRate = score.SuccessRate*0.95 + 0.05 // Weight success
		score.LastSyncTime = time.Now()
	} else {
		score.SuccessRate = score.SuccessRate * 0.9 // Penalize failure more heavily
	}
	
	// Update resource availability based on response time and bytes transferred
	// This is a simplified calculation
	resourceScore := 1.0
	if latency > time.Second {
		resourceScore = math.Max(0.1, 1.0-(float64(latency)/float64(time.Second*10)))
	}
	score.ResourceAvailable = score.ResourceAvailable*0.8 + resourceScore*0.2
	
	// Update historical score
	score.HistoricalScore = pm.calculateHistoricalScore(score)
}

// GetPeersForDomain returns a list of peer IDs that can serve a specific domain
// This is a wrapper around SelectPeersForDomain for compatibility with the gossip manager
func (pm *PeerManagerV2) GetPeersForDomain(domain string, count int) []string {
	return pm.SelectPeersForDomain(domain, count)
}
