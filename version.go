package forward

import (
	"context"
	"net/http"
)

// VersionService accesses Forward API version information.
type VersionService service

// APIVersion is the version reported by the Forward appliance.
type APIVersion struct {
	Build   string `json:"build"`
	Release string `json:"release"`
	Version string `json:"version"`
}

// Get returns the appliance's current API version.
func (s *VersionService) Get(ctx context.Context) (*APIVersion, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/version", nil)
	if err != nil {
		return nil, nil, err
	}

	version := new(APIVersion)
	resp, err := s.client.Do(req, version)
	if err != nil {
		return nil, resp, err
	}
	s.client.recordVersion(version)
	return version, resp, nil
}
