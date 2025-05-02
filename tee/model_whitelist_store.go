// Package tee provides integration with Trusted Execution Environments
package tee

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ModelWhitelistStore provides persistent storage for AI model measurements
// with thread-safe operations and robust error handling
type ModelWhitelistStore struct {
	filePath    string        // Path to the whitelist JSON file
	auditPath   string        // Path to the audit log JSON file
	cacheMutex  sync.RWMutex  // Mutex for cache access
	cache       map[[48]byte]*AIModelPolicy
	initialized bool
}

// Global singleton instance with thread-safe initialization
var (
	whitelistStore     *ModelWhitelistStore
	whitelistStoreMu   sync.Mutex
	whitelistStoreOnce sync.Once
)

// AuditLogEntry represents a single entry in the audit log
type AuditLogEntry struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	ModelID   string    `json:"model_id"`
	Action    string    `json:"action"`
	UserID    string    `json:"user_id"`
	Details   string    `json:"details"`
}

// WhitelistData represents the full whitelist data structure for serialization
type WhitelistData struct {
	Policies []*AIModelPolicy `json:"policies"`
	Updated  time.Time        `json:"updated"`
}

// AuditLogData represents the full audit log data structure for serialization
type AuditLogData struct {
	Entries []AuditLogEntry `json:"entries"`
	NextID  int64           `json:"next_id"`
}

// GetModelWhitelistStore returns the singleton instance of the whitelist store
func GetModelWhitelistStore() (*ModelWhitelistStore, error) {
	whitelistStoreOnce.Do(func() {
		whitelistStoreMu.Lock()
		defer whitelistStoreMu.Unlock()
		
		// Determine file paths from environment or use defaults
		dataDir := os.Getenv("AI_MODEL_WHITELIST_DIR")
		if dataDir == "" {
			// Default to a standard location
			dataDir = "/etc/tdx/whitelist"
			
			// For development, use a local directory if the default doesn't exist
			if _, err := os.Stat(dataDir); os.IsNotExist(err) {
				dataDir = "whitelist"
			}
		}
		
		// Create the directory if it doesn't exist
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			fmt.Printf("Warning: Failed to create whitelist directory: %v\n", err)
			whitelistStore = &ModelWhitelistStore{
				cache: make(map[[48]byte]*AIModelPolicy),
			}
			return
		}
		
		filePath := filepath.Join(dataDir, "model_whitelist.json")
		auditPath := filepath.Join(dataDir, "audit_log.json")
		
		store := &ModelWhitelistStore{
			filePath:  filePath,
			auditPath: auditPath,
			cache:     make(map[[48]byte]*AIModelPolicy),
		}
		
		// Initialize the store
		if err := store.initStore(); err != nil {
			fmt.Printf("Warning: Failed to initialize whitelist store: %v\n", err)
			whitelistStore = &ModelWhitelistStore{
				filePath:  filePath,
				auditPath: auditPath,
				cache:     make(map[[48]byte]*AIModelPolicy),
			}
			return
		}
		
		whitelistStore = store
	})
	
	// If no file path is set, we're in fallback mode
	if whitelistStore.filePath == "" {
		return whitelistStore, fmt.Errorf("whitelist store operating in memory-only mode")
	}
	
	return whitelistStore, nil
}

// initStore initializes the whitelist store
func (s *ModelWhitelistStore) initStore() error {
	// Check if whitelist file exists
	if _, err := os.Stat(s.filePath); os.IsNotExist(err) {
		// Create with example models
		if err := s.addExampleModels(); err != nil {
			return fmt.Errorf("failed to add example models: %w", err)
		}
	} else if err == nil {
		// Load existing whitelist
		if err := s.loadWhitelist(); err != nil {
			return fmt.Errorf("failed to load whitelist: %w", err)
		}
	} else {
		return fmt.Errorf("failed to check whitelist file: %w", err)
	}
	
	// Check if audit log file exists
	if _, err := os.Stat(s.auditPath); os.IsNotExist(err) {
		// Create empty audit log
		emptyLog := AuditLogData{
			Entries: []AuditLogEntry{},
			NextID:  1,
		}
		if err := s.writeJSONFile(s.auditPath, emptyLog); err != nil {
			return fmt.Errorf("failed to create audit log: %w", err)
		}
	}
	
	s.initialized = true
	return nil
}

// loadWhitelist loads policies from the whitelist file
func (s *ModelWhitelistStore) loadWhitelist() error {
	data, err := ioutil.ReadFile(s.filePath)
	if err != nil {
		return fmt.Errorf("failed to read whitelist file: %w", err)
	}
	
	var whitelistData WhitelistData
	if err := json.Unmarshal(data, &whitelistData); err != nil {
		return fmt.Errorf("failed to parse whitelist file: %w", err)
	}
	
	s.cacheMutex.Lock()
	defer s.cacheMutex.Unlock()
	
	// Clear the cache
	s.cache = make(map[[48]byte]*AIModelPolicy)
	
	// Add policies to cache
	for _, policy := range whitelistData.Policies {
		s.cache[policy.Measurement] = policy
	}
	
	return nil
}

// saveWhitelist saves policies to the whitelist file
func (s *ModelWhitelistStore) saveWhitelist() error {
	s.cacheMutex.RLock()
	defer s.cacheMutex.RUnlock()
	
	// Convert cache to slice
	policies := make([]*AIModelPolicy, 0, len(s.cache))
	for _, policy := range s.cache {
		policies = append(policies, policy)
	}
	
	// Create whitelist data
	whitelistData := WhitelistData{
		Policies: policies,
		Updated:  time.Now(),
	}
	
	// Write to file
	return s.writeJSONFile(s.filePath, whitelistData)
}

// writeJSONFile writes data to a JSON file
func (s *ModelWhitelistStore) writeJSONFile(filePath string, data interface{}) error {
	// Create temporary file
	tempFile, err := ioutil.TempFile(filepath.Dir(filePath), "temp-*.json")
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath) // Clean up in case of error
	
	// Marshal data with indentation for readability
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal data: %w", err)
	}
	
	// Write to temporary file
	if err := ioutil.WriteFile(tempPath, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write temporary file: %w", err)
	}
	
	// Rename temporary file to target (atomic operation)
	if err := os.Rename(tempPath, filePath); err != nil {
		return fmt.Errorf("failed to rename temporary file: %w", err)
	}
	
	return nil
}

// addExampleModels adds example models to the whitelist
func (s *ModelWhitelistStore) addExampleModels() error {
	// Define example policies
	policies := []*AIModelPolicy{
		{
			ID:              "model-conservative-v1",
			Measurement:     [48]byte{0x1, 0x2, 0x3, 0x4}, // Example measurement
			Approved:        true,
			MaxOrderSize:    100000, // $100,000 max order
			MaxTradesPerMin: 5,      // 5 trades per minute max
			MinHoldingTime:  300,    // 5 minute minimum holding time
			AllowedAssets:   []string{"BTC", "ETH", "AAPL", "MSFT", "GOOG"},
			RiskLevel:       3,
			VerifyMode:      "both", // Use both accumulator and QVL verification
			Description:     "Conservative trading strategy with volatility controls",
			ApprovedBy:      "Regulatory Compliance Board",
			ApprovedAt:      time.Now().Add(-30 * 24 * time.Hour), // 30 days ago
			ExpiresAt:       time.Now().Add(335 * 24 * time.Hour), // 335 days from now
		},
		{
			ID:              "model-aggressive-v1",
			Measurement:     [48]byte{0x5, 0x6, 0x7, 0x8}, // Example measurement
			Approved:        true,
			MaxOrderSize:    50000,  // $50,000 max order
			MaxTradesPerMin: 20,     // 20 trades per minute max
			MinHoldingTime:  60,     // 1 minute minimum holding time
			AllowedAssets:   []string{"BTC", "ETH", "SOL", "AAPL", "MSFT", "GOOG", "AMZN", "TSLA"},
			RiskLevel:       7,
			VerifyMode:      "accumulator", // Use only accumulator for fastest verification
			Description:     "Aggressive trading strategy for higher-risk portfolios",
			ApprovedBy:      "Regulatory Compliance Board",
			ApprovedAt:      time.Now().Add(-15 * 24 * time.Hour), // 15 days ago
			ExpiresAt:       time.Now().Add(350 * 24 * time.Hour), // 350 days from now
		},
		{
			ID:              "model-market-maker-v1",
			Measurement:     [48]byte{0x9, 0xa, 0xb, 0xc}, // Example measurement
			Approved:        true,
			MaxOrderSize:    1000000, // $1M max order
			MaxTradesPerMin: 100,     // 100 trades per minute max
			MinHoldingTime:  0,       // No minimum holding time for market making
			AllowedAssets:   []string{"BTC", "ETH", "SOL", "AAPL", "MSFT", "GOOG", "AMZN"},
			RiskLevel:       5,
			VerifyMode:      "accumulator", // Use only accumulator for fastest verification
			Description:     "Market making strategy with high-frequency trading patterns",
			ApprovedBy:      "Regulatory Compliance Board",
			ApprovedAt:      time.Now().Add(-5 * 24 * time.Hour), // 5 days ago
			ExpiresAt:       time.Now().Add(360 * 24 * time.Hour), // 360 days from now
		},
		{
			ID:              "wasi-model-basic-v1",
			Measurement:     [48]byte{0xd, 0xe, 0xf, 0x1, 0x2}, // Example WASI measurement
			Approved:        true,
			MaxOrderSize:    25000,  // $25,000 max order
			MaxTradesPerMin: 10,     // 10 trades per minute max
			MinHoldingTime:  120,    // 2 minute minimum holding time
			AllowedAssets:   []string{"BTC", "ETH", "AAPL", "MSFT"},
			RiskLevel:       4,
			VerifyMode:      "both", // Use both verification methods for WASI modules
			Description:     "WASI-based basic trading model",
			ApprovedBy:      "Regulatory Compliance Board",
			ApprovedAt:      time.Now().Add(-2 * 24 * time.Hour), // 2 days ago
			ExpiresAt:       time.Now().Add(180 * 24 * time.Hour), // 180 days from now
		},
	}
	
	// Add to cache
	s.cacheMutex.Lock()
	for _, policy := range policies {
		s.cache[policy.Measurement] = policy
	}
	s.cacheMutex.Unlock()
	
	// Save to file
	if err := s.saveWhitelist(); err != nil {
		return fmt.Errorf("failed to save whitelist: %w", err)
	}
	
	// Add audit log entries
	for _, policy := range policies {
		if err := s.logAuditEntry(AuditLogEntry{
			ID:        0, // Will be assigned by logAuditEntry
			Timestamp: time.Now(),
			ModelID:   policy.ID,
			Action:    "created",
			UserID:    "system",
			Details:   "Initial model creation",
		}); err != nil {
			return fmt.Errorf("failed to log audit entry: %w", err)
		}
	}
	
	return nil
}

// logAuditEntry adds an entry to the audit log
func (s *ModelWhitelistStore) logAuditEntry(entry AuditLogEntry) error {
	// Read existing audit log
	data, err := ioutil.ReadFile(s.auditPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Create new audit log
			auditLog := AuditLogData{
				Entries: []AuditLogEntry{},
				NextID:  1,
			}
			data, _ = json.Marshal(auditLog)
		} else {
			return fmt.Errorf("failed to read audit log: %w", err)
		}
	}
	
	var auditLog AuditLogData
	if err := json.Unmarshal(data, &auditLog); err != nil {
		return fmt.Errorf("failed to parse audit log: %w", err)
	}
	
	// Assign ID
	entry.ID = auditLog.NextID
	auditLog.NextID++
	
	// Add entry
	auditLog.Entries = append(auditLog.Entries, entry)
	
	// Save to file
	return s.writeJSONFile(s.auditPath, auditLog)
}

// GetPolicy retrieves the policy for a given TDX measurement
func (s *ModelWhitelistStore) GetPolicy(measurement []byte) (*AIModelPolicy, error) {
	// Validate measurement
	if len(measurement) != 48 {
		return nil, fmt.Errorf("invalid measurement length: %d", len(measurement))
	}
	
	// Convert to [48]byte for map lookup
	var measArray [48]byte
	copy(measArray[:], measurement)
	
	// Check cache first (with read lock)
	s.cacheMutex.RLock()
	policy, exists := s.cache[measArray]
	s.cacheMutex.RUnlock()
	
	if exists {
		// Check if policy is still valid
		if time.Now().After(policy.ExpiresAt) {
			// Expired, remove from cache
			s.cacheMutex.Lock()
			delete(s.cache, measArray)
			s.cacheMutex.Unlock()
			
			// Save changes
			if err := s.saveWhitelist(); err != nil {
				fmt.Printf("Warning: Failed to save whitelist after expiry: %v\n", err)
			}
			
			return nil, fmt.Errorf("model approval expired at %v", policy.ExpiresAt)
		}
		
		// Valid policy
		return policy, nil
	}
	
	// Not in cache or expired, reload from file in case it was updated
	if err := s.loadWhitelist(); err != nil {
		return nil, fmt.Errorf("failed to reload whitelist: %w", err)
	}
	
	// Try again
	s.cacheMutex.RLock()
	policy, exists = s.cache[measArray]
	s.cacheMutex.RUnlock()
	
	if !exists {
		return nil, fmt.Errorf("measurement not found in whitelist")
	}
	
	// Check expiry
	if time.Now().After(policy.ExpiresAt) {
		// Expired, remove from cache
		s.cacheMutex.Lock()
		delete(s.cache, measArray)
		s.cacheMutex.Unlock()
		
		// Save changes
		if err := s.saveWhitelist(); err != nil {
			fmt.Printf("Warning: Failed to save whitelist after expiry: %v\n", err)
		}
		
		return nil, fmt.Errorf("model approval expired at %v", policy.ExpiresAt)
	}
	
	return policy, nil
}

// GetAllPolicies returns all active policies
func (s *ModelWhitelistStore) GetAllPolicies() ([]*AIModelPolicy, error) {
	// Make sure we have the latest data
	if err := s.loadWhitelist(); err != nil {
		return nil, fmt.Errorf("failed to load whitelist: %w", err)
	}
	
	// Get all policies that are approved and not expired
	s.cacheMutex.RLock()
	defer s.cacheMutex.RUnlock()
	
	policies := make([]*AIModelPolicy, 0, len(s.cache))
	now := time.Now()
	
	for _, policy := range s.cache {
		if policy.Approved && now.Before(policy.ExpiresAt) {
			policies = append(policies, policy)
		}
	}
	
	return policies, nil
}

// AddModelToWhitelist adds a new model to the whitelist
func (s *ModelWhitelistStore) AddModelToWhitelist(policy *AIModelPolicy, userID string) error {
	// Validate parameters
	if policy == nil {
		return fmt.Errorf("policy cannot be nil")
	}
	
	if len(policy.ID) == 0 {
		return fmt.Errorf("model ID cannot be empty")
	}
	
	// Add to cache
	s.cacheMutex.Lock()
	s.cache[policy.Measurement] = policy
	s.cacheMutex.Unlock()
	
	// Save to file
	if err := s.saveWhitelist(); err != nil {
		return fmt.Errorf("failed to save whitelist: %w", err)
	}
	
	// Log audit entry
	details := fmt.Sprintf("Added model with measurement %s",
		hex.EncodeToString(policy.Measurement[:]))
	
	if err := s.logAuditEntry(AuditLogEntry{
		Timestamp: time.Now(),
		ModelID:   policy.ID,
		Action:    "add",
		UserID:    userID,
		Details:   details,
	}); err != nil {
		return fmt.Errorf("failed to log audit entry: %w", err)
	}
	
	return nil
}

// LogModelExecution records an execution in the audit log
func (s *ModelWhitelistStore) LogModelExecution(
	modelID string,
	measurement []byte,
	userID string,
	success bool,
	details string,
) error {
	if s.filePath == "" || s.auditPath == "" {
		return fmt.Errorf("whitelist store not initialized")
	}
	
	action := "executed"
	if !success {
		action = "failed"
	}
	
	// Add measurement hash to details if available
	if len(measurement) > 0 {
		details = fmt.Sprintf("%s (measurement: %s)", 
			details, hex.EncodeToString(measurement[:20]))
	}
	
	return s.logAuditEntry(AuditLogEntry{
		Timestamp: time.Now(),
		ModelID:   modelID,
		Action:    action,
		UserID:    userID,
		Details:   details,
	})
}

// Close closes the whitelist store
// This is a no-op for the file-based implementation but included for interface compatibility
func (s *ModelWhitelistStore) Close() error {
	return nil
}
