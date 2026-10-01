package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ComputeQuery is SetQuery's dry run: it POSTs the compute action on each
// kind's own route (the internet node has no collection, so its node route)
// and changes nothing. The result is the same {connections, error} shape a
// node's queryResult carries.
func TestSyntheticNodesComputeQueryPerKind(t *testing.T) {
	t.Parallel()

	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Query().Get("action") != "computeNqeBasedConnections" || r.URL.Query().Get("queryId") != "FQ_abc" {
			t.Errorf("request = %s %s", r.Method, r.URL.RequestURI())
		}
		seen = append(seen, r.URL.Path)
		_, _ = io.WriteString(w, `{"connections":[{"uplinkPort":{"device":"r1","port":"e1"},"source":"NQE"}]}`)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL).ForNetwork("n1")

	want := map[SyntheticNodeKind]string{
		SyntheticInternet:        "/api/networks/n1/internet-node",
		SyntheticIntranet:        "/api/networks/n1/intranet-nodes",
		SyntheticL3VPN:           "/api/networks/n1/l3-vpns",
		SyntheticL2VPN:           "/api/networks/n1/l2-vpns",
		SyntheticAdjacentNetwork: "/api/networks/n1/adjacent-networks",
	}
	for kind, path := range want {
		seen = nil
		got, _, err := client.SyntheticNodes.ComputeQuery(context.Background(), "", kind, " FQ_abc ")
		if err != nil || len(seen) != 1 || seen[0] != path || len(got.Connections) != 1 || got.Error != nil {
			t.Errorf("%s: path %v, result %+v, err %v", kind, seen, got, err)
		}
	}
	if _, _, err := client.SyntheticNodes.ComputeQuery(context.Background(), "", SyntheticL3VPN, " "); err == nil {
		t.Fatal("an empty query ID must be refused locally")
	}
}

// A query of the wrong row type is not an HTTP error: Forward reports it in
// the result, and the caller must look there.
func TestSyntheticNodesComputeQueryReportsTheQueryError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"error":{"errorMsg":"NQE query Q_1 is invalid for this synthetic device connection type.","status":"COLUMN_DATATYPE_MISMATCH"}}`)
	}))
	defer server.Close()

	got, _, err := newTestClient(t, server.URL).SyntheticNodes.ComputeQuery(context.Background(), "n1", SyntheticL2VPN, "Q_1")
	if err != nil || got.Error == nil || got.Error.Status != "COLUMN_DATATYPE_MISMATCH" || len(got.Connections) != 0 {
		t.Fatalf("result = %+v, err = %v", got, err)
	}
}

// SyntheticDeviceController.getNqeType maps each kind to the device type
// Forward filters queries by; anything else is a 400 there, so it is refused
// here first.
func TestSyntheticNodesCompatibleQueries(t *testing.T) {
	t.Parallel()

	var gotType, gotNetwork string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/synthetic-device-queries" {
			t.Errorf("path = %s", r.URL.Path)
		}
		gotType, gotNetwork = r.URL.Query().Get("type"), r.URL.Query().Get("networkId")
		_, _ = io.WriteString(w, `{"queries":[{"queryId":"Q_abc","path":"/synthetic/vpn connections"}]}`)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL).ForNetwork("n1")

	for kind, deviceType := range map[SyntheticNodeKind]string{
		SyntheticInternet: "INTERNET", SyntheticIntranet: "INTRANET", SyntheticL3VPN: "L3VPN",
		SyntheticL2VPN: "L2VPN", SyntheticAdjacentNetwork: "ADJACENT_NETWORK",
	} {
		got, _, err := client.SyntheticNodes.CompatibleQueries(context.Background(), "", kind)
		if err != nil || gotType != deviceType || gotNetwork != "n1" || len(got) != 1 || got[0].QueryID != "Q_abc" || got[0].Path != "/synthetic/vpn connections" {
			t.Errorf("%s: type=%q network=%q got=%+v err=%v", kind, gotType, gotNetwork, got, err)
		}
	}
	if _, _, err := client.SyntheticNodes.CompatibleQueries(context.Background(), "", SyntheticNodeKind("wan-circuit")); err == nil {
		t.Fatal("a kind Forward has no query type for must be refused")
	}
}

// L2 VPN connections and adjacent networks' ownedSubnets are not modelled, so
// writing either back would drop them. Put refuses, and sends nothing.
func TestSyntheticNodesPutRefusesUnmodelledKinds(t *testing.T) {
	t.Parallel()

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL).ForNetwork("n1")

	for _, kind := range []SyntheticNodeKind{SyntheticL2VPN, SyntheticAdjacentNetwork} {
		_, err := client.SyntheticNodes.Put(context.Background(), "", kind, "x", SyntheticNode{Name: "x"})
		if err == nil || !strings.Contains(err.Error(), "SetQuery") {
			t.Errorf("%s: err = %v, want a refusal pointing at SetQuery", kind, err)
		}
	}
	if calls != 0 {
		t.Fatalf("a refused Put still sent %d request(s)", calls)
	}
}

// The shape is Forward's InternetConnectionSuggestions record: the gateway
// details are unwrapped into the suggestion.
func TestSyntheticNodesInternetConnectionSuggestions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/networks/n1/internet-node/connection-suggestions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"suggestions":[
		  {"uplinkInterface":"edge-1 eth0","uplinkInterfaceDescription":"to ISP","vlan":12,"gatewayInterface":"isp-gw ge-0/0/0"},
		  {"uplinkInterface":"edge-2 eth0"}]}`)
	}))
	defer server.Close()

	got, _, err := newTestClient(t, server.URL).SyntheticNodes.InternetConnectionSuggestions(context.Background(), "n1")
	if err != nil || len(got) != 2 || got[0].VLAN == nil || *got[0].VLAN != 12 || got[0].GatewayInterface != "isp-gw ge-0/0/0" ||
		got[1].VLAN != nil || got[1].GatewayInterface != "" {
		t.Fatalf("suggestions = %+v, err = %v", got, err)
	}
}
