package rest

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
)

// CollectionSchedulesService reads and writes a network's collection schedules: when
// Forward starts collections on its own. The published Collection Schedules
// API (collection-schedules.yaml), served on primary 15398425a69 and stable
// 67e89c87124.
type CollectionSchedules service

// List returns the network's collection schedules. GET
// /api/networks/{id}/collection-schedules (getCollectionSchedules).
func (s *CollectionSchedules) List(ctx context.Context, networkID string) ([]ports.CollectionSchedule, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/networks/"+url.PathEscape(networkID)+"/collection-schedules", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "CollectionSchedules.List")
	result := listResponse[ports.CollectionSchedule]{Keys: []string{"schedules"}}
	resp, err := s.client.doRequired(req, &result)
	return result.Items, resp, err
}

// Get returns one collection schedule, or (nil, nil) when it does not exist.
// GET /api/networks/{id}/collection-schedules/{scheduleId}
// (getCollectionSchedule).
func (s *CollectionSchedules) Get(ctx context.Context, networkID, scheduleID string) (*ports.CollectionSchedule, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	if scheduleID = strings.TrimSpace(scheduleID); scheduleID == "" {
		return nil, nil, errors.New("forward: collection schedule ID is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/networks/"+url.PathEscape(networkID)+"/collection-schedules/"+url.PathEscape(scheduleID), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "CollectionSchedules.Get")
	out := new(ports.CollectionSchedule)
	resp, err := s.client.doRequired(req, out)
	if ports.IsStatus(err, http.StatusNotFound) {
		return nil, resp, nil
	}
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

func (s *CollectionSchedules) schedulePath(networkID, scheduleID string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	path := "/api/networks/" + url.PathEscape(networkID) + "/collection-schedules"
	if scheduleID == "" {
		return path, nil
	}
	return path + "/" + url.PathEscape(scheduleID), nil
}

// Create adds a collection schedule to the network and returns it with the ID
// Forward gave it. POST /api/networks/{id}/collection-schedules
// (addCollectionSchedule; EDIT_COLLECTION_SCHEDULES). Forward answers 400 for a
// duplicate of an existing schedule. A network can have several schedules.
func (s *CollectionSchedules) Create(ctx context.Context, networkID string, definition ports.CollectionScheduleDefinition) (*ports.CollectionSchedule, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	body, err := definition.Body("")
	if err != nil {
		return nil, nil, err
	}
	path, err := s.schedulePath(networkID, "")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, json.RawMessage(body))
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "CollectionSchedules.Create")
	out := new(ports.CollectionSchedule)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// Replace overwrites the schedule with schedule.ID by the given definition: it
// replaces, not merges, so every field is restated. To change one field, read
// the schedule, edit schedule.Definition() and send it back. PUT
// /api/networks/{id}/collection-schedules/{scheduleId} (replaceCollectionSchedule;
// EDIT_COLLECTION_SCHEDULES). Forward requires the ID in the path and in the
// body to match, which this does by construction, and answers 400 for a
// duplicate of another schedule.
func (s *CollectionSchedules) Replace(ctx context.Context, networkID, scheduleID string, definition ports.CollectionScheduleDefinition) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if scheduleID = strings.TrimSpace(scheduleID); scheduleID == "" {
		return nil, errors.New("forward: collection schedule ID is required")
	}
	body, err := definition.Body(scheduleID)
	if err != nil {
		return nil, err
	}
	path, err := s.schedulePath(networkID, scheduleID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPut, path, json.RawMessage(body))
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "CollectionSchedules.Replace")
	return s.client.Do(req, nil)
}

// Delete removes a collection schedule; the network then collects only on the
// schedules it has left, or never if it had one. A schedule that does not
// exist counts as success. DELETE
// /api/networks/{id}/collection-schedules/{scheduleId}
// (deleteCollectionSchedule; EDIT_COLLECTION_SCHEDULES; 204).
func (s *CollectionSchedules) Delete(ctx context.Context, networkID, scheduleID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if scheduleID = strings.TrimSpace(scheduleID); scheduleID == "" {
		// Never collapse onto the collection route.
		return nil, errors.New("forward: collection schedule ID is required")
	}
	path, err := s.schedulePath(networkID, scheduleID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "CollectionSchedules.Delete")
	resp, err := s.client.Do(req, nil)
	if ports.IsGone(err) {
		return resp, nil
	}
	return resp, err
}
