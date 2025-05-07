// Copyright (C) 2025. All rights reserved.
// See the file LICENSE for licensing terms.

package brokerdealer

import (
	"github.com/rhombus-tech/aristo/fix-gateway/pkg/config"
)

// BrokerConfig represents configuration for a single broker-dealer connection
type BrokerConfig struct {
	// ID uniquely identifies this broker-dealer
	ID string

	// Name is the display name of the broker-dealer
	Name string

	// SenderCompID is the FIX sender ID used by the broker
	SenderCompID string

	// TargetCompID is our FIX ID that the broker connects to
	TargetCompID string

	// Session contains the FIX session configuration
	Session config.SessionConfig

	// ClientAccounts contains the list of accounts this broker can trade for
	ClientAccounts []string

	// CommissionRate specifies the standard commission rate for this broker
	CommissionRate float64
}

// ManagerConfig represents configuration for the broker-dealer manager
type ManagerConfig struct {
	// Enabled determines if broker-dealer integration is enabled
	Enabled bool

	// Brokers contains the list of configured broker-dealers
	Brokers []BrokerConfig

	// RateLimit specifies the maximum number of orders per second per broker
	RateLimit int

	// DropCopyEnabled enables the drop copy service for execution reports
	DropCopyEnabled bool
}
