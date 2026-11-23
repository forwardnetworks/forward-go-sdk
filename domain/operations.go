package domain

import (
	"errors"
	"time"
)

// OperationPhase is a lifecycle phase shared by composite operations.
type OperationPhase string

const (
	OperationPhaseCollecting         OperationPhase = "COLLECTING"
	OperationPhaseWaitingForSnapshot OperationPhase = "WAITING_FOR_SNAPSHOT"
	OperationPhaseProcessingSnapshot OperationPhase = "PROCESSING_SNAPSHOT"
	OperationPhaseSucceeded          OperationPhase = "SUCCEEDED"
	OperationPhaseFailed             OperationPhase = "FAILED"
)

var (
	ErrSnapshotAppearanceTimeout = errors.New("forward: snapshot appearance timeout")
	ErrSnapshotProcessingTimeout = errors.New("forward: snapshot processing timeout")
	ErrSnapshotDisappeared       = errors.New("forward: snapshot disappeared")
)

// CollectionOperationHandle contains everything needed to resume a collection
// after a process or worker restart. Persist this value, not CollectionOperation.
type CollectionOperationHandle struct {
	NetworkID                   string    `json:"networkId"`
	TaskID                      string    `json:"taskId"`
	BaselineSnapshotID          string    `json:"baselineSnapshotId,omitempty"`
	SnapshotID                  string    `json:"snapshotId,omitempty"`
	StartedAt                   time.Time `json:"startedAt"`
	SnapshotAppearanceStartedAt time.Time `json:"snapshotAppearanceStartedAt,omitempty"`
	SnapshotProcessingStartedAt time.Time `json:"snapshotProcessingStartedAt,omitempty"`
}

// CollectionOperationState is the latest state observed across both the
// collector task and resulting snapshot.
type CollectionOperationState struct {
	Handle   CollectionOperationHandle `json:"handle"`
	Phase    OperationPhase            `json:"phase"`
	Task     *CollectorTask            `json:"task,omitempty"`
	Snapshot *Snapshot                 `json:"snapshot,omitempty"`
}

// CollectionStartOptions controls baseline capture. When BaselineSnapshotID is
// nil, the SDK reads and records the newest existing snapshot before starting
// collection. A non-nil value lets an orchestrator reuse an already observed
// baseline; point to an empty string when the network has no snapshots.
type CollectionStartOptions struct {
	BaselineSnapshotID *string
}

// SnapshotOperationHandle contains everything needed to resume waiting for an
// uploaded or otherwise known snapshot.
type SnapshotOperationHandle struct {
	NetworkID  string    `json:"networkId"`
	SnapshotID string    `json:"snapshotId"`
	StartedAt  time.Time `json:"startedAt"`
	AppearedAt time.Time `json:"appearedAt,omitempty"`
}

// SnapshotOperationState is the latest state of a known snapshot.
type SnapshotOperationState struct {
	Handle   SnapshotOperationHandle `json:"handle"`
	Phase    OperationPhase          `json:"phase"`
	Snapshot *Snapshot               `json:"snapshot,omitempty"`
}
