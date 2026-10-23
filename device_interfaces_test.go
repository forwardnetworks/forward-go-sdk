package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestDeviceInterfacesAndDiffWire pins the 26.10 device routes: ?with=neighbors only when asked (the new route omits
// neighbors otherwise), "/" in an interface name percent-encoded, the discriminated Type enum and Raw kept, and the
// device-diff filters reaching the wire as the spec's comma lists.
func TestDeviceInterfacesAndDiffWire(t *testing.T) {
	t.Parallel()
	var uris []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		uris = append(uris, r.RequestURI)
		switch {
		case r.URL.Path == "/api/networks/7/devices/sw1/interfaces":
			_, _ = w.Write([]byte(`{"interfaces":[{"type":"ACCESS","name":"Ethernet1/3","aliases":[],"isDown":false,"isLoopback":false,"inAclNames":[],"outAclNames":[],"fileLines":[],"accessVlan":10,"neighbors":[{"device":"r1","port":"e4"}]}]}`))
		case r.URL.Path == "/api/networks/7/devices/sw1/interfaces/Ethernet1/3":
			_, _ = w.Write([]byte(`{"type":"L3","name":"Ethernet1/3","aliases":["et-1/3"],"isDown":true,"isLoopback":false,"inAclNames":[],"outAclNames":[]}`))
		default:
			_, _ = w.Write([]byte(`{"beforeSnapshotCreatedAt":"a","afterSnapshotCreatedAt":"b","totalDiffs":2,"diffs":[{"deviceName":"sw1","changeType":"MODIFIED","changedProperties":["model"],"before":{"name":"sw1","model":"x"},"after":{"name":"sw1","model":"y"}}]}`))
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()

	list, _, err := client.Devices.Interfaces(ctx, "7", "sw1", DeviceInterfaceOptions{SnapshotID: "9", WithNeighbors: true})
	if err != nil || len(list) != 1 || list[0].Type != "ACCESS" || len(list[0].Neighbors) != 1 || list[0].Neighbors[0].Port != "e4" {
		t.Fatalf("Interfaces = %#v, %v", list, err)
	}
	if len(list[0].Raw) == 0 {
		t.Fatal("Raw not kept")
	}
	one, _, err := client.Devices.Interface(ctx, "7", "sw1", "Ethernet1/3", DeviceInterfaceOptions{})
	if err != nil || one.Type != "L3" || !one.IsDown {
		t.Fatalf("Interface = %#v, %v", one, err)
	}
	limit := int32(5)
	diff, _, err := client.Devices.Diff(ctx, "7", "1", "2", DeviceDiffOptions{Name: "sw*", ChangeTypes: []string{"ADDED", "MODIFIED"}, With: []string{"tags"}, Limit: &limit})
	if err != nil || diff.TotalDiffs != 2 || diff.Diffs[0].After.Model != "y" {
		t.Fatalf("Diff = %#v, %v", diff, err)
	}
	want := []string{
		"/api/networks/7/devices/sw1/interfaces?snapshotId=9&with=neighbors",
		"/api/networks/7/devices/sw1/interfaces/Ethernet1%2F3",
		"/api/networks/7/device-diffs/1/2?changeType=ADDED%2CMODIFIED&limit=5&name=sw%2A&with=tags",
	}
	for i := range want {
		if i >= len(uris) {
			t.Fatalf("only %d requests, want %d", len(uris), len(want))
		}
		if uris[i] != want[i] {
			t.Errorf("uri %d = %q, want %q", i, uris[i], want[i])
		}
	}
}

// TestSnapshotOverridesWire pins the network-level read of one snapshot's link overrides to ?view=snapshot&snapshotId=,
// the route Forward's migration guide gives for the deprecated snapshot-scoped GET.
func TestSnapshotOverridesWire(t *testing.T) {
	t.Parallel()
	var uri string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uri = r.RequestURI
		_, _ = w.Write([]byte(`{"absent":[{"port1":"a:e1","port2":"b:e1"}],"present":[]}`))
	}))
	defer server.Close()
	got, _, err := newTestClient(t, server.URL).Topology.SnapshotOverrides(context.Background(), "7", "9")
	if err != nil || len(got.Absent) != 1 {
		t.Fatalf("SnapshotOverrides = %#v, %v", got, err)
	}
	if want := "/api/networks/7/link-overrides?snapshotId=9&view=snapshot"; uri != want {
		t.Fatalf("uri = %q, want %q", uri, want)
	}
}
