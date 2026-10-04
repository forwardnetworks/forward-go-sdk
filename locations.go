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

// LocationCreateRequest is a location to add. ID is optional: absent, Forward assigns a numeric one, and a stable
// ID you choose is what lets an applier find the location again. City is required if AdminDivision or Country is
// given, and Country if City is (display only).
//
// DeviceGlobs is Forward's hidden (unpublished) dynamic assignment: device names and globs ("sjc-*") that put
// matching devices in this location. Forward recomputes the assignment after every location change; a device that
// matches globs of several locations goes to the one with the lowest id.
type LocationCreateRequest struct {
	ID            *string  `json:"id"`
	Name          string   `json:"name"`
	Lat           float64  `json:"lat"`
	Lng           float64  `json:"lng"`
	City          string   `json:"city,omitempty"`
	AdminDivision string   `json:"adminDivision,omitempty"`
	Country       string   `json:"country,omitempty"`
	DeviceGlobs   []string `json:"deviceGlobs,omitempty"`
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

// LocationPatch changes some of a location; nil fields are left alone. DeviceGlobs replaces the whole list when
// non-nil, and an empty non-nil list clears it. ID renames the location's id. Clearing City, AdminDivision or
// Country is not supported here.
type LocationPatch struct {
	ID            *string
	Name          *string
	Lat           *float64
	Lng           *float64
	City          *string
	AdminDivision *string
	Country       *string
	DeviceGlobs   []string
}

func (p LocationPatch) body() (map[string]any, error) {
	body := map[string]any{}
	set := func(key string, value *string) {
		if value != nil {
			body[key] = *value
		}
	}
	set("id", p.ID)
	set("name", p.Name)
	set("city", p.City)
	set("adminDivision", p.AdminDivision)
	set("country", p.Country)
	if p.Lat != nil {
		body["lat"] = *p.Lat
	}
	if p.Lng != nil {
		body["lng"] = *p.Lng
	}
	if p.DeviceGlobs != nil {
		body["deviceGlobs"] = p.DeviceGlobs
	}
	if len(body) == 0 {
		return nil, errors.New("forward: a location patch must change something")
	}
	if p.Name != nil && strings.TrimSpace(*p.Name) == "" {
		return nil, errors.New("forward: a location cannot be renamed to nothing")
	}
	return body, nil
}

func (s *LocationsService) locationPath(networkID, locationID string) (string, error) {
	base, err := s.base(networkID)
	if err != nil {
		return "", err
	}
	if locationID = strings.TrimSpace(locationID); locationID == "" {
		// Never collapse onto the collection route.
		return "", errors.New("forward: location ID is required")
	}
	return base + "/" + url.PathEscape(locationID), nil
}

// Get returns one location, or (nil, nil) when it does not exist. GET
// /api/networks/{networkId}/locations/{locationId} (getLocation, published;
// VIEW_NETWORK_AND_SNAPSHOTS).
func (s *LocationsService) Get(ctx context.Context, networkID, locationID string) (*Location, *Response, error) {
	path, err := s.locationPath(networkID, locationID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Locations.Get")
	out := new(Location)
	resp, err := s.client.doRequired(req, out)
	if isStatus(err, http.StatusNotFound) {
		return nil, resp, nil
	}
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// Patch changes a location and returns it. PATCH
// /api/networks/{networkId}/locations/{locationId} (patchLocation, published;
// EDIT_TOPOLOGY_LAYOUT). Forward then recomputes dynamic device assignment. This
// is the idempotent update: Get the location, compare, Patch what differs.
func (s *LocationsService) Patch(ctx context.Context, networkID, locationID string, patch LocationPatch) (*Location, *Response, error) {
	body, err := patch.body()
	if err != nil {
		return nil, nil, err
	}
	path, err := s.locationPath(networkID, locationID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, body)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Locations.Patch")
	out := new(Location)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// Delete removes a location; a location that does not exist counts as success.
// DELETE /api/networks/{networkId}/locations/{locationId} (deleteLocation,
// published; EDIT_TOPOLOGY_LAYOUT; 204). Forward recomputes dynamic assignment
// afterwards. I did not trace where devices assigned to it end up.
func (s *LocationsService) Delete(ctx context.Context, networkID, locationID string) (*Response, error) {
	path, err := s.locationPath(networkID, locationID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Locations.Delete")
	resp, err := s.client.Do(req, nil)
	if isStatus(err, http.StatusNotFound) {
		return resp, nil
	}
	return resp, err
}
