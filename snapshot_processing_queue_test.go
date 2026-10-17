package forward

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The org's snapshot processing queue is {"entries": [...]} with the snapshot, when it was queued and whether it is a reprocessing; an
// empty queue must read as an empty list, not an error.
func TestSnapshotsProcessingQueue(t *testing.T) {
	t.Parallel()

	bodies := []string{
		`{"entries":[{"snapshotId":"9001","createdAt":"2026-10-17T01:00:00Z","isReprocessing":false},{"snapshotId":"9002","createdAt":"2026-10-17T01:05:00Z","isReprocessing":true}]}`,
		`{"entries":[]}`,
	}
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/snapshot-processing-queue" {
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
		_, _ = w.Write([]byte(bodies[calls]))
		calls++
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	queue, _, err := client.Snapshots.ProcessingQueue(context.Background())
	if err != nil || len(queue) != 2 || queue[0].SnapshotID != "9001" || queue[0].IsReprocessing || !queue[1].IsReprocessing || queue[1].CreatedAt.UTC().Format("2006-01-02T15:04:05Z") != "2026-10-17T01:05:00Z" {
		t.Fatalf("%v %+v", err, queue)
	}
	queue, _, err = client.Snapshots.ProcessingQueue(context.Background())
	if err != nil || len(queue) != 0 {
		t.Fatalf("empty queue: %v %+v", err, queue)
	}
}
