#!/bin/bash
# Fix script for AMD SEV node in dual TEE cross-attestation framework

# Update package repositories
apt-get update -y

# Install required packages
apt-get install -y apache2 curl jq

# Ensure all directories exist
mkdir -p /var/www/html
mkdir -p /opt/rhombus/mesh
mkdir -p /opt/rhombus/validation
mkdir -p /opt/rhombus/tee

# Create or update node info JSON
PUBLIC_IP=$(curl -s http://169.254.169.254/latest/meta-data/public-ipv4)
PRIVATE_IP=$(curl -s http://169.254.169.254/latest/meta-data/local-ipv4)
INSTANCE_ID=$(curl -s http://169.254.169.254/latest/meta-data/instance-id)

# Create node info file
cat > /opt/rhombus/mesh/node_info.json << EOF
{
  "node_type": "SEV",
  "node_id": "${INSTANCE_ID}",
  "public_ip": "${PUBLIC_IP}",
  "private_ip": "${PRIVATE_IP}",
  "port": 7070,
  "region": "us-east-1"
}
EOF

# Copy to web directory
cp /opt/rhombus/mesh/node_info.json /var/www/html/

# Create or fix status script
cat > /opt/rhombus/tee/status.sh << 'EOF'
#!/bin/bash
IP=$(curl -s http://169.254.169.254/latest/meta-data/public-ipv4)
ID=$(curl -s http://169.254.169.254/latest/meta-data/instance-id)
echo "{\"node_type\":\"SEV\",\"id\":\"$ID\",\"ip\":\"$IP\",\"status\":\"running\",\"timestamp\":\"$(date -u +"%Y-%m-%dT%H:%M:%SZ")\"}" > /var/www/html/status.json
EOF
chmod +x /opt/rhombus/tee/status.sh

# Run status script to generate initial status file
/opt/rhombus/tee/status.sh

# Update parameter validation config
cat > /opt/rhombus/validation/parameter_config.json << EOF
{
  "validation": {
    "length_prefixed": true,
    "direct_format": true,
    "max_size": 1024,
    "format_detection": true
  },
  "cross_attestation": {
    "enabled": true,
    "partner_type": "SGX",
    "verification_ms_target": 100
  }
}
EOF
cp /opt/rhombus/validation/parameter_config.json /var/www/html/

# Set proper permissions
chown -R www-data:www-data /var/www/html/

# Restart Apache
systemctl restart apache2

echo "SEV node fix complete"
