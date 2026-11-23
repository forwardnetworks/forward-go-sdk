package rest

import (
	"encoding/json"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"testing"
)

// An endpoint create must carry collectorId for every type (each New*NetworkEndpoint
// binds it), and a patch must send it only when stated.
func TestEndpointBodiesCarryCollectorIDOnlyWhenSet(t *testing.T) {
	for _, typ := range []string{"CLI", "SNMP", "HTTP"} {
		body, err := endpointBody(typ, ports.Endpoint{Name: "e", Host: "h", CollectorID: "C5"})
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(body)
		var got map[string]any
		_ = json.Unmarshal(b, &got)
		if got["collectorId"] != "C5" {
			t.Fatalf("%s: collectorId = %v, want C5", typ, got["collectorId"])
		}
		body, _ = endpointBody(typ, ports.Endpoint{Name: "e", Host: "h"})
		b, _ = json.Marshal(body)
		got = nil
		_ = json.Unmarshal(b, &got)
		if _, present := got["collectorId"]; present {
			t.Fatalf("%s: an unset CollectorID was sent", typ)
		}
	}
	id := "9"
	b, _ := json.Marshal(ports.EndpointPatch{CollectorID: &id})
	if string(b) != `{"collectorId":"9"}` {
		t.Fatalf("patch body = %s", b)
	}
	if b, _ := json.Marshal(ports.EndpointPatch{}); string(b) != `{}` {
		t.Fatalf("empty patch body = %s, want {}", b)
	}
}
