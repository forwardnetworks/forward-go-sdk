package forward

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// maxExplainBytes bounds an optimized-plan rendering held in memory.
const maxExplainBytes = 8 << 20

// boundedBuffer holds its bytes in a named field, not an embedded
// bytes.Buffer: an embedded one would also promote ReadFrom, which io.Copy
// prefers over Write, and the limit would never be checked.
type boundedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.buf.Len()+len(p) > b.limit {
		return 0, fmt.Errorf("forward: NQE explain output exceeds %d bytes", b.limit)
	}
	return b.buf.Write(p)
}

// ExplainQuery returns the optimized plan Forward's Query Debug Panel shows for
// a SAVED library query: a text rendering of the optimized query (it may not
// itself be valid NQE) with the parallelism, totality and cardinality marks the
// optimizer chose. POST /api/internal/nqe/queries/{queryId}/explain
// (NqeController.explainCommittedQuery, Preview and internal: unpublished;
// org VIEW_NQE_LIBRARY). commitID empty explains HEAD. snapshotID empty
// validates without a snapshot, so the schema is not extended with that
// snapshot's data. params binds a parameterized query's parameters (nil for
// none; a nil value inside it is a 400). A query with diagnostics (it does
// not compile) is a 400 with the diagnostics, never a plan. Only a query
// saved in the library can be explained by id: Forward has no optimized-plan
// explain for raw query text (explainSchemaAccess takes raw text but returns
// the schema paths a query reads, not a plan).
func (s *NQEService) ExplainQuery(ctx context.Context, queryID, commitID, snapshotID string, params map[string]any) (string, *Response, error) {
	if queryID = strings.TrimSpace(queryID); queryID == "" {
		return "", nil, errors.New("forward: NQE query id is required")
	}
	query := url.Values{}
	if commitID != "" {
		query.Set("commitId", commitID)
	}
	if snapshotID != "" {
		query.Set("snapshotId", snapshotID)
	}
	return s.explain(ctx, "/api/internal/nqe/queries/"+url.PathEscape(queryID)+"/explain", query, params, "NQE.ExplainQuery")
}

// ExplainPath is ExplainQuery for the query at path in the caller's own NQE
// workspace: the draft when one exists, else the committed query. path is the
// library path without the repository, as the UI sends it (for example
// "/Operations/my query"). POST /api/internal/users/current/nqe/queries/explain
// (NqeController.explainInUserWorkspace, Preview and internal; org
// VIEW_NQE_LIBRARY). Whether a draft at a path that has no committed query
// yet resolves here was not traced.
func (s *NQEService) ExplainPath(ctx context.Context, path, snapshotID string, params map[string]any) (string, *Response, error) {
	if strings.TrimSpace(path) == "" {
		return "", nil, errors.New("forward: NQE query path is required")
	}
	query := url.Values{"path": []string{path}}
	if snapshotID != "" {
		query.Set("snapshotId", snapshotID)
	}
	return s.explain(ctx, "/api/internal/users/current/nqe/queries/explain", query, params, "NQE.ExplainPath")
}

func (s *NQEService) explain(ctx context.Context, path string, query url.Values, params map[string]any, operation string) (string, *Response, error) {
	if params == nil {
		params = map[string]any{}
	}
	if len(query) != 0 {
		path += "?" + query.Encode()
	}
	// The body is the bare parameter map, as the UI sends it. A request is not
	// retried or replayed: the plan can be large and is built per call.
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, params)
	if err != nil {
		return "", nil, err
	}
	// application/json too: Forward answers a diagnostic or error as JSON, and
	// without it Spring can only say 406.
	req.Header.Set("Accept", "text/plain, application/json")
	req = markOperation(req, operation)
	out := &boundedBuffer{limit: maxExplainBytes}
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return "", resp, err
	}
	return out.buf.String(), resp, nil
}

// NQERunMetricsOptions filters RunMetrics. The zero value asks for Forward's
// defaults: the last 7 days, runs of at least 10 seconds, at most 100 of them.
type NQERunMetricsOptions struct {
	From, To time.Time
	// MinExecutionSeconds and Limit are pointers so that an explicit value
	// differs from "use Forward's default" (10 seconds, 100 runs). A
	// MinExecutionSeconds of 0 means every run; a Limit of 0 returns nothing,
	// and a negative Limit is refused.
	MinExecutionSeconds *int
	Limit               *int
}

// NQERunMetric is one slow NQE run Forward recorded. QueryID and CommitID are
// set only for a saved library query (empty for a run of ad-hoc text).
// TrackingID ties the run to the request that started it.
type NQERunMetric struct {
	TaskID                  string
	NetworkID               string
	NetworkName             string
	SnapshotID              string
	SnapshotCreationInstant time.Time
	ExecutedAt              time.Time
	RunDuration             time.Duration
	// SourceSetID is Forward's identifier for the query's source set, a string
	// such as "NqeSourceSetId_<hex>" (the key GET /api/nqe-sources-json/{id} takes).
	SourceSetID string
	QueryID     string
	CommitID    string
	TrackingID  string
}

// UnmarshalJSON reads Forward's shape: executedAt and snapshotCreationInstant
// are epoch milliseconds, runDuration is milliseconds.
func (m *NQERunMetric) UnmarshalJSON(data []byte) error {
	var wire struct {
		TaskID                  json.RawMessage `json:"taskId"`
		NetworkID               json.RawMessage `json:"networkId"`
		NetworkName             string          `json:"networkName"`
		SnapshotID              json.RawMessage `json:"snapshotId"`
		SnapshotCreationInstant json.RawMessage `json:"snapshotCreationInstant"`
		ExecutedAt              json.RawMessage `json:"executedAt"`
		RunDuration             json.RawMessage `json:"runDuration"`
		SourceSetID             json.RawMessage `json:"sourceSetId"`
		QueryID                 string          `json:"queryId"`
		CommitID                string          `json:"commitId"`
		TrackingID              string          `json:"trackingId"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	var err error
	*m = NQERunMetric{
		TaskID: scalarText(wire.TaskID), NetworkID: scalarText(wire.NetworkID), NetworkName: wire.NetworkName,
		SnapshotID: scalarText(wire.SnapshotID), SourceSetID: scalarText(wire.SourceSetID), QueryID: wire.QueryID,
		CommitID: wire.CommitID, TrackingID: wire.TrackingID,
	}
	if m.ExecutedAt, err = instantFromJSON(wire.ExecutedAt); err != nil {
		return fmt.Errorf("executedAt: %w", err)
	}
	if m.SnapshotCreationInstant, err = instantFromJSON(wire.SnapshotCreationInstant); err != nil {
		return fmt.Errorf("snapshotCreationInstant: %w", err)
	}
	if text := strings.TrimSpace(string(wire.RunDuration)); text != "" && text != "null" {
		millis, perr := strconv.ParseFloat(text, 64)
		if perr != nil {
			return fmt.Errorf("runDuration: want milliseconds, got %s", text)
		}
		m.RunDuration = time.Duration(millis * float64(time.Millisecond))
	}
	return nil
}

// scalarText reads a JSON string or number as text, and "" for null or absent.
func scalarText(raw json.RawMessage) string {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return text
}

// instantFromJSON reads epoch milliseconds (Forward's @JsonFormat NUMBER) or an
// ISO-8601 string, and the zero time for null or absent.
func instantFromJSON(raw json.RawMessage) (time.Time, error) {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return time.Time{}, nil
	}
	if text[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return time.Time{}, err
		}
		return time.Parse(time.RFC3339Nano, s)
	}
	millis, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("want epoch milliseconds, got %s", text)
	}
	return time.UnixMilli(int64(millis)).UTC(), nil
}

// RunMetrics lists the org's slow NQE runs, for finding which queries cost the
// most. GET /api/nqe-run-metrics (NqeController.getNqeQueryRunMetrics, Preview;
// org DEBUG_NQE, which most users lack: that is a typed 403,
// errors.Is(err, ErrPermissionDenied) and MissingPermission).
func (s *NQEService) RunMetrics(ctx context.Context, options NQERunMetricsOptions) ([]NQERunMetric, *Response, error) {
	if options.Limit != nil && *options.Limit < 0 {
		return nil, nil, errors.New("forward: Limit must not be negative")
	}
	query := url.Values{}
	if !options.From.IsZero() {
		query.Set("from", options.From.UTC().Format(time.RFC3339))
	}
	if !options.To.IsZero() {
		query.Set("to", options.To.UTC().Format(time.RFC3339))
	}
	if options.MinExecutionSeconds != nil {
		query.Set("minExecutionSeconds", strconv.Itoa(*options.MinExecutionSeconds))
	}
	if options.Limit != nil {
		query.Set("limit", strconv.Itoa(*options.Limit))
	}
	path := "/api/nqe-run-metrics"
	if len(query) != 0 {
		path += "?" + query.Encode()
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "NQE.RunMetrics")
	var out []NQERunMetric
	resp, err := s.client.doRequired(req, &out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}
