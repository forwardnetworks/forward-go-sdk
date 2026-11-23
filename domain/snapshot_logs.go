package domain

// SnapshotLogOptions narrows Snapshots.Logs. DeviceName is the name the device
// was requested under (Forward's requestedDeviceName; another name yields an
// empty log). Level is a log level filter such as "WARN". Backfilled reads the
// backfill log of one device, so it needs DeviceName.
type SnapshotLogOptions struct {
	DeviceName string
	Level      string
	Backfilled bool
}

// SnapshotCollectionMetrics is how a snapshot's collection went, device by
// device (SnapshotCollectionMetric). Times are epoch milliseconds; durations
// are milliseconds.
type SnapshotCollectionMetrics struct {
	SnapshotID                Identifier                `json:"snapshotId"`
	SnapshotTimeMillis        *int64                    `json:"snapshotTime,omitempty"`
	CollectionStartTimeMillis *int64                    `json:"collectionStartTime,omitempty"`
	CollectionEndTimeMillis   *int64                    `json:"collectionEndTime,omitempty"`
	Devices                   []DeviceCollectionMetrics `json:"metrics"`
}

// DeviceCollectionMetrics is one device's collection: when it started, how
// long it took, its slowest command, and Error -- the collection and
// processing errors merged (LabeledDeviceProblem; empty when none).
// JumpServer is the id of the jump server used, if any.
type DeviceCollectionMetrics struct {
	DeviceName                   string `json:"deviceName"`
	DeviceType                   string `json:"deviceType,omitempty"`
	SourceType                   string `json:"sourceType,omitempty"`
	ConnTypeDisplayName          string `json:"connTypeDisplayName,omitempty"`
	CollectionStartTimeMillis    *int64 `json:"collectionStartTime,omitempty"`
	CollectionDurationMillis     *int64 `json:"collectionDuration,omitempty"`
	SlowestCommand               string `json:"slowestCommand,omitempty"`
	SlowestCommandDurationMillis *int64 `json:"slowestCommandDuration,omitempty"`
	JumpServer                   string `json:"jumpServer,omitempty"`
	Error                        string `json:"error,omitempty"`
}

// CollectionExceptions are the exceptions the collectors hit while collecting
// a snapshot, deduplicated (DedupedCollectionExceptions). Total counts every
// distinct exception, of which Exceptions is the first page.
type CollectionExceptions struct {
	Total             int                   `json:"actualNumDedupedExceptions"`
	CollectorIDToName map[string]string     `json:"collectorIdToName,omitempty"`
	Exceptions        []CollectionException `json:"dedupedExceptions"`
}

// CollectionException is one distinct exception and where it happened.
// TotalOccurrences counts them all; Occurrences is the first page.
type CollectionException struct {
	StackTrace       string                          `json:"stackTrace"`
	CollectorVersion string                          `json:"collectorVersion,omitempty"`
	TotalOccurrences int                             `json:"actualNumOccurrences"`
	Occurrences      []CollectionExceptionOccurrence `json:"occurrences,omitempty"`
}

// CollectionExceptionOccurrence is one occurrence. DeviceName is empty when
// the exception was not tied to a device.
type CollectionExceptionOccurrence struct {
	CollectorID     string `json:"collectorId"`
	DeviceName      string `json:"deviceName,omitempty"`
	CollectorTaskID string `json:"collectorTaskId,omitempty"`
	Timestamp       string `json:"timestamp,omitempty"`
}

// CollectionExceptionOptions pages CollectionExceptions. Forward defaults
// Limit to 1000 and OccurrencesLimit to 10.
type CollectionExceptionOptions struct {
	Limit            *int32
	OccurrencesLimit *int32
}
