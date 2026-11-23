package forward

import (
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
)

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// The NQE check diff answers at once with PROCESSING rows; WaitChecks must keep reading until none is, and a budget must end the wait
// with ErrPollBudget (not the caller's own deadline error) while handing back the last rows.
func TestDiffsWaitChecks(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/diffs/10/11/checks" || r.URL.Query().Get("type") != "NQE" {
			t.Errorf("unexpected %s", r.URL)
		}
		if calls.Add(1) < 3 {
			_, _ = w.Write([]byte(`[{"diffType":"PROCESSING","a":{"id":"1","name":"c","status":"PROCESSING"}}]`))
			return
		}
		_, _ = w.Write([]byte(`[{"diffType":"MODIFIED","a":{"id":"1","name":"c","status":"PASS"},"b":{"id":"1","name":"c","status":"FAIL"}}]`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	rows, _, err := client.Diffs.WaitChecks(context.Background(), "10", "11", "NQE", ports.PollOptions[[]domain.CheckDiffEntry]{Interval: time.Millisecond, MaxInterval: 4 * time.Millisecond})
	if err != nil || len(rows) != 1 || rows[0].DiffType != "MODIFIED" || calls.Load() != 3 {
		t.Fatalf("rows=%+v err=%v calls=%d", rows, err, calls.Load())
	}

	// Never finishes: the budget ends it.
	stuck := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"diffType":"PROCESSING"}]`))
	}))
	defer stuck.Close()
	rows, _, err = newTestClient(t, stuck.URL).Diffs.WaitChecks(context.Background(), "10", "11", "NQE", ports.PollOptions[[]domain.CheckDiffEntry]{Interval: time.Millisecond, Budget: 60 * time.Millisecond})
	if !errors.Is(err, domain.ErrPollBudget) || len(rows) != 1 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}

	// The caller's own cancellation stays the caller's error.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err = newTestClient(t, stuck.URL).Diffs.WaitChecks(ctx, "10", "11", "NQE", ports.PollOptions[[]domain.CheckDiffEntry]{}); !errors.Is(err, context.Canceled) || errors.Is(err, domain.ErrPollBudget) {
		t.Fatalf("err=%v", err)
	}
}

// Forward reads a null or absent field as "keep", so a patch must send tags as null when unset, as a list when set, and only the fields
// the caller named; an empty patch is refused before any request.
func TestChecksUpdate(t *testing.T) {
	var gotMethod, gotPath string
	var body map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		body = nil
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{"id":"287072","name":"renamed","status":"PASS"}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	name := "renamed"
	check, _, err := client.Checks.Update(context.Background(), "9", "287072", domain.CheckPatch{Name: &name})
	if err != nil || check.Name != "renamed" || string(check.ID) != "287072" {
		t.Fatalf("check=%+v err=%v", check, err)
	}
	if gotMethod != http.MethodPatch || gotPath != "/api/snapshots/9/checks/287072" {
		t.Fatalf("request %s %s", gotMethod, gotPath)
	}
	if string(body["name"]) != `"renamed"` || string(body["tags"]) != "null" || len(body) != 2 {
		t.Fatalf("body = %v", body)
	}
	if _, _, err = client.Checks.Update(context.Background(), "9", "287072", domain.CheckPatch{Tags: []string{}}); err != nil || string(body["tags"]) != "[]" {
		t.Fatalf("an empty non-nil tag list must clear: %v %v", body, err)
	}
	if _, _, err = client.Checks.Update(context.Background(), "9", "287072", domain.CheckPatch{}); err == nil {
		t.Fatal("an empty patch must be refused")
	}
}
