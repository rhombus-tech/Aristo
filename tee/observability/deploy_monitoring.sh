#!/bin/bash

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${YELLOW}TEE Architecture Monitoring Deployment${NC}"
echo "==============================================="

# Function to check if Docker is running
check_docker() {
  if ! docker info >/dev/null 2>&1; then
    echo -e "${RED}Error: Docker is not running or not installed.${NC}"
    echo "Please start Docker and try again."
    exit 1
  fi
}

# Function to start the monitoring stack
start_monitoring() {
  echo -e "${YELLOW}Starting TEE Monitoring Stack...${NC}"
  
  # Change to the observability directory
  cd "$(dirname "$0")"
  
  # Check if containers are already running
  if docker ps | grep -q "tee-prometheus\|tee-grafana"; then
    echo -e "${YELLOW}Monitoring containers are already running. Stopping them first...${NC}"
    docker-compose down
  fi
  
  # Start the monitoring stack
  docker-compose up -d
  
  # Wait for services to initialize
  echo "Waiting for services to initialize..."
  sleep 5
  
  # Check if services are running
  if docker ps | grep -q "tee-prometheus" && docker ps | grep -q "tee-grafana"; then
    echo -e "${GREEN}Monitoring stack started successfully!${NC}"
    
    # Print access information
    echo "==============================================="
    echo -e "${GREEN}TEE Architecture Monitoring Stack is Running${NC}"
    echo "==============================================="
    echo -e "Metrics endpoint: ${YELLOW}http://localhost:9090/metrics${NC}"
    echo -e "Prometheus UI: ${YELLOW}http://localhost:9091${NC}"
    echo -e "Grafana dashboards: ${YELLOW}http://localhost:3001${NC}"
    echo ""
    echo "Default dashboards:"
    echo "- Regional TEE Performance"
    echo "- Cross-Regional Verification"
    echo "- TEE Security Metrics"
    echo "==============================================="
    echo -e "${YELLOW}Default Grafana credentials:${NC}"
    echo "Username: admin"
    echo "Password: admin"
    echo "==============================================="
  else
    echo -e "${RED}Failed to start monitoring stack.${NC}"
    docker-compose logs
    exit 1
  fi
}

# Function to stop the monitoring stack
stop_monitoring() {
  echo -e "${YELLOW}Stopping TEE Monitoring Stack...${NC}"
  
  # Change to the observability directory
  cd "$(dirname "$0")"
  
  # Stop the monitoring stack
  docker-compose down
  
  echo -e "${GREEN}Monitoring stack stopped.${NC}"
}

# Function to show status
show_status() {
  echo -e "${YELLOW}TEE Monitoring Stack Status:${NC}"
  
  if docker ps | grep -q "tee-prometheus"; then
    echo -e "Prometheus: ${GREEN}Running${NC}"
  else
    echo -e "Prometheus: ${RED}Not Running${NC}"
  fi
  
  if docker ps | grep -q "tee-grafana"; then
    echo -e "Grafana: ${GREEN}Running${NC}"
  else
    echo -e "Grafana: ${RED}Not Running${NC}"
  fi
  
  # Show endpoints if running
  if docker ps | grep -q "tee-prometheus" || docker ps | grep -q "tee-grafana"; then
    echo "==============================================="
    echo "Access Information:"
    [[ $(docker ps | grep -q "tee-prometheus") ]] && echo -e "Prometheus UI: ${YELLOW}http://localhost:9091${NC}"
    [[ $(docker ps | grep -q "tee-grafana") ]] && echo -e "Grafana dashboards: ${YELLOW}http://localhost:3001${NC}"
  fi
}

# Main script execution
check_docker

# Process command line arguments
case "$1" in
  start)
    start_monitoring
    ;;
  stop)
    stop_monitoring
    ;;
  restart)
    stop_monitoring
    start_monitoring
    ;;
  status)
    show_status
    ;;
  *)
    echo "Usage: $0 {start|stop|restart|status}"
    echo "  start   - Start the TEE monitoring stack"
    echo "  stop    - Stop the TEE monitoring stack"
    echo "  restart - Restart the TEE monitoring stack"
    echo "  status  - Show status of the TEE monitoring stack"
    exit 1
    ;;
esac

exit 0
