#!/bin/bash

# Function to get timestamp from a server
get_timestamp() {
    local port=$1
    local outfile=$2
    echo -n "test-nonce-$(date +%s)" | base64 | xargs -I {} curl -s -X POST -H "X-Nonce: {}" -H "X-Region: test-region" http://localhost:$port/timestamp > "$outfile"
}

echo "Collecting timestamps from both servers in parallel..."
echo

# Create temporary files for output
temp1=$(mktemp)
temp2=$(mktemp)

# Start both requests in parallel
get_timestamp 8080 "$temp1" &
get_timestamp 8081 "$temp2" &

# Wait for both to complete
wait

# Read results
server1_response=$(cat "$temp1")
server2_response=$(cat "$temp2")

# Clean up temp files
rm "$temp1" "$temp2"

echo "Server 1 (port 8080) response:"
echo "$server1_response" | jq '.'
echo

echo "Server 2 (port 8081) response:"
echo "$server2_response" | jq '.'
echo

# Extract and compare timestamps
time1=$(echo "$server1_response" | jq -r '.Timestamp.Time')
time2=$(echo "$server2_response" | jq -r '.Timestamp.Time')
delay1=$(echo "$server1_response" | jq -r '.Delay')
delay2=$(echo "$server2_response" | jq -r '.Delay')

# Convert timestamps to nanoseconds for comparison
nano1=$(date -j -f "%Y-%m-%dT%H:%M:%S.%NZ" "$time1" +%s%N)
nano2=$(date -j -f "%Y-%m-%dT%H:%M:%S.%NZ" "$time2" +%s%N)
diff_ns=$((nano2 - nano1))
diff_ms=$(echo "scale=3; $diff_ns / 1000000" | bc)

echo "Summary:"
echo "Server 1 time: $time1 (delay: $delay1 ns)"
echo "Server 2 time: $time2 (delay: $delay2 ns)"
echo "Time difference: $diff_ms ms (server2 - server1)"
