package forward

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Each typed read must hit its kind's route and decode the JSON of Forward's record for it. The bodies below are written from the Java
// records (ScopedAddressObject, ScopedAddressGroup, ScopedApplicationObject, ScopedApplicationGroup, ScopedUrlObject, ScopedServiceObject,
// ScopedServiceGroup, ScopedUserObject, SecurityZone and their *Collection wrappers), not from the SDK's structs. A collection with nothing
// in it is {} because Forward's NON_EMPTY inclusion drops the list, and must read as an empty list, not an error.
func TestTypedFirewallObjectReads(t *testing.T) {
	bodies := map[string]string{
		"address-objects":     `{"addressObjects":[{"scopeId":"LOCAL","name":"web","description":"d","tags":["t1"],"subnets":["10.0.0.0/24"],"ipRanges":[{"start":"10.1.0.1","end":"10.1.0.9"}],"domains":["example.com"]}]}`,
		"address-groups":      `{"addressGroups":[{"scopeId":"LOCAL","name":"g","description":"","tags":[],"memberObjects":[{"scopeId":"LOCAL","name":"web","objectId":"o1"}],"memberGroups":[]}]}`,
		"application-objects": `{"applicationObjects":[{"scopeId":"PREDEFINED","name":"postgres","description":"","defaultServiceDefinitions":[{"protocolName":"TCP","sourcePorts":[],"destinationPorts":[{"start":5432,"end":5432}]}]}]}`,
		"application-groups":  `{"applicationGroups":[{"scopeId":"LOCAL","name":"ag","memberObjects":[],"memberDynamicGroups":[{"scopeId":"LOCAL","name":"dyn","objectId":""}],"memberGroups":[]}]}`,
		"url-objects":         `{"urlObjects":[{"scopeId":"LOCAL","name":"u","members":["*.example.com"]}]}`,
		"service-objects":     `{"serviceObjects":[{"scopeId":"LOCAL","name":"s","description":"","definitions":[{"protocolName":"UDP","sourcePorts":[{"start":1,"end":65535}],"destinationPorts":[{"start":53,"end":53}]}]}]}`,
		"service-groups":      `{"serviceGroups":[{"scopeId":"LOCAL","name":"sg","memberObjects":[{"scopeId":"LOCAL","name":"s","objectId":""}],"memberGroups":[]}]}`,
		"user-objects":        `{"userObjects":[{"scopeId":"LOCAL","name":"alice"}]}`,
		"zones":               `{"zones":[{"name":"trust","interfaces":["ethernet1/1","ethernet1/2"]}]}`,
	}
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		kind := r.URL.Path[len("/api/networks/n1/change-sets/c1/devices/fw1/"):]
		body, ok := bodies[kind]
		if !ok {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	ctx := context.Background()
	p := newTestClient(t, server.URL).Predict

	addr, _, err := p.ListAddressObjects(ctx, "n1", "c1", "fw1")
	if err != nil || len(addr) != 1 || addr[0].Name != "web" || addr[0].Subnets[0] != "10.0.0.0/24" || addr[0].IPRanges[0].End != "10.1.0.9" || addr[0].Domains[0] != "example.com" || addr[0].Tags[0] != "t1" {
		t.Fatalf("address objects = %+v err = %v", addr, err)
	}
	ag, _, err := p.ListAddressGroups(ctx, "n1", "c1", "fw1")
	if err != nil || len(ag) != 1 || ag[0].MemberObjects[0].ObjectID != "o1" || ag[0].MemberObjects[0].ScopeID != "LOCAL" {
		t.Fatalf("address groups = %+v err = %v", ag, err)
	}
	app, _, err := p.ListApplicationObjects(ctx, "n1", "c1", "fw1")
	if err != nil || len(app) != 1 || app[0].DefaultServiceDefinitions[0].DestinationPorts[0].Start != 5432 || app[0].DefaultServiceDefinitions[0].ProtocolName != "TCP" {
		t.Fatalf("application objects = %+v err = %v", app, err)
	}
	appg, _, err := p.ListApplicationGroups(ctx, "n1", "c1", "fw1")
	if err != nil || len(appg) != 1 || appg[0].MemberDynamicGroups[0].Name != "dyn" {
		t.Fatalf("application groups = %+v err = %v", appg, err)
	}
	urls, _, err := p.ListURLObjects(ctx, "n1", "c1", "fw1")
	if err != nil || len(urls) != 1 || urls[0].Members[0] != "*.example.com" {
		t.Fatalf("url objects = %+v err = %v", urls, err)
	}
	svc, _, err := p.ListServiceObjects(ctx, "n1", "c1", "fw1")
	if err != nil || len(svc) != 1 || svc[0].Definitions[0].ProtocolName != "UDP" || svc[0].Definitions[0].SourcePorts[0].End != 65535 {
		t.Fatalf("service objects = %+v err = %v", svc, err)
	}
	sg, _, err := p.ListServiceGroups(ctx, "n1", "c1", "fw1")
	if err != nil || len(sg) != 1 || sg[0].MemberObjects[0].Name != "s" {
		t.Fatalf("service groups = %+v err = %v", sg, err)
	}
	users, _, err := p.ListUserObjects(ctx, "n1", "c1", "fw1")
	if err != nil || len(users) != 1 || users[0].Name != "alice" {
		t.Fatalf("user objects = %+v err = %v", users, err)
	}
	zones, _, err := p.ListFirewallZones(ctx, "n1", "c1", "fw1")
	if err != nil || len(zones) != 1 || zones[0].Name != "trust" || len(zones[0].Interfaces) != 2 {
		t.Fatalf("zones = %+v err = %v", zones, err)
	}
	if len(paths) != 9 {
		t.Fatalf("expected one request per kind, got %v", paths)
	}

	// Forward drops an empty list (NON_EMPTY): {} is an empty collection.
	for kind := range bodies {
		bodies[kind] = `{}`
	}
	if got, _, err := p.ListURLObjects(ctx, "n1", "c1", "fw1"); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("an empty collection must be an empty, non-nil list: %#v %v", got, err)
	}
	if got, _, err := p.ListFirewallZones(ctx, "n1", "c1", "fw1"); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty zones: %#v %v", got, err)
	}
	if _, _, err := p.ListAddressObjects(ctx, "n1", "c1", " "); err == nil {
		t.Fatal("a blank device name must be refused before any request")
	}
}
