package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/BYTE-6D65/netadapters/pkg/http"
	"github.com/BYTE-6D65/pipeline/pkg/engine"
	"github.com/BYTE-6D65/pipeline/pkg/event"
)

func main() {
	fmt.Println("Starting HTTP Echo Server on :8080")
	fmt.Println("Try: curl -X POST http://localhost:8080/test -d 'Hello, Pipeline!'")
	fmt.Println("Press Ctrl+C to stop")

	// Create pipeline engine
	eng := engine.New()
	defer eng.Shutdown(context.Background())

	// Create HTTP server adapter (receives requests)
	httpServer := http.NewServerAdapter(":8080")

	// Create HTTP client emitter (sends responses)
	httpClient := http.NewClientEmitter()

	// Register with engine
	adapterMgr := engine.NewAdapterManager(eng)
	if err := adapterMgr.Register(httpServer); err != nil {
		log.Fatalf("Failed to register adapter: %v", err)
	}
	if err := adapterMgr.Start(); err != nil {
		log.Fatalf("Failed to start adapters: %v", err)
	}

	emitterMgr := engine.NewEmitterManager(eng)
	if err := emitterMgr.Register("http-client", httpClient, event.Filter{
		Types: []string{"net.http.response"},
	}); err != nil {
		log.Fatalf("Failed to register emitter: %v", err)
	}
	if err := emitterMgr.Start(); err != nil {
		log.Fatalf("Failed to start emitters: %v", err)
	}

	// Subscribe to HTTP requests
	sub, err := eng.ExternalBus().Subscribe(context.Background(), event.Filter{
		Types: []string{"net.http.request"},
	})
	if err != nil {
		log.Fatalf("Failed to subscribe: %v", err)
	}
	defer sub.Close()

	// Echo logic: request → response
	go func() {
		for evt := range sub.Events() {
			// Decode and log request
			codec := event.JSONCodec{}
			var payload http.HTTPRequestPayload
			if err := evt.DecodePayload(&payload, codec); err == nil {
				fmt.Printf("[%s] %s %s from %s\n",
					payload.RequestID[:8],
					payload.Method,
					payload.Path,
					payload.RemoteAddr,
				)
			}

			// Create echo response
			response, err := http.CreateEchoResponse(evt)
			if err != nil {
				log.Printf("Failed to create echo response: %v", err)
				continue
			}
			eng.ExternalBus().Publish(context.Background(), response)
		}
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\nShutting down...")
}
