package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/BYTE-6D65/pipeline/pkg/engine"
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

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	target := os.Getenv("TARGET")
	if target == "" {
		target = "http://192.168.64.6:8080"
	}

	interval := 3 * time.Second
	if intervalStr := os.Getenv("INTERVAL"); intervalStr != "" {
		if d, err := time.ParseDuration(intervalStr); err == nil {
			interval = d
		}
	}

	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println("🚀 HTTP RELAY INITIATOR")
	fmt.Println("═══════════════════════════════════════════════════════")
	log.Printf("[INIT] Target: %s", target)
	log.Printf("[INIT] Interval: %s", interval)
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println()

	// Create pipeline engine (not used but shows we're part of the ecosystem)
	eng := engine.New()
	defer eng.Shutdown(context.Background())

	requestNum := 0
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Send first request immediately
	requestNum++
	sendRequest(target, requestNum)

	// Then send on interval
	for range ticker.C {
		requestNum++
		sendRequest(target, requestNum)
	}
}

func sendRequest(target string, num int) {
	startTime := time.Now()

	payload := fmt.Sprintf("Initiator request #%d at %s", num, time.Now().Format(time.RFC3339))

	log.Printf("[SEND] 📤 Sending request #%d", num)
	log.Printf("[SEND]   Target: %s/api/test", target)
	log.Printf("[SEND]   Payload: %s", payload)

	req, err := http.NewRequest("POST", target+"/api/test", bytes.NewBufferString(payload))
	if err != nil {
		log.Printf("[SEND] ❌ Failed to create request: %v", err)
		return
	}

	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("X-Initiator", "relay-test")

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("[SEND] ❌ Request failed: %v", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[SEND] ⚠️  Failed to read response: %v", err)
		return
	}

	duration := time.Since(startTime)

	log.Printf("[RECV] ✅ Response received in %v", duration)
	log.Printf("[RECV]   Status: %d", resp.StatusCode)
	log.Printf("[RECV]   Hop count: %s", resp.Header.Get("X-Hop-Count"))
	log.Printf("[RECV]   Relay node: %s", resp.Header.Get("X-Relay-Node"))
	log.Printf("[RECV]   Body: %s", truncate(string(body), 100))
	log.Printf("[SEND] ────────────────────────────────────────")
	fmt.Println()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
