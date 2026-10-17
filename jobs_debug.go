package forward

import (
	"context"
	"encoding/json"
	"net/http"
)

// DebugJob is one running or waiting compute or search job in Forward's scheduler debug view. It is a debug surface with no shape
// contract: only the fields below are read, and Raw keeps the rest.
type DebugJob struct {
	JobType     string          `json:"jobType"`
	OrgID       json.RawMessage `json:"orgId,omitempty"`
	Priority    string          `json:"priority,omitempty"`
	SnapshotKey json.RawMessage `json:"snapshotKey,omitempty"`
	Tasks       []DebugTask     `json:"tasks,omitempty"`
	Raw         json.RawMessage `json:"-"`
}

// UnmarshalJSON keeps the whole object in Raw.
func (j *DebugJob) UnmarshalJSON(data []byte) error {
	type plain DebugJob
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*j = DebugJob(value)
	j.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// DebugTask is one task of a job. TaskStatus is INITIALIZED, QUEUED or RUNNING; ResourceExpectancy is the worker class the task needs
// (REGULAR or LARGE); Worker says which worker runs it, and is empty until one does.
type DebugTask struct {
	TaskStatus         string          `json:"taskStatus"`
	ResourceExpectancy string          `json:"resourceExpectancy,omitempty"`
	LifeExpectancy     string          `json:"lifeExpectancy,omitempty"`
	Worker             json.RawMessage `json:"worker,omitempty"`
	WaitCounter        json.RawMessage `json:"waitCounter,omitempty"`
	ResourceCost       json.RawMessage `json:"resourceCost,omitempty"`
}

// DebugJobsView is the scheduler's jobs by worker type ("COMPUTE", "SEARCH"), then priority, then job id.
type DebugJobsView map[string]map[string]struct {
	IDToJobDetails map[string]DebugJob `json:"idToJobDetails"`
}

// DebugView reads the job scheduler's debug view (GET /api/debug/jobs, JobsController.getDebugJobsView; needs ADMINISTER_SYSTEM): every
// job and task the scheduler holds right now across ALL organizations, with each task's status, worker and worker class. It is the only
// route that shows running and queued Predict and compute tasks per org with their worker class; the per-org worker cap is not in it.
// It is a point-in-time debug dump with no stability promise (not a stream of state transitions). For job rows grouped for display
// use ListActive.
func (s *JobsService) DebugView(ctx context.Context) (DebugJobsView, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/debug/jobs", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Jobs.DebugView")
	var out DebugJobsView
	resp, err := s.client.doRequired(req, &out)
	return out, resp, err
}
