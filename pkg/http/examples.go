package http

import (
	"fmt"
	"net/http"
	"time"

	"github.com/BYTE-6D65/pipeline/pkg/event"
)

// CreateEchoResponse creates a response event that echoes the request.
// This is an example helper function for demonstration purposes.
// It creates a text/plain response containing the request details.
func CreateEchoResponse(requestEvt *event.Event) (*event.Event, error) {
	// Decode request payload
	codec := event.JSONCodec{}
	var payload HTTPRequestPayload
	if err := requestEvt.DecodePayload(&payload, codec); err != nil {
		// Create error response
		errorResponse := HTTPResponsePayload{
			StatusCode: http.StatusInternalServerError,
			Body:       []byte("Invalid request payload"),
			Timestamp:  time.Now(),
		}
		return event.NewEvent("net.http.response", "http-echo", errorResponse, codec)
	}

	// Echo back request info
	echoBody := fmt.Sprintf("Echo: %s %s\n\nRequest ID: %s\nHeaders: %v\nBody: %s",
		payload.Method,
		payload.Path,
		payload.RequestID,
		payload.Headers,
		string(payload.Body),
	)

	response := HTTPResponsePayload{
		RequestID:  payload.RequestID,
		StatusCode: http.StatusOK,
		Headers: map[string]string{
			"Content-Type": "text/plain",
		},
		Body:      []byte(echoBody),
		Timestamp: time.Now(),
	}

	evt, err := event.NewEvent("net.http.response", "http-echo", response, codec)
	if err != nil {
		return nil, err
	}

	evt.WithMetadata("request_id", payload.RequestID)
	return evt, nil
}
