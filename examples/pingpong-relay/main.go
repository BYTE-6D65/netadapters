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
	LogInit  = "[INIT]"
	LogRelay = "[RELAY]"
	LogStats = "[STATS]"
	LogProm  = "[PROM]"
)

// Shared HTTP client with connection pooling
var relayClient = &http.Client{
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
	},
	Timeout: 10 * time.Second,
}

// RelayStats tracks throughput metrics
type RelayStats struct {
	requestsIn      atomic.Uint64
	requestsOut     atomic.Uint64
	responsesIn     atomic.Uint64
	responsesOut    atomic.Uint64
	errors          atomic.Uint64

	// Timing metrics (in nanoseconds)
	totalPipelineTime   atomic.Uint64
	minPipelineTime     atomic.Uint64
	maxPipelineTime     atomic.Uint64
	totalForwardTime    atomic.Uint64
	minForwardTime      atomic.Uint64
	maxForwardTime      atomic.Uint64

	// Prometheus metrics
	requestsInCounter  prometheus.Counter
	requestsOutCounter prometheus.Counter
	errorCounter       prometheus.Counter
	pipelineHistogram  prometheus.Histogram
	forwardHistogram   prometheus.Histogram
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	// Configuration
	listenAddr := getEnv("LISTEN", ":8080")
	targetURL := getEnv("TARGET", "http://192.168.64.8:8080")
	metricsAddr := getEnv("METRICS", ":9090")

	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println("🔄 PINGPONG RELAY (Container B - CDN)")
	fmt.Println("═══════════════════════════════════════════════════════")
	log.Printf("%s Listen Address: %s", LogInit, listenAddr)
	log.Printf("%s Target (Container C): %s", LogInit, targetURL)
	log.Printf("%s Metrics Address: %s", LogInit, metricsAddr)
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println()

	// Initialize Prometheus metrics
	metrics := telemetry.InitMetrics(prometheus.DefaultRegisterer)
	log.Printf("%s Pipeline metrics initialized", LogProm)

	// Create custom metrics
	stats := &RelayStats{
		requestsInCounter: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "relay_requests_in_total",
			Help: "Total number of requests received",
		}),
		requestsOutCounter: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "relay_requests_out_total",
			Help: "Total number of requests forwarded",
		}),
		errorCounter: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "relay_errors_total",
			Help: "Total number of relay errors",
		}),
		pipelineHistogram: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "relay_pipeline_duration_seconds",
			Help: "Time for Pipeline event processing",
			Buckets: []float64{
				0.000001, 0.000002, 0.000005, 0.00001, 0.00002, 0.00005,
				0.0001, 0.0002, 0.0005, 0.001, 0.002, 0.005, 0.01, 0.02, 0.05,
			},
		}),
		forwardHistogram: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "relay_forward_duration_seconds",
			Help: "Time for HTTP forwarding",
			Buckets: []float64{
				0.0001, 0.0002, 0.0005, 0.001, 0.002, 0.005,
				0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1.0,
			},
		}),
	}

	prometheus.MustRegister(stats.requestsInCounter, stats.requestsOutCounter, stats.errorCounter,
		stats.pipelineHistogram, stats.forwardHistogram)
	log.Printf("%s Relay metrics registered", LogProm)

	// Initialize min values to max uint64
	stats.minPipelineTime.Store(^uint64(0))
	stats.minForwardTime.Store(^uint64(0))

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

	// Subscribe to HTTP request events
	sub, err := eng.ExternalBus().Subscribe(context.Background(), event.Filter{
		Types: []string{nethttp.EventTypeHTTPRequest},
	})
	if err != nil {
		log.Fatalf("%s Failed to subscribe: %v", LogInit, err)
	}
	defer sub.Close()

	// Create HTTP server adapter
	httpServer := nethttp.NewServerAdapter(listenAddr)

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

	// Event processor
	go func() {
		for evt := range sub.Events() {
			pipelineStart := time.Now()

			payload, ok := evt.Payload.(nethttp.HTTPRequestPayload)
			if !ok {
				log.Printf("%s Invalid payload type", LogRelay)
				continue
			}

			stats.requestsIn.Add(1)
			stats.requestsInCounter.Inc()

			pipelineDuration := time.Since(pipelineStart)
			pipelineNs := uint64(pipelineDuration.Nanoseconds())
			stats.totalPipelineTime.Add(pipelineNs)
			updateMin(&stats.minPipelineTime, pipelineNs)
			updateMax(&stats.maxPipelineTime, pipelineNs)
			stats.pipelineHistogram.Observe(pipelineDuration.Seconds())

			// Forward asynchronously
			go func(p nethttp.HTTPRequestPayload) {
				forwardStart := time.Now()

				log.Printf("%s → Forwarding to Container C: %s", LogRelay, p.Path)

				err := forwardRequest(targetURL, &p)

				forwardDuration := time.Since(forwardStart)
				forwardNs := uint64(forwardDuration.Nanoseconds())
				stats.totalForwardTime.Add(forwardNs)
				updateMin(&stats.minForwardTime, forwardNs)
				updateMax(&stats.maxForwardTime, forwardNs)
				stats.forwardHistogram.Observe(forwardDuration.Seconds())

				if err != nil {
					log.Printf("%s ❌ Forward error: %v", LogRelay, err)
					stats.errors.Add(1)
					stats.errorCounter.Inc()
				} else {
					stats.requestsOut.Add(1)
					stats.requestsOutCounter.Inc()
					log.Printf("%s ✅ Forwarded in %v", LogRelay, forwardDuration)
				}
			}(payload)
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

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	log.Printf("%s Shutting down...", LogInit)
	printStats(stats)
}

func forwardRequest(target string, payload *nethttp.HTTPRequestPayload) error {
	req, err := http.NewRequest(payload.Method, target+payload.Path, bytes.NewReader(payload.Body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	// Copy headers
	for k, v := range payload.Headers {
		req.Header.Set(k, v)
	}

	resp, err := relayClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	// Read and discard body (we don't forward response in this direction)
	io.Copy(io.Discard, resp.Body)

	return nil
}

func printStats(stats *RelayStats) {
	reqIn := stats.requestsIn.Load()
	reqOut := stats.requestsOut.Load()
	errs := stats.errors.Load()

	var avgPipeline, avgForward time.Duration
	if reqIn > 0 {
		avgPipeline = time.Duration(stats.totalPipelineTime.Load() / reqIn)
	}
	if reqOut > 0 {
		avgForward = time.Duration(stats.totalForwardTime.Load() / reqOut)
	}
	minPipeline := time.Duration(stats.minPipelineTime.Load())
	maxPipeline := time.Duration(stats.maxPipelineTime.Load())
	minForward := time.Duration(stats.minForwardTime.Load())
	maxForward := time.Duration(stats.maxForwardTime.Load())

	// Get memory stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	log.Printf("═══════════════════════════════════════════════════════")
	log.Printf("📊 RELAY STATS (Container B)")
	log.Printf("═══════════════════════════════════════════════════════")
	log.Printf("Requests In:       %d", reqIn)
	log.Printf("Requests Out:      %d", reqOut)
	log.Printf("Errors:            %d", errs)
	log.Printf("Success Rate:      %.2f%%", float64(reqOut)/float64(reqIn)*100)
	log.Printf("─────────────────────────────────────────────────────")
	log.Printf("Pipeline Avg:      %v", avgPipeline)
	log.Printf("Pipeline Min:      %v", minPipeline)
	log.Printf("Pipeline Max:      %v", maxPipeline)
	log.Printf("─────────────────────────────────────────────────────")
	log.Printf("Forward Avg:       %v", avgForward)
	log.Printf("Forward Min:       %v", minForward)
	log.Printf("Forward Max:       %v", maxForward)
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
