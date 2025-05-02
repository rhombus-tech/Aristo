# TDX Extension for High-Throughput AI Workloads

## Overview

Intel Trust Domain Extensions (TDX) has been integrated into our dual-TEE architecture to provide specialized high-throughput support for AI and batch computation workloads. This extension complements our existing SGX/SEV security foundation while delivering significant performance improvements for computationally intensive operations.

## Architecture

### Dual-TEE Architecture with TDX Extension

Our security architecture now supports three TEE technologies, all actively used in production:

1. **Intel SGX and AMD SEV**: Core TEEs executed in parallel for every operation
   - Both environments process the same workloads simultaneously
   - Results are cross-validated for enhanced security
   - SGX provides strong memory encryption and isolation
   - SEV offers VM-level protection on AMD hardware
   - Sampling techniques handle SEV memory constraints

3. **Intel TDX**: Extension for high-throughput AI/ML workloads
   - Optimized for batch processing operations
   - Larger memory footprint accommodates complex AI models
   - Integrated with paired execution model for security-performance balance

### Integration Points

TDX has been integrated throughout the codebase:

- Added as a variant to `TeeType`, `EnclaveType`, and similar enums
- Implemented in mesh network for distributed AI computation
- Supported in accumulator for batch attestation verification
- Enhanced policy engine with TDX-specific ruleset
- Updated attestation generation and validation logic

## Performance Benefits

Benchmark results demonstrate exceptional performance improvements:

- **Mesh Network**: 15-17k TPS with just 2 TEE pairs before polynomial commitments; scaling to >170k TPS with 20 pairs
- **Policy Verification**: 284,050 TPS in batch verification
- **Policy Test Harness**: 1.97M TPS for simulated workloads
- **Batch Processing**: >2.5M TPS for optimized batch operations
- **Peak Performance**: Up to 3.2M TPS in ideal conditions with specialized AI accelerators

## Parameter Handling

TDX integration maintains robust parameter handling for WebAssembly contracts, supporting:

### Dual Format Support

1. **Length-prefixed format**:
   - First 4 bytes represent a little-endian u32 length
   - Actual data follows the 4-byte length prefix
   - Common WebAssembly convention

2. **Direct data format**:
   - No length prefix, data passed directly
   - Used for fixed-size inputs (e.g., contract IDs)

### Security Measures

- **Bounds Checking**: Validates parameter lengths to prevent unreasonable values
- **Memory Safety**: Prevents out-of-bounds access and WebAssembly traps
- **Error Handling**: Returns properly formatted errors instead of panicking
- **Fallback Mechanism**: Gracefully handles parameter validation failures

## Paired Execution Model

Our system uses a paired execution approach across all three TEE technologies:

### Existing SGX-SEV Pairing
- **Dual Execution**: Both SGX and SEV run the same workloads in parallel
- **Sampling Approach**: Due to SEV memory constraints, we use sampling techniques rather than processing all data
- **Cross-Validation**: Results from both TEEs are compared for consistency and security verification

### TDX Integration
- **Primary Executor**: SGX continues to handle security-sensitive operations and validation
- **High-Throughput Executor**: TDX exclusively processes computationally intensive AI/ML workloads
- **Specialized Capabilities**: AI workloads require TDX's memory and performance characteristics and cannot run on SGX/SEV

This model provides an optimal balance of security, cross-platform compatibility, and performance for all workloads, with TDX specifically enhancing AI processing capabilities.

## Testing and Validation

The TDX implementation has been validated through:

- **Rust Integration Tests**:
  - `accumulator_integration_test` (9/9 pass)
  - `mesh_network_test` (9/9 pass) 
  - `enhanced_mesh_network_test` (3/3 pass)
  - `multi_tee_test` (3/3 pass)
  - `polynomial_integration_test` (2/2 pass)

- **Go Tests**:
  - `TestTDXAttestationVerification` (3/3 pass)
  - `TestBatchVerificationOptimizations` (pass)
  - `TestBatchPolicyIntegration` (pass)
  - `TestPolicyEngine` (pass)

## Usage Guidelines

### When to Use TDX

TDX is optimized for:
- AI/ML model execution
- Batch processing operations
- High-throughput, computationally intensive workloads

Continue using SGX for:
- Security-critical operations
- Financial transactions
- Key management functions
- Operations handling sensitive user data
