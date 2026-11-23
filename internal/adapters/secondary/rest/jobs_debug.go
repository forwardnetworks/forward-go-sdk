package rest

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
)

// DebugView reads the job scheduler's debug view (GET /api/debug/jobs, JobsController.getDebugJobsView; needs ADMINISTER_SYSTEM): every
// job and task the scheduler holds right now across ALL organizations, with each task's status, worker and worker class. It is the only
// route that shows running and queued Predict and compute tasks per org with their worker class; the per-org worker cap is not in it.
// It is a point-in-time debug dump with no stability promise (not a stream of state transitions). For job rows grouped for display
// use ListActive.
func (s *Jobs) DebugView(ctx context.Context) (ports.DebugJobsView, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/debug/jobs", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Jobs.DebugView")
	var out ports.DebugJobsView
	resp, err := s.client.doRequired(req, &out)
	return out, resp, err
}
