// timeserver/types.go
package timeserver

import (
	"context"
	"crypto/ed25519"
	"net/http"
	"time"
)

// TimeServer represents a single time server instance
type TimeServer struct {
    ID        string
    RegionID  string
    PublicKey ed25519.PublicKey
    privKey   ed25519.PrivateKey
    Address   string
    client    *http.Client
    startTime time.Time
}

// SignedTimestamp represents a timestamp signed by a specific server
type SignedTimestamp struct {
    ServerID  string
    Time      time.Time
    Signature []byte
    Nonce     []byte
    RegionID  string
}

// VerificationRequest represents a request for a verified timestamp
type VerificationRequest struct {
    Nonce    []byte
    RegionID string
    TxID     []byte
}

// VerificationResponse represents a server's response to a timestamp request
type VerificationResponse struct {
    Timestamp   *SignedTimestamp
    ServerProof []byte
    Delay      time.Duration
}

// TimestampProof represents proof of a timestamp from a single server
type TimestampProof struct {
    ServerID  string
    Signature []byte
    Delay     time.Duration
}

// VerifiedTimestamp represents a timestamp that has been verified by multiple servers
type VerifiedTimestamp struct {
    Time       time.Time
    Proofs     []*TimestampProof
    RegionID   string
    QuorumSize int
}

// ServerStatus represents the status of a server
type ServerStatus struct {
    ID        string    `json:"id"`
    RegionID  string    `json:"region_id"`
    Uptime    string    `json:"uptime"`
    Timestamp time.Time `json:"timestamp"`
    Status    string    `json:"status"`
}

// TimeVerifier provides methods to verify timestamps
type TimeVerifier struct {
    network *TimeNetwork
}

// NewTimeVerifier creates a new time verifier instance
func NewTimeVerifier() (*TimeVerifier, error) {
    network := NewTimeNetwork(2, 100*time.Millisecond)
    if err := network.Start(); err != nil {
        return nil, err
    }
    
    return &TimeVerifier{
        network: network,
    }, nil
}

// VerifyExecutionTime gets a verified timestamp for the given region
func (tv *TimeVerifier) VerifyExecutionTime(ctx context.Context, regionID string) (*VerifiedTimestamp, error) {
    return tv.network.GetVerifiedTimestamp(ctx, regionID)
}