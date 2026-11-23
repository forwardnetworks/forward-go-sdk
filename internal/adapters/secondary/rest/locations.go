package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
)

type Locations service

func (s *Locations) List(ctx context.Context, networkID string) ([]ports.Location, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	base, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.Location]{Keys: []string{"items", "data", "results", "locations"}, AllowSingle: true}
	req, err := s.client.NewRequest(ctx, http.MethodGet, base, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Locations.List")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

func (s *Locations) Create(ctx context.Context, networkID string, input ports.LocationCreateRequest) (*ports.Location, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	out := new(ports.Location)
	response, err := s.client.Do(req, out)
	if err == nil && out.ID == "" {
		return nil, response, nil
	}
	return out, response, err
}

// Assign moves devices to locations in one atlas patch (PATCH .../atlas; the map is device name to location id). Forward refuses the WHOLE
// patch with a 400 "Unrecognized devices cannot be moved: [r1, ...]" (ErrUnrecognizedDevices, ErrorResponse.UnrecognizedDevices) when
// any device is not in the network's classic-device list or atlas, even if it is in every snapshot, as for a network fed only by snapshot
// uploads. Seen live on 26.10.6 (the AutoCon6 workshop): create each such device with ClassicDevices.Create (collect false, no
// credentials) and assign again.
func (s *Locations) Assign(ctx context.Context, networkID string, patch ports.AtlasPatch) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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

func (s *Locations) ListClusters(ctx context.Context, networkID, locationID string) ([]ports.DeviceCluster, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.clusterBase(networkID, locationID)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.DeviceCluster]{Keys: []string{"clusters", "items", "data", "results"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Locations.ListClusters")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

func (s *Locations) CreateCluster(ctx context.Context, networkID, locationID string, input ports.DeviceCluster) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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

func (s *Locations) PatchCluster(ctx context.Context, networkID, locationID, clusterName string, patch ports.DeviceClusterPatch) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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

// DeleteCluster removes a device cluster from a location; one that is already gone counts as success. A 404 that
// means the route is not served (ErrEndpointNotServed) is still an error.
// DELETE /api/networks/{networkId}/locations/{locationId}/clusters/{clusterName} (LocationController.deleteDeviceCluster).
func (s *Locations) DeleteCluster(ctx context.Context, networkID, locationID, clusterName string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := s.clusterBase(networkID, locationID)
	if err != nil {
		return nil, err
	}
	if clusterName = strings.TrimSpace(clusterName); clusterName == "" {
		return nil, errors.New("forward: cluster name is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path+"/"+url.PathEscape(clusterName), nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Locations.DeleteCluster")
	response, err := s.client.Do(req, nil)
	if ports.IsGone(err) {
		return response, nil
	}
	return response, err
}

func (s *Locations) base(networkID string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	return "/api/networks/" + url.PathEscape(networkID) + "/locations", nil
}

func (s *Locations) clusterBase(networkID, locationID string) (string, error) {
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

func (s *Locations) locationPath(networkID, locationID string) (string, error) {
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
func (s *Locations) Get(ctx context.Context, networkID, locationID string) (*ports.Location, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.locationPath(networkID, locationID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Locations.Get")
	out := new(ports.Location)
	resp, err := s.client.doRequired(req, out)
	if ports.IsStatus(err, http.StatusNotFound) {
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
func (s *Locations) Patch(ctx context.Context, networkID, locationID string, patch ports.LocationPatch) (*ports.Location, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	body, err := patch.Body()
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
	out := new(ports.Location)
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
func (s *Locations) Delete(ctx context.Context, networkID, locationID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
	if ports.IsGone(err) {
		return resp, nil
	}
	return resp, err
}

// Atlas returns the network's device atlas: which location each located device is in, as device name to
// location ID, in Forward's name order. Cloud locations and devices with no fixed location are not in it.
// GET /api/networks/{networkId}/atlas (getAtlas, published; VIEW_NETWORK_AND_SNAPSHOTS). Despite the name
// this is a device-to-location map, not a geographic database: Forward has no city or coordinate lookup.
func (s *Locations) Atlas(ctx context.Context, networkID string) (map[string]string, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	base, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, strings.TrimSuffix(base, "/locations")+"/atlas", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Locations.Atlas")
	out := map[string]string{}
	resp, err := s.client.doRequired(req, &out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// AtlasByLocation returns the atlas grouped by location, telling apart devices placed by hand, by anchoring
// and by a deviceGlobs match, in Forward's location-ID order. It is the way to confirm that a location's
// DeviceGlobs put the devices you expect there. Empty locations are omitted, and so are cloud locations and
// devices with no fixed location. GET /api/networks/{networkId}/atlas?v=2 (VIEW_NETWORK_AND_SNAPSHOTS).
// Unpublished: not in Forward's OpenAPI set.
func (s *Locations) AtlasByLocation(ctx context.Context, networkID string) ([]ports.AtlasLocation, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	base, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, strings.TrimSuffix(base, "/locations")+"/atlas?v=2", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Locations.AtlasByLocation")
	var out struct {
		Locations []ports.AtlasLocation `json:"locations"`
	}
	resp, err := s.client.doRequired(req, &out)
	if err != nil {
		return nil, resp, err
	}
	return out.Locations, resp, nil
}
