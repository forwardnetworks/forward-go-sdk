package forward

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
)

// Forward binds each endpoint type to its own class and rejects any field it
// does not declare: an SNMP endpoint sent "protocol" answered 400
// 'Unrecognized field "protocol" (class SnmpNetworkEndpoint)'. A batch carries
// exactly the published fields of its type, "type" (the discriminator)
// included, and an empty CLI protocol is omitted rather than sent as "".
func TestEndpointsAddBatchSendsOnlyTheTypesFields(t *testing.T) {
	t.Parallel()

	var bodies []map[string]any
	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		b, _ := io.ReadAll(r.Body)
		bodies = nil
		_ = json.Unmarshal(b, &bodies)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL).ForNetwork("n1")
	keys := func(m map[string]any) []string {
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	yes := true

	if _, err := client.Endpoints.AddBatch(context.Background(), "", "snmp", []Endpoint{{Name: "printer1", Host: "10.0.0.9", Port: 161, CredentialID: "S1", ProfileID: "SNMP-5", FullCollect: true}}); err != nil {
		t.Fatal(err)
	}
	if query != "action=addBatch&type=SNMP" || len(bodies) != 1 || bodies[0]["type"] != "SNMP" {
		t.Fatalf("query %q, bodies %v", query, bodies)
	}
	if got := keys(bodies[0]); !equalStrings(got, []string{"credentialId", "fullCollectionLog", "host", "name", "port", "profileId", "type"}) {
		t.Fatalf("SNMP keys = %v", got)
	}

	if _, err := client.Endpoints.AddBatch(context.Background(), "", "CLI", []Endpoint{{Name: "r1", Host: "10.0.0.1", Protocol: "SSH", JumpServerID: "J1"}, {Name: "r2", Host: "10.0.0.2"}}); err != nil {
		t.Fatal(err)
	}
	if bodies[0]["protocol"] != "SSH" || bodies[0]["jumpServerId"] != "J1" {
		t.Fatalf("CLI body = %v", bodies[0])
	}
	if _, ok := bodies[1]["protocol"]; ok {
		t.Fatalf("an empty CLI protocol must be omitted, not sent: %v", bodies[1])
	}

	if _, err := client.Endpoints.AddBatch(context.Background(), "", "HTTP", []Endpoint{{Name: "nsx", Host: "nsx.example.com", DisableSSLValidation: &yes}}); err != nil {
		t.Fatal(err)
	}
	if got := keys(bodies[0]); !equalStrings(got, []string{"disableSslValidation", "host", "name", "type"}) || bodies[0]["disableSslValidation"] != true {
		t.Fatalf("HTTP body = %v", bodies[0])
	}
}

// A field the type does not have is a caller mistake: refused with no request
// rather than silently dropped or sent into a 400.
func TestEndpointsWritesRefuseFieldsOfOtherTypes(t *testing.T) {
	t.Parallel()

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL).ForNetwork("n1")
	yes, ssh, snmpProfile := true, "SSH", "SNMP-5"

	for name, endpoints := range map[string]struct {
		kind string
		list []Endpoint
	}{
		"SNMP with protocol":        {"SNMP", []Endpoint{{Name: "p", Host: "h", Protocol: "SSH"}}},
		"SNMP with a jump server":   {"SNMP", []Endpoint{{Name: "p", Host: "h", JumpServerID: "J1"}}},
		"HTTP with full collection": {"HTTP", []Endpoint{{Name: "p", Host: "h", FullCollect: true}}},
		"CLI with SSL validation":   {"CLI", []Endpoint{{Name: "p", Host: "h", DisableSSLValidation: &yes}}},
		"type mismatch":             {"SNMP", []Endpoint{{Type: "CLI", Name: "p", Host: "h"}}},
		"unknown type":              {"TELNET", []Endpoint{{Name: "p", Host: "h"}}},
	} {
		if _, err := client.Endpoints.AddBatch(context.Background(), "", endpoints.kind, endpoints.list); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := client.Endpoints.Patch(context.Background(), "", "printer1", "SNMP", EndpointPatch{Protocol: &ssh}); err == nil {
		t.Error("an SNMP patch with protocol was accepted")
	}
	if calls != 0 {
		t.Fatalf("a refused write still sent %d request(s)", calls)
	}
	if _, err := client.Endpoints.Patch(context.Background(), "", "printer1", "SNMP", EndpointPatch{ProfileID: &snmpProfile}); err != nil || calls != 1 {
		t.Fatalf("repointing an SNMP endpoint's profile must go through: %v (%d calls)", err, calls)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
