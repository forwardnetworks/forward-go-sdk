package forward

// Snapshot processing states as Forward reports them in Snapshot.State and PredictedSnapshot.State. The fields stay strings, because a
// newer Forward may add a state; these name the ones the SDK's own waits act on (snapshotProcessingDone): PROCESSED is the only
// success, and the rest end a wait with an error.
const (
	SnapshotStateProcessed     = "PROCESSED"
	SnapshotStateFailed        = "FAILED"
	SnapshotStateCanceled      = "CANCELED"
	SnapshotStateTimedOut      = "TIMED_OUT"
	SnapshotStateRestoreFailed = "RESTORE_FAILED"
)
