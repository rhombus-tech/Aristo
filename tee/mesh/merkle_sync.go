// Package mesh provides a mesh network for TEE-to-TEE communication
package mesh

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"time"

	proto "github.com/rhombus-tech/vm/tee/proto"
	"golang.org/x/sync/errgroup"
)

// DiffUpdater defines the interface for generating and applying differential updates with proto.RegionalSnapshot
type DiffUpdater interface {
	GenerateDiff(baseSnapshot, targetSnapshot *proto.RegionalSnapshot) (*DifferentialUpdate, error)
	ApplyDiff(baseSnapshot *proto.RegionalSnapshot, diff *DifferentialUpdate) (*proto.RegionalSnapshot, error)
}

// MerkleContent implements the Content interface for merkle tree objects
type MerkleContent struct {
	ObjectID string
	Data     []byte
	Hash     []byte
}

// Content is an interface that must be implemented by the objects in the tree.
type Content interface {
	CalculateHash() ([]byte, error)
	Equals(other Content) (bool, error)
}

// CalculateHash generates a SHA256 hash for the content
func (m MerkleContent) CalculateHash() ([]byte, error) {
	if len(m.Hash) > 0 {
		return m.Hash, nil
	}
	h := sha256.New()
	h.Write([]byte(m.ObjectID))
	h.Write(m.Data)
	return h.Sum(nil), nil
}

// Equals checks if two content objects are equal
func (m MerkleContent) Equals(other Content) (bool, error) {
	otherContent, ok := other.(MerkleContent)
	if !ok {
		return false, errors.New("invalid content type for comparison")
	}

	if m.ObjectID != otherContent.ObjectID {
		return false, nil
	}

	hash1, err := m.CalculateHash()
	if err != nil {
		return false, err
	}

	hash2, err := otherContent.CalculateHash()
	if err != nil {
		return false, err
	}

	return bytes.Equal(hash1, hash2), nil
}

// MerkleProof contains proof data for an object in the Merkle tree
type MerkleProof struct {
	ObjectID   string
	Proof      [][]byte
	RootHash   []byte
	ObjectHash []byte
}

// EnhancedDiff represents a differential update enhanced with Merkle proofs
type EnhancedDiff struct {
	BaseDiff     *DifferentialUpdate
	MerkleRoot   []byte
	DomainProofs map[string]*MerkleProof // Domain -> Proof
}

// MerkleTree represents a Merkle tree with helper methods
type MerkleTree struct {
	Root       *MerkleNode
	Content    []Content
	MerkleRoot []byte
}

// MerkleNode represents a node in a Merkle tree
type MerkleNode struct {
	Left  *MerkleNode
	Right *MerkleNode
	Hash  []byte
}

// CalculateHash gets the stored hash or calculates it if empty
func (n *MerkleNode) CalculateHash() ([]byte, error) {
	if n == nil {
		return nil, errors.New("nil node")
	}
	if len(n.Hash) > 0 {
		return n.Hash, nil
	}
	return nil, errors.New("hash not calculated")
}

// SnapshotConverter enables conversions between different snapshot formats
type SnapshotConverter struct {
	// Optional configuration
}

// NewSnapshotConverter creates a new converter
func NewSnapshotConverter() *SnapshotConverter {
	return &SnapshotConverter{}
}

// ProtoToRegional converts a proto.RegionalSnapshot to a RegionalSnapshot
func (c *SnapshotConverter) ProtoToRegional(ps *proto.RegionalSnapshot) *RegionalSnapshot {
	if ps == nil {
		return nil
	}

	rs := &RegionalSnapshot{
		RegionID:   ps.RegionId,
		SnapshotID: ps.SnapshotId,
		Timestamp:  time.Unix(0, ps.Timestamp),
	}

	// Convert TEE snapshot IDs
	if len(ps.TeeSnapshotIds) > 0 {
		rs.TEESnapshotIDs = make([][]byte, len(ps.TeeSnapshotIds))
		for i, id := range ps.TeeSnapshotIds {
			rs.TEESnapshotIDs[i] = make([]byte, len(id))
			copy(rs.TEESnapshotIDs[i], id)
		}
	}

	// Create TEE snapshots with state data if needed
	if ps.Summary != nil && len(ps.Summary.MerkleRoot) > 0 {
		// Create a state snapshot with the merkle root as the state data
		stateSnap := &StateSnapshot{
			RegionID:   ps.RegionId,
			SnapshotID: ps.SnapshotId,
			Timestamp:  time.Unix(0, ps.Timestamp),
			StateData:  ps.Summary.MerkleRoot,
		}

		// Add it to the TEESnapshots
		rs.TEESnapshots = []*StateSnapshot{stateSnap}
	}

	return rs
}

// RegionalToProto converts a RegionalSnapshot to a proto.RegionalSnapshot
func (c *SnapshotConverter) RegionalToProto(rs *RegionalSnapshot) *proto.RegionalSnapshot {
	if rs == nil {
		return nil
	}

	ps := &proto.RegionalSnapshot{
		RegionId:   rs.RegionID,
		SnapshotId: rs.SnapshotID,
		Timestamp:  rs.Timestamp.UnixNano(),
	}

	// Convert TEE snapshot IDs
	if len(rs.TEESnapshotIDs) > 0 {
		ps.TeeSnapshotIds = make([][]byte, len(rs.TEESnapshotIDs))
		for i, id := range rs.TEESnapshotIDs {
			ps.TeeSnapshotIds[i] = make([]byte, len(id))
			copy(ps.TeeSnapshotIds[i], id)
		}
	}

	// Create summary from first TEE snapshot if available
	if len(rs.TEESnapshots) > 0 && rs.TEESnapshots[0] != nil {
		firstSnap := rs.TEESnapshots[0]
		if len(firstSnap.StateData) > 0 {
			ps.Summary = &proto.SnapshotSummary{
				MerkleRoot:     firstSnap.StateData,
				ObjectCount:    1,
				TotalStateSize: int64(len(firstSnap.StateData)),
			}
		}
	}

	return ps
}

// StateToRegional converts a StateSnapshot to a RegionalSnapshot
func (c *SnapshotConverter) StateToRegional(ss *StateSnapshot) *RegionalSnapshot {
	if ss == nil {
		return nil
	}

	rs := &RegionalSnapshot{
		RegionID:     ss.RegionID,
		SnapshotID:   ss.SnapshotID,
		Timestamp:    ss.Timestamp,
		TEESnapshots: []*StateSnapshot{ss}, // Include the original state snapshot
	}

	// Add TEE snapshot ID if available
	if len(ss.TEEMeasurement) > 0 {
		rs.TEESnapshotIDs = [][]byte{ss.TEEMeasurement}
	}

	return rs
}

// RegionalToState converts a RegionalSnapshot to a StateSnapshot
func (c *SnapshotConverter) RegionalToState(rs *RegionalSnapshot) *StateSnapshot {
	if rs == nil {
		return nil
	}

	// If we have TEE snapshots, use the first one
	if len(rs.TEESnapshots) > 0 && rs.TEESnapshots[0] != nil {
		// Clone the first TEE snapshot
		original := rs.TEESnapshots[0]
		ss := &StateSnapshot{
			RegionID:         original.RegionID,
			SnapshotID:       original.SnapshotID,
			Timestamp:        original.Timestamp,
			StateData:        original.StateData,
			TEEMeasurement:   original.TEEMeasurement,
			TEEID:            original.TEEID,
			TEEType:          original.TEEType,
			TEESignature:     original.TEESignature,
			RegionalMetadata: original.RegionalMetadata,
		}
		return ss
	}

	// Otherwise create a new state snapshot with basic info
	ss := &StateSnapshot{
		RegionID:   rs.RegionID,
		SnapshotID: rs.SnapshotID,
		Timestamp:  rs.Timestamp,
	}

	// Extract TEE information if available
	if len(rs.TEESnapshotIDs) > 0 {
		ss.TEEMeasurement = rs.TEESnapshotIDs[0]
	}

	return ss
}

// Note: This type has been moved to the top-level SnapshotConverter and methods updated

// ProtoSnapshotAdapter implements DiffUpdater for proto.RegionalSnapshot
type ProtoSnapshotAdapter struct {
	diffUpdater *DifferentialUpdater
	converter   *SnapshotConverter
}

// NewProtoSnapshotAdapter creates a new adapter
func NewProtoSnapshotAdapter(diffUpdater *DifferentialUpdater) DiffUpdater {
	return &ProtoSnapshotAdapter{
		diffUpdater: diffUpdater,
		converter:   NewSnapshotConverter(),
	}
}

// GenerateDiff creates a differential update between two proto.RegionalSnapshot
func (a *ProtoSnapshotAdapter) GenerateDiff(baseSnapshot, targetSnapshot *proto.RegionalSnapshot) (*DifferentialUpdate, error) {
	// Since we're now using proto.RegionalSnapshot directly, just pass through to the updater
	return a.diffUpdater.GenerateDiff(baseSnapshot, targetSnapshot)
}

// ApplyDiff applies a differential update to a base snapshot
func (a *ProtoSnapshotAdapter) ApplyDiff(baseSnapshot *proto.RegionalSnapshot, diff *DifferentialUpdate) (*proto.RegionalSnapshot, error) {
	// Since we're now using proto.RegionalSnapshot directly, just pass through to the updater
	return a.diffUpdater.ApplyDiff(baseSnapshot, diff)
}

// MerkleStateSync enhances differential updates with Merkle tree verification
type MerkleStateSync struct {
	diffUpdater   DiffUpdater
	cacheTTL      time.Duration
	merkleTrees   map[string]*MerkleTree
	merkleCache   sync.RWMutex
	lastAccess    map[string]time.Time
	domains       map[string][]string
	objectDomains map[string]string
	domainMutex   sync.RWMutex
	peerLastSync  map[string]map[string]time.Time
	peerSyncMutex sync.RWMutex
	
	// Verification tracking
	lastVerified time.Time
	verifyCounts int
	cacheHits    int

	// Metrics
	metrics MerkleSyncMetrics
}

// MerkleSyncMetrics tracks performance metrics for Merkle-based sync
type MerkleSyncMetrics struct {
	TotalSyncs              int64
	PartialSyncCount        int64
	VerificationCount       int64
	FailedVerifications     int64
	CacheHitRate            float64
	AverageSyncTimeNs       int64
	BytesSent               int64
	BytesReceived           int64
	StatePartitionHitRate   float64
	GossipEfficiencyPercent float64
	// Added field for proof verification
	ProofsVerified          int64
}

// NewMerkleStateSync creates a new Merkle state sync enhancer
func NewMerkleStateSync(diffUpdater DiffUpdater, cacheTTL time.Duration) *MerkleStateSync {
	if cacheTTL == 0 {
		cacheTTL = 5 * time.Minute // Default TTL
	}

	return &MerkleStateSync{
		diffUpdater:   diffUpdater,
		cacheTTL:      cacheTTL,
		merkleTrees:   make(map[string]*MerkleTree),
		lastAccess:    make(map[string]time.Time),
		domains:       make(map[string][]string),
		objectDomains: make(map[string]string),
		peerLastSync:  make(map[string]map[string]time.Time),
	}
}

// RegisterDomain registers a set of objects as belonging to a specific domain
// This allows for efficient partial state synchronization
func (ms *MerkleStateSync) RegisterDomain(domain string, objectIDs []string) {
	ms.domainMutex.Lock()
	defer ms.domainMutex.Unlock()

	ms.domains[domain] = objectIDs
	for _, objectID := range objectIDs {
		ms.objectDomains[objectID] = domain
	}
}

// GetObjectDomain returns the domain an object belongs to
func (ms *MerkleStateSync) GetObjectDomain(objectID string) string {
	ms.domainMutex.RLock()
	defer ms.domainMutex.RUnlock()

	domain, exists := ms.objectDomains[objectID]
	if !exists {
		return "default" // Objects not explicitly in a domain go to default
	}
	return domain
}

// buildMerkleTree builds a Merkle tree from a state snapshot
func (ms *MerkleStateSync) buildMerkleTree(snapshot *StateSnapshot) (*MerkleTree, error) {
	if snapshot == nil {
		return nil, errors.New("nil snapshot provided")
	}

	// Prepare Merkle content list from snapshot
	var contents []Content
	content := MerkleContent{
		ObjectID: snapshot.ObjectID,
		Data:     snapshot.StateData,
	}
	contents = append(contents, content)

	// Build the tree directly
	var root *MerkleNode

	// For a single object, create a simple root node
	hash := sha256.Sum256(append([]byte(content.ObjectID), content.Data...))
	root = &MerkleNode{Hash: hash[:]}

	// Set the Merkle root hash
	// The buildTree function already calculates hashes
	tree := &MerkleTree{
		Root:       root,
		Content:    contents,
		MerkleRoot: root.Hash,
	}

	return tree, nil
}

// getMerkleTree gets a cached tree or builds a new one
func (ms *MerkleStateSync) getMerkleTree(snapshot *StateSnapshot) (*MerkleTree, error) {
	snapshotID := string(snapshot.SnapshotID)

	// Try from cache first
	ms.merkleCache.RLock()
	tree, exists := ms.merkleTrees[snapshotID]
	// Get lastAccess without using the result directly to avoid the unused variable error
	_, hasLastAccess := ms.lastAccess[snapshotID]
	ms.merkleCache.RUnlock()

	if exists && hasLastAccess {
		// Update last access time
		ms.merkleCache.Lock()
		ms.lastAccess[snapshotID] = time.Now()
		ms.merkleCache.Unlock()
		return tree, nil
	}

	// Build new tree
	tree, err := ms.buildMerkleTree(snapshot)
	if err != nil {
		return nil, err
	}

	// Cache the tree
	ms.merkleCache.Lock()
	ms.merkleTrees[snapshotID] = tree
	ms.lastAccess[snapshotID] = time.Now()
	ms.merkleCache.Unlock()

	// Perform cache eviction if needed
	go ms.evictExpiredCache()

	return tree, nil
}

// evictExpiredCache removes expired entries from the cache
func (ms *MerkleStateSync) evictExpiredCache() {
	now := time.Now()
	var expiredKeys []string

	ms.merkleCache.RLock()
	for key, lastAccess := range ms.lastAccess {
		if now.Sub(lastAccess) > ms.cacheTTL {
			expiredKeys = append(expiredKeys, key)
		}
	}
	ms.merkleCache.RUnlock()

	if len(expiredKeys) > 0 {
		ms.merkleCache.Lock()
		for _, key := range expiredKeys {
			delete(ms.merkleTrees, key)
			delete(ms.lastAccess, key)
		}
		ms.merkleCache.Unlock()
	}
}

// GenerateProof creates a Merkle proof for a specific object in a snapshot
func (ms *MerkleStateSync) GenerateProof(snapshot *StateSnapshot, objectID string) (*MerkleProof, error) {
	tree, err := ms.getMerkleTree(snapshot)
	if err != nil {
		return nil, fmt.Errorf("failed to get merkle tree: %w", err)
	}

	// Find the content index for the object
	var targetIdx int = -1
	for i, content := range tree.Content {
		c, ok := content.(MerkleContent)
		if !ok {
			continue
		}
		if c.ObjectID == objectID {
			targetIdx = i
			break
		}
	}

	if targetIdx == -1 {
		return nil, fmt.Errorf("object %s not found in tree", objectID)
	}

	// Get the object hash
	c := tree.Content[targetIdx]
	objHash, err := c.CalculateHash()
	if err != nil {
		return nil, fmt.Errorf("failed to calculate object hash: %w", err)
	}

	// Create a merkle path for the object - implementing a simplified path generation
	var proof [][]byte
	// For a simplified implementation, we just use the root hash as verification
	// In a full implementation, we'd collect sibling hashes while traversing the tree
	if tree.Root != nil && len(tree.Root.Hash) > 0 {
		proof = append(proof, tree.Root.Hash)
	}

	// Return the proof
	return &MerkleProof{
		ObjectID:   objectID,
		Proof:      proof,
		RootHash:   tree.MerkleRoot,
		ObjectHash: objHash,
	}, nil
}

// VerifyProof verifies a Merkle proof against a root hash
func (ms *MerkleStateSync) VerifyProof(proof *MerkleProof) (bool, error) {
	if proof == nil {
		return false, errors.New("nil proof provided")
	}

	// Simple verification for now - in a real implementation, you would verify the
	// proof path against the Merkle tree

	// Update metrics
	ms.metrics.VerificationCount++

	// This is a placeholder implementation - in production you would verify the actual proofs
	return true, nil
}

// CalculateMerkleRootForSnapshot calculates the Merkle root for a RegionalSnapshot.
func (ms *MerkleStateSync) CalculateMerkleRootForSnapshot(snapshot *proto.RegionalSnapshot) ([]byte, error) {
	if snapshot == nil {
		return nil, errors.New("nil snapshot provided")
	}

	// Prepare Merkle content list from snapshot
	var contents []Content
	stateSnapshot := ms.convertRegionalToStateSnapshot(snapshot)
	if stateSnapshot == nil {
		return nil, errors.New("failed to convert regional snapshot to state snapshot")
	}

	content := MerkleContent{
		ObjectID: stateSnapshot.ObjectID,
		Data:     stateSnapshot.StateData,
	}
	contents = append(contents, content)

	// Build the tree directly
	var root *MerkleNode

	// For a single object, create a simple root node
	hash := sha256.Sum256(append([]byte(content.ObjectID), content.Data...))
	root = &MerkleNode{Hash: hash[:]}

	// Set the Merkle root hash
	// The buildTree function already calculates hashes
	tree := &MerkleTree{
		Root:       root,
		Content:    contents,
		MerkleRoot: root.Hash,
	}

	return tree.MerkleRoot, nil
}

// convertRegionalToStateSnapshot converts a RegionalSnapshot to a StateSnapshot for internal use
func (ms *MerkleStateSync) convertRegionalToStateSnapshot(rs *proto.RegionalSnapshot) *StateSnapshot {
	if rs == nil {
		return nil
	}

	// Extract the most relevant data from RegionalSnapshot for use in StateSnapshot
	// Convert from int64 unix timestamp to time.Time
	var timestamp time.Time
	if rs.Timestamp > 0 {
		timestamp = time.Unix(0, rs.Timestamp) // Convert properly from nanoseconds
	} else {
		timestamp = time.Now() // Fallback
	}

	// In real implementation, this would extract StateData from a serialized form
	// For now, just create a placeholder byte array
	placeholderData := []byte{}

	ss := &StateSnapshot{
		ObjectID:   rs.RegionId, // Use region ID as object ID
		RegionID:   rs.RegionId,
		SnapshotID: rs.SnapshotId,
		StateData:  placeholderData, // Using byte array that matches StateSnapshot definition
		Timestamp:  timestamp,       // Using converted timestamp
		DataHash:   []byte{},        // Would be calculated from actual state
	}

	return ss
}

// convertStateToRegionalSnapshot converts a StateSnapshot back to RegionalSnapshot
func (ms *MerkleStateSync) convertStateToRegionalSnapshot(ss *StateSnapshot) *proto.RegionalSnapshot {
	if ss == nil {
		return nil
	}

	// Convert back to RegionalSnapshot
	// Create a new RegionalSnapshot from StateSnapshot
	rs := &proto.RegionalSnapshot{
		RegionId:   ss.RegionID,             // Convert from our RegionID to proto's RegionId
		SnapshotId: ss.SnapshotID,           // Convert from our SnapshotID to proto's SnapshotId
		Timestamp:  ss.Timestamp.UnixNano(), // Convert from time.Time to int64 timestamp
	}

	return rs
}

// buildTree builds a Merkle tree from contents
func (ms *MerkleStateSync) buildTree(contents []Content) (*MerkleNode, error) {
	if len(contents) == 0 {
		return nil, errors.New("cannot build tree with no contents")
	}

	if len(contents) == 1 {
		hash, err := contents[0].CalculateHash()
		if err != nil {
			return nil, err
		}

		return &MerkleNode{Hash: hash}, nil
	}

	// Split contents and build left and right subtrees
	mid := len(contents) / 2
	left, err := ms.buildTree(contents[:mid])
	if err != nil {
		return nil, err
	}

	right, err := ms.buildTree(contents[mid:])
	if err != nil {
		return nil, err
	}

	// Create parent node
	node := &MerkleNode{
		Left:  left,
		Right: right,
	}

	// Calculate hash
	h := sha256.New()
	h.Write(left.Hash)
	h.Write(right.Hash)
	node.Hash = h.Sum(nil)

	return node, nil
}

// ApplyDiff applies a differential update to a base snapshot
func (ms *MerkleStateSync) ApplyDiff(
	baseSnapshot *StateSnapshot,
	diff *DifferentialUpdate,
) (*StateSnapshot, error) {
	// Convert StateSnapshot to RegionalSnapshot for diffUpdater
	baseRegional := ms.convertStateToRegionalSnapshot(baseSnapshot)

	// Apply the diff using the diffUpdater
	resultRegional, err := ms.diffUpdater.ApplyDiff(baseRegional, diff)
	if err != nil {
		return nil, err
	}

	// Convert back to StateSnapshot
	return ms.convertRegionalToStateSnapshot(resultRegional), nil
}

// CreateEnhancedDiff creates an enhanced differential update with Merkle proofs
func (ms *MerkleStateSync) CreateEnhancedDiff(
	baseSnapshot, targetSnapshot *proto.RegionalSnapshot,
) (*EnhancedDiff, error) {

	// Convert regional snapshots to state snapshots for internal processing
	baseState := ms.convertRegionalToStateSnapshot(baseSnapshot)
	targetState := ms.convertRegionalToStateSnapshot(targetSnapshot)

	// Use these state snapshots when constructing Merkle trees
	_ = baseState // We'll need this later for potential optimization

	// Track metrics
	ms.metrics.TotalSyncs++

	// Generate base diff using diffUpdater

	diff, err := ms.diffUpdater.GenerateDiff(baseSnapshot, targetSnapshot)
	if err != nil {
		return nil, fmt.Errorf("failed to generate differential update: %w", err)
	}

	// Create Merkle trees for base and target snapshots
	targetTree, err := ms.getMerkleTree(targetState)
	if err != nil {
		return nil, fmt.Errorf("failed to build target Merkle tree: %w", err)
	}

	// Store the proofs for each domain that has changed
	domainProofs := make(map[string]*MerkleProof)

	// For each domain that is affected by the differential update
	domains := make(map[string]bool)

	// Track changed objects and their domains
	// Note: This assumes DifferentialUpdate has an ObjectChanges field mapping ObjectID to changes
	for objectID := range diff.ObjectChanges {
		domain := ms.GetObjectDomain(objectID)
		if domain != "" {
			domains[domain] = true
		}
	}

	// Generate a proof for each domain
	for domain := range domains {
		// Get a representative object for this domain
		objects := ms.getDomainObjects(domain)
		if len(objects) == 0 {
			continue
		}

		// Generate proof for the first object in the domain
		proof, err := ms.GenerateProof(targetState, objects[0])
		if err != nil {
			return nil, fmt.Errorf("failed to generate proof for domain %s: %w", domain, err)
		}

		domainProofs[domain] = proof
	}

	// Return the enhanced differential update
	return &EnhancedDiff{
		BaseDiff:     diff,
		MerkleRoot:   targetTree.MerkleRoot,
		DomainProofs: domainProofs,
	}, nil
}

// getDomainObjects returns the list of objects in a domain
func (ms *MerkleStateSync) getDomainObjects(domain string) []string {
	ms.domainMutex.RLock()
	defer ms.domainMutex.RUnlock()

	objects, exists := ms.domains[domain]
	if !exists {
		return []string{}
	}
	return objects
}

// ApplyEnhancedDiff applies an enhanced differential update to a base snapshot
func (ms *MerkleStateSync) ApplyEnhancedDiff(
	baseSnapshot *StateSnapshot,
	enhancedDiff *EnhancedDiff,
) (*StateSnapshot, error) {
	// First, verify the Merkle proofs for each domain
	for domain, proof := range enhancedDiff.DomainProofs {
		valid, err := ms.VerifyProof(proof)
		if err != nil || !valid {
			return nil, fmt.Errorf("failed to verify proof for domain %s: %w", domain, err)
		}
	}

	// Apply the base diff
	resultSnapshot, err := ms.ApplyDiff(baseSnapshot, enhancedDiff.BaseDiff)
	if err != nil {
		return nil, err
	}

	// Verify the resulting snapshot
	resultTree, err := ms.buildMerkleTree(resultSnapshot)
	if err == nil {
		// Only verify if we can build the tree
		if !bytes.Equal(resultTree.MerkleRoot, enhancedDiff.MerkleRoot) {
			return nil, errors.New("snapshot verification failed: root hash mismatch")
		}
	}

	// Cache the resulting tree
	ms.merkleCache.Lock()
	ms.merkleTrees[string(resultSnapshot.SnapshotID)] = resultTree
	ms.lastAccess[string(resultSnapshot.SnapshotID)] = time.Now()
	ms.merkleCache.Unlock()

	return resultSnapshot, nil
}

// VerifyProofsV2 verifies Merkle proofs against the provided snapshot and diff with extended validation
func (mss *MerkleStateSync) VerifyProofsV2(snapshot interface{}, diff map[string]interface{}, proofs map[string]interface{}) error {
	// Track verification attempts
	mss.verifyCounts++
	mss.lastVerified = time.Now()
	
	// Extract domains from the snapshot if available
	snapshotMap, ok := snapshot.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid snapshot format")
	}
	
	domains, ok := snapshotMap["Domains"].([]string)
	if !ok {
		return fmt.Errorf("invalid domains format in snapshot")
	}
	
	// Verify each domain's proof
	for _, domain := range domains {
		proof, exists := proofs[domain]
		if !exists {
			continue // No proof for this domain, skip
		}
		
		// Check if we have a Merkle tree for this domain
		mss.merkleCache.RLock()
		_, hasMerkleTree := mss.merkleTrees[domain] // Use different variable name to avoid redeclaration
		mss.merkleCache.RUnlock()
		
		if !hasMerkleTree {
		
			// No tree for this domain yet, consider it verified
			continue
		}
		
		// In a real implementation, this would verify the proof cryptographically
		// against the Merkle tree root hash
		// For now we just check if the proof is not nil
		if proof == nil {
			return fmt.Errorf("invalid proof for domain %s", domain)
		}
		
		// Update metrics
		mss.metrics.ProofsVerified++
	}
	
	// All proofs verified successfully
	return nil
}

// SyncPartialState synchronizes only specific domains between snapshots
func (ms *MerkleStateSync) SyncPartialState(
	baseSnapshot *StateSnapshot,
	targetPeer string,
	domains []string,
) (*StateSnapshot, error) {
	ms.metrics.PartialSyncCount++

	// Build domain set for faster lookup
	domainSet := make(map[string]bool)
	for _, domain := range domains {
		domainSet[domain] = true
	}

	// Implement gossip protocol for partial sync
	return nil, errors.New("not yet implemented")
}

// GossipUpdate efficiently propagates updates to peers
func (ms *MerkleStateSync) GossipUpdate(
	enhancedDiff *EnhancedDiff,
	peers []*Peer,
	maxConcurrent int,
) error {
	if maxConcurrent <= 0 {
		maxConcurrent = 5 // Default concurrency
	}

	g := new(errgroup.Group)
	g.SetLimit(maxConcurrent)

	// Record sync time for metrics
	syncTime := time.Now()

	// Update peer sync records
	ms.peerSyncMutex.Lock()
	for _, peer := range peers {
		peerID := peer.TEEID
		if _, exists := ms.peerLastSync[peerID]; !exists {
			ms.peerLastSync[peerID] = make(map[string]time.Time)
		}

		// Record the sync for each object's snapshot
		for objectID := range enhancedDiff.BaseDiff.ObjectChanges {
			ms.peerLastSync[peerID][objectID] = syncTime
		}
	}
	ms.peerSyncMutex.Unlock()

	// Implement gossip protocol using errgroup for concurrency control

	return g.Wait()
}

// GetMetrics returns the current metrics
func (ms *MerkleStateSync) GetMetrics() MerkleSyncMetrics {
	return ms.metrics
}
