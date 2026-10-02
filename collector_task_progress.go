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
	"time"
)

// SnapshotTaskProgress is how a collection ran over time, sampled at
// Timestamps: every other slice has one entry per timestamp. Queued, Running,
// Succeeded, Failed, TimedOut and Canceled count the collection's subtasks (a
// subtask is usually one device) in that state at that moment. Concurrency is
// the concurrency slots those running subtasks hold, which the limits below
// cap. The gaps between samples are Forward's own grouping interval, chosen to
// keep the series a sensible length, so they are not fixed.
//
// Reading it: Running at ConcurrencyLimits.Global means that limit was the
// ceiling. Running well under it while Queued is high means the global limit
// was not what held work back; look at JumpServers and VCenters against their
// limits, and at what the queued subtasks are waiting for. An empty series (no
// Timestamps) is what Forward returns when it has no task record for the
// snapshot.
type SnapshotTaskProgress struct {
	Timestamps        []time.Time           `json:"-"`
	Queued            []int                 `json:"queued"`
	Running           []int                 `json:"running"`
	Succeeded         []int                 `json:"succeeded"`
	Failed            []int                 `json:"failed"`
	TimedOut          []int                 `json:"timedOut"`
	Canceled          []int                 `json:"canceled"`
	Concurrency       []int                 `json:"concurrency"`
	VCenters          []int                 `json:"vcenters"`
	JumpServers       map[string][]int      `json:"jumpServers"`
	ConcurrencyLimits TaskConcurrencyLimits `json:"concurrencyLimits"`
}

// TaskConcurrencyLimits are the ceilings Forward applied to the collection
// (ConcurrencyLimits). Global is the network's collector's EFFECTIVE
// concurrency with Forward's default (128) already applied, so it needs no
// Collectors.GetSettings lookup. Network repeats Global (Forward is removing
// it). VCenter is the collector's vCenter concurrency. JumpServer is the
// largest session cap among the network's jump servers, or Global if none sets
// one. With no collector for the network Forward returns its own defaults.
type TaskConcurrencyLimits struct {
	Global     int `json:"global"`
	Network    int `json:"network"`
	VCenter    int `json:"vcenter"`
	JumpServer int `json:"jumpServer"`
}

// UnmarshalJSON reads timestamps as ISO-8601 instants or epoch milliseconds,
// the two forms Forward has sent for Instants across builds.
func (p *SnapshotTaskProgress) UnmarshalJSON(data []byte) error {
	type plain SnapshotTaskProgress
	wire := struct {
		plain
		Timestamps []json.RawMessage `json:"timestamps"`
	}{}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	times := make([]time.Time, len(wire.Timestamps))
	for i, raw := range wire.Timestamps {
		millis, err := decodeEpochMillisOrInstant(raw)
		if err != nil {
			return fmt.Errorf("forward: task progress timestamp %d: %w", i, err)
		}
		times[i] = time.UnixMilli(millis).UTC()
	}
	*p = SnapshotTaskProgress(wire.plain)
	p.Timestamps = times
	return nil
}

// SnapshotProgress returns how a snapshot's collection ran over time: queued,
// running and finished subtasks, and the concurrency in use against its limit,
// at each sample. GET /api/collector-tasks?snapshotId=
// (CollectorTaskController.getSnapshotTaskProgress; VIEW_NETWORK_AND_SNAPSHOTS,
// snapshot at the START stage). Preview: not in the published spec.
func (s *CollectorTasksService) SnapshotProgress(ctx context.Context, snapshotID string) (*SnapshotTaskProgress, *Response, error) {
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID == "" {
		return nil, nil, errors.New("forward: snapshot ID is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/collector-tasks?"+url.Values{"snapshotId": []string{snapshotID}}.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "CollectorTasks.SnapshotProgress")
	out := new(SnapshotTaskProgress)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// CollectorSubTask is one subtask of a collector task (SubTaskInfo). For a
// NETWORK_COLLECTION, Description is the device name when the subtask covers
// one device and otherwise a summary. Status is one of the CollectorTask...
// constants: QUEUED, RUNNING, SUCCEEDED, FAILED, TIMED_OUT or CANCELED, which
// is what tells a device that ran out of time from one that was cancelled.
// StartedAt is zero until it starts, FinishedAt until it ends. Operation is
// the device command running right now (NETWORK_COLLECTION, running subtasks
// of the full listing only). Note carries the reason Forward recorded, if any.
type CollectorSubTask struct {
	ID          Identifier `json:"id"`
	CollectorID Identifier `json:"collectorId"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"-"`
	FinishedAt  time.Time  `json:"-"`
	Operation   string     `json:"operation,omitempty"`
	Note        string     `json:"note,omitempty"`
}

// UnmarshalJSON reads startedAt and finishedAt as ISO-8601 instants or epoch
// milliseconds.
func (t *CollectorSubTask) UnmarshalJSON(data []byte) error {
	type plain CollectorSubTask
	wire := struct {
		plain
		StartedAt  json.RawMessage `json:"startedAt"`
		FinishedAt json.RawMessage `json:"finishedAt"`
	}{}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*t = CollectorSubTask(wire.plain)
	for _, field := range []struct {
		name string
		raw  json.RawMessage
		dst  *time.Time
	}{{"startedAt", wire.StartedAt, &t.StartedAt}, {"finishedAt", wire.FinishedAt, &t.FinishedAt}} {
		millis, err := decodeEpochMillisOrInstant(field.raw)
		if err != nil {
			return fmt.Errorf("forward: collector subtask %s: %w", field.name, err)
		}
		if millis != 0 {
			*field.dst = time.UnixMilli(millis).UTC()
		}
	}
	return nil
}

// SubTasksAt returns the subtasks of a task that were in progress at an
// instant -- started by then and not finished before it -- oldest start first.
// It reads every subtask, with no cap, so it stays complete on a collection of
// tens of thousands of devices. GET /api/collector-tasks/{taskId}?view=subtasks
// &at= (VIEW_COLLECTOR_TASK_QUEUES plus permission to view the task). Preview.
func (s *CollectorTasksService) SubTasksAt(ctx context.Context, taskID string, at time.Time) ([]CollectorSubTask, *Response, error) {
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
	var out []CollectorSubTask
	resp, err := s.client.doRequired(req, &out)
	return out, resp, err
}

// CollectorTaskWithSubTasks is a task and its subtasks (the task fields sit
// beside subTasks in Forward's response).
type CollectorTaskWithSubTasks struct {
	CollectorTask
	SubTasks []CollectorSubTask `json:"subTasks"`
}

// GetWithSubTasks returns a task with its subtasks. Forward returns at most
// maxSubTasks of them (default 1000 when 0): the RUNNING, FAILED, TIMED_OUT
// and CANCELED ones first, then SUCCEEDED ones to fill the rest, and QUEUED
// ones never. So a short list is a complete picture of what went wrong, but
// not of what succeeded; raise maxSubTasks past the device count for every
// device's start and finish. GET /api/collector-tasks/{taskId}?for=ui&max=
// (VIEW_COLLECTOR_TASK_QUEUES plus permission to view the task). Preview.
func (s *CollectorTasksService) GetWithSubTasks(ctx context.Context, taskID string, maxSubTasks int) (*CollectorTaskWithSubTasks, *Response, error) {
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
	out := new(CollectorTaskWithSubTasks)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}
