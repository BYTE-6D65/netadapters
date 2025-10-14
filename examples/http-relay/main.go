package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	nethttp "github.com/BYTE-6D65/netadapters/pkg/http"
	"github.com/BYTE-6D65/pipeline/pkg/engine"
	"github.com/BYTE-6D65/pipeline/pkg/event"
)

const (
	LogEngine  = "[ENGINE]"
	LogAdapter = "[ADAPTER]"
	LogEmitter = "[EMITTER]"
	LogRelay   = "[RELAY]"
	LogStats   = "[STATS]"
	LogBus     = "[BUS]"
)

// Shared HTTP client with connection pooling (THE FIX!)
var relayClient = &http.Client{
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false, // Enable keep-alive
	},
	Timeout: 10 * time.Second,
}

type RelayStats struct {
	received        atomic.Uint64
	forwarded       atomic.Uint64
	dropped         atomic.Uint64
	errors          atomic.Uint64
	circlesComplete atomic.Uint64
	lastUpdate      atomic.Value // time.Time

	// Performance metrics
	totalBusProcessTime atomic.Uint64 // nanoseconds
	totalForwardTime    atomic.Uint64 // nanoseconds
	minBusProcess       atomic.Uint64 // nanoseconds
	maxBusProcess       atomic.Uint64 // nanoseconds
	minForward          atomic.Uint64 // nanoseconds
	maxForward          atomic.Uint64 // nanoseconds
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	// Get config from environment
	listenAddr := os.Getenv("LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = ":8080"
	}

	nextHop := os.Getenv("NEXT_HOP")
	if nextHop == "" {
		log.Fatal("NEXT_HOP environment variable required")
	}

	maxHops := 10 // Prevent infinite loops
	if maxHopsEnv := os.Getenv("MAX_HOPS"); maxHopsEnv != "" {
		if h, err := strconv.Atoi(maxHopsEnv); err == nil {
			maxHops = h
		}
	}

	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		nodeName = listenAddr
	}

	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Printf("🔄 HTTP RELAY NODE: %s\n", nodeName)
	fmt.Println("═══════════════════════════════════════════════════════")
	log.Printf("%s Starting Pipeline engine", LogEngine)

	// Create pipeline engine
	eng := engine.New()
	defer func() {
		log.Printf("%s Shutting down", LogEngine)
		eng.Shutdown(context.Background())
	}()

	log.Printf("%s Engine created", LogEngine)
	log.Printf("%s Listen address: %s", LogAdapter, listenAddr)
	log.Printf("%s Next hop: %s", LogRelay, nextHop)
	log.Printf("%s Max hops: %d", LogRelay, maxHops)

	// Statistics
	stats := &RelayStats{}
	stats.lastUpdate.Store(time.Now())

	// Create HTTP server adapter (receives requests)
	httpServer := nethttp.NewServerAdapter(listenAddr)

	// Create HTTP client emitter (sends responses back)
	httpClient := nethttp.NewClientEmitter()

	// Register adapter
	log.Printf("%s Registering HTTP Server Adapter", LogAdapter)
	adapterMgr := engine.NewAdapterManager(eng)
	if err := adapterMgr.Register(httpServer); err != nil {
		log.Fatalf("%s Failed to register adapter: %v", LogAdapter, err)
	}
	if err := adapterMgr.Start(); err != nil {
		log.Fatalf("%s Failed to start adapters: %v", LogAdapter, err)
	}
	log.Printf("%s ✅ HTTP Server Adapter started", LogAdapter)

	// Register emitter
	log.Printf("%s Registering HTTP Client Emitter", LogEmitter)
	emitterMgr := engine.NewEmitterManager(eng)
	if err := emitterMgr.Register("http-client", httpClient, event.Filter{
		Types: []string{"net.http.response"},
	}); err != nil {
		log.Fatalf("%s Failed to register emitter: %v", LogEmitter, err)
	}
	if err := emitterMgr.Start(); err != nil {
		log.Fatalf("%s Failed to start emitters: %v", LogEmitter, err)
	}
	log.Printf("%s ✅ HTTP Client Emitter started", LogEmitter)

	// Subscribe to HTTP requests
	log.Printf("%s Creating subscription", LogBus)
	sub, err := eng.ExternalBus().Subscribe(context.Background(), event.Filter{
		Types: []string{"net.http.request"},
	})
	if err != nil {
		log.Fatalf("%s Failed to subscribe: %v", LogBus, err)
	}
	defer sub.Close()
	log.Printf("%s ✅ Subscription created", LogBus)

	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println("✅ Relay node ready")
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println()

	// Relay logic: receive → forward → respond
	go func() {
		codec := event.JSONCodec{}

		for evt := range sub.Events() {
			stats.received.Add(1)
			receiveTime := time.Now()
			busProcessStart := time.Now()

			// Decode request payload
			var payload nethttp.HTTPRequestPayload
			if err := evt.DecodePayload(&payload, codec); err != nil {
				log.Printf("%s ❌ Failed to decode payload: %v", LogRelay, err)
				stats.errors.Add(1)
				continue
			}

			// Check hop count
			hopCount := 1
			if hopHeader, ok := payload.Headers["X-Hop-Count"]; ok {
				if h, err := strconv.Atoi(hopHeader); err == nil {
					hopCount = h + 1
				}
			}

			log.Printf("%s 📨 Received request #%d (hop %d) from %s",
				LogRelay, stats.received.Load(), hopCount, payload.RemoteAddr)
			log.Printf("%s   Request ID: %s", LogRelay, payload.RequestID)
			log.Printf("%s   Method: %s %s", LogRelay, payload.Method, payload.Path)
			log.Printf("%s   Body: %s", LogRelay, truncate(string(payload.Body), 60))

			// Check if this completes a circle (request visited all nodes)
			visitedNodes := payload.Headers["X-Visited-Nodes"]
			if visitedNodes != "" && strings.Contains(visitedNodes, "NodeA,NodeB,NodeC") {
				stats.circlesComplete.Add(1)
				log.Printf("%s 🔄 Circle completed! Total circles: %d", LogRelay, stats.circlesComplete.Load())
			}

			// Check if we've exceeded max hops (loop prevention)
			if hopCount > maxHops {
				log.Printf("%s ⚠️  Max hops exceeded (%d), dropping request", LogRelay, hopCount)
				stats.dropped.Add(1)

				// Send response back
				_ = createResponse(payload, fmt.Sprintf("Max hops exceeded at node %s", nodeName))
				if respEvt, err := nethttp.CreateEchoResponse(evt); err == nil {
					eng.ExternalBus().Publish(context.Background(), *respEvt)
				}
				continue
			}

			// Forward to next hop asynchronously (fire-and-forget to avoid circular deadlock)
			go func(p nethttp.HTTPRequestPayload, hc int) {
				startForward := time.Now()
				forwardErr := forwardRequest(nextHop, &p, hc, nodeName)
				forwardDuration := time.Since(startForward)
				forwardNs := uint64(forwardDuration.Nanoseconds())

				// Track forward timing
				stats.totalForwardTime.Add(forwardNs)
				updateMin(&stats.minForward, forwardNs)
				updateMax(&stats.maxForward, forwardNs)

				if forwardErr != nil {
					log.Printf("%s ❌ Forward failed: %v (took %v)", LogRelay, forwardErr, forwardDuration)
					stats.errors.Add(1)
				} else {
					log.Printf("%s ✅ Forwarded to %s in %v", LogRelay, nextHop, forwardDuration)
					stats.forwarded.Add(1)
				}
			}(payload, hopCount)

			// Create response (immediate response to avoid blocking)
			responseBody := fmt.Sprintf("Relayed by %s (hop %d) → %s\nOriginal: %s",
				nodeName, hopCount, nextHop, string(payload.Body))

			respPayload := nethttp.HTTPResponsePayload{
				RequestID:  payload.RequestID,
				StatusCode: 200,
				Headers: map[string]string{
					"Content-Type": "text/plain",
					"X-Relay-Node": nodeName,
					"X-Hop-Count":  strconv.Itoa(hopCount),
				},
				Body:      []byte(responseBody),
				Timestamp: time.Now(),
				Duration:  time.Since(receiveTime),
			}

			// Publish response
			respEvt, err := event.NewEvent("net.http.response", nodeName, respPayload, codec)
			if err != nil {
				log.Printf("%s ❌ Failed to create response event: %v", LogRelay, err)
				continue
			}
			respEvt.WithMetadata("request_id", payload.RequestID)

			if err := eng.ExternalBus().Publish(context.Background(), *respEvt); err != nil {
				log.Printf("%s ❌ Failed to publish response: %v", LogRelay, err)
			}

			// Track Pipeline event processing time (from receive to publish)
			busProcessDuration := time.Since(busProcessStart)
			busProcessNs := uint64(busProcessDuration.Nanoseconds())
			stats.totalBusProcessTime.Add(busProcessNs)
			updateMin(&stats.minBusProcess, busProcessNs)
			updateMax(&stats.maxBusProcess, busProcessNs)

			log.Printf("%s ⏱️  Total relay time: %v (bus: %v)", LogRelay, time.Since(receiveTime), busProcessDuration)
			log.Printf("%s ────────────────────────────────────────", LogRelay)
			fmt.Println()
		}
	}()

	// Stats printer
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			received := stats.received.Load()
			forwarded := stats.forwarded.Load()
			var avgBus, avgForward time.Duration
			if received > 0 {
				avgBus = time.Duration(stats.totalBusProcessTime.Load() / received)
			}
			if forwarded > 0 {
				avgForward = time.Duration(stats.totalForwardTime.Load() / forwarded)
			}
			minBus := time.Duration(stats.minBusProcess.Load())
			maxBus := time.Duration(stats.maxBusProcess.Load())
			minFwd := time.Duration(stats.minForward.Load())
			maxFwd := time.Duration(stats.maxForward.Load())

			// Get memory stats
			var m runtime.MemStats
			runtime.ReadMemStats(&m)

			log.Printf("%s ═══════════════════════════════════════", LogStats)
			log.Printf("%s 📊 RELAY STATISTICS", LogStats)
			log.Printf("%s   Received:  %d", LogStats, received)
			log.Printf("%s   Forwarded: %d", LogStats, forwarded)
			log.Printf("%s   Dropped:   %d", LogStats, stats.dropped.Load())
			log.Printf("%s   Errors:    %d", LogStats, stats.errors.Load())
			log.Printf("%s   Circles:   %d", LogStats, stats.circlesComplete.Load())
			successRate := float64(forwarded) / float64(received) * 100
			log.Printf("%s   Success:   %.1f%%", LogStats, successRate)
			log.Printf("%s", LogStats)
			log.Printf("%s ⚡ PERFORMANCE METRICS", LogStats)
			log.Printf("%s   Pipeline (avg):  %v", LogStats, avgBus)
			log.Printf("%s   Pipeline (min):  %v", LogStats, minBus)
			log.Printf("%s   Pipeline (max):  %v", LogStats, maxBus)
			log.Printf("%s   Forward (avg):   %v", LogStats, avgForward)
			log.Printf("%s   Forward (min):   %v", LogStats, minFwd)
			log.Printf("%s   Forward (max):   %v", LogStats, maxFwd)
			log.Printf("%s", LogStats)
			log.Printf("%s 💾 MEMORY USAGE", LogStats)
			log.Printf("%s   Heap Alloc:    %s", LogStats, formatBytes(m.Alloc))
			log.Printf("%s   Heap Sys:      %s", LogStats, formatBytes(m.HeapSys))
			log.Printf("%s   Stack:         %s", LogStats, formatBytes(m.StackSys))
			log.Printf("%s   Total Alloc:   %s", LogStats, formatBytes(m.TotalAlloc))
			log.Printf("%s   GC Runs:       %d", LogStats, m.NumGC)
			log.Printf("%s   Goroutines:    %d", LogStats, runtime.NumGoroutine())
			log.Printf("%s ═══════════════════════════════════════", LogStats)
		}
	}()

	// HTML dashboard generator
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		// Generate immediately on start
		generateDashboard(nodeName, stats, nextHop)

		for range ticker.C {
			generateDashboard(nodeName, stats, nextHop)
		}
	}()

	// HTTP server for dashboard on port 8081
	go func() {
		dashboardMux := http.NewServeMux()
		dashboardMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, "/tmp/dashboard.html")
		})

		dashboardServer := &http.Server{
			Addr:    ":8081",
			Handler: dashboardMux,
		}

		log.Printf("%s 🌐 Starting dashboard server on http://localhost:8081", LogStats)
		if err := dashboardServer.ListenAndServe(); err != nil {
			log.Printf("%s Dashboard server error: %v", LogStats, err)
		}
	}()

	// Wait for interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	fmt.Println()
	log.Printf("%s Received shutdown signal", LogEngine)
	log.Printf("%s Final stats: Received=%d Forwarded=%d Dropped=%d Errors=%d",
		LogStats, stats.received.Load(), stats.forwarded.Load(),
		stats.dropped.Load(), stats.errors.Load())
}

// forwardRequest forwards the request to the next hop using connection pooling
func forwardRequest(nextHop string, payload *nethttp.HTTPRequestPayload, hopCount int, nodeName string) error {
	// Build new body with relay info
	relayBody := fmt.Sprintf("[%s→hop%d] %s", nodeName, hopCount, string(payload.Body))

	// Create request
	req, err := http.NewRequest("POST", nextHop+payload.Path, bytes.NewBufferString(relayBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	// Copy headers and add hop tracking
	for k, v := range payload.Headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("X-Hop-Count", strconv.Itoa(hopCount))
	req.Header.Set("X-Relay-Node", nodeName)
	req.Header.Set("X-Original-Request-ID", payload.RequestID)

	// Track visited nodes for circle detection
	visitedNodes := payload.Headers["X-Visited-Nodes"]
	if visitedNodes == "" {
		req.Header.Set("X-Visited-Nodes", nodeName)
	} else {
		req.Header.Set("X-Visited-Nodes", visitedNodes+","+nodeName)
	}

	// Use shared client with connection pooling
	resp, err := relayClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	// Read and discard response (we're just forwarding)
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != 200 {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return nil
}

func generateDashboard(nodeName string, stats *RelayStats, nextHop string) {
	stats.lastUpdate.Store(time.Now())
	lastUpdate := stats.lastUpdate.Load().(time.Time)

	received := stats.received.Load()
	forwarded := stats.forwarded.Load()
	dropped := stats.dropped.Load()
	errors := stats.errors.Load()
	circles := stats.circlesComplete.Load()

	var successRate float64
	if received > 0 {
		successRate = float64(forwarded) / float64(received) * 100
	}

	// Calculate performance metrics
	var avgBus, avgForward time.Duration
	if received > 0 {
		avgBus = time.Duration(stats.totalBusProcessTime.Load() / received)
	}
	if forwarded > 0 {
		avgForward = time.Duration(stats.totalForwardTime.Load() / forwarded)
	}
	minBus := time.Duration(stats.minBusProcess.Load())
	maxBus := time.Duration(stats.maxBusProcess.Load())
	minFwd := time.Duration(stats.minForward.Load())
	maxFwd := time.Duration(stats.maxForward.Load())

	// Get memory stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta http-equiv="refresh" content="2">
    <title>%s - Relay Dashboard</title>
    <style>
        body {
            font-family: 'Courier New', monospace;
            background: linear-gradient(135deg, #667eea 0%%, #764ba2 100%%);
            color: white;
            margin: 0;
            padding: 20px;
            min-height: 100vh;
        }
        .container {
            max-width: 800px;
            margin: 0 auto;
        }
        .header {
            text-align: center;
            padding: 20px;
            background: rgba(0,0,0,0.3);
            border-radius: 15px;
            margin-bottom: 20px;
            box-shadow: 0 8px 32px 0 rgba(31, 38, 135, 0.37);
        }
        .node-name {
            font-size: 3em;
            font-weight: bold;
            margin: 0;
            text-shadow: 2px 2px 4px rgba(0,0,0,0.5);
        }
        .subtitle {
            font-size: 1.2em;
            margin: 10px 0;
            opacity: 0.9;
        }
        .stats-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 15px;
            margin-bottom: 20px;
        }
        .stat-card {
            background: rgba(0,0,0,0.3);
            padding: 20px;
            border-radius: 10px;
            text-align: center;
            box-shadow: 0 8px 32px 0 rgba(31, 38, 135, 0.37);
            transition: transform 0.3s ease;
        }
        .stat-card:hover {
            transform: translateY(-5px);
        }
        .stat-value {
            font-size: 2.5em;
            font-weight: bold;
            margin: 10px 0;
        }
        .stat-label {
            font-size: 0.9em;
            text-transform: uppercase;
            opacity: 0.8;
            letter-spacing: 1px;
        }
        .circles {
            background: rgba(255, 215, 0, 0.2);
            border: 2px solid gold;
        }
        .circles .stat-value {
            color: gold;
            animation: pulse 2s infinite;
        }
        @keyframes pulse {
            0%%, 100%% { transform: scale(1); }
            50%% { transform: scale(1.1); }
        }
        .success {
            background: rgba(0, 255, 0, 0.2);
            border: 2px solid lime;
        }
        .success .stat-value {
            color: lime;
        }
        .info-panel {
            background: rgba(0,0,0,0.3);
            padding: 20px;
            border-radius: 10px;
            margin-top: 20px;
            box-shadow: 0 8px 32px 0 rgba(31, 38, 135, 0.37);
        }
        .info-row {
            display: flex;
            justify-content: space-between;
            padding: 10px 0;
            border-bottom: 1px solid rgba(255,255,255,0.1);
        }
        .info-row:last-child {
            border-bottom: none;
        }
        .footer {
            text-align: center;
            margin-top: 20px;
            opacity: 0.7;
            font-size: 0.9em;
        }
        .emoji {
            font-size: 1.5em;
            margin-right: 10px;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1 class="node-name">🔄 %s</h1>
            <p class="subtitle">Pipeline Circular Relay Node</p>
        </div>

        <div class="stats-grid">
            <div class="stat-card circles">
                <div class="stat-label"><span class="emoji">🔄</span>Circles</div>
                <div class="stat-value">%d</div>
            </div>
            <div class="stat-card">
                <div class="stat-label"><span class="emoji">📨</span>Received</div>
                <div class="stat-value">%d</div>
            </div>
            <div class="stat-card">
                <div class="stat-label"><span class="emoji">📤</span>Forwarded</div>
                <div class="stat-value">%d</div>
            </div>
            <div class="stat-card success">
                <div class="stat-label"><span class="emoji">✅</span>Success Rate</div>
                <div class="stat-value">%.1f%%</div>
            </div>
        </div>

        <div class="stats-grid">
            <div class="stat-card">
                <div class="stat-label"><span class="emoji">⚠️</span>Dropped</div>
                <div class="stat-value">%d</div>
            </div>
            <div class="stat-card">
                <div class="stat-label"><span class="emoji">❌</span>Errors</div>
                <div class="stat-value">%d</div>
            </div>
        </div>

        <div class="info-panel">
            <div class="info-row">
                <span>Next Hop:</span>
                <strong>%s</strong>
            </div>
            <div class="info-row">
                <span>Last Update:</span>
                <strong>%s</strong>
            </div>
            <div class="info-row">
                <span>Status:</span>
                <strong style="color: lime;">🟢 ACTIVE</strong>
            </div>
        </div>

        <div class="info-panel">
            <h3 style="margin-top: 0; text-align: center; color: gold;">⚡ PERFORMANCE METRICS</h3>
            <div class="info-row">
                <span>🔌 Pipeline Avg:</span>
                <strong style="color: cyan;">%s</strong>
            </div>
            <div class="info-row">
                <span>🔌 Pipeline Min/Max:</span>
                <strong style="color: cyan;">%s / %s</strong>
            </div>
            <div class="info-row">
                <span>🌐 HTTP Forward Avg:</span>
                <strong style="color: orange;">%s</strong>
            </div>
            <div class="info-row">
                <span>🌐 HTTP Forward Min/Max:</span>
                <strong style="color: orange;">%s / %s</strong>
            </div>
        </div>

        <div class="info-panel">
            <h3 style="margin-top: 0; text-align: center; color: #ff6b6b;">💾 MEMORY USAGE</h3>
            <div class="info-row">
                <span>📊 Heap Alloc:</span>
                <strong style="color: #51cf66;">%s</strong>
            </div>
            <div class="info-row">
                <span>📦 Heap Sys:</span>
                <strong style="color: #51cf66;">%s</strong>
            </div>
            <div class="info-row">
                <span>📚 Stack:</span>
                <strong style="color: #51cf66;">%s</strong>
            </div>
            <div class="info-row">
                <span>🔄 GC Runs:</span>
                <strong style="color: #ffd43b;">%d</strong>
            </div>
            <div class="info-row">
                <span>⚙️ Goroutines:</span>
                <strong style="color: #ffd43b;">%d</strong>
            </div>
        </div>

        <div class="footer">
            <p>🚀 Powered by Pipeline Event Bus | Auto-refreshes every 2 seconds</p>
        </div>
    </div>
</body>
</html>`,
		nodeName,
		nodeName,
		circles,
		received,
		forwarded,
		successRate,
		dropped,
		errors,
		nextHop,
		lastUpdate.Format("15:04:05"),
		avgBus.String(),
		minBus.String(), maxBus.String(),
		avgForward.String(),
		minFwd.String(), maxFwd.String(),
		formatBytes(m.Alloc),
		formatBytes(m.HeapSys),
		formatBytes(m.StackSys),
		m.NumGC,
		runtime.NumGoroutine(),
	)

	if err := os.WriteFile("/tmp/dashboard.html", []byte(html), 0644); err != nil {
		log.Printf("[DASHBOARD] ❌ Failed to write HTML: %v", err)
	}
}

func createResponse(payload nethttp.HTTPRequestPayload, message string) nethttp.HTTPResponsePayload {
	return nethttp.HTTPResponsePayload{
		RequestID:  payload.RequestID,
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type": "text/plain",
		},
		Body:      []byte(message),
		Timestamp: time.Now(),
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func updateMin(current *atomic.Uint64, value uint64) {
	for {
		old := current.Load()
		if old != 0 && old <= value {
			return // Current min is smaller
		}
		if current.CompareAndSwap(old, value) {
			return
		}
	}
}

func updateMax(current *atomic.Uint64, value uint64) {
	for {
		old := current.Load()
		if old >= value {
			return // Current max is larger
		}
		if current.CompareAndSwap(old, value) {
			return
		}
	}
}

func formatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
