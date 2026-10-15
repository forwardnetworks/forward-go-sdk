package forward

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func unsentRetryClient(t *testing.T, baseURL string, httpClient *http.Client) *Client {
	t.Helper()
	client, err := NewClient(Config{
		BaseURL: baseURL, Username: "u", Password: "p", HTTPClient: httpClient,
		Retry: RetryPolicy{MaxAttempts: 3, Delay: time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// A failure to OBTAIN a connection means no byte of the request was written,
// so even a PATCH or POST (never retried on a transport error before) may be
// repeated: the server cannot have acted on it.
func TestRetryRepeatsAnyMethodWhenNoConnectionWasObtained(t *testing.T) {
	var served atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	var dials atomic.Int32
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if dials.Add(1) == 1 {
			return nil, &net.OpError{Op: "dial", Net: network, Err: errors.New("i/o timeout")}
		}
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	t.Cleanup(transport.CloseIdleConnections)
	client := unsentRetryClient(t, server.URL, &http.Client{Transport: transport})
	for _, method := range []string{http.MethodPatch, http.MethodPost} {
		transport.CloseIdleConnections() // a kept-alive connection would skip the dial
		dials.Store(0)
		served.Store(0)
		req, err := client.newJSONRequest(context.Background(), method, "/api/networks/1/x", map[string]any{"a": 1})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.Do(req, nil); err != nil {
			t.Fatalf("%s whose connection never opened must be retried: %v", method, err)
		}
		if dials.Load() != 2 || served.Load() != 1 {
			t.Fatalf("%s: dials=%d served=%d, want 2 dials and exactly one request served", method, dials.Load(), served.Load())
		}
	}
}

// The real failure: the server accepts the TCP connection and never completes
// the TLS handshake, so net/http's handshake timeout fires. A PATCH is then
// retried (twice more, three connections in all), because nothing was written.
func TestRetryRepeatsAPatchAfterATLSHandshakeTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var accepted atomic.Int32
	done := make(chan struct{})
	t.Cleanup(func() { close(done); _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			go func() { <-done; _ = conn.Close() }() // hold it open, say nothing
		}
	}()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSHandshakeTimeout = 50 * time.Millisecond
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	t.Cleanup(transport.CloseIdleConnections)
	client := unsentRetryClient(t, "https://"+listener.Addr().String(), &http.Client{Transport: transport})
	req, err := client.newJSONRequest(context.Background(), http.MethodPatch, "/api/networks/1/classic-devices/sw1", map[string]any{"note": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(req, nil); err == nil {
		t.Fatal("a server that never answers the handshake must still fail in the end")
	}
	for i := 0; i < 100 && accepted.Load() < 3; i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if accepted.Load() != 3 {
		t.Fatalf("a PATCH that timed out in the TLS handshake must be attempted 3 times, got %d", accepted.Load())
	}
}

// Once the request has been written the outcome is unknown: a POST must not be
// repeated, and neither must one on a transport the trace cannot see.
func TestRetryDoesNotRepeatAPostThatMayHaveBeenProcessed(t *testing.T) {
	var served atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served.Add(1)
		if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
			_ = conn.Close() // read and acted on; the answer never arrives
		}
	}))
	defer server.Close()
	client := unsentRetryClient(t, server.URL, privateHTTPClient())
	req, err := client.newJSONRequest(context.Background(), http.MethodPost, "/api/networks/1/snapshots", map[string]any{"a": 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(req, nil); err == nil || served.Load() != 1 {
		t.Fatalf("a POST that was written has an unknown outcome and must not be repeated: err=%v served=%d", err, served.Load())
	}
}

type failingTransport struct{ calls atomic.Int32 }

func (f *failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	f.calls.Add(1)
	return nil, errors.New("injected transport failure")
}

func TestRetryTreatsAnInjectedTransportAsUnknown(t *testing.T) {
	transport := &failingTransport{}
	client := unsentRetryClient(t, "http://127.0.0.1:1", &http.Client{Transport: transport})
	req, err := client.newJSONRequest(context.Background(), http.MethodPost, "/api/networks/1/snapshots", map[string]any{"a": 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(req, nil); err == nil || transport.calls.Load() != 1 {
		t.Fatalf("a transport that never fires the connection trace proves nothing was unsent: err=%v calls=%d", err, transport.calls.Load())
	}
}

// A certificate failure is permanent, not transient: no method is repeated for it.
func TestRetryDoesNotRepeatACertificateFailure(t *testing.T) {
	var handshakes atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.TLS = &tls.Config{GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) { handshakes.Add(1); return nil, nil }}
	server.StartTLS()
	defer server.Close()
	client := unsentRetryClient(t, server.URL, privateHTTPClient()) // does not trust the test certificate
	req, err := client.newJSONRequest(context.Background(), http.MethodPatch, "/api/networks/1/x", map[string]any{"a": 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(req, nil); err == nil || handshakes.Load() != 1 {
		t.Fatalf("a certificate failure must not be retried: err=%v handshakes=%d", err, handshakes.Load())
	}
}

func rateLimited(w http.ResponseWriter, retryAfter string, message string) {
	w.Header().Set("Retry-After", retryAfter)
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = w.Write([]byte(`{"message":"` + message + `"}`))
}

// Forward's rate limiter LOCKS THE USER OUT (default 1 minute) and the 429
// says how long in Retry-After. It is typed, and the lockout is per user, so a
// second concurrent request must wait for the first one's Retry-After instead
// of discovering the lockout itself; a lockout longer than MaxDelay is handed
// back to the caller, not slept through.
func TestRateLimitLockoutIsTypedAndSharedAcrossRequests(t *testing.T) {
	var mu sync.Mutex
	var arrivals []time.Time
	var first atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		arrivals = append(arrivals, time.Now())
		mu.Unlock()
		if r.URL.Path == "/api/locked" {
			rateLimited(w, "600", "Rate limit exceeded, please try again later")
			return
		}
		if first.CompareAndSwap(false, true) {
			rateLimited(w, "1", "Rate limit exceeded, please try again later")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := unsentRetryClient(t, server.URL, privateHTTPClient())
	client.retry = RetryPolicy{MaxAttempts: 4, Delay: time.Millisecond, MaxDelay: 5 * time.Second}

	do := func(path string) error {
		req, err := client.NewRequest(context.Background(), http.MethodGet, path, nil)
		if err != nil {
			return err
		}
		_, err = client.Do(req, nil)
		return err
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(1)
	go func() { defer wg.Done(); errs[0] = do("/api/a") }()
	deadline := time.Now().Add(2 * time.Second)
	for first.Load() == false && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	for client.cooldown.until.Load() == 0 && time.Now().Before(deadline) { // the 429 has been seen and the cooldown set
		time.Sleep(time.Millisecond)
	}
	started := time.Now()
	errs[1] = do("/api/b")
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("both requests must succeed after the lockout: %v %v", errs[0], errs[1])
	}
	if waited := time.Since(started); waited < 700*time.Millisecond {
		t.Fatalf("the second request must wait out the first one's Retry-After instead of sending into the lockout (waited %s)", waited)
	}

	err := do("/api/locked")
	lockout, ok := RateLimitLockout(err)
	if !errors.Is(err, ErrRateLimited) || !ok || lockout != 600*time.Second {
		t.Fatalf("a lockout longer than MaxDelay must come back typed with its length: %v, %v, %v", err, lockout, ok)
	}
}

// Not every 429 is the rate limiter (Forward's one-active-chat refusal is not),
// so it must not set the client's cooldown and stall unrelated requests; the
// limiter's own 429 must, and a ForNetwork clone shares that cooldown.
func TestOnlyTheRateLimiters429SetsTheSharedCooldown(t *testing.T) {
	message := atomic.Value{}
	message.Store("Another chat is already active")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/limited" {
			rateLimited(w, "30", message.Load().(string))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := unsentRetryClient(t, server.URL, privateHTTPClient())
	client.retry = RetryPolicy{MaxAttempts: 2, Delay: time.Millisecond, MaxDelay: time.Millisecond}

	get := func(c *Client) {
		req, _ := c.NewRequest(context.Background(), http.MethodGet, "/api/limited", nil)
		_, _ = c.Do(req, nil)
	}
	get(client)
	if client.cooldown.until.Load() != 0 {
		t.Fatal("a 429 that is not the rate limiter must not set a cooldown")
	}
	message.Store("Rate limit exceeded, please try again later")
	get(client)
	if client.cooldown.until.Load() == 0 {
		t.Fatal("the rate limiter's 429 must set the cooldown")
	}
	if clone := client.ForNetwork("net-2"); clone.cooldown != client.cooldown {
		t.Fatal("a ForNetwork clone shares one credential and so one rate limit: it must share the cooldown")
	}
}
