# TEE-Backed Polynomial ZK Archival: Production Deployment

This document outlines the production deployment process for the TEE-backed polynomial commitment system integrated with the ZK archival infrastructure.

## Prerequisites

- Access to a trusted execution environment (TEE) running the Rust controller with the polynomial commitment module
- Access to a production blockchain node with state access
- Appropriate attestation verification keys for regulatory compliance
- A monitoring system (Prometheus recommended)

## Production Configuration

Create a configuration file at `/etc/rhombus/tee_polynomial_config.json`:

```json
{
  "TEEEndpoint": "https://tee-controller.production.rhombus-tech.com/execute",
  "ZKArchiveConfig": {
    "BatchSize": 100,
    "ArchivalPeriod": "5m",
    "ReferencePoints": 20,
    "RecursiveProofLevels": 3,
    "Parallelism": 8,
    "TEEVerifiedOnly": true
  },
  "BatchSize": 50,
  "UseAcceleration": true,
  "MaxProofSize": 65536,
  "FieldElementSize": 32,
  "EnableMetrics": true,
  "MetricsEndpoint": "http://prometheus-pushgateway.monitoring:9091/metrics/job/zk-archival",
  "LogLevel": "info",
  "PerformanceLogFreq": 10
}
```

## Integration Points

Before deploying, you must implement the four integration points in `tee_polynomial_deployment.go`:

### 1. Verification Logic

```go
// Replace this in tee_polynomial_deployment.go
verifyFunc := func(ctx context.Context, fromHeight, toHeight uint64, params []byte) (bool, error) {
    parsedParams, format, err := ParseDualFormatParameter(params, true, true)
    if err != nil {
        log.Printf("Parameter parsing error: %v", err)
        return false, err
    }

    if config.LogLevel == "debug" {
        log.Printf("Using %s parameter format for verification", format)
    }

    // Use your blockchain's actual verification API
    return myBlockchain.VerifyTransition(ctx, fromHeight, toHeight, parsedParams)
}
```

### 2. State Access Logic

```go
// Replace this in tee_polynomial_deployment.go
getStateFunc := func(ctx context.Context, stateRoot [32]byte, key []byte) ([]byte, error) {
    parsedKey, format, err := ParseDualFormatParameter(key, true, true)
    if err != nil {
        log.Printf("Key parsing error: %v", err)
        return nil, err
    }

    if config.LogLevel == "debug" {
        log.Printf("Using %s key format for state query", format)
    }

    var idRoot ids.ID
    copy(idRoot[:], stateRoot[:])

    // Use your blockchain's actual state access API
    return myBlockchain.GetStateValue(ctx, idRoot, parsedKey)
}
```

### 3. Metrics Submission

```go
// Replace this in tee_polynomial_deployment.go
func submitMetrics(endpoint string, stats CompressionStats) {
    // Using Prometheus client
    prometheus.GaugeVec.WithLabelValues("zk_archival", "blocks_archived").Set(float64(stats.TotalBlocksArchived))
    prometheus.GaugeVec.WithLabelValues("zk_archival", "storage_saved_bytes").Set(float64(stats.StorageSavedBytes))
    prometheus.GaugeVec.WithLabelValues("zk_archival", "compression_ratio").Set(stats.CompressionRatio)
    prometheus.GaugeVec.WithLabelValues("zk_archival", "recursive_proof_depth").Set(float64(stats.RecursiveProofDepth))
    prometheus.GaugeVec.WithLabelValues("zk_archival", "batch_processing_time_ms").Set(stats.BatchProcessingTimeMs)
}
```

### 4. Blockchain Components

```go
// Replace this in tee_polynomial_deployment.go
// Get your actual blockchain components
chain := myBlockchain.GetStatelessChain()
verifier := myBlockchain.GetStatelessVerifier()
```

## Deployment Steps

1. Build the production binary:

```bash
cd /path/to/rhombus-tech/vm/tee/stateless/archive
go build -o tee-polynomial-archival ./cmd/production/main.go
```

2. Install the binary and configuration:

```bash
sudo install -m 755 tee-polynomial-archival /usr/local/bin/
sudo mkdir -p /etc/rhombus
sudo cp tee_polynomial_config.json /etc/rhombus/
```

3. Create a systemd service file at `/etc/systemd/system/tee-polynomial-archival.service`:

```ini
[Unit]
Description=TEE-Backed Polynomial ZK Archival Service
After=network.target

[Service]
Type=simple
User=rhombus
Group=rhombus
ExecStart=/usr/local/bin/tee-polynomial-archival /etc/rhombus/tee_polynomial_config.json
Restart=on-failure
RestartSec=5s
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
```

4. Enable and start the service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable tee-polynomial-archival
sudo systemctl start tee-polynomial-archival
```

## Monitoring

Monitor the system using your Prometheus instance. The following metrics are available:

- `zk_archival_blocks_archived`: Total blocks archived
- `zk_archival_storage_saved_bytes`: Storage saved in bytes
- `zk_archival_compression_ratio`: Compression ratio
- `zk_archival_recursive_proof_depth`: Current recursive proof depth
- `zk_archival_batch_processing_time_ms`: Time to process each batch in milliseconds

## Security Considerations

1. **Parameter Safety**: The system implements robust dual-format parameter handling and protects against memory exploits like the 3.5B byte vulnerability.

2. **TEE Attestation**: Always verify TEE attestations for regulatory compliance.

3. **Access Control**: Ensure the TEE endpoint is properly secured and only accessible from authorized services.

4. **Key Management**: Securely manage attestation verification keys.

5. **Logs & Auditing**: Retain logs for regulatory auditing purposes.

## Troubleshooting

Check logs using:

```bash
sudo journalctl -u tee-polynomial-archival -f
```

Common issues:

1. **TEE Connection Failures**: Ensure the TEE endpoint is accessible and correctly configured.
2. **State Access Errors**: Verify blockchain access permissions and connection parameters.
3. **Parameter Format Errors**: Check for any mismatches in parameter encoding between components.
4. **Memory/Resource Issues**: Monitor system resources and adjust batch sizes if needed.

## Backup and Recovery

Regularly back up the following:

1. Configuration file
2. Attestation verification keys
3. Latest state proofs

For disaster recovery, you can rebuild the ZK proofs from the blockchain state as long as you have access to a trusted TEE.
