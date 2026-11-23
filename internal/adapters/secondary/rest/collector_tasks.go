package rest

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// CollectorTasksService starts, observes, and stops collection work.
type CollectorTasks service

func (s *CollectorTasks) List(ctx context.Context, options ports.CollectorTaskListOptions) ([]ports.CollectorTask, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	query := url.Values{}
	for _, status := range options.Statuses {
		query.Add("status", status)
	}
	if options.Limit != nil {
		query.Set("limit", strconv.FormatInt(int64(*options.Limit), 10))
	}
	if networkID := strings.TrimSpace(options.NetworkID); networkID != "" {
		query.Set("networkId", networkID)
	}
	path := "/api/collector-tasks"
	if len(query) != 0 {
		path += "?" + query.Encode()
	}
	result := listResponse[ports.CollectorTask]{Keys: []string{"tasks", "items"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.client.Do(req, &result)
	if err != nil {
		return nil, resp, err
	}
	if networkID := strings.TrimSpace(options.NetworkID); networkID != "" {
		filtered := make([]ports.CollectorTask, 0, len(result.Items))
		for _, task := range result.Items {
			if string(task.NetworkID) == networkID {
				filtered = append(filtered, task)
			}
		}
		result.Items = filtered
	}
	return result.Items, resp, nil
}

// Start begins a network collection and returns its durable task identifier.
func (s *CollectorTasks) Start(ctx context.Context, networkID string) (string, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return "", nil, err
	}
	var err error
	if networkID, err = s.client.resolveNetworkID(networkID); err != nil {
		return "", nil, err
	}
	query := url.Values{"networkId": []string{networkID}, "type": []string{"NETWORK_COLLECTION"}}
	req, err := s.client.NewRequest(ctx, http.MethodPost, "/api/collector-tasks?"+query.Encode(), nil)
	if err != nil {
		return "", nil, err
	}
	var result struct {
		TaskID ports.Identifier `json:"taskId"`
		ID     ports.Identifier `json:"id"`
	}
	resp, err := s.client.Do(req, &result)
	if result.TaskID == "" {
		result.TaskID = result.ID
	}
	if err == nil && result.TaskID == "" {
		err = errors.New("forward: collection start returned no task ID")
	}
	return result.TaskID.String(), resp, err
}

// Progress reports active network collection work using the network-filtered
// collector-task route present on some appserver builds.
func (s *CollectorTasks) Progress(ctx context.Context, networkID string) (*ports.CollectionProgress, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if err := s.client.requireCapability(ports.CapabilityCollectorProgress); err != nil {
		return nil, nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	query := url.Values{"networkId": []string{networkID}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/collector-tasks?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.CollectorTask]{Keys: []string{"tasks", "items"}}
	response, err := s.client.Do(req, &result)
	if err != nil {
		return nil, response, err
	}
	progress := &ports.CollectionProgress{}
	for _, task := range result.Items {
		if task.NetworkID != "" && string(task.NetworkID) != networkID {
			continue
		}
		if !collectorTaskActive(task.Status) {
			continue
		}
		progress.InProgress = true
		progress.Total += progressInteger(task.Progress, "total")
		progress.Active += progressInteger(task.Progress, "queued") + progressInteger(task.Progress, "running")
		progress.Finished += progressInteger(task.Progress, "succeeded") + progressInteger(task.Progress, "failed") +
			progressInteger(task.Progress, "timedOut") + progressInteger(task.Progress, "canceled")
	}
	s.client.observeCapability(ports.CapabilityCollectorProgress)
	return progress, response, nil
}

func (s *CollectorTasks) Get(ctx context.Context, taskID string) (*ports.CollectorTask, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := collectorTaskPath(taskID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	task := new(ports.CollectorTask)
	resp, err := s.client.Do(req, task)
	return task, resp, err
}

// Stop cancels or skips an in-progress task. SKIP creates a snapshot from data
// already collected; CANCEL does not.
func (s *CollectorTasks) Stop(ctx context.Context, taskID, action, note string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := collectorTaskPath(taskID)
	if err != nil {
		return nil, err
	}
	action = strings.ToUpper(strings.TrimSpace(action))
	if action != "CANCEL" && action != "SKIP" {
		return nil, errors.New("forward: collector task action must be CANCEL or SKIP")
	}
	query := url.Values{"action": []string{action}}
	if note = strings.TrimSpace(note); note != "" {
		query.Set("note", note)
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path+"?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, nil)
}

func collectorTaskPath(taskID string) (string, error) {
	if taskID = strings.TrimSpace(taskID); taskID == "" {
		return "", errors.New("forward: collector task ID is required")
	}
	return "/api/collector-tasks/" + url.PathEscape(taskID), nil
}

func collectorTaskActive(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case ports.CollectorTaskQueued, ports.CollectorTaskRunning:
		return true
	default:
		return false
	}
}

func progressInteger(values map[string]any, key string) int {
	if values == nil {
		return 0
	}
	switch value := values[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case int64:
		return int(value)
	case json.Number:
		number, err := value.Int64()
		if err == nil {
			return int(number)
		}
	}
	return 0
}
