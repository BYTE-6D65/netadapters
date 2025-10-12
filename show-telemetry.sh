#!/bin/bash

# Get container IDs
SERVER_ID=$(container list | grep 192.168.64.6 | awk '{print $1}')
CLIENT_ID=$(container list | grep 192.168.64.7 | awk '{print $1}')

echo "═══════════════════════════════════════════════════════════════════════════════"
echo "🔬 PIPELINE TELEMETRY SNAPSHOT"
echo "═══════════════════════════════════════════════════════════════════════════════"
echo ""

# Get process status
echo "📊 Process Status:"
echo "─────────────────"
SERVER_PID=$(container exec $SERVER_ID ps aux | grep "[/]tmp/http-echo-inst" | awk '{print $1}')
CLIENT_PID=$(container exec $CLIENT_ID ps aux | grep "[/]tmp/http-client-inst" | awk '{print $1}')

if [ -n "$SERVER_PID" ]; then
    echo "✅ Server:  Running (PID $SERVER_PID)"
else
    echo "❌ Server:  Not running"
fi

if [ -n "$CLIENT_PID" ]; then
    echo "✅ Client:  Running (PID $CLIENT_PID)"
else
    echo "❌ Client:  Not running"
fi

echo ""

# Get statistics
echo "📈 Statistics:"
echo "─────────────"
SERVER_REQUESTS=$(container exec $SERVER_ID grep -c "📊 REQUEST #" /tmp/server-inst.log 2>/dev/null || echo "0")
SERVER_EVENTS=$(container exec $SERVER_ID grep -c "📨 Received event from bus" /tmp/server-inst.log 2>/dev/null || echo "0")
SERVER_RESPONSES=$(container exec $SERVER_ID grep -c "✅ Response published" /tmp/server-inst.log 2>/dev/null || echo "0")

CLIENT_REQUESTS=$(container exec $CLIENT_ID grep -c "📤 Initiating request" /tmp/client-inst.log 2>/dev/null || echo "0")
CLIENT_SUCCESS=$(container exec $CLIENT_ID grep -c "✅ REQUEST #" /tmp/client-inst.log 2>/dev/null || echo "0")
CLIENT_FAILED=$(container exec $CLIENT_ID grep -c "❌ Request #" /tmp/client-inst.log 2>/dev/null || echo "0")

echo "Server:"
echo "  Requests received:    $SERVER_REQUESTS"
echo "  Events processed:     $SERVER_EVENTS"
echo "  Responses published:  $SERVER_RESPONSES"
echo ""
echo "Client:"
echo "  Requests sent:        $CLIENT_REQUESTS"
echo "  Successful:           $CLIENT_SUCCESS"
echo "  Failed:               $CLIENT_FAILED"
if [ "$CLIENT_REQUESTS" -gt 0 ]; then
    SUCCESS_RATE=$(awk "BEGIN {printf \"%.1f\", ($CLIENT_SUCCESS/$CLIENT_REQUESTS)*100}")
    echo "  Success rate:         $SUCCESS_RATE%"
fi

echo ""
echo "═══════════════════════════════════════════════════════════════════════════════"
echo "📋 SERVER TELEMETRY (Last 30 lines)"
echo "═══════════════════════════════════════════════════════════════════════════════"
echo ""

container exec $SERVER_ID tail -30 /tmp/server-inst.log 2>/dev/null || echo "No server logs available"

echo ""
echo "═══════════════════════════════════════════════════════════════════════════════"
echo "📋 CLIENT TELEMETRY (Last 30 lines)"
echo "═══════════════════════════════════════════════════════════════════════════════"
echo ""

container exec $CLIENT_ID tail -30 /tmp/client-inst.log 2>/dev/null || echo "No client logs available"

echo ""
echo "═══════════════════════════════════════════════════════════════════════════════"
echo ""
echo "💡 Commands:"
echo "  Watch live:            ./watch-telemetry.sh"
echo "  Server full log:       container exec $SERVER_ID cat /tmp/server-inst.log"
echo "  Client full log:       container exec $CLIENT_ID cat /tmp/client-inst.log"
echo "  Follow server logs:    container exec $SERVER_ID tail -f /tmp/server-inst.log"
echo "  Follow client logs:    container exec $CLIENT_ID tail -f /tmp/client-inst.log"
echo ""
