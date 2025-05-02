#!/bin/bash
# Production deployment script for TEE-backed polynomial ZK archival

set -e

# Configuration
ROOT_DIR=$(cd "$(dirname "$0")/.." && pwd)
BUILD_DIR="${ROOT_DIR}/build"
CONFIG_DIR="/etc/rhombus"
BINARY_NAME="tee-polynomial-archival"
SERVICE_NAME="tee-polynomial-archival"
CONFIG_NAME="tee_polynomial_config.json"
SERVICE_USER="rhombus"
SERVICE_GROUP="rhombus"

# Color output
RED="\033[0;31m"
GREEN="\033[0;32m"
YELLOW="\033[0;33m"
BLUE="\033[0;34m"
NC="\033[0m" # No Color

# Print with timestamp
log() {
  echo -e "[$(date '+%Y-%m-%d %H:%M:%S')] ${BLUE}INFO${NC}: $1"
}

log_warn() {
  echo -e "[$(date '+%Y-%m-%d %H:%M:%S')] ${YELLOW}WARN${NC}: $1"
}

log_error() {
  echo -e "[$(date '+%Y-%m-%d %H:%M:%S')] ${RED}ERROR${NC}: $1"
}

log_success() {
  echo -e "[$(date '+%Y-%m-%d %H:%M:%S')] ${GREEN}SUCCESS${NC}: $1"
}

# Check if running as root
if [[ $EUID -ne 0 ]]; then
   log_error "This script must be run as root"
   exit 1
fi

# Check for required commands
for cmd in go systemctl mkdir cp chown chmod; do
  if ! command -v $cmd &> /dev/null; then
    log_error "$cmd command not found. Please install it and try again."
    exit 1
  fi
done

# Validate user and group existence
if ! getent passwd $SERVICE_USER > /dev/null; then
  log_warn "User $SERVICE_USER does not exist. Creating..."
  useradd -m -s /bin/bash $SERVICE_USER
fi

if ! getent group $SERVICE_GROUP > /dev/null; then
  log_warn "Group $SERVICE_GROUP does not exist. Creating..."
  groupadd $SERVICE_GROUP
fi

# Create build directory
log "Creating build directory..."
mkdir -p "$BUILD_DIR"

# Build the application
log "Building the application..."
cd "$ROOT_DIR"
go build -o "$BUILD_DIR/$BINARY_NAME" ./cmd/production/main.go

# Create config directory
log "Creating config directory..."
mkdir -p "$CONFIG_DIR"

# Check if config file exists
if [[ ! -f "$CONFIG_DIR/$CONFIG_NAME" ]]; then
  log_warn "Config file not found, creating sample config..."
  cat > "$CONFIG_DIR/$CONFIG_NAME" << EOF
{
  "TEEEndpoint": "https://tee-controller.production.example.com/execute",
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
  "MetricsEndpoint": "http://prometheus-pushgateway:9091/metrics/job/zk-archival",
  "LogLevel": "info",
  "PerformanceLogFreq": 10
}
EOF
  log_warn "⚠️ Please edit $CONFIG_DIR/$CONFIG_NAME with your production settings!"
else
  log "Config file already exists, using existing configuration."
fi

# Install binary
log "Installing binary..."
install -m 755 "$BUILD_DIR/$BINARY_NAME" "/usr/local/bin/$BINARY_NAME"

# Create systemd service file
log "Creating systemd service file..."
cat > "/etc/systemd/system/$SERVICE_NAME.service" << EOF
[Unit]
Description=TEE-Backed Polynomial ZK Archival Service
After=network.target

[Service]
Type=simple
User=$SERVICE_USER
Group=$SERVICE_GROUP
ExecStart=/usr/local/bin/$BINARY_NAME $CONFIG_DIR/$CONFIG_NAME
Restart=on-failure
RestartSec=5s
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF

# Set permissions
log "Setting permissions..."
chown -R $SERVICE_USER:$SERVICE_GROUP "$CONFIG_DIR"
chmod 644 "$CONFIG_DIR/$CONFIG_NAME"
chmod 644 "/etc/systemd/system/$SERVICE_NAME.service"

# Reload systemd
log "Reloading systemd..."
systemctl daemon-reload

# Enable and start service
log "Enabling and starting service..."
systemctl enable "$SERVICE_NAME"

# Check if user wants to start the service now
read -p "Do you want to start the service now? (y/n): " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
  systemctl start "$SERVICE_NAME"
  sleep 2
  if systemctl is-active --quiet "$SERVICE_NAME"; then
    log_success "Service $SERVICE_NAME started successfully!"
  else
    log_error "Service $SERVICE_NAME failed to start. Check logs with: journalctl -u $SERVICE_NAME"
  fi
else
  log "Service not started. You can start it manually with: systemctl start $SERVICE_NAME"
fi

# Security reminder
log_warn "⚠️ SECURITY REMINDER ⚠️"
log_warn "1. Ensure your TEE controller uses proper attestation verification"
log_warn "2. Check parameter safety to protect against the 3.5B byte vulnerability"
log_warn "3. Validate both length-prefixed and direct parameter formats"
log_warn "4. Secure access to your TEE endpoint with proper authentication"

log_success "Deployment completed!"
log "You can check the service status with: systemctl status $SERVICE_NAME"
log "View logs with: journalctl -u $SERVICE_NAME -f"
