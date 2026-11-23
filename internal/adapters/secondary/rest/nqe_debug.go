package rest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
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
func (s *NQE) ExplainQuery(ctx context.Context, queryID, commitID, snapshotID string, params map[string]any) (string, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return "", nil, err
	}
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
// VIEW_NQE_LIBRARY). A draft at a path with no committed query yet resolves
// (getUserEffectiveSourceSet reads the workspace change first and needs a
// committed query only for an unedited path); a path the workspace has deleted,
// moved away from, or that exists nowhere, is a 404.
func (s *NQE) ExplainPath(ctx context.Context, path, snapshotID string, params map[string]any) (string, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(path) == "" {
		return "", nil, errors.New("forward: NQE query path is required")
	}
	query := url.Values{"path": []string{path}}
	if snapshotID != "" {
		query.Set("snapshotId", snapshotID)
	}
	return s.explain(ctx, "/api/internal/users/current/nqe/queries/explain", query, params, "NQE.ExplainPath")
}

func (s *NQE) explain(ctx context.Context, path string, query url.Values, params map[string]any, operation string) (string, *ports.Response, error) {
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

// RunMetrics lists the org's slow NQE runs, for finding which queries cost the
// most. GET /api/nqe-run-metrics (NqeController.getNqeQueryRunMetrics, Preview;
// org DEBUG_NQE, which most users lack: that is a typed 403,
// errors.Is(err, ErrPermissionDenied) and MissingPermission).
func (s *NQE) RunMetrics(ctx context.Context, options ports.NQERunMetricsOptions) ([]ports.NQERunMetric, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	var out []ports.NQERunMetric
	resp, err := s.client.doRequired(req, &out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}
