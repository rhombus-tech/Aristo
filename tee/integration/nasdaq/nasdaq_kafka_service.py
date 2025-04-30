#!/usr/bin/env python3
# NASDAQ Cloud Data Service Integration for TEE Tokenization Platform
# This service consumes Nasdaq Basic and NLS Plus data via Kafka and integrates it into the TEE mesh network

import os
import sys
import json
import time
import logging
import argparse
import requests
import threading
import queue
from datetime import datetime
import base64
from confluent_kafka import Consumer, KafkaError, KafkaException
import subprocess

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger("NASDAQ-TEE-Integration")

# Install dependencies if needed
try:
    from confluent_kafka import Consumer, KafkaError, KafkaException
    import requests
except ImportError:
    logger.info("Installing required dependencies...")
    subprocess.check_call([sys.executable, "-m", "pip", "install", "confluent-kafka", "requests"])
    from confluent_kafka import Consumer, KafkaError, KafkaException
    import requests

class NasdaqKafkaIntegration:
    """
    Integration service between NASDAQ Cloud Data Service (Kafka) and the TEE mesh network.
    Handles Nasdaq Basic (QBBO) and NLS Plus data feeds.
    """
    
    def __init__(self, client_id, client_secret, token_endpoint, bootstrap_server, 
                 tee_mesh_endpoint, region_id="us-east"):
        """
        Initialize the NASDAQ-TEE Kafka integration service
        
        Args:
            client_id (str): OAuth2 client ID
            client_secret (str): OAuth2 client secret
            token_endpoint (str): OAuth2 token endpoint URL
            bootstrap_server (str): Kafka bootstrap server address
            tee_mesh_endpoint (str): Endpoint for the TEE mesh network
            region_id (str): Region ID for this TEE instance
        """
        self.client_id = client_id
        self.client_secret = client_secret
        self.token_endpoint = token_endpoint
        self.bootstrap_server = bootstrap_server
        self.tee_mesh_endpoint = tee_mesh_endpoint
        self.region_id = region_id
        
        self.access_token = None
        self.token_expiry = 0
        self.consumers = {}
        self.data_queue = queue.Queue()
        self.running = False
        
        # Define the topics we're entitled to
        # When not using NASDAQ SDK, we need to append .stream to topic names
        self.nasdaq_basic_topics = ["QBBO-A-CORE.stream", "QBBO-B-CORE.stream", "QBBO-C-CORE.stream"]
        self.nls_plus_topics = ["NLSCTA.stream", "NLSUTP.stream"]
        
        # Map stream topic names to their original names for internal reference
        self._topic_name_mapping = {
            "QBBO-A-CORE.stream": "QBBO-A-CORE",
            "QBBO-B-CORE.stream": "QBBO-B-CORE",
            "QBBO-C-CORE.stream": "QBBO-C-CORE",
            "NLSCTA.stream": "NLSCTA",
            "NLSUTP.stream": "NLSUTP"
        }
        
        logger.info(f"NASDAQ-TEE Kafka Integration initialized for region {region_id}")
    
    def _get_oauth_token(self):
        """
        Get OAuth2 token for Kafka authentication
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
            
            logger.info(f"Successfully obtained OAuth token, expires in {expires_in} seconds")
            return self.access_token
            
        except Exception as e:
            logger.error(f"Failed to get OAuth token: {str(e)}")
            raise
    
    def _create_consumer(self, topics, group_id_suffix):
        """
        Create a Kafka consumer for the specified topics
        
        Args:
            topics (list): List of Kafka topics to consume
            group_id_suffix (str): Suffix for the consumer group ID
            
        Returns:
            Consumer: Configured Kafka consumer
        """
        try:
            # Get fresh token
            token = self._get_oauth_token()
            
            # Use the NASDAQ-provided group ID as requested by NASDAQ (Apr 2025)
            group_id = "rhombustechnologies-tal-zisckindt"
            
            # Configure Kafka consumer
            # Create OAuth callback that returns the required tuple format
            def oauth_callback(config):
                # The callback must return (token_str, expiry_time[, principal, extensions])
                # NOTE: Exact format is critical, just like in WebAssembly parameter validation
                return (token, int(self.token_expiry))
                
            config = {
                'bootstrap.servers': self.bootstrap_server,
                'group.id': group_id,
                'auto.offset.reset': 'latest',
                'enable.auto.commit': True,
                'security.protocol': 'SASL_SSL',
                'sasl.mechanisms': 'OAUTHBEARER',
                'oauth_cb': oauth_callback
            }
            
            # Create consumer
            consumer = Consumer(config)
            
            # Subscribe to topics
            consumer.subscribe(topics)
            logger.info(f"Created consumer for topics: {topics}")
            
            return consumer
            
        except Exception as e:
            logger.error(f"Failed to create Kafka consumer: {str(e)}")
            raise
    
    def start(self):
        """Start the Kafka integration service"""
        self.running = True
        
        # Create consumers
        self.consumers['nasdaq_basic'] = self._create_consumer(self.nasdaq_basic_topics, "nasdaq-basic")
        self.consumers['nls_plus'] = self._create_consumer(self.nls_plus_topics, "nls-plus")
        
        # Start threads
        self.consumer_threads = []
        
        # Start Nasdaq Basic consumer thread
        basic_thread = threading.Thread(
            target=self._consume_messages,
            args=(self.consumers['nasdaq_basic'], 'nasdaq_basic')
        )
        basic_thread.daemon = True
        self.consumer_threads.append(basic_thread)
        
        # Start NLS Plus consumer thread
        nls_thread = threading.Thread(
            target=self._consume_messages,
            args=(self.consumers['nls_plus'], 'nls_plus')
        )
        nls_thread.daemon = True
        self.consumer_threads.append(nls_thread)
        
        # Start processor thread
        processor_thread = threading.Thread(target=self._secure_data_processor)
        processor_thread.daemon = True
        self.consumer_threads.append(processor_thread)
        
        # Start all threads
        for thread in self.consumer_threads:
            thread.start()
        
        logger.info("NASDAQ-TEE Kafka Integration service started")
    
    def stop(self):
        """Stop the Kafka integration service"""
        self.running = False
        
        # Close all consumers
        for name, consumer in self.consumers.items():
            try:
                consumer.close()
                logger.info(f"Closed {name} consumer")
            except Exception as e:
                logger.error(f"Error closing {name} consumer: {str(e)}")
        
        # Wait for threads to terminate
        for thread in self.consumer_threads:
            thread.join(timeout=2.0)
        
        logger.info("NASDAQ-TEE Kafka Integration service stopped")
    
    def _consume_messages(self, consumer, data_type):
        """
        Thread function to consume messages from Kafka topics
        
        Args:
            consumer (Consumer): Kafka consumer instance
            data_type (str): Type of data being consumed ('nasdaq_basic' or 'nls_plus')
        """
        try:
            while self.running:
                # Poll for messages with timeout
                message = consumer.poll(timeout=1.0)
                
                if message is None:
                    continue
                
                if message.error():
                    if message.error().code() == KafkaError._PARTITION_EOF:
                        # End of partition, not an error
                        continue
                    elif message.error().code() == KafkaError._TRANSPORT:
                        # Connection error, try to refresh token
                        logger.warning("Transport error, refreshing OAuth token...")
                        self._get_oauth_token()
                        time.sleep(1)
                        continue
                    else:
                        logger.error(f"Kafka error: {message.error()}")
                        continue
                
                # Process the message
                try:
                    topic = message.topic()
                    value = message.value()
                    
                    # Parse the message value
                    try:
                        # Different parsing based on message format
                        if data_type == 'nasdaq_basic':
                            # QBBO messages
                            data = self._parse_nasdaq_basic(value, topic)
                        else:
                            # NLS Plus messages
                            data = self._parse_nls_plus(value, topic)
                        
                        # Add to processing queue
                        self.data_queue.put(data)
                        
                    except Exception as e:
                        logger.error(f"Error parsing message from {topic}: {str(e)}")
                        
                except Exception as e:
                    logger.error(f"Error processing message: {str(e)}")
                    
        except Exception as e:
            logger.error(f"Fatal error in consumer thread: {str(e)}")
            if self.running:
                # Only report if we didn't stop intentionally
                logger.error("Consumer thread stopped unexpectedly")
    
    def _parse_nasdaq_basic(self, message_value, topic):
        """
        Parse Nasdaq Basic (QBBO) message
        
        Args:
            message_value (bytes): Raw message value
            topic (str): Kafka topic of the message (with .stream suffix when not using SDK)
            
        Returns:
            dict: Structured data for TEE processing
        """
        # Get original topic name without .stream suffix for internal use
        original_topic = self._topic_name_mapping.get(topic, topic)
        try:
            # Attempt to parse as JSON first
            try:
                data = json.loads(message_value)
                tape = topic.split('-')[1]  # A, B, or C
                
                # Add metadata for TEE processing
                tee_data = {
                    "nasdaq_basic": data,
                    "tee_metadata": {
                        "source_region": self.region_id,
                        "timestamp": datetime.now().isoformat(),
                        "data_type": "nasdaq_basic",
                        "tape": tape,
                        "security_level": "market_data"
                    }
                }
                return tee_data
                
            except json.JSONDecodeError:
                # Try parsing as delimited format
                # Format depends on the specific QBBO format (refer to documentation)
                data_str = message_value.decode('utf-8')
                fields = data_str.split('|')
                
                # Parse based on QBBO format - adjust field indices based on documentation
                parsed_data = {
                    "symbol": fields[0] if len(fields) > 0 else "",
                    "bid_price": float(fields[1]) if len(fields) > 1 else 0,
                    "ask_price": float(fields[2]) if len(fields) > 2 else 0,
                    "bid_size": int(fields[3]) if len(fields) > 3 else 0,
                    "ask_size": int(fields[4]) if len(fields) > 4 else 0,
                    "timestamp": fields[5] if len(fields) > 5 else "",
                }
                
                # Get the tape from the original topic name (A, B, or C)
                # Handle both formats: with or without .stream suffix
                if original_topic != topic:  # We have a mapping
                    tape = original_topic.split('-')[1] if '-' in original_topic else ''
                else:  # No mapping found, use the topic as is
                    tape = topic.split('-')[1] if '-' in topic else ''
                    
                # Add metadata for TEE processing
                tee_data = {
                    "nasdaq_basic": parsed_data,
                    "tee_metadata": {
                        "source_region": self.region_id,
                        "timestamp": datetime.now().isoformat(),
                        "data_type": "nasdaq_basic",
                        "tape": tape,
                        "security_level": "market_data"
                    }
                }
                return tee_data
                
        except Exception as e:
            logger.error(f"Error parsing Nasdaq Basic message: {str(e)}")
            raise
    
    def _parse_nls_plus(self, message_value, topic):
        """
        Parse NLS Plus message
        
        Args:
            message_value (bytes): Raw message value
            topic (str): Kafka topic of the message (with .stream suffix when not using SDK)
            
        Returns:
            dict: Structured data for TEE processing
        """
        # Get original topic name without .stream suffix for internal use
        original_topic = self._topic_name_mapping.get(topic, topic)
        try:
            # First try to parse as JSON (if it's in JSON format)
            try:
                data = json.loads(message_value)
                
                # Add metadata for TEE processing
                tee_data = {
                    "nls_plus": data,
                    "tee_metadata": {
                        "source_region": self.region_id,
                        "timestamp": datetime.now().isoformat(),
                        "data_type": "nls_plus",
                        "topic": original_topic,  # Use original topic name without .stream  # NLSCTA or NLSUTP
                        "security_level": "market_data"
                    }
                }
                return tee_data
                
            except json.JSONDecodeError:
                # Handle binary format - NLS Plus messages are typically binary data
                # Create a binary representation with fields based on NASDAQ spec
                # Instead of trying to decode as text, we'll process as binary
                
                # Create a hexdump for debugging
                hex_repr = message_value.hex()
                logger.debug(f"NLS Plus binary message: {hex_repr[:50]}... ({len(message_value)} bytes)")
                
                # Extract binary fields according to NASDAQ NLS Plus format
                # This is a simplified example - adjust according to actual NASDAQ specs
                if len(message_value) >= 8:  # Ensure we have enough data for basic header
                    # Parse basic binary structure
                    # The actual parsing logic depends on the specific NLS Plus binary format
                    # This is just a placeholder - replace with actual format parsing
                    
                    # Example: First 2 bytes might be message type
                    msg_type = int.from_bytes(message_value[0:2], byteorder='big')
                    
                    # Create parsed representation
                    parsed_data = {
                        "msg_type": msg_type,
                        "binary_size": len(message_value),
                        "binary_prefix": hex_repr[:20],  # First 10 bytes in hex for reference
                        "topic": original_topic
                    }
                    
                    # Add timestamp from system time since we can't parse it from binary yet
                    parsed_data["timestamp"] = datetime.now().isoformat()
                    
                    # Add metadata for TEE processing
                    tee_data = {
                        "nls_plus": parsed_data,
                        "tee_metadata": {
                            "source_region": self.region_id,
                            "timestamp": datetime.now().isoformat(),
                            "data_type": "nls_plus",
                            "topic": original_topic,
                            "format": "binary",
                            "security_level": "market_data"
                        }
                    }
                    return tee_data
                else:
                    # Message too small to contain valid data
                    logger.warning(f"NLS Plus message too small: {len(message_value)} bytes")
                    raise ValueError(f"Message too small: {len(message_value)} bytes")
                
        except Exception as e:
            logger.error(f"Error parsing NLS Plus message: {str(e)}")
            raise
    
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
                'X-TEE-Source': 'nasdaq-kafka-integration'
            }
            
            # Send data to TEE mesh endpoint
            response = requests.post(
                self.tee_mesh_endpoint,
                headers=headers,
                json=data,
                timeout=5.0
            )
            
            data_type = data['tee_metadata']['data_type']
            
            if response.status_code == 200:
                logger.debug(f"Successfully sent {data_type} data to TEE mesh")
            else:
                logger.warning(f"Failed to send {data_type} data to TEE mesh: {response.status_code} - {response.text}")
                
        except Exception as e:
            logger.error(f"Error sending data to TEE mesh: {str(e)}")

def main():
    """Main entry point for the NASDAQ-TEE Kafka integration service"""
    parser = argparse.ArgumentParser(description='NASDAQ Cloud Data Service Kafka Integration for TEE Mesh Network')
    
    parser.add_argument('--client-id', required=True, help='OAuth2 Client ID')
    parser.add_argument('--client-secret', required=True, help='OAuth2 Client Secret')
    parser.add_argument('--token-endpoint', required=True, help='OAuth2 Token Endpoint URL')
    parser.add_argument('--bootstrap-server', required=True, help='Kafka Bootstrap Server')
    parser.add_argument('--tee-endpoint', default='http://localhost:8080/api/v1/nasdaq-data', 
                       help='TEE Mesh Network Endpoint')
    parser.add_argument('--region', default='us-east', help='Region ID for this TEE instance')
    
    args = parser.parse_args()
    
    # Initialize and start the integration service
    integration = NasdaqKafkaIntegration(
        client_id=args.client_id,
        client_secret=args.client_secret,
        token_endpoint=args.token_endpoint,
        bootstrap_server=args.bootstrap_server,
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
        logger.info("Shutting down NASDAQ-TEE Kafka integration service...")
        integration.stop()
        sys.exit(0)

if __name__ == "__main__":
    main()
