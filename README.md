# Network Adapters

**Network protocol adapters for the [Pipeline](https://github.com/BYTE-6D65/pipeline) event processing library.**

Transform network I/O into events: HTTP requests, WebSocket messages, TCP/UDP packets, MQTT messages, and more flow through the Pipeline event bus for processing, transformation, and routing.

## 🚀 Quick Start

```bash
go get github.com/BYTE-6D65/netadapters
```

### Simple HTTP Echo Server

```go
package main

import (
    "context"
    "github.com/BYTE-6D65/pipeline/pkg/engine"
    "github.com/BYTE-6D65/pipeline/pkg/event"
    "github.com/BYTE-6D65/netadapters/pkg/http"
)

func main() {
    // Create pipeline engine
    eng := engine.New()
    defer eng.Shutdown(context.Background())

    // Create HTTP server adapter (receives requests)
    httpServer := http.NewServerAdapter(":8080")

    // Create HTTP client emitter (sends responses)
    httpClient := http.NewClientEmitter()

    // Register with engine
    adapterMgr := engine.NewAdapterManager(eng)
    adapterMgr.Register(httpServer)
    adapterMgr.Start()

    emitterMgr := engine.NewEmitterManager(eng)
    emitterMgr.Register("http-client", httpClient, event.Filter{
        Types: []string{"net.http.response"},
    })
    emitterMgr.Start()

    // Echo logic: request → response
    eng.ExternalBus().Subscribe(context.Background(), event.Filter{
        Types: []string{"net.http.request"},
    }, func(evt event.Event) {
        // Create echo response
        response := http.CreateEchoResponse(evt)
        eng.ExternalBus().Publish(context.Background(), response)
    })

    // Server running on :8080
    select {}
}
```

## 📦 Supported Protocols

| Protocol | Adapter (In) | Emitter (Out) | Status |
|----------|--------------|---------------|--------|
| **HTTP** | Server | Client | 🚧 In Progress |
| **WebSocket** | Server | Client | 📋 Planned |
| **TCP** | Listener | Client | 📋 Planned |
| **UDP** | Listener | Client | 📋 Planned |
| **MQTT** | Subscriber | Publisher | 📋 Planned |
| **gRPC** | Server | Client | 💭 Future |

## 🎯 Use Cases

### API Gateway
Transform and route HTTP requests through business logic:
```go
// HTTP → Process → HTTP Response
// HTTP → Transform → MQTT Publish
// HTTP → Validate → Database → HTTP Response
```

### WebSocket Chat Server
Broadcast messages to multiple connections:
```go
// WebSocket Client A → Process → Broadcast → All Clients
```

### Protocol Bridge
Convert between different protocols:
```go
// HTTP POST /sensors → MQTT publish to sensors/data
// MQTT sensors/# → WebSocket broadcast to dashboard
```

### Micro services Communication
Event-driven service mesh:
```go
// Service A (HTTP) → Pipeline → Service B (gRPC)
// Service C (MQTT) → Pipeline → Service D (HTTP)
```

## 🏗️ Architecture

Network adapters integrate seamlessly with Pipeline:

```
┌─────────────────┐
│ HTTP Request    │ ──┐
└─────────────────┘   │
                      │
┌─────────────────┐   │    ┌──────────────┐    ┌─────────────────┐
│ WebSocket Msg   │ ──┼───▶│   Pipeline   │───▶│  Business Logic │
└─────────────────┘   │    │  Event Bus   │    └─────────────────┘
                      │    └──────────────┘            │
┌─────────────────┐   │           │                    │
│ MQTT Message    │ ──┘           │                    │
└─────────────────┘               │                    │
                                  ▼                    ▼
                         ┌──────────────┐    ┌─────────────────-┐
                         │  Metrics &   │    │   HTTP Response  │
                         │  Monitoring  │    │   MQTT Publish   │
                         └──────────────┘    │   WebSocket Send │
                                             └─────────────────-┘
```

**Key Concepts:**
- **Adapters** = Network receivers (listen for data)
- **Emitters** = Network senders (send data)
- **Events** = Normalized representations of network I/O
- **Pipeline** = Central event processing infrastructure

## 🔧 Examples

See `examples/` for complete working examples:
- `http-echo/` - Minimal HTTP echo server using the adapter/emitter managers
- `relay-node/` + `relay-initiator/` - Multi-adapter stress harness for load/telemetry validation

## 🎨 Event Payloads

All network protocols use standardized event payloads:

### HTTP Request Event
```go
type HTTPRequestPayload struct {
    RequestID   string            // UUID for correlation
    Method      string            // GET, POST, etc.
    Path        string            // /api/users
    Headers     map[string]string
    Body        []byte
    RemoteAddr  string            // Client IP
    Timestamp   time.Time
}
```

See [ARCHITECTURE.md](ARCHITECTURE.md) for full payload definitions and conventions.

## 📊 Observability & Stress Testing

The repo ships with a multi-adapter load harness and Grafana dashboard to validate Pipeline under pressure:

1. **Build stress binaries**
   ```bash
   cd examples/relay-node
   GOOS=linux GOARCH=arm64 go build -o relay-node-linux
   cd ../relay-initiator
   GOOS=linux GOARCH=arm64 go build -o relay-initiator-linux
   ```

2. **Deploy on Apple containers (or any Linux hosts)**
   ```bash
   # Node A listens on three adapters and forwards to Node B
   ADAPTER_PORTS=:8080,:8081,:8082 \
   NEXT_HOPS=http://node-b:8080,http://node-b:8081,http://node-b:8082 \
   WORKER_COUNT=6 \
   NODE_NAME=NodeA \
   /relay-node
   ```

3. **Start the initiator with a ramping workload**
   ```bash
   TARGETS=http://node-a:8080,http://node-a:8081,http://node-a:8082 \
   INTERVAL=3s \
   PAYLOAD_START=1024 \
   PAYLOAD_MAX=104857600 \
   PAYLOAD_DURATION=1h \
   /relay-initiator
   ```

4. **Import `grafana-working-dashboard.json`** into Grafana to track:
   - External bus latency (`pipeline_event_send_duration_seconds`)
   - Engine lifecycle metrics (`pipeline_engine_operations_total`, `pipeline_engine_operation_duration_seconds`)
   - Buffer saturation per subscription
   - Initiator payload size and request rate


> **Note on sensitive traffic:** Adapters sharing an engine publish to the same external bus. Until request payloads are end-to-end encrypted, run adapters with different trust levels on separate engine instances or filter by metadata to prevent unintended data sharing.

## 📄 License

MIT License - See LICENSE file for details.
