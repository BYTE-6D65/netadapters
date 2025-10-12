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
	"github.com/BYTE-6D65/pipeline/pkg/event"
	nethttp "github.com/BYTE-6D65/netadapters/pkg/http"
)

func main() {
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

	fmt.Printf("HTTP Client starting\n")
	fmt.Printf("Target: %s\n", target)
	fmt.Printf("Interval: %s\n", interval)
	fmt.Println("---")

	// Create pipeline engine
	eng := engine.New()
	defer eng.Shutdown(context.Background())

	// Request counter
	requestNum := 0

	// Send requests in a loop
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			requestNum++
			sendRequest(target, requestNum)
		}
	}
}

func sendRequest(target string, num int) {
	// Create request payload
	payload := fmt.Sprintf("Request #%d from client at %s", num, time.Now().Format(time.RFC3339))

	// Send HTTP POST request
	resp, err := http.Post(
		target+"/api/test",
		"text/plain",
		bytes.NewBufferString(payload),
	)
	if err != nil {
		log.Printf("❌ Request #%d failed: %v", num, err)
		return
	}
	defer resp.Body.Close()

	// Read response
	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	responseBody := buf.String()

	// Log result
	if resp.StatusCode == http.StatusOK {
		fmt.Printf("✅ Request #%d: %d bytes sent, %d bytes received\n",
			num, len(payload), len(responseBody))
		fmt.Printf("   Response preview: %s...\n", truncate(responseBody, 80))
	} else {
		log.Printf("⚠️  Request #%d: status %d\n", num, resp.StatusCode)
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// Example of how to use Pipeline for outbound HTTP requests
// This demonstrates that Pipeline can be used for client-side networking too
func exampleWithPipeline(target string) {
	eng := engine.New()
	defer eng.Shutdown(context.Background())

	// Create HTTP request payload
	payload := nethttp.HTTPRequestPayload{
		Method: "POST",
		Path:   "/api/test",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body:      []byte(`{"message":"Hello from Pipeline client"}`),
		Timestamp: time.Now(),
	}

	// Create event
	codec := event.JSONCodec{}
	evt, err := event.NewEvent("net.http.request", "client", payload, codec)
	if err != nil {
		log.Printf("Failed to create event: %v", err)
		return
	}

	// In a real implementation, you'd have an HTTP client emitter
	// that consumes these events and makes actual HTTP requests
	fmt.Printf("Created request event: %s\n", evt.ID)
}
