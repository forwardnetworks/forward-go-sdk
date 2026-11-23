package rest

import (
	"net/http"
	"testing"
	"time"
)

// Jitter only shortens a backoff, within its fraction, and never touches a
// server's Retry-After.
func TestRetryJitterBounds(t *testing.T) {
	t.Parallel()

	for i := 0; i < 200; i++ {
		wait, ok := retryWait(nil, time.Second, 3, time.Minute, 0.5)
		if !ok || wait < 2*time.Second || wait > 4*time.Second {
			t.Fatalf("jittered wait = %s, want within [2s, 4s]", wait)
		}
	}
	after := &http.Response{Header: http.Header{"Retry-After": []string{"3"}}}
	if wait, _ := retryWait(after, time.Second, 1, time.Minute, 1); wait != 3*time.Second {
		t.Fatalf("Retry-After with jitter = %s, want exactly 3s", wait)
	}
	if wait, _ := retryWait(nil, time.Second, 1, time.Minute, 7); wait < 0 || wait > time.Second {
		t.Fatalf("jitter above 1 must clamp: wait = %s", wait)
	}
}
