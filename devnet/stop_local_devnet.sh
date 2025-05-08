#!/bin/bash

echo "Stopping local TEE mesh network..."

if [ -f ./stop_pids.txt ]; then
  while read pid; do
    if [ -n "$pid" ] && ps -p $pid > /dev/null 2>&1; then
      echo "Stopping process with PID $pid"
      kill $pid
    fi
  done < ./stop_pids.txt
  rm ./stop_pids.txt
  echo "All TEE nodes stopped successfully!"
else
  echo "No PID file found. Devnet may not be running."
fi
