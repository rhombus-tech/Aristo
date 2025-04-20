package accumulator

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"sync"
	"time"
)

// HardwareVerifier provides direct TEE hardware attestation verification
// that integrates with our high-performance accumulator
type HardwareVerifier struct {
	// Cache of known good measurements
	knownMeasurements map[string][]byte
	lastBootstrap    time.Time
	mutex            sync.Mutex
	simulationMode   bool
}

// NewHardwareVerifier creates a verifier that integrates with TEE hardware
func NewHardwareVerifier() *HardwareVerifier {
	// Check if we're in simulation mode
	simulationMode := os.Getenv("ENARX_SIMULATION") == "1"
	
	return &HardwareVerifier{
		knownMeasurements: make(map[string][]byte),
		simulationMode:    simulationMode,
	}
}

// Bootstrap initializes the verifier with trusted measurements
func (h *HardwareVerifier) Bootstrap() error {
	// Create a channel to implement a timeout
	done := make(chan struct{})
	var bootstrapErr error
	
	// Use a goroutine with timeout for the bootstrap operation
	go func() {
		// Attempt to acquire the lock with a timeout
		if !h.mutex.TryLock() {
			// If someone else has the lock, wait briefly and check if measurements exist
			time.Sleep(100 * time.Millisecond)
			if len(h.knownMeasurements) > 0 {
				close(done)
				return
			}
			
			// Now try the lock again
			if !h.mutex.TryLock() {
				bootstrapErr = fmt.Errorf("could not acquire lock for bootstrap")
				close(done)
				return
			}
		}
		defer h.mutex.Unlock()
		
		// Avoid frequent bootstrapping
		if time.Since(h.lastBootstrap) < 6*time.Hour && len(h.knownMeasurements) > 0 {
			close(done)
			return
		}
		
		// Initialize map if needed
		if h.knownMeasurements == nil {
			h.knownMeasurements = make(map[string][]byte)
		}
		
		// For SGX
		if h.isSGXAvailable() {
			measurement, err := h.bootstrapSGX()
			if err != nil {
				bootstrapErr = fmt.Errorf("SGX bootstrap failed: %v", err)
				close(done)
				return
			}
			h.knownMeasurements["SGX"] = measurement
		}
		
		// For SEV
		if h.isSEVAvailable() {
			measurement, err := h.bootstrapSEV()
			if err != nil {
				bootstrapErr = fmt.Errorf("SEV bootstrap failed: %v", err)
				close(done)
				return
			}
			h.knownMeasurements["SEV"] = measurement
		}
		
		// Update bootstrap time
		h.lastBootstrap = time.Now()
		close(done)
	}()
	
	// Wait for bootstrap with timeout
	select {
	case <-done:
		return bootstrapErr
	case <-time.After(5 * time.Second):
		return fmt.Errorf("bootstrap timed out")
	}
	
	return nil
}

// bootstrapSGX performs the initial SGX attestation using DCAP
func (h *HardwareVerifier) bootstrapSGX() ([]byte, error) {
	// In simulation mode, create a known measurement
	if h.simulationMode {
		measurement := sha256.Sum256([]byte("Enarx SGX Accumulator"))
		return measurement[:], nil
	}
	
	// Get a real quote from SGX hardware
	quote, err := h.getQuoteFromHardware()
	if err != nil {
		return nil, err
	}
	
	// Verify with DCAP and extract the measurement
	measurement, err := h.verifyQuoteWithDCAP(quote)
	if err != nil {
		return nil, err
	}
	
	return measurement, nil
}

// bootstrapSEV performs the initial SEV attestation
func (h *HardwareVerifier) bootstrapSEV() ([]byte, error) {
	// In simulation mode, create a known measurement
	if h.simulationMode {
		measurement := sha256.Sum256([]byte("Enarx SEV Accumulator"))
		return measurement[:], nil
	}
	
	// Get a real report from SEV hardware
	report, err := h.getReportFromHardware()
	if err != nil {
		return nil, err
	}
	
	// Verify with AMD KDS and extract the measurement
	measurement, err := h.verifyReportWithAMDKDS(report)
	if err != nil {
		return nil, err
	}
	
	return measurement, nil
}

// VerifySGXAttestation verifies an SGX attestation using the accumulator
func (h *HardwareVerifier) VerifySGXAttestation(attestation []byte) (*big.Int, []byte, error) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	
	// Extract measurement from attestation
	measurement, err := h.extractSGXMeasurement(attestation)
	if err != nil {
		return nil, nil, err
	}
	
	// Compare with known measurement
	knownMeasurement, ok := h.knownMeasurements["SGX"]
	if !ok {
		// Bootstrap if we don't have a known measurement
		if err := h.Bootstrap(); err != nil {
			return nil, nil, fmt.Errorf("bootstrap failed: %v", err)
		}
		knownMeasurement = h.knownMeasurements["SGX"]
	}

	// Verify the measurement
	if !h.verifyMeasurementEquality(measurement, knownMeasurement) {
		return nil, nil, fmt.Errorf("SGX measurement verification failed")
	}
	
	// Hash the measurement for the accumulator
	measurementHash := sha256.Sum256(measurement)
	
	// For the high-performance accumulator
	prime, err := h.hashToPrime(measurementHash[:])
	if err != nil {
		return nil, nil, err
	}
	
	return prime, measurement, nil
}

// VerifySEVAttestation verifies an SEV attestation using the accumulator
func (h *HardwareVerifier) VerifySEVAttestation(attestation []byte) (*big.Int, []byte, error) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	
	// Extract measurement from attestation
	measurement, err := h.extractSEVMeasurement(attestation)
	if err != nil {
		return nil, nil, err
	}
	
	// Compare with known measurement
	knownMeasurement, ok := h.knownMeasurements["SEV"]
	if !ok {
		// Bootstrap if we don't have a known measurement
		if err := h.Bootstrap(); err != nil {
			return nil, nil, fmt.Errorf("bootstrap failed: %v", err)
		}
		knownMeasurement = h.knownMeasurements["SEV"]
	}

	// Verify the measurement
	if !h.verifyMeasurementEquality(measurement, knownMeasurement) {
		return nil, nil, fmt.Errorf("SEV measurement verification failed")
	}
	
	// Hash the measurement for the accumulator
	measurementHash := sha256.Sum256(measurement)
	
	// For the high-performance accumulator
	prime, err := h.hashToPrime(measurementHash[:])
	if err != nil {
		return nil, nil, err
	}
	
	return prime, measurement, nil
}

// extractSGXMeasurement extracts the measurement from an SGX attestation
func (h *HardwareVerifier) extractSGXMeasurement(attestation []byte) ([]byte, error) {
	// In simulation mode
	if h.simulationMode {
		if len(attestation) < 80 {
			return nil, fmt.Errorf("attestation too short")
		}
		return attestation[48:80], nil
	}
	
	// With real hardware: Extract MRENCLAVE from the SGX quote
	if len(attestation) < 80 {
		return nil, fmt.Errorf("SGX attestation too short")
	}
	
	// The measurement (MRENCLAVE) is at a specific offset in the SGX quote
	// For Intel SGX, MRENCLAVE is a 32-byte value
	measurement := make([]byte, 32)
	copy(measurement, attestation[48:80])
	
	return measurement, nil
}

// extractSEVMeasurement extracts the measurement from an SEV attestation
func (h *HardwareVerifier) extractSEVMeasurement(attestation []byte) ([]byte, error) {
	// In simulation mode
	if h.simulationMode {
		if len(attestation) < 68 {
			return nil, fmt.Errorf("attestation too short")
		}
		return attestation[36:68], nil
	}
	
	// With real hardware: Extract measurement from SEV-SNP report
	if len(attestation) < 68 {
		return nil, fmt.Errorf("SEV attestation too short")
	}
	
	// The measurement is at a specific offset in the SEV-SNP report
	// For AMD SEV-SNP, the measurement is a 32-byte value
	measurement := make([]byte, 32)
	copy(measurement, attestation[36:68])
	
	return measurement, nil
}

// verifyMeasurementEquality verifies that two measurements are equal
// with protections against timing attacks
func (h *HardwareVerifier) verifyMeasurementEquality(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	
	var result byte
	for i := 0; i < len(a); i++ {
		result |= a[i] ^ b[i]
	}
	
	return result == 0
}

// hashToPrime hashes data to a prime suitable for the accumulator
func (h *HardwareVerifier) hashToPrime(data []byte) (*big.Int, error) {
	// Hash the data
	hash := sha256.Sum256(data)
	
	// Convert to a big.Int
	num := new(big.Int).SetBytes(hash[:])
	
	// Ensure it's odd (even numbers other than 2 aren't prime)
	if num.Bit(0) == 0 {
		num.SetBit(num, 0, 1)
	}
	
	// Find the next prime
	for i := 0; i < 1000; i++ {
		if num.ProbablyPrime(10) {
			return num, nil
		}
		num.Add(num, big.NewInt(2)) // Try next odd number
	}
	
	return nil, fmt.Errorf("failed to find prime after 1000 attempts")
}

// isSGXAvailable checks if SGX is available on the current system
func (h *HardwareVerifier) isSGXAvailable() bool {
	// Check simulation mode environment variable
	if h.simulationMode {
		return true // Simulate SGX availability
	}
	
	// Check for SGX device
	_, err := os.Stat("/dev/sgx_enclave")
	if err == nil {
		return true
	}
	
	// Try sgx_detect tool
	cmd := exec.Command("which", "sgx_detect")
	if err := cmd.Run(); err == nil {
		// Tool exists, run it
		detectCmd := exec.Command("sgx_detect")
		output, err := detectCmd.Output()
		if err == nil {
			return len(output) > 0 && string(output) != ""
		}
	}
	
	return false
}

// isSEVAvailable checks if SEV is available on the current system
func (h *HardwareVerifier) isSEVAvailable() bool {
	// Check simulation mode environment variable
	if h.simulationMode {
		return true // Simulate SEV availability
	}
	
	// Check for SEV device
	_, err := os.Stat("/dev/sev")
	if err == nil {
		return true
	}
	
	// Check for AMD CPU
	cmd := exec.Command("grep", "-q", "AuthenticAMD", "/proc/cpuinfo")
	if err := cmd.Run(); err == nil {
		// Check for SEV-SNP capability
		checkCmd := exec.Command("grep", "-q", "sev", "/sys/module/kvm_amd/parameters/sev")
		if err := checkCmd.Run(); err == nil {
			return true
		}
	}
	
	return false
}

// getQuoteFromHardware gets an SGX quote from the hardware
func (h *HardwareVerifier) getQuoteFromHardware() ([]byte, error) {
	if h.simulationMode {
		// In simulation mode, generate a fake quote
		quote := make([]byte, 512)
		_, err := rand.Read(quote)
		if err != nil {
			return nil, err
		}
		
		// Add a marker to show it's simulated
		copy(quote[:8], []byte("SIM_QUOT"))
		
		// Add the current time for freshness
		now := time.Now().Unix()
		binary.LittleEndian.PutUint64(quote[8:16], uint64(now))
		
		return quote, nil
	}
	
	// Create random data to get attestation for (nonce)
	nonce := make([]byte, 64)
	_, err := rand.Read(nonce)
	if err != nil {
		return nil, err
	}
	
	// Get attestation from SGX hardware (this is platform specific)
	// This would use SGX SDK libraries in a real implementation
	cmd := exec.Command("/opt/intel/sgxsdk/bin/x64/sgx_quote_generation", 
		"--report-data", fmt.Sprintf("%x", nonce))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("quote generation failed: %v: %s", err, output)
	}
	
	return output, nil
}

// getReportFromHardware gets an SEV report from the hardware
func (h *HardwareVerifier) getReportFromHardware() ([]byte, error) {
	if h.simulationMode {
		// In simulation mode, generate a fake SEV report
		report := make([]byte, 512)
		_, err := rand.Read(report)
		if err != nil {
			return nil, err
		}
		
		// Add a marker to show it's simulated
		copy(report[:8], []byte("SIM_SEVR"))
		
		// Add the current time for freshness
		now := time.Now().Unix()
		binary.LittleEndian.PutUint64(report[8:16], uint64(now))
		
		return report, nil
	}
	
	// Create random data to get attestation for (nonce)
	nonce := make([]byte, 64)
	_, err := rand.Read(nonce)
	if err != nil {
		return nil, err
	}
	
	// Get attestation from SEV hardware (this is platform specific)
	// This would use SEV-SNP libraries in a real implementation
	cmd := exec.Command("/opt/amd/snp-report-generator", 
		"--report-data", fmt.Sprintf("%x", nonce))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("report generation failed: %v: %s", err, output)
	}
	
	return output, nil
}

// verifyQuoteWithDCAP verifies an SGX quote using DCAP
func (h *HardwareVerifier) verifyQuoteWithDCAP(quote []byte) ([]byte, error) {
	if h.simulationMode {
		// In simulation mode, just return a mock measurement
		if len(quote) < 8 {
			return nil, fmt.Errorf("invalid quote format")
		}
		hash := sha256.Sum256(quote)
		return hash[:], nil
	}

	// Call the DCAP verification library (platform-specific)
	cmd := exec.Command("/opt/intel/sgxsdk/bin/x64/sgx_dcap_quoteverify", 
		"--quote", fmt.Sprintf("%x", quote))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("quote verification failed: %v: %s", err, output)
	}

	// Extract measurement from the response (implementation-specific)
	// Simplification - real implementation depends on DCAP response format
	return output, nil
}

// verifyReportWithAMDKDS verifies an SEV report using AMD KDS
func (h *HardwareVerifier) verifyReportWithAMDKDS(report []byte) ([]byte, error) {
	if h.simulationMode {
		// In simulation mode, just return a mock measurement
		if len(report) < 8 {
			return nil, fmt.Errorf("invalid report format") 
		}
		hash := sha256.Sum256(report)
		return hash[:], nil
	}

	// Call the AMD KDS verification library (platform-specific)
	cmd := exec.Command("/opt/amd/snp-verify", "--report", fmt.Sprintf("%x", report))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("report verification failed: %v: %s", err, output)
	}

	// Extract measurement from the response (implementation-specific)
	// Simplification - real implementation depends on KDS response format
	return output, nil
}
