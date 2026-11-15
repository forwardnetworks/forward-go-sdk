package forward

import (
	"context"
	"errors"
	"net/http"
)

// CheckPatch is the body of Checks.Update (Forward's CheckUpdate). Every field is optional and a field left out is not changed: Forward
// reads an absent field and a JSON null alike as "keep". So the strings, booleans and priority are pointers, and Tags is sent as null
// when nil (unchanged) and as a list when non-nil, where an empty non-nil list clears the tags. Definition replaces the whole
// definition; it must be the same check type, and an NQE check keeps its queryId. Note cannot be updated for an NQE check (a 400).
type CheckPatch struct {
	Definition            map[string]any `json:"definition,omitempty"`
	Name                  *string        `json:"name,omitempty"`
	Note                  *string        `json:"note,omitempty"`
	Tags                  []string       `json:"tags"`
	Enabled               *bool          `json:"enabled,omitempty"`
	Priority              *string        `json:"priority,omitempty"`
	PerfMonitoringEnabled *bool          `json:"perfMonitoringEnabled,omitempty"`
}

// Update edits a check in place, keeping its id, and returns it (PATCH /api/snapshots/{snapshotId}/checks/{checkId};
// CheckController.updateCheck, EDIT_CHECKS). Preview: the route is not in the published OpenAPI; the body is from Forward's source, not
// yet seen live. On a predicted snapshot Forward refuses check edits unless the PREDICT_UNSANDBOXED property is on. An empty patch is
// refused here rather than sent, since it changes nothing.
func (s *ChecksService) Update(ctx context.Context, snapshotID, checkID string, patch CheckPatch) (*Check, *Response, error) {
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
	check := new(Check)
	resp, err := s.client.Do(req, check)
	return check, resp, err
}
