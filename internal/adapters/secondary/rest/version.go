package rest

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
)

// VersionService accesses Forward API version information.
type Version service

// Get returns the appliance's current API version.
func (s *Version) Get(ctx context.Context) (*ports.APIVersion, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/version", nil)
	if err != nil {
		return nil, nil, err
	}

	version := new(ports.APIVersion)
	resp, err := s.client.Do(req, version)
	if err != nil {
		return nil, resp, err
	}
	s.client.recordVersion(version)
	return version, resp, nil
}
