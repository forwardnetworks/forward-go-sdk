package forward

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// "View interesting paths" is one synchronous GET with the snapshot and a type
// (FIREWALL or LOADBALANCER, plus the suspicious-path kinds), optionally bounded
// by maxSeconds, which makes the call block until Forward is done. The wire must
// carry exactly those, refuse a type Forward would answer with a 400 before
// sending it, and decode each ranked path including the path query the UI link
// embeds (the stable path is /net/snap/paths/<base64url JSON>).
func TestNetworksInterestingPaths(t *testing.T) {
	t.Parallel()

	query := `{"srcIp":"10.0.0.0/24","dstIp":"10.1.0.0/24"}`
	link := "/152733/504440/paths/" + base64.RawURLEncoding.EncodeToString([]byte(query))
	var gotURLs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURLs = append(gotURLs, r.Method+" "+r.URL.RequestURI())
		_, _ = w.Write([]byte(`{"interestingPathQueries":[{"id":"p1","searchPath":"` + link + `","score":42,"samplePathGroup":{"tags":["IN_ACL","XLATE_DNAT"],"numHops":6,"flowType":"VALID"}}],"duration":1234}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()

	found, _, err := client.Networks.InterestingPaths(ctx, "152733", "504440", InterestingPathsFirewall, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Paths) != 1 || found.Paths[0].Score != 42 || found.Paths[0].SamplePathGroup.NumHops != 6 || found.Paths[0].SamplePathGroup.Tags[1] != "XLATE_DNAT" || string(found.Duration) != "1234" {
		t.Fatalf("decoded %+v", found)
	}
	got, err := found.Paths[0].PathQuery()
	if err != nil || string(got) != query {
		t.Fatalf("PathQuery = %s, %v", got, err)
	}
	if _, err := (InterestingPath{SearchPath: "/152733/504440/device/r1"}).PathQuery(); err == nil {
		t.Fatal("a link that is not a path search has no path query")
	}
	if _, _, err := client.Networks.InterestingPaths(ctx, "152733", "504440", InterestingPathsLoadBalancer, 90); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"GET /api/networks/152733/interesting-path-queries?snapshotId=504440&type=FIREWALL",
		"GET /api/networks/152733/interesting-path-queries?maxSeconds=90&snapshotId=504440&type=LOADBALANCER",
	}
	if strings.Join(gotURLs, "|") != strings.Join(want, "|") {
		t.Fatalf("requests %v", gotURLs)
	}

	sent := len(gotURLs)
	for name, call := range map[string]func() error{
		"unknown type": func() error { _, _, err := client.Networks.InterestingPaths(ctx, "5", "6", "ROUTERS", 0); return err },
		"no snapshot": func() error {
			_, _, err := client.Networks.InterestingPaths(ctx, "5", " ", InterestingPathsFirewall, 0)
			return err
		},
		"negative wait": func() error {
			_, _, err := client.Networks.InterestingPaths(ctx, "5", "6", InterestingPathsFirewall, -1)
			return err
		},
	} {
		if call() == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if len(gotURLs) != sent {
		t.Fatal("a refused search reached Forward")
	}
}

// A snapshot that has not finished advanced reachability is a 400 with no reason code and the text "Advanced reachability COMPUTING for
// snapshot X." on any route, so the typed kinds must come from that text: a run still going or not started is "not processed", a
// canceled, timed-out or failed one is "processing failed".
func TestInterestingPathsClassifiesAdvancedReachabilityStates(t *testing.T) {
	t.Parallel()

	for state, want := range map[string]error{
		"NOT_TRIGGERED": ErrSnapshotNotProcessed, "QUEUED": ErrSnapshotNotProcessed, "COMPUTING": ErrSnapshotNotProcessed,
		"CANCELED": ErrSnapshotProcessingFailed, "TIMED_OUT": ErrSnapshotProcessingFailed, "FAILED": ErrSnapshotProcessingFailed,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"apiUrl":"/api/networks/5/interesting-path-queries","httpMethod":"GET","message":"Advanced reachability ` + state + ` for snapshot 6."}`))
		}))
		client := newTestClient(t, server.URL)
		_, _, err := client.Networks.InterestingPaths(context.Background(), "5", "6", InterestingPathsFirewall, 0)
		server.Close()
		if !errors.Is(err, want) {
			t.Errorf("%s: got %v, want %v", state, err, want)
		}
	}
}
