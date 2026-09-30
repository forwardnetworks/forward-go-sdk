package forward

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Forward's snapshot listing includes Predict forks, processed like any
// other snapshot. The newest PROCESSED row on a network that uses Predict is
// therefore often a prediction -- a state the network was never in -- so
// LatestProcessed must skip them, and must read the whole PROCESSED listing
// rather than a limit a run of predictions could exhaust.
func TestSnapshotsLatestProcessedSkipsPredictions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("state"); got != "PROCESSED" {
			t.Errorf("state = %q, want PROCESSED", got)
		}
		if r.URL.Query().Has("limit") {
			t.Errorf("a limit can hide the collected snapshot behind predictions: %s", r.URL.RawQuery)
		}
		_, _ = io.WriteString(w, `{"snapshots":[
		  {"id":"snap-9","state":"PROCESSED","processingTrigger":"PREDICT","parentSnapshotId":"snap-6"},
		  {"id":"snap-8","state":"PROCESSED","parentSnapshotId":"snap-6"},
		  {"id":"snap-6","state":"PROCESSED","processingTrigger":"COLLECTION"}]}`)
	}))
	defer server.Close()

	latest, _, err := newTestClient(t, server.URL).Snapshots.LatestProcessed(context.Background(), "net-1")
	if err != nil {
		t.Fatalf("LatestProcessed() error = %v", err)
	}
	if latest.ID != "snap-6" || latest.Predicted() {
		t.Fatalf("LatestProcessed() = %+v, want the collected snap-6", latest)
	}
}

func TestSnapshotsLatestProcessedWithOnlyPredictions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"snapshots":[{"id":"snap-9","state":"PROCESSED","processingTrigger":"PREDICT"}]}`)
	}))
	defer server.Close()

	if _, _, err := newTestClient(t, server.URL).Snapshots.LatestProcessed(context.Background(), "net-1"); !errors.Is(err, ErrNoSnapshots) {
		t.Fatalf("LatestProcessed() error = %v, want ErrNoSnapshots", err)
	}
}

// SnapshotProcessingInterceptor guards every @RequiresSnapshot handler, not
// just /api/snapshots, and ForwardHandlerExceptionResolver renders what it
// throws. The bodies below are what that resolver writes; the kind has to
// follow Forward's reason code on any route, and a snapshot that will never be
// ready must not look like one that is still processing.
func TestSnapshotReadinessErrorsAreClassifiedOnEveryRoute(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		status int
		body   string
		want   error
		kind   ErrorKind
	}{
		{
			name:   "snapshot still processing",
			status: http.StatusConflict,
			body:   `{"apiUrl":"/api/nqe","httpMethod":"POST","message":"Snapshot 1021 cannot be used at the moment (currently PROCESSING)","reason":"SNAPSHOT_UNAVAILABLE","snapshotId":1021,"snapshotState":"PROCESSING"}`,
			want:   ErrSnapshotNotProcessed,
			kind:   ErrorKindSnapshotNotProcessed,
		},
		{
			name:   "no snapshot given and the latest is not processed",
			status: http.StatusConflict,
			body:   `{"apiUrl":"/api/nqe","httpMethod":"POST","message":"No qualifying Snapshot found. The latest Snapshot, 1022, is currently PROCESSING.","reason":"NO_QUALIFYING_SNAPSHOT","latestSnapshotId":"1022","latestSnapshotState":"PROCESSING"}`,
			want:   ErrSnapshotNotProcessed,
			kind:   ErrorKindSnapshotNotProcessed,
		},
		{
			name:   "no snapshot given and the network has none",
			status: http.StatusConflict,
			body:   `{"apiUrl":"/api/nqe","httpMethod":"POST","message":"No qualifying Snapshot found","reason":"NO_QUALIFYING_SNAPSHOT"}`,
			want:   ErrNoSnapshots,
			kind:   ErrorKindNoSnapshots,
		},
		{
			name:   "processing failed",
			status: http.StatusBadRequest,
			body:   `{"apiUrl":"/api/nqe","httpMethod":"POST","message":"The snapshot you are attempting to access failed to process successfully.\nPlease contact your Forward representative for details."}`,
			want:   ErrSnapshotProcessingFailed,
			kind:   ErrorKindSnapshotProcessingFailed,
		},
		{
			name:   "processing canceled",
			status: http.StatusBadRequest,
			body:   `{"apiUrl":"/api/nqe","httpMethod":"POST","message":"Processing was canceled for Snapshot 1021."}`,
			want:   ErrSnapshotProcessingFailed,
			kind:   ErrorKindSnapshotProcessingFailed,
		},
		{
			name:   "processing timed out",
			status: http.StatusBadRequest,
			body:   `{"apiUrl":"/api/nqe","httpMethod":"POST","message":"Processing timed out for Snapshot 1021."}`,
			want:   ErrSnapshotProcessingFailed,
			kind:   ErrorKindSnapshotProcessingFailed,
		},
	}
	for _, tc := range cases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, tc.body)
		}))
		_, _, err := newTestClient(t, server.URL).NQE.Run(context.Background(), "net-1", "", NQEQueryRequest{Query: "select 1"})
		server.Close()
		var apiErr *ErrorResponse
		if !errors.As(err, &apiErr) || apiErr.Kind != tc.kind || !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v (kind %v), want %v", tc.name, err, kindOf(apiErr), tc.kind)
		}
		for _, other := range []error{ErrSnapshotNotProcessed, ErrSnapshotProcessingFailed, ErrNoSnapshots} {
			if other != tc.want && errors.Is(err, other) {
				t.Errorf("%s: also matches %v", tc.name, other)
			}
		}
	}
}

// The detail fields ride along so a caller can say which snapshot and state.
func TestSnapshotUnavailableCarriesSnapshotState(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"apiUrl":"/api/networks/net-1/paths","httpMethod":"GET","message":"Snapshot 1021 cannot be used at the moment (currently UNPROCESSED)","reason":"SNAPSHOT_UNAVAILABLE","snapshotId":1021,"snapshotState":"UNPROCESSED"}`)
	}))
	defer server.Close()

	_, err := newTestClient(t, server.URL).Raw.DoJSON(context.Background(), http.MethodGet, "/api/networks/net-1/paths", nil, nil)
	var apiErr *ErrorResponse
	if !errors.As(err, &apiErr) || !errors.Is(err, ErrSnapshotNotProcessed) {
		t.Fatalf("err = %v, want ErrSnapshotNotProcessed", err)
	}
	if apiErr.SnapshotID != "1021" || apiErr.SnapshotState != "UNPROCESSED" {
		t.Fatalf("snapshot = %q state = %q", apiErr.SnapshotID, apiErr.SnapshotState)
	}
}

func kindOf(apiErr *ErrorResponse) ErrorKind {
	if apiErr == nil {
		return ""
	}
	return apiErr.Kind
}

// The wire shape is Forward's own SnapshotMetricsTest fixture: durations are
// milliseconds and the failure maps are keyed by error name.
func TestSnapshotsMetricsDecodesForwardShape(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/snapshots/1021/metrics" {
			t.Errorf("path = %s", r.URL.EscapedPath())
		}
		_, _ = io.WriteString(w, `{"snapshotId":"1021","createdAt":"2026-09-29T10:00:00Z",
		  "numCollectionFailureDevices":1,"numProcessingFailureDevices":4,"numSuccessfulDevices":99,
		  "numCollectionFailureEndpoints":4,"numProcessingFailureEndpoints":8,"numSuccessfulEndpoints":99,
		  "deviceCollectionFailures":{"AUTHORIZATION_FAILED":2},
		  "deviceProcessingFailures":{"PARSER_EXCEPTION":2,"LICENSE_EXHAUSTED":1},
		  "endpointCollectionFailures":{"AUTHENTICATION_FAILED":4},
		  "endpointProcessingFailures":{"LICENSE_EXHAUSTED":4,"DUPLICATE":4},
		  "collectionDuration":345,"processingDuration":120000,
		  "l2IndexingStatus":"SUCCESS","hostComputationStatus":"SUCCESS","ipLocationIndexingStatus":"FAILURE",
		  "pathSearchIndexingStatus":"FAILURE","searchIndexingStatus":"CANCELED","snapshotState":"PROCESSED"}`)
	}))
	defer server.Close()

	m, _, err := newTestClient(t, server.URL).Snapshots.Metrics(context.Background(), " 1021 ")
	if err != nil {
		t.Fatalf("Metrics() error = %v", err)
	}
	if m.NumCollectionFailureDevices != 1 || m.DeviceCollectionFailures["AUTHORIZATION_FAILED"] != 2 ||
		m.DeviceProcessingFailures["PARSER_EXCEPTION"] != 2 || m.EndpointProcessingFailures["DUPLICATE"] != 4 {
		t.Fatalf("counts = %+v", m)
	}
	if m.CollectionDurationMillis == nil || *m.CollectionDurationMillis != 345 ||
		m.ProcessingDurationMillis == nil || *m.ProcessingDurationMillis != 120000 {
		t.Fatalf("durations = %v %v", m.CollectionDurationMillis, m.ProcessingDurationMillis)
	}
	if m.SearchIndexingStatus != "CANCELED" || m.SnapshotState != "PROCESSED" {
		t.Fatalf("statuses = %+v", m)
	}
}

// Only view=json is structured; the bare route returns stack traces as text.
func TestSnapshotsExceptionsReadsTheJSONView(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/snapshots/1021/exceptions" || r.URL.Query().Get("view") != "json" {
			t.Errorf("request = %s", r.URL.RequestURI())
		}
		_, _ = io.WriteString(w, `{"exceptions":[
		  {"stackTrace":"java.lang.IllegalStateException: x","occurrences":3,"exceptionType":"PARSING","devices":["leaf-1","leaf-2"]},
		  {"stackTrace":"java.lang.RuntimeException: y","occurrences":1,"exceptionType":"SNAPSHOT_GENERATION","devices":[]}]}`)
	}))
	defer server.Close()

	got, _, err := newTestClient(t, server.URL).Snapshots.Exceptions(context.Background(), "1021")
	if err != nil {
		t.Fatalf("Exceptions() error = %v", err)
	}
	if len(got) != 2 || got[0].ExceptionType != "PARSING" || got[0].Occurrences != 3 || len(got[0].Devices) != 2 {
		t.Fatalf("Exceptions() = %+v", got)
	}
	for _, id := range []string{"", "  "} {
		if _, _, err := newTestClient(t, server.URL).Snapshots.Exceptions(context.Background(), id); err == nil {
			t.Fatalf("Exceptions(%q) accepted a missing snapshot ID", id)
		}
	}
}
