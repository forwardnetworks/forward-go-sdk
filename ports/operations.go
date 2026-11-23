package ports

import (
	"time"
)

// CollectionOperationUpdate is delivered after each successful server
// observation. Previous and Current make transition-oriented callbacks easy.
type CollectionOperationUpdate struct {
	Previous CollectionOperationState
	Current  CollectionOperationState
	Response *Response
	Attempt  int
	Elapsed  time.Duration
}

// CollectionWaitOptions controls collection-to-snapshot orchestration. A zero
// timeout selects the documented default; use the parent context for a tighter
// overall deadline.
type CollectionWaitOptions struct {
	PollInterval              time.Duration
	SnapshotAppearanceTimeout time.Duration
	SnapshotProcessingTimeout time.Duration
	MissingSnapshotThreshold  int
	OnUpdate                  func(CollectionOperationUpdate)
}

type SnapshotOperationUpdate struct {
	Previous SnapshotOperationState
	Current  SnapshotOperationState
	Response *Response
	Attempt  int
	Elapsed  time.Duration
}

type SnapshotWaitOptions struct {
	PollInterval             time.Duration
	AppearanceTimeout        time.Duration
	ProcessingTimeout        time.Duration
	MissingSnapshotThreshold int
	OnUpdate                 func(SnapshotOperationUpdate)
}
