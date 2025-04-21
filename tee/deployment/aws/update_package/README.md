# Optimized TEE Accumulator Deployment for NASDAQ PoC

This package contains the necessary scripts and files to update the existing AWS TEE nodes with our optimized TEE accumulator implementation for the NASDAQ proof of concept.

## Package Contents

- `deploy_optimized_accumulator.sh`: Main deployment script to update all nodes
- Optimized implementation files that will be packaged during deployment:
  - `optimized_rsa_client.go`: Our high-performance RSA accumulator client
  - Integration with NASDAQ market data

## Performance Highlights

- **50,000+ TPS** target throughput for NASDAQ market data
- Optimized batch processing (1000 elements per batch)
- Parallel prime computation with bounded parallelism (8 cores)
- Efficient witness verification
- Cross-TEE attestation support (SGX and SEV)

## Deployment Instructions

1. **Make the script executable**:
   ```bash
   chmod +x deploy_optimized_accumulator.sh
   ```

2. **Run the deployment script**:
   ```bash
   ./deploy_optimized_accumulator.sh --region us-east-1 --stack-name dual-tee-architecture
   ```

   This will:
   - Build the optimized accumulator
   - Deploy to all SGX and SEV nodes
   - Update the NASDAQ connector
   - Run verification tests on each node

3. **Verify Deployment**:
   After deployment, the script will output performance metrics from each node.
   Expected performance per node: ~4,500 TPS
   Total cluster performance (12 nodes): ~54,000 TPS

## Configuration Options

The deployment automatically configures the following settings:

- `batch_size`: 1000 (optimal from benchmarking)
- `enable_async`: true
- `parallelism`: 8 (configured for AWS instance types)
- `verify_timeout_ms`: 100

## Monitoring

After deployment, you can monitor the performance of the TEE nodes using:

```bash
# SSH to any node
ssh ec2-user@<node-ip>

# Check accumulator service status
sudo systemctl status tee-accumulator

# View real-time logs
sudo journalctl -u tee-accumulator -f

# Check performance metrics
/opt/tee/accumulator/benchmark --stats
```

## Troubleshooting

If you encounter any issues during deployment:

1. **Rollback to previous version**:
   ```bash
   ssh ec2-user@<node-ip>
   sudo systemctl stop tee-accumulator
   sudo cp -r /opt/tee/accumulator/backup/* /opt/tee/accumulator/
   sudo systemctl start tee-accumulator
   ```

2. **Check logs for errors**:
   ```bash
   ssh ec2-user@<node-ip>
   sudo journalctl -u tee-accumulator -e
   ```

3. **Verify network connectivity**:
   ```bash
   ssh ec2-user@<node-ip>
   ping <other-node-ip>
   ```

## Next Steps

After successful deployment:

1. Connect to the NASDAQ market data feed
2. Run performance tests with real market data
3. Monitor system performance during market hours
4. Analyze throughput, latency, and resource utilization
