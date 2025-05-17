package mesh

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// MeshService represents the mesh network service
type MeshService struct {
	mu       sync.RWMutex
	pairs    map[string]*TEEPair
	nodes    map[string]*TEENode
	teeID    string
	teeType  string
	handler  ExecutionHandler
}

// TEEPair represents a pair of TEE nodes (SGX and SEV)
type TEEPair struct {
	ID          string
	SGXNode     string
	SEVNode     string
	SGXEndpoint string
	SEVEndpoint string
}

// TEENode represents a single TEE node in the mesh
type TEENode struct {
	ID       string
	TEEType  string
	Endpoint string
	Healthy  bool
}

// NewMeshService creates a new mesh service with the given configuration
func NewMeshService(config *MeshConfig) (*MeshService, error) {
	if config.TEEID == "" {
		return nil, errors.New("TEEID is required")
	}
	
	if config.TEEType == "" {
		return nil, errors.New("TEEType is required")
	}
	
	if config.RegionID == "" {
		return nil, errors.New("RegionID is required")
	}
	
	if config.Endpoint == "" {
		return nil, errors.New("Endpoint is required")
	}
	
	// Create and initialize the mesh service
	service := &MeshService{
		pairs:    make(map[string]*TEEPair),
		nodes:    make(map[string]*TEENode),
		teeID:    config.TEEID,
		teeType:  config.TEEType,
		handler:  config.Handler,
	}
	
	// Initialize the TEE node for this service
	node := &TEENode{
		ID:       config.TEEID,
		TEEType:  config.TEEType,
		Endpoint: config.Endpoint,
		Healthy:  true,
	}
	
	// Add the node to the service
	service.nodes[config.TEEID] = node
	
	return service, nil
}

// RegisterPair adds a new TEE pair to the mesh
func (m *MeshService) RegisterPair(pairID string, sgxNodeID, sevNodeID, sgxEndpoint, sevEndpoint string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if pair already exists
	if _, exists := m.pairs[pairID]; exists {
		return fmt.Errorf("pair %s already exists", pairID)
	}

	// Create and register pair
	pair := &TEEPair{
		ID:          pairID,
		SGXNode:     sgxNodeID,
		SEVNode:     sevNodeID,
		SGXEndpoint: sgxEndpoint,
		SEVEndpoint: sevEndpoint,
	}
	m.pairs[pairID] = pair

	// Register nodes if they don't exist
	if _, exists := m.nodes[sgxNodeID]; !exists {
		m.nodes[sgxNodeID] = &TEENode{
			ID:       sgxNodeID,
			TEEType:  "SGX",
			Endpoint: sgxEndpoint,
			Healthy:  true,
		}
	}

	if _, exists := m.nodes[sevNodeID]; !exists {
		m.nodes[sevNodeID] = &TEENode{
			ID:       sevNodeID,
			TEEType:  "SEV",
			Endpoint: sevEndpoint,
			Healthy:  true,
		}
	}

	return nil
}

// GetPairEndpoints returns the SGX and SEV endpoints for a pair
func (m *MeshService) GetPairEndpoints(pairID string) (string, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pair, exists := m.pairs[pairID]
	if !exists {
		return "", "", fmt.Errorf("pair %s not found", pairID)
	}

	return pair.SGXEndpoint, pair.SEVEndpoint, nil
}

// GetAllPairs returns all pair IDs in the mesh
func (m *MeshService) GetAllPairs() ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pairIDs := make([]string, 0, len(m.pairs))
	for id := range m.pairs {
		pairIDs = append(pairIDs, id)
	}

	return pairIDs, nil
}

// GetAvailablePairs returns a list of healthy pair IDs
func (m *MeshService) GetAvailablePairs() ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	availablePairs := make([]string, 0)
	for id, pair := range m.pairs {
		// Check if both nodes in the pair are healthy
		sgxNode, sgxExists := m.nodes[pair.SGXNode]
		sevNode, sevExists := m.nodes[pair.SEVNode]

		if sgxExists && sevExists && sgxNode.Healthy && sevNode.Healthy {
			availablePairs = append(availablePairs, id)
		}
	}

	return availablePairs, nil
}

// GetAllNodes returns all node IDs in the mesh
func (m *MeshService) GetAllNodes() ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	nodeIDs := make([]string, 0, len(m.nodes))
	for id := range m.nodes {
		nodeIDs = append(nodeIDs, id)
	}

	return nodeIDs, nil
}

// CheckNodeHealth checks if a node is healthy
func (m *MeshService) CheckNodeHealth(ctx context.Context, nodeID string) (bool, error) {
	m.mu.RLock()
	node, exists := m.nodes[nodeID]
	m.mu.RUnlock()

	if !exists {
		return false, fmt.Errorf("node %s not found", nodeID)
	}

	// In a real implementation, this would perform an actual health check
	// For this demo, we'll just return the current state
	return node.Healthy, nil
}

// GetSGXNodeID returns the SGX node ID for a pair
func (m *MeshService) GetSGXNodeID(pairID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pair, exists := m.pairs[pairID]
	if !exists {
		return ""
	}

	return pair.SGXNode
}

// GetSEVNodeID returns the SEV node ID for a pair
func (m *MeshService) GetSEVNodeID(pairID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pair, exists := m.pairs[pairID]
	if !exists {
		return ""
	}

	return pair.SEVNode
}

// GetPairService returns a service for interacting with a specific pair
func (m *MeshService) GetPairService(pairID string) (*PairService, error) {
	sgxEndpoint, sevEndpoint, err := m.GetPairEndpoints(pairID)
	if err != nil {
		return nil, err
	}

	return &PairService{
		PairID:      pairID,
		SGXEndpoint: sgxEndpoint,
		SEVEndpoint: sevEndpoint,
		meshService: m,
	}, nil
}

// PairService provides operations for a specific TEE pair
type PairService struct {
	PairID      string
	SGXEndpoint string
	SEVEndpoint string
	meshService *MeshService
}

// Execute runs a task on both TEEs in a pair
func (p *PairService) Execute(ctx context.Context, req interface{}) (interface{}, error) {
	// This is a simplified implementation
	// In a real system, this would execute on both TEEs and verify results
	
	// Simulate execution time
	time.Sleep(100 * time.Millisecond)
	
	// Return a simple result
	return map[string]interface{}{
		"success": true,
		"pairID":  p.PairID,
		"time":    time.Now().String(),
	}, nil
}
