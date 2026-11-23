package ports

import (
	"net/url"
	"time"
)

// SnapshotListOptions contains published filters plus preview compatibility
// options used by Forward builds before they enter the OpenAPI description.
type SnapshotListOptions struct {
	State                       string
	MinSuccessfulDevices        *int32
	MinSuccessfulDevicePercent  *float64
	MaxCollectionFailureDevices *int32
	MaxCollectionFailurePercent *float64
	MaxParsingFailureDevices    *int32
	MaxParsingFailurePercent    *float64
	Limit                       *int32
	IncludeArchived             *bool

	// IncludePredicted asks for Predict's forked snapshots (those with a
	// ParentSnapshotID) as well. From Forward 26.11 (FWD-61712) the listing
	// leaves them out unless this is true, so that State "PROCESSED" with a
	// Limit of 1 is always the newest collected snapshot even on a network that
	// uses Predict (drafts and imports are not collections, so "collected" is
	// loose); earlier builds ignore the parameter and list them anyway.
	// Get sets it, because it looks a snapshot up by ID and the snapshot may be
	// a prediction.
	IncludePredicted *bool

	// Preview: MaxResults and SkipUnprocessed are accepted by some Forward
	// versions but are not in the published snapshot OpenAPI contract.
	MaxResults      *int32
	SkipUnprocessed *bool

	// ExtraQuery carries version-specific flags that the typed SDK does not yet
	// know. Typed fields above take precedence over duplicate keys.
	ExtraQuery url.Values
}

// SnapshotUploadOptions controls snapshot import behavior.
type SnapshotUploadOptions struct {
	Note string

	// Preview: Async returns after acceptance rather than after processing.
	Async bool
	// Preview: ExcludeFailedDevices removes failed devices while merging files.
	ExcludeFailedDevices bool
	// Preview: SkipSnapshotProcessing stores the upload without starting
	// processing. The caller must explicitly reprocess it later.
	SkipSnapshotProcessing bool

	// ExtraQuery carries additional version-specific upload flags. Typed fields
	// above take precedence over duplicate keys.
	ExtraQuery url.Values

	// Timeout bounds the whole upload. Zero keeps the client's own timeout,
	// which is 60 seconds by default and covers the streamed body, so a large
	// snapshot needs more; a positive value replaces it; a negative value
	// removes it, leaving the context as the only limit. Forward itself sets no
	// size limit by default (uploads.max_size is -1), though a deployment can.
	Timeout time.Duration
}
