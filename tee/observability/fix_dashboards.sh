#!/bin/bash

# Set the base directory
BASE_DIR="$(dirname "$0")"
cd "$BASE_DIR"

echo "Current directory: $(pwd)"

# Update dashboard JSONs to use the correct datasource name
for dashboard in dashboards/*.json; do
  if [ -f "$dashboard" ]; then
    echo "Fixing datasource in $dashboard"
    sed -i '' 's/"datasource": "Prometheus"/"datasource": "TEE-Prometheus"/g' "$dashboard"
  else
    echo "Dashboard file not found: $dashboard"
    # List files in the dashboards directory
    echo "Files in dashboards directory:"
    ls -la dashboards/
  fi
done

# Restart Grafana to apply changes
docker restart tee-grafana

echo "Dashboards updated to use TEE-Prometheus datasource"
echo "Grafana restarted - please refresh your browser"
