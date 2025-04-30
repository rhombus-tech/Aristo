#!/usr/bin/env python3
# Fixed NASDAQ Basic test with specific consumer group

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

def test_basic_topic(client_id, client_secret, token_endpoint, bootstrap_server):
    """Test access to a single NASDAQ Basic topic with fixed configuration"""
    # Get OAuth token
    token = get_oauth_token(client_id, client_secret, token_endpoint)
    
    # Try different consumer group options
    # NASDAQ often requires consumer groups to have specific prefixes
    # Try these consumer group IDs in order until one works
    consumer_group_options = [
        f"rhombustechnologies-tal-zisckindt",  # NASDAQ-provided group ID (Apr 2025)
        f"rhombustechnologies-consumer",  # Based on your client ID
        f"rhombustechnologies-tal-zisckindt-consumer",  # Previous version
        f"rhombustechnologies-basic",  # Product-specific
        f"cds-consumer-rhombustechnologies",  # CDS-prefix pattern
        client_id  # Use client ID directly as consumer group
    ]
    
    # Create base Kafka consumer config
    base_config = {
        'bootstrap.servers': bootstrap_server,
        'security.protocol': 'SASL_SSL',
        'sasl.mechanisms': 'OAUTHBEARER',
        'oauth_cb': lambda x: (token, time.time() + 3600.0),
        'auto.offset.reset': 'earliest', 
        'enable.auto.commit': True,
        'ssl.ca.location': '/etc/ssl/cert.pem',
        'debug': 'broker,topic,cgrp'
    }
    
    # Try one consumer group after another
    for i, group_id in enumerate(consumer_group_options):
        logger.info(f"\n\n=== Trying consumer group #{i+1}: {group_id} ===")
        
        # Create config with this group ID
        config = base_config.copy()
        config['group.id'] = group_id
        
        logger.info("Creating Kafka consumer with configuration:")
        for key, value in config.items():
            if key != 'oauth_cb':  # Don't log the token callback
                logger.info(f"  {key}: {value}")
        
        # Create consumer
        consumer = Consumer(config)
        
        try:
            # Subscribe to just one Basic topic
            # When not using NASDAQ SDK, we need to append .stream to topic name
            topic = "QBBO-A-CORE.stream"
            logger.info(f"Subscribing to topic: {topic}")
            consumer.subscribe([topic])
            
            # Poll for messages with a short timeout
            logger.info(f"Polling for messages with consumer group '{group_id}'...")
            start_time = time.time()
            message_count = 0
            
            # Try for 10 seconds for each consumer group
            while time.time() - start_time < 10:
                msg = consumer.poll(1.0)
                
                if msg is None:
                    continue
                    
                if msg.error():
                    error_code = msg.error().code()
                    if error_code == KafkaError._PARTITION_EOF:
                        logger.info(f"Reached end of partition for {msg.topic()}/{msg.partition()}")
                    else:
                        logger.error(f"Consumer error: {msg.error()}")
                        # Break on real error
                        if error_code not in [KafkaError._PARTITION_EOF]:
                            break
                else:
                    # Success! We got a message
                    message_count += 1
                    logger.info(f"SUCCESS! Received message from {msg.topic()}/{msg.partition()}")
                    
                    try:
                        value = json.loads(msg.value().decode('utf-8'))
                        logger.info(f"Message content: {json.dumps(value, indent=2)[:200]}...")
                    except:
                        logger.info(f"Binary message, {len(msg.value())} bytes")
                    
                    # Break on success
                    break
            
            if message_count > 0:
                logger.info(f"SUCCESS with consumer group '{group_id}'")
                # Exit after finding a working consumer group
                return
            else:
                logger.info(f"No messages received with consumer group '{group_id}'")
                
        except Exception as e:
            logger.error(f"Error with consumer group '{group_id}': {str(e)}")
        finally:
            # Close the consumer
            consumer.close()
            logger.info(f"Consumer for group '{group_id}' closed")
    
    logger.error("Failed to access NASDAQ Basic data with any consumer group")

def main():
    """Main entry point"""
    parser = argparse.ArgumentParser(description="NASDAQ Basic Topic Test Consumer")
    parser.add_argument("--client-id", required=True, help="OAuth2 client ID")
    parser.add_argument("--client-secret", required=True, help="OAuth2 client secret")
    parser.add_argument("--token-endpoint", required=True, help="OAuth2 token endpoint URL")
    parser.add_argument("--bootstrap-server", required=True, help="Kafka bootstrap server")
    
    args = parser.parse_args()
    
    print("NASDAQ Basic Topic Test")
    print("======================")
    print(f"Client ID: {args.client_id}")
    print(f"Token Endpoint: {args.token_endpoint}")
    print(f"Bootstrap Server: {args.bootstrap_server}")
    print("======================")
    
    test_basic_topic(
        args.client_id,
        args.client_secret,
        args.token_endpoint,
        args.bootstrap_server
    )

if __name__ == "__main__":
    main()
