// File: tee/examples/deploy_contract_example.go
package examples

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"time"

	"github.com/rhombus-tech/vm/tee"
	"github.com/rhombus-tech/vm/verifier"
)

// This example demonstrates how to use the client to deploy a WebAssembly contract.
func DeployContractExample() {
	// Example SGX and SEV endpoints - replace with actual endpoints
	sgxEndpoint := "localhost:50051"
	sevEndpoint := "localhost:50052"

	// Create a verifier (simplified for example purposes)
	stateVerifier := &verifier.StateVerifier{}

	// Create a client
	client, err := tee.NewClient(sgxEndpoint, sevEndpoint, stateVerifier)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// Load WebAssembly contract code
	contractCode, err := ioutil.ReadFile("path/to/contract.wasm")
	if err != nil {
		log.Fatalf("Failed to read contract file: %v", err)
	}

	// Create initialization arguments
	// Use FormatParameters to properly format the arguments according to WebAssembly conventions
	// Set useLengthPrefix to true to use the 4-byte length prefix format
	initJSON := []byte(`{"owner":"0x123456789abcdef", "initialSupply": 1000000}`)
	initArgs := tee.FormatParameters(initJSON, true)

	// Set timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Deploy the contract
	// Leave regionID empty to use default region, or specify a region ID if available
	contractID, err := client.DeployContract(ctx, contractCode, initArgs, "", "MyTokenContract")
	if err != nil {
		log.Fatalf("Contract deployment failed: %v", err)
	}

	fmt.Printf("Contract deployed successfully with ID: %s\n", contractID)

	// Example of regional deployment
	// Assuming the client has been configured with regions
	regionID := "us-west-1"
	if err := client.AddRegion(regionID, "region-sgx:50051", "region-sev:50052"); err != nil {
		log.Fatalf("Failed to add region: %v", err)
	}

	// Deploy to specific region
	regionalContractID, err := client.DeployContract(ctx, contractCode, initArgs, regionID, "RegionalTokenContract")
	if err != nil {
		log.Fatalf("Regional contract deployment failed: %v", err)
	}

	fmt.Printf("Contract deployed to region %s with ID: %s\n", regionID, regionalContractID)
}

// Example showing how to handle the two parameter formats
func ParameterFormatExamples() {
	// Example 1: Length-prefixed format (most common)
	// This is the standard format for most WebAssembly contracts
	data := []byte(`{"method":"transfer","to":"0xabc","amount":100}`)
	lengthPrefixedParams := tee.FormatParameters(data, true)
	fmt.Printf("Length-prefixed format: %v\n", lengthPrefixedParams)

	// Example 2: Direct data format (used for contract IDs)
	// This is used when a fixed-size parameter is expected, like a 32-byte contract ID
	contractID := make([]byte, 32)
	// ... fill contractID with actual data
	directParams := tee.FormatParameters(contractID, false)
	fmt.Printf("Direct format (contract ID): %v\n", directParams)

	// Example 3: Parsing parameters
	// This demonstrates how to parse parameters in either format
	parsedData, err := tee.ParseParameterBytes(lengthPrefixedParams)
	if err != nil {
		log.Fatalf("Failed to parse parameters: %v", err)
	}
	fmt.Printf("Parsed data: %s\n", string(parsedData))
}
