package mesh

import (
	"context"
	"math"
	"sync"
	"time"

	"go.uber.org/zap"
)

// GossipManager handles the propagation of gossip messages throughout the mesh
type GossipManager struct {
	logger        *zap.Logger
	peerManager   *PeerManagerV2
	seenMessages  map[string]time.Time // Message ID -> time seen
	messageMutex  sync.RWMutex
	metrics       *GossipMetrics
	gossipConfig  *GossipConfig
	gossipTicker  *time.Ticker
	stopChan      chan struct{}
}

// GossipMetrics tracks performance of the gossip protocol
type GossipMetrics struct {
	MessagesOriginated      int64
	MessagesReceived        int64
	MessagesForwarded       int64
	MessagesDuplicate       int64
	MessagesDropped         int64
	PropagationLatencyMs    float64
	NetworkEfficiency       float64
	CoveragePercent         float64
	BandwidthUsedBytes      int64
}

// GossipConfig contains configuration options for the gossip protocol
type GossipConfig struct {
	MaxHopCount         int32
	MessageTTL          time.Duration
	GossipInterval      time.Duration
	PropagationFactor   float64 // What percentage of peers to propagate to
	PriorityDomains     []string
	DomainBoostFactor   float64 // How much to boost domain-specific gossip
	IncludeProofs       bool    // Whether to include Merkle proofs
}

// NewGossipManager creates a new manager for handling gossip communication
func NewGossipManager(
	logger *zap.Logger,
	peerManager *PeerManagerV2,
	config *GossipConfig,
) *GossipManager {
	if config == nil {
		config = &GossipConfig{
			MaxHopCount:       5,
			MessageTTL:        time.Minute * 10,
			GossipInterval:    time.Second * 30,
			PropagationFactor: 0.4, // Propagate to 40% of peers
			PriorityDomains:   []string{},
			DomainBoostFactor: 1.5,
			IncludeProofs:     true,
		}
	}
	
	return &GossipManager{
		logger:       logger,
		peerManager:  peerManager,
		seenMessages: make(map[string]time.Time),
		metrics:      &GossipMetrics{},
		gossipConfig: config,
		stopChan:     make(chan struct{}),
	}
}

// Start begins the gossip manager background processes
func (gm *GossipManager) Start(ctx context.Context) {
	gm.gossipTicker = time.NewTicker(gm.gossipConfig.GossipInterval)
	
	go func() {
		for {
			select {
			case <-gm.stopChan:
				gm.gossipTicker.Stop()
				return
			case <-gm.gossipTicker.C:
				gm.pruneSeenMessages()
			}
		}
	}()
}

// Stop halts all gossip manager background processes
func (gm *GossipManager) Stop() {
	close(gm.stopChan)
}

// pruneSeenMessages removes expired messages from the seen cache
func (gm *GossipManager) pruneSeenMessages() {
	gm.messageMutex.Lock()
	defer gm.messageMutex.Unlock()
	
	now := time.Now()
	for id, seenTime := range gm.seenMessages {
		// Remove messages older than TTL
		if now.Sub(seenTime) > gm.gossipConfig.MessageTTL {
			delete(gm.seenMessages, id)
		}
	}
}

// PropagateGossip broadcasts a gossip message to selected peers
func (gm *GossipManager) PropagateGossip(
	message interface{},
	domains []string,
	targets []string,
) error {
	// Create a unique message ID if needed
	messageID := generateMessageID(message)
	
	// Mark as seen to avoid processing our own messages again
	gm.markMessageSeen(messageID)
	
	// Determine how many peers to propagate to
	activePeers := gm.peerManager.GetActivePeersCount()
	propagationCount := int(math.Ceil(float64(activePeers) * gm.gossipConfig.PropagationFactor))
	
	// Ensure minimum propagation for reliability
	if propagationCount < 2 && activePeers >= 2 {
		propagationCount = 2
	}
	
	// Select peers to propagate to based on the domains
	// First prioritize peers that specialize in these domains
	domainPeers := make([]string, 0)
	for _, domain := range domains {
		// In a real implementation, this would use the peer manager's domain-specific selection
		// For now, we just simulate getting some peers for each domain
		// Include the domain name in the peer selection to avoid unused variable warning
		domainSpecificPeers := gm.peerManager.GetPeersForDomain(domain, 2)
		domainPeers = append(domainPeers, domainSpecificPeers...)
	}
	
	// Then add some random peers for network-wide propagation
	randomPeers := make([]string, 0)
	// In a real implementation, this would select random peers from the peer manager
	// For now, just simulate some random peers
	randomPeers = append(randomPeers, "peer3", "peer4")
	
	// Combine domain-specific and random peers, prioritizing domain peers
	selectedPeers := make([]string, 0, propagationCount)
	selectedPeers = append(selectedPeers, domainPeers...)
	
	// Add random peers until we reach the desired propagation count
	for _, peer := range randomPeers {
		if len(selectedPeers) >= propagationCount {
			break
		}
		if !containsString(selectedPeers, peer) {
			selectedPeers = append(selectedPeers, peer)
		}
	}
	
	// In a real implementation, this would actually send the messages
	// For now, just log and track metrics
	gm.logger.Debug("Propagating gossip message",
		zap.Int("peerCount", len(selectedPeers)),
		zap.Int("domainCount", len(domains)),
		zap.Strings("targets", targets))
	
	gm.metrics.MessagesOriginated++
	
	return nil
}

// ProcessGossipMessage handles an incoming gossip message
func (gm *GossipManager) ProcessGossipMessage(
	message interface{},
	domains []string,
	originatorID string,
	hopCount int32,
) (bool, error) {
	// Extract message ID
	messageID := generateMessageID(message)
	
	// Check if we've seen this message before
	if gm.hasSeenMessage(messageID) {
		gm.metrics.MessagesDuplicate++
		return false, nil
	}
	
	// Mark the message as seen
	gm.markMessageSeen(messageID)
	gm.metrics.MessagesReceived++
	
	// In a real implementation, this would process the message contents
	// For now, just simulate processing
	
	// Decide if we should forward this message
	shouldForward := hopCount < gm.gossipConfig.MaxHopCount
	
	if shouldForward {
		// Select peers to forward to
		// In a real implementation, this would call PropagateGossip
		// For now, just update metrics
		gm.metrics.MessagesForwarded++
	}
	
	return true, nil
}

// hasSeenMessage checks if a message has been seen before
func (gm *GossipManager) hasSeenMessage(messageID string) bool {
	gm.messageMutex.RLock()
	defer gm.messageMutex.RUnlock()
	
	_, seen := gm.seenMessages[messageID]
	return seen
}

// markMessageSeen marks a message as having been seen
func (gm *GossipManager) markMessageSeen(messageID string) {
	gm.messageMutex.Lock()
	defer gm.messageMutex.Unlock()
	
	gm.seenMessages[messageID] = time.Now()
}

// generateMessageID creates a unique ID for a message
// In a real implementation, this would use a cryptographic hash
func generateMessageID(message interface{}) string {
	// This is a placeholder - in reality would hash the message
	return "msg-" + time.Now().Format(time.RFC3339Nano)
}
