package timeserver

import (
    "context"
    "crypto/ed25519"
    "encoding/base64"
    "encoding/binary"
    "encoding/json"
    "fmt"
    "log"
    "net/http"
    "time"
)

// NewTimeServer creates a new time server instance
func NewTimeServer(id string, regionID string, address string) (*TimeServer, error) {
    pub, priv, err := ed25519.GenerateKey(nil)
    if err != nil {
        return nil, fmt.Errorf("failed to generate keys: %w", err)
    }

    return &TimeServer{
        ID:        id,
        RegionID:  regionID,
        PublicKey: pub,
        privKey:   priv,
        Address:   address,
        client:    &http.Client{Timeout: 5 * time.Second},
        startTime: time.Now(),
    }, nil
}

// GetTimestamp gets a timestamp with proof
func (ts *TimeServer) GetTimestamp(ctx context.Context, req *VerificationRequest) (*VerificationResponse, error) {
    log.Printf("[%s] Received timestamp request from region %s", ts.ID, req.RegionID)
    
    if req == nil {
        return nil, fmt.Errorf("nil request")
    }

    // Get current time
    now := time.Now().UTC()

    // Create message to sign
    msg := make([]byte, 8+len(req.Nonce))
    binary.BigEndian.PutUint64(msg[:8], uint64(now.UnixNano()))
    copy(msg[8:], req.Nonce)

    // Sign the message
    sig := ed25519.Sign(ts.privKey, msg)

    // Create timestamp
    timestamp := &SignedTimestamp{
        ServerID:  ts.ID,
        Time:     now,
        Signature: sig,
        Nonce:    req.Nonce,
    }

    response := &VerificationResponse{
        Timestamp:   timestamp,
        ServerProof: sig,
        Delay:      time.Since(now),
    }

    log.Printf("[%s] Generated timestamp %v with delay %v", ts.ID, now, response.Delay)
    return response, nil
}

// Serve starts the time server HTTP endpoint
func (ts *TimeServer) Serve(addr string) error {
    log.Printf("[%s] Starting time server on %s", ts.ID, addr)
    
    mux := http.NewServeMux()
    mux.HandleFunc("/timestamp", ts.handleTimestamp)
    mux.HandleFunc("/health", ts.handleHealth)
    
    return http.ListenAndServe(addr, mux)
}

// handleHealth responds to health check requests
func (ts *TimeServer) handleHealth(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }

    status := struct {
        ID        string    `json:"id"`
        RegionID  string    `json:"region_id"`
        Uptime    string    `json:"uptime"`
        Timestamp time.Time `json:"timestamp"`
        Status    string    `json:"status"`
    }{
        ID:        ts.ID,
        RegionID:  ts.RegionID,
        Uptime:    time.Since(ts.startTime).String(),
        Timestamp: time.Now().UTC(),
        Status:    "healthy",
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(status)
    log.Printf("[%s] Health check: uptime %v", ts.ID, status.Uptime)
}

// handleTimestamp handles HTTP timestamp requests
func (ts *TimeServer) handleTimestamp(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }

    log.Printf("[%s] Received HTTP timestamp request", ts.ID)

    // Get nonce from header
    nonceStr := r.Header.Get("X-Nonce")
    nonce, err := base64.StdEncoding.DecodeString(nonceStr)
    if err != nil {
        log.Printf("[%s] Invalid nonce: %v", ts.ID, err)
        http.Error(w, "invalid nonce", http.StatusBadRequest)
        return
    }

    // Create verification request
    req := &VerificationRequest{
        Nonce:    nonce,
        RegionID: r.Header.Get("X-Region"),
    }

    // Get timestamp
    resp, err := ts.GetTimestamp(r.Context(), req)
    if err != nil {
        log.Printf("[%s] Error generating timestamp: %v", ts.ID, err)
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    // Return response
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}