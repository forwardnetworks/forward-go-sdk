package forward

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Starting a test probes the device from the collector, so the single and bulk
// starts must hit exactly the per-device and bulkStart routes (never the
// whole-network /start, which tests every classic device), send the names as
// Forward's DeviceSet {"devices": [...]} with duplicates folded, and refuse an
// empty or oversized set before anything reaches Forward.
func TestConnectivityTestsStartStopAndCap(t *testing.T) {
	t.Parallel()

	var calls []string
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.EscapedPath())
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()

	if _, err := client.ConnectivityTests.Start(ctx, "5", "edge-1.a"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ConnectivityTests.Stop(ctx, "5", "edge1"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ConnectivityTests.BulkStart(ctx, "5", []string{"a", "b", "a", " "}); err != nil {
		t.Fatal(err)
	}
	if gotBody != `{"devices":["a","b"]}` {
		t.Fatalf("bulk body %s", gotBody)
	}
	if _, err := client.ConnectivityTests.BulkStop(ctx, "5", []string{"a"}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"POST /api/networks/5/connectivityTests/edge-1.a/start",
		"POST /api/networks/5/connectivityTests/edge1/stop",
		"POST /api/networks/5/connectivityTests/bulkStart",
		"POST /api/networks/5/connectivityTests/bulkStop",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls %v", calls)
	}

	sent := len(calls)
	many := make([]string, MaxConnectivityTestDevices+1)
	for i := range many {
		many[i] = "d" + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	for name, call := range map[string]func() error{
		"empty":    func() error { _, err := client.ConnectivityTests.BulkStart(ctx, "5", nil); return err },
		"too many": func() error { _, err := client.ConnectivityTests.BulkStart(ctx, "5", many); return err },
		"no name":  func() error { _, err := client.ConnectivityTests.Start(ctx, "5", " "); return err },
		"dot":      func() error { _, err := client.ConnectivityTests.Start(ctx, "5", "."); return err },
		"dot dot":  func() error { _, err := client.ConnectivityTests.Stop(ctx, "5", ".."); return err },
		"get dots": func() error { _, _, err := client.ConnectivityTests.Get(ctx, "5", ".."); return err },
		"bad bulk": func() error {
			_, err := client.ConnectivityTests.BulkStart(ctx, "5", []string{"ok", "a b"})
			return err
		},
	} {
		if call() == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if len(calls) != sent {
		t.Fatal("a refused call reached Forward")
	}
	if _, err := client.ConnectivityTests.BulkStart(ctx, "5", many[:MaxConnectivityTestDevices]); err != nil {
		t.Fatalf("exactly the cap is allowed: %v", err)
	}
	if _, err := client.ConnectivityTests.BulkStop(ctx, "5", many); err != nil {
		t.Fatalf("stopping is never capped: %v", err)
	}
}

// List returns each device's stored result flat (Forward unwraps the
// SourceConnectivityResult and adds deviceName); Get on a device never tested is
// a plain 404 that must not read as a missing network.
func TestConnectivityTestsListAndGet(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/networks/5/connectivityTests":
			_, _ = w.Write([]byte(`[{"deviceName":"edge1","error":"AUTHENTICATION_FAILED","errorPhase":"AUTHENTICATION","hostIps":["10.0.0.1"],"savedAt":"2026-10-16T01:00:01Z","endTime":"2026-10-16T01:00:00Z"},
				{"deviceName":"edge2","discoveredType":"cisco_ios_xe_ssh","endTime":"2026-10-16T01:00:00Z"}]`))
		case "/api/networks/5/connectivityTests/never":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"apiUrl":"/api/networks/5/connectivityTests/never","httpMethod":"GET","message":"Connectivity test result not found"}`))
		default:
			// The single-device route returns the bare result: Forward does not serialize deviceName there.
			_, _ = w.Write([]byte(`{"error":"PING_FAILED","errorPhase":"CONNECTION"}`))
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()

	results, _, err := client.ConnectivityTests.List(ctx, "5")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].DeviceName != "edge1" || results[0].Error != "AUTHENTICATION_FAILED" || results[0].SavedAt != "2026-10-16T01:00:01Z" ||
		len(results[0].HostIPs) != 1 || results[1].DiscoveredType != "cisco_ios_xe_ssh" || results[1].Error != "" {
		t.Fatalf("decoded %+v", results)
	}
	one, _, err := client.ConnectivityTests.Get(ctx, "5", "edge1")
	if err != nil || one.Error != "PING_FAILED" || one.ErrorPhase != "CONNECTION" || one.DeviceName != "edge1" {
		t.Fatalf("get: %v %+v", err, one)
	}
	if _, _, err := client.ConnectivityTests.Get(ctx, "5", "never"); err == nil || errors.Is(err, ErrNetworkNotFound) || !IsStatus(err, http.StatusNotFound) {
		t.Fatalf("an untested device is a plain 404, got %v", err)
	}
}
