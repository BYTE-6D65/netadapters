#!/bin/bash

set -e

echo "🍎 Apple Container Integration Test"
echo "===================================="
echo ""

# Get container IDs and IPs
SERVER_ID=$(container list | grep 192.168.64.6 | awk '{print $1}')
CLIENT_ID=$(container list | grep 192.168.64.7 | awk '{print $1}')
SERVER_IP="192.168.64.6"
CLIENT_IP="192.168.64.7"

echo "📦 Containers:"
echo "  Server: $SERVER_ID ($SERVER_IP)"
echo "  Client: $CLIENT_ID ($CLIENT_IP)"
echo ""

# Build binaries for linux/arm64
echo "🔨 Building binaries for linux/arm64..."
cd /Users/liam/Projects/02_scratchpad/netadapters
(cd examples/http-echo && GOOS=linux GOARCH=arm64 go build -o http-echo-linux)
(cd examples/http-client && GOOS=linux GOARCH=arm64 go build -o http-client-linux)
echo "✅ Build complete"
echo ""

# Copy binaries to containers
echo "📤 Copying binaries to containers..."
cat examples/http-echo/http-echo-linux | container exec -i $SERVER_ID sh -c 'cat > /tmp/http-echo && chmod +x /tmp/http-echo'
cat examples/http-client/http-client-linux | container exec -i $CLIENT_ID sh -c 'cat > /tmp/http-client && chmod +x /tmp/http-client'
echo "✅ Binaries copied"
echo ""

# Stop any existing processes
echo "🧹 Cleaning up existing processes..."
container exec $SERVER_ID sh -c 'pkill -f http-echo || true'
container exec $CLIENT_ID sh -c 'pkill -f http-client || true'
sleep 1
echo ""

# Start server
echo "🚀 Starting echo server on $SERVER_IP:8080..."
container exec $SERVER_ID sh -c 'rm -f /tmp/server.log && nohup /tmp/http-echo > /tmp/server.log 2>&1 &'
sleep 2

# Verify server started
if container exec $SERVER_ID ps aux | grep -q "[/]tmp/http-echo"; then
    echo "✅ Server running"
else
    echo "❌ Server failed to start"
    exit 1
fi
echo ""

# Start client
echo "🚀 Starting http client on $CLIENT_IP..."
container exec $CLIENT_ID sh -c "rm -f /tmp/client.log && TARGET_SERVER=http://$SERVER_IP:8080 INTERVAL=2s nohup /tmp/http-client > /tmp/client.log 2>&1 &"
sleep 2

# Verify client started
if container exec $CLIENT_ID ps aux | grep -q "[/]tmp/http-client"; then
    echo "✅ Client running"
else
    echo "❌ Client failed to start"
    exit 1
fi
echo ""

echo "📊 Test Status:"
echo "=============="
echo ""

# Show initial logs
echo "Server logs:"
echo "------------"
container exec $SERVER_ID tail -20 /tmp/server.log
echo ""

echo "Client logs:"
echo "------------"
container exec $CLIENT_ID tail -20 /tmp/client.log
echo ""

echo "✅ Integration test is running!"
echo ""
echo "Commands:"
echo "  Watch server logs: container exec $SERVER_ID tail -f /tmp/server.log"
echo "  Watch client logs: container exec $CLIENT_ID tail -f /tmp/client.log"
echo "  Stop server:       container exec $SERVER_ID pkill -f http-echo"
echo "  Stop client:       container exec $CLIENT_ID pkill -f http-client"
echo "  View processes:    container exec $SERVER_ID ps aux | grep http"
echo ""
echo "Press Enter to stop the test..."
read

# Cleanup
echo ""
echo "🧹 Stopping services..."
container exec $SERVER_ID pkill -f http-echo || true
container exec $CLIENT_ID pkill -f http-client || true
echo "✅ Test stopped"
