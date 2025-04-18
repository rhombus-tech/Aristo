#!/bin/bash
# Mesh Network Setup Script for Azure Deployed TEE Pairs
# This script sets up the mesh network between TEE nodes after deployment

set -e

RESOURCE_GROUP="aristo-tee"
CONFIG_DIR="/etc/aristo-tee"

# Get command line arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        -g|--resource-group)
            RESOURCE_GROUP="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1"
            echo "Usage: $0 [-g|--resource-group RESOURCE_GROUP_NAME]"
            exit 1
            ;;
    esac
done

# Check if Azure CLI is installed
if ! command -v az &> /dev/null; then
    echo "Azure CLI not found. Please install it first."
    echo "Visit: https://docs.microsoft.com/cli/azure/install-azure-cli"
    exit 1
fi

# Get VM information
echo "Getting VM information from resource group $RESOURCE_GROUP..."
VM_INFO=$(az vm list --resource-group "$RESOURCE_GROUP" --query "[].{name:name, privateIp:privateIps, publicIp:publicIps}" -o json)

# Parse VM information to find SGX and SEV pairs
echo "Parsing VM information to identify TEE pairs..."
SGX_VMS=()
SEV_VMS=()
SGX_IPS=()
SEV_IPS=()

# Process VM info
for VM in $(echo "$VM_INFO" | jq -c '.[]'); do
    VM_NAME=$(echo "$VM" | jq -r '.name')
    PRIVATE_IP=$(echo "$VM" | jq -r '.privateIp')
    
    if [[ "$VM_NAME" == *"-sgx-"* ]]; then
        SGX_VMS+=("$VM_NAME")
        SGX_IPS+=("$PRIVATE_IP")
        echo "Found SGX VM: $VM_NAME with IP: $PRIVATE_IP"
    elif [[ "$VM_NAME" == *"-sev-"* ]]; then
        SEV_VMS+=("$VM_NAME")
        SEV_IPS+=("$PRIVATE_IP")
        echo "Found SEV VM: $VM_NAME with IP: $PRIVATE_IP"
    fi
done

# Check that we have matching pairs
if [ ${#SGX_VMS[@]} -ne ${#SEV_VMS[@]} ]; then
    echo "Warning: Unequal number of SGX and SEV VMs detected."
    echo "SGX VMs: ${#SGX_VMS[@]}, SEV VMs: ${#SEV_VMS[@]}"
fi

# Generate mesh network configuration files for each VM
echo "Generating mesh network configuration files..."

# Function to extract pair number from VM name
get_pair_number() {
    local vm_name="$1"
    local pair_num=$(echo "$vm_name" | grep -o '[0-9]\+')
    echo "$pair_num"
}

# Create mesh configurations for all VMs
for ((i=0; i<${#SGX_VMS[@]}; i++)); do
    SGX_VM="${SGX_VMS[$i]}"
    SGX_IP="${SGX_IPS[$i]}"
    SGX_PAIR_NUM=$(get_pair_number "$SGX_VM")
    
    # Find matching SEV VM with same pair number if available
    SEV_VM=""
    SEV_IP=""
    for ((j=0; j<${#SEV_VMS[@]}; j++)); do
        SEV_PAIR_NUM=$(get_pair_number "${SEV_VMS[$j]}")
        if [ "$SEV_PAIR_NUM" == "$SGX_PAIR_NUM" ]; then
            SEV_VM="${SEV_VMS[$j]}"
            SEV_IP="${SEV_IPS[$j]}"
            break
        fi
    done
    
    if [ -z "$SEV_VM" ]; then
        echo "Warning: No matching SEV VM found for SGX VM $SGX_VM"
        continue
    fi
    
    echo "Creating mesh network config for pair $SGX_PAIR_NUM (SGX: $SGX_VM, SEV: $SEV_VM)"
    
    # SGX node mesh config
    SGX_MESH_CONFIG=$(cat <<EOF
{
    "node_id": "SGX-${SGX_PAIR_NUM}",
    "tee_type": "SGX",
    "listen_port": 8080,
    "attestation_interval_ms": 5000,
    "mesh_nodes": [
        {
            "node_id": "SEV-${SGX_PAIR_NUM}",
            "tee_type": "SEV",
            "host": "${SEV_IP}",
            "port": 8080
        }
    ],
    "verification": {
        "timeout_ms": 100,
        "max_retry_attempts": 3,
        "constant_time": true,
        "parameter_validation": {
            "length_prefix_max_size": 1024,
            "protect_format_detection": true,
            "bounds_check_memory": true
        }
    },
    "security": {
        "rate_limit": {
            "operations_per_second": 50000,
            "burst_factor": 1.5
        },
        "cross_regional": {
            "enabled": true,
            "consistency_threshold_ms": 100
        }
    }
}
EOF
)
    echo "$SGX_MESH_CONFIG" > "sgx_${SGX_PAIR_NUM}_mesh_config.json"
    
    # SEV node mesh config
    SEV_MESH_CONFIG=$(cat <<EOF
{
    "node_id": "SEV-${SGX_PAIR_NUM}",
    "tee_type": "SEV",
    "listen_port": 8080,
    "attestation_interval_ms": 5000,
    "mesh_nodes": [
        {
            "node_id": "SGX-${SGX_PAIR_NUM}",
            "tee_type": "SGX",
            "host": "${SGX_IP}",
            "port": 8080
        }
    ],
    "verification": {
        "timeout_ms": 100,
        "max_retry_attempts": 3,
        "constant_time": true,
        "parameter_validation": {
            "length_prefix_max_size": 1024,
            "protect_format_detection": true,
            "bounds_check_memory": true
        }
    },
    "security": {
        "rate_limit": {
            "operations_per_second": 50000,
            "burst_factor": 1.5
        },
        "cross_regional": {
            "enabled": true,
            "consistency_threshold_ms": 100
        }
    }
}
EOF
)
    echo "$SEV_MESH_CONFIG" > "sev_${SGX_PAIR_NUM}_mesh_config.json"
done

echo "Mesh network configuration files generated."
echo ""
echo "To deploy these configurations to VMs:"
echo "1. Copy the configuration files to each VM:"
echo "   scp sgx_*_mesh_config.json username@<sgx-vm-ip>:/tmp/"
echo "   scp sev_*_mesh_config.json username@<sev-vm-ip>:/tmp/"
echo ""
echo "2. On each VM, move the configuration to the correct location:"
echo "   sudo mv /tmp/sgx_*_mesh_config.json $CONFIG_DIR/mesh_config.json"
echo "   or"
echo "   sudo mv /tmp/sev_*_mesh_config.json $CONFIG_DIR/mesh_config.json"
echo ""
echo "3. Restart the TEE service on each VM:"
echo "   sudo systemctl restart aristo-tee.service"
echo ""
echo "4. Verify the mesh network is functioning:"
echo "   tail -f /var/log/aristo-tee/tee-node.log"
echo ""
echo "Mesh network setup complete."
