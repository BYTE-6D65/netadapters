package http

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/BYTE-6D65/pipeline/pkg/event"
)

func TestClientEmitter_Metadata(t *testing.T) {
	emitter := NewClientEmitter()

	if emitter.ID() != "http-client-emitter" {
		t.Errorf("Expected ID 'http-client-emitter', got %s", emitter.ID())
	}

	if emitter.Type() != "http-client" {
		t.Errorf("Expected Type 'http-client', got %s", emitter.Type())
	}
}

func TestClientEmitter_Close(t *testing.T) {
	emitter := NewClientEmitter()
	if err := emitter.Close(); err != nil {
		t.Errorf("Expected Close to return nil, got %v", err)
	}
}

func TestClientEmitter_Emit_InvalidPayload(t *testing.T) {
	emitter := NewClientEmitter()

	// Create event with invalid payload (not HTTPResponsePayload)
	evt := &event.Event{
		ID:        "test-1",
		Type:      "net.http.response",
		Source:    "test",
		Timestamp: time.Now(),
		Data:      []byte(`{"invalid": "structure"}`),
	}

	err := emitter.Emit(context.Background(), evt)
	if err == nil {
		t.Error("Expected error when decoding invalid payload, got nil")
	}
}

func TestClientEmitter_Emit_NoResponseWriter(t *testing.T) {
	emitter := NewClientEmitter()

	// Create valid response payload but with non-existent request ID
	payload := HTTPResponsePayload{
		RequestID:  "non-existent-request-id",
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "text/plain"},
		Body:       []byte("test"),
		Timestamp:  time.Now(),
	}

	codec := event.JSONCodec{}
	evt, err := event.NewEvent("net.http.response", "test", payload, codec)
	if err != nil {
		t.Fatalf("Failed to create event: %v", err)
	}

	err = emitter.Emit(context.Background(), evt)
	if err == nil {
		t.Error("Expected error when response writer not found, got nil")
	}
	if err != nil && err.Error() != "no response writer found for request ID non-existent-request-id" {
		t.Errorf("Expected specific error message, got: %v", err)
	}
}

func TestClientEmitter_Emit_WriteResponseError(t *testing.T) {
	emitter := NewClientEmitter()

	requestID := "test-write-error"
	payload := HTTPResponsePayload{
		RequestID:  requestID,
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "text/plain"},
		Body:       []byte("test"),
		Timestamp:  time.Now(),
	}

	// Create a response writer and mark it as already written
	rw := &responseWriter{
		w:         nil, // Will cause error, but written flag takes precedence
		requestID: requestID,
		written:   true, // Already written
		done:      make(chan struct{}),
	}
	globalResponseWriters.Store(requestID, rw)
	defer globalResponseWriters.Delete(requestID)

	codec := event.JSONCodec{}
	evt, err := event.NewEvent("net.http.response", "test", payload, codec)
	if err != nil {
		t.Fatalf("Failed to create event: %v", err)
	}

	err = emitter.Emit(context.Background(), evt)
	if err == nil {
		t.Error("Expected error when writing response twice, got nil")
	}
}
