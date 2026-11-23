package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
)

// Update edits a check in place, keeping its id, and returns it (PATCH /api/snapshots/{snapshotId}/checks/{checkId};
// CheckController.updateCheck, EDIT_CHECKS). Preview: the route is not in the published OpenAPI; the body is from Forward's source, not
// yet seen live. On a predicted snapshot Forward refuses check edits unless the PREDICT_UNSANDBOXED property is on. An empty patch is
// refused here rather than sent, since it changes nothing.
func (s *Checks) Update(ctx context.Context, snapshotID, checkID string, patch ports.CheckPatch) (*ports.Check, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := checkPath(snapshotID, checkID)
	if err != nil {
		return nil, nil, err
	}
	if patch.Definition == nil && patch.Name == nil && patch.Note == nil && patch.Tags == nil && patch.Enabled == nil &&
		patch.Priority == nil && patch.PerfMonitoringEnabled == nil {
		return nil, nil, errors.New("forward: a check patch must change at least one field")
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, patch)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Checks.Update")
	check := new(ports.Check)
	resp, err := s.client.Do(req, check)
	return check, resp, err
}
