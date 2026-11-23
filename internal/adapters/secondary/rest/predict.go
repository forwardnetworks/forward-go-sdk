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

// PredictService accesses Forward Predict and change-set APIs.
//
// Preview: these endpoints are not part of the published OpenAPI contract and
// may change between Forward releases. The SDK will evolve them during v0.x.
type Predict service

// CreateChangeSet creates a Predict change set.
func (s *Predict) CreateChangeSet(
	ctx context.Context,
	networkID string,
	request ports.ChangeSetCreateRequest,
) (*ports.ChangeSet, *ports.Response, error) {
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
	changeSet := new(ports.ChangeSet)
	resp, err := s.client.Do(req, changeSet)
	if err != nil {
		return nil, resp, err
	}
	s.client.observeCapability(ports.CapabilityPredict)
	return changeSet, resp, nil
}

// ListChangeSets lists Predict change sets for a network.
func (s *Predict) ListChangeSets(ctx context.Context, networkID string) ([]ports.ChangeSet, *ports.Response, error) {
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
	path, err := changeSetsPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.ChangeSet]{Keys: []string{"changeSets", "items"}}
	resp, err := s.client.Do(req, &result)
	if err != nil {
		return nil, resp, err
	}
	return result.Items, resp, nil
}

// DeleteChangeSet deletes a Predict change set (DELETE .../change-sets/{id}). It cannot be undone, and it also deletes the change set's
// predicted snapshots (ChangeSetDeletionListener; they answer 404 afterwards), so read what you need from them first.
func (s *Predict) DeleteChangeSet(
	ctx context.Context,
	networkID string,
	changeSetID string,
) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if err := s.client.requireCapability(ports.CapabilityPredict); err != nil {
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
func (s *Predict) StageCommands(
	ctx context.Context,
	networkID string,
	changeSetID string,
	deviceName string,
	commands string,
) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if err := s.client.requireCapability(ports.CapabilityPredict); err != nil {
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
		s.client.observeCapability(ports.CapabilityPredict)
	}
	return response, err
}

// StageBGPAdvertisement injects an external BGP advertisement into a device draft, or modifies a received one that has the same
// key and peer type (see BGPAdvertisement).
func (s *Predict) StageBGPAdvertisement(
	ctx context.Context,
	networkID string,
	changeSetID string,
	advertisement ports.BGPAdvertisement,
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
		s.client.observeCapability(ports.CapabilityPredict)
		s.client.observeCapability(ports.CapabilityStructuredBGPAdvertisements)
	}
	return response, err
}

// Commit records the staged draft as a commit on the change set, which is what Run then predicts from. With a draft the draft is
// committed; with no draft but an earlier commit nothing new is created (the head commit stands); with neither, the unchanged base
// change set is committed. A blank note is sent as DefaultCommitNote (Forward refuses a blank one). CommitWithOptions also returns the
// commit's metadata.
func (s *Predict) Commit(
	ctx context.Context,
	networkID string,
	changeSetID string,
	note string,
) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	_, resp, err := s.commit(ctx, networkID, changeSetID, ports.CommitOptions{Note: note}, "Predict.Commit")
	return resp, err
}

// CommitWithOptions is Commit with an optional analysis mode, returning the commit's metadata (id, note, who and when).
func (s *Predict) CommitWithOptions(
	ctx context.Context,
	networkID string,
	changeSetID string,
	options ports.CommitOptions,
) (*ports.ChangeSetCommitMetadata, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return s.commit(ctx, networkID, changeSetID, options, "Predict.CommitWithOptions")
}

func (s *Predict) commit(
	ctx context.Context,
	networkID string,
	changeSetID string,
	options ports.CommitOptions,
	operation string,
) (*ports.ChangeSetCommitMetadata, *ports.Response, error) {
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
	switch options.AnalysisMode {
	case "", ports.AnalysisModeBehavioral, ports.AnalysisModeConfigOnly:
	default:
		return nil, nil, fmt.Errorf("forward: unknown analysis mode %q", options.AnalysisMode)
	}
	note := strings.TrimSpace(options.Note)
	if note == "" {
		note = ports.DefaultCommitNote
	}
	query := url.Values{"note": []string{note}}
	if options.AnalysisMode != "" {
		query.Set("analysisMode", options.AnalysisMode)
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path+"/commits?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	out := new(ports.ChangeSetCommitMetadata)
	resp, err := s.client.Do(markOperation(req, operation), out)
	if err != nil {
		return nil, resp, err
	}
	s.client.observeCapability(ports.CapabilityPredict)
	return out, resp, nil
}

// Run starts predictive analysis and returns the predicted snapshot's metadata (not its state: use ListPredictedSnapshots). Processing
// continues asynchronously. Forward commits an uncommitted draft first with the note "Draft changes auto-saved", answers 404 when the
// change set has neither a draft nor a commit, and returns the EXISTING predicted snapshot when one already matches the commit and
// analysis mode instead of starting another. A config-only change set whose changes are only BGP advertisements is a 400. A prediction
// over a STALE base snapshot (one processed before a Forward upgrade's reprocess) can fail with no reason through any route (seen live as a
// 400 or 500 from run or commit, and a predicted snapshot that never processes): reprocess the base first.
func (s *Predict) Run(
	ctx context.Context,
	networkID string,
	changeSetID string,
	note string,
) (*ports.PredictedSnapshot, *ports.Response, error) {
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
	params := url.Values{"action": []string{"predict"}, "note": []string{note}}
	path += "?" + params.Encode()
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, nil, err
	}
	snapshot := new(ports.PredictedSnapshot)
	resp, err := s.client.Do(req, snapshot)
	if err != nil {
		return nil, resp, err
	}
	s.client.observeCapability(ports.CapabilityPredict)
	return snapshot, resp, nil
}

// ListPredictedSnapshots returns the prediction history for a change set.
func (s *Predict) ListPredictedSnapshots(
	ctx context.Context,
	networkID string,
	changeSetID string,
) ([]ports.PredictedSnapshot, *ports.Response, error) {
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
	path += "/predicted-snapshots"
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.PredictedSnapshot]{Keys: []string{"snapshots", "predictedSnapshots", "items"}}
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
