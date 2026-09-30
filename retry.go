package forward

import (
	"context"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RetryPolicy makes a client ride out a transient failure instead of surfacing
// it to the caller. The zero value retries nothing, so a client that says
// nothing about retries behaves exactly as it always has.
type RetryPolicy struct {
	// MaxAttempts counts the first try. Zero or one means no retry.
	MaxAttempts int
	// Delay is the wait before the second attempt, doubled for each one after.
	Delay time.Duration
	// MaxDelay caps any single wait, including one a server asks for with
	// Retry-After. A server asking for longer than this gets its answer
	// returned to the caller instead of a silent sleep. Zero means five
	// minutes: long enough that a schedule written before the cap existed
	// (Skyforge's BusyRetry waits out Forward's one-active-chat 429 with 5s
	// doubling to 160s) keeps every wait it asked for.
	MaxDelay time.Duration
	// RefusedOnly retries only 429 and 503 -- the two statuses that say the
	// request was turned away unprocessed -- and hands every other failure
	// (transport errors, 500, 502, 504) straight back, even for an idempotent
	// method. For callers that want a bounded, predictable read rather than
	// one that rides out an unhealthy appserver.
	RefusedOnly bool
	// Jitter randomizes each backoff wait downward by up to this fraction
	// (0.5: somewhere in [wait/2, wait]), so clients refused together do not
	// retry together. Clamped to [0, 1]; zero keeps the exact schedule. A
	// server's Retry-After is honored exactly, never shortened.
	Jitter float64
}

const (
	defaultRetryDelay    = 500 * time.Millisecond
	defaultRetryMaxDelay = 5 * time.Minute
)

// send performs one request, retrying while the policy allows and the failure
// looks transient.
//
// What is safe to repeat depends on the method. A 429 or a 503 says the
// request was turned away before it was processed, so any method may be
// repeated. Everything else leaves the outcome unknown, so only an idempotent
// method is repeated -- replaying a POST could create a second snapshot, or a
// second change set, for a request that already succeeded. That includes 502
// and 504: a proxy that gave up waiting says nothing about whether the
// appserver behind it finished the work.
func (c *Client) send(req *http.Request) (*http.Response, error) {
	attempts := c.retry.MaxAttempts
	if attempts < 1 {
		attempts = 1
	}
	delay := c.retry.Delay
	if delay <= 0 {
		delay = defaultRetryDelay
	}
	maxDelay := c.retry.MaxDelay
	if maxDelay <= 0 {
		maxDelay = defaultRetryMaxDelay
	}

	for attempt := 1; ; attempt++ {
		resp, err := c.httpClient.Do(req)
		if attempt >= attempts || !retryable(req, resp, err, c.retry.RefusedOnly) {
			return resp, err
		}
		wait, ok := retryWait(resp, delay, attempt, maxDelay, c.retry.Jitter)
		if !ok {
			return resp, err
		}
		if resp != nil {
			// Draining lets the connection be reused rather than abandoned.
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		if err := waitOrCancel(req.Context(), wait); err != nil {
			return nil, err
		}
		if req.Body != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			req.Body = body
		}
	}
}

func retryable(req *http.Request, resp *http.Response, err error, refusedOnly bool) bool {
	// A body that cannot be rewound cannot be sent twice.
	if req.Body != nil && req.GetBody == nil {
		return false
	}
	if req.Context().Err() != nil {
		return false
	}
	if err != nil {
		return !refusedOnly && idempotent(req.Method)
	}
	switch resp.StatusCode {
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		return true
	}
	if refusedOnly {
		return false
	}
	switch resp.StatusCode {
	case http.StatusNotImplemented:
		// Permanent: the route is absent from this build, not overloaded.
		return false
	}
	return resp.StatusCode >= 500 && idempotent(req.Method)
}

// retryWait is the wait before the next attempt: the server's Retry-After when
// it sent one, otherwise the doubled backoff less up to jitter of itself, both
// capped at maxDelay. It
// reports false when the server asked for longer than maxDelay, because
// sleeping that long inside a call the caller thinks is ordinary is worse than
// handing them the 429 or 503 to decide on.
func retryWait(resp *http.Response, delay time.Duration, attempt int, maxDelay time.Duration, jitter float64) (time.Duration, bool) {
	if resp != nil {
		if after, ok := retryAfter(resp.Header.Get("Retry-After"), time.Now()); ok {
			return after, after <= maxDelay
		}
	}
	for i := 1; i < attempt && delay < maxDelay; i++ {
		delay *= 2
	}
	wait := min(delay, maxDelay)
	if jitter = min(max(jitter, 0), 1); jitter > 0 {
		wait -= time.Duration(rand.Float64() * jitter * float64(wait))
	}
	return wait, true
}

// retryAfter parses a Retry-After value: delay-seconds or an HTTP date.
func retryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds < 0 {
			return 0, false
		}
		if seconds > int64(math.MaxInt64/time.Second) {
			return time.Duration(math.MaxInt64), true
		}
		return time.Duration(seconds) * time.Second, true
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	return max(when.Sub(now), 0), true
}

func idempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPut, http.MethodDelete:
		return true
	}
	return false
}

func waitOrCancel(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
