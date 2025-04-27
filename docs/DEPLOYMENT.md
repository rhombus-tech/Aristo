# Dual TEE Mesh Deployment Guide for NASDAQ Market Data Processing

This document outlines the complete deployment process for the Aristo dual TEE (SGX + SEV) mesh network for secure, high-throughput market data processing.

## System Architecture Overview

Our architecture leverages both Intel SGX and AMD SEV trusted execution environments in a security-first design:

- **SGX Nodes**: Primary processing with higher throughput (~100 TPS)
  - 500-element batch processing
  - 8-thread parallel execution model
  - Hardware enclave-based attestation

- **SEV Nodes**: Secondary verification with stronger memory isolation (~0.25-1 TPS)
  - 100-element batch processing
  - 8-thread parallel execution model
  - VM-level memory encryption

- **WebAssembly Processing**: Both nodes use WebAssembly modules for:
  - Parameter validation (both length-prefixed and direct formats)
  - RSA accumulator for cryptographic verification
  - Memory-safe execution within TEE boundaries

## Deployment Flow Overview

The complete deployment includes these key steps:

1. **Prerequisites**: Install necessary tools and dependencies
2. **WebAssembly RSA Accumulator**: Build and deploy the dual-format parameter validator 
3. **AWS TEE Infrastructure**: Deploy SGX and SEV nodes with CloudFormation
4. **Cross-Attestation Setup**: Configure the cross-attestation between TEE types
5. **Market Data Simulator**: Deploy the enhanced NASDAQ simulator
6. **Benchmark Testing**: Measure performance across the TEE mesh
7. **Multi-Region Setup**: Deploy to multiple geographic regions (production)

## Detailed Deployment Steps

### 1. Prerequisites

```bash
# Clone the repository
git clone https://github.com/rhombus-tech/aristo.git
cd aristo

# Install required dependencies
pip install -r requirements.txt

# Install TinyGo for WebAssembly compilation
brew install tinygo

# Install Enarx for WebAssembly trusted execution
curl -sSf https://download.enarx.dev/enarx-installer | sh
```

### 2. WebAssembly RSA Accumulator Deployment

The WebAssembly RSA accumulator with dual-format parameter validation must be built and deployed first:

```bash
cd tee/deployment/aws

# Build the WebAssembly module with dual-format parameter validation support
./build_accumulator_wasm.sh

# Deploy to SGX and SEV nodes
./deploy_rsa_accumulator_service.sh
```

Key configuration parameters in the WebAssembly accumulator:

- `supportLengthPrefix`: Set to `true` to enable length-prefixed format validation (4-byte prefix)
- `supportDirectFormat`: Set to `true` to enable direct format validation (no length prefix)
- `batchSize`: Set to `1000` for optimal performance
- `parallelism`: Set to `8` for multi-threaded execution

The deployment process:

1. Builds the WebAssembly module using TinyGo
2. Creates Enarx configuration for SGX and SEV
3. Deploys to both node types with appropriate runtime settings
4. Verifies the services are running and accepting connections
5. Creates RSA accumulator instances for cryptographic verification

### 3. AWS Deployment of TEE Mesh

The TEE mesh can be deployed using our CloudFormation templates:

```bash
cd tee/deployment/aws

# Deploy the complete infrastructure including SGX and SEV nodes
./deploy_nasdaq_e2e_poc.sh
```

Key deployment parameters to configure:
- `REGION`: AWS region (default: us-east-1)
- `STACK_NAME`: CloudFormation stack name (default: aristo-nasdaq-poc)
- `NODE_COUNT`: Number of SGX+SEV node pairs (default: 4)
- `INSTANCE_TYPE_SGX`: Instance type for SGX nodes (default: c5.4xlarge)
- `INSTANCE_TYPE_SEV`: Instance type for SEV nodes (default: c6a.4xlarge)

The deployment script will:
1. Validate AWS CloudFormation templates
2. Check for existing stacks and handle transitional states
3. Create or update the CloudFormation stack
4. Configure and deploy both SGX and SEV nodes
5. Set up networking and security groups
6. Configure the coordinator services

### 3. Enhanced Market Data Simulator Deployment

For testing without real market data feeds, deploy our enhanced NASDAQ simulator:

```bash
cd /path/to/aristo/tee/benchmark

# Run the enhanced NASDAQ simulator with realistic order book events
python enhanced_nasdaq_simulator.py --scenario normal --batch-size 500 --symbols 10 --duration 3600
```

Simulator options:
- `--scenario`: Market scenario (normal, volatile, open, close, earnings)
- `--batch-size`: Number of events per batch (default: 500)
- `--symbols`: Number of symbols to simulate (default: 10)
- `--duration`: Duration in seconds (default: 3600)
- `--volatility`: Volatility factor (0.1-10.0, default: 1.0)
- `--output-dir`: Directory for output files (default: current directory)

### 4. Benchmark Testing

To evaluate performance with simulated market data:

```bash
cd /path/to/aristo/tee/benchmark

# Run paired benchmark test across deployed SGX+SEV nodes
./run_paired_benchmark.sh
```

The benchmark will:
1. Connect to all deployed SGX+SEV node pairs
2. Send market data batches to each pair with configurable parameters
3. Measure throughput and latency across the mesh
4. Generate detailed performance reports

### 5. Multi-Region Deployment (Production)

For a production-ready deployment with multi-region redundancy:

```bash
cd tee/deployment/multi-region

# Deploy primary region (US East)
./deploy_multi_region.sh --region us-east-1 --primary

# Deploy secondary regions
./deploy_multi_region.sh --region us-west-2 --secondary
./deploy_multi_region.sh --region us-central-1 --secondary
```

This creates a geographically distributed mesh network with cross-region attestation for enhanced security and availability.

### 6. Validator Deployment

To deploy the validator network for attestation verification:

```bash
cd validator/deployment

# Deploy validator network (21-31 validators recommended for market data)
./deploy_validators.sh --count 21 --regions us-east-1,us-west-2,us-central-1
```

## Monitoring and Maintenance

### Performance Monitoring

```bash
# Enable enhanced monitoring and logging
./enable_monitoring.sh

# View real-time performance dashboard
./view_dashboard.sh
```

### Maintenance Operations

```bash
# Rotate SGX attestation keys
./rotate_sgx_keys.sh

# Update SEV firmware
./update_sev_firmware.sh

# Perform security audit
./run_security_audit.sh
```

## Troubleshooting

Common issues and their solutions:

1. **SGX Attestation Failures**
   - Check Intel Attestation Service connectivity
   - Verify SGX enclave signing keys
   - Ensure EPID provisioning is complete

2. **SEV Attestation Failures**
   - Verify AMD SEV firmware version
   - Check platform certificates
   - Validate VMPL configuration

3. **Cross-Attestation Errors**
   - Ensure both SGX and SEV nodes are operational
   - Verify shared key exchange
   - Check cross-attestation protocol version compatibility

4. **Performance Issues**
   - Optimize batch sizes for each node type
   - Adjust thread counts for parallel processing
   - Tune RSA accumulator parameters

## Security Considerations

- **Defense-in-Depth**: The system remains secure unless both TEE types are compromised simultaneously
- **Parameter Validation**: WebAssembly contracts validate both length-prefixed and direct parameter formats
- **Cryptographic Verification**: RSA accumulators provide batch integrity verification
- **Memory Safety**: All memory access is bounds-checked to prevent WebAssembly traps
- **Error Handling**: Proper failure handling without exposing sensitive information

## Regulatory Compliance

- **SEC/FINRA Requirements**: System design addresses key regulatory requirements
- **Audit Trails**: Cryptographic proofs provide verifiable audit trails
- **Data Sovereignty**: Regional deployment supports data residency requirements
- **Access Controls**: Role-based permissions for system administration
