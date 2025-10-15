package http

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/BYTE-6D65/pipeline/pkg/clock"
	"github.com/BYTE-6D65/pipeline/pkg/engine"
	"github.com/BYTE-6D65/pipeline/pkg/event"
)

func TestHTTPServerAdapter(t *testing.T) {
	// Create pipeline engine
	eng := engine.New()
	defer eng.Shutdown(context.Background())

	// Create HTTP server adapter
	adapter := NewServerAdapter(":18080")

	// Create HTTP client emitter
	emitter := NewClientEmitter()

	// Register adapter
	adapterMgr := engine.NewAdapterManager(eng)
	if err := adapterMgr.Register(adapter); err != nil {
		t.Fatalf("Failed to register adapter: %v", err)
	}
	if err := adapterMgr.Start(); err != nil {
		t.Fatalf("Failed to start adapters: %v", err)
	}
	defer adapterMgr.Stop()

	// Register emitter
	emitterMgr := engine.NewEmitterManager(eng)
	if err := emitterMgr.Register("http-client", emitter, event.Filter{
		Types: []string{"net.http.response"},
	}); err != nil {
		t.Fatalf("Failed to register emitter: %v", err)
	}
	if err := emitterMgr.Start(); err != nil {
		t.Fatalf("Failed to start emitters: %v", err)
	}
	defer emitterMgr.Stop()

	// Subscribe to HTTP requests and create echo responses
	sub, err := eng.ExternalBus().Subscribe(context.Background(), event.Filter{
		Types: []string{"net.http.request"},
	})
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}
	defer sub.Close()

	// Process requests in background
	go func() {
		for evt := range sub.Events() {
			response, err := CreateEchoResponse(evt)
			if err != nil {
				t.Errorf("Failed to create echo response: %v", err)
				continue
			}
			eng.ExternalBus().Publish(context.Background(), response)
		}
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Test POST request
	t.Run("POST request", func(t *testing.T) {
		body := []byte("test payload")
		resp, err := http.Post("http://localhost:18080/api/test", "text/plain", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("Failed to send POST request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}

		if !bytes.Contains(respBody, []byte("POST /api/test")) {
			t.Errorf("Expected echo response, got: %s", string(respBody))
		}
		if !bytes.Contains(respBody, body) {
			t.Errorf("Expected response to contain request body, got: %s", string(respBody))
		}
	})

	// Test GET request
	t.Run("GET request", func(t *testing.T) {
		resp, err := http.Get("http://localhost:18080/api/users")
		if err != nil {
			t.Fatalf("Failed to send GET request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}

		if !bytes.Contains(respBody, []byte("GET /api/users")) {
			t.Errorf("Expected echo response, got: %s", string(respBody))
		}
	})
}

func TestCreateEchoResponse(t *testing.T) {
	// Create a sample HTTP request payload
	payload := HTTPRequestPayload{
		RequestID: "test-123",
		Method:    "POST",
		Path:      "/test",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body:       []byte(`{"foo":"bar"}`),
		RemoteAddr: "127.0.0.1:1234",
		LocalAddr:  ":8080",
		Timestamp:  time.Now(),
		TLS:        false,
	}

	// Create event
	codec := event.JSONCodec{}
	evt, err := event.NewEvent("net.http.request", "test-adapter", payload, codec)
	if err != nil {
		t.Fatalf("Failed to create event: %v", err)
	}

	// Create echo response
	respEvt, err := CreateEchoResponse(evt)
	if err != nil {
		t.Fatalf("Failed to create echo response: %v", err)
	}

	// Decode response payload
	var respPayload HTTPResponsePayload
	if err := respEvt.DecodePayload(&respPayload, codec); err != nil {
		t.Fatalf("Failed to decode response payload: %v", err)
	}

	// Verify response
	if respPayload.RequestID != payload.RequestID {
		t.Errorf("Expected request ID %s, got %s", payload.RequestID, respPayload.RequestID)
	}
	if respPayload.StatusCode != http.StatusOK {
		t.Errorf("Expected status code 200, got %d", respPayload.StatusCode)
	}
	if !bytes.Contains(respPayload.Body, []byte("POST /test")) {
		t.Errorf("Expected body to contain method and path, got: %s", string(respPayload.Body))
	}
	if !bytes.Contains(respPayload.Body, []byte(`{"foo":"bar"}`)) {
		t.Errorf("Expected body to contain request body, got: %s", string(respPayload.Body))
	}
}

func TestParsePathParams(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		path     string
		expected map[string]string
	}{
		{
			name:     "single parameter",
			pattern:  "/users/:id",
			path:     "/users/123",
			expected: map[string]string{"id": "123"},
		},
		{
			name:     "multiple parameters",
			pattern:  "/users/:userId/posts/:postId",
			path:     "/users/123/posts/456",
			expected: map[string]string{"userId": "123", "postId": "456"},
		},
		{
			name:     "no match",
			pattern:  "/users/:id",
			path:     "/posts/123",
			expected: map[string]string{},
		},
		{
			name:     "no parameters",
			pattern:  "/users",
			path:     "/users",
			expected: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParsePathParams(tt.pattern, tt.path)
			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d params, got %d", len(tt.expected), len(result))
			}
			for key, expectedVal := range tt.expected {
				if result[key] != expectedVal {
					t.Errorf("Expected param %s=%s, got %s", key, expectedVal, result[key])
				}
			}
		})
	}
}

func TestServerAdapter_Metadata(t *testing.T) {
	adapter := NewServerAdapter(":9999")

	if adapter.ID() != "http-server-:9999" {
		t.Errorf("Expected ID 'http-server-:9999', got %s", adapter.ID())
	}

	if adapter.Type() != "http-server" {
		t.Errorf("Expected Type 'http-server', got %s", adapter.Type())
	}
}

func TestServerAdapter_StartTwice(t *testing.T) {
	adapter := NewServerAdapter(":19999")
	eng := engine.New()
	defer eng.Shutdown(context.Background())

	adapterMgr := engine.NewAdapterManager(eng)
	if err := adapterMgr.Register(adapter); err != nil {
		t.Fatalf("Failed to register adapter: %v", err)
	}
	if err := adapterMgr.Start(); err != nil {
		t.Fatalf("Failed to start adapter: %v", err)
	}
	defer adapterMgr.Stop()

	// Try to start again - should fail since it's already running
	err := adapter.Start(context.Background(), eng.ExternalBus(), clock.NewSystemClock())
	if err == nil {
		t.Error("Expected error when starting adapter twice, got nil")
	}
	if err != nil && err.Error() != "adapter already running" {
		t.Errorf("Expected 'adapter already running' error, got: %v", err)
	}
}

func TestServerAdapter_StopWhenNotRunning(t *testing.T) {
	adapter := NewServerAdapter(":29999")

	// Stop without starting - should be no-op
	err := adapter.Stop()
	if err != nil {
		t.Errorf("Expected no error when stopping non-running adapter, got: %v", err)
	}
}

func TestGetResponseWriter_NotFound(t *testing.T) {
	_, ok := GetResponseWriter("non-existent-request-id")
	if ok {
		t.Error("Expected GetResponseWriter to return false for non-existent ID")
	}
}

// MockClock for testing
type MockClock struct {
	now clock.MonoTime
}

func (m *MockClock) Now() clock.MonoTime {
	return m.now
}

func (m *MockClock) Since(t clock.MonoTime) time.Duration {
	return clock.ToDuration(m.now - t)
}
