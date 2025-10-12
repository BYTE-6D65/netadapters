# Network Adapters Architecture

**Network I/O adapters for the [Pipeline](https://github.com/BYTE-6D65/pipeline) event processing library.**

## 🎯 Overview

This repository provides network protocol adapters and emitters that integrate with the Pipeline event-driven architecture. Network events are captured, normalized into pipeline events, processed through the event bus, and can be routed to other network destinations.

## 🏗️ Architecture Principles

### 1. Adapter = Receiver (Inbound)
Adapters **listen** for network events and publish them to the pipeline:
- HTTP servers receive requests
- WebSocket servers receive messages
- TCP/UDP listeners receive packets
- MQTT subscribers receive messages
- gRPC servers receive calls

### 2. Emitter = Sender (Outbound)
Emitters **consume** pipeline events and send them over the network:
- HTTP clients send requests
- WebSocket clients send messages
- TCP/UDP clients send packets
- MQTT publishers send messages
- gRPC clients make calls

### 3. Bidirectional Patterns
Many protocols are bidirectional (request/response):
- Adapter receives → processes → Emitter responds
- Example: HTTP request adapter → business logic → HTTP response emitter

## 📦 Supported Protocols

### Phase 1: HTTP/WebSocket (Core)
**Priority:** High - Most common web protocols

#### HTTP Server Adapter
```go
// Listens for HTTP requests, publishes as events
type HTTPServerAdapter struct {
    addr     string           // ":8080"
    routes   map[string]Handler
    server   *http.Server
}

// Event Type: "net.http.request"
// Payload: HTTPRequestPayload {
//     Method, Path, Headers, Body, RemoteAddr, RequestID
// }
```

#### HTTP Client Emitter
```go
// Consumes events, sends HTTP requests
type HTTPClientEmitter struct {
    client   *http.Client
    timeout  time.Duration
}

// Consumes: "net.http.request" events
// Emits: "net.http.response" events (optional callback)
```

#### WebSocket Server Adapter
```go
// Maintains WebSocket connections, receives messages
type WebSocketServerAdapter struct {
    addr        string
    upgrader    websocket.Upgrader
    connections sync.Map  // connID → *websocket.Conn
}

// Event Type: "net.websocket.message"
// Payload: WebSocketMessagePayload {
//     ConnectionID, MessageType, Data, Timestamp
// }
```

#### WebSocket Client Emitter
```go
// Sends WebSocket messages to active connections
type WebSocketClientEmitter struct {
    connections sync.Map  // connID → *websocket.Conn
}

// Consumes: "net.websocket.message" events
// Routes to specific connection by ID
```

### Phase 2: TCP/UDP (Raw Sockets)
**Priority:** Medium - Lower level protocols

#### TCP Listener Adapter
```go
type TCPListenerAdapter struct {
    addr     string
    listener net.Listener
}

// Event Type: "net.tcp.data"
// Payload: TCPDataPayload {
//     ConnectionID, Data, RemoteAddr, LocalAddr
// }
```

#### UDP Listener Adapter
```go
type UDPListenerAdapter struct {
    addr string
    conn *net.UDPConn
}

// Event Type: "net.udp.packet"
// Payload: UDPPacketPayload {
//     Data, RemoteAddr, LocalAddr
// }
```

### Phase 3: MQTT (Pub/Sub)
**Priority:** Medium - IoT and messaging

#### MQTT Subscriber Adapter
```go
type MQTTSubscriberAdapter struct {
    broker  string
    topics  []string
    client  mqtt.Client
}

// Event Type: "net.mqtt.message"
// Payload: MQTTMessagePayload {
//     Topic, Payload, QoS, Retained
// }
```

#### MQTT Publisher Emitter
```go
type MQTTPublisherEmitter struct {
    broker string
    client mqtt.Client
}

// Consumes: "net.mqtt.message" events
// Publishes to MQTT broker
```

### Phase 4: gRPC (Modern RPC)
**Priority:** Low - Advanced use cases

## 🎨 Event Payload Design

### HTTP Request Event
```go
type HTTPRequestPayload struct {
    // Identity
    RequestID   string            `json:"request_id"`   // UUID for correlation

    // Request data
    Method      string            `json:"method"`       // GET, POST, etc.
    Path        string            `json:"path"`         // /api/users
    Query       map[string]string `json:"query"`        // ?foo=bar
    Headers     map[string]string `json:"headers"`      // Content-Type, etc.
    Body        []byte            `json:"body"`         // Request body

    // Network data
    RemoteAddr  string            `json:"remote_addr"`  // Client IP:port
    LocalAddr   string            `json:"local_addr"`   // Server IP:port

    // Metadata
    Timestamp   time.Time         `json:"timestamp"`    // When received
    TLS         bool              `json:"tls"`          // HTTPS?
}
```

### HTTP Response Event
```go
type HTTPResponsePayload struct {
    // Correlation
    RequestID   string            `json:"request_id"`   // Match to request

    // Response data
    StatusCode  int               `json:"status_code"`  // 200, 404, etc.
    Headers     map[string]string `json:"headers"`      // Content-Type, etc.
    Body        []byte            `json:"body"`         // Response body

    // Metadata
    Timestamp   time.Time         `json:"timestamp"`    // When sent
    Duration    time.Duration     `json:"duration"`     // Processing time
}
```

### WebSocket Message Event
```go
type WebSocketMessagePayload struct {
    // Identity
    ConnectionID string           `json:"connection_id"` // UUID for connection
    MessageID    string           `json:"message_id"`    // UUID for message

    // Message data
    MessageType  int              `json:"message_type"`  // Text, Binary, etc.
    Data         []byte           `json:"data"`          // Message content

    // Network data
    RemoteAddr   string           `json:"remote_addr"`   // Client IP:port

    // Metadata
    Timestamp    time.Time        `json:"timestamp"`     // When received
}
```

### TCP Data Event
```go
type TCPDataPayload struct {
    // Identity
    ConnectionID string           `json:"connection_id"` // UUID for connection

    // Data
    Data         []byte           `json:"data"`          // Raw bytes

    // Network data
    RemoteAddr   string           `json:"remote_addr"`   // Peer IP:port
    LocalAddr    string           `json:"local_addr"`    // Local IP:port

    // Metadata
    Timestamp    time.Time        `json:"timestamp"`     // When received
}
```

### MQTT Message Event
```go
type MQTTMessagePayload struct {
    // Identity
    MessageID    string           `json:"message_id"`    // UUID

    // MQTT data
    Topic        string           `json:"topic"`         // sensors/temp
    Payload      []byte           `json:"payload"`       // Message content
    QoS          byte             `json:"qos"`           // 0, 1, or 2
    Retained     bool             `json:"retained"`      // Retained flag

    // Metadata
    Timestamp    time.Time        `json:"timestamp"`     // When received
    Broker       string           `json:"broker"`        // Broker address
}
```

## 🔄 Usage Patterns

### Pattern 1: HTTP API Gateway
```go
// Receive HTTP requests, route to business logic, respond
httpAdapter := http.NewServerAdapter(":8080", routes)
httpEmitter := http.NewClientEmitter()

// Register with engine
adapterMgr.Register(httpAdapter)
emitterMgr.Register("http-client", httpEmitter, event.Filter{
    Types: []string{"net.http.response"},
})

// Business logic subscribes to requests, publishes responses
eng.ExternalBus().Subscribe(ctx, event.Filter{
    Types: []string{"net.http.request"},
})
```

### Pattern 2: WebSocket Chat Server
```go
// Broadcast messages to all connections
wsAdapter := websocket.NewServerAdapter(":8080", "/ws")
wsEmitter := websocket.NewBroadcastEmitter()

// When message received from one client, emit to all
eng.ExternalBus().Subscribe(ctx, event.Filter{
    Types: []string{"net.websocket.message"},
}, func(evt event.Event) {
    // Process and re-emit to all connections
    broadcastEvt := createBroadcastEvent(evt)
    eng.ExternalBus().Publish(ctx, broadcastEvt)
})
```

### Pattern 3: MQTT Bridge
```go
// Bridge between MQTT topics
mqttSub := mqtt.NewSubscriberAdapter("tcp://broker:1883", []string{"sensors/#"})
mqttPub := mqtt.NewPublisherEmitter("tcp://broker:1883")

// Transform sensor data and republish
eng.ExternalBus().Subscribe(ctx, event.Filter{
    Types: []string{"net.mqtt.message"},
}, func(evt event.Event) {
    // Transform and republish to different topic
    transformed := transformSensorData(evt)
    eng.ExternalBus().Publish(ctx, transformed)
})
```

### Pattern 4: HTTP → MQTT Gateway
```go
// HTTP requests trigger MQTT publishes
httpAdapter := http.NewServerAdapter(":8080", routes)
mqttEmitter := mqtt.NewPublisherEmitter("tcp://broker:1883")

// POST /sensors/temp → publish to MQTT sensors/temp
eng.ExternalBus().Subscribe(ctx, event.Filter{
    Types: []string{"net.http.request"},
}, func(evt event.Event) {
    // Convert HTTP request to MQTT message
    mqttEvt := httpToMQTT(evt)
    eng.ExternalBus().Publish(ctx, mqttEvt)
})
```

## 🔒 Security Considerations

### TLS/SSL Support
- All adapters/emitters should support TLS
- Configuration for certificates and keys
- Option to enforce HTTPS/WSS only

### Authentication
- Bearer tokens, API keys, OAuth
- Adapter validates auth before publishing events
- Auth data in event metadata for downstream processing

### Rate Limiting
- Adapters should support rate limiting
- Configurable per-connection or global
- Events published for rate limit violations

### Input Validation
- Sanitize and validate all network input
- Maximum payload sizes
- Timeout configurations

## 📊 Observability

### Metrics Events
Network adapters should publish metrics events:

```go
// Event Type: "net.metrics"
type NetworkMetrics struct {
    AdapterID       string
    Protocol        string        // http, websocket, mqtt
    ConnectionCount int           // Active connections
    BytesReceived   uint64        // Total bytes in
    BytesSent       uint64        // Total bytes out
    ErrorCount      uint64        // Errors encountered
    Timestamp       time.Time
}
```

### Connection Lifecycle Events
```go
// Event Type: "net.connection.open"
// Event Type: "net.connection.close"
type ConnectionEvent struct {
    ConnectionID string
    RemoteAddr   string
    Protocol     string
    Timestamp    time.Time
}
```

## 🎯 Implementation Phases

### Phase 1: HTTP Foundation (Week 1)
- ✅ HTTP Server Adapter
- ✅ HTTP Client Emitter
- ✅ Request/Response correlation
- ✅ Basic routing
- ✅ Examples and docs

### Phase 2: WebSocket Support (Week 2)
- ✅ WebSocket Server Adapter
- ✅ WebSocket Client Emitter
- ✅ Connection management
- ✅ Message broadcasting
- ✅ Examples (chat server)

### Phase 3: Raw Sockets (Week 3)
- ✅ TCP Listener Adapter
- ✅ TCP Client Emitter
- ✅ UDP Listener Adapter
- ✅ UDP Client Emitter
- ✅ Examples (echo server)

### Phase 4: MQTT Integration (Week 4)
- ✅ MQTT Subscriber Adapter
- ✅ MQTT Publisher Emitter
- ✅ QoS handling
- ✅ Examples (IoT bridge)

### Future: Advanced Protocols
- gRPC support
- GraphQL subscriptions
- Server-Sent Events (SSE)
- WebRTC data channels

## 📁 Repository Structure

```
netadapters/
├── pkg/
│   ├── http/              # HTTP adapters and emitters
│   │   ├── server.go      # HTTP Server Adapter
│   │   ├── client.go      # HTTP Client Emitter
│   │   ├── types.go       # HTTPRequestPayload, etc.
│   │   └── server_test.go
│   ├── websocket/         # WebSocket adapters and emitters
│   │   ├── server.go      # WebSocket Server Adapter
│   │   ├── client.go      # WebSocket Client Emitter
│   │   ├── types.go       # WebSocketMessagePayload, etc.
│   │   └── server_test.go
│   ├── tcp/               # TCP adapters and emitters
│   ├── udp/               # UDP adapters and emitters
│   └── mqtt/              # MQTT adapters and emitters
├── examples/
│   ├── http-echo/         # Simple HTTP echo server
│   ├── websocket-chat/    # WebSocket chat room
│   ├── http-to-mqtt/      # HTTP → MQTT gateway
│   └── mqtt-bridge/       # MQTT topic bridge
├── docs/
│   ├── http.md            # HTTP adapter usage
│   ├── websocket.md       # WebSocket adapter usage
│   └── patterns.md        # Common integration patterns
├── ARCHITECTURE.md        # This file
├── README.md              # Getting started
├── go.mod
└── LICENSE
```

## 🔗 Dependencies

```go
require (
    github.com/BYTE-6D65/pipeline v0.0.0-latest
    github.com/gorilla/websocket v1.5.0  // WebSocket
    github.com/eclipse/paho.mqtt.golang v1.4.3  // MQTT
    github.com/google/uuid v1.6.0  // Request IDs
)
```

## 💡 Design Philosophy

1. **Protocol Agnostic**: Pipeline doesn't care about network protocols
2. **Event Driven**: All network I/O becomes events
3. **Composable**: Mix and match adapters/emitters
4. **Observable**: Built-in metrics and lifecycle events
5. **Secure**: TLS, auth, rate limiting by default
6. **Testable**: Mock adapters/emitters for testing

## 🎓 Learning Resources

For users of this library:
- See `examples/` for complete working examples
- See `docs/` for protocol-specific guides
- See Pipeline docs for core event processing concepts

---

**Status:** Design phase - ready for implementation

**Maintainer:** BYTE-6D65
