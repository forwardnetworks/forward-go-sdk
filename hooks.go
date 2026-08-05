package forward

import (
	"context"
	"net/http"
	"time"
)

// EventType identifies a client lifecycle event.
type EventType string

const (
	EventRequest  EventType = "request"
	EventResponse EventType = "response"
)

// Event is safe request metadata for logging, tracing, metrics, and callbacks.
// It never includes authentication, query parameters, or request/response
// bodies.
type Event struct {
	Type       EventType
	Method     string
	Path       string
	StatusCode int
	Duration   time.Duration
	Err        error
	// Operation is a bounded SDK operation name when supplied by a typed
	// service. AuthMode identifies the effective per-operation principal.
	Operation string
	AuthMode  AuthMode
}

// RequestMetadata is safe for both SDK hooks and an injected RoundTripper.
// A transport can read it on every attempt while SDK hooks continue to report
// the logical operation once.
type RequestMetadata struct {
	Operation string
	AuthMode  AuthMode
}

type requestMetadataKey struct{}

// MetadataFromRequest returns bounded operation metadata attached by the SDK.
// It never includes resource IDs, query values, credentials, or bodies.
func MetadataFromRequest(req *http.Request) RequestMetadata {
	if req == nil {
		return RequestMetadata{}
	}
	metadata, _ := req.Context().Value(requestMetadataKey{}).(RequestMetadata)
	return metadata
}

func withRequestMetadata(ctx context.Context, metadata RequestMetadata) context.Context {
	return context.WithValue(ctx, requestMetadataKey{}, metadata)
}

func markOperation(req *http.Request, operation string) *http.Request {
	if req == nil {
		return nil
	}
	metadata := MetadataFromRequest(req)
	metadata.Operation = operation
	return req.WithContext(withRequestMetadata(req.Context(), metadata))
}

// Hook observes requests made by the client. Hooks run synchronously and
// should return quickly; a hook that needs blocking work should enqueue it.
type Hook func(context.Context, Event)

func (c *Client) emit(ctx context.Context, event Event) {
	for _, hook := range c.hooks {
		if hook != nil {
			hook(ctx, event)
		}
	}
}
