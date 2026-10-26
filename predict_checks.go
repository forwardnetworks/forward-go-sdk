package forward

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Change-set checks are the intent and NQE checks Forward evaluates on a change set's base snapshot and on each snapshot predicted from
// it (ListChangeSetChecks reads them with their results). They use the same definition format as network checks (core CheckDefinition,
// the checkType discriminator) but only four types: Isolation, Reachability, Existential and NQE. Preview: the routes are appserver
// ChangeSetCheckController, not Forward's published OpenAPI; they need MANAGE_CHANGE_SETS and the organization property
// PREDICT_MODELING.

// The check types a change set accepts, as Forward serializes them (checkType). Forward also reads the legacy spellings; this SDK
// writes only these.
const (
	ChangeSetCheckIsolation    = "Isolation"
	ChangeSetCheckReachability = "Reachability"
	ChangeSetCheckExists       = "Existential"
	ChangeSetCheckNQE          = "NQE"
)

// Return-path modes of an Existential check.
const (
	ReturnPathAny       = "ANY"
	ReturnPathSymmetric = "SYMMETRIC"
)

// ChangeSetCheckDefinition is one check's definition. Isolation, Reachability and Existential checks take Filters (a path-search
// query: from, to, chain, bypass, forwardingTypes, flowTypes, mode, as in the path-search API), and optionally NoiseTypes and
// HeaderFieldsWithDefaults; only Existential takes ReturnPath. An NQE check takes QueryID (an NQE Library query) and optional Params.
// Filters stays raw JSON because the path-query tree is large and shared with path search; build it as that API documents.
type ChangeSetCheckDefinition struct {
	CheckType string `json:"checkType"`
	// Filters is the path query of an intent check.
	Filters json.RawMessage `json:"filters,omitempty"`
	// NoiseTypes are the noise categories to ignore (for example INADMISSIBLE, GLEAN); Forward drops deprecated ones. A nil slice
	// sends nothing; an empty non-nil slice sends [] (Forward always echoes the list, and compares definitions for equality).
	NoiseTypes []string `json:"noiseTypes"`
	// HeaderFieldsWithDefaults lists header fields whose default values are part of the check (Forward's schema names ip_tos, tp_src,
	// app_id, url and user_id; it also accepts ip_frag_offset and tcp_flags). Nil means "unstated", for which Forward applies its
	// global default ({url}); an empty non-nil slice sends [] and means NO header defaults. Keep the difference when you read a check,
	// change it and write it back.
	HeaderFieldsWithDefaults []string `json:"headerFieldsWithDefaults"`
	// ReturnPath is ANY or SYMMETRIC (Existential only); empty validates no return path.
	ReturnPath string `json:"returnPath,omitempty"`
	// QueryID and Params define an NQE check. A parameter value must not be JSON null.
	QueryID string                     `json:"queryId,omitempty"`
	Params  map[string]json.RawMessage `json:"params,omitempty"`
}

// MarshalJSON leaves out an unstated (nil) list and sends an empty one, which encoding/json's omitempty cannot tell apart.
func (d ChangeSetCheckDefinition) MarshalJSON() ([]byte, error) {
	out := map[string]any{"checkType": d.CheckType}
	if len(d.Filters) > 0 {
		out["filters"] = d.Filters
	}
	if d.NoiseTypes != nil {
		out["noiseTypes"] = d.NoiseTypes
	}
	if d.HeaderFieldsWithDefaults != nil {
		out["headerFieldsWithDefaults"] = d.HeaderFieldsWithDefaults
	}
	if d.ReturnPath != "" {
		out["returnPath"] = d.ReturnPath
	}
	if d.QueryID != "" {
		out["queryId"] = d.QueryID
	}
	if len(d.Params) > 0 {
		out["params"] = d.Params
	}
	return json.Marshal(out)
}

func (d ChangeSetCheckDefinition) intent() bool {
	return d.CheckType == ChangeSetCheckIsolation || d.CheckType == ChangeSetCheckReachability || d.CheckType == ChangeSetCheckExists
}

// Validate refuses a definition Forward would, so the mistake surfaces before a request: an unsupported type, an intent check with no
// filters, an NQE check with no query, or fields that belong to the other kind.
func (d ChangeSetCheckDefinition) Validate() error {
	switch {
	case d.intent():
		if len(bytes.TrimSpace(d.Filters)) == 0 || string(bytes.TrimSpace(d.Filters)) == "null" {
			return fmt.Errorf("forward: a %s check needs filters (a path query)", d.CheckType)
		}
		if d.QueryID != "" || len(d.Params) > 0 {
			return fmt.Errorf("forward: queryId and params belong to NQE checks, not %s", d.CheckType)
		}
		switch d.ReturnPath {
		case "":
		case ReturnPathAny, ReturnPathSymmetric:
			if d.CheckType != ChangeSetCheckExists {
				return fmt.Errorf("forward: returnPath belongs to Existential checks, not %s", d.CheckType)
			}
		default:
			return fmt.Errorf("forward: returnPath %q must be ANY or SYMMETRIC", d.ReturnPath)
		}
	case d.CheckType == ChangeSetCheckNQE:
		if strings.TrimSpace(d.QueryID) == "" {
			return errors.New("forward: an NQE check needs a queryId")
		}
		if len(d.Filters) > 0 || d.NoiseTypes != nil || d.HeaderFieldsWithDefaults != nil || d.ReturnPath != "" {
			return errors.New("forward: filters, noiseTypes, headerFieldsWithDefaults and returnPath belong to intent checks, not NQE")
		}
	default:
		return fmt.Errorf("forward: check type %q is not supported on a change set (Isolation, Reachability, Existential, NQE)", d.CheckType)
	}
	return nil
}

// NewChangeSetCheck is the body that adds a check. Name is optional for the intent types; Forward rejects any name on an NQE check
// (it takes the query's name), so this SDK refuses one.
type NewChangeSetCheck struct {
	Name       string                   `json:"name,omitempty"`
	Definition ChangeSetCheckDefinition `json:"definition"`
}

// ChangeSetCheckPatch changes a check; nil fields are left alone. Definition replaces the whole definition and must keep the same
// category (an intent check stays intent, an NQE check stays NQE; Forward answers 400 otherwise). ClearName removes the name.
type ChangeSetCheckPatch struct {
	Name       *string
	ClearName  bool
	Definition *ChangeSetCheckDefinition
}

func (p ChangeSetCheckPatch) MarshalJSON() ([]byte, error) {
	out := map[string]any{}
	switch {
	case p.ClearName && p.Name != nil:
		return nil, errors.New("forward: set Name or ClearName, not both")
	case p.ClearName:
		out["name"] = nil
	case p.Name != nil:
		out["name"] = *p.Name
	}
	if p.Definition != nil {
		out["definition"] = p.Definition
	}
	return json.Marshal(out)
}

// ParsedDefinition decodes Definition into the typed form. Forward always writes the canonical check type, and this SDK writes only
// the canonical four: the legacy spellings Forward still reads (Exists, ExistentialCheck, IsolationCheck, ReachabilityCheck,
// NetworkQuery) are refused by Validate.
func (r ChangeSetCheckResult) ParsedDefinition() (ChangeSetCheckDefinition, error) {
	var d ChangeSetCheckDefinition
	if len(r.Definition) == 0 {
		return d, errors.New("forward: the check carries no definition")
	}
	err := json.Unmarshal(r.Definition, &d)
	return d, err
}

func (s *PredictService) checkPath(networkID, changeSetID string) (string, error) {
	path, err := s.csPath(networkID, changeSetID)
	if err != nil {
		return "", err
	}
	return path + "/checks", nil
}

// AddChangeSetCheck attaches a check to a change set (POST .../change-sets/{id}/checks) and returns it with its result on the change
// set's BASE snapshot: Forward runs a check that has not executed before it answers, so this call can take as long as the check, and
// a check that fails to run comes back as a 2xx with Status ERROR or TIMEOUT, not as an error. Forward validates the definition (400
// with the reason: an intent check against the snapshot, an NQE check that its query exists in the NQE Library) and refuses a check
// type the licence does not include (403). Forward does not deduplicate: if this call times out and is retried, there may be two
// checks, so list them before adding again. MANAGE_CHANGE_SETS; the organization property PREDICT_MODELING must be on. Results on the snapshots predicted from
// the change set are read with ListChangeSetChecks. An NQE check is removed by Forward when its query is deleted.
//
// Preview: ChangeSetCheckController, not in the published spec.
func (s *PredictService) AddChangeSetCheck(ctx context.Context, networkID, changeSetID string, check NewChangeSetCheck) (*ChangeSetCheckResult, *Response, error) {
	if err := check.Definition.Validate(); err != nil {
		return nil, nil, err
	}
	if check.Name != "" && check.Definition.CheckType == ChangeSetCheckNQE {
		return nil, nil, errors.New("forward: an NQE check takes its name from the query; Forward rejects a name")
	}
	path, err := s.checkPath(networkID, changeSetID)
	if err != nil {
		return nil, nil, err
	}
	out := new(ChangeSetCheckResult)
	resp, err := s.doJSON(ctx, http.MethodPost, path, check, out, "Predict.AddChangeSetCheck")
	return out, resp, err
}

// UpdateChangeSetCheck changes a check's name or definition (PATCH .../checks/{checkId}; 204). Changing the definition changes what
// the check means, so results from before are outdated until it runs again.
//
// Preview: ChangeSetCheckController, not in the published spec.
func (s *PredictService) UpdateChangeSetCheck(ctx context.Context, networkID, changeSetID, checkID string, patch ChangeSetCheckPatch) (*Response, error) {
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
func (s *PredictService) DeleteChangeSetCheck(ctx context.Context, networkID, changeSetID, checkID string) (*Response, error) {
	path, err := s.changeSetCheckPath(networkID, changeSetID, checkID)
	if err != nil {
		return nil, err
	}
	return s.doJSON(ctx, http.MethodDelete, path, nil, nil, "Predict.DeleteChangeSetCheck")
}

func (s *PredictService) changeSetCheckPath(networkID, changeSetID, checkID string) (string, error) {
	path, err := s.checkPath(networkID, changeSetID)
	if err != nil {
		return "", err
	}
	if checkID = strings.TrimSpace(checkID); checkID == "" {
		return "", errors.New("forward: check ID is required")
	}
	return path + "/" + url.PathEscape(checkID), nil
}
