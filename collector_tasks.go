package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// CollectorTasksService starts, observes, and stops collection work.
type CollectorTasksService service

const (
	CollectorTaskQueued    = "QUEUED"
	CollectorTaskRunning   = "RUNNING"
	CollectorTaskSucceeded = "SUCCEEDED"
	CollectorTaskFailed    = "FAILED"
	CollectorTaskTimedOut  = "TIMED_OUT"
	CollectorTaskCanceled  = "CANCELED"
)

// CollectorTask is the durable state of a collector operation.
type CollectorTask struct {
	ID           Identifier     `json:"id"`
	Type         string         `json:"type"`
	Status       string         `json:"status"`
	NetworkID    Identifier     `json:"networkId"`
	NetworkName  string         `json:"networkName,omitempty"`
	Note         string         `json:"note,omitempty"`
	CreatedByID  Identifier     `json:"createdById,omitempty"`
	CreatedBy    string         `json:"createdBy,omitempty"`
	CreatedAt    string         `json:"createdAt,omitempty"`
	StartedAt    string         `json:"startedAt,omitempty"`
	FinishedAt   string         `json:"finishedAt,omitempty"`
	CanceledByID Identifier     `json:"canceledById,omitempty"`
	CanceledBy   string         `json:"canceledBy,omitempty"`
	Progress     map[string]any `json:"progress,omitempty"`
}

// CollectionProgress summarizes active collector work for one network.
// InProgress is authoritative; counts are zero when a build omits progress.
type CollectionProgress struct {
	InProgress bool
	Total      int
	Finished   int
	Active     int
}

// CollectorTaskListOptions filters recent tasks.
type CollectorTaskListOptions struct {
	// NetworkID is a best-effort client-side filter because the published list
	// endpoint is organization-wide. The server applies Limit before this
	// filter, so busy organizations can have older matching tasks outside the
	// returned window.
	NetworkID string
	Statuses  []string
	Limit     *int32
}

func (s *CollectorTasksService) List(ctx context.Context, options CollectorTaskListOptions) ([]CollectorTask, *Response, error) {
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
	result := listResponse[CollectorTask]{Keys: []string{"tasks", "items"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.client.Do(req, &result)
	if err != nil {
		return nil, resp, err
	}
	if networkID := strings.TrimSpace(options.NetworkID); networkID != "" {
		filtered := make([]CollectorTask, 0, len(result.Items))
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
func (s *CollectorTasksService) Start(ctx context.Context, networkID string) (string, *Response, error) {
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
		TaskID Identifier `json:"taskId"`
		ID     Identifier `json:"id"`
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
func (s *CollectorTasksService) Progress(ctx context.Context, networkID string) (*CollectionProgress, *Response, error) {
	if err := s.client.requireCapability(CapabilityCollectorProgress); err != nil {
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
	result := listResponse[CollectorTask]{Keys: []string{"tasks", "items"}}
	response, err := s.client.Do(req, &result)
	if err != nil {
		return nil, response, err
	}
	progress := &CollectionProgress{}
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
	s.client.observeCapability(CapabilityCollectorProgress)
	return progress, response, nil
}

func (s *CollectorTasksService) Get(ctx context.Context, taskID string) (*CollectorTask, *Response, error) {
	path, err := collectorTaskPath(taskID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	task := new(CollectorTask)
	resp, err := s.client.Do(req, task)
	return task, resp, err
}

// StartOperation starts collection and returns a handle retaining its latest
// task state. Call Wait with a callback to drive logs, UI, or Terraform
// diagnostics.
func (s *CollectorTasksService) StartOperation(ctx context.Context, networkID string) (*Poller[CollectorTask], *Response, error) {
	taskID, response, err := s.Start(ctx, networkID)
	if err != nil {
		return nil, response, err
	}
	initial := &CollectorTask{ID: Identifier(taskID), Type: "NETWORK_COLLECTION", Status: CollectorTaskQueued, NetworkID: Identifier(strings.TrimSpace(networkID))}
	poller, err := NewPoller(initial, func(ctx context.Context) (*CollectorTask, *Response, error) {
		return s.Get(ctx, taskID)
	}, collectorTaskDone)
	return poller, response, err
}

// Stop cancels or skips an in-progress task. SKIP creates a snapshot from data
// already collected; CANCEL does not.
func (s *CollectorTasksService) Stop(ctx context.Context, taskID, action, note string) (*Response, error) {
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

func collectorTaskDone(task *CollectorTask) (bool, error) {
	if task == nil {
		return false, errors.New("forward: collector task state is nil")
	}
	switch task.Status {
	case CollectorTaskSucceeded:
		return true, nil
	case CollectorTaskFailed, CollectorTaskTimedOut, CollectorTaskCanceled:
		return true, fmt.Errorf("forward: collector task %s ended with status %s", task.ID, task.Status)
	default:
		return false, nil
	}
}

func collectorTaskActive(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case CollectorTaskQueued, CollectorTaskRunning:
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
