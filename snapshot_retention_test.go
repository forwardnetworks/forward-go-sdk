package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNetworksSnapshotRetentionPolicyRoundTrip(t *testing.T) {
	t.Parallel()

	var method, uri, body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		method, uri, body = r.Method, r.URL.RequestURI(), string(b)
		if r.Method == http.MethodPut {
			_, _ = io.WriteString(w, `{}`)
			return
		}
		_, _ = io.WriteString(w, `{"enabled":true,"lastWeek":"ALL","lastMonth":"ONE_PER_DAY","lastQuarter":"ONE_PER_WEEK","lastYear":"ONE_PER_MONTH","older":"ONE_PER_QUARTER"}`)
	}))
	defer server.Close()
	networks := newTestClient(t, server.URL).Networks
	ctx := context.Background()

	policy, _, err := networks.GetSnapshotRetentionPolicy(ctx, "N1")
	if err != nil || uri != "/api/networks/N1/snapshotRetentionPolicy" || !policy.Enabled || policy.LastMonth != SnapshotRetainPerDay || policy.Older != SnapshotRetainPerQuarter {
		t.Fatalf("Get() = %+v, %v; %s", policy, err, uri)
	}
	// Turning thinning off is read-modify-write: Forward validates every
	// granularity even when the policy is disabled.
	policy.Enabled = false
	if _, err := networks.SetSnapshotRetentionPolicy(ctx, "N1", *policy); err != nil || method != http.MethodPut ||
		body != `{"enabled":false,"lastWeek":"ALL","lastMonth":"ONE_PER_DAY","lastQuarter":"ONE_PER_WEEK","lastYear":"ONE_PER_MONTH","older":"ONE_PER_QUARTER"}` {
		t.Fatalf("Set: %v %s %s", err, method, body)
	}
	bad := *policy
	bad.Older = "FOREVER"
	before := uri
	if _, err := networks.SetSnapshotRetentionPolicy(ctx, "N1", bad); err == nil || method != http.MethodPut || uri != before {
		t.Fatalf("an unknown granularity must be refused locally: %v", err)
	}
	if _, err := networks.SetSnapshotRetentionPolicy(ctx, "N1", SnapshotRetentionPolicy{}); err == nil {
		t.Fatal("a zero policy (empty granularities) must be refused locally")
	}
}

// SaaS has one fixed policy and answers the PUT with 404, as an unknown
// network does; the error must say why without hiding the status.
func TestNetworksSetSnapshotRetentionPolicyOnSaaS(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	policy := SnapshotRetentionPolicy{Enabled: false, LastWeek: SnapshotRetainAll, LastMonth: SnapshotRetainPerDay, LastQuarter: SnapshotRetainPerWeek, LastYear: SnapshotRetainPerMonth, Older: SnapshotRetainPerQuarter}

	_, err := newTestClient(t, server.URL).Networks.SetSnapshotRetentionPolicy(context.Background(), "N1", policy)
	if err == nil || !IsStatus(err, http.StatusNotFound) {
		t.Fatalf("err = %v", err)
	}
}

// The trigger route deletes synchronously unless dryRun=true, so the preview
// must never be able to drop it: this pins the query on every call.
func TestNetworksPreviewSnapshotRetentionNeverDeletes(t *testing.T) {
	t.Parallel()

	var method, uri string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, uri = r.Method, r.URL.RequestURI()
		_, _ = io.WriteString(w, `{"count":2,"deletedSnapshots":[
		  {"id":"101","createdAt":"2026-05-01T00:00:00Z","processedAt":"2026-05-01T00:30:00Z","note":"nightly","isDraft":false},
		  {"id":"102","createdAt":"2026-05-02T00:00:00Z","parentSnapshotId":"","isDraft":false}]}`)
	}))
	defer server.Close()

	got, _, err := newTestClient(t, server.URL).Networks.PreviewSnapshotRetention(context.Background(), "N1")
	if err != nil || method != http.MethodPost || uri != "/api/networks/N1/snapshotRetentionPolicy/trigger?dryRun=true" {
		t.Fatalf("Preview: %v %s %s", err, method, uri)
	}
	if got.Count != 2 || len(got.Snapshots) != 2 || got.Snapshots[0].ID != "101" || got.Snapshots[0].Note != "nightly" || got.Snapshots[1].CreatedAt == "" {
		t.Fatalf("preview = %+v", got)
	}
}

// Deleting a data file detaches it everywhere; one that is already gone is
// not an error.
func TestDataFilesDelete(t *testing.T) {
	t.Parallel()

	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.EscapedPath())
		if r.URL.Path == "/api/data-files/gone.csv" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"No data file with name 'gone.csv' exists."}`)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	files := newTestClient(t, server.URL).DataFiles
	ctx := context.Background()

	if _, err := files.Delete(ctx, "a b.csv"); err != nil || calls[0] != "DELETE /api/data-files/a%20b.csv" {
		t.Fatalf("Delete: %v %v", err, calls)
	}
	if _, err := files.Delete(ctx, "gone.csv"); err != nil {
		t.Fatalf("a missing file must be success: %v", err)
	}
	if _, err := files.Delete(ctx, " "); err == nil || len(calls) != 2 {
		t.Fatalf("an empty name must be refused locally (calls=%v, err=%v)", calls, err)
	}
}
