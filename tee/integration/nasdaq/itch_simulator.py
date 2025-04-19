#!/usr/bin/env python3
"""
NASDAQ ITCH Protocol Message Simulator
--------------------------------------
Generates realistic ITCH protocol messages for dual TEE cross-attestation testing.
This simulator creates messages in both binary format (matching the actual wire format) 
and JSON format (for easier consumption by test environments).

The simulation includes ITCH 5.0 message types with accurate field formats and values.
"""

import struct
import random
import json
import time
import os
import argparse
import binascii
import datetime
from typing import Dict, List, Any, Tuple, Optional
from dataclasses import dataclass

# ITCH Protocol Constants
ITCH_MESSAGE_TYPES = {
    'S': 'System Event Message',
    'R': 'Stock Directory Message',
    'H': 'Stock Trading Action Message',
    'Y': 'Reg SHO Short Sale Price Test Restricted Indicator Message',
    'L': 'Market Participant Position Message',
    'V': 'MWCB Decline Level Message',
    'W': 'MWCB Breach Message',
    'K': 'IPO Quoting Period Update Message',
    'A': 'Add Order Message (No MPID)',
    'F': 'Add Order with MPID Message',
    'E': 'Order Executed Message',
    'C': 'Order Executed with Price Message',
    'X': 'Order Cancel Message',
    'D': 'Order Delete Message',
    'U': 'Order Replace Message',
    'P': 'Trade Message',
    'Q': 'Cross Trade Message',
    'B': 'Broken Trade Message',
    'I': 'NOII Message',
    'N': 'RPII Message'
}

# TEE Attestation Information
class TeeAttestation:
    """Simulated TEE attestation information for cross-attestation verification"""
    
    def __init__(self, primary_tee_id: str, secondary_tee_id: str, region_id: str):
        self.primary_tee_id = primary_tee_id
        self.secondary_tee_id = secondary_tee_id
        self.region_id = region_id
        self.timestamp = int(time.time())
        
        # Generate simulated attestation quotes
        self.primary_quote = self._generate_attestation_quote(primary_tee_id, "SGX")
        self.secondary_quote = self._generate_attestation_quote(secondary_tee_id, "SEV")
        
    def _generate_attestation_quote(self, tee_id: str, tee_type: str) -> str:
        """Generate a simulated attestation quote (simplified for testing)"""
        quote_data = {
            "tee_id": tee_id,
            "tee_type": tee_type,
            "timestamp": self.timestamp,
            "region_id": self.region_id,
            "hash": binascii.hexlify(os.urandom(16)).decode()  # 16-byte random hash
        }
        return json.dumps(quote_data)

    def to_dict(self) -> Dict[str, Any]:
        """Return attestation data as a dictionary"""
        return {
            "primary_tee_id": self.primary_tee_id,
            "secondary_tee_id": self.secondary_tee_id,
            "region_id": self.region_id,
            "timestamp": self.timestamp,
            "sgx_quote": self.primary_quote,
            "sev_quote": self.secondary_quote
        }


class ITCHMessageGenerator:
    """Generates NASDAQ ITCH protocol messages with realistic values"""
    
    def __init__(self, 
                 symbols: List[str], 
                 primary_tee_id: str = "sgx-12345", 
                 secondary_tee_id: str = "sev-67890",
                 region_id: str = "us-east-1"):
        
        self.symbols = symbols
        self.attestation = TeeAttestation(primary_tee_id, secondary_tee_id, region_id)
        
        # Initialize tracking structures for orders
        self.next_order_ref = 1
        self.orders = {}  # Track active orders by reference number
        self.stock_info = {}  # Track stock trading status and prices
        
        # Initialize stock prices
        for symbol in symbols:
            base_price = 100.0  # Default price
            
            # Set different price ranges for different symbol patterns
            if symbol.startswith("A"):
                base_price = random.uniform(50.0, 150.0)
            elif symbol.startswith("M"):
                base_price = random.uniform(200.0, 350.0)
            elif symbol.startswith("G"):
                base_price = random.uniform(100.0, 250.0)
            elif symbol.startswith("US"):  # Treasury securities
                base_price = random.uniform(98.0, 102.0)
            
            self.stock_info[symbol] = {
                "price": round(base_price, 2),
                "status": "T",  # Trading
                "tick_size": 0.01,
                "last_sale": round(base_price, 2),
                "mpid_list": ["NSDQ", "MSCO", "GSCO", "UBSS", "JPMS", "ARCA"]
            }
    
    def _get_nanosecond_timestamp(self) -> int:
        """Get current timestamp in nanoseconds since midnight"""
        now = datetime.datetime.now()
        midnight = now.replace(hour=0, minute=0, second=0, microsecond=0)
        delta = now - midnight
        return int(delta.total_seconds() * 1_000_000_000)
    
    def _get_next_order_ref(self) -> int:
        """Get the next order reference number"""
        ref = self.next_order_ref
        self.next_order_ref += 1
        return ref
    
    def _random_stock(self) -> str:
        """Return a random stock symbol from our list"""
        return random.choice(self.symbols)
    
    def _random_price(self, symbol: str) -> int:
        """Generate a random price as an integer (price * 10000)"""
        base_price = self.stock_info[symbol]["price"]
        variation = random.uniform(-0.5, 0.5)
        price = base_price + variation
        return int(price * 10000)  # Convert to integer price (ITCH format)

    def _price_to_str(self, price_int: int) -> str:
        """Convert integer price (price * 10000) to string format"""
        return f"{price_int / 10000:.4f}"
    
    def generate_system_event(self) -> Tuple[bytes, Dict[str, Any]]:
        """Generate System Event Message (Type S)"""
        # Message type, timestamp, event code
        timestamp = self._get_nanosecond_timestamp()
        event_code = random.choice(['O', 'S', 'Q', 'M', 'E'])
        
        # Binary format
        binary = struct.pack('!cQc', 
                            b'S',                   # Message type
                            timestamp,              # Timestamp (nanoseconds)
                            event_code.encode())    # Event code
        
        # JSON format with TEE attestation
        json_msg = {
            "message_type": "S",
            "timestamp_nanoseconds": timestamp,
            "event_code": event_code,
            "event_name": {
                'O': 'Start of Messages',
                'S': 'Start of System Hours',
                'Q': 'Start of Market Hours',
                'M': 'End of Market Hours',
                'E': 'End of System Hours'
            }.get(event_code),
            **self.attestation.to_dict()  # Add attestation data
        }
        
        return binary, json_msg

    def generate_add_order(self) -> Tuple[bytes, Dict[str, Any]]:
        """Generate Add Order Message (Type A)"""
        # Select a random stock and generate order details
        symbol = self._random_stock()
        timestamp = self._get_nanosecond_timestamp()
        order_ref = self._get_next_order_ref()
        buy_sell = random.choice([b'B', b'S'])  # B for Buy, S for Sell
        shares = random.randint(100, 10000)
        price = self._random_price(symbol)
        
        # Store the order
        self.orders[order_ref] = {
            "symbol": symbol,
            "price": price,
            "shares": shares,
            "buy_sell": buy_sell.decode(),
            "timestamp": timestamp
        }
        
        # Pad the stock symbol to 8 bytes
        symbol_bytes = symbol.encode().ljust(8)
        
        # Binary format
        binary = struct.pack('!cQIcI8sI',
                            b'A',                   # Message type 
                            timestamp,              # Timestamp (nanoseconds)
                            order_ref,              # Order reference number
                            buy_sell,               # Buy/Sell indicator
                            shares,                 # Number of shares
                            symbol_bytes,           # Stock symbol (8 bytes)
                            price)                  # Price (integer format)
        
        # JSON format with TEE attestation
        json_msg = {
            "message_type": "A",
            "timestamp_nanoseconds": timestamp,
            "order_reference_number": order_ref,
            "buy_sell_indicator": buy_sell.decode(),
            "shares": shares,
            "stock": symbol,
            "price": self._price_to_str(price),
            **self.attestation.to_dict()  # Add attestation data
        }
        
        return binary, json_msg

    def generate_order_executed(self) -> Optional[Tuple[bytes, Dict[str, Any]]]:
        """Generate Order Executed Message (Type E)"""
        # Need at least one order to execute
        if not self.orders:
            return None
            
        # Select a random existing order
        order_ref = random.choice(list(self.orders.keys()))
        order = self.orders[order_ref]
        
        # Generate execution details
        timestamp = self._get_nanosecond_timestamp()
        # Fix: Ensure min value is less than max value for randint
        if order["shares"] >= 100:
            shares_executed = min(order["shares"], random.randint(100, order["shares"]))
        else:
            # If shares are less than 100, execute all remaining shares
            shares_executed = order["shares"]
        match_number = random.randint(1, 999999)
        
        # Update the order's remaining shares
        order["shares"] -= shares_executed
        if order["shares"] <= 0:
            del self.orders[order_ref]
        
        # Binary format
        binary = struct.pack('!cQIIQ',
                            b'E',                   # Message type
                            timestamp,              # Timestamp (nanoseconds)
                            order_ref,              # Order reference number
                            shares_executed,        # Executed shares
                            match_number)           # Match number
        
        # JSON format with TEE attestation
        json_msg = {
            "message_type": "E",
            "timestamp_nanoseconds": timestamp,
            "order_reference_number": order_ref,
            "executed_shares": shares_executed,
            "match_number": match_number,
            "stock": order["symbol"],  # Add stock for easier reference
            **self.attestation.to_dict()  # Add attestation data
        }
        
        return binary, json_msg

    def generate_order_cancel(self) -> Optional[Tuple[bytes, Dict[str, Any]]]:
        """Generate Order Cancel Message (Type X)"""
        # Need at least one order to cancel
        if not self.orders:
            return None
            
        # Select a random existing order
        order_ref = random.choice(list(self.orders.keys()))
        order = self.orders[order_ref]
        
        # Generate cancellation details
        timestamp = self._get_nanosecond_timestamp()
        # Fix: Ensure min value is less than max value for randint
        if order["shares"] >= 100:
            shares_cancelled = min(order["shares"], random.randint(100, order["shares"]))
        else:
            # If shares are less than 100, cancel all remaining shares
            shares_cancelled = order["shares"]
        
        # Update the order's remaining shares
        order["shares"] -= shares_cancelled
        if order["shares"] <= 0:
            del self.orders[order_ref]
        
        # Binary format
        binary = struct.pack('!cQII',
                            b'X',                   # Message type
                            timestamp,              # Timestamp (nanoseconds)
                            order_ref,              # Order reference number
                            shares_cancelled)       # Cancelled shares
        
        # JSON format with TEE attestation
        json_msg = {
            "message_type": "X",
            "timestamp_nanoseconds": timestamp,
            "order_reference_number": order_ref,
            "cancelled_shares": shares_cancelled,
            "stock": order["symbol"],  # Add stock for easier reference
            **self.attestation.to_dict()  # Add attestation data
        }
        
        return binary, json_msg

    def generate_trade(self) -> Tuple[bytes, Dict[str, Any]]:
        """Generate Trade Message (Type P)"""
        # Select a random stock
        symbol = self._random_stock()
        timestamp = self._get_nanosecond_timestamp()
        order_ref = self._get_next_order_ref()  # Non-displayable order reference
        buy_sell = random.choice([b'B', b'S'])  # B for Buy, S for Sell
        shares = random.randint(100, 10000)
        price = self._random_price(symbol)
        match_number = random.randint(1, 999999)
        
        # Update the last sale price
        self.stock_info[symbol]["last_sale"] = price / 10000
        
        # Pad the stock symbol to 8 bytes
        symbol_bytes = symbol.encode().ljust(8)
        
        # Binary format
        binary = struct.pack('!cQIcI8sIQ',
                            b'P',                   # Message type
                            timestamp,              # Timestamp (nanoseconds)
                            order_ref,              # Order reference number
                            buy_sell,               # Buy/Sell indicator
                            shares,                 # Number of shares
                            symbol_bytes,           # Stock symbol (8 bytes)
                            price,                  # Price (integer format)
                            match_number)           # Match number
        
        # JSON format with TEE attestation
        json_msg = {
            "message_type": "P",
            "timestamp_nanoseconds": timestamp,
            "order_reference_number": order_ref,
            "buy_sell_indicator": buy_sell.decode(),
            "shares": shares,
            "stock": symbol,
            "price": self._price_to_str(price),
            "match_number": match_number,
            **self.attestation.to_dict()  # Add attestation data
        }
        
        return binary, json_msg

    def generate_stock_directory(self) -> Tuple[bytes, Dict[str, Any]]:
        """Generate Stock Directory Message (Type R)"""
        symbol = self._random_stock()
        timestamp = self._get_nanosecond_timestamp()
        
        # Pad the stock symbol to 8 bytes
        symbol_bytes = symbol.encode().ljust(8)
        
        # Stock category
        if symbol.startswith("US"):
            category = b'T'  # Treasury security
        else:
            category = random.choice([b'N', b'G', b'T'])  # Nasdaq Global, Treasury
            
        # Other fields
        financial_status = random.choice([b'N', b'D', b'E', b'Q', b'S', b'G', b'H', b'J', b'K'])
        round_lots_only = random.choice([b'Y', b'N'])
        round_lot_size = 100
        issue_classification = b'C' if symbol.startswith("US") else random.choice([b'A', b'B', b'C', b'F', b'I', b'L', b'N', b'O', b'P', b'Q', b'R', b'S', b'T', b'U', b'V'])
        
        # Binary format (simplified - not all fields included for brevity)
        binary = struct.pack('!cQ8scccH',
                            b'R',                   # Message type
                            timestamp,              # Timestamp (nanoseconds)
                            symbol_bytes,           # Stock symbol (8 bytes)
                            category,               # Stock category
                            financial_status,       # Financial status indicator
                            round_lots_only,        # Round lots only
                            round_lot_size)         # Round lot size
        
        # JSON format with TEE attestation
        json_msg = {
            "message_type": "R",
            "timestamp_nanoseconds": timestamp,
            "stock": symbol,
            "stock_category": category.decode(),
            "financial_status": financial_status.decode(),
            "round_lots_only": round_lots_only.decode(),
            "round_lot_size": round_lot_size,
            "issue_classification": issue_classification.decode() if hasattr(issue_classification, 'decode') else issue_classification,
            **self.attestation.to_dict()  # Add attestation data
        }
        
        return binary, json_msg

    def generate_random_message(self) -> Tuple[bytes, Dict[str, Any]]:
        """Generate a random ITCH message type"""
        message_types = [
            (70, self.generate_add_order),      # 70% Add Order
            (15, self.generate_order_executed), # 15% Order Executed 
            (5, self.generate_order_cancel),    # 5% Order Cancel
            (5, self.generate_trade),           # 5% Trade
            (3, self.generate_stock_directory), # 3% Stock Directory
            (2, self.generate_system_event),    # 2% System Event
        ]
        
        # Select a message type based on distribution
        while True:
            rand = random.randint(1, 100)
            cumulative = 0
            
            for weight, generator in message_types:
                cumulative += weight
                if rand <= cumulative:
                    result = generator()
                    if result is not None:
                        return result
                    break  # Try again if None returned
    
    def generate_message_stream(self, count: int) -> List[Dict[str, Any]]:
        """Generate a stream of ITCH messages"""
        messages = []
        for _ in range(count):
            _, json_msg = self.generate_random_message()
            messages.append(json_msg)
        return messages


def write_messages_to_file(messages: List[Dict[str, Any]], filename: str):
    """Write messages to a JSON file"""
    with open(filename, 'w') as f:
        json.dump(messages, f, indent=2)
    print(f"Wrote {len(messages)} messages to {filename}")


def main():
    """Main function to run the ITCH message generator"""
    parser = argparse.ArgumentParser(description='NASDAQ ITCH Protocol Message Generator')
    parser.add_argument('--count', type=int, default=100, help='Number of messages to generate')
    parser.add_argument('--output', default='itch_messages.json', help='Output filename')
    parser.add_argument('--primary-tee', default='sgx-12345', help='Primary TEE ID (SGX)')
    parser.add_argument('--secondary-tee', default='sev-67890', help='Secondary TEE ID (SEV)')
    parser.add_argument('--region', default='us-east-1', help='Region ID')
    
    args = parser.parse_args()
    
    # Stock symbols to use in simulation
    symbols = [
        # Equities
        "AAPL", "MSFT", "GOOGL", "AMZN", "META", "NVDA", "TSLA", "JPM", "V", "JNJ",
        # Treasury securities
        "USTRSY-2Y", "USTRSY-5Y", "USTRSY-10Y", "USTRSY-30Y"
    ]
    
    # Create the generator
    generator = ITCHMessageGenerator(
        symbols=symbols,
        primary_tee_id=args.primary_tee,
        secondary_tee_id=args.secondary_tee,
        region_id=args.region
    )
    
    # Generate messages
    print(f"Generating {args.count} ITCH protocol messages...")
    messages = generator.generate_message_stream(args.count)
    
    # Write to file
    write_messages_to_file(messages, args.output)


if __name__ == "__main__":
    main()
