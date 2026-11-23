package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// NQEService runs Network Query Engine queries and diffs.
type NQE service

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

// Run executes an NQE query synchronously against a network or snapshot.
//
// With no snapshotID, Forward picks the newest processed snapshot that is not
// a draft or a Predict fork. An explicit snapshotID is used as given, and on a
// predicted snapshot queries over live state (counters, performance) come
// back empty whatever the network looks like. The result carries SnapshotID;
// a caller that needs to tell the two apart checks Snapshot.Predicted() on
// that snapshot. A snapshot that is still processing fails with
// ErrSnapshotNotProcessed, one whose processing failed with
// ErrSnapshotProcessingFailed.
func (s *NQE) Run(
	ctx context.Context,
	networkID string,
	snapshotID string,
	query ports.NQEQueryRequest,
) (*ports.NQEResult, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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

	result := new(ports.NQEResult)
	resp, err := s.client.Do(req, result)
	if err != nil {
		return nil, resp, err
	}
	return result, resp, nil
}

// Start submits an asynchronous NQE execution.
func (s *NQE) Start(
	ctx context.Context,
	networkID string,
	snapshotID string,
	query ports.NQEExecutionRequest,
) (*ports.NQEExecution, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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

	execution := new(ports.NQEExecution)
	resp, err := s.client.Do(req, execution)
	if err != nil {
		return nil, resp, err
	}
	return execution, resp, nil
}

// Status returns the current state of an asynchronous NQE execution.
func (s *NQE) Status(ctx context.Context, networkID, executionKey string) (*ports.NQEExecution, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	execution := new(ports.NQEExecution)
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
func (s *NQE) Result(
	ctx context.Context,
	networkID string,
	executionKey string,
	options ports.NQEResultOptions,
) (*ports.NQEResult, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	result := new(ports.NQEResult)
	resp, err := s.client.Do(req, result)
	if err != nil {
		return nil, resp, err
	}
	return result, resp, nil
}

// ResultJSONLines streams a completed asynchronous result as newline-delimited
// JSON without loading the full result into memory.
func (s *NQE) ResultJSONLines(
	ctx context.Context,
	networkID string,
	executionKey string,
	options ports.NQEResultOptions,
	dst io.Writer,
) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
func (s *NQE) Diff(
	ctx context.Context,
	beforeSnapshotID string,
	afterSnapshotID string,
	query ports.NQEDiffRequest,
) (*ports.NQEDiffResult, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	result := new(ports.NQEDiffResult)
	resp, err := s.client.Do(req, result)
	if err != nil {
		return nil, resp, err
	}
	return result, resp, nil
}

// ListQueries lists committed NQE library queries, optionally under dir.
func (s *NQE) ListQueries(ctx context.Context, dir string) ([]ports.NQEQuery, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path := "/api/nqe/queries"
	if dir = strings.TrimSpace(dir); dir != "" {
		path += "?" + url.Values{"dir": []string{dir}}.Encode()
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	var queries []ports.NQEQuery
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

func nqeResultPath(networkID, executionKey string, options ports.NQEResultOptions) (string, error) {
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

// Schema returns the NQE data model as this org sees it, as raw InlinedType
// JSON. It is filtered by the org's feature settings and license tier
// (NqeAppService.getSchemaTree), and includes network.extensions.<nqeName>
// for each of the org's data files. GET /api/nqe/schema (NqeController;
// VIEW_NQE_LIBRARY). Preview: not in the published spec.
func (s *NQE) Schema(ctx context.Context) (json.RawMessage, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return s.schema(ctx, "/api/nqe/schema", "NQE.Schema")
}

// SchemaAt is Schema extended with one snapshot's context: the structure
// Forward inferred from that snapshot's endpoint and data connector responses
// (ExternalSourceSchemaLoader). GET /api/nqe/schema?snapshotId= (needs the
// snapshot created; ErrSnapshotNotProcessed and friends apply). Pass
// Snapshots.LatestProcessed's ID for a network's current view.
func (s *NQE) SchemaAt(ctx context.Context, snapshotID string) (json.RawMessage, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID == "" {
		return nil, nil, errors.New("forward: snapshot ID is required")
	}
	return s.schema(ctx, "/api/nqe/schema?"+url.Values{"snapshotId": []string{snapshotID}}.Encode(), "NQE.SchemaAt")
}

func (s *NQE) schema(ctx context.Context, path, operation string) (json.RawMessage, *ports.Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, operation)
	var out json.RawMessage
	resp, err := s.client.doRequired(req, &out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}
