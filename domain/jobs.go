package domain

// ActiveJobInfo is one row (or a group of identical rows) of currently running or queued work.
//
// GET /api/jobs/active (JobsController.getActiveJobInfos).
type ActiveJobInfo struct {
	JobType                   string            `json:"jobType"`
	OrgName                   string            `json:"orgName"`
	NetworkID                 int64             `json:"networkId"`
	SnapshotID                *int64            `json:"snapshotId,omitempty"`
	CreationTime              FlexibleTimestamp `json:"creationTime"`
	WaitingForOtherJobs       int               `json:"waitingForOtherJobs"`
	Queued                    int               `json:"queued"`
	Running                   int               `json:"running"`
	LongestQueuedTimeSeconds  int               `json:"longestQueuedTimeInSeconds"`
	LongestRunningTimeSeconds int               `json:"longestRunningTimeInSeconds"`
	DurationSeconds           int               `json:"durationInSeconds"`
	TotalCount                int               `json:"totalCount"`
	// CancelLink is opaque (Forward encodes the underlying job key); pass it to Cancel unchanged. It identifies
	// this row's job or, for a grouped row (TotalCount > 1), the whole group.
	CancelLink string `json:"cancelLink"`
}

// CompletedJobInfo is one row (or a group) of finished work.
//
// GET /api/jobs/completed (JobsController.getCompletedJobInfos).
type CompletedJobInfo struct {
	JobType                   string            `json:"jobType"`
	OrgName                   string            `json:"orgName"`
	NetworkID                 int64             `json:"networkId"`
	SnapshotID                *int64            `json:"snapshotId,omitempty"`
	CreationTime              FlexibleTimestamp `json:"creationTime"`
	State                     string            `json:"state"` // SUCCEEDED, FAILED, CANCELED or TIMEDOUT
	EarliestStartTime         FlexibleTimestamp `json:"earliestStartTime"`
	LatestEndTime             FlexibleTimestamp `json:"latestEndTime"`
	LongestQueueTimeSeconds   int               `json:"longestQueueTimeInSeconds"`
	LongestRunningTimeSeconds int               `json:"longestRunningTimeInSeconds"`
	DurationSeconds           int               `json:"durationInSeconds"`
	Count                     int               `json:"count"`
}
