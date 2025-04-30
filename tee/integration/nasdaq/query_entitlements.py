#!/usr/bin/env python3
# Query NASDAQ entitlements using their API

import os
import sys
import json
import time
import base64
import requests
import logging
from datetime import datetime

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger("NASDAQ-Entitlements")

# NASDAQ API endpoints
TOKEN_ENDPOINT = "https://clouddataservice.auth.nasdaq.com/auth/realms/pro-realm/protocol/openid-connect/token"
ENTITLEMENTS_ENDPOINT = "https://data.api.nasdaq.com/api/v1/account/entitlements"  # This may need to be adjusted
# Some providers use an alternate endpoint:
CATALOG_ENDPOINT = "https://data.api.nasdaq.com/api/v1/catalog"  # This may need to be adjusted

def get_oauth_token(client_id, client_secret):
    """Get OAuth2 token for NASDAQ Cloud Data Service"""
    logger.info("Requesting OAuth token...")
    
    try:
        # Request parameters - try both approaches
        # Method 1: Basic Auth
        auth_str = f"{client_id}:{client_secret}"
        auth_bytes = auth_str.encode('ascii')
        base64_auth = base64.b64encode(auth_bytes).decode('ascii')
        
        headers1 = {
            "Authorization": f"Basic {base64_auth}",
            "Content-Type": "application/x-www-form-urlencoded"
        }
        
        # Method 2: Form data
        data = {
            'grant_type': 'client_credentials',
            'client_id': client_id,
            'client_secret': client_secret
        }
        
        # Try both methods (some providers prefer one over the other)
        # First try form data
        logger.info("Trying form data authentication...")
        response = requests.post(TOKEN_ENDPOINT, data=data)
        
        # If that fails, try Basic Auth
        if response.status_code != 200:
            logger.info("Form data failed, trying Basic Auth...")
            response = requests.post(TOKEN_ENDPOINT, headers=headers1, data="grant_type=client_credentials")
        
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

def query_entitlements(token):
    """Query NASDAQ for entitlements using the API"""
    try:
        headers = {
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json",
            "Accept": "application/json"
        }
        
        # Try both potential endpoints
        endpoints = [
            ENTITLEMENTS_ENDPOINT,
            CATALOG_ENDPOINT,
            # Try alternate paths
            "https://data.api.nasdaq.com/api/v1/entitlements",
            "https://clouddataservice.nasdaq.com/api/v1/entitlements",
            "https://data.api.nasdaq.com/v1/entitlements"
        ]
        
        for endpoint in endpoints:
            logger.info(f"Querying entitlements from {endpoint}...")
            
            try:
                response = requests.get(endpoint, headers=headers)
                
                # Check response
                if response.status_code == 200:
                    logger.info(f"Success! Retrieved entitlement data from {endpoint}")
                    return {
                        "endpoint": endpoint,
                        "data": response.json()
                    }
                else:
                    logger.warning(f"Failed to get entitlements from {endpoint}: HTTP {response.status_code}")
                    logger.warning(f"Response: {response.text[:200]}...")
            except Exception as e:
                logger.warning(f"Error querying {endpoint}: {str(e)}")
                
        # If we got here, all endpoints failed
        logger.error("Failed to get entitlements from any endpoint")
        return None
        
    except Exception as e:
        logger.error(f"Error in query_entitlements: {str(e)}")
        return None

def query_specific_topic(token, topic_name):
    """Query NASDAQ for information about a specific topic"""
    try:
        headers = {
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json",
            "Accept": "application/json"
        }
        
        # Try different endpoint formats for topic info
        endpoints = [
            f"https://data.api.nasdaq.com/api/v1/topics/{topic_name}",
            f"https://data.api.nasdaq.com/api/v1/catalog/topics/{topic_name}",
            f"https://clouddataservice.nasdaq.com/api/v1/topics/{topic_name}"
        ]
        
        for endpoint in endpoints:
            logger.info(f"Querying topic info from {endpoint}...")
            
            try:
                response = requests.get(endpoint, headers=headers)
                
                # Check response
                if response.status_code == 200:
                    logger.info(f"Success! Retrieved topic info from {endpoint}")
                    return {
                        "endpoint": endpoint,
                        "data": response.json()
                    }
                else:
                    logger.warning(f"Failed to get topic info from {endpoint}: HTTP {response.status_code}")
            except Exception as e:
                logger.warning(f"Error querying {endpoint}: {str(e)}")
                
        # If we got here, all endpoints failed
        logger.warning(f"Failed to get info for topic {topic_name} from any endpoint")
        return None
        
    except Exception as e:
        logger.error(f"Error in query_specific_topic: {str(e)}")
        return None

def main():
    """Main entry point"""
    # Get credentials from command line or environment
    import argparse
    parser = argparse.ArgumentParser(description="NASDAQ Entitlements Query")
    parser.add_argument("--client-id", default=os.environ.get("NASDAQ_CLIENT_ID"), help="OAuth2 client ID")
    parser.add_argument("--client-secret", default=os.environ.get("NASDAQ_CLIENT_SECRET"), help="OAuth2 client secret")
    
    args = parser.parse_args()
    
    if not args.client_id or not args.client_secret:
        logger.error("Client ID and Client Secret are required")
        sys.exit(1)
    
    # Get OAuth token
    token = get_oauth_token(args.client_id, args.client_secret)
    
    # Query entitlements
    entitlements = query_entitlements(token)
    
    if entitlements:
        print("\n=== Entitlements Data ===")
        print(f"Source: {entitlements['endpoint']}")
        try:
            print(json.dumps(entitlements['data'], indent=2))
        except:
            print(f"Raw data: {entitlements['data']}")
    else:
        print("\n=== Unable to retrieve entitlements automatically ===")
        
    # Try querying specific topics we believe we have access to
    known_topics = [
        "QBBO-A-CORE",
        "QBBO-B-CORE", 
        "QBBO-C-CORE",
        "NLSCTA",
        "NLSUTP"
    ]
    
    print("\n=== Attempting to query specific topic information ===")
    for topic in known_topics:
        topic_info = query_specific_topic(token, topic)
        if topic_info:
            print(f"\n--- Topic: {topic} ---")
            try:
                print(json.dumps(topic_info['data'], indent=2))
            except:
                print(f"Raw data: {topic_info['data']}")
        else:
            print(f"\n--- Topic: {topic} - Unable to retrieve information ---")
    
    print("\n=== Query Complete ===")

if __name__ == "__main__":
    main()
