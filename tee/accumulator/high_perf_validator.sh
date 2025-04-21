#!/bin/bash
echo "Starting dual-format parameter validator on port $1"
python3 -m http.server $1 &
