#!/bin/bash
# Script to run the complete TEE monitoring infrastructure

# Default settings
REGION_ID="us-east"
MESH_API="http://localhost:8080"
EXEC_API="http://localhost:8081"
METRICS_PORT=9090

# Parse arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    --region=*)
      REGION_ID="${1#*=}"
      shift
      ;;
    --mesh-api=*)
      MESH_API="${1#*=}"
      shift
      ;;
    --exec-api=*)
      EXEC_API="${1#*=}"
      shift
      ;;
    --port=*)
      METRICS_PORT="${1#*=}"
      shift
      ;;
    *)
      echo "Unknown option: $1"
      exit 1
      ;;
  esac
done

echo "=== Regional TEE Monitoring System ==="
echo "Region: $REGION_ID"
echo "Mesh API: $MESH_API"
echo "Execution API: $EXEC_API"
echo "Metrics Port: $METRICS_PORT"
echo "====================================="

# Create Prometheus config
cat > prometheus.yml << EOF
global:
  scrape_interval: 5s
  evaluation_interval: 5s

scrape_configs:
  - job_name: 'tee_metrics'
    static_configs:
      - targets: ['localhost:$METRICS_PORT']
EOF

# Function to check if Docker is running
check_docker() {
  if ! docker info > /dev/null 2>&1; then
    echo "Docker is not running. Please start Docker first."
    exit 1
  fi
}

# Function to stop containers on exit
cleanup() {
  echo "Stopping containers..."
  docker stop tee-prometheus tee-grafana 2>/dev/null
  docker rm tee-prometheus tee-grafana 2>/dev/null
  echo "Cleanup complete"
}

# Setup trap for clean exit
trap cleanup EXIT

# Check Docker
check_docker

# Start Prometheus
echo "Starting Prometheus..."
docker run -d --name tee-prometheus \
  -p 9091:9090 \
  -v $(pwd)/prometheus.yml:/etc/prometheus/prometheus.yml \
  prom/prometheus

# Start Grafana
echo "Starting Grafana..."
docker run -d --name tee-grafana \
  -p 3000:3000 \
  -e "GF_AUTH_ANONYMOUS_ENABLED=true" \
  -e "GF_AUTH_ANONYMOUS_ORG_ROLE=Admin" \
  -e "GF_SECURITY_ALLOW_EMBEDDING=true" \
  -v $(pwd)/dashboards:/dashboards \
  grafana/grafana

echo "Building the TEE monitoring service..."
go build -o tee_monitor ./monitor_launcher.go

echo "Starting TEE monitoring service..."
./tee_monitor \
  --port=$METRICS_PORT \
  --region=$REGION_ID \
  --mesh-api=$MESH_API \
  --exec-api=$EXEC_API

# The script will automatically clean up containers when exited with Ctrl+C
