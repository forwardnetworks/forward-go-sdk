package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"strings"
)

// JobsService reports and cancels top-level backend jobs (NQE executions, snapshot processing, device-processing
// batches and similar). This is an org-admin surface (ADMINISTER_SYSTEM, granted to OrgRole.ADMIN), not the
// customer-facing API: Forward's own GUI (Settings > System Overview > Jobs) and support engineers use it to find
// and clear a wedged execution instead of restarting a worker.
type Jobs service

// ListActive returns currently running or queued top-level jobs, grouped by type/network/snapshot.
func (s *Jobs) ListActive(ctx context.Context) ([]ports.ActiveJobInfo, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/jobs/active", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Jobs.ListActive")
	var out []ports.ActiveJobInfo
	resp, err := s.client.doRequired(req, &out)
	return out, resp, err
}

// ListCompleted returns finished top-level jobs, grouped by type/network/snapshot/state.
func (s *Jobs) ListCompleted(ctx context.Context) ([]ports.CompletedJobInfo, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/jobs/completed", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Jobs.ListCompleted")
	var out []ports.CompletedJobInfo
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
// Forward has no snapshot-level cancel; Cancel stops only the job or job group its cancelLink names.
func (s *Jobs) Cancel(ctx context.Context, cancelLink string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
