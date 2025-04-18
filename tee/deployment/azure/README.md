# Azure Deployment for Dual TEE NASDAQ Market Integration

This guide explains how to deploy a secure, high-performance market data processing system using dual Trusted Execution Environments (TEE) on Azure. The deployment creates paired Intel SGX and AMD SEV instances with cross-attestation capabilities to ensure hardware-rooted trust.

## Architecture Overview

The deployment creates:

1. **TEE Pairs** - Each pair consists of:
   - 1 Intel SGX-enabled VM (DCsv2 or DCsv3 series)
   - 1 AMD SEV-enabled VM (NVv4 or ECv5 series)
   - Mesh network for cross-attestation between pairs
   - NASDAQ ITCH protocol simulation environment

2. **Security Features**:
   - Continuous cross-attestation between SGX and SEV nodes
   - Parameter validation with format detection protection
   - Secure token storage with hardware-rooted trust
   - Regional state isolation
   - Constant-time cryptographic operations

3. **Performance Targets**:
   - Sub-100ms verification
   - 50,000+ TPS for core operations

## Prerequisites

- [Azure CLI](https://docs.microsoft.com/cli/azure/install-azure-cli) installed
- Active Azure subscription
- `az login` completed with subscription access
- `jq` command-line tool installed

## Deployment Steps

1. **Review and customize parameter values**:
   - Edit `deploy.sh` to set default values or use command-line parameters
   - Key parameters include:
     - Number of TEE pairs
     - VM sizes for SGX and SEV nodes
     - Deployment regions
     - Admin credentials

2. **Run the deployment script**:
   ```bash
   ./deploy.sh
   ```

   Or with explicit parameters:
   ```bash
   ./deploy.sh --resource-group aristo-tee \
       --location eastus \
       --prefix aristotee \
       --username aristoadmin \
       --sgx-vm-size Standard_DC2s_v2 \
       --sev-vm-size Standard_NV4as_v4 \
       --count 3 \
       --regions eastus,westeurope,southeastasia
   ```

3. **Monitor deployment**:
   - Deployment typically takes 15-20 minutes
   - Monitor status via Azure portal or CLI:
     ```bash
     az deployment group show --resource-group aristo-tee --name aristo-tee-deployment --query properties.provisioningState
     ```

4. **Access VMs**:
   - Get public IP addresses:
     ```bash
     az vm list-ip-addresses --resource-group aristo-tee --output table
     ```
   - SSH to the VMs:
     ```bash
     ssh aristoadmin@<public-ip>
     ```

## Verifying Deployment

After deployment, verify the TEE environment:

1. **Check service status**:
   ```bash
   sudo systemctl status aristo-tee.service
   sudo systemctl status aristo-nasdaq-sim.service
   ```

2. **View logs**:
   ```bash
   tail -f /var/log/aristo-tee/tee-node.log
   tail -f /var/log/aristo-tee/nasdaq-sim.log
   ```

3. **Run test cross-attestation**:
   ```bash
   cd ~/aristo
   ./tee/integration/nasdaq/test_nasdaq_integration.sh
   ```

## Post-Deployment Configuration

1. **Update mesh network configuration** (if needed):
   - Edit `/etc/aristo-tee/mesh_config.json` on each node
   - Restart service: `sudo systemctl restart aristo-tee.service`

2. **Configure NASDAQ simulation parameters**:
   - Edit `/etc/aristo-tee/tee_config.json`
   - Update message rates, trading symbols, or market scenarios
   - Restart simulator: `sudo systemctl restart aristo-nasdaq-sim.service`

## Security Considerations

- All VMs are deployed as Confidential VMs with secure boot and vTPM enabled
- Default configuration enables continuous cross-attestation between paired TEE nodes
- Parameter validation includes protection against format detection confusion attacks
- Memory access is hardened with bounds checking and constant-time operations
- Client rate limiting is enabled to prevent resource exhaustion

## Troubleshooting

1. **TEE Service Failures**:
   - Check logs: `tail -f /var/log/aristo-tee/tee-node-error.log`
   - Verify SGX services: `systemctl status aesmd` (SGX nodes only)
   - Check attestation status: `journalctl -u aristo-tee -n 100`

2. **Cross-Attestation Issues**:
   - Verify network connectivity between TEE pairs
   - Check mesh network configuration matches paired node details
   - Ensure both nodes in a pair are running properly

3. **Performance Issues**:
   - Monitor CPU, memory, and network usage
   - Review logs for timing information
   - Adjust VM sizes if needed for higher performance

## Regional Compliance

The deployment supports multiple regions to meet regulatory requirements. Each TEE pair can be deployed in a different region, and the mesh network enables secure cross-regional attestation while maintaining regional autonomy.
