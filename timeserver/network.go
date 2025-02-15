package timeserver

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"
)

// TimeNetwork manages a network of time servers
type TimeNetwork struct {
	servers       map[string][]*TimeServer
	quorumSize    int
	maxLatency    time.Duration
	mu            sync.RWMutex
	overlaps      map[string][]string
	done          chan struct{}
	started       bool
	startOnce     sync.Once
}

func NewTimeNetwork(quorumSize int, maxLatency time.Duration) *TimeNetwork {
	return &TimeNetwork{
		servers:    make(map[string][]*TimeServer),
		quorumSize: quorumSize,
		maxLatency: maxLatency,
		overlaps:   make(map[string][]string),
		done:       make(chan struct{}),
	}
}

func (tn *TimeNetwork) AddServer(server *TimeServer) error {
	tn.mu.Lock()
	defer tn.mu.Unlock()

	if server.RegionID == "" {
		return fmt.Errorf("region ID required")
	}

	servers := tn.servers[server.RegionID]
	servers = append(servers, server)
	tn.servers[server.RegionID] = servers

	return nil
}

func (tn *TimeNetwork) GetVerifiedTimestamp(ctx context.Context, regionID string) (*VerifiedTimestamp, error) {
	servers := tn.getRegionalServers(regionID)
	if len(servers) < tn.quorumSize {
		return nil, fmt.Errorf("insufficient servers in region")
	}

	// First check server health
	healthyServers := make([]*TimeServer, 0)
	for _, server := range servers {
		if err := tn.checkServerHealth(ctx, server); err != nil {
			continue // Skip unhealthy servers
		}
		healthyServers = append(healthyServers, server)
	}

	if len(healthyServers) < tn.quorumSize {
		return nil, fmt.Errorf("insufficient healthy servers: got %d, need %d",
			len(healthyServers), tn.quorumSize)
	}

	// Generate nonce for this request
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Create verification request
	req := &VerificationRequest{
		Nonce:    nonce,
		RegionID: regionID,
	}

	// Collect responses from healthy servers
	responses := make(chan *VerificationResponse, len(healthyServers))
	errors := make(chan error, len(healthyServers))

	for _, server := range healthyServers {
		go func(s *TimeServer) {
			resp, err := s.GetTimestamp(ctx, req)
			if err != nil {
				errors <- err
				return
			}
			responses <- resp
		}(server)
	}

	// Wait for responses with timeout
	collected := make([]*VerificationResponse, 0, tn.quorumSize)
	timeout := time.After(tn.maxLatency)

	for i := 0; i < len(healthyServers); i++ {
		select {
		case resp := <-responses:
			if err := tn.verifyResponse(resp, nonce); err != nil {
				continue
			}
			collected = append(collected, resp)
			if len(collected) >= tn.quorumSize {
				return tn.createVerifiedTimestamp(collected)
			}
		case err := <-errors:
			fmt.Printf("Error from server: %v\n", err)
		case <-timeout:
			return nil, fmt.Errorf("timeout waiting for responses")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if len(collected) < tn.quorumSize {
		return nil, fmt.Errorf("insufficient valid responses: got %d, need %d",
			len(collected), tn.quorumSize)
	}

	return tn.createVerifiedTimestamp(collected)
}

func (tn *TimeNetwork) verifyResponse(resp *VerificationResponse, nonce []byte) error {
	server := tn.findServer(resp.Timestamp.ServerID)
	if server == nil {
		return fmt.Errorf("server not found")
	}

	if !tn.verifySignature(server, resp.Timestamp, nonce) {
		return fmt.Errorf("invalid signature")
	}

	return nil
}

func (tn *TimeNetwork) getRegionalServers(regionID string) []*TimeServer {
	tn.mu.RLock()
	defer tn.mu.RUnlock()
	return tn.servers[regionID]
}

func (tn *TimeNetwork) findServer(serverID string) *TimeServer {
	tn.mu.RLock()
	defer tn.mu.RUnlock()

	for _, servers := range tn.servers {
		for _, server := range servers {
			if server.ID == serverID {
				return server
			}
		}
	}
	return nil
}

func (tn *TimeNetwork) verifySignature(server *TimeServer, timestamp *SignedTimestamp, nonce []byte) bool {
	msg := make([]byte, 8+len(nonce))
	binary.BigEndian.PutUint64(msg[:8], uint64(timestamp.Time.UnixNano()))
	copy(msg[8:], nonce)
	return ed25519.Verify(server.PublicKey, msg, timestamp.Signature)
}

func (tn *TimeNetwork) createVerifiedTimestamp(responses []*VerificationResponse) (*VerifiedTimestamp, error) {
	if len(responses) < tn.quorumSize {
		return nil, fmt.Errorf("insufficient responses")
	}

	// Validate responses
	if err := tn.validateResponses(responses); err != nil {
		return nil, err
	}

	// Create proofs
	proofs := make([]*TimestampProof, len(responses))
	for i, resp := range responses {
		proofs[i] = &TimestampProof{
			ServerID:  resp.Timestamp.ServerID,
			Signature: resp.ServerProof,
			Delay:     resp.Delay,
		}
	}

	// Use the median timestamp
	timestamps := make([]time.Time, len(responses))
	for i, resp := range responses {
		timestamps[i] = resp.Timestamp.Time
	}
	sort.Slice(timestamps, func(i, j int) bool {
		return timestamps[i].Before(timestamps[j])
	})
	medianTime := timestamps[len(timestamps)/2]

	return &VerifiedTimestamp{
		Time:       medianTime,
		Proofs:     proofs,
		RegionID:   responses[0].Timestamp.RegionID,
		QuorumSize: tn.quorumSize,
	}, nil
}

func (tn *TimeNetwork) validateResponses(responses []*VerificationResponse) error {
	if len(responses) == 0 {
		return fmt.Errorf("no responses to validate")
	}

	baseTime := responses[0].Timestamp.Time
	for _, resp := range responses[1:] {
		diff := resp.Timestamp.Time.Sub(baseTime)
		if diff < -tn.maxLatency || diff > tn.maxLatency {
			return fmt.Errorf("timestamp deviation exceeds maximum latency")
		}
	}

	return nil
}

func (tn *TimeNetwork) checkServerHealth(ctx context.Context, server *TimeServer) error {
	client := &http.Client{Timeout: tn.maxLatency}
	url := fmt.Sprintf("%s/health", server.Address)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unhealthy server: status %d", resp.StatusCode)
	}

	return nil
}

func (tn *TimeNetwork) StartHealthChecks(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				tn.mu.RLock()
				for regionID, servers := range tn.servers {
					for _, server := range servers {
						ctx := context.Background()
						if err := tn.checkServerHealth(ctx, server); err != nil {
							fmt.Printf("Health check failed for server %s in region %s: %v\n",
								server.ID, regionID, err)
						}
					}
				}
				tn.mu.RUnlock()
			case <-tn.done:
				return
			}
		}
	}()
}

func (tn *TimeNetwork) Start() error {
	var err error
	tn.startOnce.Do(func() {
		if tn.started {
			err = fmt.Errorf("network already started")
			return
		}

		tn.mu.RLock()
		if len(tn.servers) == 0 {
			err = fmt.Errorf("no servers configured")
			tn.mu.RUnlock()
			return
		}
		tn.mu.RUnlock()

		tn.started = true
	})
	return err
}

func (tn *TimeNetwork) Stop() error {
	tn.mu.Lock()
	defer tn.mu.Unlock()

	if !tn.started {
		return fmt.Errorf("network not started")
	}

	close(tn.done)
	tn.started = false
	return nil
}