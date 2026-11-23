package rest

import (
	"net/http"
	"testing"
	"time"
)

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

type permanentMethodErr struct{}

func (permanentMethodErr) Error() string { return "guard says no" }

func (permanentMethodErr) Permanent() bool { return true }
