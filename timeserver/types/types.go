package types

import (
	"context"
	"crypto/ed25519"
	"net/http"
	"time"

	"github.com/rhombus-tech/vm/timeserver/consensus"
)

// TimeServer represents a single time server instance
type TimeServer interface {
	GetTimestamp(ctx context.Context, req *VerificationRequest) (*VerificationResponse, error)
	VerifySignature(timestamp *SignedTimestamp) error
	Start() error
	Stop() error
	GetServerID() string
	GetRegionID() string
	GetAddress() string
	GetPublicKey() ed25519.PublicKey
}

// TimeServerData contains the server data
type TimeServerData struct {
	ServerID  string
	RegionID  string
	Address   string
	PublicKey ed25519.PublicKey
	PrivKey   ed25519.PrivateKey
	Client    *http.Client
	StartTime time.Time
}

// VerificationRequest represents a request for a timestamp
type VerificationRequest struct {
	Nonce    []byte
	RegionID string
	TxID     []byte
}

// SignedTimestamp represents a timestamp signed by a server
type SignedTimestamp struct {
	ServerID  string
	RegionID  string
	Time      time.Time
	Signature []byte
	Nonce     []byte
}

// VerificationResponse represents a response containing a signed timestamp
type VerificationResponse struct {
	Timestamp   *SignedTimestamp
	ServerProof []byte
	Delay       time.Duration
}

// VerifiedTimestamp represents a timestamp that has been verified by multiple servers
type VerifiedTimestamp struct {
	Time       time.Time
	RegionID   string
	ServerIDs  []string
	Signatures [][]byte
	Proofs     []*consensus.ServerProof
}

// TimeVerifier provides methods to verify timestamps
type TimeVerifier interface {
	VerifyExecutionTime(ctx context.Context, regionID string) (*VerifiedTimestamp, error)
}
