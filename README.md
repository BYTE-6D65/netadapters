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
                      │    └──────────────┘             │
┌─────────────────┐   │           │                    │
│ MQTT Message    │ ──┘           │                    │
└─────────────────┘               │                    │
                                  ▼                    ▼
                         ┌──────────────┐    ┌─────────────────┐
                         │  Metrics &   │    │   HTTP Response │
                         │  Monitoring  │    │   MQTT Publish  │
                         └──────────────┘    │   WebSocket Send│
                                             └─────────────────┘
```

**Key Concepts:**
- **Adapters** = Network receivers (listen for data)
- **Emitters** = Network senders (send data)
- **Events** = Normalized representations of network I/O
- **Pipeline** = Central event processing infrastructure

## 📚 Documentation

- **[ARCHITECTURE.md](ARCHITECTURE.md)** - Detailed design and specifications
- **[docs/http.md](docs/http.md)** - HTTP adapter usage guide
- **[docs/websocket.md](docs/websocket.md)** - WebSocket adapter usage guide
- **[docs/patterns.md](docs/patterns.md)** - Common integration patterns

## 🔧 Examples

See `examples/` for complete working examples:
- `http-echo/` - Simple HTTP echo server
- `websocket-chat/` - WebSocket chat room
- `http-to-mqtt/` - HTTP → MQTT gateway
- `mqtt-bridge/` - MQTT topic bridge

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

See [ARCHITECTURE.md](ARCHITECTURE.md) for all payload types.

## 🔒 Security Features

- **TLS/SSL** - HTTPS, WSS support
- **Authentication** - Bearer tokens, API keys, OAuth
- **Rate Limiting** - Per-connection or global
- **Input Validation** - Size limits, sanitization
- **Timeouts** - Configurable per protocol

## 📊 Observability

Network adapters emit metrics events:

```go
// Event Type: "net.metrics"
type NetworkMetrics struct {
    Protocol        string        // http, websocket, mqtt
    ConnectionCount int           // Active connections
    BytesReceived   uint64        // Total bytes in
    BytesSent       uint64        // Total bytes out
    ErrorCount      uint64
    Timestamp       time.Time
}
```

Subscribe to metrics for monitoring dashboards:
```go
eng.ExternalBus().Subscribe(ctx, event.Filter{
    Types: []string{"net.metrics", "net.connection.*"},
}, metricsHandler)
```

## 🧪 Testing

Mock adapters and emitters for testing:

```go
// Create mock HTTP adapter
mockAdapter := http.NewMockServerAdapter()

// Inject test events
mockAdapter.InjectRequest(testRequest)

// Verify emitted responses
responses := mockEmitter.GetResponses()
```

## 🎯 Roadmap

### Phase 1: HTTP Foundation (Current)
- ✅ Architecture design
- 🚧 HTTP Server Adapter
- 🚧 HTTP Client Emitter
- 🚧 Request/Response correlation
- 🚧 Examples and tests

### Phase 2: WebSocket Support
- WebSocket Server Adapter
- WebSocket Client Emitter
- Connection management
- Chat server example

### Phase 3: Raw Sockets
- TCP Listener/Client
- UDP Listener/Client
- Echo server examples

### Phase 4: MQTT Integration
- MQTT Subscriber/Publisher
- QoS handling
- IoT bridge examples

## 🔗 Related Projects

- **[Pipeline](https://github.com/BYTE-6D65/pipeline)** - Core event processing library (required)
- **[CmdWhl](https://github.com/BYTE-6D65/CmdWhl)** - Hardware I/O adapters

## 🤝 Contributing

Contributions welcome! To add a new protocol:

1. Create `pkg/[protocol]/` directory
2. Implement `pipeline/pkg/adapter.Adapter` interface
3. Implement `pipeline/pkg/emitter.Emitter` interface
4. Define event payload types
5. Add tests and examples
6. Update documentation

See [ARCHITECTURE.md](ARCHITECTURE.md) for design guidelines.

## 📄 License

MIT License - See LICENSE file for details

---

**Status:** Early development - HTTP adapter in progress

**Maintainer:** BYTE-6D65

## 💭 Code Generation Philosophy

Because generating code is so cheap now, all code has been written via LLMs. Extensive time has been dedicated to architecture planning and logical flow. Documentation is the source of truth and the concrete reference for code generation.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
