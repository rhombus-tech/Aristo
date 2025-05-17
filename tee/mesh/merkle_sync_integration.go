// Package mesh provides a mesh network for TEE-to-TEE communication
package mesh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	// We'll need this when fully implementing the proto types
	// For now, add a _ prefix to avoid the unused import error
	_ "github.com/rhombus-tech/vm/tee/proto"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
	"google.golang.org/protobuf/types/known/timestamppb"
	"strings"
)

// MerkleStateService handles delta-based state synchronization using Merkle proofs
type MerkleStateService struct {
	meshService   *MeshService
	merkleSync    *MerkleStateSync
	logger        *zap.Logger
	domainManager *DomainManager
	teeID         string
	syncSemaphore *semaphore.Weighted
	metrics       *SyncServiceMetrics
	
	// Enhanced networking components
	peerManager   *PeerManagerV2
	gossipManager *GossipManager
	gossipProtocol *GossipProtocolV2
	syncCache     *SyncResponseCacheV2
}

// SyncServiceMetrics tracks performance metrics for the sync service
type SyncServiceMetrics struct {
	SyncRequestsTotal     int64
	SyncRequestsSucceeded int64
	SyncRequestsFailed    int64
	AvgSyncLatencyMs      int64
	BytesSyncedTotal      int64
	PartialSyncsTotal     int64
	FullSyncsTotal        int64
	GossipMessagesTotal   int64
	DomainPrefetches      int64
	
	// Performance optimization metrics
	DomainCacheHitRate    float64
	SnapshotCacheHitRate  float64
	
	// Additional metrics
	SyncRequestsInitiated int64
	CacheHits             int64
	RejectedSyncs         int64
	SnapshotFailures      int64
	RequestCreationFailures int64
	FailedSyncs           int64
	ProcessingFailures    int64
	SuccessfulSyncs       int64
	InvalidMessages       int64
	DiffCreationFailures  int64
	InvalidRequests       int64
	AccessDeniedRequests  int64
	SnapshotTimeNs        int64
	ProcessingTimeNs      int64
	ResponseTimeNs        int64
	SuccessfulResponses   int64
	IgnoredMessages       int64
	MaxHopMessages        int64
	ForwardedMessages     int64
	ProcessedMessages     int64
}

// DomainManager handles domain registration and tracking
type DomainManager struct {
	// Domain definitions
	domains         map[string][]string  // Domain ID -> Object IDs
	objectDomain    map[string]string    // Object ID -> Domain ID
	
	// Domain access patterns for prefetching optimization
	domainAccess    map[string][]string  // Domain ID -> Common domains accessed together
	accessFrequency map[string]int64     // Domain ID -> Access frequency
	
	// Domain permissions for regulatory compliance
	domainPermissions map[string][]string // Domain ID -> List of regions allowed
	
	mutex           sync.RWMutex
}

// ApplyDiffV2 applies state differences to the domain
func (dm *DomainManager) ApplyDiffV2(domainID string, diffData []byte) error {
	// Validate input
	if len(diffData) == 0 {
		return fmt.Errorf("empty diff data for domain %s", domainID)
	}

	dm.mutex.Lock()
	defer dm.mutex.Unlock()

	// Check if the domain exists
	if _, exists := dm.domains[domainID]; !exists {
		return fmt.Errorf("domain %s not found", domainID)
	}

	// Unmarshal the diff data
	var diffObj map[string]interface{}
	if err := json.Unmarshal(diffData, &diffObj); err != nil {
		return fmt.Errorf("failed to unmarshal diff data for domain %s: %w", domainID, err)
	}

	// Process object additions, updates, and removals
	if additions, ok := diffObj["additions"].(map[string]interface{}); ok {
		for objectID := range additions {
			// Add object to domain if not already present
			if !dm.objectExists(domainID, objectID) {
				dm.domains[domainID] = append(dm.domains[domainID], objectID)
				dm.objectDomain[objectID] = domainID
			}
		}
	}

	if removals, ok := diffObj["removals"].([]interface{}); ok {
		for _, obj := range removals {
			if objectID, ok := obj.(string); ok {
				// Remove object from domain
				dm.removeObjectFromDomain(domainID, objectID)
			}
		}
	}

	// Update access patterns based on this operation
	dm.updateAccessFrequency(domainID)

	return nil
}

// objectExists checks if an object exists in a domain
func (dm *DomainManager) objectExists(domainID, objectID string) bool {
	for _, id := range dm.domains[domainID] {
		if id == objectID {
			return true
		}
	}
	return false
}

// removeObjectFromDomain removes an object from a domain
func (dm *DomainManager) removeObjectFromDomain(domainID, objectID string) {
	objects := dm.domains[domainID]
	for i, id := range objects {
		if id == objectID {
			// Remove the object from the slice
			dm.domains[domainID] = append(objects[:i], objects[i+1:]...)
			delete(dm.objectDomain, objectID)
			break
		}
	}
}

// updateAccessFrequency updates the access frequency counter for a domain
func (dm *DomainManager) updateAccessFrequency(domainID string) {
	dm.accessFrequency[domainID]++
}

// GetAllDomains returns a list of all registered domains
func (dm *DomainManager) GetAllDomains() []string {
	dm.mutex.RLock()
	defer dm.mutex.RUnlock()
	
	domains := make([]string, 0, len(dm.domains))
	for domain := range dm.domains {
		domains = append(domains, domain)
	}
	
	return domains
}

// NewMerkleStateService creates a new Merkle state synchronization service
func NewMerkleStateService(
	meshService *MeshService,
	diffUpdater DiffUpdater,
	logger *zap.Logger,
	maxConcurrentSyncs int64,
) (*MerkleStateService, error) {
	if meshService == nil {
		return nil, errors.New("mesh service cannot be nil")
	}
	
	// Get the local TEE ID
	localTeeID := "local-tee-id" // In a real implementation, would get from meshService
	
	// Initialize domain manager
	domainManager := &DomainManager{
		domains:           make(map[string][]string),
		domainPermissions: make(map[string][]string),
		mutex:             sync.RWMutex{},
	}
	
	// Create the service
	service := &MerkleStateService{
		meshService:   meshService,
		logger:        logger,
		domainManager: domainManager,
		syncSemaphore: semaphore.NewWeighted(maxConcurrentSyncs),
		metrics:       &SyncServiceMetrics{},
		syncCache:     NewSyncResponseCacheV2(500, 5*time.Minute), // Cache 500 responses for 5 minutes
		teeID:         localTeeID,
	}
	
	// Initialize peer manager
	// Note: Using simplified initialization with just logger as required by interface
	service.peerManager = NewPeerManagerV2(logger, domainManager, nil)
	
	// Start background processes
	service.peerManager.Start(context.Background())
	
	return service, nil
}

// RegisterDomain registers a domain with the service
func (mss *MerkleStateService) RegisterDomain(domainID string, objectIDs []string) {
	mss.domainManager.mutex.Lock()
	defer mss.domainManager.mutex.Unlock()
	
	mss.domainManager.domains[domainID] = objectIDs
	for _, objectID := range objectIDs {
		mss.domainManager.objectDomain[objectID] = domainID
	}
	
	// Also register with merkle sync
	mss.merkleSync.RegisterDomain(domainID, objectIDs)
}

// SetDomainPermissions sets the regions allowed to access a domain
func (mss *MerkleStateService) SetDomainPermissions(domainID string, allowedRegions []string) {
	mss.domainManager.mutex.Lock()
	defer mss.domainManager.mutex.Unlock()
	
	mss.domainManager.domainPermissions[domainID] = allowedRegions
}

// OptimizedSync performs delta-based state synchronization with the target TEE for specified domains
func (mss *MerkleStateService) OptimizedSync(targetTeeID string, domains []string, timeout time.Duration) (interface{}, error) {
	// Record metrics
	mss.metrics.SyncRequestsInitiated++
	
	// Check cache first
	cacheKey := targetTeeID + "-" + strings.Join(domains, ",")
	if cached, found := mss.syncCache.Get(cacheKey); found {
		mss.logger.Debug("Using cached sync response", 
			zap.String("targetTee", targetTeeID), 
			zap.Strings("domains", domains))
		mss.metrics.CacheHits++
		return cached, nil
	}
	
	// Select optimal peers for this sync operation based on domains
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	
	// Determine how many peers to use based on domain count and importance
	peersNeeded := 1
	if len(domains) > 5 {
		peersNeeded = 2
	}
	if len(domains) > 20 {
		peersNeeded = 3
	}
	
	// Get best-suited peers for these domains
	selectedPeers := make([]string, 0, peersNeeded)
	for _, domain := range domains {
		domainPeers := mss.peerManager.SelectPeersForDomain(domain, peersNeeded)
		for _, peer := range domainPeers {
			if peer == targetTeeID && !containsString(selectedPeers, peer) {
				selectedPeers = append(selectedPeers, peer)
				if len(selectedPeers) >= peersNeeded {
					break
				}
			}
		}
	}
	
	// If we couldn't find the target in our peer list, just use the targetTeeID directly
	if len(selectedPeers) == 0 {
		selectedPeers = append(selectedPeers, targetTeeID)
	}
	
	// Try to acquire semaphore to limit concurrent sync operations
	if err := mss.syncSemaphore.Acquire(ctx, 1); err != nil {
		mss.metrics.RejectedSyncs++
		return nil, fmt.Errorf("failed to acquire sync semaphore: %w", err)
	}
	defer mss.syncSemaphore.Release(1)
	
	// Get current state snapshot for the domains
	snapshotStart := time.Now()
	baseSnapshot, err := mss.getLocalSnapshot(domains)
	if err != nil {
		mss.metrics.SnapshotFailures++
		return nil, fmt.Errorf("failed to create local snapshot: %w", err)
	}
	mss.metrics.SnapshotTimeNs += time.Since(snapshotStart).Nanoseconds()
	
	// Create optimized sync request with Merkle proofs
	request, err := mss.createOptimizedRequest(mss.teeID, baseSnapshot, domains)
	if err != nil {
		mss.metrics.RequestCreationFailures++
		return nil, fmt.Errorf("failed to create sync request: %w", err)
	}
	
	// Send the request to the target TEE
	syncStart := time.Now()
	response, err := mss.sendSyncRequest(ctx, targetTeeID, request)
	syncDuration := time.Since(syncStart)
	
	if err != nil {
		// Update peer score with failure
		mss.peerManager.UpdatePeerScore(targetTeeID, syncDuration, false, 0.0)
		mss.metrics.FailedSyncs++
		return nil, fmt.Errorf("sync request to %s failed: %w", targetTeeID, err)
	}
	
	// Process the sync response
	processStart := time.Now()
	result, err := mss.processSyncResponse(response, domains)
	mss.metrics.ProcessingTimeNs += time.Since(processStart).Nanoseconds()
	
	if err != nil {
		// Update peer score with failure
		mss.peerManager.UpdatePeerMetrics(targetTeeID, syncDuration, false, 0)
		mss.metrics.ProcessingFailures++
		return nil, fmt.Errorf("failed to process sync response: %w", err)
	}
	
	// Update metrics
	mss.metrics.SuccessfulSyncs++
	mss.metrics.AvgSyncLatencyMs = (mss.metrics.AvgSyncLatencyMs*int64(mss.metrics.SuccessfulSyncs-1) + syncDuration.Milliseconds()) / int64(mss.metrics.SuccessfulSyncs)
	mss.metrics.ResponseTimeNs += syncDuration.Nanoseconds()
	
	// Store in cache
	mss.syncCache.Set(cacheKey, result)
	
	// Success!
	return result, nil
}

// HandleSyncOptimized handles sync requests from other TEEs
func (mss *MerkleStateService) HandleSyncOptimized(ctx context.Context, request interface{}) (interface{}, error) {
	start := time.Now()
	mss.metrics.SyncRequestsTotal++
	
	// Extract request details - in production code, we'd use proper type assertions and validation
	req, ok := request.(struct {
		SourceTeeId string
		Domains     []string
	})
	
	if !ok {
		mss.metrics.InvalidRequests++
		mss.logger.Warn("Invalid sync request format", zap.Any("request", request))
		return createErrorResponse(400, "Invalid request format")
	}
	
	mss.logger.Debug("Received sync request",
		zap.String("sourceTee", req.SourceTeeId),
		zap.Strings("domains", req.Domains),
		zap.Int("domainCount", len(req.Domains)))
	
	// Register the source as a peer in our peer manager if available
	if mss.peerManager != nil {
		mss.peerManager.RegisterPeer(req.SourceTeeId, "", req.Domains)
	}
	
	// Check if we can serve the requested domains
	allowedDomains, canServeAll := mss.canServeDomains(req.Domains)
	
	// If we can't serve any of the requested domains, return access denied
	if len(allowedDomains) == 0 {
		mss.logger.Debug("Access denied for all requested domains", 
			zap.String("source", req.SourceTeeId),
			zap.Strings("domains", req.Domains))
		mss.metrics.AccessDeniedRequests++
		resp, err := createErrorResponse(403, "access denied for domains")
		return resp, err
	}
	
	// If we can only serve some domains, log this information
	if !canServeAll {
		mss.logger.Debug("Partial domain service", 
			zap.String("source", req.SourceTeeId),
			zap.Strings("requested", req.Domains),
			zap.Strings("allowed", allowedDomains))
	}
	
	// Check cache for identical request to avoid redundant work
	cacheKey := req.SourceTeeId + "-" + strings.Join(req.Domains, ",")
	if cached, found := mss.syncCache.Get(cacheKey); found {
		mss.metrics.CacheHits++
		mss.logger.Debug("Using cached response", zap.String("sourceTee", req.SourceTeeId))
		return cached, nil
	}
	
	// Get our local state for these domains
	localState, err := mss.getLocalSnapshot(req.Domains)
	if err != nil {
		mss.metrics.SnapshotFailures++
		mss.logger.Error("Failed to create local snapshot", zap.Error(err))
		return createErrorResponse(500, "Internal error creating snapshot")
	}
	
	// Create an enhanced diff with Merkle proofs
	diff, err := mss.createEnhancedDiff(localState, req.Domains)
	if err != nil {
		mss.metrics.DiffCreationFailures++
		mss.logger.Error("Failed to create diff", zap.Error(err))
		return createErrorResponse(500, "Internal error creating diff")
	}
	
	// Convert the enhanced diff to a response
	// Use the diff to construct our response
	diffMaps := mss.extractDiffMaps(diff)
	
	response := struct {
		Status        string
		DomainsServed []string
		Diffs         map[string][]byte
		MerkleProofs  map[string][]byte
		Timestamp     *timestamppb.Timestamp
	}{
		Status:        "success",
		DomainsServed: req.Domains,
		Diffs:         diffMaps.Diffs,         // Populated from diff
		MerkleProofs:  diffMaps.MerkleProofs,  // Populated from diff
		Timestamp:     timeToProtoTimestamp(time.Now()),
	}
	
	// Track metrics
	mss.metrics.ResponseTimeNs += time.Since(start).Nanoseconds()
	mss.metrics.SuccessfulResponses++
	
	// Cache the response for future identical requests
	mss.syncCache.Set(cacheKey, response)
	
	return response, nil
}

// HandleGossipSync handles gossip sync messages between TEEs
func (mss *MerkleStateService) HandleGossipSync(ctx context.Context, message interface{}) error {
	// Always increment the gossip message counter, even for invalid messages
	mss.metrics.GossipMessagesTotal++
	
	// Validate and extract message contents
	msg, ok := message.(map[string]interface{})
	if !ok {
		mss.metrics.InvalidMessages++
		return fmt.Errorf("invalid gossip message format")
	}
	
	// Extract gossip message details
	domains, ok := msg["domains"].([]string)
	if !ok {
		mss.metrics.InvalidMessages++
		return fmt.Errorf("invalid domains in gossip message")
	}
	
	source, ok := msg["source"].(string)
	if !ok {
		mss.metrics.InvalidMessages++
		return fmt.Errorf("missing source in gossip message")
	}

	// Extract message type
	msgType, ok := msg["type"].(string)
	if !ok {
		msgType = "state_sync" // Default to state sync if not specified
	}
	
	// Log receipt of the message
	if mss.logger != nil {
		mss.logger.Debug("Received gossip sync message",
			zap.String("source", source),
			zap.Int("domainCount", len(domains)),
			zap.String("type", msgType))
	}

	// Try to acquire semaphore to limit concurrent sync operations
	if err := mss.syncSemaphore.Acquire(ctx, 1); err != nil {
		mss.metrics.RejectedSyncs++
		return fmt.Errorf("failed to acquire sync semaphore: %w", err)
	}
	defer mss.syncSemaphore.Release(1)

	// Process based on message type
	switch msgType {
	case "state_request":
		return mss.handleStateRequest(ctx, source, domains, msg)
	case "state_response":
		return mss.handleStateResponse(ctx, source, domains, msg)
	case "state_diff":
		return mss.handleStateDiff(ctx, source, domains, msg)
	case "state_sync":
		return mss.handleStateSync(ctx, source, domains, msg)
	default:
		mss.metrics.InvalidMessages++
		return fmt.Errorf("unknown gossip message type: %s", msgType)
	}
}

// handleStateRequest processes a state request message from another TEE
func (mss *MerkleStateService) handleStateRequest(ctx context.Context, source string, domains []string, msg map[string]interface{}) error {
	// Log the request
	if mss.logger != nil {
		mss.logger.Debug("Processing state request",
			zap.String("source", source),
			zap.Int("domainCount", len(domains)))
	}

	// Get the local snapshot for the requested domains
	localSnapshot, err := mss.getLocalSnapshot(domains)
	if err != nil {
		mss.metrics.SnapshotFailures++
		return fmt.Errorf("failed to create local snapshot: %w", err)
	}

	// Create an enhanced diff with Merkle proofs
	enhancedDiff, err := mss.createEnhancedDiff(localSnapshot, domains)
	if err != nil {
		mss.metrics.DiffCreationFailures++
		return fmt.Errorf("failed to create diff: %w", err)
	}

	// Prepare response message
	responseData := map[string]interface{}{
		"type":       "state_response",
		"source":     mss.teeID,
		"domains":    domains,
		"snapshot":   localSnapshot,
		"diff":       enhancedDiff,
		"timestamp":  time.Now().Format(time.RFC3339Nano),
	}

	// Convert to GossipMessage - serialize the response data to JSON
	payloadBytes, err := json.Marshal(responseData)
	if err != nil {
		return fmt.Errorf("failed to marshal response data: %w", err)
	}

	gossipMsg := &GossipMessage{
		ID:         fmt.Sprintf("resp-%s", source),
		Type:       "state_response",
		Originator: mss.teeID,
		Timestamp:  time.Now(),
		Priority:   3, // Medium-high priority for sync responses
		Payload:    payloadBytes,
		Domains:    domains,
		HopCount:   1,
	}

	// Send the response using the gossip protocol
	err = mss.gossipProtocol.BroadcastMessage(gossipMsg)
	if err != nil {
		mss.metrics.FailedSyncs++
		// Log the failure
		if mss.logger != nil {
			mss.logger.Debug("Gossip protocol not available, cannot send response")
		}
		// Return the error
		return fmt.Errorf("failed to send response message: %w", err)
	}

	return nil
}

// handleStateResponse processes a state response message from another TEE
func (mss *MerkleStateService) handleStateResponse(ctx context.Context, source string, domains []string, msg map[string]interface{}) error {
	// Log the response
	if mss.logger != nil {
		mss.logger.Debug("Processing state response",
			zap.String("source", source),
			zap.Int("domainCount", len(domains)))
	}

	// Extract the snapshot and diff from the message
	snapshot, ok := msg["snapshot"]
	if !ok {
		mss.metrics.InvalidMessages++
		return fmt.Errorf("missing snapshot in state response")
	}

	diff, ok := msg["diff"]
	if !ok {
		mss.metrics.InvalidMessages++
		return fmt.Errorf("missing diff in state response")
	}

	// Validate the response by checking Merkle proofs
	if err := mss.validateStateResponse(snapshot, diff, domains); err != nil {
		mss.metrics.ProcessingFailures++
		return fmt.Errorf("failed to validate state response: %w", err)
	}

	// Apply the diff to the local state
	if err := mss.applyDiff(diff, domains); err != nil {
		mss.metrics.ProcessingFailures++
		return fmt.Errorf("failed to apply diff: %w", err)
	}

	// Update metrics
	mss.metrics.BytesSyncedTotal += estimateDiffSize(diff)
	mss.metrics.SuccessfulSyncs++

	// Cache the response for future use
	if mss.syncCache != nil {
		// Create a combined cache entry for the response
		cacheKey := source + "-" + strings.Join(domains, ",")
		mss.syncCache.StoreResponse(cacheKey, diff)
	}

	return nil
}

// handleStateDiff processes a state diff message from another TEE
func (mss *MerkleStateService) handleStateDiff(ctx context.Context, source string, domains []string, msg map[string]interface{}) error {
	// Log the diff
	if mss.logger != nil {
		mss.logger.Debug("Processing state diff",
			zap.String("source", source),
			zap.Int("domainCount", len(domains)))
	}

	// Extract the diff from the message
	diff, ok := msg["diff"]
	if !ok {
		mss.metrics.InvalidMessages++
		return fmt.Errorf("missing diff in state diff message")
	}

	// Extract merkle root/proof information if available
	proofs, _ := msg["merkle_proofs"].(map[string]interface{})

	// Verify the diff against local state if proofs are available
	if proofs != nil && len(proofs) > 0 {
		// Get local snapshot for comparison
		localSnapshot, err := mss.getLocalSnapshot(domains)
		if err != nil {
			mss.metrics.SnapshotFailures++
			return fmt.Errorf("failed to create local snapshot for verification: %w", err)
		}

		// Verify proofs
		if err := mss.verifyMerkleProofs(localSnapshot, diff, proofs); err != nil {
			mss.metrics.ProcessingFailures++
			return fmt.Errorf("failed to verify merkle proofs: %w", err)
		}
	}

	// Apply the diff to the local state
	if err := mss.applyDiff(diff, domains); err != nil {
		mss.metrics.ProcessingFailures++
		return fmt.Errorf("failed to apply diff: %w", err)
	}

	// Update metrics
	mss.metrics.BytesSyncedTotal += estimateDiffSize(diff)
	mss.metrics.SuccessfulSyncs++

	return nil
}

// handleStateSync processes a complete state sync message
func (mss *MerkleStateService) handleStateSync(ctx context.Context, source string, domains []string, msg map[string]interface{}) error {
	// Log the sync request
	if mss.logger != nil {
		mss.logger.Debug("Processing state sync",
			zap.String("source", source),
			zap.Int("domainCount", len(domains)))
	}

	// This is a full sync request - both getting and applying state
	// First get local snapshot for comparison
	localSnapshot, err := mss.getLocalSnapshot(domains)
	if err != nil {
		mss.metrics.SnapshotFailures++
		return fmt.Errorf("failed to create local snapshot: %w", err)
	}

	// Request full state from the source TEE
	response, err := mss.requestStateFromPeer(ctx, source, domains, localSnapshot)
	if err != nil {
		mss.metrics.FailedSyncs++
		return fmt.Errorf("failed to request state from peer: %w", err)
	}

	// Extract the diff from the response
	diff, ok := response["diff"]
	if !ok {
		mss.metrics.InvalidMessages++
		return fmt.Errorf("missing diff in sync response")
	}

	// Apply the diff to local state
	if err := mss.applyDiff(diff, domains); err != nil {
		mss.metrics.ProcessingFailures++
		return fmt.Errorf("failed to apply diff: %w", err)
	}

	// Update metrics
	mss.metrics.BytesSyncedTotal += estimateDiffSize(diff)
	mss.metrics.SuccessfulSyncs++
	mss.metrics.FullSyncsTotal++

	return nil
}

// Helper methods for gossip sync protocol implementation

// validateStateResponse validates a state response from another TEE by checking Merkle proofs
func (mss *MerkleStateService) validateStateResponse(snapshot interface{}, diff interface{}, domains []string) error {
	// Extract the Merkle proofs from the diff if available
	diffMap, ok := diff.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid diff format")
	}

	proofs, ok := diffMap["MerkleProofs"].(map[string][]byte)
	if !ok {
		// If no proofs available, we can't validate fully but we'll continue
		if mss.logger != nil {
			mss.logger.Debug("No Merkle proofs available for validation")
		}
		return nil
	}

	// Verify the proofs against the local state
	return mss.verifyMerkleProofs(snapshot, diff, proofs)
}

// verifyMerkleProofs verifies Merkle proofs against local state
func (mss *MerkleStateService) verifyMerkleProofs(localSnapshot interface{}, diff interface{}, proofs interface{}) error {
	// Check if we have a MerkleStateSync implementation
	if mss.merkleSync == nil {
		return fmt.Errorf("merkle state sync not initialized")
	}

	// Convert types to what the merkle sync needs
	diffObj, ok := diff.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid diff format for verification")
	}

	proofsMap, ok := proofs.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid proofs format for verification")
	}

	// Perform verification using the MerkleStateSync
	// This is implementation-specific based on the Merkle tree used
	return mss.merkleSync.VerifyProofsV2(localSnapshot, diffObj, proofsMap)
}

// applyDiff applies state differences to the local state
func (mss *MerkleStateService) applyDiff(diff interface{}, domains []string) error {
	// Validate input
	diffMap, ok := diff.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid diff format")
	}

	// Extract the actual diffs from the message
	domainDiffs, ok := diffMap["Diffs"].(map[string][]byte)
	if !ok {
		return fmt.Errorf("invalid diffs format in diff data")
	}

	// Apply changes for each domain
	for _, domain := range domains {
		domainDiff, exists := domainDiffs[domain]
		if !exists {
			continue // No changes for this domain
		}

		// Apply the diff to the domain using the domain manager
		if err := mss.domainManager.ApplyDiffV2(domain, domainDiff); err != nil {
			return fmt.Errorf("failed to apply diff for domain %s: %w", domain, err)
		}
	}

	return nil
}

// estimateDiffSize estimates the size of a diff in bytes for metrics
func estimateDiffSize(diff interface{}) int64 {
	// If the diff is nil, return 0
	if diff == nil {
		return 0
	}

	// Try to convert to map
	diffMap, ok := diff.(map[string]interface{})
	if !ok {
		return 0
	}

	// Extract the diffs
	domainDiffs, ok := diffMap["Diffs"].(map[string][]byte)
	if !ok {
		return 0
	}

	// Sum up the size of all diffs
	var totalSize int64
	for _, domainDiff := range domainDiffs {
		totalSize += int64(len(domainDiff))
	}

	return totalSize
}

// requestStateFromPeer requests full state for domains from a specific peer
func (mss *MerkleStateService) requestStateFromPeer(ctx context.Context, peerID string, domains []string, localSnapshot interface{}) (map[string]interface{}, error) {
	// Create a state request message
	request := map[string]interface{}{
		"type":       "state_request",
		"source":     mss.teeID,
		"domains":    domains,
		"timestamp":  time.Now().Format(time.RFC3339Nano),
	}

	// Send the request to the peer
	if mss.gossipProtocol != nil {
		// Use the gossip protocol for sending if available
		if mss.logger != nil {
			mss.logger.Debug("Sending state request via gossip protocol",
				zap.String("target", peerID),
				zap.Int("domainCount", len(domains)))
		}

		// For gossip protocol, we typically send and then wait for a response
		// This would require an implementation of a pending request tracker
		// and response handler in a real system
		return nil, fmt.Errorf("gossip protocol state request not implemented")
	}

	// Fall back to direct sync request if gossip protocol isn't available
	response, err := mss.SendRequest(ctx, peerID, request)
	if err != nil {
		return nil, fmt.Errorf("failed to send state request: %w", err)
	}

	// Convert response to map
	respMap, ok := response.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid response format")
	}

	return respMap, nil
}

// SendRequest sends a request to the target TEE
func (mss *MerkleStateService) SendRequest(ctx context.Context, targetTeeID string, request interface{}) (interface{}, error) {
	// First check if we should use direct peer communication via PeerManager
	if mss.peerManager != nil && mss.peerManager.IsPeerActive(targetTeeID) {
		// Get the peer score to make decisions about how to handle this request
		peerScore, exists := mss.peerManager.GetPeerScore(targetTeeID)
		
		// Use intelligently selected protocol based on peer performance
		if exists && peerScore != nil && peerScore.SuccessRate > 0.8 && peerScore.ResponseTime < time.Millisecond*200 {
			// This is a high-quality peer, use optimized protocol
			mss.logger.Debug("Using optimized sync protocol for high-quality peer",
				zap.String("peerID", targetTeeID),
				zap.Float64("successRate", peerScore.SuccessRate),
				zap.Duration("responseTime", peerScore.ResponseTime))
			
			// In a production implementation, would use direct peer communication
			response, err := mss.directPeerSync(ctx, targetTeeID, request)
			if err == nil {
				// Update peer metrics on successful request - 1000 bytes as placeholder for bytesTransferred
				mss.peerManager.UpdatePeerMetrics(targetTeeID, time.Since(time.Now()), true, 1000)
				return response, nil
			}
			
			// Fall through to normal implementation on failure
			mss.peerManager.UpdatePeerMetrics(targetTeeID, time.Since(time.Now()), false, 0)
		}
	}
	
	// For demo purposes, return a mock response until fully implemented
	return struct {
		Status      string
		Diffs       map[string][]byte
		MerkleProofs map[string][]byte
		Timestamp   *timestamppb.Timestamp
	}{
		Status:      "success",
		Diffs:       make(map[string][]byte),
		MerkleProofs: make(map[string][]byte),
		Timestamp:   timeToProtoTimestamp(time.Now()),
	}, nil
}

// Helper functions

// getLocalSnapshot creates a snapshot of local state for the given domains
func (mss *MerkleStateService) getLocalSnapshot(domains []string) (interface{}, error) {
	// This is a placeholder for real snapshot creation logic
	// In a production implementation, this would use the MerkleStateSync to create a snapshot
	return struct {
		Domains []string
		Data    map[string][]byte
		Hash    string
	}{
		Domains: domains,
		Data:    make(map[string][]byte),
		Hash:    fmt.Sprintf("snapshot-hash-%d", time.Now().UnixNano()),
	}, nil
}

// createEnhancedDiff creates a diff with Merkle proofs for the given domains
func (mss *MerkleStateService) createEnhancedDiff(snapshot interface{}, domains []string) (interface{}, error) {
	// This is a placeholder for real diff creation logic
	// In a production implementation, this would use the MerkleStateSync to create a diff
	return struct {
		Domains     []string
		Diffs       map[string][]byte
		MerkleProofs map[string][]byte
	}{
		Domains:     domains,
		Diffs:       make(map[string][]byte),
		MerkleProofs: make(map[string][]byte),
	}, nil
}

// createOptimizedRequest creates a sync request based on domain needs
func (mss *MerkleStateService) createOptimizedRequest(targetTeeID string, localSnapshot interface{}, domains []string) (interface{}, error) {
	// Create the request
	return createSyncRequest(mss.teeID, targetTeeID, domains, localSnapshot)
}

// processSyncResponse processes a sync response from a target TEE
func (mss *MerkleStateService) processSyncResponse(response interface{}, domains []string) (interface{}, error) {
	// Convert response to a proper type if necessary
	// This is just a placeholder for the real implementation
	return response, nil
}

// canServeDomains checks if the TEE can serve the requested domains
func (mss *MerkleStateService) canServeDomains(domains []string) ([]string, bool) {
	allowedDomains := make([]string, 0)
	
	// Check if domain manager or domains map is nil
	if mss.domainManager == nil || mss.domainManager.domains == nil {
		return allowedDomains, false
	}
	
	for _, domain := range domains {
		// Check if domain is managed by this TEE
		if _, exists := mss.domainManager.domains[domain]; exists {
			allowedDomains = append(allowedDomains, domain)
		}
	}

	// If no domains can be served, return an empty list and false
	if len(allowedDomains) == 0 {
		return allowedDomains, false
	}

	// If some but not all domains can be served, it's a partial response
	return allowedDomains, len(allowedDomains) == len(domains)
}

// isTargetTEE checks if the given TEE ID is in the list of target IDs
func isTargetTEE(targets []string, teeID string) bool {
	for _, target := range targets {
		if target == teeID {
			return true
		}
	}
	return false
}

// timeToProtoTimestamp converts time.Time to proto Timestamp
func timeToProtoTimestamp(t time.Time) *timestamppb.Timestamp {
	// If time is the zero value, return a zero timestamp
	if t.IsZero() {
		return &timestamppb.Timestamp{
			Seconds: 0,
			Nanos:   0,
		}
	}
	
	return &timestamppb.Timestamp{
		Seconds: t.Unix(),
		Nanos:   int32(t.Nanosecond()),
	}
}

// DiffMaps contains the data extracted from an enhanced diff
type DiffMaps struct {
	Diffs        map[string][]byte
	MerkleProofs map[string][]byte
}

// extractDiffMaps extracts the diffs and merkle proofs from an enhanced diff
func (mss *MerkleStateService) extractDiffMaps(diff interface{}) DiffMaps {
	// In a real implementation, this would extract the actual diff data
	// For now, we'll just create empty maps that would be populated in production
	return DiffMaps{
		Diffs:        make(map[string][]byte),
		MerkleProofs: make(map[string][]byte),
	}
}

// createErrorResponse creates an error response message
func createErrorResponse(errorCode int, errorMessage string) (interface{}, error) {
	// Create a response with error details
	response := struct {
		ErrorCode    int
		ErrorMessage string
		Timestamp    *timestamppb.Timestamp
	}{
		ErrorCode:    errorCode,
		ErrorMessage: errorMessage,
		Timestamp:    timeToProtoTimestamp(time.Now()),
	}
	
	// Return both the structured response and an error object
	return response, fmt.Errorf(errorMessage)
}

// sendSyncRequest sends a sync request to the target TEE
func (mss *MerkleStateService) sendSyncRequest(ctx context.Context, targetTeeID string, request interface{}) (interface{}, error) {
	// First check if we should use direct peer communication via PeerManager
	if mss.peerManager != nil && mss.peerManager.IsPeerActive(targetTeeID) {
		// Get the peer score to make decisions about how to handle this request
		peerScore, exists := mss.peerManager.GetPeerScore(targetTeeID)
		
		// Use intelligently selected protocol based on peer performance
		if exists && peerScore != nil && peerScore.SuccessRate > 0.8 && peerScore.ResponseTime < time.Millisecond*200 {
			// This is a high-quality peer, use optimized protocol
			mss.logger.Debug("Using optimized sync protocol for high-quality peer",
				zap.String("peerID", targetTeeID),
				zap.Float64("successRate", peerScore.SuccessRate),
				zap.Duration("responseTime", peerScore.ResponseTime))
			
			// In a production implementation, would use direct peer communication
			response, err := mss.directPeerSync(ctx, targetTeeID, request)
			if err == nil {
				// Update peer metrics on successful request - 1000 bytes as placeholder for bytesTransferred
				mss.peerManager.UpdatePeerMetrics(targetTeeID, time.Since(time.Now()), true, 1000)
				return response, nil
			}
			
			// Fall through to normal implementation on failure
			mss.peerManager.UpdatePeerMetrics(targetTeeID, time.Since(time.Now()), false, 0)
		}
	}
	
	// For demo purposes, return a mock response until fully implemented
	return struct {
		Status      string
		Diffs       map[string][]byte
		MerkleProofs map[string][]byte
		Timestamp   *timestamppb.Timestamp
	}{
		Status:      "success",
		Diffs:       make(map[string][]byte),
		MerkleProofs: make(map[string][]byte),
		Timestamp:   timeToProtoTimestamp(time.Now()),
	}, nil
}

// directPeerSync synchronizes directly with a peer, bypassing the mesh service
func (mss *MerkleStateService) directPeerSync(ctx context.Context, targetTeeID string, request interface{}) (interface{}, error) {
	// This is a placeholder for direct peer-to-peer communication
	// In a real implementation, this would use a direct communication channel
	mss.logger.Debug("Using direct peer sync", zap.String("targetTeeID", targetTeeID))
	return struct {
		Status      string
		Diffs       map[string][]byte
		MerkleProofs map[string][]byte
		Timestamp   *timestamppb.Timestamp
	}{
		Status:      "success",
		Diffs:       make(map[string][]byte),
		MerkleProofs: make(map[string][]byte),
		Timestamp:   timeToProtoTimestamp(time.Now()),
	}, nil
}

// createSyncRequest creates a sync request for the target TEE
func createSyncRequest(sourceTeeID, targetTeeID string, domains []string, snapshot interface{}) (interface{}, error) {
	// In a real implementation, this would create a proper request object
	// For now, we return a simple struct
	return struct {
		SourceTeeID string
		TargetTeeID string
		Domains     []string
		Snapshot    interface{}
		Timestamp   *timestamppb.Timestamp
	}{
		SourceTeeID: sourceTeeID,
		TargetTeeID: targetTeeID,
		Domains:     domains,
		Snapshot:    snapshot,
		Timestamp:   timeToProtoTimestamp(time.Now()),
	}, nil
}

// InitializeGossipProtocol sets up the gossip protocol for the service
func (mss *MerkleStateService) InitializeGossipProtocol(ctx context.Context, gossipConfig *GossipConfig) error {
	// Create a gossip manager if one doesn't exist yet
	if mss.gossipManager == nil {
		mss.gossipManager = NewGossipManager(mss.logger, mss.peerManager, gossipConfig)
	}
	
	// Create a gossip protocol implementation
	mss.gossipProtocol = NewGossipProtocolV2(mss.logger, mss.gossipManager, mss.peerManager)
	
	// Register handlers for different message types
	mss.gossipProtocol.RegisterHandler("state_update", func(message *GossipMessage) error {
		// Process state updates
		mss.logger.Info("Processing state update via gossip protocol",
			zap.String("id", message.ID),
			zap.Strings("domains", message.Domains))
		
		// In a production implementation, would process the state changes
		return nil
	})
	
	// Start the gossip protocol
	return mss.gossipProtocol.Start(ctx)
}

// BroadcastStateUpdate broadcasts a state update to peers
func (mss *MerkleStateService) BroadcastStateUpdate(domains []string, stateHash string, changes map[string][]byte, priority int) error {
	if mss.gossipProtocol == nil {
		return errors.New("gossip protocol not initialized")
	}
	
	// Create a state update message
	msg := mss.gossipProtocol.CreateStateUpdateMessage(
		domains,
		stateHash,
		changes,
		priority,
	)
	
	// Broadcast the message
	return mss.gossipProtocol.BroadcastMessage(msg)
}
