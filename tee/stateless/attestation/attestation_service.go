// Package attestation provides TEE attestation services for the stateless blockchain
package attestation

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	
	"github.com/ava-labs/avalanchego/utils/logging"
	
	"github.com/rhombus-tech/vm/coordination"
	"github.com/rhombus-tech/vm/tee"
	"github.com/rhombus-tech/vm/tee/stateless/witness"
)

const (
	// TEETypeSGX represents Intel SGX TEEs
	TEETypeSGX = "sgx"
	
	// TEETypeSEV represents AMD SEV TEEs
	TEETypeSEV = "sev"
)

var (
	// ErrInvalidTEEType indicates an unsupported TEE type
	ErrInvalidTEEType = errors.New("invalid TEE type")
	
	// ErrMissingTEEMeasurement indicates missing TEE measurement
	ErrMissingTEEMeasurement = errors.New("missing TEE measurement")
	
	// ErrInvalidAttestation indicates an invalid attestation document
	ErrInvalidAttestation = errors.New("invalid attestation document")
	
	// ErrMissingEnclaveID indicates a missing enclave ID
	ErrMissingEnclaveID = errors.New("missing enclave ID")
)

// AttestationService provides attestation for TEE enclaves
type AttestationService struct {
	// Log is the logger for the attestation service
	log logging.Logger
	
	// trustStore maintains a list of trusted TEE measurements
	trustStore *TrustStore
	
	// enclaveVerifiers contains verifiers for different TEE types
	enclaveVerifiers map[string]interface{}
}

// AccumulatorVerifier uses the RSA accumulator to verify TEE attestations
type AccumulatorVerifier struct {
	accumulator    *coordination.AccumulatorClient
	teeType        string
	log            logging.Logger
}

// NewAccumulatorVerifier creates a new accumulator-based verifier
func NewAccumulatorVerifier(
	sgxEndpoint string,
	sevEndpoint string,
	teeType string,
	log logging.Logger,
) (*AccumulatorVerifier, error) {
	// Create client with cross-validation enabled
	client := coordination.NewAccumulatorClient(sgxEndpoint, sevEndpoint, true)
	
	// Verify the accumulator is healthy
	healthy, err := client.HealthCheck()
	if err != nil || !healthy {
		return nil, fmt.Errorf("accumulator service unavailable: %w", err)
	}
	
	return &AccumulatorVerifier{
		accumulator: client,
		teeType:     teeType,
		log:         log,
	}, nil
}

// VerifyAttestation verifies an attestation using the accumulator
func (v *AccumulatorVerifier) VerifyAttestation(ctx context.Context, attestation []byte) (bool, error) {
	// Check context cancellation
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
		// Continue processing
	}
	
	// Use the accumulator to validate the attestation
	valid, format, err := v.accumulator.ValidateDualFormatParameter(attestation)
	if err != nil {
		return false, fmt.Errorf("accumulator verification failed: %w", err)
	}
	
	v.log.Debug(fmt.Sprintf("Accumulator attestation verification: teeType=%s valid=%v format=%s", 
		v.teeType, valid, format))
	
	return valid, nil
}

// GetMeasurement extracts the measurement from an attestation
func (v *AccumulatorVerifier) GetMeasurement(attestation []byte) ([]byte, error) {
	// For accumulator-based verification, we use the hash of the attestation as the measurement
	// since the actual measurement is verified by the accumulator internally
	hasher := sha256.New()
	hasher.Write(attestation)
	return hasher.Sum(nil), nil
}

// TrustStore maintains a list of trusted TEE measurements
type TrustStore struct {
	lock sync.RWMutex
	
	// trustedSGXMeasurements contains trusted SGX measurements
	trustedSGXMeasurements map[[32]byte]struct{}
	
	// trustedSEVMeasurements contains trusted SEV measurements
	trustedSEVMeasurements map[[32]byte]struct{}
}

// NewTrustStore creates a new trust store
func NewTrustStore() *TrustStore {
	return &TrustStore{
		trustedSGXMeasurements: make(map[[32]byte]struct{}),
		trustedSEVMeasurements: make(map[[32]byte]struct{}),
	}
}

// AddTrustedMeasurement adds a trusted measurement to the trust store
func (ts *TrustStore) AddTrustedMeasurement(teeType string, measurement [32]byte) error {
	ts.lock.Lock()
	defer ts.lock.Unlock()
	
	switch teeType {
	case TEETypeSGX:
		ts.trustedSGXMeasurements[measurement] = struct{}{}
	case TEETypeSEV:
		ts.trustedSEVMeasurements[measurement] = struct{}{}
	default:
		return ErrInvalidTEEType
	}
	
	return nil
}

// IsTrustedMeasurement checks if a measurement is trusted
func (ts *TrustStore) IsTrustedMeasurement(teeType string, measurement [32]byte) bool {
	ts.lock.RLock()
	defer ts.lock.RUnlock()
	
	switch teeType {
	case TEETypeSGX:
		_, ok := ts.trustedSGXMeasurements[measurement]
		return ok
	case TEETypeSEV:
		_, ok := ts.trustedSEVMeasurements[measurement]
		return ok
	default:
		return false
	}
}

// GenerateRandomAttestation creates a mock attestation for testing
// In a production environment, this would be replaced with actual accumulator-based verification
func GenerateRandomAttestation(teeType string) []byte {
	// Create a mock attestation that's suitable for the accumulator
	attestation := make([]byte, 64)
	
	// First byte indicates TEE type (0=SGX, 1=SEV)
	if teeType == TEETypeSEV {
		attestation[0] = 1
	} else {
		attestation[0] = 0
	}
	
	// Fill the rest with a recognizable pattern
	for i := 1; i < 64; i++ {
		if teeType == TEETypeSGX {
			attestation[i] = byte(i % 256)
		} else {
			attestation[i] = byte(64 - (i % 256))
		}
	}
	
	return attestation
}

// NewAttestationService creates a new attestation service
func NewAttestationService(log logging.Logger, teeConfig *tee.TEEConfig) *AttestationService {
	trustStore := NewTrustStore()
	
	// Add some default trusted measurements for demonstration
	// In a real implementation, these would be loaded from a secure source
	defaultSGXMeasurement := [32]byte{}
	defaultSEVMeasurement := [32]byte{}
	
	// Set some recognizable pattern
	for i := 0; i < 32; i++ {
		defaultSGXMeasurement[i] = byte(i)
		defaultSEVMeasurement[i] = byte(32 - i)
	}
	
	_ = trustStore.AddTrustedMeasurement(TEETypeSGX, defaultSGXMeasurement)
	_ = trustStore.AddTrustedMeasurement(TEETypeSEV, defaultSEVMeasurement)
	
	// Use the real TEE endpoints from configuration
	sgxEndpoint := teeConfig.SGXEndpoint
	sevEndpoint := teeConfig.SEVEndpoint
	
	// Add HTTP scheme if not present
	if sgxEndpoint != "" && !containsScheme(sgxEndpoint) {
		sgxEndpoint = "http://" + sgxEndpoint
	}
	if sevEndpoint != "" && !containsScheme(sevEndpoint) {
		sevEndpoint = "http://" + sevEndpoint
	}
	
	log.Debug(fmt.Sprintf("Using TEE endpoints: SGX=%s, SEV=%s", sgxEndpoint, sevEndpoint))
	
	// Create verifiers that use the accumulator
	sgxVerifier, _ := NewAccumulatorVerifier(sgxEndpoint, sevEndpoint, TEETypeSGX, log)
	sevVerifier, _ := NewAccumulatorVerifier(sgxEndpoint, sevEndpoint, TEETypeSEV, log)
	
	// Map of verifiers by TEE type
	enclaveVerifiers := map[string]interface{}{
		TEETypeSGX: sgxVerifier,
		TEETypeSEV: sevVerifier,
	}
	
	return &AttestationService{
		log:              log,
		trustStore:       trustStore,
		enclaveVerifiers: enclaveVerifiers,
	}
}

// Ensure AttestationService implements both witness.AttestationService and verification.AttestationService
var _ witness.AttestationService = (*AttestationService)(nil)

// GetAttestation implements the witness.AttestationService interface
func (s *AttestationService) GetAttestation(enclaveID []byte) (*witness.EnclaveAttestation, error) {
	// For implementation simplicity, we'll use the enclaveID to determine the TEE type
	// In a real implementation, this would query your accumulator service
	teeType := TEETypeSGX
	if len(enclaveID) > 0 && enclaveID[0] == 1 {
		teeType = TEETypeSEV
	}
	
	// Generate an attestation that would be validated by the accumulator
	randomAttestation := GenerateRandomAttestation(teeType)
	
	// Calculate measurement hash
	hasher := sha256.New()
	hasher.Write(randomAttestation)
	measurement := hasher.Sum(nil)
	
	s.log.Debug(fmt.Sprintf("Generated accumulator-verifiable attestation: teeType=%s attestationLen=%d measurementLen=%d",
		teeType, len(randomAttestation), len(measurement)))
	
	return &witness.EnclaveAttestation{
		Measurement: measurement,
	}, nil
}

// VerifySignature verifies the signature of an attestation
func (s *AttestationService) VerifySignature(attestation *witness.EnclaveAttestation) error {
	if attestation == nil {
		return ErrInvalidAttestation
	}
	
	if len(attestation.Measurement) == 0 {
		return ErrMissingTEEMeasurement
	}
	
	// In the actual witness.EnclaveAttestation struct, we don't have
	// EnclaveID or TeePlatform fields, so we'll determine the TEE type
	// from the measurement pattern instead
	teeType := TEETypeSGX
	
	// Convert to fixed size array for trust store check
	var measurementArray [32]byte
	if len(attestation.Measurement) > 32 {
		copy(measurementArray[:], attestation.Measurement[:32])
	} else {
		copy(measurementArray[:], attestation.Measurement)
	}
	
	// Check if this is a trusted measurement
	if !s.trustStore.IsTrustedMeasurement(teeType, measurementArray) {
		return fmt.Errorf("untrusted measurement for %s TEE", teeType)
	}
	
	// In a real implementation, this would verify the enclave's signature
	// using the attestation document
	
	return nil
}

// VerifyAttestation verifies an attestation document
func (s *AttestationService) VerifyAttestation(attestation []byte) (bool, error) {
	if len(attestation) == 0 {
		return false, ErrInvalidAttestation
	}
	
	// In a basic implementation, the first byte indicates the TEE type
	// 0 = SGX, 1 = SEV
	// In a real implementation, you would parse the attestation structure
	teeType := TEETypeSGX
	if len(attestation) > 0 && attestation[0] == 1 {
		teeType = TEETypeSEV
	}
	
	verifier, ok := s.enclaveVerifiers[teeType]
	if !ok {
		return false, ErrInvalidTEEType
	}
	
	// Verify the attestation using the appropriate verifier
	ctx := context.Background()
	accumVerifier, ok := verifier.(*AccumulatorVerifier)
	if !ok {
		return false, fmt.Errorf("invalid verifier type for %s", teeType)
	}
	
	return accumVerifier.VerifyAttestation(ctx, attestation)
}

// AddTrustedMeasurement adds a trusted measurement to the service
func (s *AttestationService) AddTrustedMeasurement(teeType string, measurement [32]byte) error {
	return s.trustStore.AddTrustedMeasurement(teeType, measurement)
}

// GetTrustStore returns the trust store
func (s *AttestationService) GetTrustStore() *TrustStore {
	return s.trustStore
}

// containsScheme checks if a URL contains the http:// or https:// scheme prefix
func containsScheme(url string) bool {
	return len(url) >= 7 && (url[:7] == "http://" || len(url) >= 8 && url[:8] == "https://")
}
