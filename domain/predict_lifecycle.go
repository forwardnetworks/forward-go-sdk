package domain

import (
	"encoding/json"
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
	CommittedBy     string                     `json:"committedBy,omitempty"`
	HiddenDevices   []string                   `json:"hiddenDevices,omitempty"`
}

// ChangeSetCommitMetadata is a commit's id, note and attribution, as the restore and note calls return it.
type ChangeSetCommitMetadata struct {
	CommitID      Identifier `json:"commitId"`
	Note          string     `json:"note,omitempty"`
	CommittedByID Identifier `json:"committedById,omitempty"`
	CommittedAt   string     `json:"committedAt,omitempty"`
	// CommittedBy is the committing user's name, where Forward adds it.
	CommittedBy string `json:"committedBy,omitempty"`
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

// ChangeSetSummary is a change set's base information and the ServiceNow change requests linked to it: its name, description, tags and
// the network and snapshot it is based on, without its device changes (the full content is in ListChangeSets, GetDraft and the commits).
type ChangeSetSummary struct {
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	NetworkID   Identifier `json:"networkId"`
	SnapshotID  Identifier `json:"snapshotId"`
	Tags        []string   `json:"tags,omitempty"`
	// ServiceNowChangeRequests are the change requests linked to the change set (number and sys_id); empty when none are linked or
	// ServiceNow is not set up.
	ServiceNowChangeRequests []ServiceNowChangeRequestRef `json:"serviceNowChangeRequests,omitempty"`
}

// ServiceNowChangeRequestRef identifies a ServiceNow change request by its number and sys_id.
type ServiceNowChangeRequestRef struct {
	Number string `json:"number"`
	SysID  string `json:"sysId"`
}
