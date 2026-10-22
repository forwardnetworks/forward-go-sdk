package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestNetworkLinkOverridesWire pins the four network-level staged-override calls to their method, path, query and
// body. The PUT must send [] for a nil list because Forward's LinkOverrides rejects a missing one, and ?view=staged
// must reach the wire or the call would read the full staged set instead of the edit since the last snapshot.
func TestNetworkLinkOverridesWire(t *testing.T) {
	t.Parallel()
	type seen struct{ method, uri, body string }
	var got []seen
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = append(got, seen{r.Method, r.RequestURI, string(b)})
		switch {
		case r.Method == http.MethodGet && r.URL.RawQuery == "view=staged":
			_, _ = w.Write([]byte(`{"presentAdditions":[{"port1":"a:e1","port2":"b:e1"}],"presentRemovals":[],"absentAdditions":[],"absentRemovals":[]}`))
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"present":[{"port1":"a:e1","port2":"b:e1"}]}`))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()

	cur, _, err := client.Topology.NetworkOverrides(ctx, "7")
	if err != nil || len(cur.Present) != 1 || cur.Absent == nil || len(cur.Absent) != 0 {
		t.Fatalf("NetworkOverrides = %#v, %v", cur, err)
	}
	staged, _, err := client.Topology.StagedOverrideChanges(ctx, "7")
	if err != nil || len(staged.PresentAdditions) != 1 || staged.PresentAdditions[0].Port2 != "b:e1" {
		t.Fatalf("StagedOverrideChanges = %#v, %v", staged, err)
	}
	if _, err := client.Topology.SetNetworkOverrides(ctx, "7", TopologyOverrides{Present: []TopologyOverrideLink{{Port1: "a:e1", Port2: "b:e1"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Topology.EditNetworkOverrides(ctx, "7", TopologyOverridesEdit{AbsentAdditions: []TopologyOverrideLink{{Port1: "c:e1", Port2: "d:e1"}}}); err != nil {
		t.Fatal(err)
	}
	want := []seen{
		{"GET", "/api/networks/7/link-overrides", ""},
		{"GET", "/api/networks/7/link-overrides?view=staged", ""},
		{"PUT", "/api/networks/7/link-overrides", `{"absent":[],"present":[{"port1":"a:e1","port2":"b:e1"}]}`},
		{"PATCH", "/api/networks/7/link-overrides", `{"absentAdditions":[{"port1":"c:e1","port2":"d:e1"}]}`},
	}
	if len(got) != len(want) {
		t.Fatalf("requests = %#v, want %d", got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("request %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}
