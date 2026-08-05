package forward

import (
	"context"
	"io"
	"net/http"
)

// RawService provides a forward-compatible binding for every /api route,
// including endpoints and fields not yet represented by a typed service.
type RawService service

// RawRequest describes an arbitrary in-scope Forward API request. Path must be
// relative to the configured appliance and begin with /api.
type RawRequest struct {
	Method  string
	Path    string
	Headers http.Header
	Body    io.Reader
}

// Do executes an arbitrary Forward API request and decodes dst using the same
// rules as Client.Do.
func (s *RawService) Do(ctx context.Context, request RawRequest, dst any) (*Response, error) {
	req, err := s.client.NewRequest(ctx, request.Method, request.Path, request.Body)
	if err != nil {
		return nil, err
	}
	for key, values := range request.Headers {
		req.Header.Del(key)
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	return s.client.Do(req, dst)
}

// DoJSON JSON-encodes payload and executes an arbitrary Forward API request.
func (s *RawService) DoJSON(
	ctx context.Context,
	method string,
	path string,
	payload any,
	dst any,
) (*Response, error) {
	req, err := s.client.newJSONRequest(ctx, method, path, payload)
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, dst)
}
