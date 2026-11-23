package rest

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
)

// CollectionsService reads per-device collection status.
//
// It also carried List (GET /networks/{id}/collections) and Progress
// (GET /networks/{id}/collectionProgress). Neither route exists on the
// primary 15398425a69 or stable 67e89c87124 builds; both 404'd for every
// network. Collection progress is CollectorTasks.List / CollectorTasks.Progress
// (GET /api/collector-tasks).
type Collections service

func (s *Collections) DeviceStatuses(ctx context.Context, networkID string) ([]ports.DeviceCollectionStatus, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := collectionsPath(s.client, networkID, "/device-statuses")
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.DeviceCollectionStatus]{Keys: []string{"deviceStatuses", "items", "data", "results"}, AllowSingle: true}
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
