package forward

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Bulk path search answers an array with one entry per query in order, each either a normal path response or {"error": true,
// "errorMessage": ...} for that query alone (LocalSearchWorker.PathSearchError), so one bad device name must not fail the request or
// shift the others. The shared settings ride in the body next to the queries, the snapshot as a query parameter.
func TestPathsBulkReturnsOneResultPerQueryInOrder(t *testing.T) {
	t.Parallel()

	var uri, body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		uri, body = r.URL.RequestURI(), string(raw)
		_, _ = w.Write([]byte(`[
			{"srcIpLocationType":"HOST","dstIpLocationType":"HOST","info":{"paths":[{"forwardingOutcome":"DELIVERED","hops":[]}],"totalHits":{"type":"EXACT","value":1}},"returnPathInfo":{"paths":[{"forwardingOutcome":"BLACKHOLE","hops":[]}],"totalHits":{"type":"EXACT","value":1}},"timedOut":false},
			{"error":true,"errorMessage":"No such device nope in snapshot."},
			{"info":{"paths":[],"totalHits":{"type":"EXACT","value":0}},"returnPathInfo":{"paths":[]},"timedOut":true}]`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	proto := 6
	queries := []PathQuery{
		{SrcIP: "10.0.0.1", DstIP: "10.1.0.1", IPProto: &proto, DstPort: "443"},
		{From: "nope", DstIP: "10.1.0.1"},
		{SrcIP: "10.1.0.1", DstIP: "10.0.0.1"},
	}
	results, _, err := client.Networks.PathsBulk(context.Background(), "N1", queries, PathBulkOptions{
		SnapshotID: "S1", Intent: "PREFER_VIOLATIONS", MaxReturnPathResults: Ptr(2), MaxOverallSeconds: Ptr(600), IncludeTags: Ptr(true),
	})
	if err != nil || len(results) != 3 {
		t.Fatalf("%v %+v", err, results)
	}
	if results[0].Failed || results[0].Response.Info.Paths[0].ForwardingOutcome != "DELIVERED" || results[0].Response.ReturnPathInfo.Paths[0].ForwardingOutcome != "BLACKHOLE" {
		t.Errorf("first result keeps its return path: %+v", results[0])
	}
	if !results[1].Failed || results[1].Response != nil || results[1].ErrorMessage != "No such device nope in snapshot." {
		t.Errorf("a failed query is its own error, not the request's: %+v", results[1])
	}
	if results[2].Failed || !results[2].Response.TimedOut {
		t.Errorf("order and timeout flag: %+v", results[2])
	}
	if uri != "/api/networks/N1/paths-bulk?snapshotId=S1" {
		t.Errorf("uri %s", uri)
	}
	var sent struct {
		Queries              []map[string]any `json:"queries"`
		Intent               string           `json:"intent"`
		MaxReturnPathResults int              `json:"maxReturnPathResults"`
		MaxOverallSeconds    int              `json:"maxOverallSeconds"`
		IncludeTags          bool             `json:"includeTags"`
	}
	if err := json.Unmarshal([]byte(body), &sent); err != nil {
		t.Fatal(err)
	}
	if len(sent.Queries) != 3 || sent.Queries[0]["srcIp"] != "10.0.0.1" || sent.Queries[0]["ipProto"] != float64(6) || sent.Queries[0]["dstPort"] != "443" || sent.Queries[1]["from"] != "nope" ||
		sent.Intent != "PREFER_VIOLATIONS" || sent.MaxReturnPathResults != 2 || sent.MaxOverallSeconds != 600 || !sent.IncludeTags {
		t.Errorf("body: %s", body)
	}
	if strings.Contains(body, `"maxSeconds"`) || strings.Contains(body, `"maxResults"`) {
		t.Errorf("unset limits must be left to Forward's defaults: %s", body)
	}
}

func TestPathsBulkRefusesWhatForwardRefuses(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()
	ok := PathQuery{SrcIP: "10.0.0.1", DstIP: "10.1.0.1"}
	for name, c := range map[string]struct {
		queries []PathQuery
		options PathBulkOptions
	}{
		"no queries":       {nil, PathBulkOptions{}},
		"too many queries": {make([]PathQuery, MaxPathBulkQueries+1), PathBulkOptions{}},
		"no destination":   {[]PathQuery{ok, {SrcIP: "10.0.0.1"}}, PathBulkOptions{}},
		"no source":        {[]PathQuery{{DstIP: "10.0.0.1"}}, PathBulkOptions{}},
		"url and domain":   {[]PathQuery{{SrcIP: "a", DstIP: "b", URL: "x", Domain: "y"}}, PathBulkOptions{}},
		"unknown intent":   {[]PathQuery{ok}, PathBulkOptions{Intent: "PREFER_NOTHING"}},
		"zero seconds":     {[]PathQuery{ok}, PathBulkOptions{MaxSeconds: Ptr(0)}},
		"overall too long": {[]PathQuery{ok}, PathBulkOptions{MaxOverallSeconds: Ptr(7201)}},
		"negative returns": {[]PathQuery{ok}, PathBulkOptions{MaxReturnPathResults: Ptr(-1)}},
	} {
		if _, _, err := client.Networks.PathsBulk(ctx, "N1", c.queries, c.options); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if hits.Load() != 0 {
		t.Errorf("%d request(s) for refused calls", hits.Load())
	}
	// A result array of a different length than the queries is not trusted.
	short := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`[]`)) }))
	defer short.Close()
	if _, _, err := newTestClient(t, short.URL).Networks.PathsBulk(ctx, "N1", []PathQuery{ok}, PathBulkOptions{}); err == nil {
		t.Error("a short answer must be an error")
	}
}

// Forward writes nothing until every query is done and lets a query that has already started run its full per-query time after the
// overall deadline, so the wait is overall + per-query + margin, and the SDK only ever lengthens a shorter client timeout.
func TestPathsBulkWorstCaseWait(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		options PathBulkOptions
		want    time.Duration
	}{
		{PathBulkOptions{}, (30+30)*time.Second + 2*time.Minute},
		{PathBulkOptions{MaxSeconds: Ptr(300)}, (300+300)*time.Second + 2*time.Minute},
		{PathBulkOptions{MaxSeconds: Ptr(300), MaxOverallSeconds: Ptr(600)}, (600+300)*time.Second + 2*time.Minute},
	} {
		if got := c.options.worstCase(); got != c.want {
			t.Errorf("%+v: %s, want %s", c.options, got, c.want)
		}
	}
}
