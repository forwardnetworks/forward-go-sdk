package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// NQEService runs Network Query Engine queries and diffs.
type NQEService service

const (
	NQEItemFormatJSON   = "JSON"
	NQEItemFormatLegacy = "LEGACY"

	NQESortAscending  = "ASC"
	NQESortDescending = "DESC"

	NQEFilterDefault   = "DEFAULT"
	NQEFilterIsBetween = "IS_BETWEEN"
)

// NQEQueryRequest describes a synchronous NQE execution. Exactly one of Query
// and QueryID must be set.
type NQEQueryRequest struct {
	Query              string         `json:"query,omitempty"`
	QueryID            string         `json:"queryId,omitempty"`
	CommitID           string         `json:"commitId,omitempty"`
	Options            *NQEOptions    `json:"queryOptions,omitempty"`
	Parameters         map[string]any `json:"parameters,omitempty"`
	UseLatestDataFiles *bool          `json:"useLatestDataFiles,omitempty"`
}

// NQEOptions controls paging, sorting, filtering, and result rendering.
type NQEOptions struct {
	Offset        *int32            `json:"offset,omitempty"`
	Limit         *int32            `json:"limit,omitempty"`
	SortBy        *NQESortOrder     `json:"sortBy,omitempty"`
	ColumnFilters []NQEColumnFilter `json:"columnFilters,omitempty"`
	ItemFormat    string            `json:"itemFormat,omitempty"`
}

// NQESortOrder describes one NQE result sort.
type NQESortOrder struct {
	ColumnName string `json:"columnName"`
	Order      string `json:"order,omitempty"`
}

// NQEColumnFilter describes either a DEFAULT or IS_BETWEEN filter.
type NQEColumnFilter struct {
	ColumnName string `json:"columnName"`
	Operator   string `json:"operator"`
	Value      string `json:"value,omitempty"`
	LowerBound string `json:"lowerBound,omitempty"`
	UpperBound string `json:"upperBound,omitempty"`
}

// NQERecord is one NQE result row. Raw JSON values preserve the API's dynamic
// column types without converting all numbers to float64.
type NQERecord map[string]json.RawMessage

// NQEResult is a page of synchronous or asynchronous NQE results.
type NQEResult struct {
	SnapshotID    Identifier  `json:"snapshotId"`
	Items         []NQERecord `json:"items"`
	TotalNumItems int64       `json:"totalNumItems"`
}

// UnmarshalJSON accepts the bare-array and items/rows/results/data envelopes
// used by different appserver builds.
func (r *NQEResult) UnmarshalJSON(data []byte) error {
	if r == nil {
		return errors.New("forward: unmarshal NQE result into nil receiver")
	}
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "[") {
		return json.Unmarshal(data, &r.Items)
	}
	type plain NQEResult
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*r = NQEResult(value)
	if r.Items != nil {
		return nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	for _, key := range []string{"rows", "results", "data"} {
		if raw, ok := object[key]; ok {
			return json.Unmarshal(raw, &r.Items)
		}
	}
	return nil
}

// RowsAny converts dynamic NQE cells to the same map[string]any shape used by
// older consumers. Wire decoding remains centralized in the SDK.
func (r *NQEResult) RowsAny() ([]map[string]any, error) {
	if r == nil {
		return nil, nil
	}
	rows := make([]map[string]any, len(r.Items))
	for rowIndex, record := range r.Items {
		row := make(map[string]any, len(record))
		for column, raw := range record {
			var value any
			if err := json.Unmarshal(raw, &value); err != nil {
				return nil, fmt.Errorf("forward: decode NQE row %d column %q: %w", rowIndex, column, err)
			}
			row[column] = value
		}
		rows[rowIndex] = row
	}
	return rows, nil
}

// NQEExecutionRequest describes an asynchronous NQE execution. Exactly one of
// Query and QueryID must be set.
type NQEExecutionRequest struct {
	Query              string            `json:"query,omitempty"`
	QueryID            string            `json:"queryId,omitempty"`
	CommitID           string            `json:"commitId,omitempty"`
	Parameters         map[string]any    `json:"parameters,omitempty"`
	ColumnFilters      []NQEColumnFilter `json:"columnFilters,omitempty"`
	SortKeys           []NQESortOrder    `json:"sortKeys,omitempty"`
	UseLatestDataFiles *bool             `json:"useLatestDataFiles,omitempty"`
}

// NQEExecution is the state of an asynchronous NQE query.
type NQEExecution struct {
	ExecutionKey    string         `json:"executionKey,omitempty"`
	Status          string         `json:"status"`
	Outcome         string         `json:"outcome,omitempty"`
	MillisExecuting *int64         `json:"millisExecuting,omitempty"`
	RowsProduced    *int64         `json:"rowsProduced,omitempty"`
	TimeoutMinutes  *int64         `json:"timeoutMinutes,omitempty"`
	Error           *NQEQueryError `json:"error,omitempty"`
}

// NQEQueryError is a diagnostic emitted for an invalid or failed NQE query.
type NQEQueryError struct {
	Location *NQETextRegion `json:"location,omitempty"`
	Message  string         `json:"message,omitempty"`
}

// NQETextRegion identifies a range in NQE source text.
type NQETextRegion struct {
	Start *NQETextPosition `json:"start,omitempty"`
	End   *NQETextPosition `json:"end,omitempty"`
}

// NQETextPosition is a zero-based line and character position.
type NQETextPosition struct {
	Line      int32 `json:"line"`
	Character int32 `json:"character"`
}

// NQEResultOptions controls asynchronous result paging.
type NQEResultOptions struct {
	Offset *int32
	Limit  *int32
}

// NQEDiffRequest identifies a committed query and its result options.
type NQEDiffRequest struct {
	QueryID  string      `json:"queryId"`
	CommitID string      `json:"commitId,omitempty"`
	Options  *NQEOptions `json:"options,omitempty"`
}

// NQEDiffResult contains differences between the same query on two snapshots.
type NQEDiffResult struct {
	Rows         []NQEDiffEntry `json:"rows"`
	TotalNumRows int32          `json:"totalNumRows"`
}

// NQEDiffEntry is an added, deleted, or modified NQE row.
type NQEDiffEntry struct {
	Type   string     `json:"type"`
	Before *NQERecord `json:"before,omitempty"`
	After  *NQERecord `json:"after,omitempty"`
}

// NQEQuery identifies a committed query in the Forward or organization library.
type NQEQuery struct {
	QueryID    string `json:"queryId"`
	Repository string `json:"repository"`
	Path       string `json:"path"`
	Intent     string `json:"intent"`
}

// Run executes an NQE query synchronously against a network or snapshot.
func (s *NQEService) Run(
	ctx context.Context,
	networkID string,
	snapshotID string,
	query NQEQueryRequest,
) (*NQEResult, *Response, error) {
	if err := validateNQEQuery(query.Query, query.QueryID); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(networkID) == "" {
		networkID = s.client.Network()
	}
	params, err := nqeScope(networkID, snapshotID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/nqe?"+params.Encode(), query)
	if err != nil {
		return nil, nil, err
	}

	result := new(NQEResult)
	resp, err := s.client.Do(req, result)
	if err != nil {
		return nil, resp, err
	}
	return result, resp, nil
}

// Start submits an asynchronous NQE execution.
func (s *NQEService) Start(
	ctx context.Context,
	networkID string,
	snapshotID string,
	query NQEExecutionRequest,
) (*NQEExecution, *Response, error) {
	if err := validateNQEQuery(query.Query, query.QueryID); err != nil {
		return nil, nil, err
	}
	var err error
	if networkID, err = s.client.resolveNetworkID(networkID); err != nil {
		return nil, nil, err
	}
	params := url.Values{}
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID != "" {
		params.Set("snapshotId", snapshotID)
	}
	path := fmt.Sprintf("/api/networks/%s/nqe-executions", url.PathEscape(networkID))
	if len(params) != 0 {
		path += "?" + params.Encode()
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, query)
	if err != nil {
		return nil, nil, err
	}

	execution := new(NQEExecution)
	resp, err := s.client.Do(req, execution)
	if err != nil {
		return nil, resp, err
	}
	return execution, resp, nil
}

// StartOperation submits an asynchronous NQE query and returns a stateful
// polling handle.
func (s *NQEService) StartOperation(
	ctx context.Context,
	networkID string,
	snapshotID string,
	query NQEExecutionRequest,
) (*Poller[NQEExecution], *Response, error) {
	execution, response, err := s.Start(ctx, networkID, snapshotID, query)
	if err != nil {
		return nil, response, err
	}
	poller, err := NewPoller(execution, func(ctx context.Context) (*NQEExecution, *Response, error) {
		return s.Status(ctx, networkID, execution.ExecutionKey)
	}, func(value *NQEExecution) (bool, error) {
		switch strings.ToUpper(value.Status) {
		case "COMPLETED":
			if value.Outcome == "" || strings.EqualFold(value.Outcome, "OK") || strings.EqualFold(value.Outcome, "SUCCEEDED") {
				return true, nil
			}
			return true, fmt.Errorf("forward: NQE execution %s completed with outcome %s", value.ExecutionKey, value.Outcome)
		case "FAILED", "CANCELED", "TIMED_OUT":
			return true, fmt.Errorf("forward: NQE execution %s ended with status %s", value.ExecutionKey, value.Status)
		default:
			return false, nil
		}
	})
	return poller, response, err
}

// Status returns the current state of an asynchronous NQE execution.
func (s *NQEService) Status(ctx context.Context, networkID, executionKey string) (*NQEExecution, *Response, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	path, err := nqeExecutionPath(networkID, executionKey)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	execution := new(NQEExecution)
	resp, err := s.client.Do(req, execution)
	if err != nil {
		return nil, resp, err
	}
	// The status response omits its key; retain the durable identifier supplied
	// by the caller so operation handles do not lose identity after a refresh.
	execution.ExecutionKey = executionKey
	return execution, resp, nil
}

// Result returns one JSON result page for a completed asynchronous execution.
func (s *NQEService) Result(
	ctx context.Context,
	networkID string,
	executionKey string,
	options NQEResultOptions,
) (*NQEResult, *Response, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	path, err := nqeResultPath(networkID, executionKey, options)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	result := new(NQEResult)
	resp, err := s.client.Do(req, result)
	if err != nil {
		return nil, resp, err
	}
	return result, resp, nil
}

// ResultJSONLines streams a completed asynchronous result as newline-delimited
// JSON without loading the full result into memory.
func (s *NQEService) ResultJSONLines(
	ctx context.Context,
	networkID string,
	executionKey string,
	options NQEResultOptions,
	dst io.Writer,
) (*Response, error) {
	if dst == nil {
		return nil, errors.New("forward: result writer is required")
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, err
	}
	path, err := nqeResultPath(networkID, executionKey, options)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/jsonl")
	return s.client.Do(req, dst)
}

// Diff compares a committed NQE query's results across two snapshots.
func (s *NQEService) Diff(
	ctx context.Context,
	beforeSnapshotID string,
	afterSnapshotID string,
	query NQEDiffRequest,
) (*NQEDiffResult, *Response, error) {
	if strings.TrimSpace(query.QueryID) == "" {
		return nil, nil, errors.New("forward: NQE query ID is required")
	}
	beforeSnapshotID = strings.TrimSpace(beforeSnapshotID)
	afterSnapshotID = strings.TrimSpace(afterSnapshotID)
	if beforeSnapshotID == "" || afterSnapshotID == "" {
		return nil, nil, errors.New("forward: before and after snapshot IDs are required")
	}
	path := fmt.Sprintf(
		"/api/nqe-diffs/%s/%s",
		url.PathEscape(beforeSnapshotID),
		url.PathEscape(afterSnapshotID),
	)
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, query)
	if err != nil {
		return nil, nil, err
	}
	result := new(NQEDiffResult)
	resp, err := s.client.Do(req, result)
	if err != nil {
		return nil, resp, err
	}
	return result, resp, nil
}

// ListQueries lists committed NQE library queries, optionally under dir.
func (s *NQEService) ListQueries(ctx context.Context, dir string) ([]NQEQuery, *Response, error) {
	path := "/api/nqe/queries"
	if dir = strings.TrimSpace(dir); dir != "" {
		path += "?" + url.Values{"dir": []string{dir}}.Encode()
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	var queries []NQEQuery
	resp, err := s.client.Do(req, &queries)
	if err != nil {
		return nil, resp, err
	}
	return queries, resp, nil
}

func validateNQEQuery(query, queryID string) error {
	hasQuery := strings.TrimSpace(query) != ""
	hasQueryID := strings.TrimSpace(queryID) != ""
	if hasQuery == hasQueryID {
		return errors.New("forward: exactly one of NQE query or query ID is required")
	}
	return nil
}

func nqeScope(networkID, snapshotID string) (url.Values, error) {
	params := url.Values{}
	if networkID = strings.TrimSpace(networkID); networkID != "" {
		params.Set("networkId", networkID)
	}
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID != "" {
		params.Set("snapshotId", snapshotID)
	}
	if len(params) == 0 {
		return nil, errors.New("forward: network ID or snapshot ID is required")
	}
	return params, nil
}

func nqeExecutionPath(networkID, executionKey string) (string, error) {
	networkID = strings.TrimSpace(networkID)
	executionKey = strings.TrimSpace(executionKey)
	if networkID == "" || executionKey == "" {
		return "", errors.New("forward: network ID and NQE execution key are required")
	}
	return fmt.Sprintf(
		"/api/networks/%s/nqe-executions/%s",
		url.PathEscape(networkID),
		url.PathEscape(executionKey),
	), nil
}

func nqeResultPath(networkID, executionKey string, options NQEResultOptions) (string, error) {
	path, err := nqeExecutionPath(networkID, executionKey)
	if err != nil {
		return "", err
	}
	params := url.Values{}
	if options.Offset != nil {
		params.Set("offset", strconv.FormatInt(int64(*options.Offset), 10))
	}
	if options.Limit != nil {
		params.Set("limit", strconv.FormatInt(int64(*options.Limit), 10))
	}
	path += "/result"
	if len(params) != 0 {
		path += "?" + params.Encode()
	}
	return path, nil
}
