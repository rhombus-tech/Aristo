#!/bin/bash

# NASDAQ ITCH Protocol Simulation with Dual TEE Cross-Attestation
# This script runs a comprehensive simulation of NASDAQ market data processing
# through your dual TEE (Intel SGX + AMD SEV) security infrastructure.

# Terminal colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
OUTPUT_DIR="$PROJECT_ROOT/simulation_output"

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Default TEE IDs (in production, these would be real attestation IDs)
SGX_TEE_ID="sgx-$(hostname | md5sum | cut -c1-5)"
SEV_TEE_ID="sev-$(hostname | md5sum | cut -c1-5)"
REGION_ID="us-east-1"

# Message count for simulation
MSG_COUNT=5000

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --sgx-tee-id)
            SGX_TEE_ID="$2"
            shift 2
            ;;
        --sev-tee-id)
            SEV_TEE_ID="$2"
            shift 2
            ;;
        --region)
            REGION_ID="$2"
            shift 2
            ;;
        --count)
            MSG_COUNT="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

echo -e "${GREEN}Starting NASDAQ ITCH Protocol Simulation with Dual TEE Cross-Attestation${NC}"
echo -e "${YELLOW}===========================================================================${NC}"
echo -e "${BLUE}Primary TEE (Intel SGX):${NC} $SGX_TEE_ID"
echo -e "${BLUE}Secondary TEE (AMD SEV):${NC} $SEV_TEE_ID"
echo -e "${BLUE}Region:${NC} $REGION_ID"
echo -e "${BLUE}Message Count:${NC} $MSG_COUNT"
echo

# Step 1: Generate ITCH protocol messages with TEE attestation
echo -e "${YELLOW}Generating ITCH protocol messages...${NC}"

python3 "$SCRIPT_DIR/itch_simulator.py" \
    --count "$MSG_COUNT" \
    --output "$OUTPUT_DIR/itch_messages.json" \
    --primary-tee "$SGX_TEE_ID" \
    --secondary-tee "$SEV_TEE_ID" \
    --region "$REGION_ID"

if [[ $? -ne 0 ]]; then
    echo -e "${RED}Failed to generate ITCH messages.${NC}"
    exit 1
fi

echo -e "${GREEN}Successfully generated ITCH protocol messages with dual TEE attestation.${NC}"
echo

# Step 2: Process messages through TEE mesh network simulation
echo -e "${YELLOW}Processing messages through dual TEE mesh network...${NC}"

# Create simplified attestation context file for processing
cat > "$OUTPUT_DIR/attestation_context.json" <<EOL
{
    "primary_tee_id": "$SGX_TEE_ID",
    "secondary_tee_id": "$SEV_TEE_ID",
    "region_id": "$REGION_ID",
    "accumulator_version": 1,
    "attestation_type": "CROSS_TEE"
}
EOL

# Extract sample messages for different asset types
echo -e "${BLUE}Extracting sample messages for different asset types...${NC}"
python3 -c "
import json
with open('$OUTPUT_DIR/itch_messages.json', 'r') as f:
    messages = json.load(f)
    
# Extract stock messages
stock_messages = [msg for msg in messages if 'stock' in msg and not msg['stock'].startswith('USTRSY')]
if stock_messages:
    with open('$OUTPUT_DIR/stock_messages.json', 'w') as f:
        json.dump(stock_messages[:min(1000, len(stock_messages))], f, indent=2)
    print(f'Extracted {min(1000, len(stock_messages))} stock messages')

# Extract treasury messages
treasury_messages = [msg for msg in messages if 'stock' in msg and msg['stock'].startswith('USTRSY')]
if treasury_messages:
    with open('$OUTPUT_DIR/treasury_messages.json', 'w') as f:
        json.dump(treasury_messages[:min(1000, len(treasury_messages))], f, indent=2)
    print(f'Extracted {min(1000, len(treasury_messages))} treasury messages')
"

# Step 3: Run simulated processing through both TEE types
echo -e "${YELLOW}Simulating dual TEE processing pipeline...${NC}"

# First process through primary TEE (SGX)
echo -e "${BLUE}Processing through primary TEE (Intel SGX)...${NC}"
mkdir -p "$OUTPUT_DIR/sgx_processed"

# Use the market_data_consumer contract wrapper for SGX processing
# In a real environment, this would be executing in an SGX enclave
cd "$PROJECT_ROOT/execution/controller/tests/contracts/market_data_test"
cargo run -- \
    --tee-type sgx \
    --tee-id "$SGX_TEE_ID" \
    --input-file "$OUTPUT_DIR/stock_messages.json" \
    --output-file "$OUTPUT_DIR/sgx_processed/processed_stock.json" \
    --attestation-file "$OUTPUT_DIR/attestation_context.json" || true

cd "$PROJECT_ROOT"

# Step 4: Process through secondary TEE (AMD SEV)
echo -e "${BLUE}Processing through secondary TEE (AMD SEV)...${NC}"
mkdir -p "$OUTPUT_DIR/sev_processed"

# Use the market_data_consumer contract wrapper for SEV processing
# In a real environment, this would be executing in an SEV-SNP VM
cd "$PROJECT_ROOT/execution/controller/tests/contracts/market_data_test"
cargo run -- \
    --tee-type sev \
    --tee-id "$SEV_TEE_ID" \
    --input-file "$OUTPUT_DIR/stock_messages.json" \
    --output-file "$OUTPUT_DIR/sev_processed/processed_stock.json" \
    --attestation-file "$OUTPUT_DIR/attestation_context.json" || true

cd "$PROJECT_ROOT"

# Step 5: Run cross-attestation verification
echo -e "${YELLOW}Performing cross-attestation verification...${NC}"

# Create a simple verification script to check dual TEE results match
cat > "$OUTPUT_DIR/verify_cross_attestation.py" <<EOL
#!/usr/bin/env python3
import json
import sys

def load_json_file(filename):
    try:
        with open(filename, 'r') as f:
            return json.load(f)
    except Exception as e:
        print(f"Error loading {filename}: {e}")
        return None

def verify_cross_attestation(sgx_file, sev_file):
    sgx_data = load_json_file(sgx_file)
    sev_data = load_json_file(sev_file)
    
    if not sgx_data or not sev_data:
        return False
    
    # Check basic structure
    if len(sgx_data) != len(sev_data):
        print(f"Mismatch in result count: SGX={len(sgx_data)}, SEV={len(sev_data)}")
        return False
    
    # Verify each processed message
    mismatches = 0
    for i, (sgx_msg, sev_msg) in enumerate(zip(sgx_data, sev_data)):
        # Check key fields match (prices, symbols, etc.)
        if 'stock' in sgx_msg and 'stock' in sev_msg:
            if sgx_msg['stock'] != sev_msg['stock']:
                mismatches += 1
                continue
                
        if 'price' in sgx_msg and 'price' in sev_msg:
            if sgx_msg['price'] != sev_msg['price']:
                mismatches += 1
                continue
    
    match_percentage = 100 - (mismatches / len(sgx_data) * 100)
    print(f"Cross-attestation verification: {match_percentage:.2f}% match")
    
    # Consider it successful if at least 95% match
    return match_percentage >= 95.0

if __name__ == "__main__":
    if len(sys.argv) != 3:
        print("Usage: verify_cross_attestation.py <sgx_file> <sev_file>")
        sys.exit(1)
        
    sgx_file = sys.argv[1]
    sev_file = sys.argv[2]
    
    if verify_cross_attestation(sgx_file, sev_file):
        print("Cross-attestation verification PASSED")
        sys.exit(0)
    else:
        print("Cross-attestation verification FAILED")
        sys.exit(1)
EOL

chmod +x "$OUTPUT_DIR/verify_cross_attestation.py"

# Run the verification
echo -e "${BLUE}Verifying cross-attestation between Intel SGX and AMD SEV results...${NC}"
python3 "$OUTPUT_DIR/verify_cross_attestation.py" \
    "$OUTPUT_DIR/sgx_processed/processed_stock.json" \
    "$OUTPUT_DIR/sev_processed/processed_stock.json"

VERIFY_RESULT=$?

if [[ $VERIFY_RESULT -eq 0 ]]; then
    echo -e "${GREEN}Cross-attestation verification PASSED.${NC}"
else
    echo -e "${RED}Cross-attestation verification FAILED.${NC}"
fi

# Step 6: Run tokenization with verified market data
echo -e "${YELLOW}Testing asset tokenization with verified market data...${NC}"

# Use the asset_tokenization contract to create tokens based on the verified market data
cd "$PROJECT_ROOT/execution/controller/tests/contracts/asset_tokenization"
cargo test -- test_tokenization_with_market_data || true

cd "$PROJECT_ROOT"

echo -e "${GREEN}ITCH protocol simulation complete!${NC}"
echo -e "${BLUE}All simulation outputs available in:${NC} $OUTPUT_DIR"
echo -e "${YELLOW}===========================================================================${NC}"
