package forward

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// CheckResult reads the rows an NQE check's result holds, page by page, from the result key the check reports (Check.NQEResultKey, from
// Checks.List or Checks.Get): POST /api/nqe/queryResults?snapshotId=&resultKey= (NqeController.getNqeQueryResults, USE_NQE, with the
// check's snapshot; the key is only valid for the snapshot it was computed on, and Forward answers a 400 otherwise). This is the
// full list of a failing NQE check's violating rows, where CheckDiagnosis.Details is only a sample. options pages and sorts like a
// query run (Offset, Limit, SortBy, ColumnFilters); nil reads Forward's default first page, so compare NQEResult.TotalNumItems with
// the rows read (NQEResult.Truncated) and page on. Preview: not in the published OpenAPI; the shape is from Forward's source and has not
// been seen live. Only NQE checks carry a result key; the other check types have no equivalent route.
func (s *NQEService) CheckResult(ctx context.Context, snapshotID, resultKey string, options *NQEOptions) (*NQEResult, *Response, error) {
	snapshotID, resultKey = strings.TrimSpace(snapshotID), strings.TrimSpace(resultKey)
	if snapshotID == "" || resultKey == "" {
		return nil, nil, errors.New("forward: a snapshot ID and a check result key are required")
	}
	path := "/api/nqe/queryResults?" + url.Values{"snapshotId": []string{snapshotID}, "resultKey": []string{resultKey}}.Encode()
	var (
		req *http.Request
		err error
	)
	if options == nil {
		req, err = s.client.NewRequest(ctx, http.MethodPost, path, nil)
	} else {
		req, err = s.client.newJSONRequest(ctx, http.MethodPost, path, options)
	}
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "NQE.CheckResult")
	result := new(NQEResult)
	resp, err := s.client.Do(req, result)
	if err != nil {
		return nil, resp, err
	}
	return result, resp, nil
}
