# Apple Container Integration Testing

This guide shows how to run network adapter integration tests using Apple's native container system with two Pipeline instances communicating over HTTP.

## Why Apple Containers?

Apple's container system provides **native Linux micro-VMs** that run directly on hardware (Apple Silicon or Rosetta), unlike Docker Desktop which runs containers inside a larger VM. This provides:
- Better performance
- Native IP addressing (192.168.64.x)
- Direct hardware access
- Lower overhead

## Architecture

```
┌─────────────────────────────────┐         HTTP POST         ┌──────────────────────────────────┐
│   Container 1 (192.168.64.6)    │ ──────────────────────▶  │   Container 2 (192.168.64.7)     │
│                                  │                           │                                   │
│  Alpine Linux (arm64)            │                           │  Alpine Linux (arm64)             │
│  ├─ Pipeline Engine              │                           │  ├─ Pipeline Engine               │
│  ├─ HTTP Server Adapter          │                           │  ├─ HTTP Client                   │
│  └─ HTTP Client Emitter          │ ◀──────────────────────  │  └─ Sends requests every 2s       │
│                                  │      Echo Response        │                                   │
│  Port: 8080                      │                           │  Target: 192.168.64.6:8080        │
│  /tmp/http-echo                  │                           │  /tmp/http-client                 │
└─────────────────────────────────┘                           └──────────────────────────────────┘
```

## Prerequisites

- macOS with Apple container support
- Two Alpine Linux containers running (create with `container run`)
- Go 1.23+ for building binaries

## Quick Start

### Automated Setup

```bash
# Run the automated test script
./test-apple-containers.sh
```

This script will:
1. Build linux/arm64 binaries
2. Copy them to containers
3. Start the echo server on container 1
4. Start the http client on container 2
5. Show initial logs
6. Wait for you to press Enter to stop

### Manual Setup

#### 1. List Available Containers

```bash
container list
```

Expected output:
```
ID                                    IMAGE                            OS     ARCH   STATE    ADDR          CPUS  MEMORY
a9621065-787e-4e16-9c78-a59aa6b40563  docker.io/library/alpine:latest  linux  arm64  running  192.168.64.6  4     1024 MB
61f63a57-de35-463f-b44c-9e955d40edcb  docker.io/library/alpine:latest  linux  arm64  running  192.168.64.7  4     1024 MB
```

#### 2. Build Binaries for linux/arm64

```bash
cd examples/http-echo
GOOS=linux GOARCH=arm64 go build -o http-echo-linux

cd ../http-client
GOOS=linux GOARCH=arm64 go build -o http-client-linux
```

#### 3. Copy Binaries to Containers

```bash
# Copy echo server to container 1
cat examples/http-echo/http-echo-linux | \
  container exec -i a9621065-787e-4e16-9c78-a59aa6b40563 \
  sh -c 'cat > /tmp/http-echo && chmod +x /tmp/http-echo'

# Copy client to container 2
cat examples/http-client/http-client-linux | \
  container exec -i 61f63a57-de35-463f-b44c-9e955d40edcb \
  sh -c 'cat > /tmp/http-client && chmod +x /tmp/http-client'
```

#### 4. Start Echo Server

```bash
# Start server in background
container exec a9621065-787e-4e16-9c78-a59aa6b40563 \
  sh -c 'nohup /tmp/http-echo > /tmp/server.log 2>&1 &'

# Verify it's running
container exec a9621065-787e-4e16-9c78-a59aa6b40563 ps aux | grep http-echo
```

#### 5. Start HTTP Client

```bash
# Start client in background, pointing to server
container exec 61f63a57-de35-463f-b44c-9e955d40edcb \
  sh -c 'TARGET_SERVER=http://192.168.64.6:8080 INTERVAL=2s nohup /tmp/http-client > /tmp/client.log 2>&1 &'

# Verify it's running
container exec 61f63a57-de35-463f-b44c-9e955d40edcb ps aux | grep http-client
```

#### 6. Watch Logs

```bash
# Watch both logs live
./watch-containers.sh

# Or manually:
# Server logs
container exec a9621065-787e-4e16-9c78-a59aa6b40563 tail -f /tmp/server.log

# Client logs
container exec 61f63a57-de35-463f-b44c-9e955d40edcb tail -f /tmp/client.log
```

## Expected Output

### Server Logs

```
Starting HTTP Echo Server on :8080
Try: curl -X POST http://localhost:8080/test -d 'Hello, Pipeline!'
Press Ctrl+C to stop
[c0844734] POST /api/test from 192.168.64.7:35282
[bb350e53] POST /api/test from 192.168.64.7:35282
[0d361ca0] POST /api/test from 192.168.64.7:35282
[b81f11b4] POST /api/test from 192.168.64.7:35282
```

### Client Logs

```
HTTP Client starting
Target: http://192.168.64.6:8080
Interval: 2s
---
✅ Request #1: 46 bytes sent, 230 bytes received
   Response preview: Echo: POST /api/test...
✅ Request #2: 46 bytes sent, 230 bytes received
   Response preview: Echo: POST /api/test...
✅ Request #3: 46 bytes sent, 230 bytes received
   Response preview: Echo: POST /api/test...
```

## Key Features Demonstrated

✅ **Request/Response Correlation** - Request IDs match between client and server
✅ **Native IP Networking** - Direct communication at 192.168.64.x
✅ **Pipeline Event Bus** - HTTP requests flow through event-driven architecture
✅ **Adapter/Emitter Pattern** - Server adapter receives, client emitter sends
✅ **Cross-Container Communication** - Two independent Pipeline instances cooperating

## Performance Testing

### Increase Request Frequency

```bash
# 10 requests per second
container exec CLIENT_ID sh -c 'pkill http-client'
container exec CLIENT_ID sh -c 'TARGET_SERVER=http://192.168.64.6:8080 INTERVAL=100ms nohup /tmp/http-client > /tmp/client.log 2>&1 &'
```

### Monitor Resource Usage

```bash
# Check CPU and memory in containers
container exec SERVER_ID top -b -n 1

# Check network connections
container exec SERVER_ID netstat -an | grep 8080
```

### Stress Test

```bash
# Run multiple clients
for i in {1..5}; do
  container exec CLIENT_ID sh -c "TARGET_SERVER=http://192.168.64.6:8080 INTERVAL=100ms nohup /tmp/http-client > /tmp/client-$i.log 2>&1 &"
done
```

## Debugging

### Check Connectivity

```bash
# Ping server from client
container exec CLIENT_ID ping -c 3 192.168.64.6

# Test HTTP endpoint
container exec CLIENT_ID wget -O- http://192.168.64.6:8080/api/test
```

### View Processes

```bash
# List all processes
container exec SERVER_ID ps aux

# Filter for our apps
container exec SERVER_ID ps aux | grep http
```

### Check Logs

```bash
# Full logs
container exec SERVER_ID cat /tmp/server.log
container exec CLIENT_ID cat /tmp/client.log

# Last 50 lines
container exec SERVER_ID tail -50 /tmp/server.log
```

### Network Inspection

```bash
# Check listening ports
container exec SERVER_ID netstat -tlnp

# Check established connections
container exec SERVER_ID netstat -tnp | grep ESTABLISHED
```

## Stopping Services

```bash
# Stop server
container exec SERVER_ID pkill -f http-echo

# Stop client
container exec CLIENT_ID pkill -f http-client

# Or use the test script's cleanup (press Enter when prompted)
```

## Creating New Containers

If you need fresh containers:

```bash
# Create Alpine container
container run -d --name my-pipeline-1 alpine:latest sleep infinity

# Create second container
container run -d --name my-pipeline-2 alpine:latest sleep infinity

# Install busybox-extras (if needed)
container exec my-pipeline-1 apk add --no-cache busybox-extras
container exec my-pipeline-2 apk add --no-cache busybox-extras
```

## Advanced Usage

### Custom Client Logic

Modify `examples/http-client/main.go` to:
- Send different payload types
- Test error handling
- Implement retry logic
- Add custom headers

### Multiple Servers

Run multiple echo servers on different ports:

```bash
# Server on port 8081
container exec SERVER_ID sh -c 'sed "s/:8080/:8081/" /tmp/http-echo > /tmp/http-echo-2 && chmod +x /tmp/http-echo-2'
container exec SERVER_ID /tmp/http-echo-2 &
```

### Load Balancing Test

Point multiple clients to the same server to test concurrent handling.

## Troubleshooting

### "Connection refused"

- Check if server is running: `container exec SERVER_ID ps aux | grep http-echo`
- Verify port is listening: `container exec SERVER_ID netstat -tlnp | grep 8080`
- Check firewall rules (should be open within container network)

### "Binary not found"

- Verify copy succeeded: `container exec SERVER_ID ls -lh /tmp/http-*`
- Check architecture: `container exec SERVER_ID file /tmp/http-echo`
- Ensure execute permission: `container exec SERVER_ID chmod +x /tmp/http-echo`

### No logs appearing

- Check if process is running
- Verify log file exists: `container exec SERVER_ID ls -lh /tmp/*.log`
- Try running in foreground to see errors: `container exec -it SERVER_ID /tmp/http-echo`

## Comparison: Apple Containers vs Docker

| Feature | Apple Containers | Docker Desktop |
|---------|------------------|----------------|
| **VM Overhead** | No VM (native micro-VMs) | Runs inside LinuxKit VM |
| **Performance** | Native hardware access | Virtualization layer |
| **Networking** | Direct IP (192.168.64.x) | Bridge network via VM |
| **Resource Usage** | Lower | Higher (VM + containers) |
| **Startup Time** | Faster | Slower (VM boot) |
| **Compatibility** | macOS only | Cross-platform |

## CI/CD Integration

Example GitHub Actions workflow (when running on macOS runners):

```yaml
name: Apple Container Integration Test

on: [push, pull_request]

jobs:
  integration-test:
    runs-on: macos-latest
    steps:
      - uses: actions/checkout@v3

      - name: Setup Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.23'

      - name: Create containers
        run: |
          container run -d alpine:latest sleep infinity
          container run -d alpine:latest sleep infinity

      - name: Run integration test
        run: ./test-apple-containers.sh

      - name: Collect logs
        if: always()
        run: |
          container logs > server.log
          container logs > client.log

      - name: Upload logs
        uses: actions/upload-artifact@v3
        if: always()
        with:
          name: container-logs
          path: '*.log'
```

---

**Status:** Production Ready
**Platform:** macOS with Apple container support
**Maintainer:** BYTE-6D65
