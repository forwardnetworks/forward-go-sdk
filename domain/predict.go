package domain

import (
	"bytes"
	"encoding/json"
)

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
//
// Repeating a call is safe, by Forward's own rules (DeviceChanges.addNewAdvertisements / addRemovedAdvertisements / withDeletedAdvertisements,
// pinned by DeviceChangesTest; source evidence, not recorded live): staging a key again replaces the staged one rather than adding a second
// (so StageBGPAdvertisements with a key already staged, by bulkAdd or by add, leaves one), withdrawing a key again leaves one withdrawal, and
// restoring a key that has nothing staged is ignored. A retried stage, withdraw or restore after a crash therefore ends in the same draft. The SDK does
// NOT mark these calls idempotent for you: its retry policy re-sends a write after a 5xx or a written-then-failed transport error only when
// the caller asserts it with WithIdempotent(ctx), so wrap these calls in it when you want that. What is not covered: staging and
// withdrawing the same key in one change set cancel each other (the later call wins), so replay in the original order.
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

// DefaultCommitNote is the note Commit and CommitWithOptions send when the caller gives none: Forward requires a note
// (POST .../commits?note=, a missing one is a 400).
const DefaultCommitNote = "Committed by forward-go-sdk"

// CommitOptions tunes CommitWithOptions. AnalysisMode is AnalysisModeBehavioral or AnalysisModeConfigOnly; empty sends nothing and
// Forward uses behavioral. Forward only honours it when the organization property PREDICT_CONFIG_ONLY is on; the first commit that names
// one fixes it for the change set and a different one later is a 409. A config-only change set whose only changes are BGP advertisements
// is refused (400) when it is RUN, not when it is committed. Forward writes "Chose ... Predict" ahead of the note when the named mode differs from the change set's current one and PREDICT_CONFIG_ONLY is on.
// Builds before PREDICT_CONFIG_ONLY ignore the parameter.
type CommitOptions struct {
	Note         string
	AnalysisMode string
}
