// Package rlnc provides integration between RLNC and the system
package rlnc

// RLNCStatus represents the current status of the RLNC system
type RLNCStatus struct {
	// Whether RLNC is currently enabled
	Enabled bool
	// Current operational mode (adaptive, fixed, disabled)
	Mode string
	// Current generation size
	GenSize int
	// Current redundancy factor
	CurrentRedundancy float64
	// Performance metrics
	Metrics RLNCMetrics
}

// RLNCMetrics contains performance metrics for RLNC operations
type RLNCMetrics struct {
	// Number of packets encoded
	PacketsEncoded int64
	// Number of packets decoded
	PacketsDecoded int64
	// Number of successful recovery operations
	SuccessfulRecoveries int64
	// Number of failed recovery attempts
	FailedRecoveries int64
	// Average encoding time in microseconds
	AvgEncodingTimeUs float64
	// Average decoding time in microseconds
	AvgDecodingTimeUs float64
}

// GetSystemRLNCStatus retrieves the current status of the RLNC system
// This includes configuration settings and performance metrics
func GetSystemRLNCStatus() (*RLNCStatus, error) {
	// In a real implementation, this would collect data from various RLNC components
	// For now, we return a simulated status
	return &RLNCStatus{
		Enabled:          true,
		Mode:             "adaptive",
		GenSize:          8,
		CurrentRedundancy: 1.5,
		Metrics: RLNCMetrics{
			PacketsEncoded:       1254,
			PacketsDecoded:       1132,
			SuccessfulRecoveries: 42,
			FailedRecoveries:     5,
			AvgEncodingTimeUs:    15.7,
			AvgDecodingTimeUs:    28.3,
		},
	}, nil
}
