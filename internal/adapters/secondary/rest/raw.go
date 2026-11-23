package rest

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/ports"
)

// RawService provides a forward-compatible binding for every /api route,
// including endpoints and fields not yet represented by a typed service.
type Raw service

// Do executes an arbitrary Forward API request and decodes dst using the same
// rules as Client.Do.
func (s *Raw) Do(ctx context.Context, request ports.RawRequest, dst any) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
func (s *Raw) DoJSON(
	ctx context.Context,
	method string,
	path string,
	payload any,
	dst any,
) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, method, path, payload)
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, dst)
}
