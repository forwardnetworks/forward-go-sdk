package forward

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// DeviceTagsService manages network-scoped device tag definitions and
// assignments.
type DeviceTagsService service

type DeviceTag struct {
	Name    string   `json:"name"`
	Color   string   `json:"color,omitempty"`
	Devices []string `json:"devices,omitempty"`
}

// AddBatch creates tag definitions. Forward ignores definitions that already
// exist.
func (s *DeviceTagsService) AddBatch(ctx context.Context, networkID string, tags []DeviceTag) (*Response, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, err
	}
	if len(tags) == 0 {
		return nil, errors.New("forward: at least one device tag is required")
	}
	path, _ := networkPath(networkID)
	path += "/device-tags?" + url.Values{"action": []string{"addBatch"}}.Encode()
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, tags)
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, nil)
}

// AddBatchTo applies existing tag names to devices.
func (s *DeviceTagsService) AddBatchTo(ctx context.Context, networkID string, devices, tags []string) (*Response, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, err
	}
	devices = nonEmptyStrings(devices)
	tags = nonEmptyStrings(tags)
	if len(devices) == 0 || len(tags) == 0 {
		return nil, errors.New("forward: device names and tag names are required")
	}
	path, _ := networkPath(networkID)
	path += "/device-tags?" + url.Values{"action": []string{"addBatchTo"}}.Encode()
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, map[string][]string{"devices": devices, "tags": tags})
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, nil)
}

// List returns tag definitions. with is an optional appserver expansion such
// as "devices". Bare arrays and {"tags": [...]} are both accepted.
func (s *DeviceTagsService) List(ctx context.Context, networkID, with string) ([]DeviceTag, *Response, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	path, _ := networkPath(networkID)
	path += "/device-tags"
	if with = strings.TrimSpace(with); with != "" {
		path += "?" + url.Values{"with": []string{with}}.Encode()
	}
	result := listResponse[DeviceTag]{Keys: []string{"tags", "items"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	response, err := s.client.Do(req, &result)
	if err != nil {
		return nil, response, err
	}
	sort.Slice(result.Items, func(i, j int) bool { return result.Items[i].Name < result.Items[j].Name })
	return result.Items, response, nil
}
