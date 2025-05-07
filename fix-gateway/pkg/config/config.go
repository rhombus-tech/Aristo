// Copyright (C) 2025. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"time"
)

// FIXGatewayConfig contains all configuration for the FIX gateway
type FIXGatewayConfig struct {
	// Core configuration
	Server ServerConfig `json:"server"`
	
	// TEE attestation configuration
	Attestation AttestationConfig `json:"attestation"`
	
	// NASDAQ connectivity
	NASDAQ NasdaqConfig `json:"nasdaq"`
	
	// Blockchain connectivity
	Blockchain BlockchainConfig `json:"blockchain"`
	
	// Broker-dealer integration
	BrokerDealer BrokerDealerConfig `json:"brokerDealer"`
}

// ServerConfig contains the server configuration
type ServerConfig struct {
	// Host for the FIX gateway
	Host string `json:"host"`
	
	// Port for the FIX gateway
	Port int `json:"port"`
	
	// HeartbeatInterval for FIX sessions
	HeartbeatInterval time.Duration `json:"heartbeatInterval"`
	
	// LogoutTimeout for FIX sessions
	LogoutTimeout time.Duration `json:"logoutTimeout"`
}

// AttestationConfig contains TEE attestation configuration
type AttestationConfig struct {
	// Enabled indicates if attestation is enabled
	Enabled bool `json:"enabled"`
	
	// TEETypes specifies which TEE types to use for attestation (SGX, SEV, TDX)
	TEETypes []string `json:"teeTypes"`
	
	// AttestationFrequency determines how often to perform attestation
	AttestationFrequency time.Duration `json:"attestationFrequency"`
	
	// CrossVerification enables verification across multiple TEE types
	CrossVerification bool `json:"crossVerification"`
}

// NasdaqConfig contains NASDAQ connectivity configuration
type NasdaqConfig struct {
	// Primary session configuration
	PrimarySession SessionConfig `json:"primarySession"`
	
	// Backup session configuration
	BackupSession SessionConfig `json:"backupSession"`
	
	// SenderCompID used to identify our FIX gateway
	SenderCompID string `json:"senderCompID"`
	
	// TargetCompID used to identify NASDAQ
	TargetCompID string `json:"targetCompID"`
	
	// MarketDataSession for market data
	MarketDataSession SessionConfig `json:"marketDataSession"`
}

// SessionConfig contains configuration for a FIX session
type SessionConfig struct {
	// Host for the session
	Host string `json:"host"`
	
	// Port for the session
	Port int `json:"port"`
	
	// FIXVersion for the session (4.2, 4.4, 5.0SP2)
	FIXVersion string `json:"fixVersion"`
	
	// UseSSL indicates if SSL should be used
	UseSSL bool `json:"useSSL"`
	
	// SSLCert is the path to the SSL certificate
	SSLCert string `json:"sslCert"`
	
	// SSLKey is the path to the SSL key
	SSLKey string `json:"sslKey"`
}

// BlockchainConfig contains configuration for blockchain connectivity
type BlockchainConfig struct {
	// Enabled indicates if blockchain integration is enabled
	Enabled bool `json:"enabled"`
	
	// Endpoint for primary blockchain node
	Endpoint string `json:"endpoint"`
	
	// Endpoints for additional blockchain nodes (for failover)
	Endpoints []string `json:"endpoints"`
	
	// APIKey for blockchain API
	APIKey string `json:"apiKey"`
	
	// UseAttestation indicates if attestation should be used for blockchain transactions
	UseAttestation bool `json:"useAttestation"`
	
	// Simulation indicates if blockchain operations should be simulated (not really submitted)
	Simulation bool `json:"simulation"`
	
	// MaxRetries is the maximum number of times to retry a failed blockchain operation
	MaxRetries int `json:"maxRetries"`
	
	// RetryInterval is the number of seconds to wait between retries
	RetryInterval int `json:"retryInterval"`
}

// BrokerDealerConfig contains configuration for broker-dealer connectivity
type BrokerDealerConfig struct {
	// Enabled indicates if broker-dealer integration is enabled
	Enabled bool `json:"enabled"`
	
	// BrokerDealers is a list of configured broker-dealers
	BrokerDealers []BrokerDealer `json:"brokerDealers"`
	
	// DropCopyEnabled indicates if drop copy service is enabled
	DropCopyEnabled bool `json:"dropCopyEnabled"`
	
	// RateLimitPerBroker is the maximum number of orders per second per broker
	RateLimitPerBroker int `json:"rateLimitPerBroker"`
}

// BrokerDealer contains configuration for a single broker-dealer
type BrokerDealer struct {
	// ID uniquely identifies the broker-dealer
	ID string `json:"id"`
	
	// Name is the broker-dealer's company name
	Name string `json:"name"`
	
	// SenderCompID used to identify the broker-dealer in FIX messages
	SenderCompID string `json:"senderCompID"`
	
	// TargetCompID used to identify our gateway in broker-dealer FIX messages
	TargetCompID string `json:"targetCompID"`
	
	// SessionConfig for FIX connectivity
	SessionConfig SessionConfig `json:"sessionConfig"`
	
	// AllowedAccounts is a list of client accounts this broker is authorized to trade for
	AllowedAccounts []string `json:"allowedAccounts"`
	
	// CommissionRate is the standard commission rate for this broker
	CommissionRate float64 `json:"commissionRate"`
	
	// Active indicates if this broker-dealer is currently active
	Active bool `json:"active"`
}
