package main

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var httpClient = &http.Client{
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
	},
	Timeout: 10 * time.Second,
}

type Stats struct {
	sent   atomic.Uint64
	recv   atomic.Uint64
	errors atomic.Uint64
}

type PayloadConfig struct {
	startSize   int
	maxSize     int
	increment   int
	loop        bool
	currentSize atomic.Int64
	useRamp     bool
	rampSteps   int64
	rangeBytes  int
	target      string
	gauge       prometheus.Gauge
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	// Configuration
	targets := parseCSV(getEnv("TARGETS", ""))
	if len(targets) == 0 {
		targets = []string{getEnv("TARGET", "http://192.168.64.6:8080")}
	}
	interval := getDuration("INTERVAL", 3*time.Second)
	metricsAddr := getEnv("METRICS_ADDR", ":9090")

	// Payload size configuration
	startSize := getEnvInt("PAYLOAD_START", 1024)              // 1 KB
	maxSize := getEnvInt("PAYLOAD_MAX", 104857600)             // 100 MB default
	increment := getEnvInt("PAYLOAD_INCREMENT", 1024)          // 1 KB per request (fallback)
	loopPayload := getEnv("PAYLOAD_LOOP", "false") == "true"   // Loop back to start when hitting max
	rampDuration := getDuration("PAYLOAD_DURATION", time.Hour) // Time to ramp start -> max

	log.Printf("🚀 RELAY INITIATOR")
	log.Printf("   Targets: %s", strings.Join(targets, ", "))
	log.Printf("   Interval: %s", interval)
	log.Printf("   Metrics: %s", metricsAddr)

	stats := &Stats{}

	payloadSizeGauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "relay_request_payload_bytes",
		Help: "Current payload size being sent",
	}, []string{"target"})
	prometheus.MustRegister(payloadSizeGauge)

	configs := make([]*PayloadConfig, 0, len(targets))
	for _, tgt := range targets {
		cfg := &PayloadConfig{
			startSize:  startSize,
			maxSize:    maxSize,
			increment:  increment,
			loop:       loopPayload,
			rangeBytes: maxSize - startSize,
			target:     tgt,
			gauge:      payloadSizeGauge.WithLabelValues(tgt),
		}
		if cfg.rangeBytes < 0 {
			cfg.rangeBytes = 0
		}
		cfg.currentSize.Store(int64(startSize))
		cfg.gauge.Set(float64(startSize))

		if rampDuration > 0 && interval > 0 {
			steps := int64(rampDuration / interval)
			if rampDuration%interval != 0 {
				steps++
			}
			if steps < 1 {
				steps = 1
			}
			cfg.useRamp = true
			cfg.rampSteps = steps
			cfg.loop = false
		}

		configs = append(configs, cfg)
	}

	if len(configs) == 0 {
		log.Fatal("no targets configured")
	}

	if configs[0].useRamp {
		log.Printf("   Payload ramp: %s → %s over %s", formatBytes(int64(startSize)), formatBytes(int64(maxSize)), rampDuration)
		log.Printf("   Ramp steps: %d (interval: %s)", configs[0].rampSteps, interval)
	} else {
		log.Printf("   Payload: %d bytes → %d bytes (increment: %d) [Loop: %v]", startSize, maxSize, increment, loopPayload)
	}

	// Start Prometheus metrics server
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		log.Printf("📊 Prometheus metrics: http://localhost%s/metrics", metricsAddr)
		if err := http.ListenAndServe(metricsAddr, mux); err != nil {
			log.Fatalf("Metrics server error: %v", err)
		}
	}()

	// Stats logger
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			sent := stats.sent.Load()
			recv := stats.recv.Load()
			errs := stats.errors.Load()
			var successRate float64
			if sent > 0 {
				successRate = float64(recv) / float64(sent) * 100
			}
			log.Printf("📊 Stats: Sent=%d Recv=%d Err=%d (%.1f%% success)", sent, recv, errs, successRate)
			for _, cfg := range configs {
				payloadSize := cfg.currentSize.Load()
				log.Printf("    ↳ %s payload=%s", cfg.target, formatBytes(payloadSize))
			}
		}
	}()

	// Send requests
	for _, cfg := range configs {
		cfg := cfg
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			requestNum := 0
			for range ticker.C {
				requestNum++

				// Determine current payload size
				currentSize := int(cfg.currentSize.Load())
				if cfg.useRamp {
					stepIndex := int64(requestNum - 1)
					if stepIndex >= cfg.rampSteps {
						currentSize = cfg.maxSize
					} else {
						fraction := float64(stepIndex) / float64(cfg.rampSteps)
						currentSize = cfg.startSize + int(float64(cfg.rangeBytes)*fraction)
					}
				}

				if err := sendRequest(cfg.target, requestNum, currentSize, stats); err != nil {
					log.Printf("❌ [%s] Request #%d failed: %v", cfg.target, requestNum, err)
					stats.errors.Add(1)
				}

				// Determine next payload size
				var nextSize int
				if cfg.useRamp {
					stepIndex := int64(requestNum)
					if stepIndex >= cfg.rampSteps {
						nextSize = cfg.maxSize
					} else {
						fraction := float64(stepIndex) / float64(cfg.rampSteps)
						nextSize = cfg.startSize + int(float64(cfg.rangeBytes)*fraction)
					}
				} else {
					nextSize = currentSize + cfg.increment
					if nextSize > cfg.maxSize {
						if cfg.loop {
							nextSize = cfg.startSize
							log.Printf("🔄 [%s] Payload reached max (%s), looping back to %s",
								cfg.target,
								formatBytes(int64(cfg.maxSize)),
								formatBytes(int64(cfg.startSize)))
						} else {
							nextSize = cfg.maxSize
						}
					}
				}

				cfg.currentSize.Store(int64(nextSize))
				cfg.gauge.Set(float64(nextSize))
			}
		}()
	}

	// Wait for shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	log.Printf("🛑 Shutting down...")
	log.Printf("📊 Final: Sent=%d Recv=%d Err=%d",
		stats.sent.Load(), stats.recv.Load(), stats.errors.Load())
	for _, cfg := range configs {
		log.Printf("    ↳ %s final payload=%s", cfg.target, formatBytes(cfg.currentSize.Load()))
	}
}

func sendRequest(target string, num int, payloadSize int, stats *Stats) error {
	// Generate payload with header + random data
	header := fmt.Sprintf("Request #%d at %s | Size: %d bytes\n",
		num, time.Now().Format(time.RFC3339), payloadSize)

	// Calculate how much random data we need
	randomSize := payloadSize - len(header)
	if randomSize < 0 {
		randomSize = 0
	}

	// Generate random payload data
	randomData := make([]byte, randomSize)
	if randomSize > 0 {
		if _, err := rand.Read(randomData); err != nil {
			return fmt.Errorf("failed to generate random payload: %w", err)
		}
	}

	// Combine header + random data
	payload := append([]byte(header), randomData...)

	log.Printf("📤 [%s] Sending request #%d (%s payload)", target, num, formatBytes(int64(payloadSize)))

	req, err := http.NewRequest("POST", target+"/api/test", bytes.NewBuffer(payload))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Request-ID", fmt.Sprintf("req-%d", num))
	req.Header.Set("X-Payload-Size", fmt.Sprintf("%d", payloadSize))

	stats.sent.Add(1)

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	stats.recv.Add(1)

	log.Printf("✅ [%s] Response #%d: %s (hop %s)",
		target, num, truncate(string(body), 60), resp.Header.Get("X-Hop-Count"))

	return nil
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

func parseCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
