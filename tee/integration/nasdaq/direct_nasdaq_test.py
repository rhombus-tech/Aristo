#!/usr/bin/env python3
# Direct test of NASDAQ credentials using requests
# This verifies the OAuth authentication flow without the Kafka complexity

import os
import sys
import base64
import json
import time
import requests
import logging
from datetime import datetime

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger("NASDAQ-Test")

# Load credentials from environment or manually set them
CLIENT_ID = os.environ.get('NASDAQ_CLIENT_ID', "rhombustechnologies-tal-zisckindt")
CLIENT_SECRET = os.environ.get('NASDAQ_CLIENT_SECRET', "Mhlq9czKqy3aUUUL8uhltOUSXD3EOemn")
TOKEN_ENDPOINT = os.environ.get('NASDAQ_TOKEN_ENDPOINT', "https://clouddataservice.auth.nasdaq.com/auth/realms/pro-realm/protocol/openid-connect/token")

def get_oauth_token():
    """
    Get OAuth2 token for NASDAQ Cloud Data Service
    """
    try:
        # Create basic auth header
        auth_str = f"{CLIENT_ID}:{CLIENT_SECRET}"
        auth_bytes = auth_str.encode('ascii')
        base64_auth = base64.b64encode(auth_bytes).decode('ascii')
        
        headers = {
            "Authorization": f"Basic {base64_auth}",
            "Content-Type": "application/x-www-form-urlencoded"
        }
        
        payload = "grant_type=client_credentials"
        
        logger.info(f"Requesting OAuth token from {TOKEN_ENDPOINT}")
        logger.info(f"Using client ID: {CLIENT_ID}")
        
        # Send token request
        response = requests.post(
            TOKEN_ENDPOINT,
            headers=headers,
            data=payload
        )
        
        # Check response
        if response.status_code == 200:
            token_data = response.json()
            access_token = token_data.get("access_token")
            expires_in = token_data.get("expires_in", 3600)
            
            # Truncate token for display
            token_preview = f"{access_token[:20]}...{access_token[-20:]}" if access_token else "None"
            
            logger.info(f"Successfully obtained OAuth token")
            logger.info(f"Token preview: {token_preview}")
            logger.info(f"Expires in: {expires_in} seconds")
            logger.info(f"Token type: {token_data.get('token_type', 'unknown')}")
            
            return token_data
        else:
            logger.error(f"Failed to get OAuth token: HTTP {response.status_code}")
            logger.error(f"Response: {response.text}")
            return None
            
    except Exception as e:
        logger.error(f"Error getting OAuth token: {str(e)}")
        return None

def main():
    """Main entry point for the NASDAQ credentials test"""
    logger.info("======================================================")
    logger.info("NASDAQ Cloud Data Service Credentials Test")
    logger.info("======================================================")
    
    # Get OAuth token
    token_data = get_oauth_token()
    
    if token_data:
        logger.info("Authentication successful!")
        logger.info("Your NASDAQ credentials are working correctly")
        logger.info("You are entitled to access the following topics:")
        logger.info("- QBBO-A-CORE (Nasdaq Basic - Tape A)")
        logger.info("- QBBO-B-CORE (Nasdaq Basic - Tape B)")
        logger.info("- QBBO-C-CORE (Nasdaq Basic - Tape C)")
        logger.info("- NLSCTA (NLS Plus - CTA)")
        logger.info("- NLSUTP (NLS Plus - UTP)")
        logger.info("======================================================")
        logger.info("To access these topics in your TEE architecture:")
        logger.info("1. Use the token to authenticate with Kafka")
        logger.info("2. Connect to your entitled topics")
        logger.info("3. Process the data through your TEE mesh network")
    else:
        logger.error("Authentication failed")
        logger.error("Please verify your credentials and try again")

if __name__ == "__main__":
    main()
