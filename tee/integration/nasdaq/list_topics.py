#!/usr/bin/env python3
# Script to list available NASDAQ Cloud Data Service topics with our credentials

import os
import sys
import time
import base64
import json
import requests
import logging
from confluent_kafka import Consumer, KafkaException
from confluent_kafka.admin import AdminClient

# Configure logging
logging.basicConfig(level=logging.INFO, 
                    format='%(asctime)s - NASDAQ-Topic-Lister - %(levelname)s - %(message)s')
logger = logging.getLogger(__name__)

def load_env_file(env_file):
    """Load environment variables from file"""
    env_vars = {}
    try:
        with open(env_file, 'r') as f:
            for line in f:
                line = line.strip()
                if not line or line.startswith('#'):
                    continue
                key, value = line.split('=', 1)
                env_vars[key] = value
        return env_vars
    except Exception as e:
        logger.error(f"Error loading environment file: {e}")
        sys.exit(1)

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
        
        data = "grant_type=client_credentials"
        
        response = requests.post(token_endpoint, headers=headers, data=data)
        response.raise_for_status()
        
        token_data = response.json()
        access_token = token_data.get("access_token")
        expires_in = token_data.get("expires_in", 3600)
        
        if not access_token:
            logger.error("No access token received from NASDAQ")
            sys.exit(1)
            
        logger.info(f"Successfully obtained OAuth token (expires in {expires_in} seconds)")
        return access_token
    except Exception as e:
        logger.error(f"Error getting OAuth token: {e}")
        sys.exit(1)

def create_kafka_config(bootstrap_server, access_token):
    """Create Kafka configuration with OAUTH authentication"""
    return {
        'bootstrap.servers': bootstrap_server,
        'security.protocol': 'SASL_SSL',
        'sasl.mechanisms': 'OAUTHBEARER',
        'oauth_cb': lambda x: (access_token, time.time() + 3600.0),  # 1-hour expiry from now
        'group.id': f'nasdaq-topic-lister-{int(time.time())}',
        'auto.offset.reset': 'earliest',
        'session.timeout.ms': 45000,
        'request.timeout.ms': 60000,  # Longer timeout for market data
        'enable.auto.commit': True,
        'enable.partition.eof': True,  # Enable end-of-partition notification
        'ssl.ca.location': '/etc/ssl/cert.pem',  # Standard macOS CA certificate location
        'api.version.request': True  # Negotiate API version
    }

def list_available_topics(bootstrap_server, access_token):
    """List all available topics we have access to"""
    config = create_kafka_config(bootstrap_server, access_token)
    
    try:
        # Create AdminClient to list topics
        admin_client = AdminClient(config)
        
        # Get metadata about topics
        cluster_metadata = admin_client.list_topics(timeout=10)
        
        if not cluster_metadata.topics:
            logger.warning("No topics found or accessible with current credentials")
            return []
            
        topics = list(cluster_metadata.topics.keys())
        logger.info(f"Found {len(topics)} accessible topics")
        return topics
    except KafkaException as e:
        logger.error(f"Kafka error: {e}")
        return []
    except Exception as e:
        logger.error(f"Error listing topics: {e}")
        return []

def main():
    """Main function to list NASDAQ topics"""
    # Load environment variables
    env_file = "nasdaq_credentials.env"
    env_vars = load_env_file(env_file)
    
    # Get credentials
    client_id = env_vars.get("NASDAQ_CLIENT_ID")
    client_secret = env_vars.get("NASDAQ_CLIENT_SECRET")
    token_endpoint = env_vars.get("NASDAQ_TOKEN_ENDPOINT")
    bootstrap_server = env_vars.get("NASDAQ_BOOTSTRAP_SERVER")
    
    if not all([client_id, client_secret, token_endpoint, bootstrap_server]):
        logger.error("Missing required credentials in environment file")
        sys.exit(1)
    
    # Get OAuth token
    logger.info("Getting OAuth token from NASDAQ...")
    access_token = get_oauth_token(client_id, client_secret, token_endpoint)
    
    # List topics
    logger.info(f"Connecting to NASDAQ Kafka broker: {bootstrap_server}")
    topics = list_available_topics(bootstrap_server, access_token)
    
    if topics:
        logger.info("Available NASDAQ topics:")
        for i, topic in enumerate(sorted(topics), 1):
            logger.info(f"{i}. {topic}")
    else:
        logger.warning("No accessible NASDAQ topics found with current credentials")
    
    # Try a more comprehensive approach to discover available topics
    logger.info("\nTrying broader discovery of NASDAQ topics...")
    
    # Much larger set of potential NASDAQ topic patterns to try
    nasdaq_topic_patterns = [
        # Try common NASDAQ data feeds
        "TOTALVIEW*", "ITCH*", "GLIMPSE*", "NPSI*", "NLS*", "QBBO*", "NOII*",
        "BXRP*", "PSX*", "UTDF*", "UQDF*", "OTC*", "OTCBB*", "DBEQ*",
        "BASIC*", "BX*", "LEVEL1*", "LEVEL2*", "LAST*", "TRADES*",
        # Try common data feeds without prefixes
        "TRADES", "QUOTES", "NBBO", "BOOK", "L1", "L2", "BASIC", "CORE",
        # Try delays and regions
        "*-DELAYED", "*-REALTIME", "*-TEST", "*-DEMO", "*-A*", "*-B*", "*-C*",
        # Try less specific patterns
        "*QUOTE*", "*TRADE*", "*DATA*", "*MARKET*", "*FEED*"
    ]
    
    # Try common feed patterns first
    config = create_kafka_config(bootstrap_server, access_token)
    consumer = Consumer(config)
    
    logger.info("Attempting to discover topics using common NASDAQ feed patterns...")
    for pattern in nasdaq_topic_patterns:
        try:
            logger.info(f"Searching for topics matching pattern: {pattern}")
            metadata = consumer.list_topics(pattern, timeout=5)
            
            if metadata and metadata.topics:
                found_topics = [t for t in metadata.topics.keys() if t != "__consumer_offsets"]
                if found_topics:
                    logger.info(f"✅ Found {len(found_topics)} topics matching '{pattern}':")
                    for topic in found_topics:
                        topic_meta = metadata.topics[topic]
                        partitions = len(topic_meta.partitions)
                        logger.info(f"   - {topic} ({partitions} partitions)")
        except Exception as e:
            logger.info(f"❌ Error checking pattern {pattern}: {str(e)[:100]}...")
    
    # Try to list all topics
    logger.info("\nAttempting to list all available topics...")
    try:
        all_metadata = consumer.list_topics(timeout=10)
        if all_metadata and all_metadata.topics:
            all_topics = [t for t in all_metadata.topics.keys() if t != "__consumer_offsets"]
            if all_topics:
                logger.info(f"✅ Found {len(all_topics)} accessible topics:")
                for topic in sorted(all_topics):
                    topic_meta = all_metadata.topics[topic]
                    partitions = len(topic_meta.partitions)
                    logger.info(f"   - {topic} ({partitions} partitions)")
            else:
                logger.info("No accessible topics found in the general list")
    except Exception as e:
        logger.info(f"❌ Error listing all topics: {str(e)[:100]}...")
    
    consumer.close()

if __name__ == "__main__":
    main()
