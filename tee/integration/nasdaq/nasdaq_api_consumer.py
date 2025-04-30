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
import gc
from datetime import datetime
from traceback import format_exc
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
        # Limit queue size to prevent memory overflow
        self.data_queue = queue.Queue(maxsize=1000)
        # Add circuit breaker and rate limiting
        self.circuit_open = False
        self.circuit_reset_time = 0
        self.consecutive_errors = 0
        self.error_threshold = 5
        self.circuit_reset_timeout = 60  # seconds
        self.message_count = 0
        self.last_rate_check = time.time()
        self.rate_limit = 5000  # messages per second
        
        # Define topic groups based on your entitlements
        # When not using NASDAQ SDK, we need to append .stream to topic names
        self.nasdaq_basic_topics = ["QBBO-A-CORE.stream", "QBBO-B-CORE.stream", "QBBO-C-CORE.stream"]
        self.nls_plus_topics = ["NLSCTA.stream", "NLSUTP.stream"]
        
        # Store original topic names (without .stream) for internal reference and parsing
        self._topic_name_mapping = {
            "QBBO-A-CORE.stream": "QBBO-A-CORE",
            "QBBO-B-CORE.stream": "QBBO-B-CORE",
            "QBBO-C-CORE.stream": "QBBO-C-CORE",
            "NLSCTA.stream": "NLSCTA",
            "NLSUTP.stream": "NLSUTP"
        }
        
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
        self.circuit_open = False
        self.consecutive_errors = 0
        
        # Start memory monitor thread
        memory_monitor_thread = threading.Thread(target=self._monitor_memory_usage)
        memory_monitor_thread.daemon = True
        self.consumer_threads = [memory_monitor_thread]
        
        # Start rate limiter thread
        rate_limiter_thread = threading.Thread(target=self._rate_limiter)
        rate_limiter_thread.daemon = True
        self.consumer_threads.append(rate_limiter_thread)
        
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
        
        logger.info("NASDAQ API Consumer started with memory monitoring and rate limiting")
    
    def stop(self):
        """Stop consuming NASDAQ data"""
        logger.info("Stopping NASDAQ API Consumer...")
        self.running = False
        
        # Wait for threads to terminate
        for thread in self.consumer_threads:
            thread.join(timeout=2.0)
        
        logger.info("NASDAQ API Consumer stopped")
    
    def _monitor_memory_usage(self):
        """
        Monitor memory usage and pause consumption if it gets too high
        Also triggers garbage collection when needed
        """
        try:
            import psutil
        except ImportError:
            logger.warning("psutil not available, memory monitoring disabled")
            return
            
        process = psutil.Process(os.getpid())
        warning_threshold = 70.0  # percent - lower to be more proactive
        critical_threshold = 85.0  # percent - lower to avoid crashes
        
        # Track gc runs
        last_gc_time = time.time()
        gc_interval = 60  # Run GC at least every minute
        
        while self.running:
            try:
                # Get memory usage
                memory_percent = process.memory_percent()
                current_time = time.time()
                
                # Log memory status every 30 seconds
                if current_time % 30 < 1.0:
                    logger.info(f"Memory usage: {memory_percent:.1f}% | Queue size: {self.data_queue.qsize()}")
                
                # Run garbage collection if memory high or time interval passed
                if memory_percent > warning_threshold or (current_time - last_gc_time) > gc_interval:
                    logger.info(f"Running garbage collection (memory: {memory_percent:.1f}%)")
                    gc.collect()
                    last_gc_time = current_time
                
                if memory_percent > critical_threshold:
                    logger.critical(f"Memory usage critical: {memory_percent:.1f}%")
                    if not self.circuit_open:
                        logger.critical("Opening circuit breaker due to memory pressure")
                        self.circuit_open = True
                        self.circuit_reset_time = time.time() + self.circuit_reset_timeout
                        
                    # Force garbage collection
                    gc.collect()
                    
                    # Emergency action: clear part of the queue to free memory
                    try:
                        queue_size = self.data_queue.qsize()
                        if queue_size > 50:
                            logger.critical(f"Emergency action: clearing {queue_size-50} items from queue")
                            # Clear all but 50 items from the queue
                            for i in range(queue_size - 50):
                                try:
                                    self.data_queue.get_nowait()
                                    self.data_queue.task_done()
                                except queue.Empty:
                                    break
                    except Exception as e:
                        logger.error(f"Error clearing queue: {str(e)}")
                        
                elif memory_percent > warning_threshold:
                    logger.warning(f"Memory usage high: {memory_percent:.1f}%")
                    # Slow down processing by adding small delays
                    time.sleep(0.2)
                
                # Check queue size
                queue_size = self.data_queue.qsize()
                if queue_size > 800:  # 80% of max
                    logger.warning(f"Queue size high: {queue_size} items")
                    time.sleep(0.5)  # Add delay to allow processing to catch up
            except MemoryError:
                # Specific handling for memory errors
                logger.critical("Memory error detected in monitor - emergency GC")
                gc.collect()
                time.sleep(1)
            except Exception as e:
                logger.error(f"Error in memory monitor: {str(e)}")
                logger.debug(format_exc())
                
            time.sleep(5)  # Check every 5 seconds
    
    def _rate_limiter(self):
        """
        Monitor and enforce message processing rate limits
        """
        while self.running:
            try:
                current_time = time.time()
                elapsed = current_time - self.last_rate_check
                
                if elapsed >= 1.0:  # Check every second
                    rate = self.message_count / elapsed
                    self.message_count = 0
                    self.last_rate_check = current_time
                    
                    if rate > self.rate_limit:
                        delay = (rate / self.rate_limit) * 0.1  # Proportional delay
                        logger.warning(f"Rate limiting: {rate:.1f} msgs/sec exceeds limit of {self.rate_limit}")
                        time.sleep(delay)
            except Exception as e:
                logger.error(f"Error in rate limiter: {str(e)}")
                
            time.sleep(0.1)  # Check frequently but not too often
    
    def _should_circuit_break(self):
        """
        Determine if circuit breaker should be engaged
        """
        # Reset circuit if timeout has elapsed
        if self.circuit_open and time.time() > self.circuit_reset_time:
            logger.info("Resetting circuit breaker")
            self.circuit_open = False
            self.consecutive_errors = 0
            
        return self.circuit_open
        
    def _record_error(self):
        """
        Record an error and potentially trip the circuit breaker
        """
        self.consecutive_errors += 1
        if self.consecutive_errors >= self.error_threshold and not self.circuit_open:
            logger.warning(f"Circuit breaker tripped after {self.consecutive_errors} consecutive errors")
            self.circuit_open = True
            self.circuit_reset_time = time.time() + self.circuit_reset_timeout
            
    def _record_success(self):
        """
        Record a successful operation
        """
        if self.consecutive_errors > 0:
            self.consecutive_errors = 0
            
    def _consume_topic_group(self, topics, group_name):
        """
        Consume messages from a group of topics
        
        Args:
            topics (list): List of Kafka topics to consume
            group_name (str): Name of the topic group (for logging and consumer group ID)
        """
        logger.info(f"Starting consumer for {group_name} topics: {topics}")
        
        # Use the NASDAQ-provided group ID as requested by NASDAQ (Apr 2025)
        # This ensures proper permissions and data access
        group_id = "rhombustechnologies-tal-zisckindt"
        
        logger.info(f"Using NASDAQ-provided consumer group ID: {group_id}")
        
        # Exponential backoff parameters
        retry_delay = 1
        max_retry_delay = 60
        
        while self.running:
            # Check if circuit breaker is open
            if self._should_circuit_break():
                logger.info(f"Circuit breaker open, waiting {self.circuit_reset_time - time.time():.1f}s before retry")
                time.sleep(5)
                continue
                
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
                
                # Subscribe to topics (with .stream suffix as required by NASDAQ)
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
                            
                            # Check queue size before processing
                            if self.data_queue.qsize() > 900:  # 90% capacity
                                logger.warning(f"Queue near capacity ({self.data_queue.qsize()}/1000), skipping message")
                                # Record successful poll but skipped processing
                                self._record_success()
                                continue
                                
                            # Count this message for rate limiting
                            self.message_count += 1
                            
                            # Parse the message
                            if group_name == "nasdaq_basic":
                                data = self._parse_nasdaq_basic(value, topic)
                            else:
                                data = self._parse_nls_plus(value, topic)
                            
                            # Add to queue with timeout to prevent blocking indefinitely
                            try:
                                self.data_queue.put(data, timeout=1.0)
                                self._record_success()  # Record successful processing
                            except queue.Full:
                                logger.warning("Queue full, dropping message")
                            
                        except Exception as e:
                            logger.error(f"Error processing message: {str(e)}")
                            self._record_error()  # Record error for circuit breaker
                            
                finally:
                    # Clean up consumer
                    try:
                        consumer.close()
                        logger.info(f"Closed {group_name} consumer")
                    except Exception as e:
                        logger.error(f"Error closing consumer: {str(e)}")
                
                # If we get here and still running, wait before reconnecting
                if self.running:
                    # Use exponential backoff for reconnection
                    logger.info(f"Reconnecting {group_name} consumer in {retry_delay} seconds...")
                    time.sleep(retry_delay)
                    retry_delay = min(retry_delay * 2, max_retry_delay)  # Double delay up to max
                    self._record_error()  # Record error for circuit breaker
                    
            except Exception as e:
                logger.error(f"Error in {group_name} consumer: {str(e)}")
                if self.running:
                    # Only sleep if we're still supposed to be running
                    time.sleep(retry_delay)
                    retry_delay = min(retry_delay * 2, max_retry_delay)  # Double delay up to max
                    self._record_error()  # Record error for circuit breaker
    
    def _parse_nasdaq_basic(self, message_value, topic):
        """
        Parse Nasdaq Basic (QBBO) message
        
        Args:
            message_value (bytes): Raw message value
            topic (str): Kafka topic of the message (with .stream suffix)
            
        Returns:
            dict: Structured data for TEE processing
        """
        # Get original topic name without .stream suffix
        original_topic = self._topic_name_mapping.get(topic, topic)
        try:
            # First, check if this is binary data (which is common for market data)
            is_binary = False
            for byte in message_value[:20]:  # Check first 20 bytes
                # Non-printable ASCII or high bytes suggest binary
                if byte < 32 or byte > 126:
                    is_binary = True
                    break
                    
            if is_binary:
                # Treat as binary data - this aligns with our WebAssembly direct format
                # Generate a hex representation for logging and debugging
                hex_preview = message_value[:30].hex()
                logger.debug(f"Processing binary data: {hex_preview}...")
                
                data = {
                    "binary": True,
                    "byte_length": len(message_value),
                    "hex_preview": hex_preview,
                    # Store binary data as base64 for JSON compatibility
                    "data_base64": base64.b64encode(message_value).decode('ascii')
                }
                
            else:
                # Try to parse as JSON first
                try:
                    data = json.loads(message_value)
                    
                except json.JSONDecodeError:
                    # Try various text encodings
                    encodings = ['utf-8', 'ascii', 'latin-1']
                    data_str = None
                    
                    for encoding in encodings:
                        try:
                            data_str = message_value.decode(encoding)
                            logger.debug(f"Successfully decoded using {encoding}")
                            break
                        except UnicodeDecodeError:
                            continue
                    
                    # If all decodings fail, use latin-1 which never fails but might be incorrect
                    if data_str is None:
                        data_str = message_value.decode('latin-1', errors='replace')
                        logger.warning("Falling back to latin-1 with replacement for decode")
                    
                    fields = data_str.split('|')
                    
                    # Parse based on QBBO format (simplified, adjust according to actual format)
                    data = {
                        "symbol": fields[0] if len(fields) > 0 else "",
                        "bid_price": fields[1] if len(fields) > 1 else "",
                        "ask_price": fields[2] if len(fields) > 2 else "",
                        "bid_size": fields[3] if len(fields) > 3 else "",
                        "ask_size": fields[4] if len(fields) > 4 else "",
                        "timestamp": fields[5] if len(fields) > 5 else "",
                        "raw_data": data_str[:1000]  # Limit length for safety
                    }
            
            # Extract tape from topic (handle topics with .stream suffix)
            if ".stream" in topic:
                base_topic = topic.replace(".stream", "")
                tape = base_topic.split('-')[1] if '-' in base_topic else "" # A, B, or C
            else:
                tape = topic.split('-')[1] if '-' in topic else "" # A, B, or C
            
            # Add metadata for TEE processing
            tee_data = {
                "nasdaq_basic": data,
                "tee_metadata": {
                    "source_region": self.region_id,
                    "timestamp": datetime.now().isoformat(),
                    "data_type": "nasdaq_basic",
                    "topic": original_topic,  # Use original topic name (without .stream)
                    "tape": tape,
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
                    "topic": original_topic,  # Use original topic name (without .stream)
                    "tape": original_topic.split('-')[1] if '-' in original_topic else "",
                    "security_level": "market_data",
                    "parse_error": str(e)
                }
            }
    
    def _parse_nls_plus(self, message_value, topic):
        """
        Parse NLS Plus message
        
        Args:
            message_value (bytes): Raw message value
            topic (str): Kafka topic of the message (with .stream suffix)
            
        Returns:
            dict: Structured data for TEE processing
        """
        # Get original topic name without .stream suffix
        original_topic = self._topic_name_mapping.get(topic, topic)
        try:
            # First, check if this is binary data (which is common for market data)
            is_binary = False
            for byte in message_value[:20]:  # Check first 20 bytes
                # Non-printable ASCII or high bytes suggest binary
                if byte < 32 or byte > 126:
                    is_binary = True
                    break
                    
            if is_binary:
                # Treat as binary data - this aligns with our WebAssembly direct format
                # This follows our dual TEE architecture where data can be in direct format
                hex_preview = message_value[:30].hex()
                logger.debug(f"Processing binary data: {hex_preview}...")
                
                data = {
                    "binary": True,
                    "byte_length": len(message_value),
                    "hex_preview": hex_preview,
                    # Store binary data as base64 for JSON compatibility
                    "data_base64": base64.b64encode(message_value).decode('ascii')
                }
                
            else:
                # Try to parse as JSON first
                try:
                    data = json.loads(message_value)
                    
                except json.JSONDecodeError:
                    # Try various text encodings
                    encodings = ['utf-8', 'ascii', 'latin-1']
                    data_str = None
                    
                    for encoding in encodings:
                        try:
                            data_str = message_value.decode(encoding)
                            logger.debug(f"Successfully decoded using {encoding}")
                            break
                        except UnicodeDecodeError:
                            continue
                    
                    # If all decodings fail, use latin-1 which never fails but might be incorrect
                    if data_str is None:
                        data_str = message_value.decode('latin-1', errors='replace')
                        logger.warning("Falling back to latin-1 with replacement for decode")
                    
                    fields = data_str.split('|')
                    
                    # Parse based on NLS Plus format (simplified, adjust according to actual format)
                    data = {
                        "symbol": fields[0] if len(fields) > 0 else "",
                        "price": fields[1] if len(fields) > 1 else "",
                        "size": fields[2] if len(fields) > 2 else "",
                        "trade_id": fields[3] if len(fields) > 3 else "",
                        "timestamp": fields[4] if len(fields) > 4 else "",
                        "raw_data": data_str[:1000]  # Limit length for safety
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
        with enhanced memory management and error handling
        """
        # Create a robust connection pool for requests
        try:
            import requests.adapters
            session = requests.Session()
            # Use more conservative connection pool settings to prevent resource exhaustion
            adapter = requests.adapters.HTTPAdapter(
                pool_connections=5,  # Reduced from 10
                pool_maxsize=10,     # Reduced from 20
                max_retries=3,
                pool_block=True      # Block when pool is full rather than raising error
            )
            session.mount('http://', adapter)
            session.mount('https://', adapter)
            logger.info("Created connection pool for TEE mesh communication")
        except Exception as e:
            logger.warning(f"Could not create connection pool: {str(e)}")
            session = requests  # Fallback to regular requests
        
        # Batch processing variables - start with conservative values
        batch_size = 5  # Reduced from 10 to start
        max_batch_size = 20  # Upper limit based on memory testing
        min_batch_size = 1    # Lower limit during problems
        current_batch_size = batch_size
        
        batch = []
        last_flush_time = time.time()
        max_batch_age = 0.5  # seconds - reduced from 1.0 to be more responsive
        last_success_time = time.time()
        adaptive_delay = 0.0  # Dynamic delay based on system pressure
        
        # Performance tracking
        success_count = 0
        failure_count = 0
        last_performance_check = time.time()
        performance_check_interval = 60  # seconds
        
        logger.info(f"Starting data processing with initial batch size: {batch_size}")
        
        while self.running:
            try:
                # Regular garbage collection to prevent memory buildup
                if time.time() - last_performance_check > performance_check_interval:
                    # Performance-based batch size adaptation
                    total = success_count + failure_count
                    if total > 0:
                        success_rate = success_count / total
                        logger.info(f"Performance stats - Success rate: {success_rate*100:.1f}%, " 
                                   f"Successes: {success_count}, Failures: {failure_count}, " 
                                   f"Current batch size: {current_batch_size}")
                        
                        # Adapt batch size based on success rate
                        if success_rate > 0.95 and current_batch_size < max_batch_size:
                            current_batch_size = min(current_batch_size + 1, max_batch_size)
                            logger.info(f"Increasing batch size to {current_batch_size}")
                        elif success_rate < 0.8 and current_batch_size > min_batch_size:
                            current_batch_size = max(current_batch_size - 1, min_batch_size)
                            logger.info(f"Decreasing batch size to {current_batch_size}")
                    
                    # Reset counters
                    success_count = 0
                    failure_count = 0
                    last_performance_check = time.time()
                    
                    # Force garbage collection periodically
                    gc.collect()
                
                # Check if circuit breaker is open
                if self._should_circuit_break():
                    # Even with circuit open, process any existing batch to prevent memory buildup
                    if batch:
                        logger.warning(f"Processing existing batch of {len(batch)} items before pausing (circuit open)")
                        try:
                            self._send_batch_to_tee_mesh(batch, session)
                        except Exception as e:
                            logger.error(f"Failed to send batch with circuit open: {str(e)}")
                        batch = []
                    
                    time.sleep(1)
                    continue
                
                # Apply adaptive delay if system under pressure
                if adaptive_delay > 0:
                    time.sleep(adaptive_delay)
                    # Gradually reduce delay if it was applied
                    adaptive_delay = max(0, adaptive_delay - 0.01)
                    
                # Get data from the queue with timeout
                try:
                    # Use a short timeout to allow for regular batch flushing
                    data = self.data_queue.get(timeout=0.1)
                    
                    # Validate data before adding to batch
                    try:
                        # Basic validation that it's a well-formed dictionary
                        if not isinstance(data, dict):
                            logger.warning(f"Skipping invalid data type: {type(data)}")
                            self.data_queue.task_done()
                            continue
                            
                        # Check data doesn't contain extremely large fields
                        data_size = len(str(data))
                        if data_size > 500 * 1024:  # 500KB seems excessive
                            logger.warning(f"Data item extremely large ({data_size} bytes), truncating")
                            # Try to truncate any raw_data fields
                            for key in list(data.keys()):
                                if isinstance(data[key], dict) and 'raw_data' in data[key]:
                                    data[key]['raw_data'] = data[key]['raw_data'][:1000] + "... [truncated]"
                        
                        batch.append(data)
                        self.data_queue.task_done()
                    except Exception as data_e:
                        logger.error(f"Error validating data item: {str(data_e)}")
                        self.data_queue.task_done()  # Always mark task as done
                        continue
                    
                except queue.Empty:
                    # No new data, check if we should flush existing batch
                    if batch and (time.time() - last_flush_time > max_batch_age):
                        try:
                            self._send_batch_to_tee_mesh(batch, session)
                            success_count += 1
                            last_success_time = time.time()
                        except Exception as e:
                            logger.error(f"Error sending timeout-triggered batch: {str(e)}")
                            failure_count += 1
                            # If we haven't had success in a while, apply adaptive backoff
                            if time.time() - last_success_time > 30:
                                adaptive_delay = min(adaptive_delay + 0.05, 1.0)  # Max 1 second delay
                                logger.warning(f"No successful sends in 30s, applying backoff: {adaptive_delay:.2f}s")
                        
                        batch = []
                        last_flush_time = time.time()
                    continue
                
                # Send batch if it reaches current batch size
                if len(batch) >= current_batch_size:
                    try:
                        self._send_batch_to_tee_mesh(batch, session)
                        success_count += 1
                        last_success_time = time.time()
                    except Exception as e:
                        logger.error(f"Error sending size-triggered batch: {str(e)}")
                        failure_count += 1
                        # If we haven't had success in a while, apply adaptive backoff
                        if time.time() - last_success_time > 30:
                            adaptive_delay = min(adaptive_delay + 0.05, 1.0)  # Max 1 second delay
                            logger.warning(f"No successful sends in 30s, applying backoff: {adaptive_delay:.2f}s")
                    
                    batch = []
                    last_flush_time = time.time()
                
            except MemoryError:
                logger.critical("Memory error in processing thread - emergency recovery")
                # Emergency memory recovery
                batch = []  # Clear batch to free memory
                gc.collect()  # Force garbage collection
                time.sleep(2)  # Give system time to recover
                
                # Try to clear part of the queue
                try:
                    queue_size = self.data_queue.qsize()
                    if queue_size > 10:
                        logger.critical(f"Emergency action: clearing majority of queue ({queue_size} items)")
                        # Keep only 10 items
                        for i in range(queue_size - 10):
                            try:
                                self.data_queue.get_nowait()
                                self.data_queue.task_done()
                            except queue.Empty:
                                break
                except Exception:
                    pass  # Ignore errors during emergency recovery
                    
                # Reduce batch size after memory error
                current_batch_size = min_batch_size
                logger.warning(f"Reduced batch size to {current_batch_size} after memory error")
                
            except Exception as e:
                logger.error(f"Error in data processing thread: {str(e)}")
                logger.debug(format_exc())
                self._record_error()
                # Clear batch on error to prevent cascading failures
                batch = []
                time.sleep(1)
    
    def _send_batch_to_tee_mesh(self, batch, session):
        """
        Send a batch of data to TEE mesh network with proper WebAssembly parameter format handling
        
        Args:
            batch (list): List of data items to send
            session (requests.Session): Session object for connection pooling
        """
        if not batch:
            return
        
        # Enforce maximum batch size for safety
        max_safe_batch = 20
        if len(batch) > max_safe_batch:
            logger.warning(f"Batch size {len(batch)} exceeds safe limit of {max_safe_batch}, splitting")
            # Process in smaller batches
            for i in range(0, len(batch), max_safe_batch):
                sub_batch = batch[i:i+max_safe_batch]
                self._send_batch_to_tee_mesh(sub_batch, session)
            return
            
        # Prepare headers
        headers = {
            'Content-Type': 'application/json',
            'X-TEE-Region-ID': self.region_id,
            'X-TEE-Source': 'nasdaq-api-consumer',
            'X-TEE-Batch-Size': str(len(batch)),
            'X-TEE-Format': 'length_prefixed'  # Explicitly indicate format for WebAssembly compatibility
        }
        
        try:
            # Convert batch to proper format expected by TEE
            # Format data for WebAssembly parameter format (length-prefixed)
            # This follows our dual TEE architecture requirements
            
            # Prepare each item following WebAssembly parameter pattern
            formatted_batch = []
            for item in batch:
                # Add format identifier to each item
                if 'tee_metadata' not in item:
                    item['tee_metadata'] = {}
                    
                # Ensure we're following the length-prefixed format pattern
                item['tee_metadata']['format'] = 'length_prefixed'
                
                # Validate data size doesn't exceed reasonable WebAssembly memory limits
                item_size = len(str(item))
                if item_size > 1024 * 10:  # 10KB limit
                    logger.warning(f"Item size {item_size} bytes exceeds reasonable limit, truncating")
                    # Truncate large items to prevent memory issues
                    for key in item:
                        if key != 'tee_metadata' and isinstance(item[key], dict) and 'raw_data' in item[key]:
                            item[key]['raw_data'] = item[key]['raw_data'][:1000] + '... [truncated]'
                
                formatted_batch.append(item)
            
            batch_payload = {
                "batch": formatted_batch,
                "batch_metadata": {
                    "count": len(formatted_batch),
                    "timestamp": datetime.now().isoformat(),
                    "source_region": self.region_id,
                    "format": "length_prefixed",  # Explicitly indicate format for WebAssembly compatibility
                    "version": "1.1"  # Add version for compatibility tracking
                }
            }
            
            # Send data to TEE mesh endpoint
            try:
                response = session.post(
                    self.tee_endpoint,
                    headers=headers,
                    json=batch_payload,
                    timeout=5.0
                )
                
                if response.status_code == 200:
                    logger.info(f"Sent batch of {len(batch)} items to TEE mesh")
                    self._record_success()
                else:
                    logger.warning(f"Failed to send batch to TEE mesh: {response.status_code}")
                    logger.debug(f"Response: {response.text}")
                    self._record_error()
                    
                    # If we get a 400 response, the TEE might not support batching or have parameter format issues
                    if response.status_code == 400:
                        logger.warning("Falling back to direct format for individual messages due to 400 response")
                        # Try direct format as fallback for individual items
                        for item in batch:
                            # Update to direct format
                            if 'tee_metadata' in item:
                                item['tee_metadata']['format'] = 'direct'
                            self._send_single_to_tee_mesh(item, session, param_format='direct')
            except requests.exceptions.Timeout:
                logger.warning("Timeout sending batch, retrying with smaller batch size")
                # Split the batch and retry with smaller batches
                half_size = len(batch) // 2
                if half_size > 0:
                    self._send_batch_to_tee_mesh(batch[:half_size], session)
                    self._send_batch_to_tee_mesh(batch[half_size:], session)
                else:
                    # If we can't split further, try individual sending
                    for item in batch:
                        self._send_single_to_tee_mesh(item, session)
            except requests.exceptions.RequestException as req_e:
                logger.error(f"Request error sending batch: {str(req_e)}")
                self._record_error()
                # Fall back to individual sending on request failure
                for item in batch:
                    try:
                        self._send_single_to_tee_mesh(item, session)
                    except Exception:
                        pass  # Already logged in send_single
                        
        except MemoryError:
            logger.critical("Memory error while preparing batch - emergency recovery")
            gc.collect()  # Force garbage collection
            # Try with much smaller batch or individual items if needed
            if len(batch) > 5:
                self._send_batch_to_tee_mesh(batch[:2], session)  # Just try with first 2
            else:
                # Last resort - try one by one
                for item in batch[:2]:  # Just try first 2 to recover
                    try:
                        self._send_single_to_tee_mesh(item, session)
                    except Exception:
                        pass  # Just trying to recover
        
        except Exception as e:
            logger.error(f"Error sending batch to TEE mesh: {str(e)}")
            logger.debug(format_exc())
            self._record_error()
            # Fall back to individual sending on failure, but limit to avoid cascading failures
            max_fallback = min(5, len(batch))
            for item in batch[:max_fallback]:
                try:
                    self._send_single_to_tee_mesh(item, session)
                except Exception as inner_e:
                    logger.error(f"Error in fallback individual send: {str(inner_e)}")
    
    def _send_single_to_tee_mesh(self, data, session, param_format='length_prefixed'):
        """
        Send a single data item to TEE mesh network
        
        Args:
            data (dict): Data to send to TEE mesh
            session (requests.Session): Session object for connection pooling
            param_format (str): Parameter format - either 'length_prefixed' or 'direct'
        """
        # Prepare headers
        headers = {
            'Content-Type': 'application/json',
            'X-TEE-Region-ID': self.region_id,
            'X-TEE-Source': 'nasdaq-api-consumer',
            'X-TEE-Format': param_format  # Explicitly indicate format for WebAssembly compatibility
        }
        
        # Extract data type for logging
        data_type = data.get('tee_metadata', {}).get('data_type', 'unknown')
        topic = data.get('tee_metadata', {}).get('topic', 'unknown')
        
        try:
            # Ensure the data is properly formatted for TEE mesh parameter validation
            # Apply appropriate format according to our dual TEE architecture requirements
            if 'tee_metadata' not in data:
                data['tee_metadata'] = {}
                
            # Set format in metadata
            data['tee_metadata']['format'] = param_format
            
            # Validate data size doesn't exceed reasonable WebAssembly memory limits
            item_size = len(str(data))
            if item_size > 1024 * 10:  # 10KB limit
                logger.warning(f"Item size {item_size} bytes exceeds reasonable limit, truncating")
                # Truncate large items to prevent memory issues
                for key in data:
                    if key != 'tee_metadata' and isinstance(data[key], dict) and 'raw_data' in data[key]:
                        data[key]['raw_data'] = data[key]['raw_data'][:1000] + '... [truncated]'
            
            formatted_data = data
            
            # Send data to TEE mesh endpoint with retry logic
            max_retries = 2
            retry_count = 0
            success = False
            
            while retry_count <= max_retries and not success:
                try:
                    response = session.post(
                        self.tee_endpoint,
                        headers=headers,
                        json=formatted_data,
                        timeout=5.0
                    )
                    
                    if response.status_code == 200:
                        if retry_count > 0:
                            logger.info(f"Successfully sent {data_type} data after {retry_count} retries")
                        else:
                            logger.debug(f"Sent {data_type} data from {topic} to TEE mesh")
                        self._record_success()
                        success = True
                    else:
                        # If we get a 400 and using length_prefixed, try direct format
                        if response.status_code == 400 and param_format == 'length_prefixed' and retry_count == 0:
                            logger.warning(f"Failed with length_prefixed format, trying direct format")
                            headers['X-TEE-Format'] = 'direct'
                            data['tee_metadata']['format'] = 'direct'
                            retry_count += 1
                            continue
                            
                        logger.warning(f"Failed to send {data_type} data to TEE mesh: {response.status_code}")
                        logger.debug(f"Response: {response.text}")
                        self._record_error()
                        retry_count += 1
                        time.sleep(0.5 * retry_count)  # Increasing backoff
                        
                except requests.exceptions.Timeout:
                    logger.warning(f"Timeout sending data, retry {retry_count+1}/{max_retries+1}")
                    retry_count += 1
                    time.sleep(0.5 * retry_count)  # Increasing backoff
                    
                except requests.exceptions.RequestException as req_e:
                    logger.error(f"Request error: {str(req_e)}, retry {retry_count+1}/{max_retries+1}")
                    retry_count += 1
                    time.sleep(0.5 * retry_count)  # Increasing backoff
        
        except MemoryError:
            logger.critical("Memory error in single item send - emergency recovery")
            gc.collect()  # Force garbage collection
            # We won't retry this item to avoid further memory issues
            
        except Exception as e:
            logger.error(f"Error sending data to TEE mesh: {str(e)}")
            logger.debug(format_exc())
            self._record_error()

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
    parser.add_argument('--queue-size', type=int, default=1000, help='Maximum queue size')
    parser.add_argument('--rate-limit', type=int, default=5000, help='Maximum messages per second')
    parser.add_argument('--batch-size', type=int, default=10, help='Batch size for TEE mesh sends')
    
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
    
    # Configure based on command line args
    consumer.data_queue = queue.Queue(maxsize=args.queue_size)
    consumer.rate_limit = args.rate_limit
    
    try:
        # Start the consumer
        consumer.start()
        
        # Keep the main thread alive and monitor health
        start_time = time.time()
        last_status_time = start_time
        
        while True:
            current_time = time.time()
            # Print status every minute
            if current_time - last_status_time >= 60:
                uptime = current_time - start_time
                hours, remainder = divmod(uptime, 3600)
                minutes, seconds = divmod(remainder, 60)
                
                queue_size = consumer.data_queue.qsize()
                queue_percent = (queue_size / consumer.data_queue.maxsize) * 100
                
                logger.info(f"Consumer status - Uptime: {int(hours)}h {int(minutes)}m {int(seconds)}s | " 
                           f"Queue: {queue_size}/{consumer.data_queue.maxsize} ({queue_percent:.1f}%) | " 
                           f"Circuit breaker: {'OPEN' if consumer.circuit_open else 'CLOSED'}")
                
                last_status_time = current_time
                
            time.sleep(1)
            
    except KeyboardInterrupt:
        logger.info("Shutting down NASDAQ API Consumer...")
        consumer.stop()
        sys.exit(0)

if __name__ == "__main__":
    main()
