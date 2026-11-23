package ports

import (
	"time"
)

// PollUpdate describes one state transition observed by a Poller.
type PollUpdate[T any] struct {
	Previous *T
	Value    *T
	Response *Response
	Attempt  int
	Elapsed  time.Duration
}

// PollOptions controls asynchronous operation waiting.
type PollOptions[T any] struct {
	// Interval is the wait before the first refresh (default 2s). With MaxInterval zero it stays fixed, which is the behaviour before
	// MaxInterval existed.
	Interval time.Duration
	// MaxInterval, when above Interval, doubles the wait after every refresh until it reaches this ceiling: a geometric backoff for
	// operations that take minutes, so a long wait does not poll at the short interval throughout.
	MaxInterval time.Duration
	// Budget, when above zero, bounds the whole wait. Running out returns an error that satisfies errors.Is(err, ErrPollBudget) and
	// context.DeadlineExceeded, with the last state seen; a canceled or expired caller context still returns its own error.
	Budget   time.Duration
	OnUpdate func(PollUpdate[T])
}
