package cmd

import (
	"fmt"
	"strings"

	"github.com/rhombus-tech/vm/tee/compute"
	"github.com/rhombus-tech/vm/tee/rlnc"
	"github.com/spf13/cobra"
)

// RLNC configuration vars
var (
	rlncEnabled bool
	rlncAdaptive bool
	rlncGenSize int
	rlncMinRedundancy float64
	rlncMaxRedundancy float64
	networkProfile string

	// Status command flag
	showRLNCStatus bool
)

// Network profiles with predefined RLNC settings
var networkProfiles = map[string]struct {
	Enabled       bool
	Adaptive      bool
	GenSize       int
	MinRedundancy float64
	MaxRedundancy float64
}{
	"low-latency": {
		Enabled:       true,
		Adaptive:      true,
		GenSize:       4,
		MinRedundancy: 1.25, // 25% redundancy minimum
		MaxRedundancy: 2.0,  // 100% redundancy maximum
	},
	"high-reliability": {
		Enabled:       true,
		Adaptive:      true,
		GenSize:       8,
		MinRedundancy: 1.5, // 50% redundancy minimum
		MaxRedundancy: 3.0, // 200% redundancy maximum
	},
	"balanced": {
		Enabled:       true,
		Adaptive:      true,
		GenSize:       6,
		MinRedundancy: 1.3, // 30% redundancy minimum
		MaxRedundancy: 2.5, // 150% redundancy maximum
	},
	"disabled": {
		Enabled:       false,
		Adaptive:      false,
		GenSize:       8,
		MinRedundancy: 1.0,
		MaxRedundancy: 1.0,
	},
}

var computeCmd = &cobra.Command{
	Use:   "compute",
	Short: "Manage compute nodes",
	RunE: func(*cobra.Command, []string) error {
		return ErrMissingSubcommand
	},
}

var startComputeCmd = &cobra.Command{
	Use:   "start [region-id] [port]",
	Short: "Start compute node",
	Args:  cobra.ExactArgs(2),
	RunE: func(_ *cobra.Command, args []string) error {
		regionID := args[0]
		port := args[1]

		config := compute.DefaultConfig()
		config.RegionID = regionID
		config.ControllerPath = controllerPath
		config.WasmPath = wasmPath
		
		// If a network profile was specified, apply those settings
		if networkProfile != "" {
			profile, exists := networkProfiles[strings.ToLower(networkProfile)]
			if !exists {
				return fmt.Errorf("unknown network profile: %s", networkProfile)
			}
			
			// Apply the profile settings
			rlncEnabled = profile.Enabled
			rlncAdaptive = profile.Adaptive
			rlncGenSize = profile.GenSize
			rlncMinRedundancy = profile.MinRedundancy
			rlncMaxRedundancy = profile.MaxRedundancy
		}
		
		// Configure RLNC settings
		config.RLNC = &compute.RLNCConfig{
			Enabled:       rlncEnabled,
			AdaptiveMode:  rlncAdaptive,
			GenSize:       rlncGenSize,
			MinRedundancy: rlncMinRedundancy,
			MaxRedundancy: rlncMaxRedundancy,
		}

		// Pass only the config object as that's what the function expects
		node, err := compute.NewComputeNode(config)
		if err != nil {
			return err
		}

		return node.Start(port)
	},
}

var showRLNCStatusCmd = &cobra.Command{
	Use:   "rlnc-status",
	Short: "Show current RLNC configuration and statistics",
	RunE: func(_ *cobra.Command, _ []string) error {
		// Get the RLNC system status
		status, err := rlnc.GetSystemRLNCStatus()
		if err != nil {
			return fmt.Errorf("failed to get RLNC status: %v", err)
		}
		
		fmt.Printf("RLNC Status: %s\n", map[bool]string{true: "Enabled", false: "Disabled"}[status.Enabled])
		fmt.Printf("Mode: %s\n", status.Mode)
		fmt.Printf("Generation Size: %d\n", status.GenSize)
		fmt.Printf("Current Redundancy: %.2f\n", status.CurrentRedundancy)
		fmt.Printf("Performance Metrics:\n")
		fmt.Printf("  Packets Encoded: %d\n", status.Metrics.PacketsEncoded)
		fmt.Printf("  Packets Decoded: %d\n", status.Metrics.PacketsDecoded)
		fmt.Printf("  Successful Recoveries: %d\n", status.Metrics.SuccessfulRecoveries)
		fmt.Printf("  Failed Recoveries: %d\n", status.Metrics.FailedRecoveries)
		fmt.Printf("  Avg Encoding Time: %.2f µs\n", status.Metrics.AvgEncodingTimeUs)
		fmt.Printf("  Avg Decoding Time: %.2f µs\n", status.Metrics.AvgDecodingTimeUs)
		
		return nil
	},
}

func init() {
	computeCmd.AddCommand(startComputeCmd)
	computeCmd.AddCommand(showRLNCStatusCmd)

	computeCmd.PersistentFlags().StringVar(
		&controllerPath,
		"controller",
		"/usr/local/bin/tee-controller",
		"TEE controller path",
	)
	computeCmd.PersistentFlags().StringVar(
		&wasmPath,
		"wasm",
		"/usr/local/bin/tee-wasm.wasm",
		"WASM module path",
	)
	
	// Add RLNC configuration flags
	startComputeCmd.Flags().BoolVar(
		&rlncEnabled,
		"rlnc-enable",
		true,
		"Enable RLNC for resilient network communications",
	)
	startComputeCmd.Flags().BoolVar(
		&rlncAdaptive,
		"rlnc-adaptive",
		true,
		"Enable adaptive mode for RLNC to dynamically adjust to network conditions",
	)
	startComputeCmd.Flags().IntVar(
		&rlncGenSize,
		"rlnc-gen-size",
		8,
		"RLNC generation size (2-32)",
	)
	startComputeCmd.Flags().Float64Var(
		&rlncMinRedundancy,
		"rlnc-min-redundancy",
		1.3,
		"Minimum redundancy factor for RLNC (1.0-3.0)",
	)
	startComputeCmd.Flags().Float64Var(
		&rlncMaxRedundancy,
		"rlnc-max-redundancy",
		2.5,
		"Maximum redundancy factor for RLNC (1.0-3.0)",
	)
	startComputeCmd.Flags().StringVar(
		&networkProfile,
		"network-profile",
		"",
		"Predefined network profile for RLNC (low-latency, high-reliability, balanced, disabled)",
	)
}
