// Package mesh provides a mesh network for TEE-to-TEE communication
package mesh

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// BlockchainAnchorData represents the minimal data needed to verify a snapshot
// that will be stored on the blockchain
type BlockchainAnchorData struct {
	SnapshotID      string            `json:"snapshot_id"`      // Hex-encoded snapshot ID
	RegionID        string            `json:"region_id"`        // Region this snapshot represents
	Timestamp       int64             `json:"timestamp"`        // Unix timestamp when snapshot was created
	MerkleRoot      string            `json:"merkle_root"`      // Hex-encoded Merkle root of all state roots
	TEECount        int               `json:"tee_count"`        // Number of TEEs that participated
	ConsensusLevel  float64           `json:"consensus_level"`  // Level of consensus achieved (0.0-1.0)
	PreviousID      string            `json:"previous_id"`      // Hex-encoded previous snapshot ID (for chaining)
	SignaturesCount int               `json:"signatures_count"` // Number of signatures included
	MetadataHash    string            `json:"metadata_hash"`    // Hash of region-specific metadata
	Version         uint64            `json:"version"`          // Version of the snapshot format
}

// BlockchainClient provides methods for anchoring data to the blockchain
type BlockchainClient interface {
	AnchorData(ctx context.Context, data []byte) (string, error)
}

// DefaultBlockchainClient is a basic implementation of the BlockchainClient interface
type DefaultBlockchainClient struct {
	endpoint   string
	httpClient *http.Client
}

// NewDefaultBlockchainClient creates a new blockchain client
func NewDefaultBlockchainClient(endpoint string) *DefaultBlockchainClient {
	return &DefaultBlockchainClient{
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// AnchorData anchors data to the blockchain and returns a transaction ID
func (c *DefaultBlockchainClient) AnchorData(ctx context.Context, data []byte) (string, error) {
	// In a production environment, this would submit the data to a blockchain node
	// For now, we'll simulate the process and return a fake transaction ID
	
	// Simulate blockchain latency
	time.Sleep(100 * time.Millisecond)
	
	// Create a deterministic transaction ID based on the data
	hash := sha256.Sum256(data)
	txID := hex.EncodeToString(hash[:])
	
	return txID, nil
}

// CreateBlockchainAnchorData converts a RegionalSnapshot to the minimal format for blockchain storage
func CreateBlockchainAnchorData(snapshot *RegionalSnapshot) (*BlockchainAnchorData, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("snapshot cannot be nil")
	}
	
	// Extract the consensus information
	consensusLevel := 0.0
	teeCount := 0
	if snapshot.ConsensusInfo != nil {
		consensusLevel = snapshot.ConsensusInfo.ConsensusLevel
		teeCount = snapshot.ConsensusInfo.TEECount
	}
	
	// Extract the Merkle root from summary
	merkleRootHex := ""
	if snapshot.SnapshotSummary != nil && len(snapshot.SnapshotSummary.MerkleRoot) > 0 {
		merkleRootHex = hex.EncodeToString(snapshot.SnapshotSummary.MerkleRoot)
	}
	
	// Hash the metadata for storage
	metadataHash := ""
	if len(snapshot.Metadata) > 0 {
		metadataJSON, err := json.Marshal(snapshot.Metadata)
		if err == nil {
			hash := sha256.Sum256(metadataJSON)
			metadataHash = hex.EncodeToString(hash[:])
		}
	}
	
	// Determine previous snapshot ID if any
	previousID := ""
	if prevID, ok := snapshot.Metadata["previous_snapshot_id"]; ok {
		if prevIDBytes, ok := prevID.([]byte); ok {
			previousID = hex.EncodeToString(prevIDBytes)
		} else if prevIDStr, ok := prevID.(string); ok {
			previousID = prevIDStr
		}
	}
	
	// Calculate signature count
	signaturesCount := 0
	if snapshot.CoordinatorSignature != nil {
		signaturesCount++ 
	}
	if snapshot.VerifierSignatures != nil {
		signaturesCount += len(snapshot.VerifierSignatures)
	}
	
	// Extract version from metadata or default to 1
	version := uint64(1) // Default version
	if versionVal, ok := snapshot.Metadata["version"]; ok {
		if versionInt, ok := versionVal.(uint64); ok {
			version = versionInt
		}
	}
	
	// Create the anchor data
	anchorData := &BlockchainAnchorData{
		SnapshotID:      hex.EncodeToString(snapshot.SnapshotID),
		RegionID:        snapshot.RegionID,
		Timestamp:       snapshot.Timestamp.Unix(),
		MerkleRoot:      merkleRootHex,
		TEECount:        teeCount,
		ConsensusLevel:  consensusLevel,
		PreviousID:      previousID,
		SignaturesCount: signaturesCount,
		MetadataHash:    metadataHash,
		Version:         version,
	}
	
	return anchorData, nil
}
