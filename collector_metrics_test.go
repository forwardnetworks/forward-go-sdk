package forward

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The route names the collector by NAME, needs both instants in the query,
// sends no samplesPerSeries unless one was asked for (Forward's default is 500,
// capped at 5000), and decodes every series of a sample. The 403 for a login
// without DEBUG_COLLECTORS is typed.
func TestCollectorMetricsReadsTheSeriesAndMapsThePermissionError(t *testing.T) {
	var gotPath, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.EscapedPath(), r.URL.RawQuery
		if r.URL.Path == "/api/collectors/locked/metrics" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"message":"Missing permission: OrgOperation.DEBUG_COLLECTORS"}`)
			return
		}
		_, _ = io.WriteString(w, `{"metrics":[{"timestamp":"2026-10-14T10:00:00Z","cpuMetrics":{"cpuLoad":0.93,"cpuCores":4},"memoryMetrics":{"vmMemTotalMb":16000,"vmMemFreeMb":900,"jvmMemMaxMb":8000,"jvmMemFreeMb":120,"procRssMb":7800},"gcMetrics":{"gcTimeSeconds":9,"gcLoad":0.4},"fileMetrics":{"fileDescriptorsMax":65535,"fileDescriptorsOpen":812},"diskMetrics":{"collector":{"sizeMb":100000,"availableMb":5000},"os":{"sizeMb":50000,"availableMb":20000},"tmp":{"sizeMb":10000,"availableMb":9000}},"taskMetrics":{"concurrency":8,"queueSize":31,"tasksStarted":500,"tasksFinished":470}},{"timestamp":1792000000000,"cpuMetrics":{"cpuLoad":0.1,"cpuCores":4},"memoryMetrics":{},"gcMetrics":{},"fileMetrics":{},"diskMetrics":{"collector":{},"os":{},"tmp":{}},"taskMetrics":{}}]}`)
	}))
	defer server.Close()
	collectors := newTestClient(t, server.URL).Collectors
	ctx := context.Background()
	start, end := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC), time.Date(2026, 10, 14, 11, 0, 0, 0, time.UTC)

	samples, _, err := collectors.Metrics(ctx, "dc/west", start, end, 0)
	if err != nil || len(samples) != 2 {
		t.Fatalf("samples = %+v, %v", samples, err)
	}
	if gotPath != "/api/collectors/dc%2Fwest/metrics" || gotQuery != "endTime=2026-10-14T11%3A00%3A00Z&startTime=2026-10-14T09%3A00%3A00Z" {
		t.Fatalf("request = %s ?%s", gotPath, gotQuery)
	}
	first := samples[0]
	if !first.Timestamp.Equal(time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)) || first.CPU.Load != 0.93 || first.CPU.Cores != 4 ||
		first.Memory.JVMFreeMB != 120 || first.Memory.ProcessRSSMB != 7800 || first.GC.Load != 0.4 || first.Files.Open != 812 ||
		first.Disk.Collector.AvailableMB != 5000 || first.Tasks.QueueSize != 31 || first.Tasks.TasksStarted-first.Tasks.TasksFinished != 30 {
		t.Fatalf("first sample = %+v", first)
	}
	if samples[1].Timestamp.UnixMilli() != 1792000000000 {
		t.Fatalf("a numeric timestamp must read as epoch milliseconds: %v", samples[1].Timestamp)
	}

	if _, _, err = collectors.Metrics(ctx, "c", start, end, 100000); err != nil || gotQuery != "endTime=2026-10-14T11%3A00%3A00Z&samplesPerSeries=5000&startTime=2026-10-14T09%3A00%3A00Z" {
		t.Fatalf("samplesPerSeries must be capped at what Forward allows: ?%s (%v)", gotQuery, err)
	}
	_, _, err = collectors.Metrics(ctx, "locked", start, end, 0)
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("a login without DEBUG_COLLECTORS must get the typed 403: %v", err)
	}
	if op, ok := MissingPermission(err); !ok || op != "OrgOperation.DEBUG_COLLECTORS" {
		t.Fatalf("MissingPermission = %q, %v", op, ok)
	}
	for name, call := range map[string]func() error{
		"blank name":   func() error { _, _, err := collectors.Metrics(ctx, " ", start, end, 0); return err },
		"end<start":    func() error { _, _, err := collectors.Metrics(ctx, "c", end, start, 0); return err },
		"zero times":   func() error { _, _, err := collectors.Metrics(ctx, "c", time.Time{}, end, 0); return err },
		"samples == 1": func() error { _, _, err := collectors.Metrics(ctx, "c", start, end, 1); return err },
		"samples < 0":  func() error { _, _, err := collectors.Metrics(ctx, "c", start, end, -3); return err },
	} {
		if call() == nil {
			t.Errorf("%s must fail before any request", name)
		}
	}
}
