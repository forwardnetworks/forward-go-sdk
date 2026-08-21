package forward

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestChecksListFilters(t *testing.T) {
	t.Parallel()

	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		_, _ = io.WriteString(w, `[{"id":"chk-1","name":"reach","status":"FAIL","enabled":true,
		  "numViolations":3,"definition":{"checkType":"Existential"}}]`)
	}))
	defer server.Close()

	checks, _, err := newTestClient(t, server.URL).Checks.List(context.Background(), "snap-1", CheckListOptions{
		Types:      []string{"Existential", "  "},
		Statuses:   []string{"FAIL"},
		Priorities: []string{"HIGH"},
	})
	if err != nil {
		t.Fatalf("Checks.List() error = %v", err)
	}
	// A blank entry contributes no parameter: it is not a filter that matches nothing.
	if query != "priority=HIGH&status=FAIL&type=Existential" {
		t.Fatalf("query = %q", query)
	}
	if len(checks) != 1 || checks[0].Enabled == nil || !*checks[0].Enabled {
		t.Fatalf("checks = %#v", checks)
	}
	if string(checks[0].Definition) == "" {
		t.Fatal("definition was dropped")
	}
}

// An unstated Enabled has to stay unstated, or a caller reconciling declared
// configuration cannot tell "leave it alone" from "disable it".
func TestChecksListLeavesUnstatedEnabledNil(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"id":"chk-1","name":"reach","status":"PASS"}]`)
	}))
	defer server.Close()

	checks, _, err := newTestClient(t, server.URL).Checks.List(context.Background(), "snap-1")
	if err != nil {
		t.Fatalf("Checks.List() error = %v", err)
	}
	if len(checks) != 1 || checks[0].Enabled != nil || checks[0].PerfMonitoringEnabled != nil {
		t.Fatalf("checks = %#v", checks)
	}
}

func TestChecksGetCreateAndDeactivate(t *testing.T) {
	t.Parallel()

	var created NewCheck
	var persistentQuery string
	deleted := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/snapshots/snap-1/checks/chk-1":
			_, _ = io.WriteString(w, `{"id":"chk-1","name":"reach","status":"FAIL","diagnosis":{
			  "summary":"3 flows blocked","detailsIncomplete":true,
			  "details":[{"query":"q","references":[{"key":"device","value":"fw1",
			    "files":{"running-config":[{"start":10,"end":12}]}}]}]}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/snapshots/snap-1/checks":
			persistentQuery = r.URL.Query().Get("persistent")
			if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
				t.Errorf("decode create: %v", err)
			}
			_, _ = io.WriteString(w, `{"id":"chk-2","name":"reach","status":"PASS","enabled":false}`)
		case r.Method == http.MethodDelete:
			deleted[r.URL.Path] = true
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	ctx := context.Background()

	detail, _, err := client.Checks.Get(ctx, "snap-1", "chk-1")
	if err != nil {
		t.Fatalf("Checks.Get() error = %v", err)
	}
	if detail.Diagnosis == nil || detail.Diagnosis.Summary != "3 flows blocked" {
		t.Fatalf("diagnosis = %#v", detail.Diagnosis)
	}
	if got := detail.Diagnosis.Details[0].References[0].Files["running-config"][0]; *got.Start != 10 || *got.End != 12 {
		t.Fatalf("line range = %#v", got)
	}
	if detail.Diagnosis.DetailsIncomplete == nil || !*detail.Diagnosis.DetailsIncomplete {
		t.Fatal("detailsIncomplete was dropped, so a sample would read as the whole set")
	}

	got, _, err := client.Checks.Create(ctx, "snap-1", NewCheck{
		Name:       "reach",
		Definition: map[string]any{"checkType": "Existential"},
		Enabled:    Ptr(false),
	}, Ptr(false))
	if err != nil {
		t.Fatalf("Checks.Create() error = %v", err)
	}
	if persistentQuery != "false" {
		t.Fatalf("persistent query = %q", persistentQuery)
	}
	if created.Enabled == nil || *created.Enabled {
		t.Fatalf("enabled=false did not reach the server: %#v", created.Enabled)
	}
	if got.ID != "chk-2" {
		t.Fatalf("created = %#v", got)
	}

	if _, err := client.Checks.Deactivate(ctx, "snap-1", "chk-1"); err != nil {
		t.Fatalf("Checks.Deactivate() error = %v", err)
	}
	if _, err := client.Checks.DeactivateAll(ctx, "snap-1"); err != nil {
		t.Fatalf("Checks.DeactivateAll() error = %v", err)
	}
	if !deleted["/api/snapshots/snap-1/checks/chk-1"] || !deleted["/api/snapshots/snap-1/checks"] {
		t.Fatalf("deleted = %#v", deleted)
	}
}

// Omitting persistent must send no parameter at all, so the appserver applies
// its own default rather than one asserted here.
func TestChecksCreateOmitsUnstatedPersistent(t *testing.T) {
	t.Parallel()

	var raw string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"id":"chk-2","name":"reach"}`)
	}))
	defer server.Close()

	_, _, err := newTestClient(t, server.URL).Checks.Create(context.Background(), "snap-1", NewCheck{
		Name: "reach", Definition: map[string]any{"checkType": "Existential"},
	}, nil)
	if err != nil {
		t.Fatalf("Checks.Create() error = %v", err)
	}
	if raw != "" {
		t.Fatalf("query = %q, want none", raw)
	}
}

func TestCloudAccountsGet(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"type":"AWS","name":"prod"},{"type":"AZURE","name":"lab"}]`)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	account, _, err := client.CloudAccounts.Get(context.Background(), "net-1", "lab")
	if err != nil {
		t.Fatalf("CloudAccounts.Get() error = %v", err)
	}
	if account.Type != "AZURE" {
		t.Fatalf("account = %#v", account)
	}

	_, _, err = client.CloudAccounts.Get(context.Background(), "net-1", "absent")
	// Matchable, because a read that finds nothing means "gone from state"
	// while a transport failure means "try again".
	if !errors.Is(err, ErrCloudAccountNotFound) {
		t.Fatalf("Get(absent) error = %v, want ErrCloudAccountNotFound", err)
	}
}

func TestSnapshotsCreateAndLatestCollected(t *testing.T) {
	t.Parallel()

	var note SnapshotCreateRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/networks/net-1/snapshots":
			if err := json.NewDecoder(r.Body).Decode(&note); err != nil {
				t.Errorf("decode create: %v", err)
			}
			_, _ = io.WriteString(w, `{"id":"snap-9","state":"IN_PROGRESS"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/networks/net-1/snapshots":
			// Newest first, and the newest two are predictions.
			// processingTrigger is what a current appserver actually sets;
			// the other markers cover the routes and builds that do not.
			_, _ = io.WriteString(w, `{"snapshots":[
			  {"id":"snap-9","state":"PROCESSED","processingTrigger":"PREDICT"},
			  {"id":"snap-8","state":"PROCESSED","changeSetId":"CHG-1"},
			  {"id":"snap-7","state":"IN_PROGRESS"},
			  {"id":"snap-6","state":"PROCESSED","processingTrigger":"COLLECTION"}]}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	ctx := context.Background()

	created, _, err := client.Snapshots.Create(ctx, "net-1", SnapshotCreateRequest{Note: "nightly"})
	if err != nil {
		t.Fatalf("Snapshots.Create() error = %v", err)
	}
	if note.Note != "nightly" || created.ID != "snap-9" {
		t.Fatalf("created = %#v, note = %#v", created, note)
	}

	latest, _, err := client.Snapshots.LatestCollected(ctx, "net-1")
	if err != nil {
		t.Fatalf("Snapshots.LatestCollected() error = %v", err)
	}
	// snap-9 is predicted and snap-8 belongs to a change set; predicting from
	// either is refused, so the baseline is the newest collected one.
	if latest.ID != "snap-6" {
		t.Fatalf("latest collected = %s, want snap-6", latest.ID)
	}
}

func TestSnapshotsLatestCollectedWithoutOne(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"snapshots":[{"id":"snap-9","state":"PROCESSED","processingTrigger":"PREDICT"}]}`)
	}))
	defer server.Close()

	_, _, err := newTestClient(t, server.URL).Snapshots.LatestCollected(context.Background(), "net-1")
	if !errors.Is(err, ErrNoSnapshots) {
		t.Fatalf("LatestCollected() error = %v, want ErrNoSnapshots", err)
	}
}

func TestSnapshotsOperationWaitsForProcessing(t *testing.T) {
	t.Parallel()

	states := []string{"IN_PROGRESS", "IN_PROGRESS", "PROCESSED"}
	var index int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := states[min(index, len(states)-1)]
		index++
		_, _ = io.WriteString(w, `{"id":"snap-9","state":"`+state+`"}`)
	}))
	defer server.Close()

	poller, _, err := newTestClient(t, server.URL).Snapshots.Operation(context.Background(), "net-1", "snap-9")
	if err != nil {
		t.Fatalf("Snapshots.Operation() error = %v", err)
	}
	final, _, err := poller.Wait(context.Background(), PollOptions[Snapshot]{Interval: time.Millisecond})
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if final.State != "PROCESSED" {
		t.Fatalf("final state = %s", final.State)
	}
}

func TestSnapshotsOperationFailsOnTerminalFailure(t *testing.T) {
	t.Parallel()

	states := []string{"IN_PROGRESS", "FAILED"}
	var index int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := states[min(index, len(states)-1)]
		index++
		_, _ = io.WriteString(w, `{"id":"snap-9","state":"`+state+`"}`)
	}))
	defer server.Close()

	poller, _, err := newTestClient(t, server.URL).Snapshots.Operation(context.Background(), "net-1", "snap-9")
	if err != nil {
		t.Fatalf("Snapshots.Operation() error = %v", err)
	}
	// A terminal failure must not read as "still waiting" and must not read as success.
	if _, _, err := poller.Wait(context.Background(), PollOptions[Snapshot]{Interval: time.Millisecond}); err == nil {
		t.Fatal("Wait() returned no error for a FAILED snapshot")
	}
}

// Pinned to a response captured from a running appserver. The fields a check
// actually carries are easy to get wrong from the payload that creates one:
// timestamps are RFC 3339 instants under createdAt/definedAt/executedAt, not
// the millisecond fields the create side suggests, and numViolations is absent
// entirely for a check that passed.
func TestChecksDecodeCollectedShape(t *testing.T) {
	t.Parallel()

	const captured = `[{"id":"140",
	  "definition":{"predefinedCheckType":"VLAN_CONSISTENCY","checkType":"Predefined"},
	  "enabled":true,"priority":"NOT_SET","name":"VLAN Consistency",
	  "createdAt":"2026-08-18T13:07:33.391Z","creatorId":"101","creator":"dev",
	  "definedAt":"2026-08-18T13:07:33.391Z","outdated":false,"status":"PASS",
	  "description":"VLANs should be consistently defined.",
	  "executedAt":"2026-08-21T13:43:16.197120957Z","executionDurationMillis":14}]`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, captured)
	}))
	defer server.Close()

	checks, _, err := newTestClient(t, server.URL).Checks.List(context.Background(), "539")
	if err != nil {
		t.Fatalf("Checks.List() error = %v", err)
	}
	if len(checks) != 1 {
		t.Fatalf("checks = %#v", checks)
	}
	got := checks[0]
	if got.CreatedAt != "2026-08-18T13:07:33.391Z" || got.DefinedAt == "" || got.ExecutedAt == "" {
		t.Fatalf("timestamps dropped: %#v", got)
	}
	if got.ExecutionDurationMS == nil || *got.ExecutionDurationMS != 14 {
		t.Fatalf("executionDurationMillis = %v", got.ExecutionDurationMS)
	}
	if got.Creator != "dev" || got.CreatorID != "101" || got.Description == "" {
		t.Fatalf("authorship dropped: %#v", got)
	}
	if got.Outdated == nil || *got.Outdated {
		t.Fatalf("outdated = %v", got.Outdated)
	}
	// Absent, not zero: a passing check reports no violation count at all.
	if got.NumViolations != nil {
		t.Fatalf("numViolations = %v, want nil", *got.NumViolations)
	}
}

// Some appserver builds have no per-snapshot metadata route and answer "No
// endpoint GET ..." with a 404. Get has to survive that, because the bare
// /api/snapshots/{id} route is not an alternative -- it serves the exported
// ZIP, not metadata.
func TestSnapshotsGetFallsBackToListing(t *testing.T) {
	t.Parallel()

	var listed bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/networks/net-1/snapshots/555":
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"No endpoint GET /api/networks/net-1/snapshots/555."}`)
		case "/api/networks/net-1/snapshots":
			listed = true
			if r.URL.Query().Get("includeArchived") != "true" {
				t.Errorf("fallback listing did not include archived: %s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `{"snapshots":[
			  {"id":"556","state":"PROCESSED"},{"id":"555","state":"IN_PROGRESS"}]}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	snapshot, _, err := newTestClient(t, server.URL).Snapshots.Get(context.Background(), "net-1", "555")
	if err != nil {
		t.Fatalf("Snapshots.Get() error = %v", err)
	}
	if !listed || snapshot.ID != "555" || snapshot.State != "IN_PROGRESS" {
		t.Fatalf("snapshot = %#v (listed=%v)", snapshot, listed)
	}
}

func TestSnapshotsGetReportsAbsence(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/networks/net-1/snapshots" {
			_, _ = io.WriteString(w, `{"snapshots":[{"id":"556","state":"PROCESSED"}]}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, _, err := newTestClient(t, server.URL).Snapshots.Get(context.Background(), "net-1", "555")
	if !errors.Is(err, ErrSnapshotNotFound) {
		t.Fatalf("Get(absent) error = %v, want ErrSnapshotNotFound", err)
	}
}

// Where the metadata route exists it is used, without a second request.
func TestSnapshotsGetPrefersTheMetadataRoute(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/networks/net-1/snapshots/555" {
			t.Errorf("unexpected request %s", r.URL.RequestURI())
		}
		_, _ = io.WriteString(w, `{"id":"555","state":"PROCESSED"}`)
	}))
	defer server.Close()

	snapshot, _, err := newTestClient(t, server.URL).Snapshots.Get(context.Background(), "net-1", "555")
	if err != nil || snapshot.ID != "555" || calls != 1 {
		t.Fatalf("Get() = %#v, %v after %d calls", snapshot, err, calls)
	}
}
