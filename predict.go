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

// PredictService accesses Forward Predict and change-set APIs.
//
// Preview: these endpoints are not part of the published OpenAPI contract and
// may change between Forward releases. The SDK will evolve them during v0.x.
type PredictService service

// ChangeSetCreateRequest creates a Predict change set from a base snapshot.
// The network ID belongs in the request path and must not be included here.
//
// Predict does not model every CLI side effect. In affected appserver builds,
// CLI `network <prefix>` statements produce CONFIG protocol seeds; Predict has
// no CONFIG propagation strategy and silently discards those seeds. Model new
// BGP originations with StageBGPAdvertisement instead.
type ChangeSetCreateRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	SnapshotID  string   `json:"snapshotId"`
	Tags        []string `json:"tags,omitempty"`
	// Directory puts the new change set in a change-set directory ("/" or "/a/b"; POST ...?dirPath=). Empty leaves it in the root.
	// It is sent as a query parameter, not in the body. Forward creates the change set BEFORE it files it, with no transaction: for a
	// directory that does not exist the call fails 404 but the change set exists (unfiled). Look it up by name before retrying.
	Directory string `json:"-"`
}

// Analysis modes of a change set (Forward's PredictAnalysisMode). CONFIG_ONLY compares configuration only and is only chosen when the
// organization property PREDICT_CONFIG_ONLY is on; the mode is fixed by the first commit and a later commit that names another is a 409.
const (
	AnalysisModeBehavioral = "BEHAVIORAL"
	AnalysisModeConfigOnly = "CONFIG_ONLY"
)

// ChangeSet is the common metadata returned for a Predict change set. The attribution fields and AnalysisMode are Forward's current
// shape; builds before PREDICT_CONFIG_ONLY leave AnalysisMode empty (it is also empty until a first commit fixes it).
//
// ListChangeSets rows (Forward's ChangeSetInfo) add ModifiedDeviceCount and PredictedSnapshots, newest first, and never carry
// DeviceToChanges; Create, Update and Duplicate answers are built from metadata alone and do not either.
type ChangeSet struct {
	ID              Identifier                 `json:"id"`
	Name            string                     `json:"name"`
	Description     string                     `json:"description,omitempty"`
	NetworkID       Identifier                 `json:"networkId"`
	SnapshotID      Identifier                 `json:"snapshotId"`
	Tags            []string                   `json:"tags,omitempty"`
	DeviceToChanges map[string]json.RawMessage `json:"deviceToChanges,omitempty"`
	AnalysisMode    string                     `json:"analysisMode,omitempty"`
	CreatedByID     Identifier                 `json:"createdById,omitempty"`
	CreatedAt       string                     `json:"createdAt,omitempty"`
	CreatedBy       string                     `json:"createdBy,omitempty"`
	UpdatedByID     Identifier                 `json:"updatedById,omitempty"`
	UpdatedAt       string                     `json:"updatedAt,omitempty"`
	UpdatedBy       string                     `json:"updatedBy,omitempty"`
	// ModifiedDeviceCount and PredictedSnapshots are only on ListChangeSets rows.
	ModifiedDeviceCount *int                   `json:"modifiedDeviceCount,omitempty"`
	PredictedSnapshots  []PredictedSnapshotRef `json:"predictedSnapshots,omitempty"`
}

// PredictedSnapshotRef names a snapshot (Forward's SnapshotIdentificationLight): a ListChangeSets row's predicted snapshots, newest first,
// and a predicted snapshot's base snapshot. A bare id string decodes too.
type PredictedSnapshotRef struct {
	ID              Identifier `json:"id"`
	CreationInstant string     `json:"creationInstant,omitempty"`
	NetworkID       Identifier `json:"networkId,omitempty"`
}

// UnmarshalJSON accepts the object Forward sends or a bare snapshot id.
func (r *PredictedSnapshotRef) UnmarshalJSON(data []byte) error {
	if trimmed := bytes.TrimSpace(data); len(trimmed) > 0 && trimmed[0] != '{' {
		var id Identifier
		if err := id.UnmarshalJSON(trimmed); err != nil {
			return err
		}
		*r = PredictedSnapshotRef{ID: id}
		return nil
	}
	type plain PredictedSnapshotRef
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*r = PredictedSnapshotRef(p)
	return nil
}

// SnapshotPredictInfo is what Forward records on a predicted snapshot (SnapshotPredictInfo, JSON "predictInfo"): the base snapshot, the
// change set and commit it predicts, how many devices the change set modified and the analysis mode it ran in.
type SnapshotPredictInfo struct {
	BaseSnapshot        *PredictedSnapshotRef `json:"baseSnapshot,omitempty"`
	ChangeSetID         Identifier            `json:"changeSetId,omitempty"`
	CommitID            Identifier            `json:"commitId,omitempty"`
	Name                string                `json:"name,omitempty"`
	ModifiedDeviceCount int                   `json:"modifiedDeviceCount,omitempty"`
	CommittedAt         string                `json:"committedAt,omitempty"`
	AnalysisMode        string                `json:"analysisMode,omitempty"`
}

// PredictedSnapshot is the subset of predicted snapshot metadata required to
// track processing. State and AdvancedReachabilityState are only on ListPredictedSnapshots rows (Forward's SnapshotInfo): Run answers
// the snapshot's metadata alone, so State is empty there. Run also answers the EXISTING predicted snapshot when one already matches the
// commit and mode, rather than a new one.
type PredictedSnapshot struct {
	ID                        Identifier           `json:"id"`
	State                     string               `json:"state"`
	AdvancedReachabilityState string               `json:"advancedReachabilityState,omitempty"`
	CreatedAt                 string               `json:"createdAt,omitempty"`
	ProcessedAt               string               `json:"processedAt,omitempty"`
	ProcessingTrigger         string               `json:"processingTrigger,omitempty"`
	ParentSnapshotID          Identifier           `json:"parentSnapshotId,omitempty"`
	IsDraft                   bool                 `json:"isDraft,omitempty"`
	Note                      string               `json:"note,omitempty"`
	PredictInfo               *SnapshotPredictInfo `json:"predictInfo,omitempty"`
}

// BGPAdvertisement is an external advertisement staged in a Predict change
// set. Device identifies the receiving device and is placed in the URL, not the
// JSON body. This structured form is the only reliable way to add an origination
// to Predict on builds that discard CONFIG protocol seeds generated by CLI.
//
// Forward's inject, modify and withdraw actions are not distinguished by Type: Type is the peer type, "EBGP" (the default) or
// "IBGP", and an EBGP advertisement needs a non-empty ASPath. Origin is IGP, EGP, INCOMPLETE or DEFAULT_ORIGINATE. The
// advertisement's identity is its key (VRF, ExternalPeer, Prefix). Inject and modify are the same call, StageBGPAdvertisement(s)
// (?action=add): a staged advertisement replaces an earlier staged one with the same key and cancels a staged withdrawal of it,
// and at prediction time it overrides a RECEIVED advertisement with the same key only when its Type matches that peer's type
// (otherwise the received route stays and the staged one is added next to it). WithdrawBGPAdvertisement(s) withdraws a received
// advertisement by key, and RestoreBGPAdvertisement(s) drops whatever is staged for a key, back to the collected state.
type BGPAdvertisement struct {
	Device       string   `json:"-"`
	VRF          string   `json:"vrf"`
	ExternalPeer string   `json:"externalPeer"`
	Prefix       string   `json:"prefix"`
	NextHop      string   `json:"nextHop"`
	Type         string   `json:"type"`
	Origin       string   `json:"origin"`
	LocalPref    *int64   `json:"localPref,omitempty"`
	ASPath       []int64  `json:"asPath"`
	MED          *int64   `json:"med,omitempty"`
	Communities  []string `json:"communities"`
}

// CreateChangeSet creates a Predict change set.
func (s *PredictService) CreateChangeSet(
	ctx context.Context,
	networkID string,
	request ChangeSetCreateRequest,
) (*ChangeSet, *Response, error) {
	if err := s.client.requireCapability(CapabilityPredict); err != nil {
		return nil, nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	path, err := changeSetsPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(request.Name) == "" || strings.TrimSpace(request.SnapshotID) == "" {
		return nil, nil, errors.New("forward: change-set name and base snapshot ID are required")
	}
	if dir := strings.TrimSpace(request.Directory); dir != "" {
		if !validDirPath(dir) {
			return nil, nil, errors.New(`forward: a change-set directory path is "/" or starts with "/" and does not end with "/"`)
		}
		path += "?" + url.Values{"dirPath": []string{dir}}.Encode()
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, request)
	if err != nil {
		return nil, nil, err
	}
	changeSet := new(ChangeSet)
	resp, err := s.client.Do(req, changeSet)
	if err != nil {
		return nil, resp, err
	}
	s.client.observeCapability(CapabilityPredict)
	return changeSet, resp, nil
}

// ListChangeSets lists Predict change sets for a network.
func (s *PredictService) ListChangeSets(ctx context.Context, networkID string) ([]ChangeSet, *Response, error) {
	if err := s.client.requireCapability(CapabilityPredict); err != nil {
		return nil, nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	path, err := changeSetsPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[ChangeSet]{Keys: []string{"changeSets", "items"}}
	resp, err := s.client.Do(req, &result)
	if err != nil {
		return nil, resp, err
	}
	return result.Items, resp, nil
}

// DeleteChangeSet deletes a Predict change set (DELETE .../change-sets/{id}). It cannot be undone.
func (s *PredictService) DeleteChangeSet(
	ctx context.Context,
	networkID string,
	changeSetID string,
) (*Response, error) {
	if err := s.client.requireCapability(CapabilityPredict); err != nil {
		return nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, err
	}
	path, err := changeSetPath(networkID, changeSetID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(markOperation(req, "Predict.DeleteChangeSet"), nil)
}

// StageCommands replaces a device's draft CLI commands. Interactive wrapper
// lines are removed because Predict accepts configuration-mode content only.
// A CLI `network <prefix>` is still not a reliable BGP origination; use the
// structured StageBGPAdvertisement path described above.
func (s *PredictService) StageCommands(
	ctx context.Context,
	networkID string,
	changeSetID string,
	deviceName string,
	commands string,
) (*Response, error) {
	if err := s.client.requireCapability(CapabilityPredict); err != nil {
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
	path += "/commands"
	req, err := s.client.NewRequest(ctx, http.MethodPut, path, strings.NewReader(cleanPredictCommands(commands)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	response, err := s.client.Do(req, nil)
	if err == nil {
		s.client.observeCapability(CapabilityPredict)
	}
	return response, err
}

// StageBGPAdvertisement injects an external BGP advertisement into a device draft, or modifies a received one that has the same
// key and peer type (see BGPAdvertisement).
func (s *PredictService) StageBGPAdvertisement(
	ctx context.Context,
	networkID string,
	changeSetID string,
	advertisement BGPAdvertisement,
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
	path, err := predictDevicePath(networkID, changeSetID, advertisement.Device)
	if err != nil {
		return nil, err
	}
	path += "/bgp-advertisements?action=add"
	if advertisement.ASPath == nil {
		advertisement.ASPath = []int64{}
	}
	if advertisement.Communities == nil {
		advertisement.Communities = []string{}
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, advertisement)
	if err != nil {
		return nil, err
	}
	response, err := s.client.Do(req, nil)
	if err == nil {
		s.client.observeCapability(CapabilityPredict)
		s.client.observeCapability(CapabilityStructuredBGPAdvertisements)
	}
	return response, err
}

// DefaultCommitNote is the note Commit and CommitWithOptions send when the caller gives none: Forward requires a note
// (POST .../commits?note=, a missing one is a 400).
const DefaultCommitNote = "Committed by forward-go-sdk"

// Commit records the staged draft as a commit on the change set, which is what Run then predicts from. With a draft the draft is
// committed; with no draft but an earlier commit nothing new is created (the head commit stands); with neither, the unchanged base
// change set is committed. A blank note is sent as DefaultCommitNote (Forward refuses a blank one). CommitWithOptions also returns the
// commit's metadata.
func (s *PredictService) Commit(
	ctx context.Context,
	networkID string,
	changeSetID string,
	note string,
) (*Response, error) {
	_, resp, err := s.commit(ctx, networkID, changeSetID, CommitOptions{Note: note}, "Predict.Commit")
	return resp, err
}

// CommitOptions tunes CommitWithOptions. AnalysisMode is AnalysisModeBehavioral or AnalysisModeConfigOnly; empty sends nothing and
// Forward uses behavioral. Forward only honours it when the organization property PREDICT_CONFIG_ONLY is on; the first commit that names
// one fixes it for the change set and a different one later is a 409. A config-only change set whose only changes are BGP advertisements
// is refused (400) when it is RUN, not when it is committed. Forward writes "Chose ... Predict" ahead of the note when the named mode differs from the change set's current one and PREDICT_CONFIG_ONLY is on.
// Builds before PREDICT_CONFIG_ONLY ignore the parameter.
type CommitOptions struct {
	Note         string
	AnalysisMode string
}

// CommitWithOptions is Commit with an optional analysis mode, returning the commit's metadata (id, note, who and when).
func (s *PredictService) CommitWithOptions(
	ctx context.Context,
	networkID string,
	changeSetID string,
	options CommitOptions,
) (*ChangeSetCommitMetadata, *Response, error) {
	return s.commit(ctx, networkID, changeSetID, options, "Predict.CommitWithOptions")
}

func (s *PredictService) commit(
	ctx context.Context,
	networkID string,
	changeSetID string,
	options CommitOptions,
	operation string,
) (*ChangeSetCommitMetadata, *Response, error) {
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
	switch options.AnalysisMode {
	case "", AnalysisModeBehavioral, AnalysisModeConfigOnly:
	default:
		return nil, nil, fmt.Errorf("forward: unknown analysis mode %q", options.AnalysisMode)
	}
	note := strings.TrimSpace(options.Note)
	if note == "" {
		note = DefaultCommitNote
	}
	query := url.Values{"note": []string{note}}
	if options.AnalysisMode != "" {
		query.Set("analysisMode", options.AnalysisMode)
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path+"/commits?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	out := new(ChangeSetCommitMetadata)
	resp, err := s.client.Do(markOperation(req, operation), out)
	if err != nil {
		return nil, resp, err
	}
	s.client.observeCapability(CapabilityPredict)
	return out, resp, nil
}

// Run starts predictive analysis and returns the predicted snapshot's metadata (not its state: use ListPredictedSnapshots). Processing
// continues asynchronously. Forward commits an uncommitted draft first with the note "Draft changes auto-saved", answers 404 when the
// change set has neither a draft nor a commit, and returns the EXISTING predicted snapshot when one already matches the commit and
// analysis mode instead of starting another. A config-only change set whose changes are only BGP advertisements is a 400.
func (s *PredictService) Run(
	ctx context.Context,
	networkID string,
	changeSetID string,
	note string,
) (*PredictedSnapshot, *Response, error) {
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
	params := url.Values{"action": []string{"predict"}, "note": []string{note}}
	path += "?" + params.Encode()
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, nil, err
	}
	snapshot := new(PredictedSnapshot)
	resp, err := s.client.Do(req, snapshot)
	if err != nil {
		return nil, resp, err
	}
	s.client.observeCapability(CapabilityPredict)
	return snapshot, resp, nil
}

// RunOperation starts Predict and returns a handle that polls the predicted
// snapshot metadata route until processing reaches a terminal state.
func (s *PredictService) RunOperation(
	ctx context.Context,
	networkID string,
	changeSetID string,
	note string,
) (*Poller[Snapshot], *Response, error) {
	predicted, response, err := s.Run(ctx, networkID, changeSetID, note)
	if err != nil {
		return nil, response, err
	}
	initial := &Snapshot{ID: predicted.ID, State: predicted.State, CreatedAt: predicted.CreatedAt, ProcessedAt: predicted.ProcessedAt, ProcessingTrigger: predicted.ProcessingTrigger, Note: predicted.Note}
	poller, err := NewPoller(initial, func(ctx context.Context) (*Snapshot, *Response, error) {
		return s.client.Snapshots.Get(ctx, networkID, string(predicted.ID))
	}, snapshotProcessingDone)
	return poller, response, err
}

func snapshotProcessingDone(snapshot *Snapshot) (bool, error) {
	switch strings.ToUpper(snapshot.State) {
	case "PROCESSED":
		return true, nil
	case "FAILED", "CANCELED", "TIMED_OUT", "RESTORE_FAILED":
		return true, fmt.Errorf("forward: snapshot %s ended with state %s", snapshot.ID, snapshot.State)
	default:
		return false, nil
	}
}

// ListPredictedSnapshots returns the prediction history for a change set.
func (s *PredictService) ListPredictedSnapshots(
	ctx context.Context,
	networkID string,
	changeSetID string,
) ([]PredictedSnapshot, *Response, error) {
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
	path += "/predicted-snapshots"
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[PredictedSnapshot]{Keys: []string{"snapshots", "predictedSnapshots", "items"}}
	resp, err := s.client.Do(req, &result)
	if err != nil {
		return nil, resp, err
	}
	return result.Items, resp, nil
}

func cleanPredictCommands(commands string) string {
	drop := map[string]bool{
		"enable": true, "write memory": true, "write mem": true,
		"wr mem": true, "wr": true, "!": true, "exit": true,
	}
	lines := strings.Split(commands, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if !drop[strings.ToLower(strings.TrimSpace(line))] {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

func changeSetsPath(networkID string) (string, error) {
	networkID = strings.TrimSpace(networkID)
	if networkID == "" {
		return "", errors.New("forward: network ID is required")
	}
	return fmt.Sprintf("/api/networks/%s/change-sets", url.PathEscape(networkID)), nil
}

func changeSetPath(networkID, changeSetID string) (string, error) {
	path, err := changeSetsPath(networkID)
	if err != nil {
		return "", err
	}
	changeSetID = strings.TrimSpace(changeSetID)
	if changeSetID == "" {
		return "", errors.New("forward: change-set ID is required")
	}
	return path + "/" + url.PathEscape(changeSetID), nil
}

func predictDevicePath(networkID, changeSetID, deviceName string) (string, error) {
	path, err := changeSetPath(networkID, changeSetID)
	if err != nil {
		return "", err
	}
	deviceName = strings.TrimSpace(deviceName)
	if deviceName == "" {
		return "", errors.New("forward: device name is required")
	}
	return fmt.Sprintf("%s/draft/devices/%s", path, url.PathEscape(deviceName)), nil
}
