package forward

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// StageBGPAdvertisements stages every advertisement one device receives in a
// single draft update (?action=bulkAdd, a JSON array of the records
// StageBGPAdvertisement sends one at a time). Forward appends them to the
// device's ordered list in slice order, so the order of ads is the order the
// draft records. An empty slice is a no-op that sends nothing.
//
// A build without bulkAdd answers 400 "Parameter conditions ... not met";
// callers that must support such a build fall back to StageBGPAdvertisement.
func (s *PredictService) StageBGPAdvertisements(
	ctx context.Context,
	networkID string,
	changeSetID string,
	deviceName string,
	advertisements []BGPAdvertisement,
) (*Response, error) {
	if err := s.client.requireCapability(CapabilityPredict); err != nil {
		return nil, err
	}
	if err := s.client.requireCapability(CapabilityStructuredBGPAdvertisements); err != nil {
		return nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, err
	}
	path, err := predictDevicePath(networkID, changeSetID, deviceName)
	if err != nil {
		return nil, err
	}
	if len(advertisements) == 0 {
		return nil, nil
	}
	body := make([]BGPAdvertisement, 0, len(advertisements))
	for _, advertisement := range advertisements {
		if advertisement.ASPath == nil {
			advertisement.ASPath = []int64{}
		}
		if advertisement.Communities == nil {
			advertisement.Communities = []string{}
		}
		body = append(body, advertisement)
	}
	path += "/bgp-advertisements?" + url.Values{"action": []string{"bulkAdd"}}.Encode()
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Predict.StageBGPAdvertisements")
	response, err := s.client.Do(req, nil)
	if err == nil {
		s.client.observeCapability(CapabilityPredict)
		s.client.observeCapability(CapabilityStructuredBGPAdvertisements)
	}
	return response, err
}

// ChangeSetCheckResult is a check attached to a change set, with its result
// on one snapshot (Forward's ChangeSetCheckResult: the check's fields
// unwrapped, plus status and execution fields once it has run).
type ChangeSetCheckResult struct {
	ID                  Identifier      `json:"id"`
	Name                string          `json:"name,omitempty"`
	Definition          json.RawMessage `json:"definition,omitempty"`
	DefinedAt           string          `json:"definedAt,omitempty"`
	Status              string          `json:"status,omitempty"`
	ExecutedAt          string          `json:"executedAt,omitempty"`
	ExecutionDurationMS *int64          `json:"executionDurationMillis,omitempty"`
	// NumViolations is nil when Forward sent none (the check did not fail or
	// has not run), not "zero violations".
	NumViolations *int64     `json:"numViolations,omitempty"`
	CreatedByID   Identifier `json:"createdById,omitempty"`
	CreatedAt     string     `json:"createdAt,omitempty"`
	NQEResultKey  string     `json:"nqeResultKey,omitempty"`
}

// ListChangeSetChecks returns the change set's attached checks with their
// results on snapshotID (GET .../change-sets/{cs}/checks?snapshotId=).
// snapshotID must be the change set's base or one of its predicted snapshots;
// Forward refuses anything else. An empty snapshotID omits the parameter.
func (s *PredictService) ListChangeSetChecks(
	ctx context.Context,
	networkID string,
	changeSetID string,
	snapshotID string,
) ([]ChangeSetCheckResult, *Response, error) {
	if err := s.client.requireCapability(CapabilityPredict); err != nil {
		return nil, nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	path, err := changeSetPath(networkID, changeSetID)
	if err != nil {
		return nil, nil, err
	}
	path += "/checks"
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID != "" {
		path += "?" + url.Values{"snapshotId": []string{snapshotID}}.Encode()
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Predict.ListChangeSetChecks")
	result := listResponse[ChangeSetCheckResult]{Keys: []string{"checks", "items"}}
	resp, err := s.client.doRequired(req, &result)
	if err != nil {
		return nil, resp, err
	}
	return result.Items, resp, nil
}

// CommandValidationOptions places the editor cursor Forward uses for its
// suggestions. Both are 1-based; zero means 1.
type CommandValidationOptions struct {
	CursorLine   int
	CursorColumn int
}

// CommandError is one invalid or incomplete command line (Forward's
// changeset.CommandError). Lines and columns are 1-based.
type CommandError struct {
	LineNumber        int             `json:"lineNumber"`
	StartColumnNumber int             `json:"startColumnNumber,omitempty"`
	EndColumnNumber   int             `json:"endColumnNumber,omitempty"`
	ErrorType         string          `json:"errorType"`
	ErrorMsg          string          `json:"errorMsg"`
	ExpectedArgs      json.RawMessage `json:"expectedArgs,omitempty"`
}

// CommandValidation is Forward's ChangeSetValidationResponse. CommandErrors is
// empty when Forward found nothing wrong -- which is not proof the commands
// are right: the validator only knows the commands in its grammar.
type CommandValidation struct {
	CommandErrors []CommandError    `json:"commandErrors"`
	Suggestions   []json.RawMessage `json:"suggestions,omitempty"`
	TextMarks     []json.RawMessage `json:"textMarks,omitempty"`
}

// ValidateCommands checks CLI text for a device in a change set without
// staging it (POST .../change-sets/{cs}/devices/{d}/commands?action=validate).
// Forward rejects empty commands and a cursor outside the text.
func (s *PredictService) ValidateCommands(
	ctx context.Context,
	networkID string,
	changeSetID string,
	deviceName string,
	commands string,
	options ...CommandValidationOptions,
) (*CommandValidation, *Response, error) {
	if len(options) > 1 {
		return nil, nil, errors.New("forward: at most one command validation option set is allowed")
	}
	if err := s.client.requireCapability(CapabilityPredict); err != nil {
		return nil, nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	path, err := changeSetPath(networkID, changeSetID)
	if err != nil {
		return nil, nil, err
	}
	if deviceName = strings.TrimSpace(deviceName); deviceName == "" {
		return nil, nil, errors.New("forward: device name is required")
	}
	if strings.TrimSpace(commands) == "" {
		return nil, nil, errors.New("forward: commands to validate are required")
	}
	line, column := 1, 1
	if len(options) == 1 {
		if options[0].CursorLine > 0 {
			line = options[0].CursorLine
		}
		if options[0].CursorColumn > 0 {
			column = options[0].CursorColumn
		}
	}
	path += "/devices/" + url.PathEscape(deviceName) + "/commands?" + url.Values{"action": []string{"validate"}}.Encode()
	payload := struct {
		Commands        string `json:"commands"`
		CursorLineNum   int    `json:"cursorLineNum"`
		CursorColumnNum int    `json:"cursorColumnNum"`
	}{commands, line, column}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, payload)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Predict.ValidateCommands")
	out := new(CommandValidation)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	s.client.observeCapability(CapabilityPredict)
	return out, resp, nil
}
