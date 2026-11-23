package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
)

// Change-set checks are the intent and NQE checks Forward evaluates on a change set's base snapshot and on each snapshot predicted from
// it (ListChangeSetChecks reads them with their results). They use the same definition format as network checks (core CheckDefinition,
// the checkType discriminator) but only four types: Isolation, Reachability, Existential and NQE. Preview: the routes are appserver
// ChangeSetCheckController, not Forward's published OpenAPI; they need MANAGE_CHANGE_SETS and the organization property
// PREDICT_MODELING.

func (s *Predict) checkPath(networkID, changeSetID string) (string, error) {
	path, err := s.csPath(networkID, changeSetID)
	if err != nil {
		return "", err
	}
	return path + "/checks", nil
}

// AddChangeSetCheck attaches a check to a change set (POST .../change-sets/{id}/checks) and returns it with its result on the change
// set's BASE snapshot: Forward runs a check that has not executed before it answers, so this call can take as long as the check, and
// a check that fails to run comes back as a 2xx with Status ERROR or TIMEOUT, not as an error. Newer Forward also sends Status SKIPPED (a config-only change set skips checks that need behavioral modelling) and can send UNLICENSED, PROCESSING and REQUIRES_ADDITIONAL_SNAPSHOT_PROCESSING; Status is a string, so compare, do not assume a closed set. Forward validates the definition (400
// with the reason: an intent check against the snapshot, an NQE check that its query exists in the NQE Library) and refuses a check
// type the licence does not include (403). Forward does not deduplicate: if this call times out and is retried, there may be two
// checks, so list them before adding again. MANAGE_CHANGE_SETS; the organization property PREDICT_MODELING must be on. Results on the snapshots predicted from
// the change set are read with ListChangeSetChecks. An NQE check is removed by Forward when its query is deleted.
//
// Preview: ChangeSetCheckController, not in the published spec.
func (s *Predict) AddChangeSetCheck(ctx context.Context, networkID, changeSetID string, check ports.NewChangeSetCheck) (*ports.ChangeSetCheckResult, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if err := check.Definition.Validate(); err != nil {
		return nil, nil, err
	}
	if check.Name != "" && check.Definition.CheckType == ports.ChangeSetCheckNQE {
		return nil, nil, errors.New("forward: an NQE check takes its name from the query; Forward rejects a name")
	}
	path, err := s.checkPath(networkID, changeSetID)
	if err != nil {
		return nil, nil, err
	}
	out := new(ports.ChangeSetCheckResult)
	resp, err := s.doJSON(ctx, http.MethodPost, path, check, out, "Predict.AddChangeSetCheck")
	return out, resp, err
}

// UpdateChangeSetCheck changes a check's name or definition (PATCH .../checks/{checkId}; 204). Changing the definition changes what
// the check means, so results from before are outdated until it runs again.
//
// Preview: ChangeSetCheckController, not in the published spec.
func (s *Predict) UpdateChangeSetCheck(ctx context.Context, networkID, changeSetID, checkID string, patch ports.ChangeSetCheckPatch) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if patch.Definition != nil {
		if err := patch.Definition.Validate(); err != nil {
			return nil, err
		}
	}
	if patch.Name == nil && !patch.ClearName && patch.Definition == nil {
		return nil, errors.New("forward: a check patch needs a name, ClearName or a definition")
	}
	path, err := s.changeSetCheckPath(networkID, changeSetID, checkID)
	if err != nil {
		return nil, err
	}
	return s.doJSON(ctx, http.MethodPatch, path, patch, nil, "Predict.UpdateChangeSetCheck")
}

// DeleteChangeSetCheck removes a check and its results from a change set (DELETE .../checks/{checkId}; 204). It cannot be undone.
//
// Preview: ChangeSetCheckController, not in the published spec.
func (s *Predict) DeleteChangeSetCheck(ctx context.Context, networkID, changeSetID, checkID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := s.changeSetCheckPath(networkID, changeSetID, checkID)
	if err != nil {
		return nil, err
	}
	return s.doJSON(ctx, http.MethodDelete, path, nil, nil, "Predict.DeleteChangeSetCheck")
}

func (s *Predict) changeSetCheckPath(networkID, changeSetID, checkID string) (string, error) {
	path, err := s.checkPath(networkID, changeSetID)
	if err != nil {
		return "", err
	}
	if checkID = strings.TrimSpace(checkID); checkID == "" {
		return "", errors.New("forward: check ID is required")
	}
	return path + "/" + url.PathEscape(checkID), nil
}
