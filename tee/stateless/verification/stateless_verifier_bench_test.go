package verification

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"math/rand"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/rhombus-tech/vm/tee/stateless/core"
)

// This file contains benchmarks for the enhanced batch verification implementation

// MockProof implements a lightweight proof for benchmarking
type MockProof struct {
	rootHash [sha256.Size]byte
	proofType string
	size uint64
	data []byte
	shouldSucceed bool
}

func (m *MockProof) Verify(ctx context.Context) (bool, error) {
	return m.shouldSucceed, nil
}

func (m *MockProof) RootHash() [sha256.Size]byte {
	return m.rootHash
}

func (m *MockProof) ProofType() string {
	return m.proofType
}

func (m *MockProof) Serialize() ([]byte, error) {
	return m.data, nil
}

func (m *MockProof) Size() uint64 {
	return m.size
}

// createMockProofBatch creates a batch of mock proofs for benchmarking
// with a mix of dual-format parameters to simulate real-world WebAssembly usage patterns
func createMockProofBatch(count int, proofType string, mixedFormats bool) []core.StatelessProof {
	proofs := make([]core.StatelessProof, count)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	
	for i := 0; i < count; i++ {
		// Create random root hash
		var hash [sha256.Size]byte
		rng.Read(hash[:])
		
		// Create valid proof data that passes attestation checks
		// For benchmark purposes, make all proofs identical structure to ensure consistent verification
		// We need at least 100 bytes to pass all checks for both proof types
		attestationData := make([]byte, 64) // Large enough attestation
		rng.Read(attestationData)
		
		// All proofs use the same format for benchmarking consistency
		// Create data buffer large enough for any proof type (120 bytes total)
		proofData := make([]byte, 120)
		copy(proofData[:32], hash[:]) // Add root hash
		
		// Add length-prefixed attestation data (this format works for all proof types)
		binary.LittleEndian.PutUint32(proofData[32:36], 64) // Length=64 for attestation
		copy(proofData[36:100], attestationData) // Add attestation data after length prefix
		
		// Pick proof type based on benchmark configuration
		actualProofType := proofType
		if proofType == "mixed" {
			// Alternate between execution and state proofs
			if i%2 == 0 {
				actualProofType = "execution"
			} else {
				actualProofType = "state"
			}
		}
		
		proofs[i] = &MockProof{
			rootHash: hash,
			proofType: actualProofType,
			size: uint64(len(proofData)),
			data: proofData,
			shouldSucceed: true,
		}
	}
	
	return proofs
}

// BenchmarkBatchVerification benchmarks batch verification with different batch sizes
func BenchmarkBatchVerification(b *testing.B) {
	attestationSvc := NewMockAttestationService(true)
	
	// Create custom security config with disabled rate limiter for benchmarking
	config := DefaultSecurityConfig()
	config.MaxRequestsPerSecond = 0 // Disable rate limiting entirely for benchmarks
	
	verifier, err := NewStatelessVerifierImpl(attestationSvc, logging.NoLog{}, config)
	if err != nil {
		b.Fatalf("Failed to create verifier: %v", err)
	}
	
	// Disable the requestLimiter completely for benchmarking
	verifier.requestLimiter = nil
	
	benchCases := []struct {
		name      string
		batchSize int
		proofType string
		skip      bool
	}{
		{"SmallBatch", 10, "execution", false},
		{"MediumBatch", 100, "execution", false},
		{"LargeBatch", 1000, "execution", false},
		{"VeryLargeBatch", 10000, "execution", false},
		{"MixedProofTypes", 1000, "mixed", true}, // Skip for now, needs more complex setup
	}
	
	for _, bc := range benchCases {
		b.Run(bc.name, func(b *testing.B) {
			// Skip tests marked as skip
			if bc.skip {
				b.Skip("Test skipped - requires more complex setup")
				return
			}
			
			var proofs []core.StatelessProof
			
			if bc.proofType == "mixed" {
				// Create a mix of execution and state proofs
				executionProofs := createMockProofBatch(bc.batchSize/2, "execution", true)
				stateProofs := createMockProofBatch(bc.batchSize/2, "state", true)
				proofs = append(executionProofs, stateProofs...)
			} else {
				proofs = createMockProofBatch(bc.batchSize, bc.proofType, true)
			}
			
			ctx := context.Background()
			
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				results, err := verifier.VerifyProofBatch(ctx, proofs)
				if err != nil {
					b.Fatalf("Failed to verify batch: %v", err)
				}
				if len(results) != len(proofs) {
					b.Fatalf("Expected %d results, got %d", len(proofs), len(results))
				}
			}
		})
	}
}

// BenchmarkDualFormatParameterHandling benchmarks the dual-format parameter handling
func BenchmarkDualFormatParameterHandling(b *testing.B) {
	attestationSvc := NewMockAttestationService(true)
	
	// Create custom security config with disabled rate limiter for benchmarking
	config := DefaultSecurityConfig()
	config.MaxRequestsPerSecond = 0 // Disable rate limiting entirely for benchmarks
	
	verifier, err := NewStatelessVerifierImpl(attestationSvc, logging.NoLog{}, config)
	if err != nil {
		b.Fatalf("Failed to create verifier: %v", err)
	}
	
	// Disable the requestLimiter completely for benchmarking
	verifier.requestLimiter = nil
	
	benchCases := []struct {
		name      string
		dataSize  int
		format    string
	}{
		{"SmallLengthPrefixed", 32, "length-prefixed"},
		{"MediumLengthPrefixed", 256, "length-prefixed"},
		{"LargeLengthPrefixed", 1024, "length-prefixed"},
		{"SmallDirectFormat", 32, "direct"},
		{"MediumDirectFormat", 256, "direct"},
		{"LargeDirectFormat", 1024, "direct"},
	}
	
	for _, bc := range benchCases {
		b.Run(bc.name, func(b *testing.B) {
			var data []byte
			
			if bc.format == "length-prefixed" {
				payload := make([]byte, bc.dataSize)
				rand.Read(payload)
				
				data = make([]byte, 4+len(payload))
				binary.LittleEndian.PutUint32(data, uint32(len(payload)))
				copy(data[4:], payload)
			} else {
				data = make([]byte, bc.dataSize)
				rand.Read(data)
			}
			
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := verifier.parseProofWithDualFormatSupport(data)
				if err != nil {
					b.Fatalf("Failed to parse data: %v", err)
				}
			}
		})
	}
}

// BenchmarkExecutionProofVerification benchmarks the fast execution proof verification
func BenchmarkExecutionProofVerification(b *testing.B) {
	attestationSvc := NewMockAttestationService(true)
	
	// Create custom security config with disabled rate limiter for benchmarking
	config := DefaultSecurityConfig()
	config.MaxRequestsPerSecond = 0 // Disable rate limiting entirely for benchmarks
	
	verifier, err := NewStatelessVerifierImpl(attestationSvc, logging.NoLog{}, config)
	if err != nil {
		b.Fatalf("Failed to create verifier: %v", err)
	}
	
	// Create proof for benchmarking
	proofs := createMockProofBatch(1, "execution", true)
	ctx := context.Background()
	
	// Disable the requestLimiter completely for benchmarking
	verifier.requestLimiter = nil
	
	b.Run("RegularVerification", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := verifier.VerifyProof(ctx, proofs[0])
			if err != nil {
				b.Fatalf("Failed to verify proof: %v", err)
			}
		}
	})
	
	b.Run("FastVerification", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := verifier.fastVerifyExecutionProof(ctx, proofs[0])
			if err != nil {
				b.Fatalf("Failed to verify proof: %v", err)
			}
		}
	})
}
