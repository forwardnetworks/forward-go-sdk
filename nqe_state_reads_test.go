package forward

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The reads must send Skyforge's queries (with the device and interface scoping spliced where its Python puts it), page on the
// composite key, and decode the same columns; a device name that could break out of the literal is refused before any request.
func TestStateReads(t *testing.T) {
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query        string `json:"query"`
			QueryOptions struct {
				SortBy struct {
					ColumnName string `json:"columnName"`
				} `json:"sortBy"`
			} `json:"queryOptions"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		queries = append(queries, body.Query)
		if strings.Contains(body.Query, "protocol.bgp") {
			if body.QueryOptions.SortBy.ColumnName != "Device" {
				t.Errorf("sort = %q, want the first key column", body.QueryOptions.SortBy.ColumnName)
			}
			_, _ = w.Write([]byte(`{"snapshotId":"9","totalNumItems":2,"items":[
			 {"Device":"a","VRF":"default","NeighborIP":"10.0.0.1","RemoteAS":65001,"SessionState":"ESTABLISHED"},
			 {"Device":"a","VRF":"mgmt","NeighborIP":"10.0.0.1","RemoteAS":"65002","SessionState":"IDLE"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"snapshotId":"9","totalNumItems":1,"items":[{"Device":"a","LocalIntf":"Et1","RemoteSys":"b","RemotePort":"Et2"}]}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()

	sessions, _, err := client.NQE.BGPSessions(ctx, "n1", "9", StateReadOptions{})
	if err != nil || len(sessions) != 2 || !sessions[0].Established() || sessions[0].RemoteAS != "65001" || sessions[1].Established() || sessions[1].VRF != "mgmt" {
		t.Fatalf("sessions = %+v err = %v", sessions, err)
	}
	// The same neighbor in two VRFs is two sessions: only the three-column key tells them apart.
	if sessions[0].Key() == sessions[1].Key() {
		t.Fatal("VRF must be part of the key")
	}

	adj, _, err := client.NQE.LLDPAdjacencies(ctx, "n1", "9", LLDPOptions{Devices: []string{"bb1", "bb2"}, SkipInterfaces: []string{"ma0"}})
	if err != nil || len(adj) != 1 || adj[0].Key() != [4]string{"a", "Et1", "b", "Et2"} {
		t.Fatalf("adj = %+v err = %v", adj, err)
	}
	want := "foreach device in network.devices\nwhere device.name == \"bb1\" || device.name == \"bb2\"\nforeach interface in device.interfaces\nwhere interface.name != \"ma0\"\nforeach neighbor in interface.lldp.neighbors\nselect {"
	if !strings.HasPrefix(queries[1], want) {
		t.Fatalf("query = %q", queries[1])
	}
	n := len(queries)
	if _, _, err = client.NQE.LLDPAdjacencies(ctx, "n1", "9", LLDPOptions{Devices: []string{`x" || true || device.name == "y`}}); err == nil || len(queries) != n {
		t.Fatal("a name that breaks the literal must be refused before any request")
	}
}

// A composite key repeats only when every column repeats: two rows sharing the first column are fine, an identical tuple is not.
func TestRunAllCompositeKey(t *testing.T) {
	rows := `[{"A":"x","B":"1"},{"A":"x","B":"2"}]`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"snapshotId":"9","totalNumItems":2,"items":` + rows + `}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	opts := NQEPagedOptions{KeyColumns: []string{"A", "B"}, Attempts: 1}
	if got, _, err := client.NQE.RunAll(context.Background(), "n1", "9", NQEQueryRequest{Query: "select {A: 1}"}, opts); err != nil || len(got) != 2 {
		t.Fatalf("got %d rows, err %v", len(got), err)
	}
	rows = `[{"A":"x","B":"1"},{"A":"x","B":"1"}]`
	if _, _, err := client.NQE.RunAll(context.Background(), "n1", "9", NQEQueryRequest{Query: "select {A: 1}"}, opts); err == nil {
		t.Fatal("an identical key tuple must be reported")
	}
}

// Interface admin text is Forward's enum text, never "UP"; ACL rows repeat their (Device, AclName) by design and must neither fail the
// read nor lose the count.
func TestInterfaceAdminAndACLReads(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query string `json:"query"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		if strings.Contains(body.Query, "adminStatus") {
			_, _ = w.Write([]byte(`{"snapshotId":"9","totalNumItems":2,"items":[
			 {"Device":"a","Interface":"Et1","AdminStatus":"AdminStatus.UP"},
			 {"Device":"a","Interface":"Et2","AdminStatus":"AdminStatus.DOWN"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"snapshotId":"9","totalNumItems":3,"items":[
		 {"Device":"a","AclName":"edge-in","Action":"AclAction.PERMIT"},
		 {"Device":"a","AclName":"edge-in","Action":"AclAction.DENY"},
		 {"Device":"b","AclName":"edge-in","Action":"AclAction.PERMIT"}]}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()

	ifaces, _, err := client.NQE.InterfaceAdmin(ctx, "n1", "9", StateReadOptions{})
	if err != nil || len(ifaces) != 2 || !ifaces[0].Up() || ifaces[1].Up() || ifaces[0].Key() != [2]string{"a", "Et1"} {
		t.Fatalf("ifaces = %+v err = %v", ifaces, err)
	}
	counts, _, err := client.NQE.ACLCounts(ctx, "n1", "9", StateReadOptions{})
	if err != nil || counts[ACLKey{"a", "edge-in"}] != 2 || counts[ACLKey{"b", "edge-in"}] != 1 || len(counts) != 2 {
		t.Fatalf("counts = %v err = %v", counts, err)
	}
}

// With an identifying column named in KeyColumns the repeated-key check is back on: distinct UUIDs read fine, a repeated tuple is
// reported, and KeyColumns without a query that selects them is refused.
func TestACLEntriesWithKey(t *testing.T) {
	rows := `[{"Device":"a","AclName":"r","Action":"AclAction.PERMIT","UUID":"u1"},{"Device":"a","AclName":"r","Action":"AclAction.PERMIT","UUID":"u2"}]`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"snapshotId":"9","totalNumItems":2,"items":` + rows + `}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()
	options := ACLReadOptions{StateReadOptions: StateReadOptions{Attempts: 1}, Query: "select {Device: 1}", KeyColumns: []string{"Device", "AclName", "UUID"}}
	got, _, err := client.NQE.ACLEntriesWith(ctx, "n1", "9", options)
	if err != nil || len(got) != 2 || got[0].Key() != [3]string{"a", "r", "u1"} || got[0].Key() == got[1].Key() {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	// The typed view must not lose the columns the key used: Row keeps all of them and Identity is the key columns' values.
	if got[0].Identity[2] != "u1" || got[0].IdentityKey() == got[1].IdentityKey() || len(got[0].Row) != 4 {
		t.Fatalf("identity=%v row=%v", got[0].Identity, got[0].Row)
	}
	rows = `[{"Device":"a","AclName":"r","Action":"x","UUID":"u1"},{"Device":"a","AclName":"r","Action":"x","UUID":"u1"}]`
	if _, _, err = client.NQE.ACLEntriesWith(ctx, "n1", "9", options); err == nil {
		t.Fatal("a repeated identifying key must be reported")
	}
	if _, _, err = client.NQE.ACLEntriesWith(ctx, "n1", "9", ACLReadOptions{KeyColumns: []string{"UUID"}}); err == nil {
		t.Fatal("KeyColumns with the default query must be refused")
	}
}
