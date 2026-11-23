package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
)

// Metrics returns a snapshot's collection and processing health: device and
// endpoint success/failure counts, failures by error name, durations and index
// statuses. GET /api/snapshots/{snapshotId}/metrics (SnapshotController,
// served on primary 15398425a69 and stable 67e89c87124). Forward answers 404
// when the snapshot has no metrics, and ErrSnapshotNotProcessed before
// processing has started.
func (s *Snapshots) Metrics(ctx context.Context, snapshotID string) (*ports.SnapshotMetrics, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := snapshotSubPath(snapshotID, "metrics")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Snapshots.Metrics")
	out := new(ports.SnapshotMetrics)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

// Exceptions returns the exceptions Forward recorded while parsing, modeling
// and indexing a snapshot. GET /api/snapshots/{snapshotId}/exceptions?view=json
// (SnapshotController.getExceptionsJson, served on primary 15398425a69 and
// stable 67e89c87124). It needs the DEBUG_SNAPSHOTS network permission; a
// caller without it gets a 403.
func (s *Snapshots) Exceptions(ctx context.Context, snapshotID string) ([]ports.SnapshotException, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := snapshotSubPath(snapshotID, "exceptions")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"?"+url.Values{"view": []string{"json"}}.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Snapshots.Exceptions")
	result := listResponse[ports.SnapshotException]{Keys: []string{"exceptions"}}
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

func snapshotSubPath(snapshotID, tail string) (string, error) {
	snapshotID = strings.TrimSpace(snapshotID)
	if snapshotID == "" {
		return "", errors.New("forward: snapshot ID is required")
	}
	return "/api/snapshots/" + url.PathEscape(snapshotID) + "/" + tail, nil
}

// Progress returns a snapshot's processing progress, stage by stage. GET
// /api/snapshots/{snapshotId}/progress (SnapshotController; served on primary
// 15398425a69 and stable 67e89c87124). Preview: not in the published spec.
func (s *Snapshots) Progress(ctx context.Context, snapshotID string) (*ports.SnapshotProgress, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := snapshotSubPath(snapshotID, "progress")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Snapshots.Progress")
	out := new(ports.SnapshotProgress)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}

// ProcessEstimate returns Forward's per-stage processing-time estimate for a
// snapshot. GET /api/snapshots/{snapshotId}/processEstimate (SnapshotController;
// served on primary 15398425a69 and stable 67e89c87124). Preview: not in the
// published spec.
func (s *Snapshots) ProcessEstimate(ctx context.Context, snapshotID string) (*ports.SnapshotProcessEstimate, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := snapshotSubPath(snapshotID, "processEstimate")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Snapshots.ProcessEstimate")
	out := new(ports.SnapshotProcessEstimate)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}
