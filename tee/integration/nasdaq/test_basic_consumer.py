#!/usr/bin/env python3
# Simple test script for NASDAQ Basic topic access

import os
import sys
import json
import time
import base64
import logging
import argparse
import requests
from confluent_kafka import Consumer, KafkaError

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger("NASDAQ-Basic-Test")

def get_oauth_token(client_id, client_secret, token_endpoint):
    """Get OAuth2 token for NASDAQ Cloud Data Service"""
    try:
        auth_str = f"{client_id}:{client_secret}"
        auth_bytes = auth_str.encode('ascii')
        base64_auth = base64.b64encode(auth_bytes).decode('ascii')
        
        headers = {
            "Authorization": f"Basic {base64_auth}",
            "Content-Type": "application/x-www-form-urlencoded"
        }
        
        payload = "grant_type=client_credentials"
        
        logger.info("Requesting OAuth token...")
        response = requests.post(
            token_endpoint,
            headers=headers,
            data=payload
        )
        
        response.raise_for_status()
        token_data = response.json()
        
        access_token = token_data.get("access_token")
        expires_in = token_data.get("expires_in", 3600)
        
        token_preview = f"{access_token[:20]}...{access_token[-20:]}" if access_token else "None"
        logger.info(f"Successfully obtained OAuth token: {token_preview}")
        logger.info(f"Token expires in {expires_in} seconds")
        
        return access_token
    except Exception as e:
        logger.error(f"Failed to get OAuth token: {str(e)}")
        raise

def test_nasdaq_topics(client_id, client_secret, token_endpoint, bootstrap_server, test_all_topics=False, test_duration=30):
    """Test access to NASDAQ topics"""
    logger = logging.getLogger('NASDAQ-Topic-Test')
    
    # Get OAuth token
    token = get_oauth_token(client_id, client_secret, token_endpoint)
    
    # Create Kafka consumer config
    config = {
        'bootstrap.servers': bootstrap_server,
        'security.protocol': 'SASL_SSL',
        'sasl.mechanisms': 'OAUTHBEARER',
        'oauth_cb': lambda x: (token, time.time() + 3600.0),
        'group.id': 'rhombustechnologies-tal-zisckindt',  # NASDAQ-provided group ID
        'auto.offset.reset': 'earliest', 
        'enable.auto.commit': True,
        'ssl.ca.location': '/etc/ssl/cert.pem',
        'debug': 'all'
    }
    
    logger.info("Creating Kafka consumer with configuration:")
    for key, value in config.items():
        if key != 'oauth_cb':  # Don't log the token callback
            logger.info(f"  {key}: {value}")
    
    # Create consumer
    consumer = Consumer(config)
    
    try:
        # Subscribe to topics based on the test mode
        if test_all_topics:
            # When not using NASDAQ SDK, we need to append .stream to topic names
            topics = [
                "QBBO-A-CORE.stream",  # Nasdaq Basic - Tape A
                "QBBO-B-CORE.stream",  # Nasdaq Basic - Tape B
                "QBBO-C-CORE.stream",  # Nasdaq Basic - Tape C
                "NLSCTA.stream",       # NLS Plus - CTA
                "NLSUTP.stream"        # NLS Plus - UTP
            ]
            logger.info(f"Subscribing to all topics: {', '.join(topics)}")
        else:
            # When not using NASDAQ SDK, we need to append .stream to topic name
            topics = ["QBBO-A-CORE.stream"]
            logger.info(f"Subscribing to single topic: {topics[0]}")
        
        consumer.subscribe(topics)
        
        # Poll for messages with a short timeout
        logger.info(f"Starting to poll for messages (will timeout after {test_duration} seconds)...")
        start_time = time.time()
        message_count = 0
        topic_messages = {topic: 0 for topic in topics}
        
        while time.time() - start_time < test_duration:
            msg = consumer.poll(1.0)
            
            if msg is None:
                sys.stdout.write('.')
                sys.stdout.flush()
                continue
                
            if msg.error():
                if msg.error().code() == KafkaError._PARTITION_EOF:
                    logger.info(f"Reached end of partition for {msg.topic()}/{msg.partition()}")
                else:
                    logger.error(f"Consumer error: {msg.error()}")
            else:
                message_count += 1
                topic = msg.topic()
                topic_messages[topic] = topic_messages.get(topic, 0) + 1
                
                if topic_messages[topic] == 1:
                    # Log the first message for each topic in detail
                    try:
                        value = json.loads(msg.value().decode('utf-8'))
                        logger.info(f"\nReceived first message from {topic}:")
                        logger.info(f"Message: {json.dumps(value, indent=2)[:500]}...")
                    except Exception as e:
                        logger.info(f"\nReceived binary message from {topic}, {len(msg.value())} bytes")
                        logger.debug(f"Error parsing message: {str(e)}")
                
                if message_count % 10 == 0:
                    logger.info(f"\nTotal messages: {message_count} ({', '.join([f'{t}: {c}' for t, c in topic_messages.items()])})")  
        
        logger.info(f"\nTest complete. Received {message_count} total messages in {test_duration} seconds.")
        for topic, count in topic_messages.items():
            logger.info(f"  {topic}: {count} messages")
            
    except KeyboardInterrupt:
        logger.info("Interrupted by user")
    finally:
        # Close the consumer
        consumer.close()
        logger.info("Consumer closed")

def main():
    """Main entry point"""
    parser = argparse.ArgumentParser(description="NASDAQ Topic Test Consumer")
    parser.add_argument("--client-id", required=True, help="OAuth2 client ID")
    parser.add_argument("--client-secret", required=True, help="OAuth2 client secret")
    parser.add_argument("--token-endpoint", required=True, help="OAuth2 token endpoint URL")
    parser.add_argument("--bootstrap-server", required=True, help="Kafka bootstrap server")
    parser.add_argument("--all-topics", action="store_true", help="Test all entitled topics instead of just one")
    parser.add_argument("--duration", type=int, default=30, help="Test duration in seconds (default: 30)")
    
    args = parser.parse_args()
    
    print("NASDAQ Topic Test")
    print("======================")
    print(f"Client ID: {args.client_id}")
    print(f"Token Endpoint: {args.token_endpoint}")
    print(f"Bootstrap Server: {args.bootstrap_server}")
    print(f"Testing {'ALL topics' if args.all_topics else 'single topic'}")
    print(f"Test duration: {args.duration} seconds")
    print("======================")
    
    test_nasdaq_topics(
        args.client_id,
        args.client_secret,
        args.token_endpoint,
        args.bootstrap_server,
        test_all_topics=args.all_topics,
        test_duration=args.duration
    )

if __name__ == "__main__":
    main()
