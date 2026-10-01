package forward

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Collection logs stream whole files, so the read is capped and a truncated
// read is not an error. A backfill log is per device, so it needs a name, and
// that is refused before anything is sent.
func TestSnapshotsLogs(t *testing.T) {
	t.Parallel()

	var query string
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.EscapedPath() != "/api/snapshots/9164/logs" {
			t.Errorf("path = %s", r.URL.EscapedPath())
		}
		query = r.URL.RawQuery
		_, _ = io.WriteString(w, strings.Repeat("2026-10-01 INFO collected leaf-1\n", 1000))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()

	var buf bytes.Buffer
	n, truncated, _, err := client.Snapshots.Logs(ctx, "9164", SnapshotLogOptions{DeviceName: "leaf-1", Level: "WARN"}, 100, &buf)
	if err != nil || !truncated || n != 100 || buf.Len() != 100 || query != "deviceName=leaf-1&level=WARN" {
		t.Fatalf("Logs() = %d, %v, %v; query %q", n, truncated, err, query)
	}
	buf.Reset()
	if _, _, _, err := client.Snapshots.Logs(ctx, "9164", SnapshotLogOptions{DeviceName: "leaf-1", Backfilled: true}, 1<<20, &buf); err != nil || query != "backfilled=true&deviceName=leaf-1" {
		t.Fatalf("backfilled: query %q, %v", query, err)
	}
	before := calls
	if _, _, _, err := client.Snapshots.Logs(ctx, "9164", SnapshotLogOptions{Backfilled: true}, 100, &buf); err == nil || calls != before {
		t.Fatalf("a backfill log without a device must be refused locally (err=%v)", err)
	}
	if _, _, _, err := client.Snapshots.Logs(ctx, "9164", SnapshotLogOptions{}, 0, &buf); err == nil {
		t.Fatal("a zero cap must be refused")
	}
}

// The CLI execution log and the plain-text exception report are read the same
// capped way; an empty file writes nothing and is not an error.
func TestSnapshotsCollectionLogAndExceptionsText(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/api/snapshots/9164/collection-log":
			// Forward returns an empty body when the snapshot has no CLI log.
		case "/api/snapshots/9164/exceptions":
			if r.URL.RawQuery != "" {
				t.Errorf("the text report has no view parameter: %q", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, "Device exceptions:\n\nleaf-1\njava.lang.IllegalStateException: bad config\n\tat Parser.parse(Parser.java:10)\n\n")
		default:
			t.Errorf("unexpected %s", r.URL.RequestURI())
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	var log bytes.Buffer
	if n, truncated, _, err := client.Snapshots.CollectionLog(context.Background(), "9164", 1<<20, &log); err != nil || n != 0 || truncated {
		t.Fatalf("empty CollectionLog() = %d, %v, %v", n, truncated, err)
	}
	var report bytes.Buffer
	if _, truncated, _, err := client.Snapshots.ExceptionsText(context.Background(), "9164", 1<<20, &report); err != nil || truncated ||
		!strings.Contains(report.String(), "leaf-1\njava.lang.IllegalStateException") {
		t.Fatalf("ExceptionsText() = %q, %v, %v", report.String(), truncated, err)
	}
}

// The shape is Forward's own SnapshotCollectionMetricTest fixture plus the
// DeviceStats fields it leaves out: times are epoch milliseconds, durations
// milliseconds, and error merges the collection and processing errors.
func TestSnapshotsCollectionMetrics(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/networks/n1/collection-metrics" || r.URL.Query().Get("snapshotId") != "1" {
			t.Errorf("request = %s", r.URL.RequestURI())
		}
		_, _ = io.WriteString(w, `{"snapshotId":"1","snapshotTime":1,"collectionStartTime":2,"collectionEndTime":3,"metrics":[
		  {"deviceName":"dev1","deviceType":"SWITCH","sourceType":"CLASSIC","collectionDuration":49,"collectionStartTime":123,"error":"PARSER_EXCEPTION"},
		  {"deviceName":"dev2","deviceType":"ROUTER","collectionDuration":61000,"slowestCommand":"show running-config","slowestCommandDuration":40000,"jumpServer":"J1","connTypeDisplayName":"Cisco IOS-XE"}]}`)
	}))
	defer server.Close()

	got, _, err := newTestClient(t, server.URL).Snapshots.CollectionMetrics(context.Background(), "n1", "1")
	if err != nil || got.SnapshotID != "1" || *got.CollectionEndTimeMillis != 3 || len(got.Devices) != 2 {
		t.Fatalf("CollectionMetrics() = %+v, %v", got, err)
	}
	failed, slow := got.Devices[0], got.Devices[1]
	if failed.Error != "PARSER_EXCEPTION" || *failed.CollectionDurationMillis != 49 || *failed.CollectionStartTimeMillis != 123 {
		t.Fatalf("dev1 = %+v", failed)
	}
	if slow.SlowestCommand != "show running-config" || *slow.SlowestCommandDurationMillis != 40000 || slow.JumpServer != "J1" || slow.Error != "" || slow.CollectionStartTimeMillis != nil {
		t.Fatalf("dev2 = %+v", slow)
	}
}

// DedupedCollectionExceptions: one entry per distinct stack trace, with a
// total and a first page of occurrences; a deviceless occurrence has no name.
func TestSnapshotsCollectionExceptions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.EscapedPath() != "/api/collector/exceptions" || q.Get("snapshotId") != "9164" || q.Get("limit") != "50" || q.Get("occurrencesLimit") != "3" {
			t.Errorf("request = %s", r.URL.RequestURI())
		}
		_, _ = io.WriteString(w, `{"actualNumDedupedExceptions":2,"collectorIdToName":{"C1":"lab-collector"},"dedupedExceptions":[
		  {"stackTrace":"java.net.SocketTimeoutException: Read timed out","collectorVersion":"26.9.0-18","actualNumOccurrences":12,
		   "occurrences":[{"collectorId":"C1","deviceName":"leaf-1","collectorTaskId":"P1021","timestamp":"2026-10-01T10:00:00Z"},
		                  {"collectorId":"C1","collectorTaskId":"P1021","timestamp":"2026-10-01T10:00:01Z"}]}]}`)
	}))
	defer server.Close()

	limit, occurrences := int32(50), int32(3)
	got, _, err := newTestClient(t, server.URL).Snapshots.CollectionExceptions(context.Background(), " 9164 ", CollectionExceptionOptions{Limit: &limit, OccurrencesLimit: &occurrences})
	if err != nil || got.Total != 2 || got.CollectorIDToName["C1"] != "lab-collector" || len(got.Exceptions) != 1 {
		t.Fatalf("CollectionExceptions() = %+v, %v", got, err)
	}
	e := got.Exceptions[0]
	if e.TotalOccurrences != 12 || e.CollectorVersion != "26.9.0-18" || len(e.Occurrences) != 2 || e.Occurrences[0].DeviceName != "leaf-1" || e.Occurrences[1].DeviceName != "" {
		t.Fatalf("exception = %+v", e)
	}
	if _, _, err := newTestClient(t, server.URL).Snapshots.CollectionExceptions(context.Background(), "", CollectionExceptionOptions{}); err == nil {
		t.Fatal("an empty snapshot ID must be refused")
	}
}
