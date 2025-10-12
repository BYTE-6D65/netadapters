#!/bin/bash

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Get container IDs
SERVER_ID=$(container list | grep 192.168.64.6 | awk '{print $1}')
CLIENT_ID=$(container list | grep 192.168.64.7 | awk '{print $1}')

echo "🍎 Monitoring Pipeline Network Adapters"
echo "======================================="
echo ""
echo "Server: $SERVER_ID (192.168.64.6:8080)"
echo "Client: $CLIENT_ID (192.168.64.7)"
echo ""
echo "Press Ctrl+C to stop monitoring"
echo ""

# Function to show logs side by side
show_logs() {
    echo -e "${GREEN}=== Server Logs ===${NC}"
    container exec $SERVER_ID tail -5 /tmp/server.log 2>/dev/null || echo "No server logs yet"
    echo ""
    echo -e "${BLUE}=== Client Logs ===${NC}"
    container exec $CLIENT_ID tail -5 /tmp/client.log 2>/dev/null || echo "No client logs yet"
    echo ""
    echo "---"
}

# Watch logs every 2 seconds
while true; do
    clear
    echo "🍎 Monitoring Pipeline Network Adapters - $(date '+%H:%M:%S')"
    echo "======================================="
    echo ""
    show_logs
    sleep 2
done
