package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/BYTE-6D65/netadapters/pkg/http"
	"github.com/BYTE-6D65/pipeline/pkg/engine"
	"github.com/BYTE-6D65/pipeline/pkg/event"
)

// Logger prefixes
const (
	LogAdapter  = "[ADAPTER]"
	LogEmitter  = "[EMITTER]"
	LogBus      = "[BUS]"
	LogRequest  = "[REQUEST]"
	LogResponse = "[RESPONSE]"
	LogEngine   = "[ENGINE]"
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println("🔬 INSTRUMENTED HTTP ECHO SERVER")
	fmt.Println("═══════════════════════════════════════════════════════")
	log.Printf("%s Starting Pipeline engine", LogEngine)

	// Create pipeline engine
	eng := engine.New()
	defer func() {
		log.Printf("%s Shutting down Pipeline engine", LogEngine)
		eng.Shutdown(context.Background())
	}()

	log.Printf("%s Engine created successfully", LogEngine)

	// Create HTTP server adapter (receives requests)
	log.Printf("%s Creating HTTP Server Adapter on :8080", LogAdapter)
	httpServer := http.NewServerAdapter(":8080")

	// Create HTTP client emitter (sends responses)
	log.Printf("%s Creating HTTP Client Emitter", LogEmitter)
	httpClient := http.NewClientEmitter()

	// Register adapter
	log.Printf("%s Registering HTTP Server Adapter", LogAdapter)
	adapterMgr := engine.NewAdapterManager(eng)
	if err := adapterMgr.Register(httpServer); err != nil {
		log.Fatalf("%s Failed to register adapter: %v", LogAdapter, err)
	}

	log.Printf("%s Starting adapters", LogAdapter)
	if err := adapterMgr.Start(); err != nil {
		log.Fatalf("%s Failed to start adapters: %v", LogAdapter, err)
	}
	log.Printf("%s ✅ HTTP Server Adapter started and listening", LogAdapter)

	// Register emitter
	log.Printf("%s Registering HTTP Client Emitter", LogEmitter)
	emitterMgr := engine.NewEmitterManager(eng)
	if err := emitterMgr.Register("http-client", httpClient, event.Filter{
		Types: []string{"net.http.response"},
	}); err != nil {
		log.Fatalf("%s Failed to register emitter: %v", LogEmitter, err)
	}

	log.Printf("%s Starting emitters", LogEmitter)
	if err := emitterMgr.Start(); err != nil {
		log.Fatalf("%s Failed to start emitters: %v", LogEmitter, err)
	}
	log.Printf("%s ✅ HTTP Client Emitter started", LogEmitter)

	// Subscribe to HTTP requests
	log.Printf("%s Creating subscription for 'net.http.request' events", LogBus)
	sub, err := eng.ExternalBus().Subscribe(context.Background(), event.Filter{
		Types: []string{"net.http.request"},
	})
	if err != nil {
		log.Fatalf("%s Failed to subscribe: %v", LogBus, err)
	}
	defer sub.Close()
	log.Printf("%s ✅ Subscription created", LogBus)

	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println("✅ Server ready - awaiting requests")
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println()

	// Request counter
	requestCount := 0

	// Echo logic: request → response
	go func() {
		log.Printf("%s Starting event processing loop", LogBus)
		for evt := range sub.Events() {
			requestCount++
			receiveTime := time.Now()

			log.Printf("%s ────────────────────────────────────────", LogBus)
			log.Printf("%s 📨 Received event from bus", LogBus)
			log.Printf("%s   Event ID: %s", LogBus, evt.ID)
			log.Printf("%s   Event Type: %s", LogBus, evt.Type)
			log.Printf("%s   Event Source: %s", LogBus, evt.Source)
			log.Printf("%s   Event Timestamp: %s", LogBus, evt.Timestamp.Format(time.RFC3339Nano))
			log.Printf("%s   Data Size: %d bytes", LogBus, len(evt.Data))

			// Decode and log request
			codec := event.JSONCodec{}
			var payload http.HTTPRequestPayload
			if err := evt.DecodePayload(&payload, codec); err == nil {
				log.Printf("%s ────────────────────────────────────────", LogRequest)
				log.Printf("%s HTTP Request Details:", LogRequest)
				log.Printf("%s   Request ID: %s", LogRequest, payload.RequestID)
				log.Printf("%s   Method: %s", LogRequest, payload.Method)
				log.Printf("%s   Path: %s", LogRequest, payload.Path)
				log.Printf("%s   Remote Address: %s", LogRequest, payload.RemoteAddr)
				log.Printf("%s   Local Address: %s", LogRequest, payload.LocalAddr)
				log.Printf("%s   Body Size: %d bytes", LogRequest, len(payload.Body))
				log.Printf("%s   Body Preview: %s", LogRequest, truncate(string(payload.Body), 50))
				log.Printf("%s   TLS: %v", LogRequest, payload.TLS)
				log.Printf("%s   Headers: %d", LogRequest, len(payload.Headers))
				for k, v := range payload.Headers {
					log.Printf("%s     %s: %s", LogRequest, k, truncate(v, 50))
				}

				fmt.Printf("\n📊 REQUEST #%d: %s %s from %s\n\n",
					requestCount, payload.Method, payload.Path, payload.RemoteAddr)
			} else {
				log.Printf("%s ⚠️  Failed to decode payload: %v", LogRequest, err)
			}

			// Create echo response
			log.Printf("%s Creating echo response", LogResponse)
			startCreate := time.Now()
			response, err := http.CreateEchoResponse(evt)
			createDuration := time.Since(startCreate)

			if err != nil {
				log.Printf("%s ❌ Failed to create echo response: %v", LogResponse, err)
				continue
			}

			log.Printf("%s ✅ Echo response created in %v", LogResponse, createDuration)
			log.Printf("%s   Response Event ID: %s", LogResponse, response.ID)
			log.Printf("%s   Response Data Size: %d bytes", LogResponse, len(response.Data))

			// Publish response to bus
			log.Printf("%s Publishing response event to bus", LogBus)
			startPublish := time.Now()
			if err := eng.ExternalBus().Publish(context.Background(), response); err != nil {
				log.Printf("%s ❌ Failed to publish response: %v", LogBus, err)
				continue
			}
			publishDuration := time.Since(startPublish)
			totalDuration := time.Since(receiveTime)

			log.Printf("%s ✅ Response published in %v", LogBus, publishDuration)
			log.Printf("%s 📊 Total processing time: %v", LogBus, totalDuration)
			log.Printf("%s ────────────────────────────────────────", LogBus)
			fmt.Println()
		}
		log.Printf("%s Event processing loop ended", LogBus)
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	fmt.Println()
	log.Printf("%s Received shutdown signal", LogEngine)
	log.Printf("%s Total requests processed: %d", LogEngine, requestCount)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
