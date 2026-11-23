package domain

const (
	CollectorTaskQueued    = "QUEUED"
	CollectorTaskRunning   = "RUNNING"
	CollectorTaskSucceeded = "SUCCEEDED"
	CollectorTaskFailed    = "FAILED"
	CollectorTaskTimedOut  = "TIMED_OUT"
	CollectorTaskCanceled  = "CANCELED"
)

// CollectorTask is the durable state of a collector operation.
type CollectorTask struct {
	ID           Identifier     `json:"id"`
	Type         string         `json:"type"`
	Status       string         `json:"status"`
	NetworkID    Identifier     `json:"networkId"`
	NetworkName  string         `json:"networkName,omitempty"`
	Note         string         `json:"note,omitempty"`
	CreatedByID  Identifier     `json:"createdById,omitempty"`
	CreatedBy    string         `json:"createdBy,omitempty"`
	CreatedAt    string         `json:"createdAt,omitempty"`
	StartedAt    string         `json:"startedAt,omitempty"`
	FinishedAt   string         `json:"finishedAt,omitempty"`
	CanceledByID Identifier     `json:"canceledById,omitempty"`
	CanceledBy   string         `json:"canceledBy,omitempty"`
	Progress     map[string]any `json:"progress,omitempty"`
}

// CollectionProgress summarizes active collector work for one network.
// InProgress is authoritative; counts are zero when a build omits progress.
type CollectionProgress struct {
	InProgress bool
	Total      int
	Finished   int
	Active     int
}

// CollectorTaskListOptions filters recent tasks.
type CollectorTaskListOptions struct {
	// NetworkID is a best-effort client-side filter because the published list
	// endpoint is organization-wide. The server applies Limit before this
	// filter, so busy organizations can have older matching tasks outside the
	// returned window.
	NetworkID string
	Statuses  []string
	Limit     *int32
}
