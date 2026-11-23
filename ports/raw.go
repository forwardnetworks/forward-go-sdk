package ports

import (
	"io"
	"net/http"
)

// RawRequest describes an arbitrary in-scope Forward API request. Path must be
// relative to the configured appliance and begin with /api.
type RawRequest struct {
	Method  string
	Path    string
	Headers http.Header
	Body    io.Reader
}
