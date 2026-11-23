package domain

import (
	"time"
)

// SnapshotMetrics is Forward's per-snapshot collection and processing health
// (com.forwardnetworks.cv.metric.SnapshotMetrics), from
// GET /api/snapshots/{snapshotId}/metrics.
//
// The failure maps count devices (or endpoints) per error name:
// DeviceCollectionFailures is keyed by DeviceCollectionError
// (AUTHENTICATION_FAILED, AUTHORIZATION_FAILED, ...), DeviceProcessingFailures
// by DeviceProcessingError (PARSER_EXCEPTION, LICENSE_EXHAUSTED, DUPLICATE,
// ...). Error names are version-dependent, so they stay strings.
type SnapshotMetrics struct {
	SnapshotID Identifier `json:"snapshotId"`
	CreatedAt  string     `json:"createdAt,omitempty"`

	NumSuccessfulDevices          int `json:"numSuccessfulDevices"`
	NumCollectionFailureDevices   int `json:"numCollectionFailureDevices"`
	NumProcessingFailureDevices   int `json:"numProcessingFailureDevices"`
	NumSuccessfulEndpoints        int `json:"numSuccessfulEndpoints"`
	NumCollectionFailureEndpoints int `json:"numCollectionFailureEndpoints"`
	NumProcessingFailureEndpoints int `json:"numProcessingFailureEndpoints"`

	DeviceCollectionFailures   map[string]int `json:"deviceCollectionFailures,omitempty"`
	DeviceProcessingFailures   map[string]int `json:"deviceProcessingFailures,omitempty"`
	EndpointCollectionFailures map[string]int `json:"endpointCollectionFailures,omitempty"`
	EndpointProcessingFailures map[string]int `json:"endpointProcessingFailures,omitempty"`

	// Durations are milliseconds on the wire. Nil means Forward has none: no
	// collection data (an import), or processing that has not finished.
	CollectionDurationMillis *int64 `json:"collectionDuration,omitempty"`
	ProcessingDurationMillis *int64 `json:"processingDuration,omitempty"`

	// Index statuses are StatusData.Status values (SUCCESS, FAILURE,
	// CANCELED, ...).
	L2IndexingStatus         string `json:"l2IndexingStatus,omitempty"`
	HostComputationStatus    string `json:"hostComputationStatus,omitempty"`
	IPLocationIndexingStatus string `json:"ipLocationIndexingStatus,omitempty"`
	PathSearchIndexingStatus string `json:"pathSearchIndexingStatus,omitempty"`
	SearchIndexingStatus     string `json:"searchIndexingStatus,omitempty"`
	SnapshotState            string `json:"snapshotState,omitempty"`
}

// SnapshotException is one distinct exception Forward hit while building a
// snapshot (com.forwardnetworks.cv.snapshot.service.SnapshotException):
// ExceptionType is PARSING, MODELING, SNAPSHOT_GENERATION, REACHABILITY or
// SHERLOCK_INDEX; Devices names the devices it occurred on (empty for a
// snapshot-wide stage); Occurrences counts them.
type SnapshotException struct {
	StackTrace    string   `json:"stackTrace"`
	Occurrences   int      `json:"occurrences"`
	ExceptionType string   `json:"exceptionType"`
	Devices       []string `json:"devices,omitempty"`
}

// SnapshotProgress is how far a snapshot's processing has got
// (SnapshotProgressReport), from GET /api/snapshots/{snapshotId}/progress. Done
// is Forward's own verdict: every stage but ADVANCED_REACHABILITY finished.
type SnapshotProgress struct {
	NetworkID  Identifier              `json:"networkId"`
	SnapshotID Identifier              `json:"snapshotId"`
	Stages     []SnapshotProgressStage `json:"stages"`
	Done       bool                    `json:"done"`
}

// SnapshotProgressStage is one processing stage (ProcessingStageInfo). Stage
// (CREATION, TEXT_SEARCH_INDEX, REACHABILITY, ADVANCED_REACHABILITY, ...) and
// OperationState (NOT_TRIGGERED, QUEUED, COMPUTING, SUCCEEDED, FAILED, ...)
// stay strings so a new value does not break decoding. StartedAtMillis and
// UpdatedAtMillis are epoch milliseconds, nil while the stage has not started;
// NumObjects is nil until the stage has reported.
type SnapshotProgressStage struct {
	Stage           string `json:"stage"`
	OperationState  string `json:"operationState"`
	StartedAtMillis *int64 `json:"startedAt,omitempty"`
	UpdatedAtMillis *int64 `json:"updatedAt,omitempty"`
	NumObjects      *int64 `json:"numObjects,omitempty"`
}

// StartedAt returns when the stage started, and false if it has not.
func (s SnapshotProgressStage) StartedAt() (time.Time, bool) {
	return millisTime(s.StartedAtMillis)
}

// UpdatedAt returns when the stage last reported, and false if it has not.
func (s SnapshotProgressStage) UpdatedAt() (time.Time, bool) {
	return millisTime(s.UpdatedAtMillis)
}

func millisTime(ms *int64) (time.Time, bool) {
	if ms == nil {
		return time.Time{}, false
	}
	return time.UnixMilli(*ms), true
}

// SnapshotProcessEstimate is Forward's estimate of how long each processing
// stage takes for this snapshot (ProcessEstimate), in milliseconds per
// ProgressStage name (DEVICE_MODEL_GENERATION, SNAPSHOT_GENERATION, ...).
// Stage names are version-dependent, so they stay strings.
type SnapshotProcessEstimate struct {
	StageToDurationMillis map[string]int64 `json:"stageToDuration"`
}

// Duration returns the estimate for one stage, and false if Forward gave none.
func (e SnapshotProcessEstimate) Duration(stage string) (time.Duration, bool) {
	ms, ok := e.StageToDurationMillis[stage]
	return time.Duration(ms) * time.Millisecond, ok
}
