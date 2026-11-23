package forward

import (
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
)

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func retryClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := NewClient(Config{HTTPClient: privateHTTPClient(),
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
		context.Background(), "net-1", domain.ChangeSetCreateRequest{Name: "nightly", SnapshotID: "539"},
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

	client, err := NewClient(Config{HTTPClient: privateHTTPClient(),
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
			context.Background(), "net-1", domain.ChangeSetCreateRequest{Name: "nightly", SnapshotID: "539"},
		)
		server.Close()
		if !ports.IsStatus(err, status) || calls.Load() != 1 {
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
	if !ports.IsStatus(err, http.StatusTooManyRequests) || calls.Load() != 1 || time.Since(started) > 5*time.Second {
		t.Fatalf("err = %v after %d calls in %s", err, calls.Load(), time.Since(started))
	}
}

type permanentMethodErr struct{}

func (permanentMethodErr) Error() string   { return "guard says no" }
func (permanentMethodErr) Permanent() bool { return true }

type refusingTransport struct {
	calls atomic.Int32
	err   error
}

func (r *refusingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	r.calls.Add(1)
	return nil, r.err
}

// A client-side guard (a RoundTripper that refuses a request before it is sent)
// is permanent: waiting and asking again gets the same refusal. An error that
// wraps ErrPermanent, or answers Permanent() true, ends the retry loop at the
// first attempt for every method, and the caller's own error stays reachable
// through the SDK's wrapping. An ordinary transport error is still retried for
// an idempotent method, so the marker is what changes the behaviour.
func TestRetryStopsOnPermanentTransportError(t *testing.T) {
	t.Parallel()

	errGuard := errors.New("guard refused")
	for _, tc := range []struct {
		name      string
		err       error
		wantCalls int32
	}{
		{"sentinel", fmt.Errorf("%w: %w", errGuard, domain.ErrPermanent), 1},
		{"method", fmt.Errorf("blocked: %w", permanentMethodErr{}), 1},
		{"unmarked is retried", errGuard, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rt := &refusingTransport{err: tc.err}
			client, err := NewClient(Config{
				HTTPClient: &http.Client{Transport: rt},
				BaseURL:    "http://forward.invalid",
				Username:   "user",
				Password:   "pass",
				Retry:      RetryPolicy{MaxAttempts: 3, Delay: time.Millisecond},
			})
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			_, _, err = client.Networks.List(WithIdempotent(context.Background()))
			if err == nil {
				t.Fatal("Networks.List() succeeded through a refusing transport")
			}
			if !errors.Is(err, errGuard) && tc.name != "method" {
				t.Fatalf("error %v lost the caller's error", err)
			}
			var pm permanentMethodErr
			if tc.name == "method" && !errors.As(err, &pm) {
				t.Fatalf("error %v lost the caller's error type", err)
			}
			if got := rt.calls.Load(); got != tc.wantCalls {
				t.Fatalf("transport calls = %d, want %d", got, tc.wantCalls)
			}
		})
	}
}
