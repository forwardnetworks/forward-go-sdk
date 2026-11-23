package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// SnapshotProgress returns how a snapshot's collection ran over time: queued,
// running and finished subtasks, and the concurrency in use against its limit,
// at each sample. GET /api/collector-tasks?snapshotId=
// (CollectorTaskController.getSnapshotTaskProgress; VIEW_NETWORK_AND_SNAPSHOTS,
// snapshot at the START stage). Preview: not in the published spec.
func (s *CollectorTasks) SnapshotProgress(ctx context.Context, snapshotID string) (*ports.SnapshotTaskProgress, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID == "" {
		return nil, nil, errors.New("forward: snapshot ID is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/collector-tasks?"+url.Values{"snapshotId": []string{snapshotID}}.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "CollectorTasks.SnapshotProgress")
	out := new(ports.SnapshotTaskProgress)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// SubTasksAt returns the subtasks of a task that were in progress at an
// instant -- started by then and not finished before it -- oldest start first.
// It reads every subtask, with no cap, so it stays complete on a collection of
// tens of thousands of devices. GET /api/collector-tasks/{taskId}?view=subtasks
// &at= (VIEW_COLLECTOR_TASK_QUEUES plus permission to view the task). Preview.
func (s *CollectorTasks) SubTasksAt(ctx context.Context, taskID string, at time.Time) ([]ports.CollectorSubTask, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if at.IsZero() {
		return nil, nil, errors.New("forward: the instant to read subtasks at is required")
	}
	path, err := collectorTaskPath(taskID)
	if err != nil {
		return nil, nil, err
	}
	query := url.Values{"view": []string{"subtasks"}, "at": []string{at.UTC().Format(time.RFC3339Nano)}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "CollectorTasks.SubTasksAt")
	var out []ports.CollectorSubTask
	resp, err := s.client.doRequired(req, &out)
	return out, resp, err
}

// GetWithSubTasks returns a task with its subtasks. Forward returns at most
// maxSubTasks of them (default 1000 when 0): the RUNNING, FAILED, TIMED_OUT
// and CANCELED ones first, then SUCCEEDED ones to fill the rest, and QUEUED
// ones never. So a short list is a complete picture of what went wrong, but
// not of what succeeded; raise maxSubTasks past the device count for every
// device's start and finish. GET /api/collector-tasks/{taskId}?for=ui&max=
// (VIEW_COLLECTOR_TASK_QUEUES plus permission to view the task). Preview.
func (s *CollectorTasks) GetWithSubTasks(ctx context.Context, taskID string, maxSubTasks int) (*ports.CollectorTaskWithSubTasks, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if maxSubTasks < 0 {
		return nil, nil, errors.New("forward: subtask limit must not be negative")
	}
	path, err := collectorTaskPath(taskID)
	if err != nil {
		return nil, nil, err
	}
	query := url.Values{"for": []string{"ui"}}
	if maxSubTasks != 0 {
		query.Set("max", strconv.Itoa(maxSubTasks))
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "CollectorTasks.GetWithSubTasks")
	out := new(ports.CollectorTaskWithSubTasks)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}
