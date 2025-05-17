package mesh

import (
	"fmt"
	"math/rand"
	"time"
)

// CheckpointRequest is sent to TEE nodes to capture state
type CheckpointRequest struct {
	Type              CheckpointType          `json:"type"`
	PreviousStateHash []byte                  `json:"previous_state_hash,omitempty"`
	IncludeStateData  bool                    `json:"include_state_data"`
	Timestamp         time.Time               `json:"timestamp"`
	MaxSizeBytes      int64                   `json:"max_size_bytes"`
	HighPriority      bool                    `json:"high_priority"`
}

// CheckpointResponse is returned by TEE nodes with state snapshot
type CheckpointResponse struct {
	NodeID            string                  `json:"node_id"`
	StateHash         []byte                  `json:"state_hash"`
	StateData         map[string]interface{}  `json:"state_data,omitempty"`
	DeltaUpdates      []CheckpointStateChange `json:"delta_updates,omitempty"`
	Timestamp         time.Time               `json:"timestamp"`
	Sequence          uint64                  `json:"sequence"`
	IsConsistent      bool                    `json:"is_consistent"`
	Attestation       *AttestationInfo        `json:"attestation,omitempty"`
}

// CheckpointStateChange represents a change to a single state value for checkpoint
type CheckpointStateChange struct {
	Key               string                  `json:"key"`
	Value             interface{}             `json:"value"`
	Operation         string                  `json:"operation"` // "add", "update", "delete"
	Version           uint64                  `json:"version"`
}

// AttestationInfo contains attestation metadata for checkpoints
type AttestationInfo struct {
	QuorumSize        int                     `json:"quorum_size"`
	LatencyMs         float64                 `json:"latency_ms"`
	EnclaveID         []byte                  `json:"enclave_id,omitempty"`
}

// RecoveryRequest is sent to initiate node recovery
type RecoveryRequest struct {
	CheckpointID      string                  `json:"checkpoint_id"`
	StateHash         []byte                  `json:"state_hash"`
	StateData         map[string]interface{}  `json:"state_data"`
	DeltaUpdates      []CheckpointStateChange `json:"delta_updates,omitempty"`
	SourceTEEType     string                  `json:"source_tee_type"`
	TargetTEEType     string                  `json:"target_tee_type"`
	Timestamp         time.Time               `json:"timestamp"`
}

// RecoveryResponse is returned after recovery attempt
type RecoveryResponse struct {
	Success           bool                    `json:"success"`
	NewStateHash      []byte                  `json:"new_state_hash"`
	RecoveryTime      time.Duration           `json:"recovery_time"`
	ErrorMessage      string                  `json:"error_message,omitempty"`
}

// VerificationRequest is used to verify checkpoint across pairs
type VerificationRequest struct {
	CheckpointID      string                  `json:"checkpoint_id"`
	PairID            string                  `json:"pair_id"`
	StateHash         []byte                  `json:"state_hash"`
	Timestamp         time.Time               `json:"timestamp"`
}

// VerificationResponse contains verification results
type VerificationResponse struct {
	CheckpointID      string                  `json:"checkpoint_id"`
	VerificationPairID string                 `json:"verification_pair_id"`
	Verified          bool                    `json:"verified"`
	Signature         []byte                  `json:"signature,omitempty"`
	ErrorMessage      string                  `json:"error_message,omitempty"`
}

// CheckpointStats contains statistics for a checkpoint
type CheckpointStats struct {
	CheckpointID      string                  `json:"checkpoint_id"`
	CreationTimeMs    int64                   `json:"creation_time_ms"`
	CheckpointSizeBytes int64                 `json:"checkpoint_size_bytes"`
	StateEntryCount   int                     `json:"state_entry_count"`
	DeltaEntryCount   int                     `json:"delta_entry_count,omitempty"`
	CompressionRatio  float64                 `json:"compression_ratio,omitempty"`
}

// Helper functions for checkpoint type strings

// checkpointTypeToString converts CheckpointType to string
func checkpointTypeToString(cpType CheckpointType) string {
	switch cpType {
	case CheckpointTypeFull:
		return "Full"
	case CheckpointTypeDelta:
		return "Delta"
	case CheckpointTypeRecovery:
		return "Recovery"
	default:
		return "Unknown"
	}
}

// checkpointStatusToString converts CheckpointStatus to string
func checkpointStatusToString(status CheckpointStatus) string {
	switch status {
	case CheckpointStatusPending:
		return "Pending"
	case CheckpointStatusComplete:
		return "Complete"
	case CheckpointStatusVerified:
		return "Verified"
	case CheckpointStatusFailed:
		return "Failed"
	default:
		return "Unknown"
	}
}

// estimateCheckpointSize estimates the size of a checkpoint in bytes
func estimateCheckpointSize(snapshot *PairSnapshot) int64 {
	if snapshot == nil {
		return 0
	}
	
	// Rough estimation - this would ideally be implemented with more accurate
	// size calculation based on actual data structures
	var totalSize int64 = 1024 // Base size for metadata
	
	// Add SGX data size
	if snapshot.SGXSnapshot != nil && snapshot.SGXSnapshot.StateData != nil {
		// Estimate state data size based on number of entries
		totalSize += int64(len(snapshot.SGXSnapshot.StateData) * 256)
	}
	
	// Add SEV data size
	if snapshot.SEVSnapshot != nil && snapshot.SEVSnapshot.StateData != nil {
		totalSize += int64(len(snapshot.SEVSnapshot.StateData) * 256)
	}
	
	return totalSize
}

// generateCheckpointID creates a unique ID for checkpoints
func generateCheckpointID(pairID string, cpType CheckpointType, timestamp time.Time) string {
	typeStr := checkpointTypeToString(cpType)
	timeStr := timestamp.Format("20060102-150405")
	return fmt.Sprintf("%s-%s-%s-%s", pairID, typeStr, timeStr, generateRandomString(6))
}

// generateRandomString creates a random string of specified length
func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, length)
	
	// Use crypto/rand in a real implementation
	for i := range result {
		result[i] = charset[rand.Intn(len(charset))]
	}
	
	return string(result)
}
