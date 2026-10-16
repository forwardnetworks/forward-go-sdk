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

// Someone once mistook a custom command for a Forward built-in, so a group has to
// be readable with exactly what Forward stores: name, connection types, the CLI
// commands or SNMP OIDs (one kind only), the device globs, and the disabled flag.
// Get filters the list ignoring case, since Forward has no per-name read.
func TestCustomCommandGroupsListAndGet(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/networks/5/custom-command-groups" {
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
		_, _ = w.Write([]byte(`[{"name":"Edge Show","deviceConnTypes":["cisco_ios_xe_ssh"],"type":"CLI","commands":["show clock"],"devices":["edge*"],"disabled":false},
			{"name":"snmp-extra","deviceConnTypes":["cisco_ios_xe_snmp"],"type":"SNMP","oids":[{"name":"sys","oid":"1.3.6.1.2.1.1"}],"disabled":true}]`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()

	groups, _, err := client.CustomCommandGroups.List(ctx, "5")
	if err != nil || len(groups) != 2 || groups[0].Commands[0] != "show clock" || groups[0].Devices[0] != "edge*" || !groups[1].Disabled || groups[1].OIDs[0].OID != "1.3.6.1.2.1.1" {
		t.Fatalf("list: %v %+v", err, groups)
	}
	one, _, err := client.CustomCommandGroups.Get(ctx, "5", "edge show")
	if err != nil || one.Name != "Edge Show" {
		t.Fatalf("get: %v %+v", err, one)
	}
	if _, _, err := client.CustomCommandGroups.Get(ctx, "5", "nope"); !errors.Is(err, ErrCustomCommandGroupNotFound) {
		t.Fatalf("a missing group must be ErrCustomCommandGroupNotFound, got %v", err)
	}
}

// Create sends one of commands or oids, never both or neither (Forward's rule,
// refused here before the request), enabled unless told otherwise. Update is a
// PATCH where nil leaves a field alone, an empty Devices slice means "every
// device of the types", and Disabled is how a group is switched off. Delete
// treats a group that is not there as deleted but not a missing network, whose
// 404 is worded like one.
func TestCustomCommandGroupsWrites(t *testing.T) {
	t.Parallel()

	var calls, bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		bodies = append(bodies, string(body))
		switch {
		case r.Method == http.MethodPost && r.URL.RawQuery == "":
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"unsupported":[{"command":"conf t","unsupportedConnTypes":["cisco_ios_xe_ssh"]}]}`))
		case r.Method == http.MethodPatch:
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/network-checks"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"apiUrl":"/api/networks/5/custom-command-groups/network-checks","httpMethod":"DELETE","message":"Custom command group \"network-checks\" not found"}`))
		case strings.HasSuffix(r.URL.Path, "/gone"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"apiUrl":"/api/networks/5/custom-command-groups/gone","httpMethod":"DELETE","message":"Custom command group \"gone\" not found"}`))
		case r.URL.Path == "/api/networks/9/custom-command-groups/x":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"apiUrl":"/api/networks/9/custom-command-groups/x","httpMethod":"DELETE","message":"Network 9 not found"}`))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()
	groups := client.CustomCommandGroups

	if _, err := groups.Create(ctx, "5", NewCustomCommandGroup{Name: "g", DeviceConnTypes: []string{"cisco_ios_xe_ssh"}, Commands: []string{"show clock"}, Devices: []string{"edge*"}}); err != nil {
		t.Fatal(err)
	}
	off := true
	empty := []string{}
	if _, err := groups.Update(ctx, "5", "g", CustomCommandGroupUpdate{Disabled: &off, Devices: &empty}); err != nil {
		t.Fatal(err)
	}
	unsupported, _, err := groups.Validate(ctx, "5", []string{"cisco_ios_xe_ssh"}, []string{"conf t"})
	if err != nil || len(unsupported) != 1 || unsupported[0].Command != "conf t" {
		t.Fatalf("validate: %v %+v", err, unsupported)
	}
	if _, err := groups.Delete(ctx, "5", "gone"); err != nil {
		t.Fatalf("an absent group counts as deleted: %v", err)
	}
	if _, err := groups.Delete(ctx, "5", "network-checks"); err != nil {
		t.Fatalf("an absent group named like a network still counts as deleted: %v", err)
	}
	if _, err := groups.Delete(ctx, "9", "x"); !errors.Is(err, ErrNetworkNotFound) {
		t.Fatalf("a missing network must not read as a completed delete: %v", err)
	}
	want := []string{
		"POST /api/networks/5/custom-command-groups",
		"PATCH /api/networks/5/custom-command-groups/g",
		"POST /api/networks/5/custom-command-groups?action=validate",
		"DELETE /api/networks/5/custom-command-groups/gone",
		"DELETE /api/networks/5/custom-command-groups/network-checks",
		"DELETE /api/networks/9/custom-command-groups/x",
	}
	if strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls %v", calls)
	}
	if bodies[0] != `{"name":"g","deviceConnTypes":["cisco_ios_xe_ssh"],"commands":["show clock"],"devices":["edge*"]}` || bodies[1] != `{"devices":[],"disabled":true}` {
		t.Fatalf("bodies %v", bodies)
	}

	sent := len(calls)
	one := NewCustomCommandGroup{Name: "g", DeviceConnTypes: []string{"t"}, Commands: []string{"c"}, OIDs: []CustomOID{{Name: "n", OID: "1.2"}}}
	for name, call := range map[string]func() error{
		"both kinds": func() error { _, err := groups.Create(ctx, "5", one); return err },
		"neither": func() error {
			_, err := groups.Create(ctx, "5", NewCustomCommandGroup{Name: "g", DeviceConnTypes: []string{"t"}})
			return err
		},
		"no types": func() error {
			_, err := groups.Create(ctx, "5", NewCustomCommandGroup{Name: "g", Commands: []string{"c"}})
			return err
		},
		"no name":  func() error { _, err := groups.Update(ctx, "5", " ", CustomCommandGroupUpdate{}); return err },
		"dot name": func() error { _, err := groups.Delete(ctx, "5", ".."); return err },
		"update both": func() error {
			_, err := groups.Update(ctx, "5", "g", CustomCommandGroupUpdate{Commands: []string{"c"}, OIDs: []CustomOID{{Name: "n", OID: "1"}}})
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
}

// A group's Devices are globs; ResolveGlobs answers which classic devices of the
// given types each one matches, as a read, so an audit can say where a group's
// commands run.
func TestClassicDevicesResolveGlobs(t *testing.T) {
	t.Parallel()

	var gotURL, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.Method + " " + r.URL.RequestURI()
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_, _ = w.Write([]byte(`{"globToResolvedDevices":{"edge*":["edge1","edge2"],"core*":[]}}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	resolved, _, err := client.ClassicDevices.ResolveGlobs(context.Background(), "5", []string{"edge*", "core*"}, []string{"cisco_ios_xe_ssh"})
	if err != nil || len(resolved["edge*"]) != 2 || len(resolved["core*"]) != 0 {
		t.Fatalf("%v %+v", err, resolved)
	}
	if gotURL != "POST /api/networks/5/classic-devices?action=resolveGlobs" || gotBody != `{"connTypes":["cisco_ios_xe_ssh"],"globs":["edge*","core*"]}` {
		t.Fatalf("%s %s", gotURL, gotBody)
	}
	if _, _, err := client.ClassicDevices.ResolveGlobs(context.Background(), "5", []string{" "}, []string{"t"}); err == nil {
		t.Fatal("a blank glob must be refused")
	}
}
