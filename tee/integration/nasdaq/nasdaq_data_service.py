#!/usr/bin/env python3
# NASDAQ Cloud Data Service Integration for TEE Tokenization Platform
# This service fetches real market data and securely integrates it into the TEE mesh network

import os
import sys
import json
import time
import logging
import argparse
import requests
from datetime import datetime, timedelta
import subprocess
import threading
import queue

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger("NASDAQ-TEE-Integration")

# Import NASDAQ Cloud Data Service SDK
try:
    from ncds.client import NcdsClient
    from ncds.constants.product import Product
    from ncds.constants.time_of_day import TimeOfDay
except ImportError:
    logger.error("NASDAQ Cloud Data Service SDK not found. Installing...")
    subprocess.check_call([sys.executable, "-m", "pip", "install", "nasdaq-data-link"])
    from ncds.client import NcdsClient
    from ncds.constants.product import Product
    from ncds.constants.time_of_day import TimeOfDay

class NasdaqTeeIntegration:
    """Integration service between NASDAQ Cloud Data Service and the TEE mesh network"""
    
    def __init__(self, api_key, api_secret, tee_mesh_endpoint, region_id="us-east"):
        """
        Initialize the NASDAQ-TEE integration service
        
        Args:
            api_key (str): NASDAQ API key
            api_secret (str): NASDAQ API secret
            tee_mesh_endpoint (str): Endpoint for the TEE mesh network
            region_id (str): Region ID for this TEE instance
        """
        self.api_key = api_key
        self.api_secret = api_secret
        self.tee_mesh_endpoint = tee_mesh_endpoint
        self.region_id = region_id
        self.data_queue = queue.Queue()
        self.running = False
        
        # Initialize NASDAQ client
        self.ncds_client = NcdsClient(
            api_key=api_key,
            api_secret=api_secret
        )
        
        logger.info(f"NASDAQ-TEE Integration initialized for region {region_id}")
    
    def start(self):
        """Start the integration service"""
        self.running = True
        
        # Start threads
        self.nasdaq_thread = threading.Thread(target=self._nasdaq_data_fetcher)
        self.processing_thread = threading.Thread(target=self._secure_data_processor)
        
        self.nasdaq_thread.daemon = True
        self.processing_thread.daemon = True
        
        self.nasdaq_thread.start()
        self.processing_thread.start()
        
        logger.info("NASDAQ-TEE Integration service started")
    
    def stop(self):
        """Stop the integration service"""
        self.running = False
        
        # Wait for threads to terminate
        self.nasdaq_thread.join(timeout=2.0)
        self.processing_thread.join(timeout=2.0)
        
        logger.info("NASDAQ-TEE Integration service stopped")
    
    def _nasdaq_data_fetcher(self):
        """Thread that fetches data from NASDAQ Cloud Data Service"""
        while self.running:
            try:
                # Get today's date in YYYYMMDD format for NASDAQ API
                today = datetime.now().strftime('%Y%m%d')
                yesterday = (datetime.now() - timedelta(days=1)).strftime('%Y%m%d')
                
                # Fetch Equity Trades data
                logger.info("Fetching NASDAQ Equity Trades data...")
                eq_trades = self.ncds_client.get_equities_trades(
                    Product.NASDAQ_BASIC,
                    yesterday,
                    TimeOfDay.AFTER_HOURS,
                    symbols=["AAPL", "MSFT", "AMZN", "NVDA", "GOOGL"]
                )
                
                # Process each trade
                for trade in eq_trades:
                    # Add region information and timestamp for TEE processing
                    trade_data = {
                        "nasdaq_trade": trade.to_json(),
                        "tee_metadata": {
                            "source_region": self.region_id,
                            "timestamp": datetime.now().isoformat(),
                            "data_type": "equity_trade",
                            "security_level": "confidential"
                        }
                    }
                    
                    # Add to processing queue
                    self.data_queue.put(trade_data)
                
                # Fetch NASDAQ Index data
                logger.info("Fetching NASDAQ Index data...")
                index_data = self.ncds_client.get_nasdaq_index(
                    today,
                    symbols=["NDX", "COMP", "XAU"]
                )
                
                for idx_item in index_data:
                    index_data = {
                        "nasdaq_index": idx_item.to_json(),
                        "tee_metadata": {
                            "source_region": self.region_id,
                            "timestamp": datetime.now().isoformat(),
                            "data_type": "index_data",
                            "security_level": "public"
                        }
                    }
                    
                    # Add to processing queue
                    self.data_queue.put(index_data)
                
                # Wait before fetching again
                time.sleep(60)  # Fetch data every minute
                
            except Exception as e:
                logger.error(f"Error fetching NASDAQ data: {str(e)}")
                time.sleep(30)  # Retry after 30 seconds
    
    def _secure_data_processor(self):
        """Thread that processes data securely and feeds it to the TEE mesh network"""
        while self.running:
            try:
                # Get data from the queue
                if not self.data_queue.empty():
                    data = self.data_queue.get(timeout=1.0)
                    
                    # Process the data through the TEE mesh network
                    self._feed_to_tee_mesh(data)
                    
                    # Mark task as done
                    self.data_queue.task_done()
                else:
                    time.sleep(0.1)
                    
            except queue.Empty:
                time.sleep(0.1)
            except Exception as e:
                logger.error(f"Error processing data: {str(e)}")
    
    def _feed_to_tee_mesh(self, data):
        """
        Feed data to the TEE mesh network
        
        Args:
            data (dict): Data to be processed by the TEE mesh network
        """
        try:
            # Prepare headers
            headers = {
                'Content-Type': 'application/json',
                'X-TEE-Region-ID': self.region_id,
                'X-TEE-Source': 'nasdaq-integration'
            }
            
            # Send data to TEE mesh endpoint
            response = requests.post(
                self.tee_mesh_endpoint,
                headers=headers,
                json=data,
                timeout=5.0
            )
            
            if response.status_code == 200:
                logger.info(f"Successfully sent {data['tee_metadata']['data_type']} data to TEE mesh")
            else:
                logger.warning(f"Failed to send data to TEE mesh: {response.status_code} - {response.text}")
                
        except Exception as e:
            logger.error(f"Error sending data to TEE mesh: {str(e)}")

def main():
    """Main entry point for the NASDAQ-TEE integration service"""
    parser = argparse.ArgumentParser(description='NASDAQ Cloud Data Service Integration for TEE Mesh Network')
    
    parser.add_argument('--api-key', required=True, help='NASDAQ API Key')
    parser.add_argument('--api-secret', required=True, help='NASDAQ API Secret')
    parser.add_argument('--tee-endpoint', default='http://localhost:8080/api/v1/nasdaq-data', 
                       help='TEE Mesh Network Endpoint')
    parser.add_argument('--region', default='us-east', help='Region ID for this TEE instance')
    
    args = parser.parse_args()
    
    # Initialize and start the integration service
    integration = NasdaqTeeIntegration(
        api_key=args.api_key,
        api_secret=args.api_secret,
        tee_mesh_endpoint=args.tee_endpoint,
        region_id=args.region
    )
    
    try:
        # Start the service
        integration.start()
        
        # Keep running until interrupted
        while True:
            time.sleep(1)
            
    except KeyboardInterrupt:
        logger.info("Shutting down NASDAQ-TEE integration service...")
        integration.stop()
        sys.exit(0)

if __name__ == "__main__":
    main()
