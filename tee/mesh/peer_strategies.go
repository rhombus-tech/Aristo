package mesh

import (
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"
)

// PeerSelectionContext contains information needed for peer selection
type PeerSelectionContext struct {
	Domain         string
	RequestedCount int
	AvailablePeers []string
	PeerScores     map[string]*PeerScore
	ActivePeers    map[string]bool
}

// LoadBalancingContext contains information needed for load balancing
type LoadBalancingContext struct {
	AllPeers           []*PeerScore
	CurrentConnections map[string]int
	MaxConnections     int
	MinConnections     int
	DomainManager      *DomainManager
}

// TopologyContext contains information needed for topology management
type TopologyContext struct {
	AllPeers    []*PeerScore
	ActivePeers map[string]bool
	RegionPeers map[string][]string
	DomainPeers map[string][]string
}

// PeerSelectionStrategy defines the interface for peer selection algorithms
type PeerSelectionStrategy interface {
	SelectPeers(ctx *PeerSelectionContext) []string
}

// LoadBalancer defines the interface for connection load balancing
type LoadBalancer interface {
	BalanceConnections(ctx *LoadBalancingContext) map[string]int
}

// TopologyManager defines the interface for network topology optimization
type TopologyManager interface {
	OptimizeTopology(ctx *TopologyContext) map[string][]string
}

// ----------------------------------------
// Peer Selection Strategies
// ----------------------------------------

// RandomSelectionStrategy selects peers randomly
type RandomSelectionStrategy struct {
	rand *rand.Rand
	mu   sync.Mutex
}

func (s *RandomSelectionStrategy) SelectPeers(ctx *PeerSelectionContext) []string {
	if len(ctx.AvailablePeers) == 0 {
		return []string{}
	}
	
	s.mu.Lock()
	if s.rand == nil {
		s.rand = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	
	// Create a copy of available peers that we can shuffle
	peers := make([]string, len(ctx.AvailablePeers))
	copy(peers, ctx.AvailablePeers)
	s.rand.Shuffle(len(peers), func(i, j int) { peers[i], peers[j] = peers[j], peers[i] })
	s.mu.Unlock()
	
	// Select the requested number (or all available if less)
	count := ctx.RequestedCount
	if count > len(peers) {
		count = len(peers)
	}
	
	return peers[:count]
}

// RoundRobinSelectionStrategy selects peers in a round-robin fashion
type RoundRobinSelectionStrategy struct {
	lastIndex map[string]int
	mu        sync.Mutex
}

func (s *RoundRobinSelectionStrategy) SelectPeers(ctx *PeerSelectionContext) []string {
	if len(ctx.AvailablePeers) == 0 {
		return []string{}
	}
	
	s.mu.Lock()
	defer s.mu.Unlock()
	
	// Initialize if needed
	if s.lastIndex == nil {
		s.lastIndex = make(map[string]int)
	}
	
	// Get the starting index for this domain
	startIndex := s.lastIndex[ctx.Domain]
	if startIndex >= len(ctx.AvailablePeers) {
		startIndex = 0
	}
	
	// Select peers in round-robin order
	result := make([]string, 0, ctx.RequestedCount)
	index := startIndex
	
	for len(result) < ctx.RequestedCount && len(result) < len(ctx.AvailablePeers) {
		peer := ctx.AvailablePeers[index]
		result = append(result, peer)
		
		index = (index + 1) % len(ctx.AvailablePeers)
		if index == startIndex {
			break // We've gone full circle
		}
	}
	
	// Update the last index for next time
	s.lastIndex[ctx.Domain] = index
	
	return result
}

// DomainPrioritySelectionStrategy selects peers based on domain specialization
type DomainPrioritySelectionStrategy struct{}

func (s *DomainPrioritySelectionStrategy) SelectPeers(ctx *PeerSelectionContext) []string {
	if len(ctx.AvailablePeers) == 0 {
		return []string{}
	}
	
	// Score peers based on domain relevance
	type scoredPeer struct {
		id    string
		score float64
	}
	
	scoredPeers := make([]scoredPeer, 0, len(ctx.AvailablePeers))
	
	for _, peerID := range ctx.AvailablePeers {
		if score, exists := ctx.PeerScores[peerID]; exists {
			// Domain overlap is the main factor here
			domainScore := score.DomainOverlap * 0.7
			
			// Also consider success rate and response time
			successScore := score.SuccessRate * 0.2
			
			// Normalize response time (lower is better)
			responseTimeScore := 0.1 * math.Max(0, 1.0-float64(score.ResponseTime)/float64(time.Second))
			
			totalScore := domainScore + successScore + responseTimeScore
			scoredPeers = append(scoredPeers, scoredPeer{peerID, totalScore})
		}
	}
	
	// Sort by score (descending)
	sort.Slice(scoredPeers, func(i, j int) bool {
		return scoredPeers[i].score > scoredPeers[j].score
	})
	
	// Select top peers
	count := ctx.RequestedCount
	if count > len(scoredPeers) {
		count = len(scoredPeers)
	}
	
	result := make([]string, count)
	for i := 0; i < count; i++ {
		result[i] = scoredPeers[i].id
	}
	
	return result
}

// WeightedSelectionStrategy selects peers using a weighted probability approach
type WeightedSelectionStrategy struct {
	rand *rand.Rand
	mu   sync.Mutex
}

func (s *WeightedSelectionStrategy) SelectPeers(ctx *PeerSelectionContext) []string {
	if len(ctx.AvailablePeers) == 0 {
		return []string{}
	}
	
	s.mu.Lock()
	if s.rand == nil {
		s.rand = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	s.mu.Unlock()
	
	// Calculate weights for each peer
	type weightedPeer struct {
		id     string
		weight float64
	}
	
	peers := make([]weightedPeer, 0, len(ctx.AvailablePeers))
	totalWeight := 0.0
	
	for _, peerID := range ctx.AvailablePeers {
		if score, exists := ctx.PeerScores[peerID]; exists {
			// Historical score is already a combination of performance metrics
			weight := score.HistoricalScore
			
			// Add a slight preference for already active peers to reduce churn
			if ctx.ActivePeers[peerID] {
				weight *= 1.1
			}
			
			peers = append(peers, weightedPeer{peerID, weight})
			totalWeight += weight
		}
	}
	
	// Normalize weights
	if totalWeight > 0 {
		for i := range peers {
			peers[i].weight /= totalWeight
		}
	} else {
		// If all weights are zero, use uniform weights
		uniformWeight := 1.0 / float64(len(peers))
		for i := range peers {
			peers[i].weight = uniformWeight
		}
	}
	
	// Create cumulative distribution
	cdf := make([]float64, len(peers))
	sum := 0.0
	for i, peer := range peers {
		sum += peer.weight
		cdf[i] = sum
	}
	
	// Select peers using weighted random selection
	selected := make(map[string]bool)
	result := make([]string, 0, ctx.RequestedCount)
	
	// First try to select distinct peers
	attempts := 0
	for len(selected) < ctx.RequestedCount && len(selected) < len(peers) && attempts < 100 {
		r := s.rand.Float64()
		
		// Find the peer whose cumulative probability includes r
		for i, threshold := range cdf {
			if r <= threshold {
				peerID := peers[i].id
				if !selected[peerID] {
					selected[peerID] = true
					result = append(result, peerID)
					break
				}
				break
			}
		}
		
		attempts++
	}
	
	// If we couldn't select enough distinct peers, allow duplicates
	// This shouldn't happen in practice but is here for robustness
	if len(result) < ctx.RequestedCount {
		for len(result) < ctx.RequestedCount {
			r := s.rand.Float64()
			
			// Find the peer whose cumulative probability includes r
			for i, threshold := range cdf {
				if r <= threshold {
					result = append(result, peers[i].id)
					break
				}
			}
		}
	}
	
	return result
}

// ----------------------------------------
// Load Balancing Strategies
// ----------------------------------------

// RoundRobinLoadBalancer distributes connections evenly among peers
type RoundRobinLoadBalancer struct{}

func (lb *RoundRobinLoadBalancer) BalanceConnections(ctx *LoadBalancingContext) map[string]int {
	newConnections := make(map[string]int)
	totalDesiredConnections := ctx.MaxConnections
	
	// Ensure we have at least the minimum number of connections
	if len(ctx.AllPeers) < ctx.MinConnections {
		// Connect to all available peers
		for _, peer := range ctx.AllPeers {
			newConnections[peer.PeerID] = 1
		}
		return newConnections
	}
	
	// Sort peers by historical score (higher first)
	sort.Slice(ctx.AllPeers, func(i, j int) bool {
		return ctx.AllPeers[i].HistoricalScore > ctx.AllPeers[j].HistoricalScore
	})
	
	// Allocate connections round-robin among top peers
	// Start with one connection per peer for the top peers
	for i := 0; i < ctx.MinConnections && i < len(ctx.AllPeers); i++ {
		newConnections[ctx.AllPeers[i].PeerID] = 1
		totalDesiredConnections--
	}
	
	// Distribute remaining connections round-robin
	if totalDesiredConnections > 0 {
		i := 0
		for totalDesiredConnections > 0 && i < ctx.MinConnections && i < len(ctx.AllPeers) {
			peerID := ctx.AllPeers[i].PeerID
			newConnections[peerID]++
			totalDesiredConnections--
			i = (i + 1) % ctx.MinConnections
		}
	}
	
	return newConnections
}

// DomainAwareLoadBalancer distributes connections based on domain coverage
type DomainAwareLoadBalancer struct{}

func (lb *DomainAwareLoadBalancer) BalanceConnections(ctx *LoadBalancingContext) map[string]int {
	newConnections := make(map[string]int)
	
	// Get all domains we care about
	allDomains := ctx.DomainManager.GetAllDomains()
	if len(allDomains) == 0 {
		// If no domains are defined, fall back to a simpler strategy
		for i, peer := range ctx.AllPeers {
			if i < ctx.MaxConnections {
				newConnections[peer.PeerID] = 1
			} else {
				break
			}
		}
		return newConnections
	}
	
	// Create a map of domains to peer scores
	domainPeerScores := make(map[string]map[string]float64)
	for _, domain := range allDomains {
		domainPeerScores[domain] = make(map[string]float64)
	}
	
	// Score each peer for each domain
	for _, peer := range ctx.AllPeers {
		for _, domain := range allDomains {
			// This is a simplified scoring - in a real implementation,
			// we'd have actual domain-specific statistics
			score := peer.HistoricalScore * peer.DomainOverlap
			domainPeerScores[domain][peer.PeerID] = score
		}
	}
	
	// For each domain, ensure we have enough coverage
	connectionsPerDomain := ctx.MaxConnections / len(allDomains)
	if connectionsPerDomain < 1 {
		connectionsPerDomain = 1
	}
	
	// Allocate connections for each domain
	for domainName, peerScores := range domainPeerScores {
		// Sort peers by score for this domain
		type scoredPeer struct {
			id    string
			score float64
		}
		
		domainPeers := make([]scoredPeer, 0, len(peerScores))
		for peerID, score := range peerScores {
			domainPeers = append(domainPeers, scoredPeer{peerID, score})
		}
		
		sort.Slice(domainPeers, func(i, j int) bool {
			return domainPeers[i].score > domainPeers[j].score
		})
		
		// Allocate connections to top peers for this domain
		// Use domainName to avoid unused variable warning
		_ = domainName
		for i := 0; i < connectionsPerDomain && i < len(domainPeers); i++ {
			peerID := domainPeers[i].id
			newConnections[peerID]++
		}
	}
	
	// Limit total connections to max
	totalConnections := 0
	for _, count := range newConnections {
		totalConnections += count
	}
	
	if totalConnections > ctx.MaxConnections {
		// We need to reduce connections
		// Sort peers by total connections descending
		type connectedPeer struct {
			id         string
			connections int
		}
		
		peers := make([]connectedPeer, 0, len(newConnections))
		for peerID, count := range newConnections {
			peers = append(peers, connectedPeer{peerID, count})
		}
		
		sort.Slice(peers, func(i, j int) bool {
			return peers[i].connections > peers[j].connections
		})
		
		// Reduce connections from the most connected peers
		excess := totalConnections - ctx.MaxConnections
		i := 0
		for excess > 0 && i < len(peers) {
			if peers[i].connections > 1 {
				newConnections[peers[i].id]--
				excess--
			}
			i++
			if i >= len(peers) {
				i = 0 // Start again from the beginning if needed
			}
		}
	}
	
	// Ensure we have at least minConnections
	if len(newConnections) < ctx.MinConnections && len(ctx.AllPeers) >= ctx.MinConnections {
		// Sort peers by score
		sort.Slice(ctx.AllPeers, func(i, j int) bool {
			return ctx.AllPeers[i].HistoricalScore > ctx.AllPeers[j].HistoricalScore
		})
		
		// Add connections to high-scoring peers that don't have any yet
		added := 0
		for _, peer := range ctx.AllPeers {
			if newConnections[peer.PeerID] == 0 {
				newConnections[peer.PeerID] = 1
				added++
				
				if len(newConnections) >= ctx.MinConnections {
					break
				}
			}
		}
	}
	
	return newConnections
}

// ----------------------------------------
// Topology Management Strategies
// ----------------------------------------

// ProximityBasedTopologyManager optimizes topology based on network proximity
type ProximityBasedTopologyManager struct{}

func (tm *ProximityBasedTopologyManager) OptimizeTopology(ctx *TopologyContext) map[string][]string {
	// This implementation focuses on optimizing which peers should
	// communicate with each other based on network proximity
	
	// The result is a map of peerID -> recommended peers to connect to
	result := make(map[string][]string)
	
	// Group peers by proximity scores
	proximityGroups := make(map[int][]string)
	
	for _, peer := range ctx.AllPeers {
		// Skip inactive peers
		if !ctx.ActivePeers[peer.PeerID] {
			continue
		}
		
		// Group by proximity (multiply by 10 and round to create groups)
		proximityGroup := int(peer.NetworkProximity * 10)
		if proximityGroups[proximityGroup] == nil {
			proximityGroups[proximityGroup] = make([]string, 0)
		}
		proximityGroups[proximityGroup] = append(proximityGroups[proximityGroup], peer.PeerID)
	}
	
	// For each active peer, recommend connections
	for _, peer := range ctx.AllPeers {
		if !ctx.ActivePeers[peer.PeerID] {
			continue
		}
		
		proximityGroup := int(peer.NetworkProximity * 10)
		
		// First, add peers from the same proximity group
		sameGroupPeers := make([]string, 0)
		for _, otherPeerID := range proximityGroups[proximityGroup] {
			if otherPeerID != peer.PeerID {
				sameGroupPeers = append(sameGroupPeers, otherPeerID)
			}
		}
		
		// Then add some peers from nearby groups for diversity
		nearbyPeers := make([]string, 0)
		for g := 0; g <= 10; g++ {
			if g != proximityGroup && proximityGroups[g] != nil {
				nearbyPeers = append(nearbyPeers, proximityGroups[g]...)
				if len(nearbyPeers) >= 5 {
					break
				}
			}
		}
		
		// Combine and limit
		recommended := append(sameGroupPeers, nearbyPeers...)
		if len(recommended) > 15 {
			recommended = recommended[:15]
		}
		
		result[peer.PeerID] = recommended
	}
	
	return result
}

// RegionAwareTopologyManager optimizes topology based on regions
type RegionAwareTopologyManager struct{}

func (tm *RegionAwareTopologyManager) OptimizeTopology(ctx *TopologyContext) map[string][]string {
	result := make(map[string][]string)
	
	// For each active peer
	for _, peer := range ctx.AllPeers {
		peerID := peer.PeerID
		
		if !ctx.ActivePeers[peerID] {
			continue
		}
		
		recommended := make([]string, 0)
		
		// Find which region this peer belongs to
		peerRegion := ""
		for region, peers := range ctx.RegionPeers {
			for _, regionPeerID := range peers {
				if regionPeerID == peerID {
					peerRegion = region
					break
				}
			}
			if peerRegion != "" {
				break
			}
		}
		
		// First add peers from the same region
		if peerRegion != "" {
			for _, regionPeerID := range ctx.RegionPeers[peerRegion] {
				if regionPeerID != peerID && ctx.ActivePeers[regionPeerID] {
					recommended = append(recommended, regionPeerID)
				}
			}
		}
		
		// Then add some peers from other regions for cross-region connectivity
		otherRegionPeers := make([]string, 0)
		for region, peers := range ctx.RegionPeers {
			if region != peerRegion {
				for _, regionPeerID := range peers {
					if ctx.ActivePeers[regionPeerID] {
						otherRegionPeers = append(otherRegionPeers, regionPeerID)
						if len(otherRegionPeers) >= 5 {
							break
						}
					}
				}
			}
			if len(otherRegionPeers) >= 5 {
				break
			}
		}
		
		// Combine and limit
		recommended = append(recommended, otherRegionPeers...)
		if len(recommended) > 15 {
			recommended = recommended[:15]
		}
		
		result[peerID] = recommended
	}
	
	return result
}
