package http

import (
	"context"
	"fmt"

	"github.com/BYTE-6D65/pipeline/pkg/event"
)

// ClientEmitter sends HTTP responses by writing to http.ResponseWriter
type ClientEmitter struct {
	id string
}

// NewClientEmitter creates a new HTTP client emitter
func NewClientEmitter() *ClientEmitter {
	return &ClientEmitter{
		id: "http-client-emitter",
	}
}

// ID returns the emitter's unique identifier
func (e *ClientEmitter) ID() string {
	return e.id
}

// Type returns the emitter type
func (e *ClientEmitter) Type() string {
	return "http-client"
}

// Emit sends an HTTP response by writing to the ResponseWriter
func (e *ClientEmitter) Emit(ctx context.Context, evt event.Event) error {
	// Decode response payload
	codec := event.JSONCodec{}
	var payload HTTPResponsePayload
	if err := evt.DecodePayload(&payload, codec); err != nil {
		return fmt.Errorf("failed to decode payload: %w", err)
	}

	// Get response writer from registry by request ID
	rw, ok := GetResponseWriter(payload.RequestID)
	if !ok {
		return fmt.Errorf("no response writer found for request ID %s", payload.RequestID)
	}

	// Write response
	return rw.WriteResponse(payload.StatusCode, payload.Headers, payload.Body)
}

// Close closes the emitter (no-op for HTTP client emitter)
func (e *ClientEmitter) Close() error {
	return nil
}
