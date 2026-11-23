package forward

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
)

// usecaseRequest is one request a test server saw.
type usecaseRequest struct {
	method, path, query, body string
}

// usecaseServer answers each request with handle and records it.
func usecaseServer(t *testing.T, handle func(w http.ResponseWriter, r *http.Request, body string)) (*httptest.Server, func() []usecaseRequest) {
	t.Helper()
	var mu sync.Mutex
	var seen []usecaseRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, usecaseRequest{r.Method, r.URL.EscapedPath(), r.URL.RawQuery, string(raw)})
		mu.Unlock()
		handle(w, r, string(raw))
	}))
	t.Cleanup(server.Close)
	return server, func() []usecaseRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]usecaseRequest(nil), seen...)
	}
}

// syntheticNodeKind is one of the two synthetic node families whose typed helpers are tested the same way.
type syntheticNodeKind struct {
	name    string
	segment string
	list    func(*Client, context.Context, string) ([]domain.SyntheticNode, error)
	get     func(*Client, context.Context, string, string) (*domain.SyntheticNode, error)
	put     func(*Client, context.Context, string, string, domain.SyntheticNode) error
	del     func(*Client, context.Context, string, string) error
}

var syntheticNodeKinds = []syntheticNodeKind{
	{
		name:    "intranet",
		segment: "intranet-nodes",
		list: func(c *Client, ctx context.Context, n string) ([]domain.SyntheticNode, error) {
			v, _, err := c.SyntheticNodes.ListIntranetNodes(ctx, n)
			return v, err
		},
		get: func(c *Client, ctx context.Context, n, name string) (*domain.SyntheticNode, error) {
			v, _, err := c.SyntheticNodes.GetIntranetNode(ctx, n, name)
			return v, err
		},
		put: func(c *Client, ctx context.Context, n, name string, node domain.SyntheticNode) error {
			_, err := c.SyntheticNodes.PutIntranetNode(ctx, n, name, node)
			return err
		},
		del: func(c *Client, ctx context.Context, n, name string) error {
			_, err := c.SyntheticNodes.DeleteIntranetNode(ctx, n, name)
			return err
		},
	},
	{
		name:    "l3vpn",
		segment: "l3-vpns",
		list: func(c *Client, ctx context.Context, n string) ([]domain.SyntheticNode, error) {
			v, _, err := c.SyntheticNodes.ListL3VPNs(ctx, n)
			return v, err
		},
		get: func(c *Client, ctx context.Context, n, name string) (*domain.SyntheticNode, error) {
			v, _, err := c.SyntheticNodes.GetL3VPN(ctx, n, name)
			return v, err
		},
		put: func(c *Client, ctx context.Context, n, name string, node domain.SyntheticNode) error {
			_, err := c.SyntheticNodes.PutL3VPN(ctx, n, name, node)
			return err
		},
		del: func(c *Client, ctx context.Context, n, name string) error {
			_, err := c.SyntheticNodes.DeleteL3VPN(ctx, n, name)
			return err
		},
	},
}

// Each typed synthetic-node helper must address its own family's collection (intranet-nodes or l3-vpns, never another's), list the
// network's nodes, read one by name with an absent node (Forward's 404 "No intranet node named ...", IntranetNodeController) as nil
// rather than an error, replace one with a PUT of the whole node, and treat deleting an already absent node as done.
func TestSyntheticNodeTypedHelpers(t *testing.T) {
	t.Parallel()
	for _, kind := range syntheticNodeKinds {
		kind := kind
		t.Run(kind.name, func(t *testing.T) {
			t.Parallel()
			base := "/api/networks/net-1/" + kind.segment
			server, seen := usecaseServer(t, func(w http.ResponseWriter, r *http.Request, body string) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == base:
					_, _ = io.WriteString(w, `[{"name":"edge-1","connections":[]},{"name":"edge-2","connections":[]}]`)
				case r.Method == http.MethodGet && r.URL.Path == base+"/edge-1":
					_, _ = io.WriteString(w, `{"name":"edge-1","connections":[],"queryId":"Q_1"}`)
				case r.Method == http.MethodPut && r.URL.Path == base+"/edge-1":
					w.WriteHeader(http.StatusNoContent)
				case r.Method == http.MethodDelete && r.URL.Path == base+"/edge-1":
					w.WriteHeader(http.StatusNoContent)
				default:
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusNotFound)
					_, _ = io.WriteString(w, `{"httpMethod":"`+r.Method+`","apiUrl":"`+r.URL.Path+`","message":"No intranet node named gone"}`)
				}
			})
			client := newTestClient(t, server.URL)
			ctx := context.Background()

			nodes, err := kind.list(client, ctx, "net-1")
			if err != nil || len(nodes) != 2 || nodes[0].Name != "edge-1" || nodes[1].Name != "edge-2" {
				t.Fatalf("list = %#v, %v", nodes, err)
			}
			node, err := kind.get(client, ctx, "net-1", "edge-1")
			if err != nil || node == nil || node.Name != "edge-1" || node.QueryID != "Q_1" {
				t.Fatalf("get = %#v, %v", node, err)
			}
			absent, err := kind.get(client, ctx, "net-1", "gone")
			if err != nil || absent != nil {
				t.Fatalf("get of an absent node = %#v, %v; want nil, nil", absent, err)
			}
			if err := kind.put(client, ctx, "net-1", "edge-1", domain.SyntheticNode{Name: "edge-1", QueryID: "Q_1"}); err != nil {
				t.Fatalf("put: %v", err)
			}
			if err := kind.del(client, ctx, "net-1", "edge-1"); err != nil {
				t.Fatalf("delete: %v", err)
			}
			if err := kind.del(client, ctx, "net-1", "gone"); err != nil {
				t.Fatalf("delete of an absent node must succeed: %v", err)
			}

			got := seen()
			want := []struct{ method, path string }{
				{http.MethodGet, base}, {http.MethodGet, base + "/edge-1"}, {http.MethodGet, base + "/gone"},
				{http.MethodPut, base + "/edge-1"}, {http.MethodDelete, base + "/edge-1"}, {http.MethodDelete, base + "/gone"},
			}
			if len(got) != len(want) {
				t.Fatalf("requests = %#v", got)
			}
			for i, w := range want {
				if got[i].method != w.method || got[i].path != w.path || got[i].query != "" {
					t.Errorf("request %d = %s %s?%s, want %s %s", i, got[i].method, got[i].path, got[i].query, w.method, w.path)
				}
			}
			var put map[string]any
			if err := json.Unmarshal([]byte(got[3].body), &put); err != nil {
				t.Fatalf("put body %q: %v", got[3].body, err)
			}
			if put["name"] != "edge-1" || put["queryId"] != "Q_1" {
				t.Fatalf("put body = %s", got[3].body)
			}
		})
	}
}

// A non-404 failure of a delete is not "already absent": it must reach the caller.
func TestSyntheticNodeDeleteOtherErrorsFail(t *testing.T) {
	t.Parallel()
	server, _ := usecaseServer(t, func(w http.ResponseWriter, r *http.Request, body string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"message":"Missing permission"}`)
	})
	client := newTestClient(t, server.URL)
	for _, kind := range syntheticNodeKinds {
		if err := kind.del(client, context.Background(), "net-1", "edge-1"); err == nil {
			t.Errorf("%s: a 403 delete must be an error", kind.name)
		}
	}
}

// StartChatOperation starts the chat (POST /api/ai-chats with the prompt, network and snapshot) and hands back a handle that already
// holds the started chat, keeps the newest state on each refresh, and lets Wait run until the chat is DONE with a callback per
// change, so a caller can inspect or wait on the chat while Forward works on it.
func TestAIStartChatOperation(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	gets := 0
	server, seen := usecaseServer(t, func(w http.ResponseWriter, r *http.Request, body string) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/ai-chats":
			_, _ = io.WriteString(w, `{"id":"chat-7","networkId":"net-1","snapshotId":"snap-1","status":"PROCESSING"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/ai-chats/chat-7":
			gets++
			status := "PROCESSING"
			if gets >= 2 {
				status = "DONE"
			}
			_, _ = io.WriteString(w, `{"id":"chat-7","networkId":"net-1","snapshotId":"snap-1","status":"`+status+`","name":"risks"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.RequestURI())
			http.NotFound(w, r)
		}
	})
	client := newTestClient(t, server.URL)
	ctx := context.Background()
	op, _, err := client.AI.StartChatOperation(ctx, "net-1", "snap-1", "find risks")
	if err != nil || op == nil {
		t.Fatalf("StartChatOperation = %v, %v", op, err)
	}
	if cur := op.Current(); cur == nil || cur.ID != "chat-7" || cur.Status != "PROCESSING" {
		t.Fatalf("Current() after start = %#v", cur)
	}
	if _, _, err := op.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if cur := op.Current(); cur == nil || cur.Status != "PROCESSING" || cur.Name != "risks" {
		t.Fatalf("Current() after refresh = %#v", cur)
	}
	var updates []string
	final, _, err := op.Wait(ctx, ports.PollOptions[domain.AIChat]{Interval: time.Millisecond, Budget: 30 * time.Second,
		OnUpdate: func(u ports.PollUpdate[domain.AIChat]) { updates = append(updates, u.Value.Status) }})
	if err != nil || final == nil || final.Status != "DONE" {
		t.Fatalf("Wait = %#v, %v", final, err)
	}
	if cur := op.Current(); cur == nil || cur.Status != "DONE" {
		t.Fatalf("Current() after Wait = %#v", cur)
	}
	if len(updates) == 0 || updates[len(updates)-1] != "DONE" {
		t.Fatalf("updates = %v, want the change to DONE reported", updates)
	}
	got := seen()
	if len(got) < 3 || got[0].method != http.MethodPost || got[0].path != "/api/ai-chats" {
		t.Fatalf("requests = %#v", got)
	}
	if got[0].query != "networkId=net-1&snapshotId=snap-1" {
		t.Fatalf("start query = %q", got[0].query)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(got[0].body), &body); err != nil || body["prompt"] != "find risks" {
		t.Fatalf("start body = %q, %v", got[0].body, err)
	}
}

// ExistingNames lets a corpus promotion skip checks that already exist: it is the set of names of the checks Forward lists on the
// snapshot (GET /api/snapshots/{id}/checks), every one present and nothing else.
func TestChecksExistingNames(t *testing.T) {
	t.Parallel()
	server, seen := usecaseServer(t, func(w http.ResponseWriter, r *http.Request, body string) {
		_, _ = io.WriteString(w, `[{"id":"C-1","name":"Web Reachable","status":"PASS"},{"id":"C-2","name":"ssh isolated","status":"FAIL"}]`)
	})
	client := newTestClient(t, server.URL)
	names, _, err := client.Checks.ExistingNames(context.Background(), "snap-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || !names["Web Reachable"] || !names["ssh isolated"] || names["other"] {
		t.Fatalf("names = %v", names)
	}
	got := seen()
	if len(got) != 1 || got[0].method != http.MethodGet || got[0].path != "/api/snapshots/snap-1/checks" || got[0].query != "" {
		t.Fatalf("requests = %#v", got)
	}
}

// nqeRequest is the part of a POST /api/nqe body these tests look at.
type nqeRequest struct {
	Query   string `json:"query"`
	Options struct {
		Offset *int32 `json:"offset"`
		Limit  *int32 `json:"limit"`
		SortBy *struct {
			ColumnName string `json:"columnName"`
		} `json:"sortBy"`
	} `json:"queryOptions"`
}

// DiffSource reads the same inline query on both snapshots and compares the rows by the key column, so a source query (which
// Forward's own nqe-diffs route cannot take) still yields ADDED, DELETED and MODIFIED rows, with unchanged rows left out.
func TestNQEDiffSource(t *testing.T) {
	t.Parallel()
	const query = "foreach d in network.devices select { Device: d.name, OS: toString(d.platform.os) }"
	server, seen := usecaseServer(t, func(w http.ResponseWriter, r *http.Request, body string) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/nqe" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.RequestURI())
			http.NotFound(w, r)
			return
		}
		switch r.URL.Query().Get("snapshotId") {
		case "before":
			_, _ = io.WriteString(w, `{"snapshotId":"before","items":[{"Device":"a","OS":"EOS"},{"Device":"b","OS":"IOS"},{"Device":"c","OS":"NXOS"}],"totalNumItems":3}`)
		case "after":
			_, _ = io.WriteString(w, `{"snapshotId":"after","items":[{"Device":"b","OS":"IOS_XE"},{"Device":"c","OS":"NXOS"},{"Device":"d","OS":"JUNOS"}],"totalNumItems":3}`)
		default:
			t.Errorf("unexpected snapshot %q", r.URL.RawQuery)
			http.NotFound(w, r)
		}
	})
	client := newTestClient(t, server.URL)
	diff, _, err := client.NQE.DiffSource(context.Background(), "net-1", "before", "after",
		domain.NQEQueryRequest{Query: query}, domain.NQEPagedOptions{KeyColumn: "Device", PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	types := map[string]string{}
	for _, e := range diff {
		rec := e.After
		if rec == nil {
			rec = e.Before
		}
		var device string
		_ = json.Unmarshal((*rec)["Device"], &device)
		types[device] = e.Type
	}
	if len(types) != 3 || types["a"] != "DELETED" || types["b"] != "MODIFIED" || types["d"] != "ADDED" {
		t.Fatalf("diff = %v (c is unchanged and must not appear)", types)
	}
	snapshots := map[string]bool{}
	for _, req := range seen() {
		if !strings.Contains(req.query, "networkId=net-1") {
			t.Errorf("query %q lacks the network", req.query)
		}
		var body nqeRequest
		if err := json.Unmarshal([]byte(req.body), &body); err != nil || body.Query != query {
			t.Errorf("body = %q, %v", req.body, err)
		}
		snapshots[strings.TrimPrefix(req.query[strings.Index(req.query, "snapshotId="):], "snapshotId=")] = true
	}
	if !snapshots["before"] || !snapshots["after"] || len(snapshots) != 2 {
		t.Fatalf("snapshots read = %v, want before and after", snapshots)
	}
}

// ACLEntries reads every ACL entry of one snapshot through the NQE endpoint and returns them as typed rows, one per entry, including
// several entries of one ACL (the rows have no unique column).
func TestNQEACLEntries(t *testing.T) {
	t.Parallel()
	server, seen := usecaseServer(t, func(w http.ResponseWriter, r *http.Request, body string) {
		_, _ = io.WriteString(w, `{"items":[{"Device":"r1","AclName":"EDGE-IN","Action":"PERMIT"},{"Device":"r1","AclName":"EDGE-IN","Action":"DENY"},{"Device":"r2","AclName":"MGMT","Action":"PERMIT"}],"totalNumItems":3}`)
	})
	client := newTestClient(t, server.URL)
	entries, _, err := client.NQE.ACLEntries(context.Background(), "net-1", "snap-1", domain.StateReadOptions{PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %#v", entries)
	}
	if entries[0].Device != "r1" || entries[0].AclName != "EDGE-IN" || entries[0].Action != "PERMIT" ||
		entries[1].Action != "DENY" || entries[2].Device != "r2" || entries[2].AclName != "MGMT" {
		t.Fatalf("entries = %#v", entries)
	}
	got := seen()
	if len(got) == 0 {
		t.Fatal("no request sent")
	}
	for _, req := range got {
		if req.method != http.MethodPost || req.path != "/api/nqe" || req.query != "networkId=net-1&snapshotId=snap-1" {
			t.Errorf("request = %s %s?%s", req.method, req.path, req.query)
		}
		var body nqeRequest
		if err := json.Unmarshal([]byte(req.body), &body); err != nil {
			t.Fatalf("body %q: %v", req.body, err)
		}
		if !strings.Contains(body.Query, "aclEntries") || body.Options.Limit == nil || *body.Options.Limit != 50 ||
			body.Options.Offset == nil || *body.Options.Offset != 0 || body.Options.SortBy == nil || body.Options.SortBy.ColumnName != "Device" {
			t.Errorf("body = %s", req.body)
		}
	}
}
