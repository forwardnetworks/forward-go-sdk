package forward

import (
	"errors"

	"net/http"

	"sync/atomic"
)

type failingTransport struct{ calls atomic.Int32 }

func (f *failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	f.calls.Add(1)
	return nil, errors.New("injected transport failure")
}
