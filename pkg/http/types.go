package http

import "time"

// HTTPRequestPayload represents an HTTP request event
type HTTPRequestPayload struct {
	// Identity
	RequestID string `json:"request_id"` // UUID for correlation

	// Request data
	Method  string            `json:"method"`  // GET, POST, etc.
	Path    string            `json:"path"`    // /api/users
	Query   map[string]string `json:"query"`   // ?foo=bar
	Headers map[string]string `json:"headers"` // Content-Type, etc.
	Body    []byte            `json:"body"`    // Request body

	// Network data
	RemoteAddr string `json:"remote_addr"` // Client IP:port
	LocalAddr  string `json:"local_addr"`  // Server IP:port

	// Metadata
	Timestamp time.Time `json:"timestamp"` // When received
	TLS       bool      `json:"tls"`       // HTTPS?
}

// HTTPResponsePayload represents an HTTP response event
type HTTPResponsePayload struct {
	// Correlation
	RequestID string `json:"request_id"` // Match to request

	// Response data
	StatusCode int               `json:"status_code"` // 200, 404, etc.
	Headers    map[string]string `json:"headers"`     // Content-Type, etc.
	Body       []byte            `json:"body"`        // Response body

	// Metadata
	Timestamp   time.Time `json:"timestamp"`    // When sent
	DurationNs  int64     `json:"duration_ns"`  // Processing time in nanoseconds
}
