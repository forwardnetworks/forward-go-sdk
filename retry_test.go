package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func retryClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := NewClient(Config{
		BaseURL:  baseURL,
		Username: "user",
		Password: "pass",
		Retry:    RetryPolicy{MaxAttempts: 3, Delay: time.Millisecond},
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}

func TestRetryRidesOutServerError(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, `[{"id":"network-1","name":"Lab","orgId":"org-1"}]`)
	}))
	defer server.Close()

	networks, _, err := retryClient(t, server.URL).Networks.List(context.Background())
	if err != nil {
		t.Fatalf("Networks.List() error = %v", err)
	}
	if len(networks) != 1 || calls.Load() != 3 {
		t.Fatalf("networks = %#v after %d calls", networks, calls.Load())
	}
}

func TestRetryStopsAtMaxAttempts(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	_, _, err := retryClient(t, server.URL).Networks.List(context.Background())
	if err == nil {
		t.Fatal("Networks.List() succeeded against a server that only fails")
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("calls = %d, want 3", got)
	}
}

// A POST that may already have been applied must not be replayed: repeating it
// could start a second collection for a request that in fact succeeded.
func TestRetryDoesNotReplayPostOnServerError(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	_, _, err := retryClient(t, server.URL).CollectorTasks.Start(context.Background(), "net-1")
	if err == nil {
		t.Fatal("CollectorTasks.Start() succeeded against a server that only fails")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1 -- a 500 leaves the outcome unknown", got)
	}
}

// A gateway status is different: the appserver never saw the request, so even
// a POST is safe to repeat.
func TestRetryReplaysPostOnGatewayStatus(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	var lastBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		lastBody = string(body)
		if calls.Add(1) < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{"id":"chg-9","name":"nightly","networkId":"net-1","snapshotId":"539"}`)
	}))
	defer server.Close()

	created, _, err := retryClient(t, server.URL).Predict.CreateChangeSet(
		context.Background(), "net-1", ChangeSetCreateRequest{Name: "nightly", SnapshotID: "539"},
	)
	if err != nil {
		t.Fatalf("Predict.CreateChangeSet() error = %v", err)
	}
	if created.ID != "chg-9" || calls.Load() != 2 {
		t.Fatalf("created = %#v after %d calls", created, calls.Load())
	}
	// The body has to be rewound, or the retry sends an empty request.
	if lastBody != `{"name":"nightly","snapshotId":"539"}` {
		t.Fatalf("replayed body = %q", lastBody)
	}
}

// 501 means the route is absent from this build. Retrying cannot help, and
// waiting out three backoffs to say so wastes the caller's time.
func TestRetrySkipsNotImplemented(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotImplemented)
	}))
	defer server.Close()

	if _, _, err := retryClient(t, server.URL).Networks.List(context.Background()); err == nil {
		t.Fatal("Networks.List() succeeded against a 501")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
}

func TestRetryHonorsContextCancel(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:  server.URL,
		Username: "user",
		Password: "pass",
		Retry:    RetryPolicy{MaxAttempts: 5, Delay: time.Hour},
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	// Long enough that the first request always lands, far shorter than the
	// hour-long backoff the policy would otherwise wait out.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, _, err := client.Networks.List(ctx); err == nil {
		t.Fatal("Networks.List() succeeded despite a canceled context")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1 -- the backoff must not outlive the context", got)
	}
}

// The zero policy is the historical behavior, so an existing client keeps it.
func TestNoRetryByDefault(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	if _, _, err := newTestClient(t, server.URL).Networks.List(context.Background()); err == nil {
		t.Fatal("Networks.List() succeeded against a server that only fails")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
}
