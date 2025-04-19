# AWS Dual TEE Cross-Attestation Deployment

This deployment package enables setting up a dual TEE cross-attestation framework using Intel SGX and AMD SEV in AWS, specifically designed to connect with NASDAQ Kafka for market data integration.

## Architecture Overview

The deployment creates:

1. **Intel SGX Nodes** - Running on c5a.xlarge instances for hardware-rooted trust
2. **AMD SEV Nodes** - Running on c6a.2xlarge instances with AMD SEV-SNP support 
3. **NASDAQ Connector** - A dedicated instance for connecting to NASDAQ Kafka

These components work together to provide a secure, high-performance environment for processing market data with parameter validation across both length-prefixed and direct formats.

## Prerequisites

- AWS CLI installed and configured
- An AWS EC2 Key Pair for SSH access
- VPC and subnet identified in the same region as NASDAQ Kafka (us-east-1)
- Appropriate IAM permissions to create CloudFormation stacks

## Deployment Steps

### 1. Deploy the Infrastructure

Make the deployment script executable:

```bash
chmod +x deploy.sh
chmod +x mesh_network_setup.sh
```

Run the deployment script:

```bash
./deploy.sh \
  --stack-name dual-tee-architecture \
  --region us-east-1 \
  --key-name YOUR_KEY_NAME \
  --vpc-id vpc-xxxxxxxx \
  --subnet-id subnet-xxxxxxxx \
  --sgx-instance-type c5a.xlarge \
  --sev-instance-type c6a.2xlarge \
  --sgx-count 2 \
  --sev-count 2
```

### 2. Set up the Mesh Network and Cross-Attestation

After the infrastructure is deployed, run:

```bash
./mesh_network_setup.sh \
  --stack-name dual-tee-architecture \
  --region us-east-1 \
  --ssh-key /path/to/your/private-key.pem
```

## Security Features

This deployment implements several critical security features:

### Enhanced Parameter Validation
- Support for both length-prefixed (4-byte length + data) and direct parameter formats
- Automatic format detection based on first 4 bytes
- Size limits and sanity checks on all parameters
- Protection against format detection confusion attacks

### Cross-Attestation Verification
- Verification of operations across both Intel SGX and AMD SEV TEEs
- Result comparison between different TEE platforms
- Detection of attestation inconsistencies

### Hardened Memory Access
- Enhanced bounds checking for WebAssembly memory access
- Guards against integer overflow in offset+size calculations
- Defensive copies to prevent time-of-check-time-of-use issues

## Testing the Deployment

After setup is complete, you can verify the deployment:

```bash
# Test connectivity to SGX node
ssh -i /path/to/your/key.pem ubuntu@SGX_NODE_IP

# Test connectivity to SEV node
ssh -i /path/to/your/key.pem ubuntu@SEV_NODE_IP

# Verify cross-attestation is working
ssh -i /path/to/your/key.pem ubuntu@SGX_NODE_IP "sudo cat /var/log/tee-integration/attestation.log | grep 'cross-attestation'"
```

## NASDAQ Market Data Integration

The NASDAQ Connector instance is configured to connect to NASDAQ Kafka and distribute market data to the TEE nodes. To customize the NASDAQ connection:

1. SSH into the NASDAQ Connector instance
2. Modify `/opt/kafka/kafka_2.13-3.2.3/config/nasdaq-connector.properties`
3. Update the `bootstrap.servers` parameter with your NASDAQ Kafka endpoint
4. Restart the service: `sudo systemctl restart market-data`

## Performance

The deployment is optimized for:
- 50,000+ TPS throughput
- Sub-100ms verification time for cross-attestation

## Troubleshooting

If you encounter issues with the deployment:

- Check CloudFormation stack events: `aws cloudformation describe-stack-events --stack-name dual-tee-architecture`
- Verify instance status: `aws ec2 describe-instance-status --instance-ids YOUR_INSTANCE_ID`
- Check logs on the instances: `/var/log/cloud-init-output.log` and `/var/log/tee-integration/*.log`
