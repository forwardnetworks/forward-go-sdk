package forward

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Each alias type sends only its own fields, under the names Forward's
// AliasBuilder subtypes bind; HEADERS sends its map as "values".
func TestAliasBuilderBodies(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		alias AliasBuilder
		want  string
	}{
		{"hosts", AliasBuilder{Name: "web_servers", Type: AliasTypeHosts, Values: []string{"10.30.1.0/24"}, Locations: []string{"tor_switches"}},
			`{"locations":["tor_switches"],"name":"web_servers","type":"HOSTS","values":["10.30.1.0/24"]}`},
		{"hosts by location only", AliasBuilder{Name: "h", Type: AliasTypeHosts, Locations: []string{"d1 eth0"}},
			`{"locations":["d1 eth0"],"name":"h","type":"HOSTS"}`},
		{"devices", AliasBuilder{Name: "border_routers", Type: AliasTypeDevices, Values: []string{"bbr?_rtr"}},
			`{"name":"border_routers","type":"DEVICES","values":["bbr?_rtr"]}`},
		{"interfaces", AliasBuilder{Name: "vlan_20_to_29_access", Type: AliasTypeInterfaces, VLANIDs: []string{"20-29"}, VLANIntfTypes: []string{"ACCESS"}, IsExposurePoint: Ptr(false)},
			`{"isExposurePoint":false,"name":"vlan_20_to_29_access","type":"INTERFACES","vlanIds":["20-29"],"vlanIntfTypes":["ACCESS"]}`},
		{"headers", AliasBuilder{Name: "VOIP", Type: AliasTypeHeaders, HeaderValues: map[string][]string{"ip_proto": {"UDP"}, "tp_port": {"10000", "20000"}}},
			`{"name":"VOIP","type":"HEADERS","values":{"ip_proto":["UDP"],"tp_port":["10000","20000"]}}`},
		{"logical network", AliasBuilder{Name: "enclave1", Type: AliasTypeLogicalNetwork, Devices: []string{"d1", "d2*"}, EdgeNodes: []string{"en1"}},
			`{"devices":["d1","d2*"],"edgeNodes":["en1"],"name":"enclave1","type":"LOGICAL_NETWORK"}`},
	}
	for _, tc := range cases {
		got, err := json.Marshal(tc.alias)
		if err != nil || string(got) != tc.want {
			t.Errorf("%s:\n got %s (%v)\nwant %s", tc.name, got, err, tc.want)
		}
	}
}

func TestAliasBuilderRefusals(t *testing.T) {
	t.Parallel()

	for name, alias := range map[string]AliasBuilder{
		"no name":                   {Type: AliasTypeDevices, Values: []string{"d"}},
		"unknown type":              {Name: "a", Type: "HOSTFILTER"},
		"hosts with nothing":        {Name: "a", Type: AliasTypeHosts},
		"headers with nothing":      {Name: "a", Type: AliasTypeHeaders},
		"headers with a field name": {Name: "a", Type: AliasTypeHeaders, HeaderValues: map[string][]string{"ipv4_src": {"1.1.1.1"}}},
		"headers in upper case":     {Name: "a", Type: AliasTypeHeaders, HeaderValues: map[string][]string{"IP_ADDR": {"1.1.1.1"}}},
		"locations on devices":      {Name: "a", Type: AliasTypeDevices, Values: []string{"d"}, Locations: []string{"x"}},
		"vlans on hosts":            {Name: "a", Type: AliasTypeHosts, Values: []string{"h"}, VLANIDs: []string{"10"}},
		"edge nodes on interfaces":  {Name: "a", Type: AliasTypeInterfaces, Values: []string{"d p"}, EdgeNodes: []string{"e"}},
		"bad vlan interface type":   {Name: "a", Type: AliasTypeInterfaces, VLANIDs: []string{"10"}, VLANIntfTypes: []string{"access"}},
		"header values on logical":  {Name: "a", Type: AliasTypeLogicalNetwork, HeaderValues: map[string][]string{"ip_addr": {"1.1.1.1"}}},
	} {
		if _, err := json.Marshal(alias); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}

func TestAliasesGetPutDeactivate(t *testing.T) {
	t.Parallel()

	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.RequestURI()+" "+string(b))
		switch {
		case r.URL.Path == "/api/snapshots/9/aliases/gone":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPut:
			_, _ = io.WriteString(w, `{"name":"border routers","type":"DEVICES","createdAt":"2026-10-02T10:00:00Z","creatorId":"12","values":["bbr?_rtr"]}`)
		case r.Method == http.MethodDelete:
			_, _ = io.WriteString(w, `{"name":"border routers","type":"DEVICES","createdAt":"2026-10-02T10:00:00Z","creatorId":"12"}`)
		default:
			_, _ = io.WriteString(w, `{"name":"border routers","type":"DEVICES","createdAt":"2026-10-02T10:00:00Z","creatorId":"12","values":["bbr?_rtr"],"resolvedValue":{"devices":["bbra_rtr"]}}`)
		}
	}))
	defer server.Close()
	aliases := newTestClient(t, server.URL).Aliases
	ctx := context.Background()

	put, _, err := aliases.Put(ctx, "9", AliasBuilder{Name: "border routers", Type: AliasTypeDevices, Values: []string{"bbr?_rtr"}})
	if err != nil || put.Type != AliasTypeDevices || put.CreatorID != "12" || calls[0] != `PUT /api/snapshots/9/aliases/border%20routers {"name":"border routers","type":"DEVICES","values":["bbr?_rtr"]}` {
		t.Fatalf("Put() = %+v, %v; %q", put, err, calls[0])
	}
	got, _, err := aliases.Get(ctx, "9", "border routers")
	if err != nil || got.Name != "border routers" || string(got.Definition) == "" {
		t.Fatalf("Get() = %+v, %v", got, err)
	}
	var def map[string]any
	_ = json.Unmarshal(got.Definition, &def)
	if def["resolvedValue"] == nil {
		t.Fatalf("Get must keep the resolved value in Definition: %v", def)
	}
	if gone, _, err := aliases.Get(ctx, "9", "gone"); gone != nil || err != nil {
		t.Fatalf("absent Get() = %+v, %v", gone, err)
	}
	was, _, err := aliases.Deactivate(ctx, "9", "border routers")
	if err != nil || was.Name != "border routers" || calls[len(calls)-1] != "DELETE /api/snapshots/9/aliases/border%20routers " {
		t.Fatalf("Deactivate() = %+v, %v; %q", was, err, calls[len(calls)-1])
	}
	if was, _, err := aliases.Deactivate(ctx, "9", "gone"); was != nil || err != nil {
		t.Fatalf("deactivating an inactive alias must succeed: %+v, %v", was, err)
	}
}

// DELETE on the collection route deactivates EVERY alias, so a blank name
// must never collapse onto it.
func TestAliasesBlankNameNeverReachesTheCollection(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer server.Close()
	aliases := newTestClient(t, server.URL).Aliases
	ctx := context.Background()

	if _, _, err := aliases.Deactivate(ctx, "9", " "); err == nil {
		t.Fatal("a blank alias name must be refused")
	}
	if _, _, err := aliases.Get(ctx, "9", ""); err == nil {
		t.Fatal("a blank alias name must be refused")
	}
	if _, _, err := aliases.Put(ctx, "9", AliasBuilder{Type: AliasTypeDevices, Values: []string{"d"}}); err == nil {
		t.Fatal("a blank alias name must be refused")
	}
	if _, _, err := aliases.Deactivate(ctx, " ", "a"); err == nil || calls != 0 {
		t.Fatalf("a blank snapshot ID must be refused, and nothing may be sent (calls=%d)", calls)
	}
}
