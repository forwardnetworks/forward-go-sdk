package domain

import (
	"errors"
	"io"
	"strings"
	"time"
)

// Snapshot describes a Forward network snapshot.
type Snapshot struct {
	ID                        Identifier `json:"id"`
	State                     string     `json:"state,omitempty"`
	AdvancedReachabilityState string     `json:"advancedReachabilityState,omitempty"`
	CreatedAt                 string     `json:"createdAt,omitempty"`
	ProcessedAt               string     `json:"processedAt,omitempty"`
	RestoredAt                string     `json:"restoredAt,omitempty"`
	FavoritedAt               string     `json:"favoritedAt,omitempty"`
	FavoritedBy               string     `json:"favoritedBy,omitempty"`
	FavoritedByUserID         Identifier `json:"favoritedByUserId,omitempty"`
	ParentSnapshotID          Identifier `json:"parentSnapshotId,omitempty"`
	ChangeSetID               Identifier `json:"changeSetId,omitempty"`
	ProcessingTrigger         string     `json:"processingTrigger,omitempty"`
	// CollectionTaskID ties a collected snapshot back to the collector task
	// that produced it. Forward records it without the task id's prefix, so
	// task "P1021" appears here as "1021".
	CollectionTaskID Identifier `json:"collectionTaskId,omitempty"`
	Note             string     `json:"note,omitempty"`
	IsDraft          bool       `json:"isDraft,omitempty"`
	IsPredicted      bool       `json:"isPredicted,omitempty"`
	// PredictInfo is on a predicted snapshot only (base snapshot, change set, commit, analysis mode); see SnapshotPredictInfo.
	PredictInfo  *SnapshotPredictInfo `json:"predictInfo,omitempty"`
	TotalDevices int                  `json:"totalDevices,omitempty"`
	Message      string               `json:"message,omitempty"`
}

// SnapshotStateTransition is returned by snapshot invalidation/reprocessing.
type SnapshotStateTransition struct {
	PreviousState string `json:"previousState"`
	State         string `json:"state"`
}

// SnapshotUploadFile is one snapshot ZIP to import. Multiple files are merged
// by Forward and must not contain overlapping devices.
type SnapshotUploadFile struct {
	Name   string
	Reader io.Reader
}

// SnapshotSubsetRequest selects one side of a disjoint snapshot merge. Exactly
// one of IncludeDevices or ExcludeDevices must be non-empty.
type SnapshotSubsetRequest struct {
	IncludeDevices []string
	ExcludeDevices []string
	PollInterval   time.Duration
	Attempts       int
}

var ErrNoSnapshots = errors.New("forward: network has no snapshots")

// ErrSnapshotNotFound reports that a network holds no snapshot of that ID.
var ErrSnapshotNotFound = errors.New("forward: snapshot not found")

// Snapshot.AdvancedReachabilityState values (Forward's AdvancedReachabilityState). The field stays a string, so a value a newer build adds
// still decodes; compare it with these.
const (
	AdvancedReachabilityUnprocessed = "UNPROCESSED"
	AdvancedReachabilityProcessing  = "PROCESSING"
	AdvancedReachabilityProcessed   = "PROCESSED"
	AdvancedReachabilityFailed      = "FAILED"
	AdvancedReachabilityCanceled    = "CANCELED"
	AdvancedReachabilityTimedOut    = "TIMED_OUT"
)

// Predicted reports whether the snapshot came from Predict rather than a
// collection or import, so it describes a state the network was never in.
// Queries that read live state (counters, performance) come back empty on one
// whatever the network looks like, so an empty result there is not a clean
// network.
//
// ProcessingTrigger is the field Forward sets -- PREDICT where a collection
// reads COLLECTION -- and the rest are corroborating signals older builds and
// other routes supply instead. ParentSnapshotID is Forward's own definition of
// a fork (SnapshotMeta.isFork), which is what its automatic snapshot choice
// skips: NQE and other snapshot-scoped calls made without an explicit
// snapshot ID never land on a predicted snapshot; only an ID that came from
// one does.
//
// The method is not called IsPredicted because Snapshot already has a JSON
// field of that name, which only some builds send.
func (s Snapshot) Predicted() bool {
	return strings.EqualFold(s.ProcessingTrigger, "PREDICT") ||
		s.IsPredicted || s.ParentSnapshotID != "" || s.ChangeSetID != ""
}
