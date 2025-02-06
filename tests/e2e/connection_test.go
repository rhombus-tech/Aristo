package e2e

import (
    "context"
    "testing"
    "time"
    "os"
    "os/exec"
    "path/filepath"

    "github.com/stretchr/testify/require"
    "google.golang.org/grpc"
    
    "github.com/rhombus-tech/vm/tee/proto"
)

func TestConnectionFlow(t *testing.T) {
    // Step 1: Build the test contract
    cmd := exec.Command("cargo", "build", "--target", "wasm32-unknown-unknown", "--release")
    cmd.Dir = filepath.Join("..", "contracts", "simple_add")
    err := cmd.Run()
    require.NoError(t, err, "Failed to build test contract")

    // Step 2: Start the TEE execution service
    teeCmd := exec.Command("cargo", "run", "--bin", "tee-controller")
    teeCmd.Dir = filepath.Join("..", "..", "execution", "controller")
    err = teeCmd.Start()
    require.NoError(t, err, "Failed to start TEE service")
    defer teeCmd.Process.Kill()

    // Wait for service to start
    time.Sleep(2 * time.Second)

    // Step 3: Connect to TEE service
    conn, err := grpc.Dial("localhost:50051", grpc.WithInsecure())
    require.NoError(t, err, "Failed to connect to TEE service")
    defer conn.Close()

    teeClient := proto.NewTeeExecutionClient(conn)
    ctx := context.Background()

    // Step 4: Get TEE attestations
    attestReq := &proto.GetAttestationsRequest{
        RegionId: "default",
    }
    attestResp, err := teeClient.GetAttestations(ctx, attestReq)
    require.NoError(t, err, "Failed to get attestations")
    require.NotEmpty(t, attestResp.Attestations, "No attestations returned")

    // Step 5: Read the WASM contract
    wasmPath := filepath.Join("..", "contracts", "simple_add", "target", "wasm32-unknown-unknown", "release", "simple_add.wasm")
    wasmBytes, err := os.ReadFile(wasmPath)
    require.NoError(t, err, "Failed to read WASM file")

    // Step 6: Execute the contract
    // Combine WASM code and parameters
    requestParams := append(wasmBytes, []byte(",5,10")...)
    execReq := &proto.ExecutionRequest{
        IdTo: "12345",  
        FunctionCall: "add",
        Parameters: requestParams,
        RegionId: "default",
        DetailedProof: true,
    }
    execResp, err := teeClient.Execute(ctx, execReq)
    require.NoError(t, err, "Failed to execute contract")
    require.NotNil(t, execResp, "No response returned")

    // Step 7: Verify the result
    // The result should be 15 (5 + 10), encoded as bytes
    expectedOutput := []byte{15, 0, 0, 0, 0, 0, 0, 0} // uint64(15) in little-endian
    require.Equal(t, expectedOutput, execResp.Result, "Unexpected result")

    // Step 8: Verify attestation chain
    require.NotEmpty(t, execResp.Attestations, "No attestations in response")

    t.Log("All connections verified successfully!")
}
