package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// The shapes are the published WAN Circuits schemas. source is read back but
// never sent: WanCircuit's JSON creator does not take it.
func TestWanCircuitsReadAndWrite(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, r.Method+" "+r.URL.Path+" "+string(b))
		mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/networks/n1/wan-circuits":
			_, _ = io.WriteString(w, `{"wanCircuits":[{"name":"wc1","source":"MANUAL","connection1":{"device":"ce1","port":"e1","vlan":100},"connection2":{"device":"ce2","port":"e2"}}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/networks/n1/wan-circuits/gone":
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"No WanCircuit named gone"}`)
		case r.Method == http.MethodPatch:
			_, _ = io.WriteString(w, `{"name":"wc2","connection1":{"device":"ce1","port":"e1"},"connection2":{"device":"ce3","port":"e9"}}`)
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/gone"):
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL).ForNetwork("n1")
	ctx := context.Background()

	list, _, err := client.WanCircuits.List(ctx, "")
	if err != nil || len(list) != 1 || list[0].Connection1.VLAN == nil || *list[0].Connection1.VLAN != 100 || list[0].Source != "MANUAL" {
		t.Fatalf("List() = %+v, %v", list, err)
	}
	if got, _, err := client.WanCircuits.Get(ctx, "", "gone"); got != nil || err != nil {
		t.Fatalf("absent Get() = %+v, %v; want nil, nil", got, err)
	}
	// Writing back what was read keeps the circuit and drops source.
	if _, err := client.WanCircuits.Put(ctx, "", "wc1", list[0]); err != nil {
		t.Fatal(err)
	}
	put := bodies[len(bodies)-1]
	if !strings.HasPrefix(put, "PUT /api/networks/n1/wan-circuits/wc1 ") || strings.Contains(put, "source") || !strings.Contains(put, `"vlan":100`) {
		t.Fatalf("PUT = %s", put)
	}
	if _, err := client.WanCircuits.Put(ctx, "", "wc1", WanCircuit{Name: "other"}); err == nil {
		t.Fatal("a body naming a different circuit must be refused: Forward would store it under the body's name")
	}
	if _, err := client.WanCircuits.Put(ctx, "", " ", list[0]); err == nil {
		t.Fatal("an empty name must not collapse onto the collection route")
	}
	rename := "wc2"
	patched, _, err := client.WanCircuits.Patch(ctx, "", "wc1", WanCircuitPatch{Name: &rename, Connection2: &WanCircuitConnection{Device: "ce3", Port: "e9"}})
	if err != nil || patched.Name != "wc2" || !strings.Contains(bodies[len(bodies)-1], `{"name":"wc2","connection2":{"device":"ce3","port":"e9"}}`) {
		t.Fatalf("Patch() = %+v, %v; sent %s", patched, err, bodies[len(bodies)-1])
	}
	if _, err := client.WanCircuits.Delete(ctx, "", "gone"); err != nil {
		t.Fatalf("deleting an absent circuit is success: %v", err)
	}
}

// A PUT of the collection replaces every circuit, so an empty list wipes them
// all: refused unless the caller says so.
func TestWanCircuitsReplaceAllGuardsTheEmptyList(t *testing.T) {
	t.Parallel()

	var sent []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		sent = append(sent, r.Method+" "+r.URL.Path+" "+string(b))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL).ForNetwork("n1")

	if _, err := client.WanCircuits.ReplaceAll(context.Background(), "", nil, false); err == nil || len(sent) != 0 {
		t.Fatalf("an unasked empty ReplaceAll must be refused without a request: err=%v sent=%v", err, sent)
	}
	if _, err := client.WanCircuits.ReplaceAll(context.Background(), "", nil, true); err != nil || sent[0] != `PUT /api/networks/n1/wan-circuits {"wanCircuits":[]}` {
		t.Fatalf("an asked-for empty ReplaceAll = %v, %v", sent, err)
	}
	one := []WanCircuit{{Name: "wc1", Source: "MANUAL", Connection1: WanCircuitConnection{Device: "a", Port: "1"}, Connection2: WanCircuitConnection{Device: "b", Port: "2"}}}
	if _, err := client.WanCircuits.ReplaceAll(context.Background(), "", one, false); err != nil || strings.Contains(sent[1], "source") {
		t.Fatalf("ReplaceAll body = %s, %v", sent[1], err)
	}
}

// Backdate invalidates every snapshot from the named one onward, so: the
// parameter is exactly what each controller maps (op= for the synthetic
// collections, action= for link overrides), an empty snapshot is refused
// before anything is sent, and the operation name marks it for audit hooks.
func TestBackdateRoutesAndGuards(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var seen, ops []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	hooked, err := NewClient(Config{BaseURL: server.URL, Username: "u", Password: "p", NetworkID: "n1", Hooks: []Hook{func(_ context.Context, e Event) {
		if e.Type == EventRequest {
			mu.Lock()
			ops = append(ops, e.Operation)
			mu.Unlock()
		}
	}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	want := map[SyntheticNodeKind]string{
		SyntheticInternet:        "POST /api/networks/n1/internet-node?op=backdate&snapshotId=1021",
		SyntheticIntranet:        "POST /api/networks/n1/intranet-nodes?op=backdate&snapshotId=1021",
		SyntheticL3VPN:           "POST /api/networks/n1/l3-vpns?op=backdate&snapshotId=1021",
		SyntheticL2VPN:           "POST /api/networks/n1/l2-vpns?op=backdate&snapshotId=1021",
		SyntheticAdjacentNetwork: "POST /api/networks/n1/adjacent-networks?op=backdate&snapshotId=1021",
	}
	for kind, request := range want {
		seen, ops = nil, nil
		if _, err := hooked.SyntheticNodes.Backdate(ctx, "", kind, " 1021 "); err != nil || len(seen) != 1 || seen[0] != request {
			t.Errorf("%s: %v %v", kind, seen, err)
		}
		if len(ops) != 1 || !strings.HasPrefix(ops[0], "SyntheticNodes.Backdate") {
			t.Errorf("%s: operation = %v", kind, ops)
		}
	}
	seen, ops = nil, nil
	if _, err := hooked.WanCircuits.Backdate(ctx, "", "1021"); err != nil || seen[0] != "POST /api/networks/n1/wan-circuits?op=backdate&snapshotId=1021" || ops[0] != "WanCircuits.Backdate" {
		t.Fatalf("WanCircuits.Backdate: %v %v %v", seen, ops, err)
	}
	seen, ops = nil, nil
	if _, err := hooked.Topology.BackdateLinkOverrides(ctx, "", "1021"); err != nil || seen[0] != "POST /api/networks/n1/link-overrides?action=backdate&snapshotId=1021" || ops[0] != "Topology.BackdateLinkOverrides" {
		t.Fatalf("BackdateLinkOverrides: %v %v %v", seen, ops, err)
	}

	seen = nil
	if _, err := hooked.SyntheticNodes.Backdate(ctx, "", SyntheticL3VPN, " "); err == nil {
		t.Error("SyntheticNodes.Backdate accepted an empty snapshot ID")
	}
	if _, err := hooked.WanCircuits.Backdate(ctx, "", ""); err == nil {
		t.Error("WanCircuits.Backdate accepted an empty snapshot ID")
	}
	if _, err := hooked.Topology.BackdateLinkOverrides(ctx, "", ""); err == nil {
		t.Error("BackdateLinkOverrides accepted an empty snapshot ID")
	}
	if len(seen) != 0 {
		t.Fatalf("a refused backdate still sent: %v", seen)
	}
}
