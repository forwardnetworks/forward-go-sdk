package forward

import (
	"context"
	"net/http"
	"net/http/httptest"
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
