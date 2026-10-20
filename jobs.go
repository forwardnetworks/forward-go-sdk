package forward

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
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
// DELETE /api/jobs/{cancelLink} (JobsController.cancelJob; ADMINISTER_SYSTEM). A single job that already finished or was already
// canceled answers 404 (CANCELATION_STATUS_NOT_FOUND), returned as an error; a grouped or network-level link always answers 200.
// To stop one snapshot's jobs use CancelForSnapshot.
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

// SnapshotCancelResult is what CancelForSnapshot did: how many active job rows it found for the snapshot, how many cancels Forward
// accepted (Canceled; for a grouped or network-level link Forward always answers 200, so this means "accepted", not "a job was
// stopped"), and how many single-job rows had already finished by the time the cancel arrived (AlreadyDone; Forward answers 404).
type SnapshotCancelResult struct {
	Matched     int
	Canceled    int
	AlreadyDone int
}

// CancelForSnapshot cancels the running or queued SNAPSHOT-LEVEL jobs of one snapshot: it reads the active job list (ListActive) and
// cancels every row whose SnapshotID matches (Cancel). Forward has NO snapshot-level cancel route; this is the only server-side stop,
// and it needs ADMINISTER_SYSTEM because both /api/jobs/active and DELETE /api/jobs/{cancelLink} do. It is the stop for a predicted
// snapshot's Protocol Modeling and for the other jobs that carry the snapshot's id (DAG merge, NQE, diffs, path searches).
//
// What it does NOT cover:
//   - Per-device processing of a snapshot import, reprocessing or collection ("Flowlet generation", "Model generation") is network-level:
//     its rows carry no snapshot id, so they are never matched, and cancelling their link would cancel those jobs for the whole network.
//     A snapshot import can therefore keep parsing after a CancelForSnapshot with Matched == 0.
//   - A snapshot still waiting in the processing throttle queue has no job row yet; invalidating it removes it from that queue and
//     cancels its future (Snapshots invalidate), which CancelForSnapshot cannot do. Otherwise invalidating or deleting a snapshot does
//     NOT stop its running jobs (SnapshotInvalidator carries a TODO for it, FWD-8083).
//   - A collection is stopped with CollectorTasks.Stop.
//
// Side effects to expect: EVERY top-level job row of that snapshot is cancelled, including other users' NQE queries, diffs and path
// searches (a grouped row's link cancels all of its type on the snapshot), and shared work is shared: when a predicted snapshot's
// Protocol Modeling started the base snapshot's, cancelling it can fail that base job for every other prediction waiting on it. Rows of
// other snapshots are never cancelled. A snapshot with no active row matches nothing and is not an error, so check Matched.
//
// A cancel completes the job's future with a cancellation and cancels its context (FutureCache.ActiveFuture.cancel). Protocol Modeling
// stops cooperatively, not by thread interrupt: it checks for cancellation between path-selection prefixes, route-propagation seeds and
// DAG node creation, and the steps in between (seed route set generation, aggregation, topology generation, and a single seed's
// propagation) run to the end, so expect a delay, and this SDK has not seen it stop one live. After a cancel, re-read ListActive to
// confirm the rows are gone. Per-row failures other than "already finished" are returned together as the error while the remaining
// rows are still cancelled; a cancelled ctx stops the loop.
func (s *JobsService) CancelForSnapshot(ctx context.Context, snapshotID string) (*SnapshotCancelResult, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(snapshotID), 10, 64)
	if err != nil || id <= 0 {
		return nil, fmt.Errorf("forward: snapshot ID %q is not a numeric snapshot id", snapshotID)
	}
	rows, _, err := s.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	result := new(SnapshotCancelResult)
	var failures []error
	for _, row := range rows {
		if row.SnapshotID == nil || *row.SnapshotID != id {
			continue
		}
		if err := ctx.Err(); err != nil {
			failures = append(failures, err)
			break
		}
		result.Matched++
		_, err := s.Cancel(ctx, row.CancelLink)
		switch {
		case err == nil:
			result.Canceled++
		case isGone(err):
			result.AlreadyDone++
		default:
			failures = append(failures, fmt.Errorf("job %s: %w", row.JobType, err))
		}
	}
	return result, errors.Join(failures...)
}
