package forward

import (
	"github.com/forwardnetworks/forward-go-sdk/domain"
)

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

	facets, _, err := client.Trace.FacetsQuery(context.Background(), "net1", "s1", domain.TracePathQuery{
		From: &domain.TraceEndpoint{Location: domain.TraceInterface("n1", "eth1/1"), Headers: []domain.TraceHeaders{{"ipv4_dst": {"10.0.0.2"}}}},
		Chain: []domain.TraceChainHop{
			{Transit: domain.TraceTransitThrough, Location: domain.TraceDevice("core")},
		},
		To:   &domain.TraceEndpoint{Location: domain.TraceInterface("n2", "eth1/1"), Headers: []domain.TraceHeaders{{"ip_proto": {"6"}}}},
		Mode: domain.TraceModePermitAll,
	}, domain.TraceFacetsOptions{MaxFacetValues: 5})
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
	ambiguous, _, err := client.Trace.FacetsQuery(context.Background(), "net1", "ambiguous", domain.TracePathQuery{To: &domain.TraceEndpoint{Location: domain.TraceSubnet("10.0.0.0/24")}}, domain.TraceFacetsOptions{})
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
	if _, ok, _ := (&domain.TraceFacets{}).Total(); ok {
		t.Fatal("an absent totalResults (a timed out request) reported a total")
	}
	if _, ok, _ := (&domain.TraceFacets{}).PossibleLocations(); ok {
		t.Fatal("an absent possibleIpLocations reported candidates")
	}
}

// Each location kind renders the object Forward's @JsonSubTypes names, and a
// subnet or zone carries its device.
func TestTraceLocationsRenderForwardsFilterObjects(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		loc  domain.TraceLocation
		want string
	}{
		{"host", domain.TraceHost("h1"), `{"type":"HostFilter","values":["h1"]}`},
		{"vrf", domain.TraceVRF("mgmt"), `{"type":"VrfFilter","values":["mgmt"]}`},
		{"device vrf", domain.TraceDeviceVRF("d1", "mgmt"), `{"device":"d1","type":"DeviceVrfFilter","vrf":"mgmt"}`},
		{"zone", domain.TraceSecurityZone("fw", "untrust"), `{"device":"fw","type":"SecurityZoneFilter","value":"untrust"}`},
		{"subnet", domain.TraceSubnet("10.0.0.0/24"), `{"type":"SubnetLocationFilter","value":"10.0.0.0/24"}`},
		{"subnet at device", domain.TraceSubnetAtDevice("10.0.0.0/24", "d1"), `{"device":"d1","type":"SubnetLocationFilter","value":"10.0.0.0/24"}`},
		{"not", domain.TraceNot(domain.TraceDevice("d1")), `{"clause":{"type":"DeviceFilter","values":["d1"]},"type":"NotFilter"}`},
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
	from := &domain.TraceEndpoint{Location: domain.TraceDevice("d1")}

	for name, q := range map[string]domain.TracePathQuery{
		"empty":                  {},
		"mode only":              {Mode: domain.TraceModePermitAll},
		"double negation":        {From: &domain.TraceEndpoint{Location: domain.TraceNot(domain.TraceNot(domain.TraceDevice("d")))}},
		"blank interface part":   {From: &domain.TraceEndpoint{Location: domain.TraceInterface("", "eth1/1")}},
		"host in chain":          {From: from, Chain: []domain.TraceChainHop{{Transit: domain.TraceTransitThrough, Location: domain.TraceHost("h")}}},
		"subnet in chain":        {From: from, Chain: []domain.TraceChainHop{{Transit: domain.TraceTransitThrough, Location: domain.TraceSubnet("10.0.0.0/8")}}},
		"not host in chain":      {From: from, Chain: []domain.TraceChainHop{{Transit: domain.TraceTransitThrough, Location: domain.TraceNot(domain.TraceHost("h"))}}},
		"bad transit":            {From: from, Chain: []domain.TraceChainHop{{Transit: "via", Location: domain.TraceDevice("d")}}},
		"blank device":           {From: &domain.TraceEndpoint{Location: domain.TraceDevice(" ")}},
		"zero location":          {From: &domain.TraceEndpoint{}},
		"zone without device":    {From: &domain.TraceEndpoint{Location: domain.TraceSecurityZone("", "z")}},
		"unknown mode":           {From: from, Mode: "DENY_ALL"},
		"from-only header in to": {From: from, To: &domain.TraceEndpoint{Location: domain.TraceDevice("d2"), Headers: []domain.TraceHeaders{{"app_id": {"x"}}}}},
		"empty header":           {From: &domain.TraceEndpoint{Location: domain.TraceDevice("d1"), Headers: []domain.TraceHeaders{{}}}},
	} {
		if _, _, err := trace.FacetsQuery(context.Background(), "net1", "s1", q, domain.TraceFacetsOptions{}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if calls != 0 {
		t.Fatalf("%d refused queries reached the server", calls)
	}

	// From-only header fields are fine in From.
	if _, _, err := trace.FacetsQuery(context.Background(), "net1", "s1", domain.TracePathQuery{From: &domain.TraceEndpoint{Location: domain.TraceDevice("d1"), Headers: []domain.TraceHeaders{{"app_id": {"x"}}}}}, domain.TraceFacetsOptions{}); err != nil {
		t.Fatalf("a From header with app_id was refused: %v", err)
	}
}

// A path query with a chain hop and a To location is listed and traced with the SAME filters body on both routes, so the path chosen from
// the facets is the one traced; the trace route gets the selected path and behavior sample back unchanged.
func TestTraceForAndExplainQuery(t *testing.T) {
	var facetsFilters, traceFilters string
	var picked string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Filters        json.RawMessage `json:"filters"`
			SelectedFacets json.RawMessage `json:"selectedFacets"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		switch r.URL.Path {
		case "/api/networks/n1/trace-facets":
			facetsFilters = string(body.Filters)
			_, _ = w.Write([]byte(`{"facets":{"pathGroups":[{"traceType":"VALID","paths":[{"path":["a","b"],"behaviorSample":{"k":1}}]}]},"totalResults":{"value":1,"type":"EXACT"}}`))
		case "/api/networks/n1/trace":
			traceFilters, picked = string(body.Filters), string(body.SelectedFacets)
			_, _ = w.Write([]byte(`{"traceType":"VALID","deviceHops":[{"deviceName":"a"},{"deviceName":"b"}],"pathDescriptor":"a>b"}`))
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	q := domain.TracePathQuery{
		From:  &domain.TraceEndpoint{Location: domain.TraceDevice("a")},
		Chain: []domain.TraceChainHop{{Transit: domain.TraceTransitThrough, Location: domain.TraceDevice("b")}},
		To:    &domain.TraceEndpoint{Location: domain.TraceSubnet("10.0.0.0/24")},
	}
	explained, _, err := client.Trace.ExplainQuery(context.Background(), "n1", "9", q)
	if err != nil || len(explained.Paths) != 1 || explained.Paths[0].Trace == nil || explained.Paths[0].Descriptor != "a>b" {
		t.Fatalf("explained=%+v err=%v", explained, err)
	}
	if facetsFilters == "" || facetsFilters != traceFilters {
		t.Fatalf("both routes must carry the same filters:\nfacets=%s\ntrace=%s", facetsFilters, traceFilters)
	}
	if !strings.Contains(traceFilters, `"chain"`) || !strings.Contains(picked, `"behavior":{"k":1}`) || !strings.Contains(picked, `"traceType":"VALID"`) {
		t.Fatalf("filters=%s picked=%s", traceFilters, picked)
	}
	if _, _, err = client.Trace.TraceFor(context.Background(), "n1", "9", q, domain.TraceSelection{}); err == nil {
		t.Fatal("a trace without a selected path must be refused")
	}
}
