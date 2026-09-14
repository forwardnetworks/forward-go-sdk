package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A path search that cannot be scoped to a snapshot cannot answer about a
// network that does not exist yet, which is the whole use for it here.
func TestPathSearchIsSnapshotScopedAndCarriesTheFilters(t *testing.T) {
	t.Parallel()

	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/networks/network-1/paths" {
			t.Errorf("path = %s", r.URL.Path)
		}
		got = r.URL.RawQuery
		_, _ = io.WriteString(w, `{
			"srcIpLocationType":"SUBNET","dstIpLocationType":"SUBNET","timedOut":false,
			"info":{"paths":[{"forwardingOutcome":"DELIVERED","securityOutcome":"PERMITTED",
			  "hops":[{"deviceName":"rtr1","networkFunctions":{"acl":[{"name":"allow","action":"PERMIT"}]}}]}],
			  "totalHits":{"type":"EXACT","value":1}}}`)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	proto, syn, tags := 6, 1, true
	result, _, err := client.Networks.Paths(context.Background(), "network-1", PathSearchRequest{
		SrcIP: "10.0.0.1", DstIP: "10.1.0.1", SnapshotID: "snapshot-9",
		IPProto: &proto, DstPort: "443", TCPFlags: PathTCPFlags{SYN: &syn},
		IncludeNetworkFunctions: &tags,
	})
	if err != nil {
		t.Fatalf("Paths() = %v", err)
	}
	if len(result.Info.Paths) != 1 || result.Info.Paths[0].ForwardingOutcome != "DELIVERED" {
		t.Fatalf("outcome not decoded: %#v", result.Info)
	}
	// The ACL on a hop is why includeNetworkFunctions exists; it has to survive decoding.
	if result.Info.Paths[0].Hops[0].NetworkFunctions == nil ||
		len(result.Info.Paths[0].Hops[0].NetworkFunctions.ACL) != 1 {
		t.Fatalf("network functions not decoded: %#v", result.Info.Paths[0].Hops[0])
	}
	for _, want := range []string{"snapshotId=snapshot-9", "ipProto=6", "dstPort=443", "syn=1", "includeNetworkFunctions=true"} {
		if !containsSubstring(got, want) {
			t.Fatalf("query %q is missing %s", got, want)
		}
	}
}

// A search with no destination has no question in it, and one with neither a
// source nor a start point has nowhere to begin.
func TestPathSearchRequiresADestinationAndAStart(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("request should not have been made: %s", r.URL.RequestURI())
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	if _, _, err := client.Networks.Paths(context.Background(), "network-1", PathSearchRequest{SrcIP: "10.0.0.1"}); err == nil {
		t.Fatal("expected a missing destination to be refused")
	}
	if _, _, err := client.Networks.Paths(context.Background(), "network-1", PathSearchRequest{DstIP: "10.1.0.1"}); err == nil {
		t.Fatal("expected a missing source and start point to be refused")
	}
}

func containsSubstring(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
