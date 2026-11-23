package forward

import (
	"github.com/forwardnetworks/forward-go-sdk/domain"
)

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"

	"sync/atomic"
	"testing"
	"time"
)

// The lifecycle calls go through the same client as every other call, so a
// Forward 429 (the per-user rate limiter or any refusal) is retried with backoff
// when the client has a retry policy -- including a PUT with a text body, which
// has to be re-sent whole -- and a call with no policy is sent once.
func TestPredictLifecycleHonoursTheRetryPolicyOn429(t *testing.T) {
	t.Parallel()

	var puts, gets atomic.Int32
	var lastBody atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		count := &gets
		if r.Method == http.MethodPut {
			count = &puts
			lastBody.Store(string(body))
		}
		if count.Add(1) <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"message":"Rate limit exceeded, please try again later"}`))
			return
		}
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = w.Write([]byte("hostname r1\n"))
	}))
	defer server.Close()
	ctx := context.Background()

	retrying, err := NewClient(Config{BaseURL: server.URL, Username: "u", Password: "p", HTTPClient: privateHTTPClient(),
		Retry: RetryPolicy{MaxAttempts: 4, Delay: time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := retrying.Predict.SaveDeviceConfig(ctx, "5", "cs1", "r1", "hostname r1\n"); err != nil || puts.Load() != 3 || lastBody.Load() != "hostname r1\n" {
		t.Fatalf("PUT after two 429s: err=%v attempts=%d body=%v", err, puts.Load(), lastBody.Load())
	}
	if cfg, _, err := retrying.Predict.GetDraftConfig(ctx, "5", "cs1", "r1"); err != nil || cfg != "hostname r1\n" || gets.Load() != 3 {
		t.Fatalf("GET after two 429s: err=%v attempts=%d %q", err, gets.Load(), cfg)
	}

	gets.Store(0)
	once, err := NewClient(Config{BaseURL: server.URL, Username: "u", Password: "p", HTTPClient: privateHTTPClient()})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := once.Predict.GetDraftConfig(ctx, "5", "cs1", "r1"); !errors.Is(err, domain.ErrRateLimited) || gets.Load() != 1 {
		t.Fatalf("with no retry policy the 429 comes back after one attempt: err=%v attempts=%d", err, gets.Load())
	}
}

// Forward has no plain per-change-set read; ?view=summary gives the base information and the linked ServiceNow change requests, which is
// how a caller learns the base snapshot of one change set without listing them all.
func TestPredictGetChangeSetSummary(t *testing.T) {
	t.Parallel()

	var gotURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.Method + " " + r.URL.RequestURI()
		_, _ = w.Write([]byte(`{"name":"depeer lumen","description":"what-if","networkId":"205507","snapshotId":"9001","tags":["wf"],"serviceNowChangeRequests":[{"number":"CHG001","sysId":"abc"}]}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	summary, _, err := client.Predict.GetChangeSetSummary(context.Background(), "205507", "cs1")
	if err != nil {
		t.Fatal(err)
	}
	if gotURL != "GET /api/networks/205507/change-sets/cs1?view=summary" || summary.SnapshotID != "9001" || summary.Name != "depeer lumen" ||
		len(summary.ServiceNowChangeRequests) != 1 || summary.ServiceNowChangeRequests[0].SysID != "abc" {
		t.Fatalf("%s %+v", gotURL, summary)
	}
	if summary.NetworkID != "205507" || len(summary.Tags) != 1 || summary.Tags[0] != "wf" {
		t.Fatalf("base fields %+v", summary)
	}
	if _, _, err := client.Predict.GetChangeSetSummary(context.Background(), "205507", " "); err == nil {
		t.Fatal("a change set id is required")
	}
}

// With nothing linked Forward leaves serviceNowChangeRequests and tags out of the body, which is the shape most callers see.
func TestPredictGetChangeSetSummaryWithoutLinks(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"x","networkId":"5","snapshotId":"6"}`))
	}))
	defer server.Close()
	summary, _, err := newTestClient(t, server.URL).Predict.GetChangeSetSummary(context.Background(), "5", "cs1")
	if err != nil || summary.Name != "x" || len(summary.Tags) != 0 || len(summary.ServiceNowChangeRequests) != 0 || summary.Description != "" {
		t.Fatalf("%v %+v", err, summary)
	}
}
