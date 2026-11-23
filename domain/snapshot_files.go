package domain

import (
	"strconv"
)

// FileLines is an inclusive, 0-based range of lines of a stored file (Forward's LineRange).
type FileLines struct{ Start, End int }

func (r FileLines) String() string { return strconv.Itoa(r.Start) + "-" + strconv.Itoa(r.End) }

// PredictedFileInfo says what a prediction changed in a file (Forward's PredictedFileInfo): how many lines differ from the base snapshot,
// and the change set, base snapshot and commit it came from.
type PredictedFileInfo struct {
	NumModifiedLines int        `json:"numModifiedLines"`
	ChangeSetID      Identifier `json:"changeSetId"`
	BaseSnapshotID   Identifier `json:"baseSnapshotId"`
	CommitID         Identifier `json:"commitId"`
}

// FileExcerpt is a run of lines of a file (Forward's Excerpt): Start and End are the 0-based inclusive line numbers, Lines the text.
type FileExcerpt struct {
	Start int      `json:"start"`
	End   int      `json:"end"`
	Lines []string `json:"lines"`
}

// FileExcerpts is a read of some lines of a stored file (Forward's FileExcerpts). Excerpts come back sorted by Start, not in the order
// requested. PredictedFileInfo is set only for a file of a PREDICTED snapshot that is in that snapshot or its base, and only when the org
// property PREDICT_MODELING is on.
type FileExcerpts struct {
	LineCount         int                `json:"lineCount"`
	Excerpts          []FileExcerpt      `json:"excerpts"`
	PredictedFileInfo *PredictedFileInfo `json:"predictedFileInfo,omitempty"`
}
