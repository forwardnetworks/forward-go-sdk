package forward

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestJobsListActiveParsesForwardsJSON pins ListActive to GET /api/jobs/active and to the field names Forward's
// ActiveJobInfo record actually serializes (jobType, networkId, longestQueuedTimeInSeconds, cancelLink, ...), not
// a guessed shape.
func TestJobsListActiveParsesForwardsJSON(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/jobs/active" {
			http.Error(w, "No endpoint "+r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`[{
			"jobType": "NQE_EXECUTION",
			"orgName": "WellsFargo",
			"networkId": 4610,
			"snapshotId": 13951,
			"creationTime": "2026-10-08T09:32:00Z",
			"waitingForOtherJobs": 0,
			"queued": 0,
			"running": 1,
			"longestQueuedTimeInSeconds": 5,
			"longestRunningTimeInSeconds": 7320,
			"durationInSeconds": 7325,
			"totalCount": 1,
			"cancelLink": "QkFTRTY0LUpPQi1JRA=="
		}]`))
	}))
	defer server.Close()
	client, err := NewClient(Config{HTTPClient: privateHTTPClient(), BaseURL: server.URL, Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	jobs, _, err := client.Jobs.ListActive(context.Background())
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("len(jobs) = %d, want 1", len(jobs))
	}
	got := jobs[0]
	if got.JobType != "NQE_EXECUTION" || got.NetworkID != 4610 {
		t.Fatalf("job = %#v, want JobType=NQE_EXECUTION NetworkID=4610", got)
	}
	if got.SnapshotID == nil || *got.SnapshotID != 13951 {
		t.Fatalf("SnapshotID = %v, want 13951", got.SnapshotID)
	}
	if got.LongestRunningTimeSeconds != 7320 {
		t.Fatalf("LongestRunningTimeSeconds = %d, want 7320", got.LongestRunningTimeSeconds)
	}
	if got.CancelLink != "QkFTRTY0LUpPQi1JRA==" {
		t.Fatalf("CancelLink = %q", got.CancelLink)
	}
	if got.CreationTime.IsZero() {
		t.Fatal("CreationTime did not parse")
	}
}

// TestJobsListCompletedParsesState pins ListCompleted to GET /api/jobs/completed and confirms the terminal
// state string (SUCCEEDED/FAILED/CANCELED/TIMEDOUT) round-trips as-is.
func TestJobsListCompletedParsesState(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/jobs/completed" {
			http.Error(w, "No endpoint "+r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`[{
			"jobType": "SNAPSHOT_PROCESSING",
			"orgName": "WellsFargo",
			"networkId": 4610,
			"snapshotId": 13950,
			"creationTime": "2026-10-08T08:00:00Z",
			"state": "TIMEDOUT",
			"earliestStartTime": "2026-10-08T08:00:05Z",
			"latestEndTime": "2026-10-08T08:20:05Z",
			"longestQueueTimeInSeconds": 5,
			"longestRunningTimeInSeconds": 1200,
			"durationInSeconds": 1205,
			"count": 1
		}]`))
	}))
	defer server.Close()
	client, err := NewClient(Config{HTTPClient: privateHTTPClient(), BaseURL: server.URL, Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	jobs, _, err := client.Jobs.ListCompleted(context.Background())
	if err != nil {
		t.Fatalf("ListCompleted: %v", err)
	}
	if len(jobs) != 1 || jobs[0].State != "TIMEDOUT" {
		t.Fatalf("jobs = %#v, want one job with state TIMEDOUT", jobs)
	}
	if jobs[0].EarliestStartTime.IsZero() || jobs[0].LatestEndTime.IsZero() {
		t.Fatal("start/end time did not parse")
	}
}

// TestJobsCancelSendsLinkVerbatim pins Cancel to DELETE /api/jobs/{cancelLink} with the cancelLink reaching the
// wire byte-for-byte unchanged. A real cancelLink is ALREADY percent-encoded by Forward for direct path
// insertion (Forward's own GUI does exactly `/jobs/${cancelLink}`, no further encoding) -- it contains literal
// "%2F"/"%3D" text, not raw "/" or "=". r.URL.Path is decoded by net/http and would read the same either way, so
// this asserts on r.URL.EscapedPath() (and the raw RequestURI), the only way to catch the SDK re-escaping an
// already-escaped "%" into "%25..." and corrupting it (Forward then answers 400, not the test's 200).
func TestJobsCancelSendsLinkVerbatim(t *testing.T) {
	t.Parallel()
	const link = "IgUI%2F2wQGg%3D%3D" // a real shape: base64 bytes containing '/' and '==', pre-escaped by Forward
	var gotEscapedPath, gotRequestURI, gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEscapedPath, gotRequestURI, gotMethod = r.URL.EscapedPath(), r.RequestURI, r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client, err := NewClient(Config{HTTPClient: privateHTTPClient(), BaseURL: server.URL, Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Jobs.Cancel(context.Background(), link); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("method = %q, want DELETE", gotMethod)
	}
	if want := "/api/jobs/" + link; gotEscapedPath != want {
		t.Fatalf("wire path = %q, want %q (cancelLink must reach Forward byte-for-byte, not re-escaped)", gotEscapedPath, want)
	}
	if want := "/api/jobs/" + link; gotRequestURI != want {
		t.Fatalf("RequestURI = %q, want %q", gotRequestURI, want)
	}
}

func TestJobsCancelRequiresLink(t *testing.T) {
	t.Parallel()
	client, err := NewClient(Config{HTTPClient: privateHTTPClient(), BaseURL: "http://127.0.0.1:0", Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Jobs.Cancel(context.Background(), "  "); err == nil {
		t.Fatal("Cancel(\"\") error = nil, want an error")
	}
}

// TestJobsCancelNotFound pins the 404 case (job already finished or already canceled) to a plain error, not a
// panic or a silently-ignored success -- JobsController.cancelJob maps CANCELATION_STATUS_NOT_FOUND to 404.
func TestJobsCancelNotFound(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	client, err := NewClient(Config{HTTPClient: privateHTTPClient(), BaseURL: server.URL, Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Jobs.Cancel(context.Background(), "gone"); !IsStatus(err, http.StatusNotFound) {
		t.Fatalf("Cancel() error = %v, want 404", err)
	}
}

// A client-side wait does not stop Forward's compute (a Predict's modeling thread kept running for hours after the client gave up) and
// Forward has no snapshot-level cancel route, so the stop is the admin job cancel: read the active job rows, cancel those of ONE
// snapshot with the cancelLink exactly as Forward gave it (already percent-encoded, never escaped again), tolerate a job that finished
// meanwhile (404), report the rest, and never touch another snapshot's jobs.
func TestJobsCancelForSnapshot(t *testing.T) {
	t.Parallel()

	var deleted []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/jobs/active":
			_, _ = w.Write([]byte(`[
				{"jobType":"Protocol Modeling","orgName":"o","networkId":5,"snapshotId":9001,"cancelLink":"AbC%2FdE%3D","totalCount":1},
				{"jobType":"NQE Query","orgName":"o","networkId":5,"snapshotId":9001,"cancelLink":"Gone%3D","totalCount":1},
				{"jobType":"Protocol Modeling","orgName":"o","networkId":5,"snapshotId":9002,"cancelLink":"Other%3D","totalCount":1},
				{"jobType":"Flowlet generation","orgName":"o","networkId":5,"cancelLink":"NoSnap%3D","totalCount":1}]`))
		case r.Method == http.MethodDelete:
			deleted = append(deleted, r.URL.EscapedPath())
			if strings.HasSuffix(r.URL.EscapedPath(), "Gone%3D") {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"apiUrl":"/api/jobs/Gone%3D","httpMethod":"DELETE","message":"gone"}`))
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	result, err := client.Jobs.CancelForSnapshot(context.Background(), "9001")
	if err != nil {
		t.Fatal(err)
	}
	if result.Matched != 2 || result.Canceled != 1 || result.AlreadyDone != 1 {
		t.Fatalf("result %+v", result)
	}
	if strings.Join(deleted, "|") != "/api/jobs/AbC%2FdE%3D|/api/jobs/Gone%3D" {
		t.Fatalf("cancelled %v: only snapshot 9001's rows, links unescaped a second time", deleted)
	}
	none, err := client.Jobs.CancelForSnapshot(context.Background(), "424242")
	if err != nil || none.Matched != 0 || len(deleted) != 2 {
		t.Fatalf("a snapshot with no active job matches nothing and is not an error: %v %+v", err, none)
	}
	if _, err := client.Jobs.CancelForSnapshot(context.Background(), "latest"); err == nil {
		t.Fatal("a non-numeric snapshot id must be refused")
	}
}

// One row failing must not stop the others, a 404 that means the route is not served is not "already done", and a cancelled context
// stops the loop instead of repeating the same error per row.
func TestJobsCancelForSnapshotFailureModes(t *testing.T) {
	t.Parallel()

	var deleted []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[{"jobType":"A","snapshotId":7,"cancelLink":"Boom%3D"},{"jobType":"B","snapshotId":7,"cancelLink":"NotServed%3D"},{"jobType":"C","snapshotId":7,"cancelLink":"Fine%3D"}]`))
			return
		}
		deleted = append(deleted, r.URL.EscapedPath())
		switch {
		case strings.HasSuffix(r.URL.EscapedPath(), "Boom%3D"):
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"boom"}`))
		case strings.HasSuffix(r.URL.EscapedPath(), "NotServed%3D"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"apiUrl":"/api/jobs/NotServed%3D","httpMethod":"DELETE","message":"No endpoint DELETE /api/jobs/NotServed%3D."}`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	result, err := client.Jobs.CancelForSnapshot(context.Background(), "7")
	if err == nil || result.Matched != 3 || result.Canceled != 1 || result.AlreadyDone != 0 || len(deleted) != 3 {
		t.Fatalf("a failing row and an unserved route are errors and the rest still run: %v %+v %v", err, result, deleted)
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := client.Jobs.CancelForSnapshot(cancelled, "7"); err == nil {
		t.Fatal("a cancelled context must be an error")
	}
}
