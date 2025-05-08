// Copyright (C) 2025, Rhombus Technologies. All rights reserved.
// See the file LICENSE for licensing terms.

package archive

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMeshClient_SecureCommit tests the dual execution of secure commit on both SGX and SEV
func TestMeshClient_SecureCommit(t *testing.T) {
	// Set up mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Parse request
		var req MeshExecuteRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"error": "invalid request: %v"}`, err)
			return
		}
		
		// Verify that it's using both SGX and SEV in parallel
		assert.Equal(t, "parallel", req.ExecutionMode, "Should be using parallel execution")
		assert.Equal(t, string(TEETypeIntelSGX), req.PrimaryTEEType, "Primary TEE should be SGX")
		assert.Equal(t, string(TEETypeSEV), req.SecondaryTEEType, "Secondary TEE should be SEV")
		
		// Mock a commitment response
		commitment := make([]byte, 32)
		for i := range commitment {
			commitment[i] = byte(i)
		}
		
		// Create properly formatted mock attestation data
		// Format: [tee_type_size(4)][tee_type][attestation_data]
		teeTypeStr := string(TEETypeIntelSGX)
		attestationData := make([]byte, 32)
		for i := range attestationData {
			attestationData[i] = byte(i + 32)
		}
		
		teeTypeBytes := []byte(teeTypeStr)
		attestation := make([]byte, 4+len(teeTypeBytes)+len(attestationData))
		binary.LittleEndian.PutUint32(attestation[0:4], uint32(len(teeTypeBytes)))
		copy(attestation[4:4+len(teeTypeBytes)], teeTypeBytes)
		copy(attestation[4+len(teeTypeBytes):], attestationData)
		
		// Format the result as the TEE would: [commitment_size(4)][commitment][attestation_size(4)][attestation]
		// IMPORTANT: The MeshClient expects the attestation size at the end of the byte array
		offset := 4 + len(commitment) // Skip past commitment size and commitment
		result := make([]byte, offset+4+len(attestation))
		
		// Write commitment size at beginning
		binary.LittleEndian.PutUint32(result[0:4], uint32(len(commitment)))
		
		// Copy commitment after size
		copy(result[4:offset], commitment)
		
		// Write attestation size after commitment
		binary.LittleEndian.PutUint32(result[offset:offset+4], uint32(len(attestation)))
		
		// Copy attestation after size
		copy(result[offset+4:], attestation)
		
		// Send response
		resp := map[string]interface{}{
			"result": result,
			"executor_tee_type": string(TEETypeIntelSGX),
			"verifier_tee_type": string(TEETypeSEV),
			"metrics": map[string]interface{}{
				"latency_ms": 15,
			},
			"error": "",
		}
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()
	
	// Create a mesh client
	config := MeshClientConfig{
		ConnectionTimeout:       5 * time.Second,
		MaxRetries:              1,
		RegionalPreference:      true,
		CircuitBreakerThreshold: 3,
		AttestationCacheTTL:     1 * time.Minute,
		PreferredTEETypes:       []TEEType{TEETypeIntelSGX, TEETypeSEV},
	}
	client := NewMeshClient(server.URL, "us-east-1", config)
	
	// Create state roots for the polynomial
	stateRoots := [][]byte{
		make([]byte, 32), // Root 1
		make([]byte, 32), // Root 2
	}
	for i := range stateRoots[1] {
		stateRoots[1][i] = byte(i)
	}
	
	// Test SecureCommit with the mesh client
	commitment, attestation, err := client.SecureCommit(context.Background(), stateRoots, 1)
	
	// Verify results
	require.NoError(t, err, "SecureCommit should not return an error")
	require.NotNil(t, commitment, "Commitment should not be nil")
	require.NotNil(t, attestation, "Attestation should not be nil")
	require.Equal(t, 32, len(commitment), "Commitment should be 32 bytes")
	
	t.Logf("Successfully executed SecureCommit with dual TEE verification")
}

// TestTEEPolynomialCircuit_WithMeshClient tests the TEEPolynomialCircuit with the mesh client
func TestTEEPolynomialCircuit_WithMeshClient(t *testing.T) {
	// Set up mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Parse request
		var req MeshExecuteRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"error": "invalid request: %v"}`, err)
			return
		}
		
		// Mock a response based on the operation
		switch req.Operation {
		case "SecureCommit":
			// Mock a commitment response
			commitment := make([]byte, 32)
			for i := range commitment {
				commitment[i] = byte(i)
			}
			
			// Create properly formatted mock attestation data
			// Format: [tee_type_size(4)][tee_type][attestation_data]
			teeTypeStr := string(TEETypeIntelSGX)
			attestationData := make([]byte, 32)
			for i := range attestationData {
				attestationData[i] = byte(i + 32)
			}
			
			teeTypeBytes := []byte(teeTypeStr)
			attestation := make([]byte, 4+len(teeTypeBytes)+len(attestationData))
			binary.LittleEndian.PutUint32(attestation[0:4], uint32(len(teeTypeBytes)))
			copy(attestation[4:4+len(teeTypeBytes)], teeTypeBytes)
			copy(attestation[4+len(teeTypeBytes):], attestationData)
			
			// Format the result as the TEE would: [commitment_size(4)][commitment][attestation_size(4)][attestation]
			// IMPORTANT: The MeshClient expects the attestation size at the end of the byte array
			offset := 4 + len(commitment) // Skip past commitment size and commitment
			result := make([]byte, offset+4+len(attestation))
			
			// Write commitment size at beginning
			binary.LittleEndian.PutUint32(result[0:4], uint32(len(commitment)))
			
			// Copy commitment after size
			copy(result[4:offset], commitment)
			
			// Write attestation size after commitment
			binary.LittleEndian.PutUint32(result[offset:offset+4], uint32(len(attestation)))
			
			// Copy attestation after size
			copy(result[offset+4:], attestation)
			
			resp := map[string]interface{}{
				"result": result,
				"executor_tee_type": string(TEETypeIntelSGX),
				"verifier_tee_type": string(TEETypeSEV),
				"metrics": map[string]interface{}{
					"latency_ms": 15,
				},
				"error": "",
			}
			
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			
		case "SecureOpenAtPoint":
			// Mock a verification response (true)
			verifyResult := []byte{1} // 1 = valid, 0 = invalid
			
			// Create properly formatted mock attestation data
			// Format: [tee_type_size(4)][tee_type][attestation_data]
			teeTypeStr := string(TEETypeIntelSGX)
			attestationData := make([]byte, 32)
			for i := range attestationData {
				attestationData[i] = byte(i + 32)
			}
			
			teeTypeBytes := []byte(teeTypeStr)
			attestation := make([]byte, 4+len(teeTypeBytes)+len(attestationData))
			binary.LittleEndian.PutUint32(attestation[0:4], uint32(len(teeTypeBytes)))
			copy(attestation[4:4+len(teeTypeBytes)], teeTypeBytes)
			copy(attestation[4+len(teeTypeBytes):], attestationData)
			
			// Format the result: [valid(1)][attestation_size(4)][attestation]
			// MeshClient expects: result byte followed by attestation size and data
			result := make([]byte, 1+4+len(attestation))
			
			// Set result byte (1 = valid)
			result[0] = verifyResult[0] // valid = true
			
			// Add attestation size
			binary.LittleEndian.PutUint32(result[1:5], uint32(len(attestation)))
			
			// Copy attestation after size
			copy(result[5:], attestation)
			
			resp := map[string]interface{}{
				"result": result,
				"executor_tee_type": string(TEETypeIntelSGX),
				"verifier_tee_type": string(TEETypeSEV),
				"metrics": map[string]interface{}{
					"latency_ms": 15,
				},
				"error": "",
			}
			
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer server.Close()
	
	// Create a TEEPolynomialCircuit with mesh client
	circuit := NewMeshTEEPolynomialCircuit(server.URL, "us-east-1",
		WithMaxBatchSize(100),
		WithAcceleration(true),
		WithMeshNetwork(true),
		WithRegion("us-east-1"))
	
	// Test polynomial operations
	ctx := context.Background()
	
	// Create mock matrix directly instead of calling encodeBlocksToMatrix
	matrix := createMockMatrix()
	
	// Test polynomial commitment
	
	commitment, attestation, err := circuit.callTEESecureCommit(ctx, matrix)
	require.NoError(t, err, "Should commit to polynomial")
	require.NotNil(t, commitment, "Commitment should not be nil")
	require.NotNil(t, attestation, "Attestation should not be nil")
	
	// Test polynomial verification
	point := make([]byte, 32)
	for i := range point {
		point[i] = byte(i)
	}
	valid, err := circuit.callTEESecureOpenAtPoint(ctx, commitment, point)
	require.NoError(t, err, "Should verify polynomial commitment")
	require.True(t, valid, "Verification should be valid")
	
	t.Logf("Successfully tested TEEPolynomialCircuit with dual TEE execution")
}

// Helper function to create a mock matrix for testing
func createMockMatrix() []byte {
	// Create a mock matrix
	matrix := make([]byte, 100)
	// Set rows and columns
	binary.LittleEndian.PutUint32(matrix[0:4], 2) // 2 rows
	binary.LittleEndian.PutUint32(matrix[4:8], 3) // 3 columns
	
	// Fill with sample data
	for i := 8; i < 100; i++ {
		matrix[i] = byte(i % 256)
	}
	
	return matrix
}
