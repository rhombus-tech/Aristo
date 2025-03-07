# TEE Client for Contract Deployment

This package provides a client implementation for interacting with the Trusted Execution Environment (TEE) service, including support for contract deployment across SGX and SEV enclaves.

## Features

- Deploy WebAssembly contracts to both SGX and SEV TEEs
- Support for regional deployment to specific TEE pairs
- Automatic verification of attestations
- Cross-validation of results between SGX and SEV
- Support for multiple parameter formats when interacting with WebAssembly contracts

## Parameter Handling

When working with WebAssembly contracts, the client supports two parameter formats:

1. **Length-prefixed format** (standard):
   - First 4 bytes represent a little-endian u32 length
   - Actual data follows the 4-byte length prefix
   - Used for most parameter passing to WebAssembly functions

2. **Direct data format**:
   - No length prefix, data is passed directly
   - Used primarily for fixed-size data like contract IDs (32 bytes)

## Usage

### Basic Contract Deployment

```go
// Create a client
client, err := tee.NewClient(sgxEndpoint, sevEndpoint, stateVerifier)
if err != nil {
    log.Fatalf("Failed to create client: %v", err)
}
defer client.Close()

// Load contract code
contractCode, err := ioutil.ReadFile("path/to/contract.wasm")
if err != nil {
    log.Fatalf("Failed to read contract file: %v", err)
}

// Format initialization arguments with length prefix
initArgs := tee.FormatParameters([]byte(`{"owner":"0x123"}`), true)

// Deploy the contract
contractID, err := client.DeployContract(
    context.Background(), 
    contractCode, 
    initArgs, 
    "", // Empty string for default region
    "MyContract"
)
if err != nil {
    log.Fatalf("Contract deployment failed: %v", err)
}

fmt.Printf("Contract deployed with ID: %s\n", contractID)
```

### Regional Deployment

```go
// Add a region to the client
regionID := "us-west-1"
if err := client.AddRegion(regionID, "region-sgx:50051", "region-sev:50052"); err != nil {
    log.Fatalf("Failed to add region: %v", err)
}

// Deploy to specific region
regionalContractID, err := client.DeployContract(
    context.Background(),
    contractCode, 
    initArgs, 
    regionID,
    "RegionalContract"
)
```

## Parameter Formatting Helpers

The package includes helper functions for parameter formatting:

```go
// Format with length prefix (most common)
params := tee.FormatParameters(myData, true)

// Format without length prefix (for fixed-size data)
contractID := tee.FormatParameters(idBytes, false)

// Parse parameter bytes (handles both formats)
data, err := tee.ParseParameterBytes(params)
```

## Error Handling

- Contract deployment validates inputs before sending to TEEs
- Both SGX and SEV results are compared to ensure consistency
- Attestations are verified for each deployment
- Comprehensive error messages help diagnose deployment issues

## See Also

For more examples, see `examples/deploy_contract_example.go`.
