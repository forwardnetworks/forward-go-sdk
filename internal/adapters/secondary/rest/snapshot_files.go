package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// FileExcerpts reads line ranges of a device's stored file in a snapshot (GET /api/snapshots/{snapshotId}/files/{device},{file}?lines=a-b;
// VIEW_COLLECTED_FILES; the snapshot must have its stored files). At least one range is required. Forward names the file with the
// device in front, "<device>,<file>" (this method builds it; a global file with no device cannot be read this way), and on a predicted
// snapshot the answer also says how many lines the prediction changed (see FileExcerpts.PredictedFileInfo). Several ranges go in ONE
// comma-separated lines value ("7-45,60-80"), which is the form Forward's converter takes; Forward needs them sorted and refuses overlaps
// (400), so this method sorts them and refuses overlaps first. A range that starts past the end of the file is a 400; one that ends past
// it is cut short.
func (s *Snapshots) FileExcerpts(ctx context.Context, snapshotID, deviceName, fileName string, ranges []ports.FileLines) (*ports.FileExcerpts, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	snapshotID, deviceName, fileName = strings.TrimSpace(snapshotID), strings.TrimSpace(deviceName), strings.TrimSpace(fileName)
	if snapshotID == "" || deviceName == "" || fileName == "" {
		return nil, nil, errors.New("forward: snapshot ID, device and file name are required")
	}
	if len(ranges) == 0 {
		return nil, nil, errors.New("forward: at least one line range is required")
	}
	sorted := append([]ports.FileLines(nil), ranges...)
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
	out := new(ports.FileExcerpts)
	resp, err := s.client.doRequired(markOperation(req, "Snapshots.FileExcerpts"), out)
	return out, resp, err
}
