package rest

import (
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
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
func (s *Predict) StageBGPAdvertisements(
	ctx context.Context,
	networkID string,
	changeSetID string,
	deviceName string,
	advertisements []ports.BGPAdvertisement,
) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if err := s.client.requireCapability(ports.CapabilityPredict); err != nil {
		return nil, err
	}
	if err := s.client.requireCapability(ports.CapabilityStructuredBGPAdvertisements); err != nil {
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
	body := make([]ports.BGPAdvertisement, 0, len(advertisements))
	// Forward stores two staged advertisements with the same key and peer type from one bulk body as given, then fails when the
	// change set runs (its seed-route map has no merge for equal keys). Refuse that here; to change one, stage it once.
	type stagedKey struct{ vrf, peer, prefix, kind string }
	seen := map[stagedKey]bool{}
	for _, advertisement := range advertisements {
		key := stagedKey{strings.TrimSpace(advertisement.VRF), strings.TrimSpace(advertisement.ExternalPeer), strings.TrimSpace(advertisement.Prefix), strings.ToUpper(strings.TrimSpace(advertisement.Type))}
		if seen[key] {
			return nil, fmt.Errorf("forward: BGP advertisement %s %s %s (%s) is staged twice in one call", key.vrf, key.peer, key.prefix, key.kind)
		}
		seen[key] = true
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
		s.client.observeCapability(ports.CapabilityPredict)
		s.client.observeCapability(ports.CapabilityStructuredBGPAdvertisements)
	}
	return response, err
}

// ListChangeSetChecks returns the change set's attached checks with their
// results on snapshotID (GET .../change-sets/{cs}/checks?snapshotId=).
// snapshotID must be the change set's base or one of its predicted snapshots;
// Forward refuses anything else. An empty snapshotID omits the parameter.
func (s *Predict) ListChangeSetChecks(
	ctx context.Context,
	networkID string,
	changeSetID string,
	snapshotID string,
) ([]ports.ChangeSetCheckResult, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if err := s.client.requireCapability(ports.CapabilityPredict); err != nil {
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
	result := listResponse[ports.ChangeSetCheckResult]{Keys: []string{"checks", "items"}}
	resp, err := s.client.doRequired(req, &result)
	if err != nil {
		return nil, resp, err
	}
	return result.Items, resp, nil
}

// ValidateCommands is ADVISORY: it reports problems and nothing rejects a bad command at staging (StageCommands accepts text Forward
// would flag here), so an empty CommandErrors proves nothing, and a vendor without a command grammar has little to check. Only a
// Predict run shows whether the change was modelled. Every error seen live was ErrorType INVALID (Forward also has INCOMPLETE and UNSUPPORTED), with the line, columns
// and the expected arguments; PAN-OS answers an empty result for anything, invented lines included; and EOS marks some real commands
// INVALID (extended ACL lines, "management api"). Treat INVALID as evidence to review, not as proof the command is wrong.
//
// ValidateCommands checks CLI text for a device in a change set without
// staging it (POST .../change-sets/{cs}/devices/{d}/commands?action=validate).
// Forward rejects empty commands and a cursor outside the text.
func (s *Predict) ValidateCommands(
	ctx context.Context,
	networkID string,
	changeSetID string,
	deviceName string,
	commands string,
	options ...ports.CommandValidationOptions,
) (*ports.CommandValidation, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if len(options) > 1 {
		return nil, nil, errors.New("forward: at most one command validation option set is allowed")
	}
	if err := s.client.requireCapability(ports.CapabilityPredict); err != nil {
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
	out := new(ports.CommandValidation)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	s.client.observeCapability(ports.CapabilityPredict)
	return out, resp, nil
}
