package forward

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The material-diff gate must ignore state-file churn and count everything
// else; the wire paths are the appserver DiffController's.
func TestDiffsMaterialSummaryIgnoresSuperficialFileChurn(t *testing.T) {
	seen := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen[r.URL.Path+"?"+r.URL.RawQuery] = true
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/diffs/100/101/devices" && r.URL.RawQuery == "count":
			_, _ = w.Write([]byte(`{"count":0,"complete":true}`))
		case r.URL.Path == "/api/diffs/100/101/interfaces" && r.URL.RawQuery == "count":
			_, _ = w.Write([]byte(`{"count":2,"complete":true}`))
		case r.URL.Path == "/api/diffs/100/101/routing-loop/count":
			_, _ = w.Write([]byte(`{"count":0,"complete":false}`))
		case r.URL.Path == "/api/diffs/100/101/files" && r.URL.RawQuery == "count":
			_, _ = w.Write([]byte(`{"count":7,"complete":true,"types":{"SHOW":7,"CONFIG":0}}`))
		case r.URL.Path == "/api/diffs/100/101/checks" && r.URL.RawQuery == "counts":
			_, _ = w.Write([]byte(`{"PREDEFINED":{"count":0,"complete":true},"NQE":{"count":0,"complete":true}}`))
		default:
			_, _ = w.Write([]byte(`{"count":0,"complete":true}`))
		}
	}))
	defer server.Close()
	c, err := NewClient(Config{BaseURL: server.URL, Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	sum, err := c.Diffs.MaterialSummary(context.Background(), "100", "101")
	if err != nil {
		t.Fatal(err)
	}
	if !seen["/api/diffs/100/101/devices?count"] || !seen["/api/diffs/100/101/routing-loop/count?"] || !seen["/api/diffs/100/101/files?count"] || !seen["/api/diffs/100/101/checks?counts"] {
		t.Fatalf("paths hit: %v", seen)
	}
	if sum.Counts["interfaces"] != 2 || len(sum.Files) != 0 {
		t.Fatalf("expected only the interface change to count (state files are churn): %+v", sum)
	}
	if !sum.Material() {
		t.Fatal("2 interface diffs are material")
	}
	if len(sum.Incomplete) != 1 || sum.Incomplete[0] != "routing-loop/count" {
		t.Fatalf("incomplete=%v", sum.Incomplete)
	}
	// Only churn => not material.
	sum2 := MaterialDiffSummary{Counts: map[string]int64{}, Files: map[string]int64{}}
	if sum2.Material() {
		t.Fatal("empty summary must not be material")
	}
}

func TestDiffsMaterialSummaryCountsConfigFiles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/diffs/1/2/files" {
			_, _ = w.Write([]byte(`{"count":1,"complete":true,"types":{"CONFIG":1}}`))
			return
		}
		if r.URL.Path == "/api/diffs/1/2/checks" {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		_, _ = w.Write([]byte(`{"count":0,"complete":true}`))
	}))
	defer server.Close()
	c, _ := NewClient(Config{BaseURL: server.URL, Username: "u", Password: "p"})
	sum, err := c.Diffs.MaterialSummary(context.Background(), "1", "2")
	if err != nil {
		t.Fatal(err)
	}
	if sum.Files["CONFIG"] != 1 || !sum.Material() {
		t.Fatalf("a config file diff is material: %+v", sum)
	}
}
