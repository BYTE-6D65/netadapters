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
	"sync/atomic"
	"syscall"
	"time"

	nethttp "github.com/BYTE-6D65/netadapters/pkg/http"
	"github.com/BYTE-6D65/pipeline/pkg/engine"
	"github.com/BYTE-6D65/pipeline/pkg/event"
	"github.com/BYTE-6D65/pipeline/pkg/telemetry"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	LogInit = "[INIT]"
	LogSend = "[SEND]"
	LogRecv = "[RECV]"
	LogProm = "[PROM]"
)

// Shared HTTP client with connection pooling
var httpClient = &http.Client{
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
	},
	Timeout: 10 * time.Second,
}

// PingPongStats tracks request/response metrics
type PingPongStats struct {
	requestsSent    atomic.Uint64
	responsesRecv   atomic.Uint64
	errors          atomic.Uint64

	// Timing metrics (in nanoseconds)
	totalRTT        atomic.Uint64
	minRTT          atomic.Uint64
	maxRTT          atomic.Uint64

	// Prometheus metrics
	requestCounter  prometheus.Counter
	responseCounter prometheus.Counter
	errorCounter    prometheus.Counter
	rttHistogram    prometheus.Histogram
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	// Configuration
	targetURL := getEnv("TARGET", "http://192.168.64.7:8080")
	listenAddr := getEnv("LISTEN", ":8080")
	metricsAddr := getEnv("METRICS", ":9090")
	interval := getDuration("INTERVAL", 1*time.Second)

	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println("🏓 PINGPONG INITIATOR (Container A)")
	fmt.Println("═══════════════════════════════════════════════════════")
	log.Printf("%s Target (Container B): %s", LogInit, targetURL)
	log.Printf("%s Listen Address: %s", LogInit, listenAddr)
	log.Printf("%s Metrics Address: %s", LogInit, metricsAddr)
	log.Printf("%s Ping Interval: %s", LogInit, interval)
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println()

	// Initialize Prometheus metrics
	metrics := telemetry.InitMetrics(prometheus.DefaultRegisterer)
	log.Printf("%s Pipeline metrics initialized", LogProm)

	// Create custom metrics for ping-pong
	stats := &PingPongStats{
		requestCounter: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "pingpong_requests_sent_total",
			Help: "Total number of ping requests sent",
		}),
		responseCounter: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "pingpong_responses_received_total",
			Help: "Total number of pong responses received",
		}),
		errorCounter: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "pingpong_errors_total",
			Help: "Total number of ping-pong errors",
		}),
		rttHistogram: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "pingpong_rtt_seconds",
			Help: "Round-trip time for ping-pong in seconds",
			Buckets: []float64{
				0.0001, 0.0002, 0.0005, 0.001, 0.002, 0.005,
				0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1.0,
			},
		}),
	}

	// Register custom metrics
	prometheus.MustRegister(stats.requestCounter, stats.responseCounter, stats.errorCounter, stats.rttHistogram)
	log.Printf("%s Ping-pong metrics registered", LogProm)

	// Initialize min RTT to max uint64
	stats.minRTT.Store(^uint64(0))

	// Create Pipeline engine
	eng := engine.New(
		engine.WithInternalBus(event.NewInMemoryBus(
			event.WithBufferSize(64),
			event.WithBusName("internal"),
			event.WithMetrics(metrics),
		)),
		engine.WithExternalBus(event.NewInMemoryBus(
			event.WithBufferSize(128),
			event.WithBusName("external"),
			event.WithMetrics(metrics),
		)),
	)
	defer eng.Shutdown(context.Background())
	log.Printf("%s Pipeline engine created", LogInit)

	// Create HTTP server adapter (receives pong responses)
	httpServer := nethttp.NewServerAdapter(listenAddr)
	eng.ExternalBus().Subscribe(context.Background(), event.Filter{
		Types: []string{nethttp.EventTypeHTTPRequest},
	})

	// Start HTTP server
	go func() {
		if err := httpServer.Start(context.Background(), eng); err != nil {
			log.Fatalf("%s Failed to start HTTP server: %v", LogInit, err)
		}
	}()
	log.Printf("%s HTTP server started on %s", LogInit, listenAddr)

	// Start Prometheus metrics server
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		log.Printf("%s Prometheus metrics server starting on %s", LogProm, metricsAddr)
		if err := http.ListenAndServe(metricsAddr, mux); err != nil {
			log.Fatalf("%s Failed to start metrics server: %v", LogProm, err)
		}
	}()

	// Periodic stats logger
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			printStats(stats)
		}
	}()

	// Ping sender
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		requestNum := 0
		for range ticker.C {
			requestNum++
			sendPing(targetURL, requestNum, stats)
		}
	}()

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	log.Printf("%s Shutting down...", LogInit)
	printStats(stats)
}

func sendPing(target string, num int, stats *PingPongStats) {
	startTime := time.Now()

	payload := fmt.Sprintf("PING #%d from Initiator at %s", num, time.Now().Format(time.RFC3339Nano))

	log.Printf("%s 📤 Sending PING #%d", LogSend, num)

	req, err := http.NewRequest("POST", target+"/api/ping", bytes.NewBufferString(payload))
	if err != nil {
		log.Printf("%s ❌ Failed to create request: %v", LogSend, err)
		stats.errors.Add(1)
		stats.errorCounter.Inc()
		return
	}

	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("X-Request-ID", fmt.Sprintf("ping-%d", num))
	req.Header.Set("X-Timestamp", fmt.Sprintf("%d", time.Now().UnixNano()))

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("%s ❌ Request failed: %v", LogSend, err)
		stats.errors.Add(1)
		stats.errorCounter.Inc()
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("%s ⚠️  Failed to read response: %v", LogSend, err)
		stats.errors.Add(1)
		stats.errorCounter.Inc()
		return
	}

	rtt := time.Since(startTime)
	rttNs := uint64(rtt.Nanoseconds())

	// Update stats
	stats.requestsSent.Add(1)
	stats.responsesRecv.Add(1)
	stats.totalRTT.Add(rttNs)
	updateMin(&stats.minRTT, rttNs)
	updateMax(&stats.maxRTT, rttNs)

	// Update Prometheus metrics
	stats.requestCounter.Inc()
	stats.responseCounter.Inc()
	stats.rttHistogram.Observe(rtt.Seconds())

	log.Printf("%s ✅ PONG received in %v", LogRecv, rtt)
	log.Printf("%s    Body: %s", LogRecv, truncate(string(body), 100))
}

func printStats(stats *PingPongStats) {
	sent := stats.requestsSent.Load()
	recv := stats.responsesRecv.Load()
	errs := stats.errors.Load()

	var avgRTT time.Duration
	if sent > 0 {
		avgRTT = time.Duration(stats.totalRTT.Load() / sent)
	}
	minRTT := time.Duration(stats.minRTT.Load())
	maxRTT := time.Duration(stats.maxRTT.Load())

	// Get memory stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	log.Printf("═══════════════════════════════════════════════════════")
	log.Printf("📊 INITIATOR STATS")
	log.Printf("═══════════════════════════════════════════════════════")
	log.Printf("Requests Sent:     %d", sent)
	log.Printf("Responses Recv:    %d", recv)
	log.Printf("Errors:            %d", errs)
	log.Printf("Success Rate:      %.2f%%", float64(recv)/float64(sent)*100)
	log.Printf("─────────────────────────────────────────────────────")
	log.Printf("RTT Avg:           %v", avgRTT)
	log.Printf("RTT Min:           %v", minRTT)
	log.Printf("RTT Max:           %v", maxRTT)
	log.Printf("─────────────────────────────────────────────────────")
	log.Printf("Heap Alloc:        %s", formatBytes(m.Alloc))
	log.Printf("Total Alloc:       %s", formatBytes(m.TotalAlloc))
	log.Printf("GC Runs:           %d", m.NumGC)
	log.Printf("Goroutines:        %d", runtime.NumGoroutine())
	log.Printf("═══════════════════════════════════════════════════════")
}

func updateMin(atomic *atomic.Uint64, value uint64) {
	for {
		old := atomic.Load()
		if value >= old {
			return
		}
		if atomic.CompareAndSwap(old, value) {
			return
		}
	}
}

func updateMax(atomic *atomic.Uint64, value uint64) {
	for {
		old := atomic.Load()
		if value <= old {
			return
		}
		if atomic.CompareAndSwap(old, value) {
			return
		}
	}
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

func getDuration(key string, defaultValue time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return defaultValue
}

func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
