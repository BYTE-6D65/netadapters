# Docker Integration Testing

This guide shows how to run network adapter integration tests using Docker containers with two Pipeline instances communicating over HTTP.

## Architecture

```
┌─────────────────────────┐         HTTP POST          ┌─────────────────────────┐
│  Container A: Client    │ ─────────────────────────▶ │  Container B: Server    │
│                         │                            │                         │
│  - Pipeline Engine      │                            │  - Pipeline Engine      │
│  - HTTP Client          │                            │  - HTTP Adapter         │
│  - Sends requests every │ ◀───────────────────────── │  - HTTP Emitter         │
│    2 seconds            │      Echo Response         │  - Echoes requests      │
└─────────────────────────┘                            └─────────────────────────┘
         pipeline-http-client                               pipeline-echo-server
```

## Prerequisites

- Docker or Docker Desktop installed
- docker-compose installed

## Quick Start

### Build and Run

```bash
# Build and start both containers
docker-compose up --build

# Run in detached mode
docker-compose up -d --build

# View logs
docker-compose logs -f

# View logs for specific service
docker-compose logs -f http-client
docker-compose logs -f echo-server

# Stop containers
docker-compose down
```

## Configuration

### Environment Variables

**echo-server:**
- `PORT` - Server port (default: 8080)

**http-client:**
- `TARGET_SERVER` - Target server URL (default: http://echo-server:8080)
- `INTERVAL` - Request interval (default: 2s)

### Example: Custom Configuration

```yaml
# docker-compose.override.yml
version: '3.8'

services:
  http-client:
    environment:
      - INTERVAL=5s  # Send requests every 5 seconds
```

## Viewing Results

### Expected Output

**Server (echo-server):**
```
Starting HTTP Echo Server on :8080
Try: curl -X POST http://localhost:8080/test -d 'Hello, Pipeline!'
Press Ctrl+C to stop
[f6ce8114] POST /api/test from 172.18.0.3:54321
[a7b2c9d5] POST /api/test from 172.18.0.3:54322
[d4e8f1a3] POST /api/test from 172.18.0.3:54323
```

**Client (http-client):**
```
HTTP Client starting
Target: http://echo-server:8080
Interval: 2s
---
✅ Request #1: 65 bytes sent, 198 bytes received
   Response preview: Echo: POST /api/test...
✅ Request #2: 65 bytes sent, 198 bytes received
   Response preview: Echo: POST /api/test...
✅ Request #3: 65 bytes sent, 198 bytes received
   Response preview: Echo: POST /api/test...
```

## Testing Scenarios

### Scenario 1: Load Testing

Increase request frequency to test throughput:

```bash
docker-compose up -d echo-server
docker-compose run --rm -e INTERVAL=100ms http-client
```

### Scenario 2: Network Latency

Add latency to simulate real-world conditions:

```bash
# On the client container
docker exec pipeline-http-client tc qdisc add dev eth0 root netem delay 100ms
```

### Scenario 3: Multiple Clients

Scale up the client:

```bash
docker-compose up -d echo-server
docker-compose up -d --scale http-client=5
```

## Debugging

### Check Container Status

```bash
docker-compose ps
```

### Inspect Networks

```bash
docker network inspect pipeline-network
```

### Test Connectivity

```bash
# From client container
docker exec pipeline-http-client ping echo-server

# From client to server
docker exec pipeline-http-client wget -O- http://echo-server:8080/health
```

### Access Containers

```bash
# Shell into server
docker exec -it pipeline-echo-server /bin/sh

# Shell into client
docker exec -it pipeline-http-client /bin/sh
```

## Performance Metrics

Monitor container resource usage:

```bash
# Real-time stats
docker stats pipeline-echo-server pipeline-http-client

# Sample output:
# CONTAINER              CPU %   MEM USAGE / LIMIT    MEM %   NET I/O
# pipeline-echo-server   0.5%    12.5MiB / 1.952GiB   0.63%   2.1kB / 1.8kB
# pipeline-http-client   0.3%    10.2MiB / 1.952GiB   0.51%   1.8kB / 2.1kB
```

## Cleanup

```bash
# Stop and remove containers
docker-compose down

# Remove volumes and networks
docker-compose down -v

# Remove images
docker-compose down --rmi all
```

## Extending the Test

### Add More Services

```yaml
# docker-compose.yml
services:
  echo-server-2:
    build:
      context: .
      args:
        EXAMPLE: http-echo
    ports:
      - "8081:8080"
    networks:
      - pipeline-network
```

### Custom Client Logic

Create a new example in `examples/custom-client/` and build with:

```dockerfile
# Build with custom example
docker build --build-arg EXAMPLE=custom-client -t my-client .
```

## Troubleshooting

### Connection Refused

- Ensure both containers are on the same network
- Check if server is listening: `docker-compose logs echo-server`
- Verify DNS resolution: `docker exec pipeline-http-client nslookup echo-server`

### Health Check Failing

- Check server logs: `docker-compose logs echo-server`
- Verify port binding: `docker-compose ps`
- Test manually: `docker exec pipeline-echo-server wget -O- http://localhost:8080/`

### Build Fails

- Clear build cache: `docker-compose build --no-cache`
- Check Go module issues: `docker-compose run --rm echo-server go mod verify`

## CI/CD Integration

Example GitHub Actions workflow:

```yaml
name: Docker Integration Test

on: [push, pull_request]

jobs:
  integration-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Build containers
        run: docker-compose build

      - name: Start services
        run: docker-compose up -d

      - name: Wait for services
        run: sleep 10

      - name: Check logs
        run: |
          docker-compose logs echo-server
          docker-compose logs http-client

      - name: Verify connectivity
        run: |
          curl -f http://localhost:8080/api/test -d "test"

      - name: Cleanup
        run: docker-compose down
```

---

**Status:** Ready for testing

**Maintainer:** BYTE-6D65
