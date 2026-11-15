package forwardtest_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	forward "github.com/forwardnetworks/forward-go-sdk"
	"github.com/forwardnetworks/forward-go-sdk/forwardtest"
)

// The point of the fake: each canned error must classify as the same forward error the real appserver's reply does, and a route nobody
// mapped must read as "not served here", not as a missing network.
func TestErrorRepliesClassifyLikeTheRealAppserver(t *testing.T) {
	cases := []struct {
		name  string
		reply forwardtest.Reply
		want  error
	}{
		{"rate limited", forwardtest.RateLimited(30), forward.ErrRateLimited},
		{"feature gated", forwardtest.FeatureGated("SOFTWARE_CENTRAL", false, "organization"), forward.ErrFeatureGated},
		{"missing permission", forwardtest.MissingPermission("NetworkOperation", "EDIT_CHECKS"), forward.ErrPermissionDenied},
		{"unauthorized", forwardtest.Unauthorized(), forward.ErrAuthentication},
		{"action not supported", forwardtest.ActionNotSupported("bulkAdd"), forward.ErrActionNotSupported},
		{"snapshot not processed", forwardtest.SnapshotNotProcessed(), forward.ErrSnapshotNotProcessed},
	}
	for _, tc := range cases {
		srv := forwardtest.New(t)
		srv.Handle(http.MethodGet, "/api/networks", tc.reply)
		_, _, err := srv.Client().Networks.List(context.Background())
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want errors.Is %v", tc.name, err, tc.want)
		}
	}

	srv := forwardtest.New(t) // nothing mapped
	_, _, err := srv.Client().Networks.List(context.Background())
	if !errors.Is(err, forward.ErrEndpointNotServed) {
		t.Fatalf("an unmapped route must read as endpoint-not-served, got %v", err)
	}
}

// Replies are served in order and the last repeats, so a flaky call and its recovery can be scripted; the rate-limit reply carries the
// lockout the client's retry honours.
func TestRepliesAreServedInOrderAndRetriesSeeThem(t *testing.T) {
	srv := forwardtest.New(t)
	srv.Handle(http.MethodGet, "/api/networks", forwardtest.RateLimited(0), forwardtest.JSON(http.StatusOK, []map[string]any{{"id": "1", "name": "lab"}}))
	client := srv.Client(func(c *forward.Config) {
		c.Retry = forward.RetryPolicy{MaxAttempts: 3, Delay: time.Millisecond, MaxDelay: time.Second}
	})
	networks, _, err := client.Networks.List(context.Background())
	if err != nil || len(networks) != 1 {
		t.Fatalf("networks=%v err=%v", networks, err)
	}
	if got := len(srv.RequestsTo(http.MethodGet, "/api/networks")); got != 2 {
		t.Fatalf("expected the 429 and then the retry, got %d requests", got)
	}
}

// Predict BGP end to end: stage, read the diffs back, and see a withdraw that matched nothing as the error it is.
func TestPredictBGPStagingScenario(t *testing.T) {
	srv := forwardtest.New(t)
	srv.BGPStaging("n1", "c1", "edge")
	staged := forward.BGPAdvertisement{VRF: "default", ExternalPeer: "192.0.2.1", Prefix: "203.0.113.0/24", Type: "EBGP", NextHop: "192.0.2.1", Origin: "IGP", ASPath: []int64{64500}}
	ghost := forward.BGPAdvertisementKey{VRF: "default", ExternalPeer: "192.0.2.9", Prefix: "198.51.100.0/24"}
	srv.BGPDiffs("n1", "c1", "edge", forwardtest.BGPInjected(staged), forwardtest.BGPAbsent(ghost))
	client := srv.Client()
	ctx := context.Background()

	if _, err := client.Predict.WithdrawBGPAdvertisement(ctx, "n1", "c1", "edge", ghost); err != nil {
		t.Fatal(err)
	}
	report, _, err := client.Predict.VerifyBGPStaging(ctx, "n1", "c1", "edge", []forward.BGPAdvertisement{staged}, []forward.BGPAdvertisementKey{ghost})
	if !errors.Is(err, forward.ErrBGPKeyMatchedNothing) || report.Staged[0].Change != forward.BGPChangeInjected {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}

// NQE paging: RunAll reads every page of the fake's rows and the totals agree.
func TestNQEPagingScenario(t *testing.T) {
	srv := forwardtest.New(t)
	rows := make([]map[string]any, 25)
	for i := range rows {
		rows[i] = map[string]any{"Device": fmt.Sprintf("d%02d", i)}
	}
	srv.NQE("9", rows)
	got, _, err := srv.Client().NQE.RunAll(context.Background(), "n1", "9", forward.NQEQueryRequest{Query: "select {Device: 1}"}, forward.NQEPagedOptions{KeyColumn: "Device", PageSize: 10})
	if err != nil || len(got) != 25 {
		t.Fatalf("got %d rows, err %v", len(got), err)
	}
	if pages := len(srv.RequestsTo(http.MethodPost, "/api/nqe")); pages != 3 {
		t.Fatalf("expected 3 pages, got %d", pages)
	}
}

// Check diffs that are still PROCESSING: WaitChecks must read until the fake says they are done.
func TestCheckDiffsProcessingScenario(t *testing.T) {
	srv := forwardtest.New(t)
	srv.CheckDiffs("10", "11", "NQE", 2, []forward.CheckDiffEntry{{DiffType: "UNCHANGED"}})
	rows, _, err := srv.Client().Diffs.WaitChecks(context.Background(), "10", "11", "NQE", forward.PollOptions[[]forward.CheckDiffEntry]{Interval: time.Millisecond})
	if err != nil || len(rows) != 1 || rows[0].DiffType != "UNCHANGED" {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	if got := len(srv.Requests()); got != 3 {
		t.Fatalf("expected the first read and two polls, got %d", got)
	}
}

// Received advertisements: a prefix with nothing is an empty list, as on a real device.
func TestReceivedBGPScenario(t *testing.T) {
	srv := forwardtest.New(t)
	row := forward.BGPAdvertisement{VRF: "default", ExternalPeer: "10.0.0.2", Prefix: "10.0.0.2", Type: "EBGP", NextHop: "10.0.0.2", Origin: "IGP", ASPath: []int64{65001}}
	srv.ReceivedBGP("n1", "edge", map[[2]string][]forward.BGPAdvertisement{{"default", "10.0.0.2"}: {row}})
	client := srv.Client()
	got, _, err := client.Devices.BGPAdvertisements(context.Background(), "n1", "edge", "", "default", "10.0.0.2")
	if err != nil || len(got) != 1 || got[0].ExternalPeer != "10.0.0.2" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	none, _, err := client.Devices.BGPAdvertisements(context.Background(), "n1", "edge", "", "default", "192.0.2.0/24")
	if err != nil || none == nil || len(none) != 0 {
		t.Fatalf("none=%#v err=%v", none, err)
	}
}
