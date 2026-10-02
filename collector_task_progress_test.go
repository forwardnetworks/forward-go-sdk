package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The series is parallel arrays keyed by timestamps: an idle gap followed by a
// burst reads as running staying near zero while queued stays high, then
// running jumping to the limit, and concurrencyLimits.global is the collector's
// effective concurrency (default applied), so no settings lookup is needed.
func TestCollectorTasksSnapshotProgress(t *testing.T) {
	t.Parallel()

	var uri string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uri = r.URL.RequestURI()
		_, _ = io.WriteString(w, `{"timestamps":["2026-10-02T08:00:00Z","2026-10-02T08:05:00Z",1759395000000],
		  "queued":[2600,2600,0],"running":[0,0,128],"succeeded":[0,0,0],"failed":[0,0,0],"timedOut":[0,0,0],"canceled":[0,0,0],
		  "concurrency":[0,0,128],"vcenters":[0,0,0],"jumpServers":{"js1":[0,0,20]},
		  "concurrencyLimits":{"global":128,"network":128,"vcenter":1,"jumpServer":20}}`)
	}))
	defer server.Close()

	got, _, err := newTestClient(t, server.URL).CollectorTasks.SnapshotProgress(context.Background(), " 1021 ")
	if err != nil || uri != "/api/collector-tasks?snapshotId=1021" {
		t.Fatalf("SnapshotProgress() = %v; sent %s", err, uri)
	}
	if len(got.Timestamps) != 3 || !got.Timestamps[1].Equal(time.Date(2026, 10, 2, 8, 5, 0, 0, time.UTC)) || !got.Timestamps[2].Equal(time.UnixMilli(1759395000000)) {
		t.Fatalf("timestamps (ISO and epoch millis) = %v", got.Timestamps)
	}
	if got.Queued[1] != 2600 || got.Running[1] != 0 || got.Running[2] != 128 || got.Concurrency[2] != 128 || got.JumpServers["js1"][2] != 20 {
		t.Fatalf("series = %+v", got)
	}
	if got.ConcurrencyLimits != (TaskConcurrencyLimits{Global: 128, Network: 128, VCenter: 1, JumpServer: 20}) {
		t.Fatalf("limits = %+v", got.ConcurrencyLimits)
	}
	if _, _, err := newTestClient(t, server.URL).CollectorTasks.SnapshotProgress(context.Background(), " "); err == nil {
		t.Fatal("an empty snapshot ID must be refused")
	}
}

// Forward answers an unknown snapshot with an empty series, not an error.
func TestCollectorTasksSnapshotProgressEmpty(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"timestamps":[],"queued":[],"running":[],"succeeded":[],"failed":[],"timedOut":[],"canceled":[],"concurrency":[],"vcenters":[],"jumpServers":{},"concurrencyLimits":{"global":128,"network":128,"vcenter":1,"jumpServer":128}}`)
	}))
	defer server.Close()

	got, _, err := newTestClient(t, server.URL).CollectorTasks.SnapshotProgress(context.Background(), "9")
	if err != nil || len(got.Timestamps) != 0 || got.ConcurrencyLimits.Global != 128 {
		t.Fatalf("empty series = %+v, %v", got, err)
	}
}

// The per-device status is what separates a timeout from a cancellation.
func TestCollectorTasksSubTasks(t *testing.T) {
	t.Parallel()

	var uris []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uris = append(uris, r.URL.RequestURI())
		if r.URL.Query().Get("view") == "subtasks" {
			_, _ = io.WriteString(w, `[{"id":"501","collectorId":"7","description":"edge-fw1","status":"RUNNING","startedAt":"2026-10-02T08:05:01.250Z"},
			  {"id":"502","collectorId":"7","description":"core1","status":"SUCCEEDED","startedAt":1759392300000,"finishedAt":"2026-10-02T08:05:30Z"}]`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"88","type":"NETWORK_COLLECTION","status":"RUNNING","networkId":"9","progress":{"total":3,"running":1,"timedOut":1,"canceled":1},
		  "startedAt":"2026-10-02T08:00:00Z",
		  "subTasks":[
		    {"id":"501","collectorId":"7","description":"edge-fw1","status":"RUNNING","startedAt":"2026-10-02T08:05:01Z","operation":"show version"},
		    {"id":"503","collectorId":"7","description":"acc7","status":"TIMED_OUT","startedAt":"2026-10-02T08:05:02Z","finishedAt":"2026-10-02T11:05:02Z","note":"device collection timeout"},
		    {"id":"504","collectorId":"7","description":"acc8","status":"CANCELED","startedAt":"2026-10-02T08:05:03Z","finishedAt":"2026-10-02T09:00:00Z"}]}`)
	}))
	defer server.Close()
	tasks := newTestClient(t, server.URL).CollectorTasks
	ctx := context.Background()

	at := time.Date(2026, 10, 2, 10, 0, 0, 0, time.FixedZone("x", -7*3600)) // 17:00Z
	running, _, err := tasks.SubTasksAt(ctx, "88", at)
	if err != nil || uris[0] != "/api/collector-tasks/88?at=2026-10-02T17%3A00%3A00Z&view=subtasks" {
		t.Fatalf("SubTasksAt: %v %s", err, uris[0])
	}
	if len(running) != 2 || running[0].Description != "edge-fw1" || !running[0].StartedAt.Equal(time.Date(2026, 10, 2, 8, 5, 1, 250_000_000, time.UTC)) || !running[0].FinishedAt.IsZero() {
		t.Fatalf("running subtask = %+v", running[0])
	}
	if !running[1].StartedAt.Equal(time.UnixMilli(1759392300000)) || running[1].Status != CollectorTaskSucceeded {
		t.Fatalf("epoch-millis subtask = %+v", running[1])
	}
	if _, _, err := tasks.SubTasksAt(ctx, "88", time.Time{}); err == nil {
		t.Fatal("a zero instant must be refused: Forward requires 'at'")
	}

	full, _, err := tasks.GetWithSubTasks(ctx, "88", 50000)
	if err != nil || uris[1] != "/api/collector-tasks/88?for=ui&max=50000" {
		t.Fatalf("GetWithSubTasks: %v %s", err, uris[1])
	}
	if full.ID != "88" || full.Status != CollectorTaskRunning || len(full.SubTasks) != 3 {
		t.Fatalf("task = %+v", full)
	}
	timedOut, canceled := full.SubTasks[1], full.SubTasks[2]
	if timedOut.Status != CollectorTaskTimedOut || timedOut.Note != "device collection timeout" || timedOut.FinishedAt.Sub(timedOut.StartedAt) != 3*time.Hour {
		t.Fatalf("timed-out subtask = %+v", timedOut)
	}
	if canceled.Status != CollectorTaskCanceled || full.SubTasks[0].Operation != "show version" {
		t.Fatalf("cancelled / operation = %+v", full.SubTasks)
	}
	if _, _, err := tasks.GetWithSubTasks(ctx, "88", 0); err != nil || uris[2] != "/api/collector-tasks/88?for=ui" {
		t.Fatalf("a zero limit must send no max so Forward's default applies: %v %s", err, uris[2])
	}
	if _, _, err := tasks.GetWithSubTasks(ctx, "88", -1); err == nil {
		t.Fatal("a negative limit must be refused")
	}
}
