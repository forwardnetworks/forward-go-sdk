package forward

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// The "devices need to be migrated" count joins a device's configured type, or
// the type its connectivity test discovered, against the connection-type list,
// and counts a Cisco IOS XE device only when the test found controllers. So a
// test result's discoveredType and controllerInfos must survive decoding, and a
// device with no test result must leave them empty rather than inventing any.
func TestSourceTestStatusDiscoveredTypeAndControllers(t *testing.T) {
	t.Parallel()

	var got SourceTestStatus
	body := `{"name":"edge1","type":"cisco_ios_xe_ssh","testResult":{"discoveredType":"cisco_ios_xe_ssh","controllerInfos":[{"name":"vmanage1","ipAddress":"10.0.0.5"},{"ipAddress":"10.0.0.6"}]}}`
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	want := []ControllerInfo{{Name: "vmanage1", IPAddress: "10.0.0.5"}, {IPAddress: "10.0.0.6"}}
	if got.Type != "cisco_ios_xe_ssh" || got.DiscoveredType != "cisco_ios_xe_ssh" || !reflect.DeepEqual(got.ControllerInfos, want) {
		t.Fatalf("decoded %+v", got)
	}

	var bare SourceTestStatus
	if err := json.Unmarshal([]byte(`{"name":"edge2","type":"arista_eos_ssh"}`), &bare); err != nil {
		t.Fatal(err)
	}
	if bare.Type != "arista_eos_ssh" || bare.DiscoveredType != "" || len(bare.ControllerInfos) != 0 {
		t.Fatalf("no test result must leave the discovery fields empty: %+v", bare)
	}
}

// Forward answers GET /api/deviceConnTypes with a map keyed by type id, and
// lists INTERNAL types only for ?for=ui, so ConnTypes always sends for=ui and
// turns the map into a sorted list carrying each id.
func TestClassicDevicesConnTypes(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/deviceConnTypes" || r.URL.RawQuery != "for=ui" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
		}
		_, _ = w.Write([]byte(`{
			"cisco_apic_api":{"name":"Cisco APIC","vendor":"CISCO","status":"PRODUCTION","protocol":"HTTPS","httpCredentialType":"LOGIN","collectionSourceType":"CONTROLLER_MANAGED","relatedTypes":["cisco_nxos_aci_ssh"]},
			"arista_eos_ssh":{"name":"Arista EOS","vendor":"ARISTA","status":"PRODUCTION","protocol":"SSH","collectionSourceType":"CLASSIC"}
		}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	types, _, err := client.ClassicDevices.ConnTypes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(types) != 2 || types[0].ID != "arista_eos_ssh" || types[1].ID != "cisco_apic_api" {
		t.Fatalf("want two types sorted by id, got %+v", types)
	}
	apic := types[1]
	if apic.Vendor != "CISCO" || apic.CollectionSourceType != "CONTROLLER_MANAGED" || !reflect.DeepEqual(apic.RelatedTypes, []string{"cisco_nxos_aci_ssh"}) {
		t.Fatalf("decoded %+v", apic)
	}
}
