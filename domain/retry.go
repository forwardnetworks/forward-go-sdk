package domain

import (
	"errors"
)

// ErrPermanent marks a transport failure that repeating cannot fix. A
// RoundTripper wrapped under Config.HTTPClient (a guard that refuses a request
// before sending it, a policy check, a credential that will never work) returns
// an error that wraps ErrPermanent, or that has a Permanent() bool method
// answering true, and the retry loop returns it at once instead of backing off
// and trying again. errors.Is and errors.As still see the caller's own error
// through the SDK's wrapping, so a guard needs nothing else:
//
//	return nil, fmt.Errorf("%w: network not allowed: %w", ErrGuardRefused, domain.ErrPermanent)
//
// The first Permanent() method found in the error chain decides. It applies to
// every method and wins over WithIdempotent. It changes nothing
// when no retry policy is set (the zero policy retries nothing anyway).
var ErrPermanent = errors.New("forward: permanent failure, do not retry")
