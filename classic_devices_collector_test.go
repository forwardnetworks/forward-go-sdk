package forward

import (
	"encoding/json"
	"testing"
)

// Spreading a large lab over several collectors needs a per-device collector.
// Forward carries it as collectorId on the device object (DeviceSetupMeta is
// unwrapped into both the PUT body and the GET response). Omitting it must send
// no field, so the appserver keeps the network's default collector.
func TestClassicDeviceRequestSendsCollectorIDOnlyWhenSet(t *testing.T) {
	set, err := json.Marshal(ClassicDeviceRequest{Name: "a", Host: "h", CollectorID: "42"})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(set, &got); err != nil {
		t.Fatal(err)
	}
	if got["collectorId"] != "42" {
		t.Fatalf("collectorId = %v, want 42", got["collectorId"])
	}
	unset, _ := json.Marshal(ClassicDeviceRequest{Name: "a", Host: "h"})
	got = nil
	_ = json.Unmarshal(unset, &got)
	if _, present := got["collectorId"]; present {
		t.Fatalf("an unset CollectorID was sent as %v; absent must stay absent", got["collectorId"])
	}
}

func TestClassicDeviceBatchItemSendsCollectorIDOnlyWhenSet(t *testing.T) {
	b, _ := json.Marshal([]ClassicDeviceBatchItem{{Name: "a", Host: "h", CollectorID: "7"}, {Name: "b", Host: "h"}})
	var items []map[string]any
	if err := json.Unmarshal(b, &items); err != nil {
		t.Fatal(err)
	}
	if items[0]["collectorId"] != "7" {
		t.Fatalf("collectorId = %v, want 7", items[0]["collectorId"])
	}
	if _, present := items[1]["collectorId"]; present {
		t.Fatalf("an unset CollectorID was sent; absent must stay absent")
	}
}

// The appserver serialises CollectorId as "C<n>"; a read must keep that and also accept
// a bare number or string, and an absent field must read as empty rather than as an error.
func TestClassicDeviceReadsCollectorID(t *testing.T) {
	for body, want := range map[string]Identifier{
		`{"name":"a","host":"h","collectorId":42}`:    "42",
		`{"name":"a","host":"h","collectorId":"42"}`:  "42",
		`{"name":"a","host":"h","collectorId":"C42"}`: "C42",
		`{"name":"a","host":"h"}`:                     "",
		`{"name":"a","host":"h","collectorId":null}`:  "",
	} {
		var d ClassicDevice
		if err := json.Unmarshal([]byte(body), &d); err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		if d.CollectorID != want {
			t.Fatalf("%s: CollectorID = %q, want %q", body, d.CollectorID, want)
		}
	}
}

// An endpoint create must carry collectorId for every type (each New*NetworkEndpoint
// binds it), and a patch must send it only when stated.
func TestEndpointBodiesCarryCollectorIDOnlyWhenSet(t *testing.T) {
	for _, typ := range []string{"CLI", "SNMP", "HTTP"} {
		body, err := endpointBody(typ, Endpoint{Name: "e", Host: "h", CollectorID: "C5"})
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(body)
		var got map[string]any
		_ = json.Unmarshal(b, &got)
		if got["collectorId"] != "C5" {
			t.Fatalf("%s: collectorId = %v, want C5", typ, got["collectorId"])
		}
		body, _ = endpointBody(typ, Endpoint{Name: "e", Host: "h"})
		b, _ = json.Marshal(body)
		got = nil
		_ = json.Unmarshal(b, &got)
		if _, present := got["collectorId"]; present {
			t.Fatalf("%s: an unset CollectorID was sent", typ)
		}
	}
	id := "9"
	b, _ := json.Marshal(EndpointPatch{CollectorID: &id})
	if string(b) != `{"collectorId":"9"}` {
		t.Fatalf("patch body = %s", b)
	}
	if b, _ := json.Marshal(EndpointPatch{}); string(b) != `{}` {
		t.Fatalf("empty patch body = %s, want {}", b)
	}
}
