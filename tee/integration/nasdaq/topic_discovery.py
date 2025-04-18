#!/usr/bin/env python3
# NASDAQ Cloud Data Service Topic Discovery Tool
# This tool helps identify which topics you have access to in your NASDAQ account

import os
import sys
import json
import time
import base64
import logging
import argparse
import requests
from confluent_kafka.admin import AdminClient
from confluent_kafka import Consumer, KafkaException, KafkaError

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger("NASDAQ-Topic-Discovery")


class NasdaqTopicDiscovery:
    """
    NASDAQ Cloud Data Service Topic Discovery Tool
    
    This class helps identify what topics you have access to in your NASDAQ CDS account
    and shows the proper naming patterns required for subscription.
    """
    
    def __init__(self, client_id, client_secret, token_endpoint, bootstrap_server):
        """
        Initialize the NASDAQ Topic Discovery Tool
        
        Args:
            client_id (str): OAuth2 client ID
            client_secret (str): OAuth2 client secret
            token_endpoint (str): OAuth2 token endpoint URL
            bootstrap_server (str): Kafka bootstrap server address
        """
        self.client_id = client_id
        self.client_secret = client_secret
        self.token_endpoint = token_endpoint
        self.bootstrap_server = bootstrap_server
        
        self.access_token = None
        self.token_expiry = 0
        
        # Known NASDAQ topic patterns to search for
        self.topic_patterns = [
            "QBBO", "NLS", "NLSCTA", "NLSUTP", "CORE", "TAPE", 
            "BASIC", "rhombustechnologies", "tal-zisckindt"
        ]
        
        logger.info("NASDAQ Topic Discovery initialized")
    
    def get_oauth_token(self):
        """
        Get OAuth2 token for NASDAQ Cloud Data Service
        
        Returns:
            str: The OAuth2 access token
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
    
    def create_kafka_config(self):
        """
        Create Kafka configuration for AdminClient
        
        Returns:
            dict: Kafka configuration dictionary
        """
        token = self.get_oauth_token()
        
        # Return tuple of (token, expiry_time) from the callback
        def oauth_callback(config_str):
            return token, time.time() + 3600.0
        
        config = {
            'bootstrap.servers': self.bootstrap_server,
            'security.protocol': 'SASL_SSL',
            'sasl.mechanisms': 'OAUTHBEARER',
            'oauth_cb': oauth_callback,
            'request.timeout.ms': 30000,
            'api.version.request': True,
            'debug': 'broker,admin',
        }
        
        return config
    
    def discover_topics(self):
        """
        Discover accessible topics in the NASDAQ Cloud Data Service
        
        This method:
        1. Attempts to list all topics directly (if allowed)
        2. Tries to access specific topic patterns we know about
        3. Tests metadata access for each potential topic
        
        Returns:
            dict: Information about discovered topics
        """
        config = self.create_kafka_config()
        
        logger.info("Creating Admin client for topic discovery...")
        admin_client = AdminClient(config)
        
        # Try to get cluster metadata which might list all topics
        logger.info("Attempting to fetch cluster metadata...")
        try:
            cluster_metadata = admin_client.list_topics(timeout=10.0)
            
            if cluster_metadata and cluster_metadata.topics:
                logger.info(f"Successfully retrieved {len(cluster_metadata.topics)} topics")
                
                # Log all discovered topics
                for topic_name, topic_metadata in cluster_metadata.topics.items():
                    partitions = len(topic_metadata.partitions)
                    logger.info(f"Found topic: {topic_name} (partitions: {partitions})")
                
                return {
                    "success": True,
                    "discovery_method": "direct_list",
                    "topics": list(cluster_metadata.topics.keys())
                }
        except KafkaException as e:
            logger.warning(f"Could not list all topics directly: {str(e)}")
        
        # If we couldn't list all topics, try pattern-based discovery
        logger.info("Attempting pattern-based topic discovery...")
        
        discovered_topics = []
        known_base_topics = [
            "QBBO-A-CORE", "QBBO-B-CORE", "QBBO-C-CORE",
            "NLSCTA", "NLSUTP"
        ]
        
        # Check for prefixed variations of known topics
        prefixes = [
            "", "rhombustechnologies.", "rhombustechnologies-tal-zisckindt.", 
            f"{self.client_id}.", f"{self.client_id}-"
        ]
        
        # Try different topic naming patterns
        test_topics = []
        for prefix in prefixes:
            for topic in known_base_topics:
                test_topics.append(f"{prefix}{topic}")
        
        # Create a consumer to test topic access
        consumer_config = config.copy()
        consumer_config.update({
            'group.id': f'topic-discovery-{int(time.time())}',
            'auto.offset.reset': 'earliest',
        })
        
        consumer = Consumer(consumer_config)
        
        # Test each potential topic
        for topic in test_topics:
            try:
                logger.info(f"Testing access to topic: {topic}")
                metadata = consumer.list_topics(topic, timeout=5.0)
                
                if metadata and topic in metadata.topics:
                    topic_metadata = metadata.topics[topic]
                    partitions = len(topic_metadata.partitions)
                    
                    if partitions > 0:
                        logger.info(f"✅ Accessible topic: {topic} (partitions: {partitions})")
                        discovered_topics.append({
                            "name": topic,
                            "partitions": partitions,
                            "metadata": str(topic_metadata)
                        })
                    else:
                        logger.info(f"❌ Topic exists but has 0 partitions: {topic}")
            except Exception as e:
                logger.info(f"❌ Could not access topic {topic}: {str(e)}")
        
        # Clean up consumer
        consumer.close()
        
        return {
            "success": len(discovered_topics) > 0,
            "discovery_method": "pattern_based",
            "topics": discovered_topics
        }


def main():
    """Main entry point for the NASDAQ Topic Discovery Tool"""
    parser = argparse.ArgumentParser(description='NASDAQ Cloud Data Service Topic Discovery Tool')
    
    parser.add_argument('--client-id', required=True, help='OAuth2 Client ID')
    parser.add_argument('--client-secret', required=True, help='OAuth2 Client Secret')
    parser.add_argument('--token-endpoint', required=True, help='OAuth2 Token Endpoint URL')
    parser.add_argument('--bootstrap-server', required=True, help='Kafka Bootstrap Server')
    parser.add_argument('--output', help='Path to save results JSON file')
    parser.add_argument('--log-level', default='INFO', 
                       choices=['DEBUG', 'INFO', 'WARNING', 'ERROR', 'CRITICAL'],
                       help='Logging level')
    
    args = parser.parse_args()
    
    # Set logging level
    logging.getLogger().setLevel(getattr(logging, args.log_level))
    
    try:
        discovery = NasdaqTopicDiscovery(
            client_id=args.client_id,
            client_secret=args.client_secret,
            token_endpoint=args.token_endpoint,
            bootstrap_server=args.bootstrap_server
        )
        
        logger.info("Starting NASDAQ topic discovery...")
        results = discovery.discover_topics()
        
        # Print summary of results
        if results["success"]:
            logger.info(f"Topic discovery successful! Found {len(results['topics'])} topics")
            
            # Format and print the discovered topics
            if results["discovery_method"] == "direct_list":
                # Just print the topic names
                for topic in results["topics"]:
                    print(f"Topic: {topic}")
            else:
                # Print more detailed info
                for topic in results["topics"]:
                    print(f"Topic: {topic['name']} (Partitions: {topic['partitions']})")
        else:
            logger.warning("Could not discover any accessible topics")
        
        # Save results to file if requested
        if args.output:
            with open(args.output, 'w') as f:
                json.dump(results, f, indent=2)
            logger.info(f"Results saved to {args.output}")
        
    except KeyboardInterrupt:
        logger.info("Topic discovery interrupted")
        sys.exit(0)
    except Exception as e:
        logger.error(f"Error during topic discovery: {str(e)}")
        sys.exit(1)

if __name__ == "__main__":
    main()
