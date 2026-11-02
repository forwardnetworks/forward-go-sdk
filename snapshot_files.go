package forward

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
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

// FileExcerpts reads line ranges of a device's stored file in a snapshot (GET /api/snapshots/{snapshotId}/files/{device},{file}?lines=a-b;
// VIEW_COLLECTED_FILES; the snapshot must have its stored files). At least one range is required. Forward names the file with the
// device in front, "<device>,<file>" (this method builds it; a global file with no device cannot be read this way), and on a predicted
// snapshot the answer also says how many lines the prediction changed (see FileExcerpts.PredictedFileInfo). Several ranges go in ONE
// comma-separated lines value ("7-45,60-80"), which is the form Forward's converter takes; Forward needs them sorted and refuses overlaps
// (400), so this method sorts them and refuses overlaps first. A range that starts past the end of the file is a 400; one that ends past
// it is cut short.
func (s *SnapshotsService) FileExcerpts(ctx context.Context, snapshotID, deviceName, fileName string, ranges []FileLines) (*FileExcerpts, *Response, error) {
	snapshotID, deviceName, fileName = strings.TrimSpace(snapshotID), strings.TrimSpace(deviceName), strings.TrimSpace(fileName)
	if snapshotID == "" || deviceName == "" || fileName == "" {
		return nil, nil, errors.New("forward: snapshot ID, device and file name are required")
	}
	if len(ranges) == 0 {
		return nil, nil, errors.New("forward: at least one line range is required")
	}
	sorted := append([]FileLines(nil), ranges...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start < sorted[j].Start })
	parts := make([]string, 0, len(sorted))
	for i, r := range sorted {
		if r.Start < 0 || r.End < r.Start {
			return nil, nil, errors.New("forward: a line range is 0-based with start at most end")
		}
		if i > 0 && r.Start <= sorted[i-1].End {
			return nil, nil, errors.New("forward: line ranges must not overlap")
		}
		parts = append(parts, r.String())
	}
	query := url.Values{"lines": []string{strings.Join(parts, ",")}}
	path := "/api/snapshots/" + url.PathEscape(snapshotID) + "/files/" + url.PathEscape(deviceName) + "," + url.PathEscape(fileName) + "?" + query.Encode()
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	out := new(FileExcerpts)
	resp, err := s.client.doRequired(markOperation(req, "Snapshots.FileExcerpts"), out)
	return out, resp, err
}
