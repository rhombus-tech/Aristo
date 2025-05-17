package mesh

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// GossipProtocolV2 is the enhanced implementation of the gossip protocol using the GossipManager
type GossipProtocolV2 struct {
	logger        *zap.Logger
	gossipManager *GossipManager
	peerManager   *PeerManagerV2
	messageCache  sync.Map // Cache of recently processed messages
	handlers      map[string]GossipMessageHandler
	handlersMutex sync.RWMutex
}

// GossipMessage represents a message to be propagated through the gossip protocol
type GossipMessage struct {
	ID          string
	Type        string
	Payload     []byte
	Domains     []string
	Originator  string
	HopCount    int32
	Timestamp   time.Time
	Priority    int
	IncludeProof bool
	TargetPeers []string
}

// GossipMessageHandler is a function that processes a specific type of gossip message
type GossipMessageHandler func(message *GossipMessage) error

// NewGossipProtocolV2 creates a new instance of the enhanced gossip protocol
func NewGossipProtocolV2(
	logger *zap.Logger,
	gossipManager *GossipManager,
	peerManager *PeerManagerV2,
) *GossipProtocolV2 {
	return &GossipProtocolV2{
		logger:        logger,
		gossipManager: gossipManager,
		peerManager:   peerManager,
		handlers:      make(map[string]GossipMessageHandler),
	}
}

// RegisterHandler registers a handler for a specific message type
func (gp *GossipProtocolV2) RegisterHandler(messageType string, handler GossipMessageHandler) {
	gp.handlersMutex.Lock()
	defer gp.handlersMutex.Unlock()
	
	gp.handlers[messageType] = handler
	gp.logger.Debug("Registered gossip handler", zap.String("messageType", messageType))
}

// BroadcastMessage sends a message to the network via the gossip protocol
func (gp *GossipProtocolV2) BroadcastMessage(message *GossipMessage) error {
	if message.HopCount == 0 {
		// This is a new message, set initial values
		message.HopCount = 1
		message.Timestamp = time.Now()
		message.Originator = "self" // In reality, this would be the node's ID
	}
	
	// Increment hop count for outgoing messages
	message.HopCount++
	
	gp.logger.Debug("Broadcasting gossip message",
		zap.String("id", message.ID),
		zap.String("type", message.Type),
		zap.Int32("hopCount", message.HopCount),
		zap.Strings("domains", message.Domains))
	
	// Use the gossip manager to propagate the message
	return gp.gossipManager.PropagateGossip(message, message.Domains, message.TargetPeers)
}

// HandleMessage processes an incoming gossip message
func (gp *GossipProtocolV2) HandleMessage(message *GossipMessage) error {
	// First check if this message should be processed
	processed, err := gp.gossipManager.ProcessGossipMessage(
		message,
		message.Domains,
		message.Originator,
		message.HopCount,
	)
	
	if err != nil {
		return fmt.Errorf("error processing gossip message: %w", err)
	}
	
	if !processed {
		// Message was a duplicate or otherwise not processed
		return nil
	}
	
	// Grab the appropriate handler for this message type
	gp.handlersMutex.RLock()
	handler, exists := gp.handlers[message.Type]
	gp.handlersMutex.RUnlock()
	
	if !exists {
		return fmt.Errorf("no handler found for message type: %s", message.Type)
	}
	
	// Process the message using the handler
	if err := handler(message); err != nil {
		return fmt.Errorf("handler error for message type %s: %w", message.Type, err)
	}
	
	// If configured, forward the message to other peers
	if message.HopCount < gp.gossipManager.gossipConfig.MaxHopCount {
		// Create a new message for forwarding
		forwardMsg := *message // Copy the message
		forwardMsg.HopCount++  // Increment hop count
		
		// Forward the message
		if err := gp.BroadcastMessage(&forwardMsg); err != nil {
			gp.logger.Warn("Failed to forward gossip message",
				zap.String("id", message.ID),
				zap.Error(err))
		}
	}
	
	return nil
}

// CreateStateUpdateMessage creates a gossip message for a state update
func (gp *GossipProtocolV2) CreateStateUpdateMessage(
	domains []string,
	stateHash string,
	changeSet map[string][]byte,
	priority int,
) *GossipMessage {
	// In a real implementation, this would serialize the change set
	// For now, just create a simple message
	msg := &GossipMessage{
		ID:          fmt.Sprintf("state-update-%s", time.Now().Format(time.RFC3339Nano)),
		Type:        "state_update",
		Domains:     domains,
		Priority:    priority,
		Timestamp:   time.Now(),
		IncludeProof: true,
	}
	
	// In a real implementation, this would properly serialize the payload
	// For now, just add a dummy payload
	msg.Payload = []byte(fmt.Sprintf("state-hash:%s", stateHash))
	
	return msg
}

// Start begins the gossip protocol operations
func (gp *GossipProtocolV2) Start(ctx context.Context) error {
	// Set up default handlers if needed
	gp.setupDefaultHandlers()
	
	// Start the gossip manager
	gp.gossipManager.Start(ctx)
	
	gp.logger.Info("Gossip protocol started")
	return nil
}

// Stop halts the gossip protocol operations
func (gp *GossipProtocolV2) Stop() {
	gp.gossipManager.Stop()
	gp.logger.Info("Gossip protocol stopped")
}

// setupDefaultHandlers registers the default message handlers
func (gp *GossipProtocolV2) setupDefaultHandlers() {
	// Register a default state update handler
	if _, exists := gp.handlers["state_update"]; !exists {
		gp.RegisterHandler("state_update", gp.handleStateUpdate)
	}
	
	// Register other default handlers as needed
}

// handleStateUpdate is the default handler for state update messages
func (gp *GossipProtocolV2) handleStateUpdate(message *GossipMessage) error {
	// In a real implementation, this would apply the state update
	// For now, just log it
	gp.logger.Debug("Received state update via gossip",
		zap.String("id", message.ID),
		zap.Strings("domains", message.Domains),
		zap.Int32("hopCount", message.HopCount))
	
	// In a real implementation, this would process the state update
	return nil
}
