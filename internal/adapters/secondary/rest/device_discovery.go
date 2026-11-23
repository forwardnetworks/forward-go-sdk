package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DeviceDiscoveryService starts and reads Forward's device discovery: a collector scans subnets, follows seed devices'
// neighbours, or reads cloud setups, and Forward lists what it found so it can be added as classic devices. Discovery
// finds devices; it adds nothing, and Start scans a customer network from the collector, so call it deliberately.
//
// Routes are in DeviceDiscoveryController and ScheduledDiscoveryController (published OpenAPI is not consulted for them;
// treat them as preview).
type DeviceDiscovery service

// Start begins a discovery from collectorID's collector (empty: the network's default) and returns the collector task id
// (POST ...deviceDiscovery?op=start, MANAGE_COLLECTION_SOURCES). It returns as soon as the job is queued; read the outcome with
// ListDiscovered. The task id carries a prefix ("P8781") and is not a DiscoveryJob.ID ("8781"): match the job by its StartedAt
// and Type, or take the newest. The network is scanned by that collector until the job ends or Stop is called. Forward answers
// 409 when a discovery is already running, and 400 for a type that logs in to devices when the organization's
// EXPLORATORY_LOGIN setting is off.
func (s *DeviceDiscovery) Start(ctx context.Context, networkID, collectorID string, params ports.DiscoveryParams) (ports.Identifier, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return "", nil, err
	}
	if params == nil {
		return "", nil, errors.New("forward: discovery params are required")
	}
	if err := params.Validate(); err != nil {
		return "", nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", nil, err
	}
	query := url.Values{"op": []string{"start"}}
	if collectorID = strings.TrimSpace(collectorID); collectorID != "" {
		query.Set("collectorId", collectorID)
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/networks/"+url.PathEscape(networkID)+"/deviceDiscovery?"+query.Encode(), params)
	if err != nil {
		return "", nil, err
	}
	req = markOperation(req, "DeviceDiscovery.Start")
	var out struct {
		TaskID ports.Identifier `json:"taskId"`
	}
	resp, err := s.client.doRequired(req, &out)
	return out.TaskID, resp, err
}

// Stop ends the network's running discovery (POST ...deviceDiscovery?op=stop; 204). What it already found stays listed.
func (s *DeviceDiscovery) Stop(ctx context.Context, networkID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, "/api/networks/"+url.PathEscape(networkID)+"/deviceDiscovery?op=stop", nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "DeviceDiscovery.Stop")
	return s.client.Do(req, nil)
}

// ListDiscovered returns the discovery jobs and the devices they found (GET ...discovered-devices; VIEW_NETWORK_AND_SNAPSHOTS).
// A non-zero since limits the result to what changed at or after that time (Forward's `since` instant).
func (s *DeviceDiscovery) ListDiscovered(ctx context.Context, networkID string, since time.Time) (*ports.DiscoveredDevices, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	path := "/api/networks/" + url.PathEscape(networkID) + "/discovered-devices"
	if !since.IsZero() {
		path += "?since=" + url.QueryEscape(since.UTC().Format(time.RFC3339Nano))
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "DeviceDiscovery.ListDiscovered")
	out := new(ports.DiscoveredDevices)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}

func (s *DeviceDiscovery) scheduledPath(networkID string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	return "/api/networks/" + url.PathEscape(networkID) + "/scheduled-discoveries", nil
}

// ListScheduled returns the network's scheduled discoveries (GET ...scheduled-discoveries; VIEW_COLLECTION_SETTINGS).
func (s *DeviceDiscovery) ListScheduled(ctx context.Context, networkID string) ([]ports.ScheduledDiscovery, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.scheduledPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "DeviceDiscovery.ListScheduled")
	var out []ports.ScheduledDiscovery
	resp, err := s.client.doRequired(req, &out)
	return out, resp, err
}

// GetScheduled returns one scheduled discovery by id (a 404 when there is none).
func (s *DeviceDiscovery) GetScheduled(ctx context.Context, networkID, id string) (*ports.ScheduledDiscovery, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.scheduledItemPath(networkID, id)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "DeviceDiscovery.GetScheduled")
	out := new(ports.ScheduledDiscovery)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}

// CreateScheduled adds a scheduled discovery (POST ...scheduled-discoveries; EDIT_COLLECTION_SETTINGS) and returns it with
// its id. Forward refuses (400) a name that matches an existing one ignoring case, or a schedule identical to an existing one
// (a second one with no schedule included). The scheduled scan runs from the network's default collector; Forward's scheduled-discovery params carry no
// collector. An enabled schedule scans the customer network on its own, so create it disabled unless that is intended.
func (s *DeviceDiscovery) CreateScheduled(ctx context.Context, networkID string, definition ports.ScheduledDiscoveryDefinition) (*ports.ScheduledDiscovery, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(definition.Name) == "" {
		return nil, nil, errors.New("forward: scheduled discovery name is required")
	}
	if definition.Params == nil {
		return nil, nil, errors.New("forward: scheduled discovery params are required")
	}
	if definition.Params.DiscoveryType() == "CLOUD" {
		return nil, nil, errors.New("forward: CLOUD discovery cannot be scheduled")
	}
	if err := definition.Params.Validate(); err != nil {
		return nil, nil, err
	}
	if definition.Schedule != nil {
		if err := definition.Schedule.Check(); err != nil {
			return nil, nil, err
		}
	}
	path, err := s.scheduledPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, definition)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "DeviceDiscovery.CreateScheduled")
	out := new(ports.ScheduledDiscovery)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}

// DeleteScheduled removes a scheduled discovery by id (DELETE ...scheduled-discoveries/{id}; 204). Forward answers 204 for an
// id that does not exist too, so a 204 does not prove it was there; a 404 is a missing network (or an unserved route) and is
// returned as the error, never as success, so a wrong network id cannot look like a completed delete. Discovered devices it
// already found stay listed.
func (s *DeviceDiscovery) DeleteScheduled(ctx context.Context, networkID, id string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := s.scheduledItemPath(networkID, id)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "DeviceDiscovery.DeleteScheduled")
	return s.client.Do(req, nil)
}

func (s *DeviceDiscovery) scheduledItemPath(networkID, id string) (string, error) {
	if id = strings.TrimSpace(id); id == "" {
		return "", errors.New("forward: scheduled discovery id is required")
	}
	path, err := s.scheduledPath(networkID)
	if err != nil {
		return "", err
	}
	return path + "/" + url.PathEscape(id), nil
}
