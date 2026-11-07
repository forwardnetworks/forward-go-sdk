package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Forward 26.11 (FWD-61712) hides predicted snapshots from the network
// snapshot listing unless includePredicted=true. Predict.RunOperation polls
// the snapshot it started through Snapshots.Get, which reads that listing, so Get must ask
// for them or the poll never finds its snapshot. A caller who sets nothing must
// send no parameter, so earlier builds and the new default are untouched.
func TestSnapshotListingAsksForPredictedSnapshotsOnlyWhenWanted(t *testing.T) {
	t.Parallel()

	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		if r.URL.Query().Get("includePredicted") == "true" {
			_, _ = io.WriteString(w, `{"snapshots":[{"id":"collected-1","state":"PROCESSED"},{"id":"predicted-1","state":"PROCESSED","processingTrigger":"PREDICT","parentSnapshotId":"collected-1"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"snapshots":[{"id":"collected-1","state":"PROCESSED"}]}`)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	if _, _, err := client.Snapshots.List(context.Background(), "n1", SnapshotListOptions{}); err != nil {
		t.Fatal(err)
	}
	if queries[0] != "" {
		t.Fatalf("a plain List sent %q, want no query", queries[0])
	}

	yes := true
	listed, _, err := client.Snapshots.List(context.Background(), "n1", SnapshotListOptions{IncludePredicted: &yes})
	if err != nil || len(listed) != 2 || queries[1] != "includePredicted=true" {
		t.Fatalf("List(IncludePredicted) = %d rows, %v, query %q", len(listed), err, queries[1])
	}

	// ListDocument keeps Forward's envelope but must forward the same flag.
	if _, _, err := client.Snapshots.ListDocument(context.Background(), "n1", SnapshotListOptions{IncludePredicted: &yes}); err != nil || queries[len(queries)-1] != "includePredicted=true" {
		t.Fatalf("ListDocument(IncludePredicted) err %v, query %q", err, queries[len(queries)-1])
	}

	got, _, err := client.Snapshots.Get(context.Background(), "n1", "predicted-1")
	if err != nil || got.ID != "predicted-1" || !got.Predicted() {
		t.Fatalf("Get(predicted-1) = %#v, %v", got, err)
	}
	if q := queries[len(queries)-1]; q != "includeArchived=true&includePredicted=true&limit=1000" {
		t.Fatalf("Get listing query = %q", q)
	}
}

// GET /api/networks/{id} is new in 26.11 (NetworkController.getNetwork). It
// escapes the ID and refuses an empty one before sending.
func TestNetworksGetReadsOneNetwork(t *testing.T) {
	t.Parallel()

	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Method + " " + r.URL.EscapedPath()
		_, _ = io.WriteString(w, `{"id":"net 1","name":"Lab","orgId":"org-1"}`)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	network, _, err := client.Networks.Get(context.Background(), "net 1")
	if err != nil || network.Name != "Lab" || got != "GET /api/networks/net%201" {
		t.Fatalf("Networks.Get = %#v, %v, sent %q", network, err, got)
	}
	got = ""
	if _, _, err := client.Networks.Get(context.Background(), " "); err == nil || got != "" {
		t.Fatalf("an empty network ID reached the server (%q) or was accepted: %v", got, err)
	}
}

// Reprocessing an already invalidated snapshot is its own action in 26.11 and
// answers 202 with the state change; it must not carry the invalidate action
// or the reprocess flag that Snapshots.Reprocess uses.
func TestSnapshotsReprocessInvalidatedSendsTheReprocessAction(t *testing.T) {
	t.Parallel()

	var method, uri string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, uri = r.Method, r.URL.RequestURI()
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"previousState":"UNPROCESSED","state":"PROCESSING"}`)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	transition, _, err := client.Snapshots.ReprocessInvalidated(context.Background(), "s/1")
	if err != nil || transition.PreviousState != "UNPROCESSED" || transition.State != "PROCESSING" {
		t.Fatalf("ReprocessInvalidated = %#v, %v", transition, err)
	}
	if method != http.MethodPost || uri != "/api/snapshots/s%2F1?action=reprocess" {
		t.Fatalf("sent %s %s", method, uri)
	}
	method = ""
	if _, _, err := client.Snapshots.ReprocessInvalidated(context.Background(), ""); err == nil || method != "" {
		t.Fatalf("an empty snapshot ID reached the server or was accepted: %v", err)
	}
}
