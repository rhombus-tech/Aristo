// Copyright (C) 2025. All rights reserved.
// See the file LICENSE for licensing terms.

package blockchain

import (
	"fmt"
	"time"
)

// Config holds blockchain integration configuration
type Config struct {
	// The blockchain endpoint URL
	Endpoint string `json:"endpoint"`
	
	// Connection timeout in seconds
	TimeoutSeconds int `json:"timeoutSeconds"`
	
	// Retry configuration
	MaxRetries    int           `json:"maxRetries"`
	RetryInterval time.Duration `json:"retryInterval"`
	
	// Enable simulation mode (no actual blockchain submission)
	SimulationMode bool `json:"simulationMode"`
}

// DefaultConfig returns a default configuration
func DefaultConfig() Config {
	return Config{
		Endpoint:       "http://localhost:9650",
		TimeoutSeconds: 10,
		MaxRetries:     3,
		RetryInterval:  time.Second * 2,
		SimulationMode: false,
	}
}

// Validate checks the configuration for validity
func (c *Config) Validate() error {
	if c.Endpoint == "" {
		return fmt.Errorf("blockchain endpoint URL cannot be empty")
	}
	
	if c.TimeoutSeconds <= 0 {
		return fmt.Errorf("timeout seconds must be greater than 0")
	}
	
	if c.MaxRetries < 0 {
		return fmt.Errorf("max retries cannot be negative")
	}
	
	if c.RetryInterval < 0 {
		return fmt.Errorf("retry interval cannot be negative")
	}
	
	return nil
}

// GetTimeout returns the timeout as a time.Duration
func (c *Config) GetTimeout() time.Duration {
	return time.Duration(c.TimeoutSeconds) * time.Second
}
