#!/usr/bin/env python3
# Script to list available Kafka topics and examine metadata

import os
import sys
import json
import time
import base64
import logging
import argparse
import requests
from confluent_kafka import Consumer, KafkaError, KafkaException

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)

logger = logging.getLogger('NASDAQ-Topic-Inspector')

def get_oauth_token(client_id, client_secret, token_endpoint):
    """Get OAuth2 token for NASDAQ Cloud Data Service"""
    logger.info("Requesting OAuth token...")
    
    try:
        # Request parameters
        data = {
            'grant_type': 'client_credentials',
            'client_id': client_id,
            'client_secret': client_secret
        }
        
        # Make the request
        response = requests.post(token_endpoint, data=data)
        response.raise_for_status()  # Raise exception for non-200 responses
        
        # Parse the response
        token_data = response.json()
        token = token_data.get('access_token')
        expires_in = token_data.get('expires_in', 0)
        
        if not token:
            logger.error("No access token in response")
            raise Exception("Failed to obtain OAuth token: No access_token in response")
        
        logger.info(f"Successfully obtained OAuth token: {token[:10]}...{token[-10:]}")
        logger.info(f"Token expires in {expires_in} seconds")
        
        return token
        
    except Exception as e:
        logger.error(f"Failed to get OAuth token: {str(e)}")
        raise

def inspect_kafka_topics(client_id, client_secret, token_endpoint, bootstrap_server):
    """Inspect available Kafka topics and metadata"""
    logger.info(f"Inspecting Kafka topics at {bootstrap_server}")
    
    # Get OAuth token
    token = get_oauth_token(client_id, client_secret, token_endpoint)
    
    # Create Kafka consumer config
    config = {
        'bootstrap.servers': bootstrap_server,
        'security.protocol': 'SASL_SSL',
        'sasl.mechanisms': 'OAUTHBEARER',
        'oauth_cb': lambda x: (token, time.time() + 3600.0),
        'group.id': f'rhombustechnologies-tal-zisckindt',
        'auto.offset.reset': 'earliest', 
        'enable.auto.commit': True,
        'ssl.ca.location': '/etc/ssl/cert.pem',
    }
    
    logger.info("Creating Kafka consumer with configuration:")
    for key, value in config.items():
        if key != 'oauth_cb':  # Don't log the token callback
            logger.info(f"  {key}: {value}")
    
    # Create consumer
    consumer = Consumer(config)
    
    # Add debug level for more verbose logging
    config['debug'] = 'topic,broker,metadata'
    
    try:
        # Method 1: Get metadata for all topics
        logger.info("Attempting to get cluster metadata...")
        cluster_metadata = consumer.list_topics(timeout=10)
        
        if cluster_metadata and hasattr(cluster_metadata, 'topics'):
            topics = list(cluster_metadata.topics.keys())
            logger.info(f"Found {len(topics)} topics in metadata:")
            for topic in topics:
                logger.info(f"  - {topic}")
                
            # Get detailed topic metadata
            for topic_name, topic_metadata in cluster_metadata.topics.items():
                logger.info(f"Topic: {topic_name}")
                logger.info(f"  Error: {topic_metadata.error}")
                logger.info(f"  Partitions: {len(topic_metadata.partitions)}")
                
                for partition_id, partition in topic_metadata.partitions.items():
                    logger.info(f"    Partition {partition_id}:")
                    logger.info(f"      Leader: {partition.leader}")
                    logger.info(f"      Replicas: {partition.replicas}")
                    logger.info(f"      ISRs: {partition.isrs}")
        else:
            logger.warning("No topics found in metadata or metadata structure is unexpected")
            
        # Method 2: Try subscribing to specific topics and check errors
        entitled_topics = [
            "QBBO-A-CORE",
            "QBBO-B-CORE",
            "QBBO-C-CORE",
            "NLSCTA",
            "NLSUTP"
        ]
        
        logger.info("\nTesting individual topic subscriptions:")
        
        for topic in entitled_topics:
            try:
                logger.info(f"Attempting to subscribe to {topic}...")
                consumer.unsubscribe()  # Clear previous subscriptions
                consumer.subscribe([topic])
                
                # Poll briefly to trigger metadata fetch
                msg = consumer.poll(5.0)
                
                if msg is None:
                    logger.info(f"  {topic}: No errors in subscription, but no messages received")
                elif msg.error():
                    logger.error(f"  {topic}: Subscription error: {msg.error()}")
                else:
                    logger.info(f"  {topic}: Successfully received a message!")
                    
            except KafkaException as e:
                logger.error(f"  {topic}: KafkaException: {str(e)}")
                
        # Try additional topic name patterns
        logger.info("\nTesting alternative topic name patterns:")
        alternative_patterns = [
            # Try with client ID prefix
            f"rhombustechnologies-tal-zisckindt.QBBO-A-CORE",
            # Try lowercase
            "qbbo-a-core",
            # Try without hyphens
            "QBBOCORE"
        ]
        
        for topic in alternative_patterns:
            try:
                logger.info(f"Attempting to subscribe to {topic}...")
                consumer.unsubscribe()
                consumer.subscribe([topic])
                msg = consumer.poll(5.0)
                
                if msg is None:
                    logger.info(f"  {topic}: No errors in subscription, but no messages received")
                elif msg.error():
                    logger.error(f"  {topic}: Subscription error: {msg.error()}")
                else:
                    logger.info(f"  {topic}: Successfully received a message!")
                    
            except KafkaException as e:
                logger.error(f"  {topic}: KafkaException: {str(e)}")
                
        # Try listing all topics with wildcard pattern
        logger.info("\nAttempting to list all topics with wildcard pattern...")
        try:
            consumer.unsubscribe()
            # Using '#' as a wildcard pattern (if supported by the broker)
            consumer.subscribe(['#'])
            msg = consumer.poll(5.0)
            if msg is None:
                logger.info("  No errors with wildcard pattern, but no messages received")
            elif msg.error():
                logger.error(f"  Wildcard pattern subscription error: {msg.error()}")
            else:
                logger.info(f"  Successfully received a message with wildcard pattern!")
        except KafkaException as e:
            logger.error(f"  Wildcard pattern error: {str(e)}")
            
    except KafkaException as e:
        logger.error(f"Kafka error: {str(e)}")
    except Exception as e:
        logger.error(f"Unexpected error: {str(e)}")
    finally:
        # Close the consumer
        consumer.close()
        logger.info("Consumer closed")

def main():
    """Main entry point"""
    parser = argparse.ArgumentParser(description="NASDAQ Kafka Topic Inspector")
    parser.add_argument("--client-id", required=True, help="OAuth2 client ID")
    parser.add_argument("--client-secret", required=True, help="OAuth2 client secret")
    parser.add_argument("--token-endpoint", required=True, help="OAuth2 token endpoint URL")
    parser.add_argument("--bootstrap-server", required=True, help="Kafka bootstrap server")
    
    args = parser.parse_args()
    
    print("NASDAQ Kafka Topic Inspector")
    print("======================")
    print(f"Client ID: {args.client_id}")
    print(f"Token Endpoint: {args.token_endpoint}")
    print(f"Bootstrap Server: {args.bootstrap_server}")
    print("======================")
    
    inspect_kafka_topics(
        args.client_id,
        args.client_secret,
        args.token_endpoint,
        args.bootstrap_server
    )

if __name__ == "__main__":
    main()
