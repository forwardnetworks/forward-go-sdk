package forward

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The explain routes take the bare parameter map as the body (not wrapped),
// answer text/plain, and need the id or path escaped as the UI does; an
// absent commit or snapshot must send no parameter so Forward applies HEAD
// and no snapshot schema.
func TestExplainSendsTheBareParametersAndReturnsThePlan(t *testing.T) {
	var gotPath, gotQuery, gotBody, gotAccept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotPath, gotQuery, gotBody, gotAccept = r.URL.EscapedPath(), r.URL.RawQuery, string(raw), r.Header.Get("Accept")
		if r.URL.Query().Get("snapshotId") == "bad" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"message":"query has diagnostics"}`)
			return
		}
		w.Header().Set("Content-Type", "text/plain;charset=UTF-8")
		_, _ = io.WriteString(w, "parallel_foreach d in devices\n  -- total, cardinality 1\n")
	}))
	defer server.Close()
	nqe := newTestClient(t, server.URL).NQE
	ctx := context.Background()

	plan, _, err := nqe.ExplainQuery(ctx, "FQ_a/b", "c1", "s1", map[string]any{"threshold": 3})
	if err != nil || !strings.Contains(plan, "parallel_foreach") {
		t.Fatalf("plan = %q, %v", plan, err)
	}
	if gotPath != "/api/internal/nqe/queries/FQ_a%2Fb/explain" || gotQuery != "commitId=c1&snapshotId=s1" || gotBody != `{"threshold":3}` || !strings.HasPrefix(gotAccept, "text/plain") {
		t.Fatalf("request = %s ?%s body=%s accept=%s", gotPath, gotQuery, gotBody, gotAccept)
	}
	if _, _, err = nqe.ExplainQuery(ctx, "FQ_x", "", "", nil); err != nil || gotQuery != "" || gotBody != `{}` {
		t.Fatalf("no commit, snapshot or params must send none: ?%s body=%s (%v)", gotQuery, gotBody, err)
	}
	if _, _, err = nqe.ExplainPath(ctx, "/Ops/my query", "s1", nil); err != nil ||
		gotPath != "/api/internal/users/current/nqe/queries/explain" || gotQuery != "path=%2FOps%2Fmy+query&snapshotId=s1" {
		t.Fatalf("path explain = %s ?%s (%v)", gotPath, gotQuery, err)
	}
	if _, _, err = nqe.ExplainQuery(ctx, "FQ_x", "", "bad", nil); !isStatus(err, http.StatusBadRequest) {
		t.Fatalf("a query with diagnostics must be a 400, not a plan: %v", err)
	}
	if _, _, err = nqe.ExplainQuery(ctx, " ", "", "", nil); err == nil {
		t.Fatal("a blank query id must fail before any request")
	}
}

// DEBUG_NQE is an org operation most users lack, so the 403 must be typed.
// Times are epoch milliseconds, runDuration milliseconds; the 0 for
// MinExecutionSeconds is sent (every run), unlike the unset default.
func TestRunMetricsDecodesAndMapsThePermissionError(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		if r.URL.Query().Get("limit") == "1" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"message":"Missing permission: OrgOperation.DEBUG_NQE"}`)
			return
		}
		_, _ = io.WriteString(w, `[{"taskId":"T-1","networkId":"12","networkName":"lab","snapshotId":"s1","snapshotCreationInstant":1792000000000,"executedAt":1792003600000,"runDuration":12345,"sourceSetId":"NqeSourceSetId_ab12","queryId":"FQ_1","commitId":"c9","trackingId":"trk"},{"taskId":"T-2","executedAt":1792003700000,"runDuration":10000,"sourceSetId":"NqeSourceSetId_cd34","trackingId":"t2"}]`)
	}))
	defer server.Close()
	nqe := newTestClient(t, server.URL).NQE
	ctx := context.Background()

	metrics, _, err := nqe.RunMetrics(ctx, NQERunMetricsOptions{
		From: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), MinExecutionSeconds: Ptr(0),
	})
	if err != nil || len(metrics) != 2 {
		t.Fatalf("metrics = %+v, %v", metrics, err)
	}
	if gotQuery != "from=2026-10-01T00%3A00%3A00Z&minExecutionSeconds=0" {
		t.Fatalf("query = %s", gotQuery)
	}
	first := metrics[0]
	if first.TaskID != "T-1" || first.NetworkID != "12" || first.SnapshotID != "s1" || first.SourceSetID != "NqeSourceSetId_ab12" || first.NetworkName != "lab" || first.QueryID != "FQ_1" || first.CommitID != "c9" || first.TrackingID != "trk" ||
		first.RunDuration != 12345*time.Millisecond || first.ExecutedAt.UnixMilli() != 1792003600000 || first.SnapshotCreationInstant.UnixMilli() != 1792000000000 {
		t.Fatalf("first = %+v", first)
	}
	if second := metrics[1]; second.QueryID != "" || second.SnapshotID != "" || !second.SnapshotCreationInstant.IsZero() {
		t.Fatalf("an ad-hoc run has no query or snapshot: %+v", second)
	}
	_, _, err = nqe.RunMetrics(ctx, NQERunMetricsOptions{Limit: Ptr(1)})
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("a caller without DEBUG_NQE must get the typed 403: %v", err)
	}
	if op, ok := MissingPermission(err); !ok || op != "OrgOperation.DEBUG_NQE" {
		t.Fatalf("MissingPermission = %q, %v", op, ok)
	}
}

// A plan larger than the cap must fail rather than fill memory: the cap is
// checked on the path io.Copy really takes, not only on Write.
func TestExplainRefusesAPlanBeyondTheCap(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := strings.Repeat("x", 1<<20)
		for i := 0; i < maxExplainBytes>>20+2; i++ {
			_, _ = io.WriteString(w, chunk)
		}
	}))
	defer server.Close()
	plan, _, err := newTestClient(t, server.URL).NQE.ExplainQuery(context.Background(), "FQ_big", "", "", nil)
	if err == nil || plan != "" || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("an oversized plan must be refused: len=%d, %v", len(plan), err)
	}
	if _, _, err := newTestClient(t, server.URL).NQE.RunMetrics(context.Background(), NQERunMetricsOptions{Limit: Ptr(-1)}); err == nil {
		t.Fatal("a negative limit must fail before any request")
	}
}
