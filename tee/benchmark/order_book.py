#!/usr/bin/env python3
# Order Book implementation for enhanced NASDAQ simulation
# Used by the enhanced_nasdaq_simulator.py

import heapq
import time
import uuid
import random
from enum import Enum
from dataclasses import dataclass
from typing import Dict, List, Optional, Tuple, Set

class Side(Enum):
    BUY = "BUY"
    SELL = "SELL"

class MessageType(Enum):
    ADD_ORDER = "A"
    MODIFY_ORDER = "M"
    DELETE_ORDER = "D"
    TRADE = "T"
    SYSTEM = "S"

@dataclass
class Order:
    order_id: str
    symbol: str
    side: Side
    price: float
    quantity: int
    timestamp: int
    venue: str

class OrderBook:
    """Simulated order book for a single symbol"""
    
    def __init__(self, symbol: str):
        self.symbol = symbol
        self.buy_orders = []  # max heap (-price, timestamp, order_id)
        self.sell_orders = []  # min heap (price, timestamp, order_id)
        self.orders = {}  # order_id -> Order
        self.last_price = None
        self.sequence_number = 0
    
    def add_order(self, side: Side, price: float, quantity: int, venue: str = "NASDAQ") -> Tuple[Order, List]:
        """Add an order to the book and return any resulting messages"""
        
        # Create new order
        order_id = str(uuid.uuid4())
        timestamp = int(time.time() * 1_000_000)  # microseconds
        order = Order(order_id, self.symbol, side, price, quantity, timestamp, venue)
        self.orders[order_id] = order
        
        messages = []
        
        # Add to appropriate heap
        if side == Side.BUY:
            # Negate price for max heap behavior
            heapq.heappush(self.buy_orders, (-price, timestamp, order_id))
            # Check for matches against sell orders
            messages.extend(self._match_orders())
        else:
            heapq.heappush(self.sell_orders, (price, timestamp, order_id))
            # Check for matches against buy orders
            messages.extend(self._match_orders())
        
        # Always include the add order message
        messages.insert(0, self._create_message(MessageType.ADD_ORDER, order))
        
        return order, messages
    
    def cancel_order(self, order_id: str) -> Optional[dict]:
        """Cancel an order in the book"""
        if order_id not in self.orders:
            return None
        
        order = self.orders[order_id]
        del self.orders[order_id]
        
        # Note: We don't remove from the heap directly as it's expensive
        # Instead, we'll filter it out during matching
        
        return self._create_message(MessageType.DELETE_ORDER, order)
    
    def modify_order(self, order_id: str, new_quantity: int) -> Optional[dict]:
        """Modify an order's quantity"""
        if order_id not in self.orders:
            return None
        
        order = self.orders[order_id]
        old_quantity = order.quantity
        order.quantity = new_quantity
        
        return self._create_message(MessageType.MODIFY_ORDER, order)
    
    def _match_orders(self) -> List[dict]:
        """Match orders and return resulting trade messages"""
        messages = []
        
        # Clean up any canceled orders
        self.buy_orders = [(p, t, oid) for p, t, oid in self.buy_orders if oid in self.orders]
        self.sell_orders = [(p, t, oid) for p, t, oid in self.sell_orders if oid in self.orders]
        
        # Reheapify
        heapq.heapify(self.buy_orders)
        heapq.heapify(self.sell_orders)
        
        # Match orders
        while self.buy_orders and self.sell_orders:
            # Get the best buy and sell prices
            best_buy = -self.buy_orders[0][0]  # Negate back to get actual price
            best_sell = self.sell_orders[0][0]
            
            # If no match, we're done
            if best_buy < best_sell:
                break
                
            # We have a match at market price
            match_price = best_sell
            _, buy_time, buy_id = heapq.heappop(self.buy_orders)
            _, sell_time, sell_id = heapq.heappop(self.sell_orders)
            
            buy_order = self.orders[buy_id]
            sell_order = self.orders[sell_id]
            
            # Determine match quantity
            match_qty = min(buy_order.quantity, sell_order.quantity)
            
            # Create trade message
            trade_msg = {
                "message_type": MessageType.TRADE.value,
                "symbol": self.symbol,
                "price": match_price,
                "quantity": match_qty,
                "buy_order_id": buy_id,
                "sell_order_id": sell_id,
                "timestamp": int(time.time() * 1_000_000),
                "sequence": self._next_sequence()
            }
            messages.append(trade_msg)
            
            # Update last price
            self.last_price = match_price
            
            # Update order quantities
            buy_order.quantity -= match_qty
            sell_order.quantity -= match_qty
            
            # If orders still have quantity, push back to heap
            if buy_order.quantity > 0:
                heapq.heappush(self.buy_orders, (-buy_order.price, buy_time, buy_id))
            else:
                del self.orders[buy_id]
                
            if sell_order.quantity > 0:
                heapq.heappush(self.sell_orders, (sell_order.price, sell_time, sell_id))
            else:
                del self.orders[sell_id]
                
        return messages
    
    def _create_message(self, msg_type: MessageType, order: Order) -> dict:
        """Create a standardized message from an order"""
        return {
            "message_type": msg_type.value,
            "order_id": order.order_id,
            "symbol": order.symbol,
            "side": order.side.value,
            "price": order.price,
            "quantity": order.quantity,
            "timestamp": order.timestamp,
            "venue": order.venue,
            "sequence": self._next_sequence()
        }
    
    def _next_sequence(self) -> int:
        """Get next sequence number for this book"""
        self.sequence_number += 1
        return self.sequence_number
    
    def get_market_price(self) -> float:
        """Get current market price (mid-price if spread exists)"""
        if not self.buy_orders and not self.sell_orders:
            # Use last price, or default starting price
            return self.last_price or 100.0
            
        if self.buy_orders and self.sell_orders:
            # Mid price
            best_bid = -self.buy_orders[0][0]
            best_ask = self.sell_orders[0][0]
            return (best_bid + best_ask) / 2
        
        if self.buy_orders:
            return -self.buy_orders[0][0]
            
        return self.sell_orders[0][0]
    
    def get_top_of_book(self) -> dict:
        """Get top of book for this symbol"""
        best_bid = -self.buy_orders[0][0] if self.buy_orders else None
        best_ask = self.sell_orders[0][0] if self.sell_orders else None
        
        return {
            "symbol": self.symbol,
            "bid": best_bid,
            "ask": best_ask,
            "mid": (best_bid + best_ask) / 2 if best_bid and best_ask else None,
            "timestamp": int(time.time() * 1_000_000)
        }

class MarketSimulator:
    """Multi-symbol market simulator"""
    
    def __init__(self, symbols=None):
        self.symbols = symbols or ["AAPL", "MSFT", "AMZN", "GOOGL", "META", "TSLA", "NVDA", "AMD", "INTC", "CSCO"]
        self.books = {symbol: OrderBook(symbol) for symbol in self.symbols}
        self.global_sequence = 0
        
    def generate_random_orders(self, count=10, volatility=0.2) -> List[dict]:
        """Generate a batch of random orders with realistic price movements"""
        messages = []
        
        for _ in range(count):
            # Select random symbol
            symbol = random.choice(self.symbols)
            book = self.books[symbol]
            
            # Get current market price as base
            base_price = book.get_market_price()
            
            # Generate realistic price with small movements around current price
            # Higher volatility = larger price jumps
            price_change = random.normalvariate(0, volatility)
            new_price = max(0.01, base_price * (1 + price_change))
            new_price = round(new_price, 2)  # Round to cents
            
            # Generate random quantity (with distribution favoring smaller sizes)
            quantity = int(max(1, random.lognormvariate(3, 1)))
            
            # Slightly favor adding orders vs canceling existing ones
            if random.random() < 0.7 or len(book.orders) < 5:  # Add new order
                side = Side.BUY if random.random() < 0.5 else Side.SELL
                venue = random.choice(["NASDAQ", "NYSE", "IEX", "ARCA"])
                _, new_messages = book.add_order(side, new_price, quantity, venue)
                messages.extend(new_messages)
            else:  # Cancel or modify existing order
                if book.orders:
                    order_id = random.choice(list(book.orders.keys()))
                    if random.random() < 0.5:  # Cancel
                        msg = book.cancel_order(order_id)
                    else:  # Modify
                        new_qty = max(1, book.orders[order_id].quantity + random.randint(-5, 5))
                        msg = book.modify_order(order_id, new_qty)
                    
                    if msg:
                        messages.append(msg)
        
        return messages
    
    def generate_market_volatility(self, symbol, magnitude=0.05) -> List[dict]:
        """Generate a volatility event for a specific symbol"""
        messages = []
        book = self.books[symbol]
        
        # Current price
        base_price = book.get_market_price()
        
        # Direction of volatility
        direction = 1 if random.random() < 0.5 else -1
        
        # Generate a burst of orders in the volatility direction
        order_count = random.randint(20, 50)  # More orders during volatility
        
        for i in range(order_count):
            # Price becomes progressively more extreme
            progress = i / order_count
            price_change = direction * magnitude * (1 + progress)
            new_price = base_price * (1 + price_change)
            new_price = max(0.01, round(new_price, 2))
            
            # Quantity increases with volatility
            quantity = int(max(1, random.lognormvariate(4, 1.5)))
            
            # During volatility, orders are mostly in one direction
            if random.random() < 0.8:
                side = Side.SELL if direction < 0 else Side.BUY
            else:
                side = Side.BUY if direction < 0 else Side.SELL
                
            venue = random.choice(["NASDAQ", "NYSE", "IEX", "ARCA"])
            _, new_messages = book.add_order(side, new_price, quantity, venue)
            messages.extend(new_messages)
            
            # Occasionally cancel existing orders (panic)
            if random.random() < 0.3 and book.orders:
                order_id = random.choice(list(book.orders.keys()))
                msg = book.cancel_order(order_id)
                if msg:
                    messages.append(msg)
        
        return messages
    
    def serialize_to_length_prefixed(self, message):
        """Convert a message to length-prefixed binary format"""
        import struct
        import json
        
        # Convert message to JSON string
        json_data = json.dumps(message).encode('utf-8')
        
        # Create length prefix (4-byte little-endian unsigned int)
        length = len(json_data)
        length_prefix = struct.pack("<I", length)
        
        # Return as bytes
        return length_prefix + json_data

if __name__ == "__main__":
    # Simple test
    simulator = MarketSimulator()
    messages = simulator.generate_random_orders(5)
    for msg in messages:
        print(f"{msg['message_type']} - {msg['symbol']} - {msg['price']:.2f} x {msg['quantity']}")
