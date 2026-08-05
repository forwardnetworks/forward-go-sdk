package forward

import (
	"context"
	"net/http"
	"net/url"
)

type CollectionsService service

type LegacyCollectionOperation struct {
	ID               Identifier `json:"id,omitempty"`
	NetworkID        Identifier `json:"networkId,omitempty"`
	Type             string     `json:"type,omitempty"`
	Devices          []string   `json:"devices,omitempty"`
	StartUnixMillis  int64      `json:"startUnixMillis,omitempty"`
	FinishUnixMillis int64      `json:"finishUnixMillis,omitempty"`
}
type LegacyCollectionProgress struct {
	NetworkID    Identifier     `json:"networkId,omitempty"`
	Total        int            `json:"total"`
	Finished     int            `json:"finished"`
	Active       int            `json:"active"`
	InProgress   bool           `json:"inProgress,omitempty"`
	DevicesDone  int            `json:"devicesDone,omitempty"`
	TotalDevices int            `json:"totalDevices,omitempty"`
	ByType       map[string]int `json:"byType,omitempty"`
	ActiveByType map[string]int `json:"activeByType,omitempty"`
	SampleActive []string       `json:"sampleActive,omitempty"`
}
type DeviceCollectionStatus struct {
	Name             string `json:"name,omitempty"`
	DeviceName       string `json:"deviceName,omitempty"`
	CollectionStatus string `json:"collectionStatus,omitempty"`
	Status           string `json:"status,omitempty"`
	State            string `json:"state,omitempty"`
	CollectionFailed bool   `json:"collectionFailed,omitempty"`
	CollectionError  string `json:"collectionError,omitempty"`
}

func (s *CollectionsService) List(ctx context.Context, networkID string) ([]LegacyCollectionOperation, *Response, error) {
	path, err := collectionsPath(s.client, networkID, "/collections")
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[LegacyCollectionOperation]{Keys: []string{"collections", "items", "data", "results"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Collections.List")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}
func (s *CollectionsService) Progress(ctx context.Context, networkID string) (*LegacyCollectionProgress, *Response, error) {
	path, err := collectionsPath(s.client, networkID, "/collectionProgress")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Collections.Progress")
	out := new(LegacyCollectionProgress)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}
func (s *CollectionsService) DeviceStatuses(ctx context.Context, networkID string) ([]DeviceCollectionStatus, *Response, error) {
	path, err := collectionsPath(s.client, networkID, "/device-statuses")
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[DeviceCollectionStatus]{Keys: []string{"deviceStatuses", "items", "data", "results"}, AllowSingle: true}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Collections.DeviceStatuses")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}
func collectionsPath(client *Client, networkID, suffix string) (string, error) {
	networkID, err := client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	return "/api/networks/" + url.PathEscape(networkID) + suffix, nil
}
