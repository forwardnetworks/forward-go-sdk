package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// An empty name must never collapse onto the collection route. A PUT there
// replaces every synthetic node of that kind, and a DELETE there is worse --
// both are one missing string away from wiping a network's synthetic devices.
func TestSyntheticNodeNameCannotEscapeToTheCollection(t *testing.T) {
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no request should be sent, got %s %s", r.Method, r.URL.Path)
	})
	ctx := context.Background()
	for _, kind := range []SyntheticNodeKind{SyntheticIntranet, SyntheticL3VPN} {
		if _, _, err := c.SyntheticNodes.Get(ctx, "net-1", kind, "  "); err == nil {
			t.Errorf("%s: an empty name must be refused on get", kind)
		}
		if _, err := c.SyntheticNodes.Put(ctx, "net-1", kind, "", SyntheticNode{}); err == nil {
			t.Errorf("%s: an empty name must be refused on put", kind)
		}
		if _, err := c.SyntheticNodes.Delete(ctx, "net-1", kind, ""); err == nil {
			t.Errorf("%s: an empty name must be refused on delete", kind)
		}
	}
	// The internet node is one per network and has no delete route at all.
	if _, err := c.SyntheticNodes.Delete(ctx, "net-1", SyntheticInternet, ""); err == nil {
		t.Error("the internet node must not be deletable")
	}
}

// Absence is not an error on a read: the caller reads precisely to learn
// whether it must create.
func TestSyntheticNodeGetTreatsAbsenceAsAbsence(t *testing.T) {
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	got, _, err := c.SyntheticNodes.Get(context.Background(), "net-1", SyntheticL3VPN, "vpn-a")
	if err != nil {
		t.Fatalf("a 404 must not be an error: %v", err)
	}
	if got != nil {
		t.Fatalf("a 404 must yield no node, got %+v", got)
	}
}

// Forward returns these as an ARRAY under a per-kind key, even though the
// Java field behind it is a map keyed by name. Its own L3VpnListTest pins the
// serialized form as {"l3Vpns": [...]}.
func TestSyntheticNodeListAcceptsForwardsEnvelopes(t *testing.T) {
	for name, tc := range map[string]struct {
		kind SyntheticNodeKind
		body string
	}{
		"l3 vpns keyed":  {SyntheticL3VPN, `{"l3Vpns":[{"name":"a"},{"name":"b"}]}`},
		"intranet keyed": {SyntheticIntranet, `{"intranetNodes":[{"name":"a"},{"name":"b"}]}`},
		"bare array":     {SyntheticL3VPN, `[{"name":"a"},{"name":"b"}]`},
	} {
		c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(tc.body))
		})
		got, _, err := c.SyntheticNodes.List(context.Background(), "net-1", tc.kind)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(got) != 2 || got[0].Name != "a" {
			t.Fatalf("%s: got %+v", name, got)
		}
	}

	// The internet node has no collection; listing it is empty, not an error.
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the internet node has no collection route")
	})
	got, _, err := c.SyntheticNodes.List(context.Background(), "net-1", SyntheticInternet)
	if err != nil || got != nil {
		t.Fatalf("internet list must be empty and non-error: %v %v", got, err)
	}
}

// A node read with an NQE query and written back must keep it: Forward's node PUT replaces the whole node, so a client that dropped queryId on the
// way back would silently remove the dynamic connections. The computed queryResult is read-only and must not be sent.
func TestSyntheticNodeRoundTripKeepsTheQueryAndNeverSendsItsResult(t *testing.T) {
	var putBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			io.WriteString(w, `{"name":"dir","connections":[],"queryId":"Q_abc","queryResult":{"connections":[{"uplinkPort":{"device":"r1","port":"e1"},"source":"NQE"}],"error":null}}`)
		case http.MethodPut:
			b, _ := io.ReadAll(r.Body)
			putBody = string(b)
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL).ForNetwork("n1")
	n, _, err := client.SyntheticNodes.Get(context.Background(), "", SyntheticL3VPN, "dir")
	if err != nil || n == nil || n.QueryID != "Q_abc" || n.QueryResult == nil || len(n.QueryResult.Connections) != 1 {
		t.Fatalf("Get: %+v %v", n, err)
	}
	if _, err := client.SyntheticNodes.Put(context.Background(), "", SyntheticL3VPN, "dir", *n); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(putBody, `"queryId":"Q_abc"`) || strings.Contains(putBody, "queryResult") {
		t.Fatalf("the PUT must keep queryId and omit queryResult: %s", putBody)
	}
}

// SetQuery sends only the queryId (a string to set it, an explicit null to clear it) with PATCH on the node's own route, and reports the node's query error.
func TestSyntheticNodesSetQuerySendsOnlyTheQueryAndReturnsTheResult(t *testing.T) {
	var method, path, body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		io.WriteString(w, `{"name":"dir","connections":[],"queryId":"Q_abc","queryResult":{"error":{"errorMsg":"wrong row type","status":"COLUMN_DATATYPE_MISMATCH"}}}`)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL).ForNetwork("n1")
	n, _, err := client.SyntheticNodes.SetQuery(context.Background(), "", SyntheticL3VPN, "dir", " Q_abc ")
	if err != nil || method != http.MethodPatch || path != "/api/networks/n1/l3-vpns/dir" || body != `{"queryId":"Q_abc"}` {
		t.Fatalf("%s %s %s %v", method, path, body, err)
	}
	if n.QueryResult == nil || n.QueryResult.Error == nil || n.QueryResult.Error.Status != "COLUMN_DATATYPE_MISMATCH" {
		t.Fatalf("the query error must come back: %+v", n)
	}
	if _, _, err := client.SyntheticNodes.SetQuery(context.Background(), "", SyntheticIntranet, "x", ""); err != nil || body != `{"queryId":null}` || path != "/api/networks/n1/intranet-nodes/x" {
		t.Fatalf("clearing sends an explicit null: %s %s %v", path, body, err)
	}
	if _, _, err := client.SyntheticNodes.SetQuery(context.Background(), "", SyntheticIntranet, " ", "Q_1"); err == nil {
		t.Fatal("an empty node name must be refused locally (it would address the collection)")
	}
}
