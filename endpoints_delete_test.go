package forward

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Deleting an endpoint is how a name gets released back to the SHARED
// device-name namespace. If a 404 came back as an error, a converge loop
// would fail on the very state it is trying to reach, so absence must read
// as success -- and the request must actually be a DELETE on the endpoint
// path, not a PATCH with a body.
func TestEndpointsDeleteIsIdempotentAndHitsTheRightRoute(t *testing.T) {
	var gotMethod, gotPath string
	var calls int
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	if _, err := c.Endpoints.Delete(context.Background(), "net-1", "leaf 01"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("method was %s, want DELETE", gotMethod)
	}
	if gotPath != "/api/networks/net-1/endpoints/leaf 01" {
		t.Fatalf("path was %q", gotPath)
	}
	if calls != 1 {
		t.Fatalf("made %d requests, want 1", calls)
	}

	// A 404 is the desired state, not a failure.
	missing := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"no such endpoint"}`))
	})
	if _, err := missing.Endpoints.Delete(context.Background(), "net-1", "gone"); err != nil {
		t.Fatalf("404 must be success, got %v", err)
	}

	// A real failure still fails.
	broken := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	if _, err := broken.Endpoints.Delete(context.Background(), "net-1", "x"); err == nil {
		t.Fatal("a 500 must surface as an error")
	}

	// An empty name must never become a DELETE on the collection, which
	// would ask Forward to remove every endpoint in the network.
	fatal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no request should be sent for an empty name, got %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(fatal.Close)
	safe, err := NewClient(Config{BaseURL: fatal.URL, Username: "u", Password: "p"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := safe.Endpoints.Delete(context.Background(), "net-1", "  "); err == nil {
		t.Fatal("an empty endpoint name must be refused")
	}
}
