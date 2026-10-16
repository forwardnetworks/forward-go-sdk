package forward

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// The SDK builds request paths from caller-supplied IDs and names, then resolves
// them against the base URL, which removes "." and ".." segments. An ID of ".."
// therefore turned GET /api/networks/5/snapshots/.. into GET /api/networks/5/snapshots,
// and a device named "." turned its per-device test route into the
// whole-network one. A segment like that is refused before any request is sent,
// whether it arrives literally or percent-encoded, while dots inside a name
// ("a..b", ".hidden", "v1.2") stay legal.
func TestPathDotSegmentsAreRefused(t *testing.T) {
	t.Parallel()

	var sent atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent.Add(1)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	for _, path := range []string{
		"/api/networks/5/snapshots/..",
		"/api/networks/5/connectivityTests/./start",
		"/api/networks/5/%2e%2e",
		"/api/networks/../users",
		"/api/networks/5/%2E/x",
		"/api/networks/5/.",
		"/api/networks/5/.%2E/x",
		"/api/../login",     // used to pass the /api/ prefix check and resolve to /login
		"/api%2F..%2Flogin", // an escaped climb must not pass as an /api/ path either
	} {
		if _, err := client.NewRequest(context.Background(), http.MethodGet, path, nil); err == nil {
			t.Errorf("%s must be refused", path)
		}
	}
	for _, path := range []string{"/api/networks/5/devices/a..b", "/api/networks/5/devices/.hidden", "/api/networks/5/devices/v1.2", "/api/networks/5/devices/a%2F..%2Fb", "/api/networks/5/devices/", "/api/x?p=../y"} {
		if _, err := client.NewRequest(context.Background(), http.MethodGet, path, nil); err != nil {
			t.Errorf("%s must stay legal: %v", path, err)
		}
	}
	// End to end through a typed method that builds its path from an ID.
	if _, _, err := client.Aliases.Get(context.Background(), "5", ".."); err == nil {
		t.Error("an alias name of .. must be refused")
	}
	if sent.Load() != 0 {
		t.Fatalf("%d requests reached the server", sent.Load())
	}
}

// Diffs.Count builds its path from a DiffKind that is not escaped (the routing
// loop family is "routing-loop/count"), so a kind with a query, fragment,
// escape or space must be refused rather than reshape the request; every
// family the SDK itself lists stays accepted.
func TestDiffKindCannotReshapeTheRequest(t *testing.T) {
	t.Parallel()

	var sent atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent.Add(1)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()

	for _, bad := range []DiffKind{"devices?x=1", "devices#f", "a b", "%2e%2e", "../x", "devices/", "/devices", "Devices", "a//b", "devices\n", "devices%3Fx", "a_b"} {
		if _, _, err := client.Diffs.Count(ctx, "1", "2", bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
	if sent.Load() != 0 {
		t.Fatalf("%d refused kinds reached the server", sent.Load())
	}
	for _, kind := range CountableDiffKinds {
		if _, _, err := client.Diffs.Count(ctx, "1", "2", kind); err != nil {
			t.Errorf("%q must stay accepted: %v", kind, err)
		}
	}
}
