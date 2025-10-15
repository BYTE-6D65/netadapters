package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
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

var relayClient = &http.Client{
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	},
	Timeout: 10 * time.Second,
}

type AdapterStats struct {
	received  atomic.Uint64
	forwarded atomic.Uint64
	dropped   atomic.Uint64
	errors    atomic.Uint64
}

type Stats struct {
	received  atomic.Uint64
	forwarded atomic.Uint64
	dropped   atomic.Uint64
	errors    atomic.Uint64
}

type adapterRoute struct {
	id         string
	listenAddr string
	nextHop    string
	stats      *AdapterStats
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	adapterPorts := parseCSV(getEnv("ADAPTER_PORTS", ""))
	if len(adapterPorts) == 0 {
		adapterPorts = []string{getEnv("LISTEN_ADDR", ":8080")}
	}

	defaultNextHop := getEnv("NEXT_HOP", "")
	if defaultNextHop == "" {
		log.Fatal("NEXT_HOP environment variable required")
	}

	rawNextHops := parseCSV(getEnv("NEXT_HOPS", ""))
	nextHopList := make([]string, len(adapterPorts))
	for i := range adapterPorts {
		if i < len(rawNextHops) && strings.TrimSpace(rawNextHops[i]) != "" {
			nextHopList[i] = strings.TrimSpace(rawNextHops[i])
		} else {
			nextHopList[i] = defaultNextHop
		}
	}

	maxHops := getEnvInt("MAX_HOPS", 10)
	workerCount := getEnvInt("WORKER_COUNT", len(adapterPorts))
	if workerCount < 1 {
		workerCount = 1
	}
	nodeName := getEnv("NODE_NAME", "pipeline-node")
	metricsAddr := getEnv("METRICS_ADDR", ":9090")

	log.Printf("🔄 RELAY NODE: %s", nodeName)
	log.Printf("   Adapters: %s", strings.Join(adapterPorts, ", "))
	log.Printf("   Next Hops: %s", strings.Join(nextHopList, ", "))
	log.Printf("   Workers: %d", workerCount)
	log.Printf("   Max Hops: %d", maxHops)
	log.Printf("   Metrics: %s", metricsAddr)

	metrics := telemetry.InitMetrics(prometheus.DefaultRegisterer)
	log.Printf("✅ Pipeline telemetry initialized")

	relayReceived := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "relay_requests_received_total",
		Help: "Total requests received",
	}, []string{"adapter"})
	relayForwarded := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "relay_requests_forwarded_total",
		Help: "Total requests forwarded",
	}, []string{"adapter"})
	relayDropped := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "relay_requests_dropped_total",
		Help: "Total requests dropped (max hops)",
	}, []string{"adapter"})
	relayErrors := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "relay_errors_total",
		Help: "Total relay errors",
	}, []string{"adapter"})

	httpEgressDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "relay_http_egress_duration_seconds",
		Help:    "Time spent in HTTP client Do() (socket write + network)",
		Buckets: []float64{0.0001, 0.0002, 0.0005, 0.001, 0.002, 0.005, 0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1.0},
	}, []string{"adapter"})

	payloadSizeGauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "relay_request_payload_bytes",
		Help: "Current request payload size in bytes",
	}, []string{"adapter"})

	prometheus.MustRegister(relayReceived, relayForwarded, relayDropped, relayErrors, httpEgressDuration, payloadSizeGauge)

	totalStats := &Stats{}
	adapterRoutes := make(map[string]*adapterRoute, len(adapterPorts))

	eng := engine.New(
		engine.WithInternalBus(event.NewInMemoryBus(
			event.WithBufferSize(8),
			event.WithBusName("internal"),
			event.WithMetrics(metrics),
		)),
		engine.WithExternalBus(event.NewInMemoryBus(
			event.WithBufferSize(8),
			event.WithBusName("external"),
			event.WithMetrics(metrics),
		)),
	)
	defer eng.Shutdown(context.Background())
	log.Printf("✅ Pipeline engine created")

	adapterMgr := engine.NewAdapterManager(eng)
	defer adapterMgr.Shutdown()

	routesInOrder := make([]*adapterRoute, 0, len(adapterPorts))
	for i, port := range adapterPorts {
		port = strings.TrimSpace(port)
		if port == "" {
			continue
		}

		srv := nethttp.NewServerAdapter(port)
		if err := adapterMgr.Register(srv); err != nil {
			log.Fatalf("Failed to register adapter %s: %v", port, err)
		}

		route := &adapterRoute{
			id:         srv.ID(),
			listenAddr: port,
			nextHop:    nextHopList[i],
			stats:      &AdapterStats{},
		}
		adapterRoutes[route.id] = route
		routesInOrder = append(routesInOrder, route)

		relayReceived.WithLabelValues(route.id).Add(0)
		relayForwarded.WithLabelValues(route.id).Add(0)
		relayDropped.WithLabelValues(route.id).Add(0)
		relayErrors.WithLabelValues(route.id).Add(0)
		payloadSizeGauge.WithLabelValues(route.id).Set(0)
	}

	if len(routesInOrder) == 0 {
		log.Fatal("no adapters configured")
	}

	if err := adapterMgr.Start(); err != nil {
		log.Fatalf("Failed to start adapters: %v", err)
	}
	for _, route := range routesInOrder {
		log.Printf("✅ Adapter %s listening on %s → %s", route.id, route.listenAddr, route.nextHop)
	}

	emitterMgr := engine.NewEmitterManager(eng)
	defer emitterMgr.Shutdown()

	httpClient := nethttp.NewClientEmitter()
	if err := emitterMgr.Register("http-client", httpClient, event.Filter{Types: []string{"net.http.response"}}); err != nil {
		log.Fatalf("Failed to register emitter: %v", err)
	}
	if err := emitterMgr.Start(); err != nil {
		log.Fatalf("Failed to start emitters: %v", err)
	}
	log.Printf("✅ HTTP emitter started")

	sub, err := eng.ExternalBus().Subscribe(context.Background(), event.Filter{Types: []string{"net.http.request"}})
	if err != nil {
		log.Fatalf("Failed to subscribe: %v", err)
	}
	defer sub.Close()
	log.Printf("✅ Subscribed to requests (workers=%d)", workerCount)

	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		log.Printf("📊 Prometheus metrics available at http://%s%s/metrics", nodeName, metricsAddr)
		if err := http.ListenAndServe(metricsAddr, mux); err != nil {
			log.Fatalf("Metrics server error: %v", err)
		}
	}()

	codec := event.JSONCodec{}
	var workerWG sync.WaitGroup

	processEvent := func(evt *event.Event) {
		adapterID := evt.Metadata["adapter_id"]
		route, ok := adapterRoutes[adapterID]
		if !ok {
			route = routesInOrder[0]
		}

		totalStats.received.Add(1)
		route.stats.received.Add(1)
		relayReceived.WithLabelValues(route.id).Inc()

		var payload nethttp.HTTPRequestPayload
		if err := evt.DecodePayload(&payload, codec); err != nil {
			log.Printf("❌ Decode error (%s): %v", route.id, err)
			totalStats.errors.Add(1)
			route.stats.errors.Add(1)
			relayErrors.WithLabelValues(route.id).Inc()
			return
		}

		payloadSize := len(payload.Body)
		if sizeStr, ok := payload.Headers["X-Payload-Size"]; ok {
			if v, err := strconv.ParseFloat(sizeStr, 64); err == nil {
				payloadSize = int(v)
			}
		}
		payloadSizeGauge.WithLabelValues(route.id).Set(float64(payloadSize))

		hopCount := 1
		if hopHeader, ok := payload.Headers["X-Hop-Count"]; ok {
			if h, err := strconv.Atoi(hopHeader); err == nil {
				hopCount = h + 1
			}
		}

		log.Printf("📨 [%s] Request %s hop %d size=%dB", route.id, payload.RequestID, hopCount, payloadSize)

		if hopCount > maxHops {
			log.Printf("⚠️  [%s] Max hops exceeded, dropping", route.id)
			totalStats.dropped.Add(1)
			route.stats.dropped.Add(1)
			relayDropped.WithLabelValues(route.id).Inc()

			respPayload := nethttp.HTTPResponsePayload{
				RequestID:  payload.RequestID,
				StatusCode: 200,
				Headers:    map[string]string{"Content-Type": "text/plain"},
				Body:       []byte(fmt.Sprintf("Max hops reached at %s", nodeName)),
				Timestamp:  time.Now(),
			}
			respEvt, _ := event.NewEvent("net.http.response", nodeName, respPayload, codec)
			respEvt.WithMetadata("request_id", payload.RequestID)
			eng.ExternalBus().Publish(context.Background(), respEvt)
			return
		}

		go func(p *nethttp.HTTPRequestPayload, hc int, r *adapterRoute) {
			observer := httpEgressDuration.WithLabelValues(r.id)
			if err := forwardRequest(r.nextHop, p, hc, nodeName, observer); err != nil {
				log.Printf("❌ Forward error [%s]: %v", r.id, err)
				totalStats.errors.Add(1)
				r.stats.errors.Add(1)
				relayErrors.WithLabelValues(r.id).Inc()
			} else {
				totalStats.forwarded.Add(1)
				r.stats.forwarded.Add(1)
				relayForwarded.WithLabelValues(r.id).Inc()
			}
		}(&payload, hopCount, route)

		respPayload := nethttp.HTTPResponsePayload{
			RequestID:  payload.RequestID,
			StatusCode: 200,
			Headers: map[string]string{
				"Content-Type": "text/plain",
				"X-Relay-Node": nodeName,
				"X-Hop-Count":  strconv.Itoa(hopCount),
			},
			Body:      []byte(fmt.Sprintf("Relayed by %s (hop %d)", nodeName, hopCount)),
			Timestamp: time.Now(),
		}

		respEvt, err := event.NewEvent("net.http.response", nodeName, respPayload, codec)
		if err != nil {
			log.Printf("❌ Response event error [%s]: %v", route.id, err)
			return
		}
		respEvt.WithMetadata("request_id", payload.RequestID)
		eng.ExternalBus().Publish(context.Background(), respEvt)
	}

	for i := 0; i < workerCount; i++ {
		workerWG.Add(1)
		go func(workerID int) {
			defer workerWG.Done()
			for evt := range sub.Events() {
				processEvent(evt)
			}
		}(i)
	}

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			log.Printf("📊 Totals: Recv=%d Fwd=%d Drop=%d Err=%d",
				totalStats.received.Load(), totalStats.forwarded.Load(),
				totalStats.dropped.Load(), totalStats.errors.Load())
			for _, route := range routesInOrder {
				log.Printf("    ↳ %s (%s→%s) Recv=%d Fwd=%d Drop=%d Err=%d",
					route.id, route.listenAddr, route.nextHop,
					route.stats.received.Load(), route.stats.forwarded.Load(),
					route.stats.dropped.Load(), route.stats.errors.Load())
			}
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	log.Printf("🛑 Shutting down...")
	sub.Close()
	workerWG.Wait()
	adapterMgr.Shutdown()
	emitterMgr.Shutdown()

	log.Printf("📊 Final Totals: Recv=%d Fwd=%d Drop=%d Err=%d",
		totalStats.received.Load(), totalStats.forwarded.Load(),
		totalStats.dropped.Load(), totalStats.errors.Load())
	for _, route := range routesInOrder {
		log.Printf("    ↳ %s (%s→%s) Recv=%d Fwd=%d Drop=%d Err=%d",
			route.id, route.listenAddr, route.nextHop,
			route.stats.received.Load(), route.stats.forwarded.Load(),
			route.stats.dropped.Load(), route.stats.errors.Load())
	}
}

func forwardRequest(nextHop string, payload *nethttp.HTTPRequestPayload, hopCount int, nodeName string, observer prometheus.Observer) error {
	// Create prefix without copying the entire body
	prefix := []byte(fmt.Sprintf("[%s→hop%d] ", nodeName, hopCount))

	// Use io.MultiReader to concatenate prefix + body without copying
	// This creates a reader that reads prefix first, then body, with zero copies
	bodyReader := io.MultiReader(bytes.NewReader(prefix), bytes.NewReader(payload.Body))

	req, err := http.NewRequest("POST", nextHop+payload.Path, bodyReader)
	if err != nil {
		return err
	}

	for k, v := range payload.Headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("X-Hop-Count", strconv.Itoa(hopCount))
	req.Header.Set("X-Relay-Node", nodeName)

	start := time.Now()
	resp, err := relayClient.Do(req)
	if err != nil {
		observer.Observe(time.Since(start).Seconds())
		return err
	}
	defer resp.Body.Close()

	io.Copy(io.Discard, resp.Body)
	observer.Observe(time.Since(start).Seconds())
	return nil
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
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
