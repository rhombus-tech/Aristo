package tee

import (
	"crypto/rand"
	"encoding/hex"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	// Export measurementToPrimeRep for testing
	MeasurementToPrimeRep = func(acc *RsaAccumulatorClient, measurement []byte) (*big.Int, error) {
		return acc.measurementToPrimeRep(measurement)
	}
)

func TestRSAAccumulator(t *testing.T) {
	// Create a temporary directory for test accumulator files
	tempDir, err := os.MkdirTemp("", "rsa_accumulator_test")
	require.NoError(t, err, "Should create temp directory")
	defer os.RemoveAll(tempDir)

	// Save original env vars and restore after test
	origAccumPath := os.Getenv("TDX_ACCUMULATOR_PATH")
	origAllowDefault := os.Getenv("TDX_ALLOW_DEFAULT_ACCUMULATOR")
	origAllowWitness := os.Getenv("TDX_ALLOW_WITNESS_CALCULATION")
	defer func() {
		os.Setenv("TDX_ACCUMULATOR_PATH", origAccumPath)
		os.Setenv("TDX_ALLOW_DEFAULT_ACCUMULATOR", origAllowDefault)
		os.Setenv("TDX_ALLOW_WITNESS_CALCULATION", origAllowWitness)
	}()

	// Set test environment
	testAccumPath := filepath.Join(tempDir, "test_accumulator.json")
	os.Setenv("TDX_ACCUMULATOR_PATH", testAccumPath)
	os.Setenv("TDX_ALLOW_DEFAULT_ACCUMULATOR", "true")
	os.Setenv("TDX_ALLOW_WITNESS_CALCULATION", "true")

	// Note: Since we're using a different accumulator path for each test,
	// we don't need to reset singleton state between tests

	t.Run("MeasurementToPrimeRepresentation", func(t *testing.T) {
		// Test with various measurements
		testMeasurements := [][]byte{
			[]byte("test-measurement-1"),
			[]byte("test-measurement-2"),
			make([]byte, 48), // Empty measurement
			func() []byte {    // Random 48-byte measurement
				b := make([]byte, 48)
				rand.Read(b)
				return b
			}(),
		}

		for i, measurement := range testMeasurements {
			t.Run(hex.EncodeToString(measurement)[:16], func(t *testing.T) {
				// Get our accumulator instance
				accum, err := GetAccumulatorClient(testAccumPath)
				require.NoError(t, err)

				// Convert to prime representatives (used later in tests)
				primeRep1, err := accum.measurementToPrimeRep(measurement)
				require.NoError(t, err, "Should convert to prime rep")

				// Verify it's actually a strong prime
				assert.True(t, primeRep1.ProbablyPrime(20), "Should generate a probable prime")

				// Different measurements should map to different primes
				for j, otherMeasurement := range testMeasurements {
					if i == j {
						continue
					}
					otherPrime, err := MeasurementToPrimeRep(accum, otherMeasurement)
					require.NoError(t, err)
					assert.NotEqual(t, primeRep1, otherPrime, "Different measurements should map to different primes")
				}
			})
		}
	})

	t.Run("InitializeAccumulator", func(t *testing.T) {
		// Initialize a new accumulator
		accumulator, err := GetAccumulatorClient(testAccumPath)
		require.NoError(t, err, "Should create accumulator")
		require.NotNil(t, accumulator, "Accumulator should not be nil")

		// Verify the accumulator has core properties
		assert.NotNil(t, accumulator.modulus, "Should initialize modulus")
		assert.NotNil(t, accumulator.exponent, "Should initialize exponent")
		assert.NotNil(t, accumulator.witnessMap, "Should initialize witness map")
		assert.NotEmpty(t, accumulator.accumulatorPath, "Should set accumulator path")
		assert.Equal(t, big.NewInt(1), accumulator.getCurrentAccumulatedValue(), "Should start with accumulator value 1")
	})

	t.Run("AddMeasurement", func(t *testing.T) {
		// Get accumulator
		accumulator, err := GetAccumulatorClient(testAccumPath)
		require.NoError(t, err)

		// Generate a test measurement
		testMeasurement := []byte("test-measurement-for-addition")

		// Get initial accumulated value
		initialValue := accumulator.getCurrentAccumulatedValue()

		// Get prime rep and calculate witness
		primeRep, err := MeasurementToPrimeRep(accumulator, testMeasurement)
		require.NoError(t, err)
		witness, err := accumulator.simulateWitnessCalculation(primeRep)
		require.NoError(t, err)

		// Store the witness
		err = accumulator.storeWitness(testMeasurement, witness)
		require.NoError(t, err, "Should store witness without error")

		// Verify accumulator state
		newValue := accumulator.getCurrentAccumulatedValue()
		assert.NotEqual(t, initialValue, newValue, "Accumulated value should be different after adding measurement")

		// Just verify the unknown measurement doesn't match
		// No need to actually compute the prime representation
		witness, err = accumulator.getWitness(testMeasurement)
		assert.NoError(t, err, "Should retrieve witness")
		assert.NotNil(t, witness, "Witness should not be nil")
	})

	t.Run("VerifyMeasurement", func(t *testing.T) {
		// Get accumulator
		accumulator, err := GetAccumulatorClient(testAccumPath)
		require.NoError(t, err)

		// Generate and prepare a test measurement
		testMeasurement := []byte("verify-measurement-test")

		// Generate prime representation and witness
		primeRep, err := MeasurementToPrimeRep(accumulator, testMeasurement)
		require.NoError(t, err)
		witness, err := accumulator.simulateWitnessCalculation(primeRep)
		require.NoError(t, err)

		// Store the witness
		err = accumulator.storeWitness(testMeasurement, witness)
		require.NoError(t, err, "Should store witness")

		// Verify the measurement is accepted
		result, err := accumulator.verifyMeasurement(testMeasurement)
		require.NoError(t, err, "Should verify without error")
		assert.True(t, result, "Should verify added measurement")

		// Temporarily unset TDX_ALLOW_UNKNOWN_MEASUREMENTS to get expected error behavior
		origEnv := os.Getenv("TDX_ALLOW_UNKNOWN_MEASUREMENTS")
		os.Setenv("TDX_ALLOW_UNKNOWN_MEASUREMENTS", "")
		defer os.Setenv("TDX_ALLOW_UNKNOWN_MEASUREMENTS", origEnv)
		
		// Test with an unrecognized measurement
		unknownMeasurement := []byte("unknown-measurement")
		result, err = accumulator.verifyMeasurement(unknownMeasurement)
		
		// For unknown measurements, we expect verification to fail with an error
		require.Error(t, err, "Should return error for unknown measurement")
		assert.False(t, result, "Should not verify unknown measurement")
		assert.Contains(t, err.Error(), "not found", "Error should mention witness not found")
	})

	t.Run("WitnessCalculation", func(t *testing.T) {
		// Get accumulator
		accumulator, err := GetAccumulatorClient(testAccumPath)
		require.NoError(t, err)

		// Calculate prime representation
		testMeasurement := []byte("test-measurement-witness-calculation")
		prime, err := MeasurementToPrimeRep(accumulator, testMeasurement)
		require.NoError(t, err)

		// Calculate witness
		witness, err := accumulator.simulateWitnessCalculation(prime)
		require.NoError(t, err, "Should calculate witness")
		assert.NotNil(t, witness, "Witness should not be nil")

		// Store witness
		err = accumulator.storeWitness(testMeasurement, witness)
		require.NoError(t, err)

		// Get the witness from the accumulator
		retWitness, err := accumulator.getWitness(testMeasurement)
		require.NoError(t, err, "Should get witness")
		assert.Equal(t, witness.String(), retWitness.String(), "Witnesses should match")
	})

	t.Run("DualFormatParameterHandling", func(t *testing.T) {
		// Get accumulator
		accumulator, err := GetAccumulatorClient(testAccumPath)
		require.NoError(t, err)

		// Test measurement
		originalMeasurement := []byte("dual-format-test-measurement")

		// Add it to the accumulator
		primeRep, err := MeasurementToPrimeRep(accumulator, originalMeasurement)
		require.NoError(t, err)
		witness, err := accumulator.simulateWitnessCalculation(primeRep)
		require.NoError(t, err)
		err = accumulator.storeWitness(originalMeasurement, witness)
		require.NoError(t, err, "Should add measurement")

		// Verify the measurement works
		result, err := accumulator.verifyMeasurement(originalMeasurement)
		assert.NoError(t, err, "Should verify direct format")
		assert.True(t, result, "Direct format should verify")

		// Create length-prefixed format
		prefixedMeasurement := make([]byte, 4+len(originalMeasurement))
		prefixedMeasurement[0] = byte(len(originalMeasurement))
		prefixedMeasurement[1] = 0
		prefixedMeasurement[2] = 0
		prefixedMeasurement[3] = 0
		copy(prefixedMeasurement[4:], originalMeasurement)

		// Test verification with length-prefixed format
		result, err = accumulator.verifyMeasurement(prefixedMeasurement)
		assert.NoError(t, err, "Should verify length-prefixed format")
		assert.True(t, result, "Length-prefixed format should verify")
	})

	t.Run("PublicAPIVerification", func(t *testing.T) {
		// Test the public API function
		testMeasurement := []byte("public-api-test-measurement")

		// Add to accumulator
		accumulator, err := GetAccumulatorClient(testAccumPath)
		require.NoError(t, err)

		// Calculate prime rep and witness
		primeRep, err := MeasurementToPrimeRep(accumulator, testMeasurement)
		require.NoError(t, err)
		witness, err := accumulator.simulateWitnessCalculation(primeRep)
		require.NoError(t, err)
		err = accumulator.storeWitness(testMeasurement, witness)
		require.NoError(t, err)

		// Test public API
		result, err := AccumulatorVerifyMeasurement(testMeasurement, testAccumPath)
		require.NoError(t, err, "Public API should verify measurement")
		assert.True(t, result, "Public API should verify valid measurement")

		// Try with nil measurement
		result, err = AccumulatorVerifyMeasurement(nil, testAccumPath)
		assert.Error(t, err, "Should reject nil measurement")
		assert.False(t, result, "Should not verify nil measurement")

		// Try with unknown measurement
		unknownMeasurement := []byte("unknown-measurement")
		result, err = AccumulatorVerifyMeasurement(unknownMeasurement, testAccumPath)
		// In test mode, unknown measurements are handled without errors
		// but should still return false for verification
		require.NoError(t, err, "Should handle unknown measurement")
		assert.False(t, result, "Should not verify unknown measurement")
	})

	t.Run("PersistenceAndRecovery", func(t *testing.T) {
		// Get initial accumulator
		accumulator, err := GetAccumulatorClient(testAccumPath)
		require.NoError(t, err)

		// Create test measurements
		measurements := [][]byte{
			[]byte("persistence-test-1"),
			[]byte("persistence-test-2"),
			[]byte("persistence-test-3"),
		}

		// Add all measurements (generate and store witnesses)
		for _, m := range measurements {
			primeRep, err := MeasurementToPrimeRep(accumulator, m)
			require.NoError(t, err)
			witness, err := accumulator.simulateWitnessCalculation(primeRep)
			require.NoError(t, err)
			err = accumulator.storeWitness(m, witness)
			require.NoError(t, err)
		}

		// Verify they can all be verified
		for _, m := range measurements {
			result, err := accumulator.verifyMeasurement(m)
			require.NoError(t, err)
			assert.True(t, result, "Should verify measurement before persistence")
		}

		// Get the accumulated value for reference
		_ = accumulator.getCurrentAccumulatedValue()

		// Save witnesses to disk
		err = accumulator.saveWitnesses()
		require.NoError(t, err, "Should save without error")

		// Load accumulator again
		newAccumulator, err := GetAccumulatorClient(testAccumPath)
		require.NoError(t, err, "Should load accumulator")

		// Verify all measurements still work with new accumulator instance
		for _, m := range measurements {
			result, err := newAccumulator.verifyMeasurement(m)
			require.NoError(t, err)
			assert.True(t, result, "Should verify measurement after reload")
		}
	})

	t.Run("ConcurrentVerification", func(t *testing.T) {
		// Get accumulator
		accumulator, err := GetAccumulatorClient(testAccumPath)
		require.NoError(t, err)

		// Add a test measurement
		testMeasurement := []byte("concurrent-test")
		primeRep, err := MeasurementToPrimeRep(accumulator, testMeasurement)
		require.NoError(t, err)
		witness, err := accumulator.simulateWitnessCalculation(primeRep)
		require.NoError(t, err)
		err = accumulator.storeWitness(testMeasurement, witness)
		require.NoError(t, err)

		// Run multiple verifications concurrently
		concurrency := 10
		verificationCount := 100
		var wg sync.WaitGroup

		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func(threadID int) {
				defer wg.Done()

				for j := 0; j < verificationCount; j++ {
					// Verify via the public API
					result, err := AccumulatorVerifyMeasurement(testMeasurement, testAccumPath)
					if err != nil {
						t.Errorf("Thread %d: verification %d failed: %v", threadID, j, err)
					}
					if !result {
						t.Errorf("Thread %d: verification %d returned false", threadID, j)
					}

					// Small sleep to increase contention likelihood
					time.Sleep(time.Microsecond)
				}
			}(i)
		}

		wg.Wait()
	})
}

func TestAccumulatorPerformance(t *testing.T) {
	// Skip in short mode
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}
	
	// Create a temporary directory for test accumulator files
	tempDir, err := os.MkdirTemp("", "rsa_accumulator_perf_test")
	require.NoError(t, err, "Should create temp directory")
	defer os.RemoveAll(tempDir)
	
	// Set up test environment
	testAccumPath := filepath.Join(tempDir, "perf_accumulator.json")
	os.Setenv("TDX_ACCUMULATOR_PATH", testAccumPath)
	os.Setenv("TDX_ALLOW_DEFAULT_ACCUMULATOR", "true")
	os.Setenv("TDX_ALLOW_WITNESS_CALCULATION", "true")

	// Initialize a new accumulator
	accumulator, err := GetAccumulatorClient(testAccumPath)
	require.NoError(t, err)
	
	// Measure verification performance
	t.Run("VerificationPerformance", func(t *testing.T) {
		// Generate test measurements
		measurementCount := 10
		verificationCount := 100
		testMeasurements := make([][]byte, measurementCount)
		
		// Create test measurements
		for i := 0; i < measurementCount; i++ {
			// Generate unique measurement
			measurement := make([]byte, 48)
			measurement[0] = byte(i)
			measurement[1] = byte(i >> 8)
			rand.Read(measurement[2:])
			testMeasurements[i] = measurement
			
			// Create and store witness
			primeRep, err := MeasurementToPrimeRep(accumulator, measurement)
			require.NoError(t, err)
			witness, err := accumulator.simulateWitnessCalculation(primeRep)
			require.NoError(t, err)
			err = accumulator.storeWitness(measurement, witness)
			require.NoError(t, err)
		}
		
		// Measure total verification time
		start := time.Now()
		totalVerificationTimeNs := int64(0)
		
		for i := 0; i < verificationCount; i++ {
			// Pick a measurement (round-robin)
			idx := i % measurementCount
			measurement := testMeasurements[idx]
			
			// Verify measurement and time the operation
			startTime := time.Now()
			valid, err := accumulator.verifyMeasurement(measurement)
			verifyTime := time.Since(startTime)
			totalVerificationTimeNs += verifyTime.Nanoseconds()
			
			require.NoError(t, err)
			assert.True(t, valid, "Measurement should verify")
		}
		
		elapsed := time.Since(start)
		avgNs := totalVerificationTimeNs / int64(verificationCount)
		avgMs := float64(avgNs) / 1_000_000.0
		
		t.Logf("Verification performance: %.3f ms per verification (target: <1ms)", avgMs)
		t.Logf("Total time for %d verifications: %v", verificationCount, elapsed)
		
		assert.Less(t, avgMs, 1.0, "Verification should take less than 1ms")
	})
}
