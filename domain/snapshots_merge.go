package domain

import (
	"time"
)

// SnapshotMergeType says who wins for a device that is in both the uploaded ZIP and the network's latest snapshot (Forward's
// SnapshotMergeType). The zero value is NONE.
type SnapshotMergeType string

const (
	// SnapshotMergeNone refuses the merge when any device is in both: a 409 with ErrorResponse.ConflictDevices.
	SnapshotMergeNone SnapshotMergeType = "NONE"
	// SnapshotMergeReplaceExisting takes the uploaded ZIP's data for a device in both (locations and tags follow).
	SnapshotMergeReplaceExisting SnapshotMergeType = "REPLACE_EXISTING"
	// SnapshotMergePreserveExisting keeps the latest snapshot's data for a device in both.
	SnapshotMergePreserveExisting SnapshotMergeType = "PRESERVE_EXISTING"
)

// SnapshotMergeOptions controls Snapshots.MergeIntoLatest.
type SnapshotMergeOptions struct {
	// Note is the new snapshot's note. Empty sends none.
	Note string
	// MergeType defaults to NONE; any other value than the three constants is refused before a request.
	MergeType SnapshotMergeType
	// Timeout bounds the whole upload, as SnapshotUploadOptions.Timeout does (zero keeps the client's 60 seconds, negative removes it).
	Timeout time.Duration
}

// SnapshotMergeResult is the 202 a merge answers: the new snapshot, still UNPACKING, and its id from the Location header.
type SnapshotMergeResult struct {
	Snapshot *Snapshot
	// ID is the new snapshot's id (from Location /api/snapshots/{id}, else the body's).
	ID Identifier
}
