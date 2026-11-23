package domain

// DiffCount is Forward's DiffCount: how many entries differ and whether the
// computation finished (an incomplete count is a lower bound).
type DiffCount struct {
	Count    int64 `json:"count"`
	Complete bool  `json:"complete"`
}

// FilesDiffCount is the files variant: device count plus a per-file-type
// breakdown (e.g. CONFIG vs collected show-command output). State files change
// on every collection (timers, counters), so callers that want "material"
// change read Types and ignore the state kinds.
type FilesDiffCount struct {
	Count    int64            `json:"count"`
	Complete bool             `json:"complete"`
	Types    map[string]int64 `json:"types"`
}

// DiffEntry is one added/deleted/modified object in a full diff listing. Before
// is the older snapshot's object (the appserver's "a", nil when Type is ADDED)
// and After the newer one's ("b", nil when DELETED). The wire keys are
// diffType, a and b (cv.diff.DiffEntry); this type read type, before and after
// until v0.4.13, which no response carries, so every entry decoded empty.
type DiffEntry[T any] struct {
	Type   string `json:"diffType"`
	Before *T     `json:"a,omitempty"`
	After  *T     `json:"b,omitempty"`
}

// InventoryDeviceInfo is the device record a devices diff carries. Fields are
// the subset every Forward version has; unknown fields are ignored on decode.
type InventoryDeviceInfo struct {
	Name     string `json:"name"`
	Vendor   string `json:"vendor,omitempty"`
	OS       string `json:"os,omitempty"`
	Model    string `json:"model,omitempty"`
	Type     string `json:"type,omitempty"`
	Hostname string `json:"hostname,omitempty"`
}

// DiffKind names one countable diff family under /api/diffs.
type DiffKind string

const (
	DiffDevices      DiffKind = "devices"
	DiffInterfaces   DiffKind = "interfaces"
	DiffTopology     DiffKind = "topology"
	DiffL2           DiffKind = "l2"
	DiffACL          DiffKind = "acl"
	DiffCloudObjects DiffKind = "cloud-objects"
	DiffCloudACL     DiffKind = "cloud-acl"
	DiffRoutingLoop  DiffKind = "routing-loop/count"
)

// CountableDiffKinds is every family Count supports, in the order a caller
// would report them.
var CountableDiffKinds = []DiffKind{DiffDevices, DiffInterfaces, DiffTopology, DiffL2, DiffACL, DiffCloudObjects, DiffCloudACL, DiffRoutingLoop}

// MaterialDiffSummary is the answer to "is this snapshot worth looking at
// versus that one": per-family counts with state-file churn excluded.
type MaterialDiffSummary struct {
	SnapshotA  string            `json:"snapshotA"`
	SnapshotB  string            `json:"snapshotB"`
	Counts     map[string]int64  `json:"counts"`          // family -> count (families with 0 omitted)
	Files      map[string]int64  `json:"files,omitempty"` // file type -> devices changed (config kinds only)
	Checks     map[string]int64  `json:"checks,omitempty"`
	Incomplete []string          `json:"incomplete,omitempty"` // families whose count was a lower bound
	Errors     map[string]string `json:"errors,omitempty"`     // families that could not be computed
}

// Material reports whether anything in the summary is a real change.
func (m MaterialDiffSummary) Material() bool {
	for _, n := range m.Counts {
		if n > 0 {
			return true
		}
	}
	for _, n := range m.Files {
		if n > 0 {
			return true
		}
	}
	for _, n := range m.Checks {
		if n > 0 {
			return true
		}
	}
	return false
}

// CheckDiffEntry is one check compared between two snapshots: diffType is
// ADDED, DELETED, MODIFIED, UNCHANGED, or PROCESSING (NQE-type diffs evaluate
// asynchronously and report PROCESSING until Forward finishes). A is the check
// on the older snapshot, B on the newer; either is nil when absent there.
type CheckDiffEntry struct {
	DiffType string `json:"diffType"`
	A        *Check `json:"a,omitempty"`
	B        *Check `json:"b,omitempty"`
}
