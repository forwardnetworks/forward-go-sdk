package forward

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// xlsxAccept is the content type Forward's check reports are produced as.
const xlsxAccept = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// ChecksReportOptions bounds a report download. Forward builds the whole
// workbook before it sends the first byte, so a big snapshot takes a while.
// Timeout works as on SnapshotExportOptions: zero keeps the client's own (60
// seconds by default), positive replaces it, negative removes it.
type ChecksReportOptions struct {
	Timeout time.Duration
}

// ChecksReport streams the snapshot's checks as an .xlsx workbook to dst and
// returns the bytes written. GET /api/snapshots/{snapshotId}/checksReport
// (CheckController.getChecksReportXlsx; VIEW_CHECKS; the snapshot must be past
// the CREATION stage). Unpublished: not in Forward's OpenAPI set. Nothing is
// retried; write to a temporary file. A workbook is a ZIP, so a body that does
// not start like one is an error.
func (s *ChecksService) ChecksReport(ctx context.Context, snapshotID string, options ChecksReportOptions, dst io.Writer) (int64, *Response, error) {
	snapshotID = strings.TrimSpace(snapshotID)
	if snapshotID == "" {
		return 0, nil, errors.New("forward: snapshot ID is required")
	}
	return s.report(ctx, "/api/snapshots/"+url.PathEscape(snapshotID)+"/checksReport", "Checks.ChecksReport", options, dst)
}

// CheckCategoryReport is ChecksReport for one category of check: NQE,
// PREDEFINED, INTENT or ADVANCED. Directory, INTENT only, limits it to one
// directory of intent checks. GET /api/snapshots/{snapshotId}/checks-report
// ?type=&dir= (VIEW_CHECKS; NQE also needs the USE_NQE permission). Unpublished.
func (s *ChecksService) CheckCategoryReport(ctx context.Context, snapshotID, category, directory string, options ChecksReportOptions, dst io.Writer) (int64, *Response, error) {
	snapshotID = strings.TrimSpace(snapshotID)
	if snapshotID == "" {
		return 0, nil, errors.New("forward: snapshot ID is required")
	}
	category = strings.ToUpper(strings.TrimSpace(category))
	switch category {
	case "NQE", "PREDEFINED", "INTENT", "ADVANCED":
	default:
		return 0, nil, fmt.Errorf("forward: check category %q must be NQE, PREDEFINED, INTENT or ADVANCED", category)
	}
	query := url.Values{"type": []string{category}}
	if directory = strings.TrimSpace(directory); directory != "" {
		if category != "INTENT" {
			return 0, nil, errors.New("forward: a directory applies only to the INTENT category")
		}
		query.Set("dir", directory)
	}
	return s.report(ctx, "/api/snapshots/"+url.PathEscape(snapshotID)+"/checks-report?"+query.Encode(), "Checks.CheckCategoryReport", options, dst)
}

func (s *ChecksService) report(ctx context.Context, path, operation string, options ChecksReportOptions, dst io.Writer) (int64, *Response, error) {
	if dst == nil {
		return 0, nil, errors.New("forward: a destination writer is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", xlsxAccept+", application/json")
	req = markOperation(req, operation)
	sink := &zipSink{dst: dst}
	resp, err := s.client.withCallTimeout(options.Timeout).Do(req, sink)
	if err != nil {
		return sink.written, resp, err
	}
	if !sink.isZip() {
		return sink.written, resp, fmt.Errorf("forward: checks report did not return a workbook (%d bytes, starting %q)", sink.written, sink.head())
	}
	return sink.written, resp, nil
}
