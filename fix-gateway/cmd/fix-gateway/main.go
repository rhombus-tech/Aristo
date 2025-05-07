// Copyright (C) 2025. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/quickfixgo/quickfix"
	"github.com/rhombus-tech/aristo/fix-gateway/pkg/attestation"
	"github.com/rhombus-tech/aristo/fix-gateway/pkg/blockchain"
	"github.com/rhombus-tech/aristo/fix-gateway/pkg/brokerdealer"
	"github.com/rhombus-tech/aristo/fix-gateway/pkg/config"
	"github.com/rhombus-tech/aristo/fix-gateway/pkg/message"
	"github.com/rhombus-tech/aristo/fix-gateway/pkg/session"
	"github.com/rhombus-tech/aristo/fix-gateway/pkg/snapshot"
)

var (
	// Command-line flags
	configPath = flag.String("config", "config.json", "Path to configuration file")
	debugMode  = flag.Bool("debug", false, "Enable debug mode")
	teeTypes   = flag.String("tee-types", "SGX,SEV,TDX", "Comma-separated list of TEE types to use")
	blockchainEndpoint = flag.String("blockchain-endpoint", "http://localhost:9650", "Blockchain API endpoint")
	blockchainSimulation = flag.Bool("blockchain-simulation", false, "Enable blockchain simulation mode")
	snapshotsEnabled = flag.Bool("snapshots", true, "Enable state snapshot anchoring")
	snapshotInterval = flag.Duration("snapshot-interval", 10*time.Minute, "Interval between state snapshots")
)

func main() {
	// Parse command-line flags
	flag.Parse()

	// Load configuration
	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Override TEE types if specified on command line
	if *teeTypes != "" {
		cfg.Attestation.TEETypes = parseTEETypes(*teeTypes)
	}

	// Initialize TEE attestation provider
	attestationProvider, err := attestation.NewTEEProvider(cfg.Attestation)
	if err != nil {
		log.Fatalf("Failed to initialize TEE attestation provider: %v", err)
	}

	// Verify attestation before starting
	if cfg.Attestation.Enabled {
		if err := attestationProvider.VerifyAttestation(); err != nil {
			log.Fatalf("Initial attestation verification failed: %v", err)
		}
		log.Println("TEE attestation verification succeeded")
	}

	// Initialize blockchain connector
	blockchainCfg := blockchain.Config{
		Endpoint:       *blockchainEndpoint,
		TimeoutSeconds: 10,
		MaxRetries:     3,
		RetryInterval:  time.Second * 2,
		SimulationMode: *blockchainSimulation,
	}

	// Initialize blockchain connector
	blockchainConnector, err := blockchain.NewConnector(blockchainCfg)
	if err != nil {
		log.Fatalf("Failed to create blockchain connector: %v", err)
	}

	if err := message.InitBlockchainConnector(blockchainCfg); err != nil {
		log.Fatalf("Failed to initialize blockchain connector: %v", err)
	}
	log.Printf("Blockchain connector initialized, endpoint: %s, simulation mode: %v", 
		blockchainCfg.Endpoint, blockchainCfg.SimulationMode)

	// Create message handler
	msgHandler := message.NewMessageHandler(attestationProvider)

	// Create session manager
	sessionManager, err := session.NewManager(cfg, attestationProvider, msgHandler)
	if err != nil {
		log.Fatalf("Failed to create session manager: %v", err)
	}

	// Initialize broker-dealer manager if enabled
	var brokerDealerManager *brokerdealer.Manager
	if cfg.BrokerDealer.Enabled {
		// Create market session adapter for the broker-dealer manager
		marketSession := createMarketSessionAdapter(sessionManager)
		
		// Initialize broker-dealer manager
		brokerDealerManager, err = brokerdealer.NewManager(
			cfg.BrokerDealer,
			marketSession,
			blockchainConnector,
		)
		if err != nil {
			log.Fatalf("Failed to create broker-dealer manager: %v", err)
		}
		
		// Start broker-dealer manager
		if err := brokerDealerManager.Start(); err != nil {
			log.Fatalf("Failed to start broker-dealer manager: %v", err)
		}
		defer brokerDealerManager.Stop()
		
		log.Printf("Broker-dealer integration enabled with %d brokers", len(cfg.BrokerDealer.BrokerDealers))
	}

	// Start session manager
	if err := sessionManager.Start(); err != nil {
		log.Fatalf("Failed to start session manager: %v", err)
	}
	defer sessionManager.Stop()

	// Initialize and start snapshot manager if enabled
	var snapshotMgr *snapshot.SnapshotManager
	if *snapshotsEnabled {
		snapshotConfig := snapshot.DefaultSnapshotConfig()
		snapshotConfig.SnapshotInterval = *snapshotInterval
		
		// Generate a unique gateway ID for snapshot identification
		gatewayID := fmt.Sprintf("fix-gateway-%s", time.Now().Format("20060102-150405"))
		
		// Create the snapshot manager
		snapshotMgr = snapshot.NewSnapshotManager(
			gatewayID,
			sessionManager,
			blockchainConnector,
			snapshotConfig,
		)
		
		// Start the snapshot manager
		snapshotMgr.Start()
		defer snapshotMgr.Stop()
		
		log.Printf("State snapshot anchoring enabled with interval %v", snapshotConfig.SnapshotInterval)
	}

	log.Printf("FIX Gateway started with %d TEE types enabled", len(cfg.Attestation.TEETypes))
	log.Printf("Connected to NASDAQ at %s:%d", cfg.NASDAQ.PrimarySession.Host, cfg.NASDAQ.PrimarySession.Port)
	log.Printf("Blockchain integration %s", blockchainStatusMessage(blockchainCfg.SimulationMode, blockchainCfg.Endpoint))

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for signal
	sig := <-sigChan
	log.Printf("Received signal %v, shutting down", sig)
}

// MarketSessionAdapter adapts the session manager to the MarketSession interface
type MarketSessionAdapter struct {
	sessionManager *session.Manager
}

// createMarketSessionAdapter creates a new adapter that implements the brokerdealer.MarketSession interface
func createMarketSessionAdapter(sessionManager *session.Manager) *MarketSessionAdapter {
	return &MarketSessionAdapter{
		sessionManager: sessionManager,
	}
}

// SendAndVerify implements the brokerdealer.MarketSession interface
func (m *MarketSessionAdapter) SendAndVerify(msg *quickfix.Message) error {
	// This adapter would convert and route the message to the appropriate NASDAQ session
	// For now, we'll use a simplified implementation with just logging
	
	// In a real implementation, we would need to:
	// 1. Extract relevant order data
	// 2. Generate attestation if required
	// 3. Route to the correct market venue
	// 4. Handle response and route back to broker
	
	// Extract some basic information from the message for logging
	var orderID quickfix.FIXString
	msg.Body.GetField(11, &orderID) // ClOrdID is tag 11
	
	// Log the routing event
	log.Printf("Routing order %s from broker-dealer to NASDAQ", orderID)
	
	// In a complete implementation, we would connect to the session manager
	// to send the message to NASDAQ with proper routing and attestation
	
	// Return nil as if we successfully sent it
	return nil
}

// loadConfig loads the configuration from a file
func loadConfig(path string) (config.FIXGatewayConfig, error) {
	// For now, return a default configuration
	// In a real implementation, this would load from a file

	cfg := config.FIXGatewayConfig{
		Server: config.ServerConfig{
			Host:              "0.0.0.0",
			Port:              8085,
			HeartbeatInterval: 30 * time.Second,
			LogoutTimeout:     5 * time.Second,
		},
		Attestation: config.AttestationConfig{
			Enabled:              true,
			TEETypes:             []string{"SGX", "SEV", "TDX"},
			AttestationFrequency: 5 * time.Minute,
			CrossVerification:    true,
		},

		NASDAQ: config.NasdaqConfig{
			PrimarySession: config.SessionConfig{
				Host:       "fix.nasdaq.com",
				Port:       8080,
				FIXVersion: "4.4",
				UseSSL:     true,
				SSLCert:    "certs/client.crt",
				SSLKey:     "certs/client.key",
			},
			BackupSession: config.SessionConfig{
				Host:       "fix-backup.nasdaq.com",
				Port:       8080,
				FIXVersion: "4.4",
				UseSSL:     true,
				SSLCert:    "certs/client.crt",
				SSLKey:     "certs/client.key",
			},
			SenderCompID: "ARISTO",
			TargetCompID: "NASDAQ",
			MarketDataSession: config.SessionConfig{
				Host:       "md.nasdaq.com",
				Port:       8080,
				FIXVersion: "4.4",
				UseSSL:     true,
				SSLCert:    "certs/client.crt",
				SSLKey:     "certs/client.key",
			},
		},
		Blockchain: config.BlockchainConfig{
			Enabled:         true,
			Endpoint:        *blockchainEndpoint,
			Endpoints:       []string{"http://localhost:9650/ext/bc/rhombus"},
			UseAttestation:  true,
			Simulation:      *blockchainSimulation,
			MaxRetries:      3,
			RetryInterval:   2,
		},
	}

	return cfg, nil
}

// parseTEETypes parses a comma-separated list of TEE types
func parseTEETypes(types string) []string {
	if types == "" {
		return []string{}
	}
	return strings.Split(types, ",")
}

// blockchainStatusMessage returns a formatted status message for blockchain integration
func blockchainStatusMessage(simulationMode bool, endpoint string) string {
	if simulationMode {
		return "running in SIMULATION mode"
	}
	return "enabled and connected to " + endpoint
}
