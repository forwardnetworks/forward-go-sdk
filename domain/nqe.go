package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

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

// Truncated reports that the page holds fewer rows than Forward says the query matched (TotalNumItems), so a caller that reads one page
// must not treat it as the whole answer. False when Forward sent no total (a bare array), which says nothing either way.
func (r *NQEResult) Truncated() bool { return r != nil && r.TotalNumItems > int64(len(r.Items)) }

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

// RowsNumber is RowsAny with every JSON number kept exact: a number decodes to
// a json.Number (at any depth, in nested objects and lists) instead of a
// float64, which loses precision above 2^53 (byte counters, 64-bit IDs).
// Convert with Int64(), Float64() or String(). RowsAny stays as it was for
// callers that rely on float64.
func (r *NQEResult) RowsNumber() ([]map[string]any, error) {
	if r == nil {
		return nil, nil
	}
	rows := make([]map[string]any, len(r.Items))
	for rowIndex, record := range r.Items {
		row := make(map[string]any, len(record))
		for column, raw := range record {
			value, err := decodeExactJSON(raw)
			if err != nil {
				return nil, fmt.Errorf("forward: decode NQE row %d column %q: %w", rowIndex, column, err)
			}
			row[column] = value
		}
		rows[rowIndex] = row
	}
	return rows, nil
}

// Int64 reads the column as an integer without going through float64. It
// reports false when the column is absent or null, and an error when the value
// is not a JSON integer that fits an int64 (a string, a fraction, 1e3, a number
// beyond the int64 range). A nil *NQERecord (NQEDiff.Before or After when a row
// exists on one side only) must be checked by the caller before calling.
func (r NQERecord) Int64(column string) (int64, bool, error) {
	raw, ok := r[column]
	if !ok {
		return 0, false, nil
	}
	value, err := decodeExactJSON(raw)
	if err != nil {
		return 0, false, fmt.Errorf("forward: decode NQE column %q: %w", column, err)
	}
	switch v := value.(type) {
	case nil:
		return 0, false, nil
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0, false, fmt.Errorf("forward: NQE column %q = %s is not an int64: %w", column, v, err)
		}
		return n, true, nil
	default:
		return 0, false, fmt.Errorf("forward: NQE column %q is %T, not a number", column, value)
	}
}

func decodeExactJSON(raw json.RawMessage) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	// Decode stops after the first value; json.Unmarshal (RowsAny) rejects
	// anything after it, so this does too.
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errors.New("unexpected data after the JSON value")
	}
	return value, nil
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
