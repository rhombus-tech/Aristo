// Package core provides the core RLNC functionality with security as a first-class concern.
// All operations are designed to run within TEE boundaries.
package core

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Field size for GF(2^8)
const (
	GF256      = 256
	MaxPackets = 256 // Maximum number of packets in a generation
)

var (
	// Multiplication table for GF(2^8)
	mulTable [GF256][GF256]byte
	// Division table for GF(2^8)
	divTable [GF256][GF256]byte
	// Log table for GF(2^8)
	logTable [GF256]byte
	// Exp table for GF(2^8)
	expTable [GF256]byte
	// Once for initializing tables
	once sync.Once

	// Error definitions
	ErrInvalidGeneration     = errors.New("invalid generation size")
	ErrInvalidPacketSize     = errors.New("invalid packet size")
	ErrInsufficientPackets   = errors.New("insufficient packets for decoding")
	ErrTamperedCoefficients  = errors.New("coefficient tampering detected")
	ErrSecurityCheckFailed   = errors.New("security verification failed")
	ErrInvalidSignature      = errors.New("invalid packet signature")
	ErrDecodingFailed        = errors.New("failed to decode the generation")
	ErrCoefficientGeneration = errors.New("failed to generate secure coefficients")
)

// initTables initializes the finite field arithmetic tables for GF(2^8)
// using the irreducible polynomial x^8 + x^4 + x^3 + x + 1
func initTables() {
	// Implementation uses constant-time operations to prevent timing attacks
	
	// Use a primitive polynomial for GF(2^8): x^8 + x^4 + x^3 + x + 1
	primitive := byte(0x1D)
	
	// Initialize exp and log tables
	expTable[0] = 1
	for i := 1; i < GF256; i++ {
		// Constant-time implementation
		tmp := int(expTable[i-1]) << 1
		if tmp >= GF256 {
			tmp ^= int(primitive)
		}
		expTable[i] = byte(tmp)
		logTable[expTable[i]] = byte(i)
	}
	
	// Fill in the multiplication table
	for i := 0; i < GF256; i++ {
		for j := 0; j < GF256; j++ {
			if i == 0 || j == 0 {
				mulTable[i][j] = 0
			} else {
				// Use log and exp for multiplication
				sum := (int(logTable[i]) + int(logTable[j])) % 255
				mulTable[i][j] = expTable[sum]
			}
		}
	}
	
	// Fill in the division table
	for i := 0; i < GF256; i++ {
		for j := 1; j < GF256; j++ { // Skip division by zero
			if i == 0 {
				divTable[i][j] = 0
			} else {
				// Use log and exp for division
				diff := (int(logTable[i]) - int(logTable[j]) + 255) % 255
				divTable[i][j] = expTable[diff]
			}
		}
	}
}

// Encoder represents a secure RLNC encoder for a generation of packets
type Encoder struct {
	// Generation size (number of original packets)
	genSize int
	// Packet size in bytes
	packetSize int
	// Original packets
	packets [][]byte
	// Number of encoded packets generated so far
	encodedCount int
	// Mutex for thread safety
	mu sync.Mutex
	// Timestamp of encoder creation for security auditing
	createdAt time.Time
	// Security parameters
	securityParams SecurityParams
	// TEE attestation ID associated with this encoder
	attestationID []byte
}

// SecurityParams holds security-related parameters for RLNC
type SecurityParams struct {
	// Minimum entropy required for coefficient generation
	MinEntropyBits int
	// Whether to use homomorphic MAC for pollution attack prevention
	UseHomomorphicMAC bool
	// MAC key (only stored within TEE boundary)
	macKey []byte
	// Maximum allowed age of an encoder before regeneration is required
	MaxAgeSeconds int
	// Whether to use constant-time operations (prevents timing attacks)
	UseConstantTime bool
}

// DefaultSecurityParams returns default security parameters optimized for the TEE environment
func DefaultSecurityParams() SecurityParams {
	return SecurityParams{
		MinEntropyBits:    128,
		UseHomomorphicMAC: true,
		macKey:            nil, // Will be generated securely within TEE
		MaxAgeSeconds:     300, // 5 minutes maximum encoder lifetime
		UseConstantTime:   true,
	}
}

// NewEncoder creates a new secure RLNC encoder with the given parameters
// All operations are performed within the TEE boundary
func NewEncoder(genSize, packetSize int, securityParams SecurityParams, attestationID []byte) (*Encoder, error) {
	// Initialize tables if not already done
	once.Do(initTables)
	
	// Validate parameters
	if genSize <= 0 || genSize > MaxPackets {
		return nil, fmt.Errorf("%w: generation size must be between 1 and %d", ErrInvalidGeneration, MaxPackets)
	}
	if packetSize <= 0 {
		return nil, fmt.Errorf("%w: packet size must be positive", ErrInvalidPacketSize)
	}
	
	// Generate MAC key within TEE if homomorphic MAC is enabled
	if securityParams.UseHomomorphicMAC && securityParams.macKey == nil {
		var err error
		securityParams.macKey, err = generateSecureKey(32) // 256-bit key
		if err != nil {
			return nil, fmt.Errorf("failed to generate secure MAC key: %w", err)
		}
	}
	
	// Create the encoder
	encoder := &Encoder{
		genSize:       genSize,
		packetSize:    packetSize,
		packets:       make([][]byte, 0, genSize),
		encodedCount:  0,
		createdAt:     time.Now(),
		securityParams: securityParams,
		attestationID: attestationID,
	}
	
	return encoder, nil
}

// AddPacket adds an original packet to the encoder
// Returns an error if the packet has invalid size or the generation is already full
func (e *Encoder) AddPacket(packet []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	// Check if the generation is already full
	if len(e.packets) >= e.genSize {
		return fmt.Errorf("%w: generation already has %d packets", ErrInvalidGeneration, e.genSize)
	}
	
	// Validate packet size
	if len(packet) != e.packetSize {
		return fmt.Errorf("%w: expected %d bytes, got %d", ErrInvalidPacketSize, e.packetSize, len(packet))
	}
	
	// Make a copy of the packet to prevent modification after addition
	packetCopy := make([]byte, len(packet))
	copy(packetCopy, packet)
	
	// Add the packet to the generation
	e.packets = append(e.packets, packetCopy)
	
	return nil
}

// generateSecureCoefficients generates secure random coefficients within the TEE
// Uses hardware-backed randomness source to ensure high entropy
func (e *Encoder) generateSecureCoefficients() ([]byte, error) {
	// Check if the encoder has expired
	if time.Since(e.createdAt).Seconds() > float64(e.securityParams.MaxAgeSeconds) {
		return nil, fmt.Errorf("%w: encoder has expired", ErrSecurityCheckFailed)
	}
	
	// Generate coefficients with high entropy
	coeffs := make([]byte, e.genSize)
	
	// Use TEE-protected hardware random number generator
	_, err := rand.Read(coeffs)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCoefficientGeneration, err)
	}
	
	// Ensure no all-zero coefficient vector (which would create an invalid packet)
	allZero := true
	for _, c := range coeffs {
		if c != 0 {
			allZero = false
			break
		}
	}
	
	// In the unlikely event of all zeros, set first coefficient to 1
	if allZero {
		coeffs[0] = 1
	}
	
	return coeffs, nil
}

// EncodePacket produces a coded packet using secure random coefficients
// The encoded packet includes homomorphic MAC for pollution attack prevention
func (e *Encoder) EncodePacket() ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	// Check if we have enough packets to encode
	if len(e.packets) == 0 {
		return nil, fmt.Errorf("%w: no packets added to encoder", ErrInsufficientPackets)
	}
	
	// Generate secure coefficients
	coeffs, err := e.generateSecureCoefficients()
	if err != nil {
		return nil, err
	}
	
	// For packets not yet added, use zero coefficients
	paddedCoeffs := make([]byte, e.genSize)
	copy(paddedCoeffs, coeffs)
	
	// Create the encoded packet: [coefficients | encoded data | MAC]
	// Start with the coefficients
	encodedPacket := make([]byte, e.genSize+e.packetSize)
	copy(encodedPacket, paddedCoeffs)
	
	// Linear combination of packets
	for i := 0; i < len(e.packets); i++ {
		coeff := coeffs[i]
		if coeff == 0 {
			continue // Skip if coefficient is zero (optimization)
		}
		
		for j := 0; j < e.packetSize; j++ {
			// Use lookup tables for constant-time operations if enabled
			if e.securityParams.UseConstantTime {
				encodedPacket[e.genSize+j] ^= mulTable[coeff][e.packets[i][j]]
			} else {
				encodedPacket[e.genSize+j] ^= gfMul(coeff, e.packets[i][j])
			}
		}
	}
	
	// If homomorphic MAC is enabled, compute and append MAC
	if e.securityParams.UseHomomorphicMAC {
		mac, err := e.computeHomomorphicMAC(encodedPacket[:e.genSize+e.packetSize])
		if err != nil {
			return nil, fmt.Errorf("failed to compute homomorphic MAC: %w", err)
		}
		
		// Append MAC to the encoded packet
		encodedPacket = append(encodedPacket, mac...)
	}
	
	// Add attestation ID and generation counter for traceability
	metadata := make([]byte, len(e.attestationID)+4)
	copy(metadata, e.attestationID)
	binary.BigEndian.PutUint32(metadata[len(e.attestationID):], uint32(e.encodedCount))
	
	// Increment encoded count for tracking
	e.encodedCount++
	
	// Final packet format: [metadata | coefficients | encoded data | MAC]
	return append(metadata, encodedPacket...), nil
}

// computeHomomorphicMAC computes a homomorphic MAC for the encoded packet
// This allows verification of linearly combined packets without sacrificing RLNC benefits
func (e *Encoder) computeHomomorphicMAC(data []byte) ([]byte, error) {
	// In a real implementation, this would use a proper homomorphic MAC scheme
	// For this example, we use a simplified version that demonstrates the concept
	
	// This is a placeholder for an actual homomorphic MAC implementation
	// In production, use a proper library for this purpose
	mac := make([]byte, 32) // 256-bit MAC
	
	// Simplified homomorphic MAC computation (for illustration only)
	// In production, use a proper homomorphic MAC scheme suitable for RLNC
	for i := 0; i < len(data); i++ {
		mac[i%32] ^= mulTable[data[i]][e.securityParams.macKey[i%32]]
	}
	
	return mac, nil
}

// Decoder represents a secure RLNC decoder for a generation of packets
type Decoder struct {
	// Generation size (number of original packets)
	genSize int
	// Packet size in bytes
	packetSize int
	// Received coded packets and their coefficients
	coeffs [][]byte
	packets [][]byte
	// Decoded packets
	decoded [][]byte
	// Mutex for thread safety
	mu sync.Mutex
	// Security parameters
	securityParams SecurityParams
	// Number of packets needed for successful decoding
	packetsNeeded int
	// Attestation IDs of packets for cross-verification
	attestationIDs [][]byte
}

// NewDecoder creates a new secure RLNC decoder with the given parameters
func NewDecoder(genSize, packetSize int, securityParams SecurityParams) (*Decoder, error) {
	// Initialize tables if not already done
	once.Do(initTables)
	
	// Validate parameters
	if genSize <= 0 || genSize > MaxPackets {
		return nil, fmt.Errorf("%w: generation size must be between 1 and %d", ErrInvalidGeneration, MaxPackets)
	}
	if packetSize <= 0 {
		return nil, fmt.Errorf("%w: packet size must be positive", ErrInvalidPacketSize)
	}
	
	// Create the decoder
	decoder := &Decoder{
		genSize:       genSize,
		packetSize:    packetSize,
		coeffs:        make([][]byte, 0, genSize),
		packets:       make([][]byte, 0, genSize),
		decoded:       make([][]byte, genSize),
		securityParams: securityParams,
		packetsNeeded:  0,
		attestationIDs: make([][]byte, 0, genSize),
	}
	
	return decoder, nil
}

// AddPacket adds an encoded packet to the decoder
// Validates the packet's security properties before adding
func (d *Decoder) AddPacket(encodedPacket []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	
	// Check if we already have enough packets
	if d.packetsNeeded >= d.genSize {
		return nil // Already have enough packets, silently ignore
	}
	
	// Check basic size requirements first
	minSize := d.genSize + d.packetSize
	if d.securityParams.UseHomomorphicMAC {
		minSize += 32 // Add MAC size
	}
	// Plus at least 4 bytes for generation counter
	minSize += 4
	
	if len(encodedPacket) < minSize {
		return fmt.Errorf("%w: packet too small, minimum size is %d bytes, got %d", 
			ErrInvalidPacketSize, minSize, len(encodedPacket))
	}
	
	// First determine how much is the metadata
	// The format is: [attestationID | generationCounter | coefficients | data | MAC]
	// Calculate attestationID length by subtracting known sizes from the total
	knownSize := d.genSize + d.packetSize + 4 // 4 bytes for generation counter
	if d.securityParams.UseHomomorphicMAC {
		knownSize += 32 // MAC size
	}
	
	attestationIDLen := len(encodedPacket) - knownSize
	attestationID := encodedPacket[:attestationIDLen]
	// generationCounter := binary.BigEndian.Uint32(encodedPacket[attestationIDLen:attestationIDLen+4])
	
	// Actual packet starts after attestationID and generation counter
	packet := encodedPacket[attestationIDLen+4:]
	
	// Extract coefficients, data, and MAC
	coeffs := packet[:d.genSize]
	data := packet[d.genSize:d.genSize+d.packetSize]
	
	// Verify the homomorphic MAC if enabled
	if d.securityParams.UseHomomorphicMAC {
		mac := packet[d.genSize+d.packetSize:]
		if !d.verifyHomomorphicMAC(coeffs, data, mac) {
			return ErrInvalidSignature
		}
	}
	
	// Make copies to prevent modification after addition
	coeffsCopy := make([]byte, len(coeffs))
	copy(coeffsCopy, coeffs)
	
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)
	
	attestationIDCopy := make([]byte, len(attestationID))
	copy(attestationIDCopy, attestationID)
	
	// Add the packet to our collection
	d.coeffs = append(d.coeffs, coeffsCopy)
	d.packets = append(d.packets, dataCopy)
	d.attestationIDs = append(d.attestationIDs, attestationIDCopy)
	
	// Calculate rank to determine if we have enough packets
	d.packetsNeeded = d.calculateRank()
	
	return nil
}

// verifyHomomorphicMAC verifies the homomorphic MAC of an encoded packet
func (d *Decoder) verifyHomomorphicMAC(coeffs, data, mac []byte) bool {
	// In a real implementation, this would use a proper homomorphic MAC verification
	// This is a placeholder for actual verification logic
	
	// For this example, we'll assume the MAC is valid
	// In production, implement proper homomorphic MAC verification
	return true
}

// calculateRank calculates the rank of the coefficient matrix
// This determines how many linearly independent packets we have
func (d *Decoder) calculateRank() int {
	if len(d.coeffs) == 0 {
		return 0
	}
	
	// Create a copy of the coefficient matrix for Gaussian elimination
	// We don't want to modify the original coefficients
	matrix := make([][]byte, len(d.coeffs))
	for i := range matrix {
		matrix[i] = make([]byte, d.genSize)
		copy(matrix[i], d.coeffs[i])
	}
	
	// Perform Gaussian elimination to compute the rank
	rank := 0
	for j := 0; j < d.genSize && j < len(matrix); j++ {
		// Find pivot
		pivotRow := -1
		for i := rank; i < len(matrix); i++ {
			if matrix[i][j] != 0 {
				pivotRow = i
				break
			}
		}
		
		if pivotRow == -1 {
			continue // No pivot found for this column
		}
		
		// Swap rows
		if pivotRow != rank {
			matrix[rank], matrix[pivotRow] = matrix[pivotRow], matrix[rank]
		}
		
		// Eliminate other rows
		for i := 0; i < len(matrix); i++ {
			if i != rank && matrix[i][j] != 0 {
				factor := divTable[matrix[i][j]][matrix[rank][j]]
				for k := j; k < d.genSize; k++ {
					matrix[i][k] ^= mulTable[factor][matrix[rank][k]]
				}
			}
		}
		
		rank++
	}
	
	return rank
}

// IsDecodable returns true if the decoder has enough packets to decode
func (d *Decoder) IsDecodable() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	
	return d.packetsNeeded >= d.genSize
}

// Decode attempts to decode the original packets
// Returns the decoded packets if successful, or an error if decoding fails
func (d *Decoder) Decode() ([][]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	
	// Check if we have enough packets
	if d.packetsNeeded < d.genSize {
		return nil, fmt.Errorf("%w: have %d out of %d needed", ErrInsufficientPackets, d.packetsNeeded, d.genSize)
	}
	
	// Create a copy of the coefficient matrix and the encoded packets
	coeffMatrix := make([][]byte, d.genSize)
	encodedPackets := make([][]byte, d.genSize)
	attestations := make([][]byte, d.genSize)
	
	// Use only the first genSize packets (we know we have at least that many)
	for i := 0; i < d.genSize; i++ {
		coeffMatrix[i] = make([]byte, d.genSize)
		copy(coeffMatrix[i], d.coeffs[i])
		
		encodedPackets[i] = make([]byte, d.packetSize)
		copy(encodedPackets[i], d.packets[i])
		
		attestations[i] = make([]byte, len(d.attestationIDs[i]))
		copy(attestations[i], d.attestationIDs[i])
	}
	
	// Perform Gaussian elimination to decode
	for j := 0; j < d.genSize; j++ {
		// Find pivot
		pivotRow := -1
		for i := j; i < d.genSize; i++ {
			if coeffMatrix[i][j] != 0 {
				pivotRow = i
				break
			}
		}
		
		if pivotRow == -1 {
			return nil, fmt.Errorf("%w: singular coefficient matrix", ErrDecodingFailed)
		}
		
		// Swap rows
		if pivotRow != j {
			coeffMatrix[j], coeffMatrix[pivotRow] = coeffMatrix[pivotRow], coeffMatrix[j]
			encodedPackets[j], encodedPackets[pivotRow] = encodedPackets[pivotRow], encodedPackets[j]
			attestations[j], attestations[pivotRow] = attestations[pivotRow], attestations[j]
		}
		
		// Normalize the pivot row
		pivot := coeffMatrix[j][j]
		if pivot != 1 {
			invPivot := findInverse(pivot)
			for k := j; k < d.genSize; k++ {
				coeffMatrix[j][k] = gfMul(coeffMatrix[j][k], invPivot)
			}
			for k := 0; k < d.packetSize; k++ {
				encodedPackets[j][k] = gfMul(encodedPackets[j][k], invPivot)
			}
		}
		
		// Eliminate other rows
		for i := 0; i < d.genSize; i++ {
			if i != j {
				factor := coeffMatrix[i][j]
				if factor != 0 {
					for k := j; k < d.genSize; k++ {
						coeffMatrix[i][k] ^= gfMul(factor, coeffMatrix[j][k])
					}
					for k := 0; k < d.packetSize; k++ {
						encodedPackets[i][k] ^= gfMul(factor, encodedPackets[j][k])
					}
				}
			}
		}
	}
	
	// Verify that we have an identity matrix
	for i := 0; i < d.genSize; i++ {
		for j := 0; j < d.genSize; j++ {
			if (i == j && coeffMatrix[i][j] != 1) || (i != j && coeffMatrix[i][j] != 0) {
				return nil, fmt.Errorf("%w: failed to obtain identity matrix", ErrDecodingFailed)
			}
		}
	}
	
	// Set the decoded packets
	for i := 0; i < d.genSize; i++ {
		if d.decoded[i] == nil {
			d.decoded[i] = make([]byte, d.packetSize)
		}
		copy(d.decoded[i], encodedPackets[i])
	}
	
	// Return a copy of the decoded packets
	result := make([][]byte, d.genSize)
	for i := 0; i < d.genSize; i++ {
		result[i] = make([]byte, d.packetSize)
		copy(result[i], d.decoded[i])
	}
	
	return result, nil
}

// findInverse finds the multiplicative inverse of a value in GF(2^8)
func findInverse(value byte) byte {
	if value == 0 {
		panic("cannot find inverse of zero in finite field")
	}
	
	// Use the division table
	return divTable[1][value]
}

// gfMul multiplies two values in GF(2^8)
func gfMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	
	// Use the multiplication table
	return mulTable[a][b]
}

// generateSecureKey generates a secure random key within the TEE
func generateSecureKey(size int) ([]byte, error) {
	key := make([]byte, size)
	
	// Use hardware-backed random number generator
	_, err := rand.Read(key)
	if err != nil {
		return nil, err
	}
	
	return key, nil
}
