package forward

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"strings"
	"sync/atomic"
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
	// (transport errors, including a connection that could not be
	// obtained, 500, 502, 504) straight back, even for an idempotent method. For callers that want a bounded, predictable read rather than
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
// appserver behind it finished the work. The one exception is a failure to
// OBTAIN a connection (a dial error, a TLS handshake timeout): nothing was
// written, so any method may be repeated (see notConnected).
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
		// A rate-limit lockout is per user, so once any request on this client (or
		// its ForNetwork clones) has been told to wait, the others wait too rather
		// than each discovering the lockout for itself.
		if attempts > 1 {
			if err := c.cooldown.wait(req.Context(), maxDelay); err != nil {
				return nil, err
			}
		}
		// The trace is the evidence for the one transport failure that is safe to
		// repeat for any method: no connection was ever obtained (see
		// notConnected). GetConn fires when net/http's own transport starts to
		// obtain one, GotConn when it has one. The trace only proves anything on
		// that transport; an injected RoundTripper never fires either, which
		// reads as "unknown", so it is retried as before (idempotent methods only).
		var gotConn, getConn atomic.Bool
		traced := req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
			GetConn: func(string) { getConn.Store(true) },
			GotConn: func(httptrace.GotConnInfo) { gotConn.Store(true) },
		}))
		resp, err := c.httpClient.Do(traced)
		if attempts > 1 && resp != nil && resp.StatusCode == http.StatusTooManyRequests && isRateLimitBody(resp) {
			if wait, ok := retryAfter(resp.Header.Get("Retry-After"), time.Now()); ok {
				c.cooldown.set(time.Now().Add(wait))
			}
		}
		if attempt >= attempts || !retryable(req, resp, err, c.retry.RefusedOnly, notConnected(err, getConn.Load(), gotConn.Load())) {
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

// notConnected reports whether err is a failure to OBTAIN a connection (a dial
// error, a TLS handshake timeout, a proxy CONNECT failure): net/http began to
// get one (getConn) and never had one (gotConn), so not a byte of the request
// was written and the server cannot have acted on it, whatever the method. A
// certificate failure is excluded: it is permanent, not transient. Anything
// else (a request that was written, a failure on a reused connection, an
// injected transport) is unknown.
func notConnected(err error, getConn, gotConn bool) bool {
	if err == nil || !getConn || gotConn {
		return false
	}
	var certErr *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	var hostname x509.HostnameError
	if errors.As(err, &certErr) || errors.As(err, &unknownAuthority) || errors.As(err, &invalid) || errors.As(err, &hostname) {
		return false
	}
	return true
}

func retryable(req *http.Request, resp *http.Response, err error, refusedOnly, notConnected bool) bool {
	// A body that cannot be rewound cannot be sent twice.
	if req.Body != nil && req.GetBody == nil {
		return false
	}
	if req.Context().Err() != nil {
		return false
	}
	if err != nil {
		if refusedOnly {
			return false
		}
		// notConnected: nothing was sent, safe for every method. Otherwise the
		// outcome is unknown, so only a method that is idempotent, or a call the
		// caller marked idempotent (WithIdempotent), is repeated.
		return notConnected || idempotent(req.Method) || idempotentCall(req.Context())
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
	return resp.StatusCode >= 500 && (idempotent(req.Method) || idempotentCall(req.Context()))
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

// cooldown is the moment before which a client should not send: set from a
// rate-limit lockout's Retry-After and shared by every clone of the client.
type cooldown struct{ until atomic.Int64 } // unix nanoseconds

func (c *cooldown) set(until time.Time) {
	if c == nil {
		return
	}
	for {
		current := c.until.Load()
		if until.UnixNano() <= current || c.until.CompareAndSwap(current, until.UnixNano()) {
			return
		}
	}
}

// wait sleeps until the cooldown ends, unless that is longer than maxDelay: a
// lockout that long (Forward allows up to 60 minutes) is not slept through
// silently; the request is sent and Forward answers it at once with the 429
// and its Retry-After, for the caller to decide on.
func (c *cooldown) wait(ctx context.Context, maxDelay time.Duration) error {
	if c == nil {
		return nil
	}
	remaining := time.Until(time.Unix(0, c.until.Load()))
	if remaining <= 0 || remaining > maxDelay {
		return nil
	}
	return waitOrCancel(ctx, remaining)
}

// isRateLimitBody reports whether a 429 is Forward's rate limiter ("Rate limit
// exceeded, ...") rather than another 429 (the one-active-chat refusal), without
// consuming the body: what was read is put back in front of the rest.
func isRateLimitBody(resp *http.Response) bool {
	head, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	resp.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(head), resp.Body), resp.Body}
	return isRateLimitMessage(head)
}

// DefaultRetryPolicy is a sensible opt-in policy for a script or service: five
// attempts, exponential backoff from one second (doubling, capped at two
// minutes) with half jitter so clients refused together do not retry together,
// and a server's Retry-After honored exactly. It retries 429 and 503 for any
// method, a failure to obtain a connection for any method, and other transport
// errors and 5xx only for idempotent methods (see RetryPolicy). A rate-limit
// lockout longer than MaxDelay is returned to the caller (RateLimitLockout)
// instead of slept through.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{MaxAttempts: 5, Delay: time.Second, MaxDelay: 2 * time.Minute, Jitter: 0.5}
}

// isRateLimitMessage is the one test for Forward's limiter 429: the JSON
// message begins "Rate limit exceeded" (RateDetectionFilter and
// SessionRateLimitFilter). errors.go classifies on the same text.
func isRateLimitMessage(body []byte) bool {
	var parsed struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &parsed) != nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(parsed.Message), "Rate limit exceeded")
}

type idempotentKey struct{}

// WithIdempotent marks every request made with the returned context as safe to
// repeat, whatever its method: the caller asserts that applying it twice has
// the effect of applying it once (a PATCH that sets a field to a value, a
// PatchMany with the same patch). The retry policy then treats it like a GET or
// PUT, so a transport error after the request was written, a 502, a 504 or
// another 5xx is retried too, where it otherwise would not be because the
// outcome is unknown. It changes nothing else: it does not turn retries on
// (Config.Retry still decides), and 501 is never retried. Only claim it for a
// call that really is idempotent: a repeated POST that creates something is
// not. The mark covers every request made with the context, so do not put it
// on a composite call that also creates something (a read followed by a create).
func WithIdempotent(ctx context.Context) context.Context {
	return context.WithValue(ctx, idempotentKey{}, true)
}

func idempotentCall(ctx context.Context) bool {
	marked, _ := ctx.Value(idempotentKey{}).(bool)
	return marked
}
