package http

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/BYTE-6D65/pipeline/pkg/clock"
	"github.com/BYTE-6D65/pipeline/pkg/event"
	"github.com/google/uuid"
)

// ServerAdapter listens for HTTP requests and publishes them as events
type ServerAdapter struct {
	id     string
	addr   string
	server *http.Server
	bus    event.Bus
	clk    clock.Clock

	mu      sync.Mutex
	running bool
}

// NewServerAdapter creates a new HTTP server adapter
func NewServerAdapter(addr string) *ServerAdapter {
	return &ServerAdapter{
		id:   fmt.Sprintf("http-server-%s", addr),
		addr: addr,
	}
}

// ID returns the adapter's unique identifier
func (a *ServerAdapter) ID() string {
	return a.id
}

// Type returns the adapter type
func (a *ServerAdapter) Type() string {
	return "http-server"
}

// Start begins listening for HTTP requests
func (a *ServerAdapter) Start(ctx context.Context, bus event.Bus, clk clock.Clock) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.running {
		return fmt.Errorf("adapter already running")
	}

	a.bus = bus
	a.clk = clk

	// Create HTTP handler that publishes events
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.handleRequest(ctx, w, r)
	})

	a.server = &http.Server{
		Addr:    a.addr,
		Handler: handler,
	}

	// Start server in goroutine
	go func() {
		if err := a.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// Log error - in production would use proper logging
			fmt.Printf("HTTP server error: %v\n", err)
		}
	}()

	a.running = true
	return nil
}

// Stop shuts down the HTTP server
func (a *ServerAdapter) Stop() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.running {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := a.server.Shutdown(ctx)
	a.running = false
	return err
}

// handleRequest processes an HTTP request and publishes it as an event
func (a *ServerAdapter) handleRequest(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Parse query parameters
	query := make(map[string]string)
	for key, values := range r.URL.Query() {
		if len(values) > 0 {
			query[key] = values[0] // Take first value
		}
	}

	// Parse headers
	headers := make(map[string]string)
	for key, values := range r.Header {
		if len(values) > 0 {
			headers[key] = values[0] // Take first value
		}
	}

	// Generate request ID
	requestID := uuid.New().String()

	// Get local address
	localAddr := a.addr

	// Create payload
	payload := HTTPRequestPayload{
		RequestID:  requestID,
		Method:     r.Method,
		Path:       r.URL.Path,
		Query:      query,
		Headers:    headers,
		Body:       body,
		RemoteAddr: r.RemoteAddr,
		LocalAddr:  localAddr,
		Timestamp:  time.Now(),
		TLS:        r.TLS != nil,
	}

	// Create event with JSON codec
	codec := event.JSONCodec{}
	evt, err := event.NewEvent("net.http.request", a.id, payload, codec)
	if err != nil {
		http.Error(w, "Failed to create event", http.StatusInternalServerError)
		return
	}

	// Add metadata
	evt.WithMetadata("adapter_id", a.id).
		WithMetadata("request_id", requestID)

	// Store response writer in global registry
	rw := &responseWriter{
		w:         w,
		requestID: requestID,
		written:   false,
		done:      make(chan struct{}),
	}
	globalResponseWriters.Store(requestID, rw)

	// Publish event
	if err := a.bus.Publish(ctx, evt); err != nil {
		globalResponseWriters.Delete(requestID)
		http.Error(w, "Failed to process request", http.StatusInternalServerError)
		return
	}

	// Wait for response with timeout
	select {
	case <-rw.done:
		// Response was written
		globalResponseWriters.Delete(requestID)
	case <-time.After(30 * time.Second):
		// Timeout - write default response
		globalResponseWriters.Delete(requestID)
		if !rw.written {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Request processed"))
		}
	}
}

// responseWriter wraps http.ResponseWriter with tracking
type responseWriter struct {
	w         http.ResponseWriter
	requestID string
	written   bool
	done      chan struct{}
	mu        sync.Mutex
}

// WriteResponse writes the HTTP response (called by emitter)
func (rw *responseWriter) WriteResponse(statusCode int, headers map[string]string, body []byte) error {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	if rw.written {
		return fmt.Errorf("response already written")
	}

	// Set headers
	for key, value := range headers {
		rw.w.Header().Set(key, value)
	}

	// Write status code
	rw.w.WriteHeader(statusCode)

	// Write body
	if len(body) > 0 {
		_, err := rw.w.Write(body)
		if err != nil {
			return err
		}
	}

	rw.written = true
	close(rw.done) // Signal that response is written
	return nil
}

// Global response writer registry
var globalResponseWriters sync.Map

// GetResponseWriter retrieves a response writer by request ID
func GetResponseWriter(requestID string) (*responseWriter, bool) {
	val, ok := globalResponseWriters.Load(requestID)
	if !ok {
		return nil, false
	}
	return val.(*responseWriter), true
}
