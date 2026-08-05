package forward

import (
	"context"
	"errors"
	"sync"
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
	Interval time.Duration
	OnUpdate func(PollUpdate[T])
}

// Poller retains the latest state of an asynchronous Forward operation. It is
// safe to inspect from another goroutine while Wait is polling.
type Poller[T any] struct {
	mu      sync.RWMutex
	current *T
	poll    func(context.Context) (*T, *Response, error)
	done    func(*T) (bool, error)
}

// NewPoller constructs an operation handle from an initial server state, a
// refresh function, and a completion predicate.
func NewPoller[T any](
	initial *T,
	poll func(context.Context) (*T, *Response, error),
	done func(*T) (bool, error),
) (*Poller[T], error) {
	if initial == nil || poll == nil || done == nil {
		return nil, errors.New("forward: poller initial state, poll function, and completion function are required")
	}
	return &Poller[T]{current: initial, poll: poll, done: done}, nil
}

// Current returns the latest state observed by this handle.
func (p *Poller[T]) Current() *T {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.current
}

// Refresh fetches and stores the latest operation state.
func (p *Poller[T]) Refresh(ctx context.Context) (*T, *Response, error) {
	if p == nil {
		return nil, nil, errors.New("forward: poller is nil")
	}
	value, response, err := p.poll(ctx)
	if err != nil {
		return nil, response, err
	}
	p.mu.Lock()
	p.current = value
	p.mu.Unlock()
	return value, response, nil
}

// Wait polls until the operation completes, fails, or ctx is canceled.
func (p *Poller[T]) Wait(ctx context.Context, options PollOptions[T]) (*T, *Response, error) {
	if p == nil {
		return nil, nil, errors.New("forward: poller is nil")
	}
	if ctx == nil {
		return nil, nil, errors.New("forward: context is nil")
	}
	interval := options.Interval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	started := time.Now()
	current := p.Current()
	complete, err := p.done(current)
	if err != nil || complete {
		return current, nil, err
	}

	timer := time.NewTimer(interval)
	defer timer.Stop()
	for attempt := 1; ; attempt++ {
		select {
		case <-ctx.Done():
			return p.Current(), nil, ctx.Err()
		case <-timer.C:
		}
		previous := current
		value, response, err := p.Refresh(ctx)
		if err != nil {
			return p.Current(), response, err
		}
		if options.OnUpdate != nil {
			options.OnUpdate(PollUpdate[T]{Previous: previous, Value: value, Response: response, Attempt: attempt, Elapsed: time.Since(started)})
		}
		complete, err = p.done(value)
		if err != nil || complete {
			return value, response, err
		}
		current = value
		timer.Reset(interval)
	}
}
