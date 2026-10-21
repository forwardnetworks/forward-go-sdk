package forward

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The routing app answers from RP_PREFIX_SEARCH routes whose bodies and shapes come from PredictController, FrontendRpPrefix*Request and
// the search worker's JSON records. The wire facts the SDK must keep: snapshotId is a query parameter on every route, the device
// prefix view is the shared devices/{d} path selected by view=, and a trace hop is passed back to the details route UNCHANGED.
func TestRoutingReadsPrefixesEndToEnd(t *testing.T) {
	t.Parallel()

	type seen struct{ method, uri, body string }
	var requests []seen
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests = append(requests, seen{r.Method, r.URL.RequestURI(), string(body)})
		switch {
		case strings.HasSuffix(r.URL.Path, "/bgp-prefix-search"):
			_, _ = w.Write([]byte(`{"origin":[{"device":"r2","vrf":"default","locationId":"L","protocol":"BGP"}],"devicesByOutcome":{"INSTALLED":[{"device":"r1","vrf":"default","protocol":"BGP"}],"FILTERED_OUT":[]}}`))
		case strings.HasSuffix(r.URL.Path, "/bgp-prefix-trace"):
			_, _ = w.Write([]byte(`[[{"device":"r2","vrf":"default","routeSetId":3,"asNumber":65002,"protocol":"BGP","importPolicyNames":["P-IN"],"nodeKey":{"device":"r2","vrf":"default","protocol":"BGP","neighborAddr":"192.0.2.9"}},{"device":"r1","vrf":"default","routeSetId":4,"asNumber":4200000000,"routerId":"1.1.1.1","protocol":"BGP","nodeKey":{"device":"r1","vrf":"default","protocol":"BGP"}}]]`))
		case strings.HasSuffix(r.URL.Path, "/bgp-prefix-details"):
			_, _ = w.Write([]byte(`{"prefix":"10.0.0.0/8","device":"r1","vrf":"default","attributes":{"protocol":"BGP","asPath":[65002],"origin":"IGP"},"policies":{"IMPORT":[{"name":"P-IN"}]}}`))
		case strings.HasSuffix(r.URL.Path, "/devices/r%2F1"), strings.HasSuffix(r.URL.Path, "/devices/r/1"):
			_, _ = w.Write([]byte(`{"device":"r/1","vendor":"CISCO","model":"X","vrfs":[{"vrf":"default","recvAdvertisements":[{"nexthop":"192.0.2.9/32","outcome":"INSTALLED","originatedFrom":{"device":"r2","vrf":"default"}}],"moreSpecificInstalledRoutes":["10.1.0.0/16"]}]}`))
		default:
			_, _ = w.Write([]byte(`{"totalPrefixCount":1,"nodes":[{"device":"r1","vendorOs":"cisco_ios","vrf":"default","prefixes":["10.0.0.0/8"]}]}`))
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()

	search, _, err := client.Routing.PrefixSearch(ctx, "N1", "S1", RoutingPrefixSearch{Prefix: "10.0.0.0/8", Outcome: RouteOutcomeInstalled})
	if err != nil || len(search.Origin) != 1 || search.DevicesByOutcome[RouteOutcomeInstalled][0].Device != "r1" {
		t.Fatalf("search: %+v %v", search, err)
	}
	paths, _, err := client.Routing.PrefixTrace(ctx, "N1", "S1", RoutingTraceRequest{Prefix: "10.0.0.0/8", Router: DeviceVRF{Device: "r1", VRF: "default"}, OriginatedFrom: DeviceVRF{Device: "r2", VRF: "default"}, Outcome: RouteOutcomeInstalled})
	if err != nil || len(paths) != 1 || len(paths[0]) != 2 || paths[0][1].ASNumber != 4200000000 || paths[0][0].RouteSetID != 3 {
		t.Fatalf("trace: %+v %v", paths, err)
	}
	hop := paths[0]
	details, _, err := client.Routing.PrefixDetails(ctx, "N1", "S1", RoutingPrefixDetailsRequest{Prefix: "10.0.0.0/8", Node: hop[1], PreviousNode: &hop[0]})
	if err != nil || details.Device != "r1" || len(details.Policies["IMPORT"]) == 0 || !strings.Contains(string(details.Attributes), `"asPath":[65002]`) {
		t.Fatalf("details: %+v %v", details, err)
	}
	info, _, err := client.Routing.DevicePrefixInfo(ctx, "N1", "S1", "r/1", "10.0.0.0/8")
	if err != nil || info.Vendor != "CISCO" || info.VRFs[0].RecvAdvertisements[0].OriginatedFrom.Device != "r2" || info.VRFs[0].MoreSpecificInstalledRoutes[0] != "10.1.0.0/16" {
		t.Fatalf("device info: %+v %v", info, err)
	}
	installed, _, err := client.Routing.InstalledPrefixes(ctx, "N1", "S1", RoutingInstalledOptions{Vendors: []string{"cisco_ios", "arista_eos"}, Limit: 50})
	if err != nil || installed.TotalPrefixCount != 1 || installed.Nodes[0].Prefixes[0] != "10.0.0.0/8" {
		t.Fatalf("installed: %+v %v", installed, err)
	}

	if len(requests) != 5 {
		t.Fatalf("%d requests: %+v", len(requests), requests)
	}
	if requests[0].method != "POST" || requests[0].uri != "/api/networks/N1/bgp-prefix-search?snapshotId=S1" || requests[0].body != `{"prefix":"10.0.0.0/8","outcome":"INSTALLED"}` {
		t.Errorf("search wire: %+v", requests[0])
	}
	if requests[1].uri != "/api/networks/N1/bgp-prefix-trace?snapshotId=S1" {
		t.Errorf("trace wire: %+v", requests[1])
	}
	// Current Forward builds fail (500) on a trace without name-based keys, so the SDK sends them, as the GUI does.
	if !strings.Contains(requests[1].body, `"routerKey":{"device":"r1","vrf":"default","protocol":"BGP"}`) || !strings.Contains(requests[1].body, `"originatedFromKey":{"device":"r2","vrf":"default","protocol":"BGP"}`) {
		t.Errorf("trace body must carry the keys: %s", requests[1].body)
	}
	if strings.Contains(requests[0].body, "routerKey") {
		t.Errorf("a search with no router sends no key: %s", requests[0].body)
	}
	// A hop goes back byte for byte as Forward sent it (node key, policy names and all), not re-built from the fields this SDK reads.
	var sent struct {
		Node         json.RawMessage `json:"node"`
		PreviousNode json.RawMessage `json:"previousNode"`
	}
	if err := json.Unmarshal([]byte(requests[2].body), &sent); err != nil {
		t.Fatal(err)
	}
	if string(sent.PreviousNode) != string(hop[0].Raw) || string(sent.Node) != string(hop[1].Raw) || !strings.Contains(string(sent.PreviousNode), `"importPolicyNames":["P-IN"]`) {
		t.Errorf("details must echo the trace hops unchanged: %s", requests[2].body)
	}
	if requests[3].method != "GET" || !strings.Contains(requests[3].uri, "view=bgp-prefix-info") || !strings.Contains(requests[3].uri, "prefix=10.0.0.0%2F8") || !strings.Contains(requests[3].uri, "snapshotId=S1") {
		t.Errorf("device info wire: %+v", requests[3])
	}
	if requests[4].uri != "/api/networks/N1/bgp-installed-prefixes?limit=50&snapshotId=S1&vendors=cisco_ios&vendors=arista_eos" {
		t.Errorf("installed wire: %+v", requests[4])
	}
}

func TestRoutingRefusesBadInputBeforeAnyRequest(t *testing.T) {
	t.Parallel()

	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()
	if _, _, err := client.Routing.PrefixSearch(ctx, "N1", "", RoutingPrefixSearch{Prefix: "10.0.0.0/8"}); err == nil {
		t.Error("a snapshot is required")
	}
	if _, _, err := client.Routing.PrefixSearch(ctx, "N1", "S1", RoutingPrefixSearch{}); err == nil {
		t.Error("a prefix is required")
	}
	if _, _, err := client.Routing.PrefixSearch(ctx, "N1", "S1", RoutingPrefixSearch{Prefix: "10.0.0.0/8", Outcome: "LOST"}); err == nil {
		t.Error("an unknown outcome must be refused")
	}
	if _, _, err := client.Routing.PrefixTrace(ctx, "N1", "S1", RoutingTraceRequest{Prefix: "10.0.0.0/8"}); err == nil {
		t.Error("a trace needs router, origin and outcome")
	}
	if _, _, err := client.Routing.InstalledPrefixes(ctx, "N1", "S1", RoutingInstalledOptions{Device: "r1", Vendors: []string{"cisco_ios"}}); err == nil {
		t.Error("device and vendors cannot be combined")
	}
	if hits != 0 {
		t.Errorf("%d request(s) for refused calls", hits)
	}
}

// Forward answers HTTP 200 {"error": "..."} when a details or device lookup matches nothing; that must not decode into an empty result.
func TestRoutingLookupFailureIsAnError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error":"No matching node for device=r1"}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()
	if _, _, err := client.Routing.PrefixDetails(ctx, "N1", "S1", RoutingPrefixDetailsRequest{Prefix: "10.0.0.0/8", Node: RoutingNode{Device: "r1"}}); !errors.Is(err, ErrRoutingLookup) {
		t.Errorf("details: %v", err)
	}
	if _, _, err := client.Routing.DevicePrefixInfo(ctx, "N1", "S1", "r1", "10.0.0.0/8"); !errors.Is(err, ErrRoutingLookup) {
		t.Errorf("device info: %v", err)
	}
	// A caller-set key is kept, so another protocol can be asked for.
	var gotBody string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer other.Close()
	_, _, _ = newTestClient(t, other.URL).Routing.PrefixSearch(ctx, "N1", "S1", RoutingPrefixSearch{Prefix: "10.0.0.0/8", Router: &DeviceVRF{Device: "r1"}, RouterKey: &DeviceVRFProtocol{Device: "r1", Protocol: "OSPF"}})
	if !strings.Contains(gotBody, `"routerKey":{"device":"r1","protocol":"OSPF"}`) {
		t.Errorf("an explicit key must win: %s", gotBody)
	}
}
