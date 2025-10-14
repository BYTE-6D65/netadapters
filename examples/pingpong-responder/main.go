package main

import (
	"bytes"
	"context"
	"fmt"
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
	LogRecv = "[RECV]"
	LogSend = "[SEND]"
	LogStats = "[STATS]"
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

// ResponderStats tracks response metrics
type ResponderStats struct {
	requestsRecv    atomic.Uint64
	responsesSent   atomic.Uint64
	errors          atomic.Uint64

	// Timing metrics (in nanoseconds)
	totalPipelineTime   atomic.Uint64
	minPipelineTime     atomic.Uint64
	maxPipelineTime     atomic.Uint64
	totalResponseTime   atomic.Uint64
	minResponseTime     atomic.Uint64
	maxResponseTime     atomic.Uint64

	// Prometheus metrics
	requestCounter    prometheus.Counter
	responseCounter   prometheus.Counter
	errorCounter      prometheus.Counter
	pipelineHistogram prometheus.Histogram
	responseHistogram prometheus.Histogram
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	// Configuration
	listenAddr := getEnv("LISTEN", ":8080")
	replyTarget := getEnv("REPLY_TARGET", "http://192.168.64.7:8080")
	metricsAddr := getEnv("METRICS", ":9090")

	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println("🎾 PINGPONG RESPONDER (Container C)")
	fmt.Println("═══════════════════════════════════════════════════════")
	log.Printf("%s Listen Address: %s", LogInit, listenAddr)
	log.Printf("%s Reply Target (Container B): %s", LogInit, replyTarget)
	log.Printf("%s Metrics Address: %s", LogInit, metricsAddr)
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println()

	// Initialize Prometheus metrics
	metrics := telemetry.InitMetrics(prometheus.DefaultRegisterer)
	log.Printf("%s Pipeline metrics initialized", LogProm)

	// Create custom metrics
	stats := &ResponderStats{
		requestCounter: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "responder_requests_received_total",
			Help: "Total number of ping requests received",
		}),
		responseCounter: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "responder_responses_sent_total",
			Help: "Total number of pong responses sent",
		}),
		errorCounter: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "responder_errors_total",
			Help: "Total number of responder errors",
		}),
		pipelineHistogram: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "responder_pipeline_duration_seconds",
			Help: "Time for Pipeline event processing",
			Buckets: []float64{
				0.000001, 0.000002, 0.000005, 0.00001, 0.00002, 0.00005,
				0.0001, 0.0002, 0.0005, 0.001, 0.002, 0.005, 0.01, 0.02, 0.05,
			},
		}),
		responseHistogram: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "responder_response_duration_seconds",
			Help: "Time to send response back",
			Buckets: []float64{
				0.0001, 0.0002, 0.0005, 0.001, 0.002, 0.005,
				0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1.0,
			},
		}),
	}

	prometheus.MustRegister(stats.requestCounter, stats.responseCounter, stats.errorCounter,
		stats.pipelineHistogram, stats.responseHistogram)
	log.Printf("%s Responder metrics registered", LogProm)

	// Initialize min values to max uint64
	stats.minPipelineTime.Store(^uint64(0))
	stats.minResponseTime.Store(^uint64(0))

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
				log.Printf("%s Invalid payload type", LogRecv)
				continue
			}

			stats.requestsRecv.Add(1)
			stats.requestCounter.Inc()

			requestID := payload.Headers["X-Request-ID"]
			log.Printf("%s 📥 Received PING: %s", LogRecv, requestID)

			pipelineDuration := time.Since(pipelineStart)
			pipelineNs := uint64(pipelineDuration.Nanoseconds())
			stats.totalPipelineTime.Add(pipelineNs)
			updateMin(&stats.minPipelineTime, pipelineNs)
			updateMax(&stats.maxPipelineTime, pipelineNs)
			stats.pipelineHistogram.Observe(pipelineDuration.Seconds())

			// Send PONG response back asynchronously
			go func(p nethttp.HTTPRequestPayload, reqID string) {
				responseStart := time.Now()

				pongPayload := fmt.Sprintf("PONG response to %s from Responder at %s",
					reqID, time.Now().Format(time.RFC3339Nano))

				log.Printf("%s ← Sending PONG to Container B: %s", LogSend, reqID)

				err := sendResponse(replyTarget, pongPayload, reqID)

				responseDuration := time.Since(responseStart)
				responseNs := uint64(responseDuration.Nanoseconds())
				stats.totalResponseTime.Add(responseNs)
				updateMin(&stats.minResponseTime, responseNs)
				updateMax(&stats.maxResponseTime, responseNs)
				stats.responseHistogram.Observe(responseDuration.Seconds())

				if err != nil {
					log.Printf("%s ❌ Response error: %v", LogSend, err)
					stats.errors.Add(1)
					stats.errorCounter.Inc()
				} else {
					stats.responsesSent.Add(1)
					stats.responseCounter.Inc()
					log.Printf("%s ✅ PONG sent in %v", LogSend, responseDuration)
				}
			}(payload, requestID)
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

func sendResponse(target, payload, requestID string) error {
	req, err := http.NewRequest("POST", target+"/api/pong", bytes.NewBufferString(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("X-Request-ID", requestID)
	req.Header.Set("X-Response-Timestamp", fmt.Sprintf("%d", time.Now().UnixNano()))

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	return nil
}

func printStats(stats *ResponderStats) {
	recv := stats.requestsRecv.Load()
	sent := stats.responsesSent.Load()
	errs := stats.errors.Load()

	var avgPipeline, avgResponse time.Duration
	if recv > 0 {
		avgPipeline = time.Duration(stats.totalPipelineTime.Load() / recv)
	}
	if sent > 0 {
		avgResponse = time.Duration(stats.totalResponseTime.Load() / sent)
	}
	minPipeline := time.Duration(stats.minPipelineTime.Load())
	maxPipeline := time.Duration(stats.maxPipelineTime.Load())
	minResponse := time.Duration(stats.minResponseTime.Load())
	maxResponse := time.Duration(stats.maxResponseTime.Load())

	// Get memory stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	log.Printf("═══════════════════════════════════════════════════════")
	log.Printf("📊 RESPONDER STATS (Container C)")
	log.Printf("═══════════════════════════════════════════════════════")
	log.Printf("Requests Recv:     %d", recv)
	log.Printf("Responses Sent:    %d", sent)
	log.Printf("Errors:            %d", errs)
	log.Printf("Success Rate:      %.2f%%", float64(sent)/float64(recv)*100)
	log.Printf("─────────────────────────────────────────────────────")
	log.Printf("Pipeline Avg:      %v", avgPipeline)
	log.Printf("Pipeline Min:      %v", minPipeline)
	log.Printf("Pipeline Max:      %v", maxPipeline)
	log.Printf("─────────────────────────────────────────────────────")
	log.Printf("Response Avg:      %v", avgResponse)
	log.Printf("Response Min:      %v", minResponse)
	log.Printf("Response Max:      %v", maxResponse)
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
