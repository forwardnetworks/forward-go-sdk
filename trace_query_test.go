package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// FacetsQuery sends the GUI's PathQuery as the "filters" of the trace-facets
// body: from, chain (transitType lower case), to, mode, with every location as a
// typed filter object and every header filter as a PacketFilter. An interface
// is "<device> <interface>" with a SPACE, the form Forward accepted live (the
// comma form was refused). The query string carries the snapshot and the facet
// cap, and TraceQuery-based Facets is untouched.
func TestTraceFacetsQuerySendsThePathQuery(t *testing.T) {
	t.Parallel()

	var uri, body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		uri, body = r.Method+" "+r.URL.RequestURI(), string(b)
		if r.URL.Query().Get("snapshotId") == "ambiguous" {
			// An ambiguous address: Forward answers with the candidates instead of paths, and no totalResults.
			_, _ = io.WriteString(w, `{"timedOut":false,"pagedResults":0,"possibleIpLocations":{"to":{"type":"HOST","locations":[{"device":"d1","subnet":"10.0.0.0/24"},{"device":"d2","subnet":"10.0.0.0/24"}]}}}`)
			return
		}
		_, _ = io.WriteString(w, `{"facets":{"pathGroups":[{"traceType":"VALID","paths":[{"path":["n1","n2"]}]}],"outcomes":{"VALID":["x"]}},"timedOut":false,"pagedResults":2,"totalResults":{"value":2,"type":"EXACT"}}`)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	facets, _, err := client.Trace.FacetsQuery(context.Background(), "net1", "s1", TracePathQuery{
		From: &TraceEndpoint{Location: TraceInterface("n1", "eth1/1"), Headers: []TraceHeaders{{"ipv4_dst": {"10.0.0.2"}}}},
		Chain: []TraceChainHop{
			{Transit: TraceTransitThrough, Location: TraceDevice("core")},
		},
		To:   &TraceEndpoint{Location: TraceInterface("n2", "eth1/1"), Headers: []TraceHeaders{{"ip_proto": {"6"}}}},
		Mode: TraceModePermitAll,
	}, TraceFacetsOptions{MaxFacetValues: 5})
	if err != nil {
		t.Fatal(err)
	}
	wantBody := `{"filters":{"chain":[{"location":{"type":"DeviceFilter","values":["core"]},"transitType":"through"}],` +
		`"from":{"headers":[{"type":"PacketFilter","values":{"ipv4_dst":["10.0.0.2"]}}],"location":{"type":"InterfaceFilter","values":["n1 eth1/1"]}},` +
		`"mode":"PERMIT_ALL",` +
		`"to":{"headers":[{"type":"PacketFilter","values":{"ip_proto":["6"]}}],"location":{"type":"InterfaceFilter","values":["n2 eth1/1"]}}}}`
	if uri != "POST /api/networks/net1/trace-facets?maxFacetValues=5&snapshotId=s1" || body != wantBody {
		t.Fatalf("sent %s\n%s\nwant body\n%s", uri, body, wantBody)
	}
	if len(facets.PathGroups) != 1 || facets.PathGroups[0].TraceType != "VALID" {
		t.Fatalf("path groups = %+v", facets.PathGroups)
	}

	total, ok, err := facets.Total()
	if err != nil || !ok || total.Value != 2 || total.Type != "EXACT" {
		t.Fatalf("Total = %+v, %v, %v", total, ok, err)
	}
	ambiguous, _, err := client.Trace.FacetsQuery(context.Background(), "net1", "ambiguous", TracePathQuery{To: &TraceEndpoint{Location: TraceSubnet("10.0.0.0/24")}}, TraceFacetsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := ambiguous.Total(); ok {
		t.Fatal("an ambiguous-location answer carries no totalResults")
	}
	possible, ok, err := ambiguous.PossibleLocations()
	if err != nil || !ok || possible.From != nil || possible.To == nil || possible.To.Type != "HOST" || len(possible.To.Locations) != 2 || possible.To.Locations[0].Device != "d1" {
		t.Fatalf("PossibleLocations = %+v, %v, %v", possible, ok, err)
	}
	if _, ok, _ := (&TraceFacets{}).Total(); ok {
		t.Fatal("an absent totalResults (a timed out request) reported a total")
	}
	if _, ok, _ := (&TraceFacets{}).PossibleLocations(); ok {
		t.Fatal("an absent possibleIpLocations reported candidates")
	}
}

// Each location kind renders the object Forward's @JsonSubTypes names, and a
// subnet or zone carries its device.
func TestTraceLocationsRenderForwardsFilterObjects(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		loc  TraceLocation
		want string
	}{
		{"host", TraceHost("h1"), `{"type":"HostFilter","values":["h1"]}`},
		{"vrf", TraceVRF("mgmt"), `{"type":"VrfFilter","values":["mgmt"]}`},
		{"device vrf", TraceDeviceVRF("d1", "mgmt"), `{"device":"d1","type":"DeviceVrfFilter","vrf":"mgmt"}`},
		{"zone", TraceSecurityZone("fw", "untrust"), `{"device":"fw","type":"SecurityZoneFilter","value":"untrust"}`},
		{"subnet", TraceSubnet("10.0.0.0/24"), `{"type":"SubnetLocationFilter","value":"10.0.0.0/24"}`},
		{"subnet at device", TraceSubnetAtDevice("10.0.0.0/24", "d1"), `{"device":"d1","type":"SubnetLocationFilter","value":"10.0.0.0/24"}`},
		{"not", TraceNot(TraceDevice("d1")), `{"clause":{"type":"DeviceFilter","values":["d1"]},"type":"NotFilter"}`},
	} {
		got, err := tc.loc.MarshalJSON()
		if err != nil || string(got) != tc.want {
			t.Errorf("%s = %s, %v; want %s", tc.name, got, err, tc.want)
		}
	}
}

// Queries Forward would refuse (400) are refused before sending: a host or
// subnet as a chain hop, a double negation, a blank name, an unknown transit or
// mode, and a From-only header in To. Two refusals are the SDK's own choice,
// not Forward's: a query with nothing in it (a mode alone included) and an empty
// header filter, both of which Forward would accept.
func TestTraceFacetsQueryRefusesWhatForwardRefuses(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	trace := newTestClient(t, server.URL).Trace
	from := &TraceEndpoint{Location: TraceDevice("d1")}

	for name, q := range map[string]TracePathQuery{
		"empty":                  {},
		"mode only":              {Mode: TraceModePermitAll},
		"double negation":        {From: &TraceEndpoint{Location: TraceNot(TraceNot(TraceDevice("d")))}},
		"blank interface part":   {From: &TraceEndpoint{Location: TraceInterface("", "eth1/1")}},
		"host in chain":          {From: from, Chain: []TraceChainHop{{Transit: TraceTransitThrough, Location: TraceHost("h")}}},
		"subnet in chain":        {From: from, Chain: []TraceChainHop{{Transit: TraceTransitThrough, Location: TraceSubnet("10.0.0.0/8")}}},
		"not host in chain":      {From: from, Chain: []TraceChainHop{{Transit: TraceTransitThrough, Location: TraceNot(TraceHost("h"))}}},
		"bad transit":            {From: from, Chain: []TraceChainHop{{Transit: "via", Location: TraceDevice("d")}}},
		"blank device":           {From: &TraceEndpoint{Location: TraceDevice(" ")}},
		"zero location":          {From: &TraceEndpoint{}},
		"zone without device":    {From: &TraceEndpoint{Location: TraceSecurityZone("", "z")}},
		"unknown mode":           {From: from, Mode: "DENY_ALL"},
		"from-only header in to": {From: from, To: &TraceEndpoint{Location: TraceDevice("d2"), Headers: []TraceHeaders{{"app_id": {"x"}}}}},
		"empty header":           {From: &TraceEndpoint{Location: TraceDevice("d1"), Headers: []TraceHeaders{{}}}},
	} {
		if _, _, err := trace.FacetsQuery(context.Background(), "net1", "s1", q, TraceFacetsOptions{}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if calls != 0 {
		t.Fatalf("%d refused queries reached the server", calls)
	}

	// From-only header fields are fine in From.
	if _, _, err := trace.FacetsQuery(context.Background(), "net1", "s1", TracePathQuery{From: &TraceEndpoint{Location: TraceDevice("d1"), Headers: []TraceHeaders{{"app_id": {"x"}}}}}, TraceFacetsOptions{}); err != nil {
		t.Fatalf("a From header with app_id was refused: %v", err)
	}
}
