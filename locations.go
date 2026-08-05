package forward

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

type LocationsService service

type Location struct {
	ID            Identifier `json:"id"`
	Name          string     `json:"name"`
	Lat           float64    `json:"lat,omitempty"`
	Lng           float64    `json:"lng,omitempty"`
	City          string     `json:"city,omitempty"`
	AdminDivision string     `json:"adminDivision,omitempty"`
	Country       string     `json:"country,omitempty"`
	DeviceGlobs   []string   `json:"deviceGlobs,omitempty"`
}

type LocationCreateRequest struct {
	ID            *string `json:"id"`
	Name          string  `json:"name"`
	Lat           float64 `json:"lat"`
	Lng           float64 `json:"lng"`
	City          string  `json:"city,omitempty"`
	AdminDivision string  `json:"adminDivision,omitempty"`
	Country       string  `json:"country,omitempty"`
}

type AtlasPatch map[string]string

type DeviceCluster struct {
	Name    string   `json:"name"`
	Devices []string `json:"devices"`
}

type DeviceClusterPatch struct {
	Name    *string  `json:"name,omitempty"`
	Devices []string `json:"devices,omitempty"`
}

func (s *LocationsService) List(ctx context.Context, networkID string) ([]Location, *Response, error) {
	base, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[Location]{Keys: []string{"items", "data", "results", "locations"}, AllowSingle: true}
	req, err := s.client.NewRequest(ctx, http.MethodGet, base, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Locations.List")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

func (s *LocationsService) Create(ctx context.Context, networkID string, input LocationCreateRequest) (*Location, *Response, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return nil, nil, errors.New("forward: location name is required")
	}
	base, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, base, input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Locations.Create")
	out := new(Location)
	response, err := s.client.Do(req, out)
	if err == nil && out.ID == "" {
		return nil, response, nil
	}
	return out, response, err
}

func (s *LocationsService) Assign(ctx context.Context, networkID string, patch AtlasPatch) (*Response, error) {
	base, err := s.base(networkID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, strings.TrimSuffix(base, "/locations")+"/atlas", patch)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Locations.Assign")
	return s.client.Do(req, nil)
}

func (s *LocationsService) ListClusters(ctx context.Context, networkID, locationID string) ([]DeviceCluster, *Response, error) {
	path, err := s.clusterBase(networkID, locationID)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[DeviceCluster]{Keys: []string{"clusters", "items", "data", "results"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Locations.ListClusters")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

func (s *LocationsService) CreateCluster(ctx context.Context, networkID, locationID string, input DeviceCluster) (*Response, error) {
	path, err := s.clusterBase(networkID, locationID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Name) == "" {
		return nil, errors.New("forward: cluster name is required")
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, input)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Locations.CreateCluster")
	return s.client.Do(req, nil)
}

func (s *LocationsService) PatchCluster(ctx context.Context, networkID, locationID, clusterName string, patch DeviceClusterPatch) (*Response, error) {
	path, err := s.clusterBase(networkID, locationID)
	if err != nil {
		return nil, err
	}
	clusterName = strings.TrimSpace(clusterName)
	if clusterName == "" {
		return nil, errors.New("forward: cluster name is required")
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path+"/"+url.PathEscape(clusterName), patch)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Locations.PatchCluster")
	return s.client.Do(req, nil)
}

func (s *LocationsService) base(networkID string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	return "/api/networks/" + url.PathEscape(networkID) + "/locations", nil
}

func (s *LocationsService) clusterBase(networkID, locationID string) (string, error) {
	base, err := s.base(networkID)
	if err != nil {
		return "", err
	}
	locationID = strings.TrimSpace(locationID)
	if locationID == "" {
		return "", errors.New("forward: location ID is required")
	}
	return base + "/" + url.PathEscape(locationID) + "/clusters", nil
}
