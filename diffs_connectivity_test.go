package forward

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// The summary is asynchronous: reads before the computation settles come back
// all-zero with isPartialResult=true. Wait must poll past them and return the
// settled result, not the first zero.
func TestWaitForSubnetConnectivityPollsPastPartialZeros(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/diffs/100/101/subnet-connectivity" || r.URL.RawQuery != "" {
			t.Errorf("request = %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) < 3 {
			_, _ = w.Write([]byte(`{"newlyIsolatedSubnetPairs":0,"evaluatedSubnetPairs":0,"totalSubnetPairs":39,"isPartialResult":true}`))
			return
		}
		_, _ = w.Write([]byte(`{"newlyConnectedSubnetPairs":0,"newlyIsolatedSubnetPairs":14,"newlyIsolatedImpactedLocations":7,"modifiedSubnetPairs":1,"evaluatedSubnetPairs":39,"totalSubnetPairs":39,"totalImpactedLocations":7,"isPartialResult":false}`))
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)

	got, _, err := c.Diffs.WaitForSubnetConnectivity(context.Background(), "100", "101",
		ConnectivityWaitOptions{Timeout: 5 * time.Second, PollInterval: 10 * time.Millisecond})
	if err != nil {
		t.Fatalf("WaitForSubnetConnectivity: %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("made %d reads, want 3 (two partial, one settled)", calls.Load())
	}
	if got.IsPartialResult || got.NewlyIsolatedSubnetPairs != 14 || got.NewlyIsolatedImpactedLocations != 7 || got.ModifiedSubnetPairs != 1 || got.TotalSubnetPairs != 39 {
		t.Fatalf("settled result decoded wrong: %+v", got)
	}
}

// A comparison still partial at the deadline is an error that carries the
// partial result -- never a zero-change answer.
func TestWaitForSubnetConnectivityTimesOutWithThePartialResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"evaluatedSubnetPairs":5,"totalSubnetPairs":39,"isPartialResult":true}`))
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)

	got, _, err := c.Diffs.WaitForSubnetConnectivity(context.Background(), "100", "101",
		ConnectivityWaitOptions{Timeout: 60 * time.Millisecond, PollInterval: 20 * time.Millisecond})
	if got != nil {
		t.Fatalf("a partial comparison was returned as a result: %+v", got)
	}
	if !errors.Is(err, ErrConnectivityDiffPartial) {
		t.Fatalf("err = %v, want ErrConnectivityDiffPartial", err)
	}
	var partial *ConnectivityDiffPartialError
	if !errors.As(err, &partial) || partial.Partial == nil || partial.Partial.EvaluatedSubnetPairs != 5 {
		t.Fatalf("partial result not carried: %#v", err)
	}
}

// A caller cancellation ends the wait with the context error, and an API error
// surfaces as the typed ErrorResponse rather than being polled past.
func TestWaitForSubnetConnectivityHonoursCancellationAndErrors(t *testing.T) {
	partial := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"isPartialResult":true}`))
	}))
	defer partial.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, err := newTestClient(t, partial.URL).Diffs.WaitForSubnetConnectivity(ctx, "100", "101",
		ConnectivityWaitOptions{Timeout: time.Minute, PollInterval: 20 * time.Millisecond})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled wait err = %v, want context.DeadlineExceeded", err)
	}

	var calls atomic.Int32
	forbidden := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"LOCATION_CONNECTIVITY_DIFFS is disabled"}`))
	}))
	defer forbidden.Close()
	_, _, err = newTestClient(t, forbidden.URL).Diffs.WaitForSubnetConnectivity(context.Background(), "100", "101", ConnectivityWaitOptions{})
	if !IsStatus(err, http.StatusForbidden) || calls.Load() != 1 {
		t.Fatalf("err = %v after %d calls, want one 403 ErrorResponse", err, calls.Load())
	}
	if _, _, err := newTestClient(t, forbidden.URL).Diffs.SubnetConnectivity(context.Background(), "", "101"); err == nil {
		t.Fatal("positive control: a missing snapshot id must be refused before any request")
	}
}

func TestConnectivityDiffLocationsAndLocationPair(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		q := r.URL.Query()
		switch {
		case r.URL.Path != "/api/diffs/100/101/subnet-connectivity":
			http.NotFound(w, r)
		case q.Get("view") == "locations":
			_, _ = w.Write([]byte(`{"stats":[{"locationId":"1","incoming":{"newlyConnected":0,"newlyIsolated":2,"modified":0},"outgoing":{"newlyConnected":0,"newlyIsolated":1,"modified":0}},{"locationId":3,"incoming":{},"outgoing":{}}]}`))
		case q.Get("view") == "locationPair" && q.Get("loc1") == "1" && q.Get("loc2") == "3":
			_, _ = w.Write([]byte(`{"stats":[{"src":"10.1.0.0/24","dst":"10.3.0.0/24","stats":{"newlyConnected":0,"newlyIsolated":1,"modified":0}}]}`))
		default:
			http.Error(w, "unexpected view", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)
	ctx := context.Background()

	locations, _, err := c.Diffs.ConnectivityDiffLocations(ctx, "100", "101")
	if err != nil {
		t.Fatalf("ConnectivityDiffLocations: %v", err)
	}
	if len(locations) != 2 || locations[0].LocationID != "1" || locations[0].Incoming.NewlyIsolated != 2 ||
		locations[0].Outgoing.NewlyIsolated != 1 || !locations[0].Outgoing.Any() {
		t.Fatalf("locations decoded wrong: %+v", locations)
	}
	if locations[1].LocationID != "3" || locations[1].Incoming.Any() || locations[1].Outgoing.Any() {
		t.Fatalf("numeric location id / empty stats decoded wrong: %+v", locations[1])
	}

	pairs, _, err := c.Diffs.ConnectivityDiffLocationPair(ctx, "100", "101", "1", "3")
	if err != nil {
		t.Fatalf("ConnectivityDiffLocationPair: %v", err)
	}
	if len(pairs) != 1 || pairs[0].Src != "10.1.0.0/24" || pairs[0].Dst != "10.3.0.0/24" || pairs[0].Stats.NewlyIsolated != 1 {
		t.Fatalf("pairs decoded wrong: %+v", pairs)
	}
	want := []string{
		"GET /api/diffs/100/101/subnet-connectivity?view=locations",
		"GET /api/diffs/100/101/subnet-connectivity?loc1=1&loc2=3&view=locationPair",
	}
	for i := range want {
		if i >= len(seen) || seen[i] != want[i] {
			t.Fatalf("request %d = %v, want %q", i, seen, want[i])
		}
	}
	if _, _, err := c.Diffs.ConnectivityDiffLocationPair(ctx, "100", "101", "1", " "); err == nil || len(seen) != 2 {
		t.Fatal("positive control: a missing location id must be refused before any request")
	}
}

func TestDiffsChecksListsOneCheckType(t *testing.T) {
	var gotPath, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"checks":[{"diffType":"MODIFIED","a":{"id":4568,"name":"ospf","status":"PASS"},"b":{"id":4568,"name":"ospf","status":"FAIL","numViolations":2}},{"diffType":"ADDED","b":{"id":"abc","name":"new","status":"PASS"}}]}`))
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)

	rows, _, err := c.Diffs.Checks(context.Background(), "100", "101", "PREDEFINED")
	if err != nil {
		t.Fatalf("Checks: %v", err)
	}
	if gotPath != "/api/diffs/100/101/checks" || gotQuery != "type=PREDEFINED" {
		t.Fatalf("request = %s?%s", gotPath, gotQuery)
	}
	if len(rows) != 2 || rows[0].A == nil || rows[0].A.ID != "4568" || rows[0].B.Status != "FAIL" || *rows[0].B.NumViolations != 2 {
		t.Fatalf("rows decoded wrong: %+v", rows)
	}
	if rows[1].A != nil || rows[1].B == nil || rows[1].B.ID != "abc" {
		t.Fatalf("ADDED row decoded wrong: %+v", rows[1])
	}
	if _, _, err := c.Diffs.Checks(context.Background(), "100", "101", ""); err == nil {
		t.Fatal("positive control: an empty check type must be refused (the route without type= is the counts route)")
	}
}
