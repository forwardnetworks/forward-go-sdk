package forward

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// The FDB reads send the prefix (required) as a query; the collection is named Purge so a delete gate sees that it removes data, and it
// answers Forward's one line of text.
func TestFDBReadsAndPurge(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var seen []predictSeen
	client := newTestClient(t, predictRecorder(t, 200, `{"sizes":[1,2]}`, &seen).URL)
	if u, _, err := client.Admin.FDBUsage(ctx, "n/1/2/"); err != nil || string(u.Sizes) != "[1,2]" {
		t.Fatalf("usage = %+v, %v", u, err)
	}
	if _, _, err := client.Admin.FDBUsage(ctx, " "); err == nil || len(seen) != 1 {
		t.Fatal("a blank prefix was sent")
	}
	if seen[0].method != "GET" || seen[0].uri != "/api/system/fdb/usage?prefix=n%2F1%2F2%2F" {
		t.Fatalf("usage request %+v", seen[0])
	}
	sized := newTestClient(t, predictRecorder(t, 200, `123456`, &seen).URL)
	if n, _, err := sized.Admin.FDBSize(ctx, "n/1/"); err != nil || n != 123456 {
		t.Fatalf("size = %d, %v", n, err)
	}
	gc := newTestClient(t, predictRecorder(t, 200, `Garbage collection started`, &seen).URL)
	text, _, err := gc.Admin.PurgeFDBGarbage(ctx)
	if err != nil || text != "Garbage collection started" {
		t.Fatalf("gc = %q, %v", text, err)
	}
	if last := seen[len(seen)-1]; last.method != "POST" || last.uri != "/api/system/fdb/gc" {
		t.Fatalf("gc request %+v", last)
	}
}

// orgServer answers by route and records every request; failNetwork's delete answers 500.
func orgServer(t *testing.T, failNetwork string) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/admin/users":
			_, _ = io.WriteString(w, `[{"id":"u1","orgId":"8","username":"other"}]`)
		case r.Method == "GET" && r.URL.Path == "/api/admin/networks":
			_, _ = io.WriteString(w, `[{"id":"n1","orgId":"7"},{"id":"n2","orgId":"7"},{"id":"x9","orgId":"8"}]`)
		case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/api/orgs/7/networks/"):
			if strings.HasSuffix(r.URL.Path, "/n3") {
				w.WriteHeader(404)
				_, _ = io.WriteString(w, `{"message":"gone"}`)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/"+failNetwork) {
				w.WriteHeader(500)
				_, _ = io.WriteString(w, `{"message":"boom"}`)
				return
			}
			_, _ = io.WriteString(w, `{"id":"n","name":"n"}`)
		case r.Method == "DELETE" && r.URL.Path == "/api/admin/orgs/7":
			_, _ = io.WriteString(w, `{"id":"7","name":"gone"}`)
		default:
			w.WriteHeader(204)
		}
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

// Deleting an org the clean way: disable first, delete only the org's own networks through the support route (never another org's),
// and delete the org last. The listing needs a service client, so a caller can pass the networks instead.
func TestDeleteOrgCleanlyOrderAndScope(t *testing.T) {
	t.Parallel()
	server, calls := orgServer(t, "")
	client, err := NewClient(Config{BaseURL: server.URL, AuthMode: AuthModeService, Username: "u", Password: "p", HTTPClient: privateHTTPClient()})
	if err != nil {
		t.Fatal(err)
	}
	res, err := client.Organizations.DeleteOrgCleanly(context.Background(), "7", DeleteOrgOptions{})
	if err != nil || res.Org == nil || len(res.Deleted) != 2 || len(res.Failed) != 0 || !res.Disabled {
		t.Fatalf("result = %+v, %v", res, err)
	}
	want := []string{
		"GET /api/admin/networks",
		"GET /api/admin/users",
		"POST /api/admin/orgs/7?action=disable",
		"DELETE /api/orgs/7/networks/n1",
		"DELETE /api/orgs/7/networks/n2",
		"DELETE /api/orgs/7/networks",
		"DELETE /api/admin/orgs/7",
	}
	if strings.Join(*calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls = %v", *calls)
	}
}

// One network that will not delete leaves the org disabled and NOT deleted (deleting it would cascade past the network's data), and the
// failure is reported per network; a network that is already gone counts as deleted.
func TestDeleteOrgCleanlyStopsOnFailure(t *testing.T) {
	t.Parallel()
	server, calls := orgServer(t, "n2")
	client := newTestClient(t, server.URL)
	res, err := client.Organizations.DeleteOrgCleanly(context.Background(), "7", DeleteOrgOptions{Networks: []string{"n1", "n2", "n3"}})
	if err == nil || res.Org != nil || len(res.Failed) != 1 || res.Failed["n2"] == nil || !res.Disabled {
		t.Fatalf("result = %+v, %v", res, err)
	}
	for _, c := range *calls {
		if strings.HasPrefix(c, "DELETE /api/admin/orgs/") {
			t.Fatalf("the org was deleted after a network failed: %v", *calls)
		}
	}
	// n3 was still attempted after n2 failed (it is already gone: a 404), and neither the sweep nor the org delete happened.
	got := strings.Join(*calls, "|")
	if !strings.Contains(got, "DELETE /api/orgs/7/networks/n3") || strings.HasSuffix(got, "DELETE /api/orgs/7/networks") {
		t.Fatalf("calls = %s", got)
	}
	if len(res.Deleted) != 2 {
		t.Errorf("deleted = %v, want n1 and the already-gone n3", res.Deleted)
	}
	var failed *ErrorResponse
	if !errors.As(res.Failed["n2"], &failed) {
		t.Errorf("the failure lost its error: %v", res.Failed["n2"])
	}
}

// The two network routes and the refusals.
func TestDeleteNetworkInOrgAndOrgNetworks(t *testing.T) {
	t.Parallel()
	server, calls := orgServer(t, "")
	client := newTestClient(t, server.URL)
	ctx := context.Background()
	if n, _, err := client.Organizations.DeleteNetworkInOrg(ctx, "7", "n 1"); err != nil || n.ID == "" {
		t.Fatalf("delete = %+v, %v", n, err)
	}
	if _, err := client.Organizations.DeleteOrgNetworks(ctx, "7"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Organizations.DeleteNetworkInOrg(ctx, "", "n1"); err == nil {
		t.Error("a blank org was sent")
	}
	if _, err := client.Organizations.DeleteOrgNetworks(ctx, " "); err == nil {
		t.Error("a blank org was sent")
	}
	if _, err := client.Organizations.DeleteOrgCleanly(ctx, " ", DeleteOrgOptions{}); err == nil {
		t.Error("a blank org was sent")
	}
	if got := strings.Join(*calls, "|"); got != "DELETE /api/orgs/7/networks/n%201|DELETE /api/orgs/7/networks" {
		t.Fatalf("calls = %s", got)
	}
}

// Forward refuses to delete an org that still has users, after the networks are already gone; with a service client the SDK looks first
// and changes nothing.
func TestDeleteOrgCleanlyRefusesAnOrgWithUsers(t *testing.T) {
	t.Parallel()
	var seen []predictSeen
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, predictSeen{r.Method, r.URL.RequestURI(), "", ""})
		_, _ = io.WriteString(w, `[{"id":"u1","orgId":"7","username":"alice"}]`)
	}))
	defer server.Close()
	client, err := NewClient(Config{BaseURL: server.URL, AuthMode: AuthModeService, Username: "u", Password: "p", HTTPClient: privateHTTPClient()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Organizations.DeleteOrgCleanly(context.Background(), "7", DeleteOrgOptions{Networks: []string{"n1"}}); err == nil || !strings.Contains(err.Error(), "still has users") {
		t.Fatalf("err = %v", err)
	}
	for _, c := range seen {
		if c.method != "GET" {
			t.Fatalf("a change was sent before the refusal: %+v", seen)
		}
	}
}
