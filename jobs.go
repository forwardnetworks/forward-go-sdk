package forward

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// JobsService reports and cancels top-level backend jobs (NQE executions, snapshot processing, device-processing
// batches and similar). This is an org-admin surface (ADMINISTER_SYSTEM, granted to OrgRole.ADMIN), not the
// customer-facing API: Forward's own GUI (Settings > System Overview > Jobs) and support engineers use it to find
// and clear a wedged execution instead of restarting a worker.
type JobsService service

// ActiveJobInfo is one row (or a group of identical rows) of currently running or queued work.
//
// GET /api/jobs/active (JobsController.getActiveJobInfos).
type ActiveJobInfo struct {
	JobType                   string            `json:"jobType"`
	OrgName                   string            `json:"orgName"`
	NetworkID                 int64             `json:"networkId"`
	SnapshotID                *int64            `json:"snapshotId,omitempty"`
	CreationTime              FlexibleTimestamp `json:"creationTime"`
	WaitingForOtherJobs       int               `json:"waitingForOtherJobs"`
	Queued                    int               `json:"queued"`
	Running                   int               `json:"running"`
	LongestQueuedTimeSeconds  int               `json:"longestQueuedTimeInSeconds"`
	LongestRunningTimeSeconds int               `json:"longestRunningTimeInSeconds"`
	DurationSeconds           int               `json:"durationInSeconds"`
	TotalCount                int               `json:"totalCount"`
	// CancelLink is opaque (Forward encodes the underlying job key); pass it to Cancel unchanged. It identifies
	// this row's job or, for a grouped row (TotalCount > 1), the whole group.
	CancelLink string `json:"cancelLink"`
}

// CompletedJobInfo is one row (or a group) of finished work.
//
// GET /api/jobs/completed (JobsController.getCompletedJobInfos).
type CompletedJobInfo struct {
	JobType                   string            `json:"jobType"`
	OrgName                   string            `json:"orgName"`
	NetworkID                 int64             `json:"networkId"`
	SnapshotID                *int64            `json:"snapshotId,omitempty"`
	CreationTime              FlexibleTimestamp `json:"creationTime"`
	State                     string            `json:"state"` // SUCCEEDED, FAILED, CANCELED or TIMEDOUT
	EarliestStartTime         FlexibleTimestamp `json:"earliestStartTime"`
	LatestEndTime             FlexibleTimestamp `json:"latestEndTime"`
	LongestQueueTimeSeconds   int               `json:"longestQueueTimeInSeconds"`
	LongestRunningTimeSeconds int               `json:"longestRunningTimeInSeconds"`
	DurationSeconds           int               `json:"durationInSeconds"`
	Count                     int               `json:"count"`
}

// ListActive returns currently running or queued top-level jobs, grouped by type/network/snapshot.
func (s *JobsService) ListActive(ctx context.Context) ([]ActiveJobInfo, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/jobs/active", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Jobs.ListActive")
	var out []ActiveJobInfo
	resp, err := s.client.doRequired(req, &out)
	return out, resp, err
}

// ListCompleted returns finished top-level jobs, grouped by type/network/snapshot/state.
func (s *JobsService) ListCompleted(ctx context.Context) ([]CompletedJobInfo, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/jobs/completed", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Jobs.ListCompleted")
	var out []CompletedJobInfo
	resp, err := s.client.doRequired(req, &out)
	return out, resp, err
}

// Cancel cancels the job (or job group) identified by cancelLink, exactly as returned in an ActiveJobInfo's
// CancelLink field. It stops the tracked execution; whether the underlying worker thread is interrupted
// immediately or only once it reaches a checkpoint is up to that job's own implementation.
//
// cancelLink is already percent-encoded by Forward for direct insertion into the path (Forward's own GUI
// builds the URL as a bare `/jobs/${cancelLink}` template, with no further encoding) -- it is NOT raw base64,
// even though it is base64 underneath. Escaping it again here (as a normal path segment would need) corrupts
// it: a real cancelLink contains literal "%2F"/"%3D" text, and re-escaping that "%" turns it into "%25..." and
// Forward answers 400 Bad Request, not 404. So this is deliberately NOT url.PathEscape(cancelLink).
//
// DELETE /api/jobs/{cancelLink} (JobsController.cancelJob). Returns an error wrapping ErrEndpointNotServed-style
// "not found" semantics if the job already finished or was already canceled (CANCELATION_STATUS_NOT_FOUND -> 404).
func (s *JobsService) Cancel(ctx context.Context, cancelLink string) (*Response, error) {
	if cancelLink = strings.TrimSpace(cancelLink); cancelLink == "" {
		return nil, errors.New("forward: cancelLink is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, "/api/jobs/"+cancelLink, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Jobs.Cancel")
	return s.client.Do(req, nil)
}
