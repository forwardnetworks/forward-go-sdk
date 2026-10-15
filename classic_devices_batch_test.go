package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// One PATCH carries a patch for many devices (the way to change thousands
// without thousands of calls); the body is {names, update}, the patch keeps its
// presence semantics inside it (a Clear name is an explicit null), and a blank
// or empty name list fails before any request.
func TestPatchManySendsOneRequestForManyDevices(t *testing.T) {
	var method, path, body string
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		method, path, body = r.Method, r.URL.Path, string(raw)
		requests++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	devices := newTestClient(t, server.URL).ClassicDevices
	ctx := context.Background()

	_, err := devices.PatchMany(ctx, "net-1", []string{"sw1", " sw2 "}, ClassicDevicePatch{CLICredential2ID: Ptr("PM-2"), Clear: []string{"collectorId"}})
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPatch || path != "/api/networks/net-1/classic-devices" ||
		body != `{"names":["sw1","sw2"],"update":{"cliCredential2Id":"PM-2","collectorId":null}}` {
		t.Fatalf("request = %s %s %s", method, path, body)
	}
	for name, names := range map[string][]string{"no names": nil, "blank name": {"sw1", " "}} {
		if _, err := devices.PatchMany(ctx, "net-1", names, ClassicDevicePatch{}); err == nil || requests != 1 {
			t.Errorf("%s must fail before any request (err=%v, requests=%d)", name, err, requests)
		}
	}
}

func TestGetManyPostsTheNamesAndKeepsWith(t *testing.T) {
	var path, query, body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		path, query, body = r.URL.Path, r.URL.RawQuery, string(raw)
		_, _ = io.WriteString(w, `{"devices":[{"name":"sw1","host":"10.0.0.1","cliCredential2Id":"PM-2"}]}`)
	}))
	defer server.Close()
	got, _, err := newTestClient(t, server.URL).ClassicDevices.GetMany(context.Background(), "net-1", []string{"sw1", "gone"}, "tags", "testResult")
	if err != nil || len(got) != 1 || got[0].Name != "sw1" || got[0].CLICredential2ID != "PM-2" {
		t.Fatalf("got = %+v, %v", got, err)
	}
	if path != "/api/networks/net-1/classic-devices" || query != "action=getBatch&with=tags&with=testResult" || body != `{"names":["sw1","gone"]}` {
		t.Fatalf("request = %s ?%s %s", path, query, body)
	}
}
