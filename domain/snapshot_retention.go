package domain

// SnapshotRetentionGranularity is how many of a time period's snapshots
// Forward keeps when it thins a network's snapshots: ALL keeps every one,
// ONE_PER_DAY (TWO_DAYS, WEEK, TWO_WEEKS, MONTH, QUARTER) keeps one per
// interval, and NONE deletes the lot. They run from fine to coarse, and a
// period may not be finer than the one before it.
type SnapshotRetentionGranularity string

const (
	SnapshotRetainAll        SnapshotRetentionGranularity = "ALL"
	SnapshotRetainPerDay     SnapshotRetentionGranularity = "ONE_PER_DAY"
	SnapshotRetainPerTwoDays SnapshotRetentionGranularity = "ONE_PER_TWO_DAYS"
	SnapshotRetainPerWeek    SnapshotRetentionGranularity = "ONE_PER_WEEK"
	SnapshotRetainPerTwoWeek SnapshotRetentionGranularity = "ONE_PER_TWO_WEEKS"
	SnapshotRetainPerMonth   SnapshotRetentionGranularity = "ONE_PER_MONTH"
	SnapshotRetainPerQuarter SnapshotRetentionGranularity = "ONE_PER_QUARTER"
	SnapshotRetainNone       SnapshotRetentionGranularity = "NONE"
)

func (g SnapshotRetentionGranularity) Valid() bool {
	switch g {
	case SnapshotRetainAll, SnapshotRetainPerDay, SnapshotRetainPerTwoDays, SnapshotRetainPerWeek,
		SnapshotRetainPerTwoWeek, SnapshotRetainPerMonth, SnapshotRetainPerQuarter, SnapshotRetainNone:
		return true
	}
	return false
}

// SnapshotRetentionPolicy is a network's snapshot thinning policy
// (SnapshotRetentionPolicy). Each period applies to snapshots of that age:
// LastWeek is 0-7 days, LastMonth 7-30, LastQuarter 30-90, LastYear 90-365 and
// Older over a year. With Enabled false nothing is ever thinned.
//
// Forward's rules, which it enforces with a 400: LastWeek must be ALL; each
// period may not be finer than the one before it; LastMonth may be ALL,
// ONE_PER_DAY, ONE_PER_TWO_DAYS or ONE_PER_WEEK; LastQuarter ALL, ONE_PER_DAY,
// ONE_PER_WEEK, ONE_PER_TWO_WEEKS or NONE; LastYear ONE_PER_DAY, ONE_PER_WEEK,
// ONE_PER_MONTH or NONE; Older those plus ONE_PER_QUARTER.
//
// The on-prem default, which applies until one is saved: enabled, ALL for a
// week, ONE_PER_DAY for a month, ONE_PER_WEEK for a quarter, ONE_PER_MONTH for
// a year, ONE_PER_QUARTER after. SaaS has one fixed policy (ONE_PER_WEEK from a
// quarter on) that cannot be changed.
type SnapshotRetentionPolicy struct {
	Enabled     bool                         `json:"enabled"`
	LastWeek    SnapshotRetentionGranularity `json:"lastWeek"`
	LastMonth   SnapshotRetentionGranularity `json:"lastMonth"`
	LastQuarter SnapshotRetentionGranularity `json:"lastQuarter"`
	LastYear    SnapshotRetentionGranularity `json:"lastYear"`
	Older       SnapshotRetentionGranularity `json:"older"`
}

// SnapshotRetentionPreview is what the next thinning pass would delete.
// Snapshots are listed as Forward's snapshot metadata (id, createdAt, note,
// and so on); Count equals len(Snapshots).
type SnapshotRetentionPreview struct {
	Count     int        `json:"count"`
	Snapshots []Snapshot `json:"deletedSnapshots"`
}
