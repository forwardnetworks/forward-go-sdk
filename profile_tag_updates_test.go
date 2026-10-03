package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The PATCH body carries only what is set. A cleared setting is an explicit
// null, which is the only way to say "remove it"; omitting leaves it as is.
func TestEndpointsUpdateProfileBodies(t *testing.T) {
	t.Parallel()

	var method, uri, body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		method, uri, body = r.Method, r.URL.RequestURI(), string(b)
		_, _ = io.WriteString(w, `{"id":"SNMP-4","name":"core","type":"SNMP","customOids":[{"name":"x","oid":"1.3.6"}],"updatedBy":"mary"}`)
	}))
	defer server.Close()
	endpoints := newTestClient(t, server.URL).Endpoints
	ctx := context.Background()

	got, _, err := endpoints.UpdateProfile(ctx, "SNMP-4", EndpointProfilePatch{
		ResponseTimeoutSec: Ptr(20), OIDSets: []string{"SYSTEM"}, CustomOIDs: []CustomOID{{Name: "x", OID: "1.3.6"}},
		Clear: []string{"nameDetectorOid"},
	})
	if err != nil || got.ID != "SNMP-4" || got.UpdatedBy != "mary" || method != http.MethodPatch || uri != "/api/endpoint-profiles/SNMP-4" {
		t.Fatalf("UpdateProfile() = %+v, %v; %s %s", got, err, method, uri)
	}
	if body != `{"customOids":[{"name":"x","oid":"1.3.6"}],"nameDetectorOid":null,"oidSets":["SYSTEM"],"responseTimeoutSec":20}` {
		t.Fatalf("snmp body = %s", body)
	}
	if _, _, err := endpoints.UpdateProfile(ctx, "CLI-1", EndpointProfilePatch{CustomCommands: []string{}, Prompt: Ptr("#")}); err != nil ||
		body != `{"customCommands":[],"prompt":"#"}` {
		t.Fatalf("an empty non-nil list is a real value (empties the list): %v %s", err, body)
	}
	if _, _, err := endpoints.UpdateProfile(ctx, "HTTP-2", EndpointProfilePatch{HTTPS: Ptr(false), Headers: map[string]string{"accept": "application/json"}, Endpoints: []HTTPEndpoint{{Name: "s", Path: "/s"}}}); err != nil ||
		body != `{"endpoints":[{"name":"s","path":"/s"}],"headers":{"accept":"application/json"},"https":false}` {
		t.Fatalf("http body: %v %s", err, body)
	}
}

func TestEndpointsUpdateProfileRefusals(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer server.Close()
	endpoints := newTestClient(t, server.URL).Endpoints

	for name, c := range map[string]struct {
		id    string
		patch EndpointProfilePatch
	}{
		"snmp field on a cli profile":   {"CLI-1", EndpointProfilePatch{DetectorOID: Ptr("1.3")}},
		"cli field on an http profile":  {"HTTP-1", EndpointProfilePatch{Prompt: Ptr("#")}},
		"http field on an snmp profile": {"SNMP-1", EndpointProfilePatch{HTTPS: Ptr(true)}},
		"clearing a foreign field":      {"CLI-1", EndpointProfilePatch{Clear: []string{"oidSets"}}},
		"clearing https":                {"HTTP-1", EndpointProfilePatch{Clear: []string{"https"}}},
		"clearing the name":             {"CLI-1", EndpointProfilePatch{Clear: []string{"name"}}},
		"set and cleared":               {"CLI-1", EndpointProfilePatch{Prompt: Ptr("#"), Clear: []string{"prompt"}}},
		"empty patch":                   {"CLI-1", EndpointProfilePatch{}},
		"unknown profile type":          {"FTP-1", EndpointProfilePatch{Name: Ptr("x")}},
		"no type prefix":                {"7", EndpointProfilePatch{Name: Ptr("x")}},
		"blank id":                      {" ", EndpointProfilePatch{Name: Ptr("x")}},
	} {
		if _, _, err := endpoints.UpdateProfile(context.Background(), c.id, c.patch); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if calls != 0 {
		t.Fatalf("refused patches reached the wire %d times", calls)
	}
}

// Forward documents tag PATCH as "creates or updates"; the SDK passes the
// change through, validates the colour, and treats deleting a missing tag as
// done.
func TestDeviceTagsUpdateAndDelete(t *testing.T) {
	t.Parallel()

	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.EscapedPath()+" "+string(b))
		switch {
		case r.URL.Path == "/api/networks/N1/device-tags/gone":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPatch:
			_, _ = io.WriteString(w, `{"name":"edge-2","color":"#00ff00"}`)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	tags := newTestClient(t, server.URL).DeviceTags
	ctx := context.Background()

	got, _, err := tags.UpdateTag(ctx, "N1", "edge 1", DeviceTagPatch{Name: Ptr("edge-2"), Color: Ptr("#00ff00")})
	if err != nil || got.Name != "edge-2" || got.Color != "#00ff00" || calls[0] != `PATCH /api/networks/N1/device-tags/edge%201 {"color":"#00ff00","name":"edge-2"}` {
		t.Fatalf("UpdateTag() = %+v, %v; %q", got, err, calls[0])
	}
	if _, err := tags.DeleteTag(ctx, "N1", "edge-2"); err != nil || calls[1] != "DELETE /api/networks/N1/device-tags/edge-2 " {
		t.Fatalf("DeleteTag: %v %q", err, calls[1])
	}
	if _, err := tags.DeleteTag(ctx, "N1", "gone"); err != nil {
		t.Fatalf("deleting a missing tag must be success: %v", err)
	}
	before := len(calls)
	for name, f := range map[string]func() error{
		"empty patch": func() error { _, _, err := tags.UpdateTag(ctx, "N1", "t", DeviceTagPatch{}); return err },
		"short colour": func() error {
			_, _, err := tags.UpdateTag(ctx, "N1", "t", DeviceTagPatch{Color: Ptr("#fff")})
			return err
		},
		"no hash": func() error {
			_, _, err := tags.UpdateTag(ctx, "N1", "t", DeviceTagPatch{Color: Ptr("00ff00")})
			return err
		},
		"blank rename": func() error { _, _, err := tags.UpdateTag(ctx, "N1", "t", DeviceTagPatch{Name: Ptr(" ")}); return err },
		"blank tag":    func() error { _, _, err := tags.UpdateTag(ctx, "N1", " ", DeviceTagPatch{Name: Ptr("x")}); return err },
		"blank delete": func() error { _, err := tags.DeleteTag(ctx, "N1", ""); return err },
	} {
		if err := f(); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if len(calls) != before {
		t.Fatalf("refused calls reached the wire: %q", calls[before:])
	}
}
