#!/bin/bash
# Build and test WebAssembly policy modules for TDX attestation

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
POLICY_DIR="${SCRIPT_DIR}/sample_policy"
WASM_DIR="${POLICY_DIR}/wasm"

# Ensure directories exist
mkdir -p "${WASM_DIR}"

# Create package.json if it doesn't exist
if [ ! -f "${POLICY_DIR}/package.json" ]; then
    echo "Creating package.json..."
    echo '{
  "name": "wasm-policy-engine",
  "version": "1.0.0",
  "description": "WebAssembly policy engine for TDX attestation",
  "scripts": {
    "asbuild": "asc"
  }
}' > "${POLICY_DIR}/package.json"
fi

# Install AssemblyScript locally
if [ ! -d "${POLICY_DIR}/node_modules" ]; then
    echo "Installing AssemblyScript locally..."
    cd "${POLICY_DIR}" && npm install --save-dev assemblyscript
fi

# Paths to local asc compiler
ASC_BIN="${POLICY_DIR}/node_modules/.bin/asc"

# Compile AssemblyScript modules to WebAssembly
echo "Compiling measurement validator module..."
"${ASC_BIN}" "${POLICY_DIR}/measurement_validator.ts" \
    --optimize --noAssert \
    --target release \
    --exportRuntime \
    --outFile "${WASM_DIR}/measurement_validator.wasm"

echo "Compiling trading limits module..."
"${ASC_BIN}" "${POLICY_DIR}/trading_limits.ts" \
    --optimize --noAssert \
    --target release \
    --exportRuntime \
    --outFile "${WASM_DIR}/trading_limits.wasm"

echo "WebAssembly modules compiled successfully."

# Create test policy directories
POLICY_TEST_DIR="${SCRIPT_DIR}/../../../test/policy"
TEST_TEMP_DIR="/tmp/tdx-policy-test"

# Set up test directories
mkdir -p "${POLICY_TEST_DIR}/wasm"
mkdir -p "${TEST_TEMP_DIR}/wasm"
mkdir -p "${SCRIPT_DIR}/sample_policy/test_integration/wasm"

# Copy policy.json to test directories with the correct name
cp "${POLICY_DIR}/policy.json" "${POLICY_TEST_DIR}/default-tdx-policy.json"
cp "${POLICY_DIR}/policy.json" "${TEST_TEMP_DIR}/default-tdx-policy.json"

# Copy WebAssembly modules to all test directories
cp "${WASM_DIR}/"*.wasm "${POLICY_TEST_DIR}/wasm/"
cp "${WASM_DIR}/"*.wasm "${TEST_TEMP_DIR}/wasm/"
cp "${WASM_DIR}/"*.wasm "${SCRIPT_DIR}/sample_policy/test_integration/wasm/"

echo "Policy files copied to test directory."

# Create go.mod file
echo "Creating go.mod file..."
echo "module wasm-policy-engine" > "${SCRIPT_DIR}/sample_policy/test_integration/go.mod"

# Run policy engine tests
echo "Running policy engine tests..."
cd "${SCRIPT_DIR}/../../.."
go test -v ./tee/policy -run TestPolicyEngine

# Run the policy test harness
echo "Running policy test harness..."
go test -v ./tee/policy -run TestRunPolicyTestHarness

# Run our dedicated WebAssembly policy module tests
echo "Running dedicated WebAssembly policy module tests..."
cd "${SCRIPT_DIR}/sample_policy/test_integration"
go test -v

# Run benchmark to verify throughput
echo "Running performance benchmark..."
go test -bench=BenchmarkPolicyEngine ./tee/policy -benchtime=5s

echo "All tests completed successfully."
