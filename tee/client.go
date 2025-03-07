// File: tee/client.go
package tee

import (
    "bytes"
    "context"
    "fmt"
    "time"

    "google.golang.org/grpc"

    "github.com/rhombus-tech/vm/tee/proto"
    "github.com/rhombus-tech/vm/actions"
    "github.com/rhombus-tech/vm/core"
    "github.com/rhombus-tech/vm/verifier"
)

const (
    TEEPairStatusActive   = "active"
    TEEPairStatusDegraded = "degraded"
    TEEPairStatusFailed   = "failed"
)

// TEEPair holds SGX and SEV clients for a region
type TEEPair struct {
    sgxClient proto.TeeExecutionClient
    sevClient proto.TeeExecutionClient
    sgxConn   *grpc.ClientConn
    sevConn   *grpc.ClientConn
}

// Client holds TEE connections and a verifier
type Client struct {
    // Default (non-regional) TEE clients
    sgxClient proto.TeeExecutionClient
    sevClient proto.TeeExecutionClient
    sgxConn   *grpc.ClientConn
    sevConn   *grpc.ClientConn

    // Regional TEE clients
    regionTEEs map[string]*TEEPair

    verifier *verifier.StateVerifier
    
    // Default timeout for client operations
    defaultTimeout time.Duration
}

func CreateTEEPair(config *TEEPairConfig) (*TEEPair, error) {
    // Connect to SGX endpoint
    sgxConn, err := grpc.Dial(config.SGXEndpoint, grpc.WithInsecure())
    if err != nil {
        return nil, fmt.Errorf("failed to connect to SGX: %w", err)
    }

    // Connect to SEV endpoint
    sevConn, err := grpc.Dial(config.SEVEndpoint, grpc.WithInsecure())
    if err != nil {
        // If SEV fails, close SGX too
        _ = sgxConn.Close()
        return nil, fmt.Errorf("failed to connect to SEV: %w", err)
    }

    return &TEEPair{
        sgxClient: proto.NewTeeExecutionClient(sgxConn),
        sevClient: proto.NewTeeExecutionClient(sevConn),
        sgxConn:   sgxConn,
        sevConn:   sevConn,
    }, nil
}

// NewClient creates a new client with default TEE connections
func NewClient(
    sgxEndpoint, sevEndpoint string,
    v *verifier.StateVerifier,
) (*Client, error) {
    // Connect to the SGX TEE
    sgxConn, err := grpc.Dial(sgxEndpoint, grpc.WithInsecure())
    if err != nil {
        return nil, fmt.Errorf("failed to dial SGX: %w", err)
    }

    // Connect to the SEV TEE
    sevConn, err := grpc.Dial(sevEndpoint, grpc.WithInsecure())
    if err != nil {
        // If SEV fails, close SGX too:
        _ = sgxConn.Close()
        return nil, fmt.Errorf("failed to dial SEV: %w", err)
    }

    client := &Client{
        sgxClient:  proto.NewTeeExecutionClient(sgxConn),
        sevClient:  proto.NewTeeExecutionClient(sevConn),
        sgxConn:    sgxConn,
        sevConn:    sevConn,
        verifier:   v,
        regionTEEs: make(map[string]*TEEPair),
    }
    return client, nil
}

// NewClientWithConnections creates a new client using existing gRPC connections.
// This is particularly useful for testing.
func NewClientWithConnections(sgxConn, sevConn *grpc.ClientConn, stateVerifier *verifier.StateVerifier) (*Client, error) {
    sgxClient := proto.NewTeeExecutionClient(sgxConn)
    sevClient := proto.NewTeeExecutionClient(sevConn)

    client := &Client{
        sgxClient:      sgxClient,
        sevClient:      sevClient,
        sgxConn:        sgxConn,
        sevConn:        sevConn,
        regionTEEs:     make(map[string]*TEEPair),
        verifier:       stateVerifier,
        defaultTimeout: 30 * time.Second,
    }
    return client, nil
}

// AddRegion adds a new region's TEE endpoints
func (c *Client) AddRegion(
    regionID string,
    sgxEndpoint string,
    sevEndpoint string,
) error {
    // Connect to the SGX TEE
    sgxConn, err := grpc.Dial(sgxEndpoint, grpc.WithInsecure())
    if err != nil {
        return fmt.Errorf("failed to dial SGX for region %s: %w", regionID, err)
    }

    // Connect to the SEV TEE
    sevConn, err := grpc.Dial(sevEndpoint, grpc.WithInsecure())
    if err != nil {
        // If SEV fails, close SGX too
        _ = sgxConn.Close()
        return fmt.Errorf("failed to dial SEV for region %s: %w", regionID, err)
    }

    c.regionTEEs[regionID] = &TEEPair{
        sgxClient: proto.NewTeeExecutionClient(sgxConn),
        sevClient: proto.NewTeeExecutionClient(sevConn),
        sgxConn:   sgxConn,
        sevConn:   sevConn,
    }

    return nil
}

// Close closes all TEE connections
func (c *Client) Close() error {
    var errs []error
    
    // Close default connections
    if err := c.sgxConn.Close(); err != nil {
        errs = append(errs, fmt.Errorf("closing default SGX: %w", err))
    }
    if err := c.sevConn.Close(); err != nil {
        errs = append(errs, fmt.Errorf("closing default SEV: %w", err))
    }

    // Close regional connections
    for regionID, pair := range c.regionTEEs {
        if err := pair.sgxConn.Close(); err != nil {
            errs = append(errs, fmt.Errorf("closing SGX for region %s: %w", regionID, err))
        }
        if err := pair.sevConn.Close(); err != nil {
            errs = append(errs, fmt.Errorf("closing SEV for region %s: %w", regionID, err))
        }
    }

    if len(errs) > 0 {
        return fmt.Errorf("TEE client close errors: %v", errs)
    }
    return nil
}

// ExecuteAction maintains original functionality while adding regional support
func (c *Client) ExecuteAction(ctx context.Context, action *actions.SendEventAction) error {
    // Build the request proto
    req := &proto.ExecutionRequest{
        IdTo:         action.IDTo,
        FunctionCall: action.FunctionCall,
        Parameters:   action.Parameters,
        RegionId:     action.RegionID,
    }

    var sgxResult, sevResult *proto.ExecutionResult
    var err error

    // Check if this is a regional execution
    if action.RegionID != "" {
        if pair, ok := c.regionTEEs[action.RegionID]; ok {
            // Execute in specific region
            sgxResult, err = pair.sgxClient.Execute(ctx, req)
            if err != nil {
                return fmt.Errorf("regional SGX Execute failed: %w", err)
            }

            sevResult, err = pair.sevClient.Execute(ctx, req)
            if err != nil {
                return fmt.Errorf("regional SEV Execute failed: %w", err)
            }
        }
    }

    // Fall back to default TEE clients if no region specified or not found
    if sgxResult == nil {
        sgxResult, err = c.sgxClient.Execute(ctx, req)
        if err != nil {
            return fmt.Errorf("SGX Execute failed: %w", err)
        }

        sevResult, err = c.sevClient.Execute(ctx, req)
        if err != nil {
            return fmt.Errorf("SEV Execute failed: %w", err)
        }
    }

    // Convert attestations and verify
    sgxAtts, err := protoToCoreAttestations(sgxResult.Attestations)
    if err != nil {
        return fmt.Errorf("failed to convert SGX attestations: %w", err)
    }
    
    sevAtts, err := protoToCoreAttestations(sevResult.Attestations)
    if err != nil {
        return fmt.Errorf("failed to convert SEV attestations: %w", err)
    }

    // Verify both attestation sets
    if err := c.verifier.VerifyAttestationPair(ctx, sgxAtts, nil); err != nil {
        return fmt.Errorf("SGX attestation verify failed: %w", err)
    }
    if err := c.verifier.VerifyAttestationPair(ctx, sevAtts, nil); err != nil {
        return fmt.Errorf("SEV attestation verify failed: %w", err)
    }

    // Compare results to ensure they match
    if err := c.compareResults(sgxResult, sevResult); err != nil {
        return err
    }

    return nil
}

func (c *Client) compareResults(sgxRes, sevRes *proto.ExecutionResult) error {
    if !bytes.Equal(sgxRes.StateHash, sevRes.StateHash) {
        return fmt.Errorf("state hash mismatch between SGX and SEV results")
    }
    if !bytes.Equal(sgxRes.Result, sevRes.Result) {
        return fmt.Errorf("execution result mismatch between SGX and SEV")
    }
    return nil
}

// DeployContract deploys a WebAssembly contract to both SGX and SEV TEEs
// in the specified region (or default if regionID is empty).
// It verifies attestations and ensures results from both TEEs match.
func (c *Client) DeployContract(
    ctx context.Context,
    contractCode []byte,
    initArgs []byte,
    regionID string,
    contractName string,
) (string, error) {
    // Validate parameters
    if len(contractCode) == 0 {
        return "", fmt.Errorf("contract code cannot be empty")
    }
    
    // Build the request proto
    req := &proto.DeployContractRequest{
        ContractCode:   contractCode,
        InitArgs:       initArgs,
        RegionId:       regionID,
        ContractName:   contractName,
        DetailedProof:  true, // Enable detailed proof by default
    }

    var sgxResult, sevResult *proto.DeployContractResponse
    var err error

    // Check if this is a regional deployment
    if regionID != "" {
        if pair, ok := c.regionTEEs[regionID]; ok {
            // Deploy in specific region
            sgxResult, err = pair.sgxClient.DeployContract(ctx, req)
            if err != nil {
                return "", fmt.Errorf("regional SGX DeployContract failed: %w", err)
            }

            sevResult, err = pair.sevClient.DeployContract(ctx, req)
            if err != nil {
                return "", fmt.Errorf("regional SEV DeployContract failed: %w", err)
            }
        }
    }

    // Fall back to default TEE clients if no region specified or not found
    if sgxResult == nil {
        sgxResult, err = c.sgxClient.DeployContract(ctx, req)
        if err != nil {
            return "", fmt.Errorf("SGX DeployContract failed: %w", err)
        }

        sevResult, err = c.sevClient.DeployContract(ctx, req)
        if err != nil {
            return "", fmt.Errorf("SEV DeployContract failed: %w", err)
        }
    }

    // Convert attestations and verify
    sgxAtts, err := protoToCoreAttestations(sgxResult.Attestations)
    if err != nil {
        return "", fmt.Errorf("failed to convert SGX attestations: %w", err)
    }
    
    sevAtts, err := protoToCoreAttestations(sevResult.Attestations)
    if err != nil {
        return "", fmt.Errorf("failed to convert SEV attestations: %w", err)
    }

    // Verify both attestation sets
    if err := c.verifier.VerifyAttestationPair(ctx, sgxAtts, nil); err != nil {
        return "", fmt.Errorf("SGX attestation verify failed: %w", err)
    }
    if err := c.verifier.VerifyAttestationPair(ctx, sevAtts, nil); err != nil {
        return "", fmt.Errorf("SEV attestation verify failed: %w", err)
    }

    // Compare results to ensure they match
    if err := c.compareDeployResults(sgxResult, sevResult); err != nil {
        return "", err
    }

    // Return the contract ID
    return sgxResult.ContractId, nil
}

// Helper method to compare deployment results from SGX and SEV
func (c *Client) compareDeployResults(sgxRes, sevRes *proto.DeployContractResponse) error {
    if sgxRes.ContractId != sevRes.ContractId {
        return fmt.Errorf("contract ID mismatch between SGX and SEV results")
    }
    if !bytes.Equal(sgxRes.StateHash, sevRes.StateHash) {
        return fmt.Errorf("state hash mismatch between SGX and SEV results")
    }
    return nil
}

// CallContract calls a function on a deployed contract in both SGX and SEV TEEs.
// It verifies that the results from both TEEs match and returns the result.
func (c *Client) CallContract(ctx context.Context, contractID, functionName string, params []byte, regionID string) ([]byte, error) {
    // Create the request
    request := &proto.CallContractRequest{
        ContractId:    contractID,
        FunctionName:  functionName,
        Parameters:    params,
        RegionId:      regionID,
        DetailedProof: true,
    }

    // Determine which TEE clients to use based on regionID
    var sgxClient, sevClient proto.TeeExecutionClient
    if regionID != "" {
        region, exists := c.regionTEEs[regionID]
        if !exists {
            return nil, fmt.Errorf("region %s not found", regionID)
        }
        sgxClient = region.sgxClient
        sevClient = region.sevClient
    } else {
        sgxClient = c.sgxClient
        sevClient = c.sevClient
    }

    // Set a timeout if the context doesn't have one
    var cancel context.CancelFunc
    if _, hasDeadline := ctx.Deadline(); !hasDeadline {
        ctx, cancel = context.WithTimeout(ctx, c.defaultTimeout)
        defer cancel()
    }

    // Call the function on SGX TEE
    sgxResp, err := sgxClient.CallContract(ctx, request)
    if err != nil {
        return nil, fmt.Errorf("SGX contract call failed: %w", err)
    }

    // Call the function on SEV TEE
    sevResp, err := sevClient.CallContract(ctx, request)
    if err != nil {
        return nil, fmt.Errorf("SEV contract call failed: %w", err)
    }

    // Compare the results
    if err := c.compareCallResults(sgxResp, sevResp); err != nil {
        return nil, err
    }

    // Verify attestations
    attestations := [2]core.TEEAttestation{}
    for i, att := range sgxResp.Attestations {
        if i >= 2 {
            break
        }
        
        // Parse timestamp string to time.Time
        timestamp, err := time.Parse(time.RFC3339, att.Timestamp)
        if err != nil {
            return nil, fmt.Errorf("failed to parse attestation timestamp: %w", err)
        }
        
        attestations[i] = core.TEEAttestation{
            EnclaveID:  att.EnclaveId,
            Measurement: att.Measurement,
            Timestamp:  timestamp,
        }
    }
    
    // Verify attestation pair
    if err := c.verifier.VerifyAttestationPair(ctx, attestations, nil); err != nil {
        return nil, fmt.Errorf("attestation verification failed: %w", err)
    }

    // Return the result (using SGX response as the canonical one)
    return sgxResp.Result, nil
}

// compareCallResults compares the results from SGX and SEV TEEs to ensure they match.
func (c *Client) compareCallResults(sgxResp, sevResp *proto.CallContractResponse) error {
    // Compare state hashes
    if !bytes.Equal(sgxResp.StateHash, sevResp.StateHash) {
        return fmt.Errorf("state hash mismatch: SGX %x, SEV %x", sgxResp.StateHash, sevResp.StateHash)
    }

    // Compare results
    if !bytes.Equal(sgxResp.Result, sevResp.Result) {
        return fmt.Errorf("result mismatch: SGX %x, SEV %x", sgxResp.Result, sevResp.Result)
    }

    return nil
}

func (p *TEEPair) Close() error {
    var errs []error
    if p.sgxConn != nil {
        if err := p.sgxConn.Close(); err != nil {
            errs = append(errs, fmt.Errorf("failed to close SGX connection: %w", err))
        }
    }
    if p.sevConn != nil {
        if err := p.sevConn.Close(); err != nil {
            errs = append(errs, fmt.Errorf("failed to close SEV connection: %w", err))
        }
    }
    if len(errs) > 0 {
        return fmt.Errorf("errors closing connections: %v", errs)
    }
    return nil
}