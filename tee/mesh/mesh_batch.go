package mesh

import (
	"context"
	"errors"
	"sort"

	"github.com/rhombus-tech/vm/tee/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// BatchDirectExecute processes multiple operations in a single batch
func (m *MeshService) BatchDirectExecute(ctx context.Context, req *proto.BatchDirectExecutionRequest) (*proto.BatchDirectExecutionResponse, error) {
	if m.batchProcessor == nil {
		return nil, status.Error(codes.Internal, "batch processor not initialized")
	}
	return m.batchProcessor.BatchDirectExecute(ctx, req)
}

// BatchProxyExecute handles batch execution with automatic failover
func (m *MeshService) BatchProxyExecute(ctx context.Context, req *proto.BatchProxyExecutionRequest) (*proto.BatchDirectExecutionResponse, error) {
	if m.batchProcessor == nil {
		return nil, status.Error(codes.Internal, "batch processor not initialized")
	}
	return m.batchProcessor.BatchProxyExecute(ctx, req)
}

// GetSuitablePeers returns a list of peers suitable for execution based on criteria
func (m *MeshService) GetSuitablePeers(regionID, preferredTEEType string, excludedPeers []string) []*Peer {
	m.peerMutex.RLock()
	defer m.peerMutex.RUnlock()
	
	// Build a list of peers matching the criteria
	var peers []*Peer
	
	for _, peer := range m.peers {
		// Skip peers in excluded list
		if containsString(excludedPeers, peer.TEEID) {
			continue
		}
		
		// Skip peers in different regions if regionID is specified
		if regionID != "" && peer.RegionID != regionID {
			continue
		}
		
		// Add all peers matching the criteria
		peers = append(peers, peer)
	}
	
	// Sort peers by preferred type and latency
	sort.Slice(peers, func(i, j int) bool {
		// First prefer peers of the requested type
		if preferredTEEType != "" {
			if peers[i].TEEType == preferredTEEType && peers[j].TEEType != preferredTEEType {
				return true
			}
			if peers[i].TEEType != preferredTEEType && peers[j].TEEType == preferredTEEType {
				return false
			}
		}
		
		// Then sort by latency
		return peers[i].AverageLatencyNs < peers[j].AverageLatencyNs
	})
	
	return peers
}

// containsString checks if a string slice contains a specific string
func containsString(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}

// GetBatchProcessorMetrics returns metrics from the batch processor
func (m *MeshService) GetBatchProcessorMetrics() (*BatchProcessorMetrics, error) {
	if m.batchProcessor == nil {
		return nil, errors.New("batch processor not initialized")
	}
	return m.batchProcessor.GetMetrics(), nil
}
