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

// A 503 is different: the request was turned away before it was processed, so
// even a POST is safe to repeat.
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

// A 504 is a proxy that stopped waiting, not an appserver that refused: the
// change set may already exist, so a POST is not repeated.
func TestRetryDoesNotReplayPostOnGatewayTimeout(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusBadGateway, http.StatusGatewayTimeout} {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.WriteHeader(status)
		}))
		_, _, err := retryClient(t, server.URL).Predict.CreateChangeSet(
			context.Background(), "net-1", ChangeSetCreateRequest{Name: "nightly", SnapshotID: "539"},
		)
		server.Close()
		if !IsStatus(err, status) || calls.Load() != 1 {
			t.Fatalf("status %d: err = %v after %d calls, want the %d back after one", status, err, calls.Load(), status)
		}
	}
}

// The same 504 on a GET is safe to ride out.
func TestRetryReplaysGetOnGatewayTimeout(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 2 {
			w.WriteHeader(http.StatusGatewayTimeout)
			return
		}
		_, _ = io.WriteString(w, `{"build":"26.9.0-18","release":"26.9"}`)
	}))
	defer server.Close()

	if _, _, err := retryClient(t, server.URL).Version.Get(context.Background()); err != nil || calls.Load() != 2 {
		t.Fatalf("Version.Get() err = %v after %d calls", err, calls.Load())
	}
}

// A server that asks for longer than MaxDelay gets its 429 handed back rather
// than a call that silently hangs for however long it named.
func TestRetryReturnsWhenRetryAfterExceedsMaxDelay(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	started := time.Now()
	_, _, err := retryClient(t, server.URL).Version.Get(context.Background())
	if !IsStatus(err, http.StatusTooManyRequests) || calls.Load() != 1 || time.Since(started) > 5*time.Second {
		t.Fatalf("err = %v after %d calls in %s", err, calls.Load(), time.Since(started))
	}
}

func TestRetryWaitHonorsRetryAfterAndCapsBackoff(t *testing.T) {
	t.Parallel()

	withHeader := func(value string) *http.Response {
		return &http.Response{Header: http.Header{"Retry-After": []string{value}}}
	}
	if wait, ok := retryWait(withHeader("2"), time.Millisecond, 1, time.Minute, 0); !ok || wait != 2*time.Second {
		t.Fatalf("Retry-After 2: wait = %s, ok = %v", wait, ok)
	}
	date := time.Now().Add(10 * time.Second).UTC().Format(http.TimeFormat)
	if wait, ok := retryWait(withHeader(date), time.Millisecond, 1, time.Minute, 0); !ok || wait <= 0 || wait > 10*time.Second {
		t.Fatalf("Retry-After date: wait = %s, ok = %v", wait, ok)
	}
	if _, ok := retryWait(withHeader("99999999999999999"), time.Millisecond, 1, time.Minute, 0); ok {
		t.Fatal("an absurd Retry-After must not be slept on")
	}
	// Doubling past the cap used to overflow the shift into a zero or
	// negative wait, which turned a long retry budget into a hot loop.
	if wait, ok := retryWait(nil, 500*time.Millisecond, 200, 30*time.Second, 0); !ok || wait != 30*time.Second {
		t.Fatalf("attempt 200: wait = %s, ok = %v", wait, ok)
	}
	if wait, _ := retryWait(withHeader("soon"), 500*time.Millisecond, 3, 30*time.Second, 0); wait != 2*time.Second {
		t.Fatalf("unparseable Retry-After falls back to backoff: wait = %s", wait)
	}
}

// A policy written before MaxDelay existed must keep its schedule: Skyforge's
// BusyRetry waits out a busy AI chat with 5s doubling to 160s, about five
// minutes in all, and a shorter default cap would quietly give up early.
func TestRetryDefaultMaxDelayKeepsExistingSchedules(t *testing.T) {
	t.Parallel()

	want := []time.Duration{5, 10, 20, 40, 80, 160}
	for i, seconds := range want {
		if wait, ok := retryWait(nil, 5*time.Second, i+1, defaultRetryMaxDelay, 0); !ok || wait != seconds*time.Second {
			t.Fatalf("attempt %d: wait = %s, want %ds", i+1, wait, seconds)
		}
	}
}
