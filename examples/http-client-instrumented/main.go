package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/BYTE-6D65/pipeline/pkg/engine"
)

// Logger prefixes
const (
	LogEngine   = "[ENGINE]"
	LogTransmit = "[TRANSMIT]"
	LogReceive  = "[RECEIVE]"
	LogNetwork  = "[NETWORK]"
	LogStats    = "[STATS]"
)

type RequestStats struct {
	TotalRequests     int
	SuccessfulReqs    int
	FailedReqs        int
	TotalBytesSent    int64
	TotalBytesRecv    int64
	TotalRoundTripTime time.Duration
	MinRoundTrip      time.Duration
	MaxRoundTrip      time.Duration
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	// Get target server from environment
	target := os.Getenv("TARGET_SERVER")
	if target == "" {
		target = "http://localhost:8080"
	}

	// Get interval from environment (default 2 seconds)
	interval := 2 * time.Second
	if intervalStr := os.Getenv("INTERVAL"); intervalStr != "" {
		if d, err := time.ParseDuration(intervalStr); err == nil {
			interval = d
		}
	}

	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println("🔬 INSTRUMENTED HTTP CLIENT")
	fmt.Println("═══════════════════════════════════════════════════════")
	log.Printf("%s Starting Pipeline engine", LogEngine)

	// Create pipeline engine
	eng := engine.New()
	defer func() {
		log.Printf("%s Shutting down Pipeline engine", LogEngine)
		eng.Shutdown(context.Background())
	}()

	log.Printf("%s Engine created successfully", LogEngine)
	log.Printf("%s Target server: %s", LogNetwork, target)
	log.Printf("%s Request interval: %s", LogNetwork, interval)

	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println("✅ Client ready - starting request loop")
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println()

	// Statistics
	stats := &RequestStats{
		MinRoundTrip: time.Hour, // Initialize to high value
	}

	// Request counter
	requestNum := 0

	// Send requests in a loop
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Send first request immediately
	requestNum++
	sendRequest(target, requestNum, stats)

	// Print stats every 10 requests
	statsTicker := time.NewTicker(10 * interval)
	defer statsTicker.Stop()

	for {
		select {
		case <-ticker.C:
			requestNum++
			sendRequest(target, requestNum, stats)

		case <-statsTicker.C:
			printStats(stats)
		}
	}
}

func sendRequest(target string, num int, stats *RequestStats) {
	log.Printf("%s ════════════════════════════════════════", LogTransmit)
	log.Printf("%s 📤 Initiating request #%d", LogTransmit, num)

	// Create request payload
	timestamp := time.Now()
	payload := fmt.Sprintf("Request #%d from client at %s", num, timestamp.Format(time.RFC3339))

	log.Printf("%s   Timestamp: %s", LogTransmit, timestamp.Format(time.RFC3339Nano))
	log.Printf("%s   Payload size: %d bytes", LogTransmit, len(payload))
	log.Printf("%s   Payload: %s", LogTransmit, payload)
	log.Printf("%s   Target: %s/api/test", LogTransmit, target)

	// Track timing
	startTime := time.Now()

	log.Printf("%s Establishing HTTP connection", LogNetwork)

	// Send HTTP POST request
	connectStart := time.Now()
	resp, err := http.Post(
		target+"/api/test",
		"text/plain",
		bytes.NewBufferString(payload),
	)
	connectDuration := time.Since(connectStart)

	if err != nil {
		log.Printf("%s ❌ Request #%d failed", LogTransmit, num)
		log.Printf("%s   Error: %v", LogTransmit, err)
		log.Printf("%s   Duration: %v", LogTransmit, connectDuration)
		stats.TotalRequests++
		stats.FailedReqs++
		fmt.Printf("\n❌ REQUEST #%d FAILED: %v\n\n", num, err)
		return
	}
	defer resp.Body.Close()

	log.Printf("%s ✅ HTTP connection established in %v", LogNetwork, connectDuration)
	log.Printf("%s   Status code: %d %s", LogNetwork, resp.StatusCode, http.StatusText(resp.StatusCode))
	log.Printf("%s   Response headers:", LogNetwork)
	for k, v := range resp.Header {
		if len(v) > 0 {
			log.Printf("%s     %s: %s", LogNetwork, k, v[0])
		}
	}

	// Read response
	log.Printf("%s Reading response body", LogReceive)
	readStart := time.Now()
	buf := new(bytes.Buffer)
	bytesRead, err := buf.ReadFrom(resp.Body)
	readDuration := time.Since(readStart)

	if err != nil {
		log.Printf("%s ⚠️  Error reading response: %v", LogReceive, err)
		stats.TotalRequests++
		stats.FailedReqs++
		return
	}

	responseBody := buf.String()
	roundTripTime := time.Since(startTime)

	log.Printf("%s ✅ Response body read in %v", LogReceive, readDuration)
	log.Printf("%s   Bytes received: %d", LogReceive, bytesRead)
	log.Printf("%s   Response preview: %s", LogReceive, truncate(responseBody, 80))

	// Update statistics
	stats.TotalRequests++
	stats.SuccessfulReqs++
	stats.TotalBytesSent += int64(len(payload))
	stats.TotalBytesRecv += bytesRead
	stats.TotalRoundTripTime += roundTripTime

	if roundTripTime < stats.MinRoundTrip {
		stats.MinRoundTrip = roundTripTime
	}
	if roundTripTime > stats.MaxRoundTrip {
		stats.MaxRoundTrip = roundTripTime
	}

	// Log timing breakdown
	log.Printf("%s ⏱️  Timing breakdown:", LogStats)
	log.Printf("%s   Connection: %v", LogStats, connectDuration)
	log.Printf("%s   Body read:  %v", LogStats, readDuration)
	log.Printf("%s   Total RTT:  %v", LogStats, roundTripTime)
	log.Printf("%s ════════════════════════════════════════", LogTransmit)

	// Calculate success rate
	successRate := float64(stats.SuccessfulReqs) / float64(stats.TotalRequests) * 100
	avgRTT := stats.TotalRoundTripTime / time.Duration(stats.TotalRequests)

	fmt.Printf("✅ REQUEST #%d: sent %d bytes, received %d bytes, RTT %v (avg: %v, success: %.1f%%)\n\n",
		num, len(payload), bytesRead, roundTripTime, avgRTT, successRate)
}

func printStats(stats *RequestStats) {
	if stats.TotalRequests == 0 {
		return
	}

	fmt.Println()
	log.Printf("%s ═══════════════════════════════════════════", LogStats)
	log.Printf("%s 📊 CUMULATIVE STATISTICS", LogStats)
	log.Printf("%s ═══════════════════════════════════════════", LogStats)
	log.Printf("%s Total requests:     %d", LogStats, stats.TotalRequests)
	log.Printf("%s Successful:         %d (%.1f%%)", LogStats,
		stats.SuccessfulReqs,
		float64(stats.SuccessfulReqs)/float64(stats.TotalRequests)*100)
	log.Printf("%s Failed:             %d (%.1f%%)", LogStats,
		stats.FailedReqs,
		float64(stats.FailedReqs)/float64(stats.TotalRequests)*100)
	log.Printf("%s ───────────────────────────────────────────", LogStats)
	log.Printf("%s Total bytes sent:   %d (%.2f KB)", LogStats,
		stats.TotalBytesSent,
		float64(stats.TotalBytesSent)/1024)
	log.Printf("%s Total bytes recv:   %d (%.2f KB)", LogStats,
		stats.TotalBytesRecv,
		float64(stats.TotalBytesRecv)/1024)
	log.Printf("%s ───────────────────────────────────────────", LogStats)

	if stats.TotalRequests > 0 {
		avgRTT := stats.TotalRoundTripTime / time.Duration(stats.TotalRequests)
		log.Printf("%s Avg round-trip:     %v", LogStats, avgRTT)
		log.Printf("%s Min round-trip:     %v", LogStats, stats.MinRoundTrip)
		log.Printf("%s Max round-trip:     %v", LogStats, stats.MaxRoundTrip)

		// Calculate throughput
		throughputSent := float64(stats.TotalBytesSent) / stats.TotalRoundTripTime.Seconds()
		throughputRecv := float64(stats.TotalBytesRecv) / stats.TotalRoundTripTime.Seconds()
		log.Printf("%s ───────────────────────────────────────────", LogStats)
		log.Printf("%s Throughput sent:    %.2f KB/s", LogStats, throughputSent/1024)
		log.Printf("%s Throughput recv:    %.2f KB/s", LogStats, throughputRecv/1024)
		log.Printf("%s Requests/sec:       %.2f", LogStats,
			float64(stats.TotalRequests)/stats.TotalRoundTripTime.Seconds())
	}
	log.Printf("%s ═══════════════════════════════════════════", LogStats)
	fmt.Println()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
