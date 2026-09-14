package forward

import (
	"context"
	"io"
	"net/http"
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
}

const defaultRetryDelay = 500 * time.Millisecond

// send performs one request, retrying while the policy allows and the failure
// looks transient.
//
// What is safe to repeat depends on the method. A 429 or a gateway status says
// the appserver never processed the request, so any method may be repeated. A
// transport error or a plain 500 leaves the outcome unknown, so only an
// idempotent method is repeated -- replaying a POST could create a second
// snapshot, or a second change set, for a request that already succeeded.
func (c *Client) send(req *http.Request) (*http.Response, error) {
	attempts := c.retry.MaxAttempts
	if attempts < 1 {
		attempts = 1
	}
	delay := c.retry.Delay
	if delay <= 0 {
		delay = defaultRetryDelay
	}

	for attempt := 1; ; attempt++ {
		resp, err := c.httpClient.Do(req)
		if attempt >= attempts || !retryable(req, resp, err) {
			return resp, err
		}
		if resp != nil {
			// Draining lets the connection be reused rather than abandoned.
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		if err := waitOrCancel(req.Context(), delay<<uint(attempt-1)); err != nil {
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

func retryable(req *http.Request, resp *http.Response, err error) bool {
	// A body that cannot be rewound cannot be sent twice.
	if req.Body != nil && req.GetBody == nil {
		return false
	}
	if req.Context().Err() != nil {
		return false
	}
	if err != nil {
		return idempotent(req.Method)
	}
	switch resp.StatusCode {
	case http.StatusTooManyRequests, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	case http.StatusNotImplemented:
		// Permanent: the route is absent from this build, not overloaded.
		return false
	}
	return resp.StatusCode >= 500 && idempotent(req.Method)
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
