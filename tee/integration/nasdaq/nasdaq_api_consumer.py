#!/usr/bin/env python3
# NASDAQ Cloud Data Service Kafka Consumer for TEE Integration
# This version uses a more direct approach for Kafka authentication

import os
import sys
import json
import time
import base64
import logging
import argparse
import requests
import threading
import queue
from datetime import datetime
from confluent_kafka import Consumer, Producer, KafkaError, KafkaException

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger("NASDAQ-TEE-Integration")

class NasdaqApiConsumer:
    """
    NASDAQ Cloud Data Service Consumer that integrates with TEE architecture
    Uses direct authentication instead of OAuth callbacks for better compatibility
    """
    
    def __init__(self, client_id, client_secret, token_endpoint, bootstrap_server, 
                 tee_endpoint, region_id="us-east"):
        """
        Initialize the NASDAQ API Consumer
        
        Args:
            client_id (str): OAuth2 client ID
            client_secret (str): OAuth2 client secret
            token_endpoint (str): OAuth2 token endpoint URL
            bootstrap_server (str): Kafka bootstrap server address
            tee_endpoint (str): Endpoint for the TEE mesh network
            region_id (str): Region ID for this TEE instance
        """
        self.client_id = client_id
        self.client_secret = client_secret
        self.token_endpoint = token_endpoint
        self.bootstrap_server = bootstrap_server
        self.tee_endpoint = tee_endpoint
        self.region_id = region_id
        
        self.access_token = None
        self.token_expiry = 0
        self.running = False
        self.data_queue = queue.Queue()
        
        # Define topic groups based on your entitlements
        self.nasdaq_basic_topics = ["QBBO-A-CORE", "QBBO-B-CORE", "QBBO-C-CORE"]
        self.nls_plus_topics = ["NLSCTA", "NLSUTP"]
        
        logger.info(f"NASDAQ API Consumer initialized for region {region_id}")
    
    def get_oauth_token(self):
        """
        Get OAuth2 token for NASDAQ Cloud Data Service
        """
        current_time = time.time()
        
        # Return existing token if still valid
        if self.access_token and current_time < self.token_expiry - 60:
            return self.access_token
            
        try:
            auth_str = f"{self.client_id}:{self.client_secret}"
            auth_bytes = auth_str.encode('ascii')
            base64_auth = base64.b64encode(auth_bytes).decode('ascii')
            
            headers = {
                "Authorization": f"Basic {base64_auth}",
                "Content-Type": "application/x-www-form-urlencoded"
            }
            
            payload = "grant_type=client_credentials"
            
            logger.info("Requesting OAuth token...")
            response = requests.post(
                self.token_endpoint,
                headers=headers,
                data=payload
            )
            
            response.raise_for_status()
            token_data = response.json()
            
            self.access_token = token_data.get("access_token")
            expires_in = token_data.get("expires_in", 3600)
            self.token_expiry = current_time + expires_in
            
            token_preview = f"{self.access_token[:20]}...{self.access_token[-20:]}" if self.access_token else "None"
            logger.info(f"Successfully obtained OAuth token: {token_preview}")
            logger.info(f"Token expires in {expires_in} seconds")
            
            return self.access_token
            
        except Exception as e:
            logger.error(f"Failed to get OAuth token: {str(e)}")
            raise
    
    def create_sasl_config(self):
        """
        Create SASL configuration for Kafka Consumer using NASDAQ Cloud Data Service token
        
        Returns:
            dict: Configuration dictionary for Kafka Consumer
        """
        # Using the NASDAQ-specific approach for token-based authentication
        # This is based on their documentation for Cloud Data Service
        
        # 1. Get fresh token
        token = self.get_oauth_token()
        
        # 2. Create the complete configuration with the token
        # NASDAQ CDS requires a specific auth approach
        config = {
            'bootstrap.servers': self.bootstrap_server,
            'security.protocol': 'SASL_SSL',
            'sasl.mechanisms': 'OAUTHBEARER',
            'oauth_cb': lambda x: (token, time.time() + 3600.0),  # 1-hour expiry from now
            'session.timeout.ms': 45000,
            'request.timeout.ms': 60000,  # Longer timeout for market data
            'auto.offset.reset': 'earliest',  # Read from beginning to get historical data
            'enable.auto.commit': True,
            'enable.partition.eof': True,  # Enable end-of-partition notification
            'ssl.ca.location': '/etc/ssl/cert.pem',  # Standard macOS CA certificate location
            'api.version.request': True,  # Negotiate API version
            'socket.keepalive.enable': True,
        }
        
        logger.info(f"Created Kafka configuration for NASDAQ Cloud Data Service")
        return config
    
    def start(self):
        """Start consuming NASDAQ data"""
        self.running = True
        
        # Start the consumer threads
        self.consumer_threads = []
        
        # Create and start Nasdaq Basic consumer thread
        basic_thread = threading.Thread(
            target=self._consume_topic_group,
            args=(self.nasdaq_basic_topics, "nasdaq_basic")
        )
        basic_thread.daemon = True
        self.consumer_threads.append(basic_thread)
        
        # Create and start NLS Plus consumer thread
        nls_thread = threading.Thread(
            target=self._consume_topic_group,
            args=(self.nls_plus_topics, "nls_plus")
        )
        nls_thread.daemon = True
        self.consumer_threads.append(nls_thread)
        
        # Create and start processor thread
        processor_thread = threading.Thread(target=self._process_data_queue)
        processor_thread.daemon = True
        self.consumer_threads.append(processor_thread)
        
        # Start all threads
        for thread in self.consumer_threads:
            thread.start()
        
        logger.info("NASDAQ API Consumer started")
    
    def stop(self):
        """Stop consuming NASDAQ data"""
        logger.info("Stopping NASDAQ API Consumer...")
        self.running = False
        
        # Wait for threads to terminate
        for thread in self.consumer_threads:
            thread.join(timeout=2.0)
        
        logger.info("NASDAQ API Consumer stopped")
    
    def _consume_topic_group(self, topics, group_name):
        """
        Consume messages from a group of topics
        
        Args:
            topics (list): List of Kafka topics to consume
            group_name (str): Name of the topic group (for logging and consumer group ID)
        """
        logger.info(f"Starting consumer for {group_name} topics: {topics}")
        
        # Create unique consumer group ID for this region
        # Start with a fresh consumer group each time to ensure we get data
        # Include timestamp to ensure uniqueness
        timestamp = int(time.time())
        group_id = f"tee-{self.region_id}-{group_name}-{timestamp}"
        
        logger.info(f"Using consumer group ID: {group_id}")
        
        while self.running:
            try:
                # Create base configuration
                config = self.create_sasl_config()
                
                # Add consumer-specific configuration
                config.update({
                    'group.id': group_id,
                    'auto.offset.reset': 'earliest',  # Start from earliest available offset
                    'enable.auto.commit': True,
                    'debug': 'consumer,topic'  # Enable debug logging for consumer and topic
                })
                
                # Create consumer instance
                consumer = Consumer(config)
                
                # Subscribe to topics
                consumer.subscribe(topics)
                logger.info(f"Subscribed to {group_name} topics: {topics}")
                
                # Log metadata for each topic to verify connectivity
                try:
                    for topic in topics:
                        metadata = consumer.list_topics(topic, timeout=5.0)
                        if metadata:
                            logger.info(f"Topic metadata for {topic}: {metadata.topics[topic]}")
                            for partition in metadata.topics[topic].partitions:
                                partition_info = metadata.topics[topic].partitions[partition]
                                logger.info(f"{topic} Partition {partition}: Leader={partition_info.leader}, Replicas={partition_info.replicas}")
                                
                except Exception as e:
                    logger.warning(f"Could not fetch metadata for {topic}: {str(e)}")
                
                try:
                    # Main consumer loop
                    while self.running:
                        # Poll for messages with timeout
                        message = consumer.poll(timeout=1.0)
                        
                        # Log periodic status to show we're still polling
                        if not message and (time.time() % 10) < 1.0:  # Log roughly every 10 seconds
                            logger.info(f"Still polling {group_name} topics, waiting for messages...")
                        
                        if message is None:
                            continue
                        
                        if message.error():
                            if message.error().code() == KafkaError._PARTITION_EOF:
                                # End of partition, not an error
                                continue
                            elif message.error().code() == KafkaError._TRANSPORT:
                                # Transport error, retry with new token
                                logger.warning("Transport error, refreshing token...")
                                break  # Break out of inner loop to recreate consumer
                            else:
                                logger.error(f"Kafka error: {message.error()}")
                                continue
                        
                        # Process the message
                        try:
                            topic = message.topic()
                            value = message.value()
                            
                            logger.info(f"Received message from topic: {topic}")
                            
                            # Parse the message and add to queue
                            if group_name == "nasdaq_basic":
                                data = self._parse_nasdaq_basic(value, topic)
                            else:
                                data = self._parse_nls_plus(value, topic)
                            
                            self.data_queue.put(data)
                            
                        except Exception as e:
                            logger.error(f"Error processing message: {str(e)}")
                            
                finally:
                    # Clean up consumer
                    try:
                        consumer.close()
                        logger.info(f"Closed {group_name} consumer")
                    except Exception as e:
                        logger.error(f"Error closing consumer: {str(e)}")
                
                # If we get here and still running, wait before reconnecting
                if self.running:
                    logger.info(f"Reconnecting {group_name} consumer in 5 seconds...")
                    time.sleep(5)
                    
            except Exception as e:
                logger.error(f"Error in {group_name} consumer: {str(e)}")
                if self.running:
                    # Only sleep if we're still supposed to be running
                    time.sleep(5)
    
    def _parse_nasdaq_basic(self, message_value, topic):
        """
        Parse Nasdaq Basic (QBBO) message
        
        Args:
            message_value (bytes): Raw message value
            topic (str): Kafka topic of the message
            
        Returns:
            dict: Structured data for TEE processing
        """
        try:
            # Try to parse as JSON first
            try:
                data = json.loads(message_value)
                
            except json.JSONDecodeError:
                # Try parsing as delimited format
                data_str = message_value.decode('utf-8')
                fields = data_str.split('|')
                
                # Parse based on QBBO format (simplified, adjust according to actual format)
                data = {
                    "symbol": fields[0] if len(fields) > 0 else "",
                    "bid_price": fields[1] if len(fields) > 1 else "",
                    "ask_price": fields[2] if len(fields) > 2 else "",
                    "bid_size": fields[3] if len(fields) > 3 else "",
                    "ask_size": fields[4] if len(fields) > 4 else "",
                    "timestamp": fields[5] if len(fields) > 5 else "",
                    "raw_data": data_str
                }
            
            # Extract tape from topic
            tape = topic.split('-')[1]  # A, B, or C
            
            # Add metadata for TEE processing
            tee_data = {
                "nasdaq_basic": data,
                "tee_metadata": {
                    "source_region": self.region_id,
                    "timestamp": datetime.now().isoformat(),
                    "data_type": "nasdaq_basic",
                    "tape": tape,
                    "topic": topic,
                    "security_level": "market_data"
                }
            }
            
            return tee_data
                
        except Exception as e:
            logger.error(f"Error parsing Nasdaq Basic message: {str(e)}")
            
            # Return partial data for debugging
            return {
                "nasdaq_basic": {"raw": str(message_value)[:200]},
                "tee_metadata": {
                    "source_region": self.region_id,
                    "timestamp": datetime.now().isoformat(),
                    "data_type": "nasdaq_basic",
                    "topic": topic,
                    "security_level": "market_data",
                    "parse_error": str(e)
                }
            }
    
    def _parse_nls_plus(self, message_value, topic):
        """
        Parse NLS Plus message
        
        Args:
            message_value (bytes): Raw message value
            topic (str): Kafka topic of the message
            
        Returns:
            dict: Structured data for TEE processing
        """
        try:
            # Try to parse as JSON first
            try:
                data = json.loads(message_value)
                
            except json.JSONDecodeError:
                # Try parsing as delimited format
                data_str = message_value.decode('utf-8')
                fields = data_str.split('|')
                
                # Parse based on NLS Plus format (simplified, adjust according to actual format)
                data = {
                    "symbol": fields[0] if len(fields) > 0 else "",
                    "price": fields[1] if len(fields) > 1 else "",
                    "size": fields[2] if len(fields) > 2 else "",
                    "trade_id": fields[3] if len(fields) > 3 else "",
                    "timestamp": fields[4] if len(fields) > 4 else "",
                    "raw_data": data_str
                }
            
            # Add metadata for TEE processing
            tee_data = {
                "nls_plus": data,
                "tee_metadata": {
                    "source_region": self.region_id,
                    "timestamp": datetime.now().isoformat(),
                    "data_type": "nls_plus",
                    "topic": topic,
                    "security_level": "market_data"
                }
            }
            
            return tee_data
                
        except Exception as e:
            logger.error(f"Error parsing NLS Plus message: {str(e)}")
            
            # Return partial data for debugging
            return {
                "nls_plus": {"raw": str(message_value)[:200]},
                "tee_metadata": {
                    "source_region": self.region_id,
                    "timestamp": datetime.now().isoformat(),
                    "data_type": "nls_plus",
                    "topic": topic,
                    "security_level": "market_data",
                    "parse_error": str(e)
                }
            }
    
    def _process_data_queue(self):
        """
        Process data from the queue and send to TEE mesh network
        """
        while self.running:
            try:
                # Get data from the queue with timeout
                try:
                    data = self.data_queue.get(timeout=1.0)
                except queue.Empty:
                    continue
                
                # Send to TEE mesh network
                try:
                    self._send_to_tee_mesh(data)
                except Exception as e:
                    logger.error(f"Error sending data to TEE mesh: {str(e)}")
                
                # Mark as done
                self.data_queue.task_done()
                
            except Exception as e:
                logger.error(f"Error in data processing thread: {str(e)}")
                time.sleep(1)
    
    def _send_to_tee_mesh(self, data):
        """
        Send data to TEE mesh network
        
        Args:
            data (dict): Data to send to TEE mesh
        """
        # Prepare headers
        headers = {
            'Content-Type': 'application/json',
            'X-TEE-Region-ID': self.region_id,
            'X-TEE-Source': 'nasdaq-api-consumer'
        }
        
        # Extract data type for logging
        data_type = data.get('tee_metadata', {}).get('data_type', 'unknown')
        topic = data.get('tee_metadata', {}).get('topic', 'unknown')
        
        try:
            # Send data to TEE mesh endpoint
            response = requests.post(
                self.tee_endpoint,
                headers=headers,
                json=data,
                timeout=5.0
            )
            
            if response.status_code == 200:
                logger.info(f"Sent {data_type} data from {topic} to TEE mesh")
            else:
                logger.warning(f"Failed to send {data_type} data to TEE mesh: {response.status_code}")
                logger.debug(f"Response: {response.text}")
        
        except Exception as e:
            logger.error(f"Error sending data to TEE mesh: {str(e)}")
            # Retry logic could be added here if needed

def main():
    """Main entry point for NASDAQ API Consumer"""
    parser = argparse.ArgumentParser(description='NASDAQ Cloud Data Service API Consumer for TEE Integration')
    
    parser.add_argument('--client-id', required=True, help='OAuth2 Client ID')
    parser.add_argument('--client-secret', required=True, help='OAuth2 Client Secret')
    parser.add_argument('--token-endpoint', required=True, help='OAuth2 Token Endpoint URL')
    parser.add_argument('--bootstrap-server', required=True, help='Kafka Bootstrap Server')
    parser.add_argument('--tee-endpoint', default='http://localhost:8080/api/v1/nasdaq-data', 
                       help='TEE Mesh Network Endpoint')
    parser.add_argument('--region', default='us-east', help='Region ID for this TEE instance')
    parser.add_argument('--log-level', default='INFO', 
                       choices=['DEBUG', 'INFO', 'WARNING', 'ERROR', 'CRITICAL'],
                       help='Logging level')
    
    args = parser.parse_args()
    
    # Set logging level
    logging.getLogger().setLevel(getattr(logging, args.log_level))
    
    # Initialize the API consumer
    consumer = NasdaqApiConsumer(
        client_id=args.client_id,
        client_secret=args.client_secret,
        token_endpoint=args.token_endpoint,
        bootstrap_server=args.bootstrap_server,
        tee_endpoint=args.tee_endpoint,
        region_id=args.region
    )
    
    try:
        # Start the consumer
        consumer.start()
        
        # Keep the main thread alive
        while True:
            time.sleep(1)
            
    except KeyboardInterrupt:
        logger.info("Shutting down NASDAQ API Consumer...")
        consumer.stop()
        sys.exit(0)

if __name__ == "__main__":
    main()
