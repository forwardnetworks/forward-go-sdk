package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ChangeSetCommit is one stored version of a change set: its content plus the commit's id, note and who committed it and when.
// A change set's versions are its commits (Forward's "version history"); each Run of a commit produces a predicted snapshot
// (Predict.ListPredictedSnapshots). HiddenDevices lists devices the caller may not see (device access policy), whose changes are
// left out of DeviceToChanges.
type ChangeSetCommit struct {
	ID              Identifier                 `json:"id"`
	Name            string                     `json:"name"`
	Description     string                     `json:"description,omitempty"`
	NetworkID       Identifier                 `json:"networkId,omitempty"`
	SnapshotID      Identifier                 `json:"snapshotId,omitempty"`
	Tags            []string                   `json:"tags,omitempty"`
	DeviceToChanges map[string]json.RawMessage `json:"deviceToChanges,omitempty"`
	CommitID        Identifier                 `json:"commitId"`
	Note            string                     `json:"note,omitempty"`
	CommittedByID   Identifier                 `json:"committedById,omitempty"`
	CommittedAt     string                     `json:"committedAt,omitempty"`
	HiddenDevices   []string                   `json:"hiddenDevices,omitempty"`
}

// ChangeSetCommitMetadata is a commit's id, note and attribution, as the restore and note calls return it.
type ChangeSetCommitMetadata struct {
	CommitID      Identifier `json:"commitId"`
	Note          string     `json:"note,omitempty"`
	CommittedByID Identifier `json:"committedById,omitempty"`
	CommittedAt   string     `json:"committedAt,omitempty"`
}

// ChangeSetDraft is the caller's uncommitted edits to a change set (Forward auto-saves a draft per user). Draft is nil when there
// is none. SavedAt is when it was last saved.
type ChangeSetDraft struct {
	Draft *DraftChangeSet `json:"draft"`
}

// DraftChangeSet is the content of a draft: the change set's fields plus when it was saved (epoch milliseconds) and the devices the
// caller may not see.
type DraftChangeSet struct {
	ChangeSet
	SavedAt       json.Number `json:"savedAt,omitempty"`
	HiddenDevices []string    `json:"hiddenDevices,omitempty"`
}

// ChangeSetPatch is a partial update of a change set's metadata (PATCH .../change-sets/{id}; nil fields are left alone). Changing
// SnapshotID re-bases the change set; Forward keys predictions by base snapshot and version, so Run again to get a new predicted
// snapshot (the earlier ones are kept). Forward does not check that the new snapshot belongs to the same network; that is on the caller.
type ChangeSetPatch struct {
	Name        *string   `json:"name,omitempty"`
	Description *string   `json:"description,omitempty"`
	SnapshotID  *string   `json:"snapshotId,omitempty"`
	Tags        *[]string `json:"tags,omitempty"`
}

// DuplicateChangeSetOptions names the copy and, optionally, the snapshot to base it on (in the target network when one is given).
// Empty fields send nothing, so Forward picks its own name and keeps the source's base snapshot.
type DuplicateChangeSetOptions struct {
	TargetName           string `json:"targetName,omitempty"`
	TargetBaseSnapshotID string `json:"targetBaseSnapshotId,omitempty"`
}

// maxDeviceConfigBytes is Forward's limit on a device configuration saved to a change set (ChangeSetController).
const maxDeviceConfigBytes = 2 * 1024 * 1024

func (s *PredictService) csPath(networkID, changeSetID string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	return changeSetPath(networkID, changeSetID)
}

func (s *PredictService) csDevicePath(networkID, changeSetID, deviceName string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	return predictDevicePath(networkID, changeSetID, deviceName)
}

func (s *PredictService) doJSON(ctx context.Context, method, path string, body, out any, operation string) (*Response, error) {
	var req *http.Request
	var err error
	if body == nil {
		req, err = s.client.NewRequest(ctx, method, path, nil)
	} else {
		req, err = s.client.newJSONRequest(ctx, method, path, body)
	}
	if err != nil {
		return nil, err
	}
	req = markOperation(req, operation)
	return s.firewallDo(req, out)
}

// UpdateChangeSet changes a change set's name, description, tags or base snapshot (PATCH .../change-sets/{id}; MANAGE_CHANGE_SETS)
// and returns it. Edits to its devices are staged with StageCommands, SaveDeviceConfig and the firewall calls, then Commit and Run
// produce a new version and predicted snapshot.
func (s *PredictService) UpdateChangeSet(ctx context.Context, networkID, changeSetID string, patch ChangeSetPatch) (*ChangeSet, *Response, error) {
	path, err := s.csPath(networkID, changeSetID)
	if err != nil {
		return nil, nil, err
	}
	out := new(ChangeSet)
	resp, err := s.doJSON(ctx, http.MethodPatch, path, patch, out, "Predict.UpdateChangeSet")
	return out, resp, err
}

// DuplicateChangeSet copies a change set within its network (POST ...change-sets/{id}?action=duplicate) and returns the copy.
func (s *PredictService) DuplicateChangeSet(ctx context.Context, networkID, changeSetID string, options DuplicateChangeSetOptions) (*ChangeSet, *Response, error) {
	return s.duplicate(ctx, networkID, changeSetID, "", options)
}

// DuplicateChangeSetToNetwork copies a change set into another network (?action=duplicate&targetNetworkId=) and returns the copy.
// Forward needs the right to manage change sets in both networks. TargetBaseSnapshotID is required here: it must be a snapshot of
// the target network (without it Forward reuses the source's base snapshot, which belongs to the source network, and answers 400).
func (s *PredictService) DuplicateChangeSetToNetwork(ctx context.Context, networkID, changeSetID, targetNetworkID string, options DuplicateChangeSetOptions) (*ChangeSet, *Response, error) {
	if strings.TrimSpace(targetNetworkID) == "" {
		return nil, nil, errors.New("forward: target network ID is required")
	}
	if strings.TrimSpace(options.TargetBaseSnapshotID) == "" {
		return nil, nil, errors.New("forward: a base snapshot of the target network is required to copy a change set to another network")
	}
	return s.duplicate(ctx, networkID, changeSetID, targetNetworkID, options)
}

func (s *PredictService) duplicate(ctx context.Context, networkID, changeSetID, targetNetworkID string, options DuplicateChangeSetOptions) (*ChangeSet, *Response, error) {
	path, err := s.csPath(networkID, changeSetID)
	if err != nil {
		return nil, nil, err
	}
	query := url.Values{"action": []string{"duplicate"}}
	if targetNetworkID = strings.TrimSpace(targetNetworkID); targetNetworkID != "" {
		query.Set("targetNetworkId", targetNetworkID)
	}
	operation := "Predict.DuplicateChangeSet"
	if targetNetworkID != "" {
		operation = "Predict.DuplicateChangeSetToNetwork"
	}
	out := new(ChangeSet)
	resp, err := s.doJSON(ctx, http.MethodPost, path+"?"+query.Encode(), options, out, operation)
	return out, resp, err
}

// GetDraft returns the caller's uncommitted edits to the change set (GET .../change-sets/{id}/draft); Draft is nil when there are none.
func (s *PredictService) GetDraft(ctx context.Context, networkID, changeSetID string) (*ChangeSetDraft, *Response, error) {
	path, err := s.csPath(networkID, changeSetID)
	if err != nil {
		return nil, nil, err
	}
	out := new(ChangeSetDraft)
	resp, err := s.doJSON(ctx, http.MethodGet, path+"/draft", nil, out, "Predict.GetDraft")
	return out, resp, err
}

// DiscardDraftDevice throws away the caller's uncommitted edits to one device (DELETE .../draft/devices/{device}).
func (s *PredictService) DiscardDraftDevice(ctx context.Context, networkID, changeSetID, deviceName string) (*Response, error) {
	path, err := s.csDevicePath(networkID, changeSetID, deviceName)
	if err != nil {
		return nil, err
	}
	return s.doJSON(ctx, http.MethodDelete, path, nil, nil, "Predict.DiscardDraftDevice")
}

// ListCommits returns the change set's version history, newest first as Forward orders it (GET .../change-sets/{id}/commits).
func (s *PredictService) ListCommits(ctx context.Context, networkID, changeSetID string) ([]ChangeSetCommit, *Response, error) {
	path, err := s.csPath(networkID, changeSetID)
	if err != nil {
		return nil, nil, err
	}
	var out []ChangeSetCommit
	resp, err := s.doJSON(ctx, http.MethodGet, path+"/commits", nil, &out, "Predict.ListCommits")
	return out, resp, err
}

// GetHeadCommit returns the latest version of the change set (GET .../commits/head).
func (s *PredictService) GetHeadCommit(ctx context.Context, networkID, changeSetID string) (*ChangeSetCommit, *Response, error) {
	path, err := s.csPath(networkID, changeSetID)
	if err != nil {
		return nil, nil, err
	}
	out := new(ChangeSetCommit)
	resp, err := s.doJSON(ctx, http.MethodGet, path+"/commits/head", nil, out, "Predict.GetHeadCommit")
	return out, resp, err
}

func (s *PredictService) commitPath(networkID, changeSetID, commitID string) (string, error) {
	path, err := s.csPath(networkID, changeSetID)
	if err != nil {
		return "", err
	}
	if commitID = strings.TrimSpace(commitID); commitID == "" {
		return "", errors.New("forward: commit ID is required")
	}
	return path + "/commits/" + url.PathEscape(commitID), nil
}

// GetCommit returns one version of the change set (GET .../commits/{commitId}).
func (s *PredictService) GetCommit(ctx context.Context, networkID, changeSetID, commitID string) (*ChangeSetCommit, *Response, error) {
	path, err := s.commitPath(networkID, changeSetID, commitID)
	if err != nil {
		return nil, nil, err
	}
	out := new(ChangeSetCommit)
	resp, err := s.doJSON(ctx, http.MethodGet, path, nil, out, "Predict.GetCommit")
	return out, resp, err
}

// RestoreCommit makes an earlier version the change set's current content (POST .../commits/{commitId}?action=restore) and
// returns the commit it created. The earlier versions stay in the history. If the caller has a pending draft, Forward commits it
// first with the note "Draft changes auto-saved", so one restore can create two commits.
func (s *PredictService) RestoreCommit(ctx context.Context, networkID, changeSetID, commitID string) (*ChangeSetCommitMetadata, *Response, error) {
	path, err := s.commitPath(networkID, changeSetID, commitID)
	if err != nil {
		return nil, nil, err
	}
	out := new(ChangeSetCommitMetadata)
	resp, err := s.doJSON(ctx, http.MethodPost, path+"?action=restore", nil, out, "Predict.RestoreCommit")
	return out, resp, err
}

// RestoreCommitAsNew copies an earlier version into a new change set (?action=restoreAsNew) and returns it. targetName and
// targetBaseSnapshotID are optional ("" lets Forward choose); a given snapshot must have finished processing up to its creation
// stage, or Forward answers with a snapshot-unavailable or processing error.
func (s *PredictService) RestoreCommitAsNew(ctx context.Context, networkID, changeSetID, commitID, targetName, targetBaseSnapshotID string) (*ChangeSetCommit, *Response, error) {
	path, err := s.commitPath(networkID, changeSetID, commitID)
	if err != nil {
		return nil, nil, err
	}
	query := url.Values{"action": []string{"restoreAsNew"}}
	if targetName = strings.TrimSpace(targetName); targetName != "" {
		query.Set("targetName", targetName)
	}
	if targetBaseSnapshotID = strings.TrimSpace(targetBaseSnapshotID); targetBaseSnapshotID != "" {
		query.Set("targetBaseSnapshotId", targetBaseSnapshotID)
	}
	out := new(ChangeSetCommit)
	resp, err := s.doJSON(ctx, http.MethodPost, path+"?"+query.Encode(), nil, out, "Predict.RestoreCommitAsNew")
	return out, resp, err
}

// EditCommitNote replaces a version's note (PATCH .../commits/{commitId}?note=); the note must not be blank.
func (s *PredictService) EditCommitNote(ctx context.Context, networkID, changeSetID, commitID, note string) (*ChangeSetCommitMetadata, *Response, error) {
	if strings.TrimSpace(note) == "" {
		return nil, nil, errors.New("forward: a commit note is required")
	}
	path, err := s.commitPath(networkID, changeSetID, commitID)
	if err != nil {
		return nil, nil, err
	}
	out := new(ChangeSetCommitMetadata)
	resp, err := s.doJSON(ctx, http.MethodPatch, path+"?"+url.Values{"note": []string{note}}.Encode(), nil, out, "Predict.EditCommitNote")
	return out, resp, err
}

// DeleteCommit removes one version from the change set's history (DELETE .../commits/{commitId}; 204) AND deletes the predicted
// snapshots made from it (ChangeSetService.deleteCommit runs deletePredictedSnapshots in the same transaction). It cannot be undone.
func (s *PredictService) DeleteCommit(ctx context.Context, networkID, changeSetID, commitID string) (*Response, error) {
	path, err := s.commitPath(networkID, changeSetID, commitID)
	if err != nil {
		return nil, err
	}
	return s.doJSON(ctx, http.MethodDelete, path, nil, nil, "Predict.DeleteCommit")
}

// SaveDeviceConfig replaces a device's whole running configuration in the change set's draft (PUT
// .../draft/devices/{device}/config, text/plain): the "config file" option. Forward stores the text as given, checks nothing about
// it, and refuses more than 2 MB (this SDK refuses it first); it replaces any staged commands for that device (a device has either
// commands or a config file). Send the complete configuration. Commit the draft, then Run. Whether Predict can model the result
// depends on the device type, which Forward finds out when it runs.
func (s *PredictService) SaveDeviceConfig(ctx context.Context, networkID, changeSetID, deviceName, config string) (*Response, error) {
	if strings.TrimSpace(config) == "" {
		return nil, errors.New("forward: a device configuration is required")
	}
	if len(config) > maxDeviceConfigBytes {
		return nil, fmt.Errorf("forward: a device configuration is limited to %d bytes, this one is %d", maxDeviceConfigBytes, len(config))
	}
	path, err := s.csDevicePath(networkID, changeSetID, deviceName)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPut, path+"/config", strings.NewReader(config))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	req = markOperation(req, "Predict.SaveDeviceConfig")
	return s.firewallDo(req, nil)
}

// GetDraftConfig returns a device's configuration as the draft would leave it (GET .../draft/devices/{device}/config). Forward
// answers 404 when there is no draft, or when the device has no commands, config or security changes in it.
func (s *PredictService) GetDraftConfig(ctx context.Context, networkID, changeSetID, deviceName string) (string, *Response, error) {
	path, err := s.csDevicePath(networkID, changeSetID, deviceName)
	if err != nil {
		return "", nil, err
	}
	return s.textGet(ctx, path+"/config", "Predict.GetDraftConfig")
}

// GetCommittedConfig returns a device's configuration as one committed version has it (GET
// .../commits/{commitId}/devices/{device}/config). Compare it with GetDraftConfig or another version for a per-device config diff.
func (s *PredictService) GetCommittedConfig(ctx context.Context, networkID, changeSetID, commitID, deviceName string) (string, *Response, error) {
	path, err := s.commitPath(networkID, changeSetID, commitID)
	if err != nil {
		return "", nil, err
	}
	if deviceName = strings.TrimSpace(deviceName); deviceName == "" {
		return "", nil, errors.New("forward: device name is required")
	}
	return s.textGet(ctx, path+"/devices/"+url.PathEscape(deviceName)+"/config", "Predict.GetCommittedConfig")
}

func (s *PredictService) textGet(ctx context.Context, path, operation string) (string, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", nil, err
	}
	// The route produces text/plain only; a JSON-only Accept would be a 406. JSON stays acceptable so an error body comes through.
	req.Header.Set("Accept", "text/plain, application/json")
	req = markOperation(req, operation)
	var body strings.Builder
	resp, err := s.firewallDo(req, &body)
	return body.String(), resp, err
}

// PreviewCommandsConfig returns the configuration a device would have if commands were applied to its BASE snapshot's config
// (the draft is ignored), without staging them (POST .../devices/{device}/config?action=preview, text in and text out): the
// "preview config changes" view. Commands are cleaned the way StageCommands cleans them; Forward answers 404 for blank commands,
// and this SDK refuses commands that are blank after cleaning.
func (s *PredictService) PreviewCommandsConfig(ctx context.Context, networkID, changeSetID, deviceName, commands string) (string, *Response, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", nil, err
	}
	path, err := s.changeSetDevicePath(networkID, changeSetID, deviceName)
	if err != nil {
		return "", nil, err
	}
	cleaned := cleanPredictCommands(commands)
	if strings.TrimSpace(cleaned) == "" {
		return "", nil, errors.New("forward: commands to preview are required (blank after cleaning)")
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path+"/config?action=preview", strings.NewReader(cleaned))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	req.Header.Set("Accept", "text/plain, application/json")
	req = markOperation(req, "Predict.PreviewCommandsConfig")
	var body strings.Builder
	resp, err := s.firewallDo(req, &body)
	return body.String(), resp, err
}

// EditSecurityRule replaces a staged or existing rule's definition (PATCH .../security-rules/{ruleId} {"definition": ...}).
// Only the local rulebase is editable (FirewallScopeLocal, FirewallRulebasePrimary). Needs EDIT_CHANGE_SET_SECURITY_DATA and the
// FIREWALL_PREDICT organization property. The definition replaces the rule's whole definition, and Enabled is always sent: leave
// it false and the rule is staged disabled.
func (s *PredictService) EditSecurityRule(ctx context.Context, networkID, changeSetID, deviceName, scopeID, rulebaseID, ruleID string, definition SecurityRuleDefinition) (*Response, error) {
	if strings.TrimSpace(definition.Name) == "" {
		return nil, errors.New("forward: security rule name is required")
	}
	return s.patchSecurityRule(ctx, networkID, changeSetID, deviceName, scopeID, rulebaseID, ruleID, map[string]any{"definition": definition}, "Predict.EditSecurityRule")
}

// MoveSecurityRule moves a rule to sit right after the rule afterRuleID in its rulebase; an empty afterRuleID moves it to the top
// (PATCH .../security-rules/{ruleId} {"predecessor": ...}).
func (s *PredictService) MoveSecurityRule(ctx context.Context, networkID, changeSetID, deviceName, scopeID, rulebaseID, ruleID, afterRuleID string) (*Response, error) {
	var predecessor any
	if afterRuleID = strings.TrimSpace(afterRuleID); afterRuleID != "" {
		predecessor = afterRuleID
	}
	return s.patchSecurityRule(ctx, networkID, changeSetID, deviceName, scopeID, rulebaseID, ruleID, map[string]any{"predecessor": predecessor}, "Predict.MoveSecurityRule")
}

func (s *PredictService) patchSecurityRule(ctx context.Context, networkID, changeSetID, deviceName, scopeID, rulebaseID, ruleID string, body any, operation string) (*Response, error) {
	if strings.TrimSpace(ruleID) == "" {
		return nil, errors.New("forward: security rule id is required")
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, err
	}
	path, err := s.rulebasePath(networkID, changeSetID, deviceName, scopeID, rulebaseID)
	if err != nil {
		return nil, err
	}
	return s.doJSON(ctx, http.MethodPatch, path+"/security-rules/"+url.PathEscape(strings.TrimSpace(ruleID)), body, nil, operation)
}

// DiscardSecurityRuleChange undoes one rule's staged add, edit, move or removal (POST .../security-rules/{ruleId}?action=discardChanges; 204).
func (s *PredictService) DiscardSecurityRuleChange(ctx context.Context, networkID, changeSetID, deviceName, scopeID, rulebaseID, ruleID string) (*Response, error) {
	if strings.TrimSpace(ruleID) == "" {
		return nil, errors.New("forward: security rule id is required")
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, err
	}
	path, err := s.rulebasePath(networkID, changeSetID, deviceName, scopeID, rulebaseID)
	if err != nil {
		return nil, err
	}
	return s.doJSON(ctx, http.MethodPost, path+"/security-rules/"+url.PathEscape(strings.TrimSpace(ruleID))+"?action=discardChanges", nil, nil, "Predict.DiscardSecurityRuleChange")
}
