package forward

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The forwarding-state diff families share one shape (a device list with counts, then one device's entries) but differ in query and
// path: the L3 device list is ?view=devices on the shared l3 path, and its prefix detail takes the prefix as an ESCAPED path segment
// the way the UI sends it (10.0.0.0%2F8). Optional paging parameters must be left out when zero so Forward applies its own defaults.
func TestDiffForwardingStateFamiliesWire(t *testing.T) {
	t.Parallel()

	var uris []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uris = append(uris, r.URL.EscapedPath()+"?"+r.URL.RawQuery)
		switch {
		case strings.HasSuffix(r.URL.Path, "/arp/devices/edge-1"):
			_, _ = w.Write([]byte(`{"diffs":[{"diffType":"ADDED","b":{"ipAddress":"10.0.0.9","vrfName":"default","macAddress":"aa:bb:cc:dd:ee:ff","port":"Gi1","fileLines":{"show arp":[{"start":3,"end":3}]}}}]}`))
		case strings.HasSuffix(r.URL.Path, "/mac/devices/edge-1"):
			_, _ = w.Write([]byte(`{"diffs":[{"diffType":"DELETED","a":{"macAddress":"aa:bb:cc:dd:ee:ff","vlan":"10","ports":["Gi2"]}},{"diffType":"ADDED","b":{"macAddress":"aa:bb:cc:dd:ee:01","vlan":"untagged"}},{"diffType":"ADDED","b":{"macAddress":"aa:bb:cc:dd:ee:02"}}],"stats":{"ADDED":2,"DELETED":1,"UNCHANGED":5}}`))
		case strings.HasSuffix(r.URL.Path, "/vulnerabilities/counts") && r.URL.RawQuery == "":
			_, _ = w.Write([]byte(`{"newCveCount":4}`))
		case strings.HasSuffix(r.URL.Path, "/vulnerabilities/counts"):
			_, _ = w.Write([]byte(`{"warnings":{"cveCount":4,"exposedVulnerableDevicesCount":1},"improvements":{"cveCount":2}}`))
		case strings.HasSuffix(r.URL.Path, "/routing-loop/count"):
			_, _ = w.Write([]byte(`{"warnings":{"count":1,"complete":true},"improvements":{"count":0,"complete":true}}`))
		case strings.HasSuffix(r.URL.Path, "/arp"), strings.HasSuffix(r.URL.Path, "/mac"), strings.HasSuffix(r.URL.Path, "/nat"), strings.HasSuffix(r.URL.Path, "/l3") && r.URL.Query().Get("view") == "devices":
			_, _ = w.Write([]byte(`{"deviceInfos":[{"deviceName":"edge-1","hasConfigChange":true,"stats":{"ADDED":2,"DELETED":1}}]}`))
		default:
			_, _ = w.Write([]byte(`{"raw":true}`))
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()

	for _, family := range []struct {
		name string
		call func() ([]DeviceDiffSummary, *Response, error)
	}{
		{"arp", func() ([]DeviceDiffSummary, *Response, error) { return client.Diffs.ARPDevices(ctx, "A", "B") }},
		{"mac", func() ([]DeviceDiffSummary, *Response, error) { return client.Diffs.MACDevices(ctx, "A", "B") }},
		{"nat", func() ([]DeviceDiffSummary, *Response, error) { return client.Diffs.NATDevices(ctx, "A", "B") }},
		{"l3", func() ([]DeviceDiffSummary, *Response, error) { return client.Diffs.L3Devices(ctx, "A", "B") }},
	} {
		devices, _, err := family.call()
		if err != nil || len(devices) != 1 || devices[0].DeviceName != "edge-1" || !devices[0].HasConfigChange || devices[0].Stats["ADDED"] != 2 {
			t.Errorf("%s devices: %+v %v", family.name, devices, err)
		}
	}
	arp, _, err := client.Diffs.DeviceARP(ctx, "A", "B", "edge-1")
	if err != nil || len(arp.Diffs) != 1 || arp.Diffs[0].Type != "ADDED" || arp.Diffs[0].Before != nil || arp.Diffs[0].After.IPAddress != "10.0.0.9" || arp.Diffs[0].After.FileLines["show arp"][0].Start != 3 {
		t.Fatalf("arp: %+v %v", arp, err)
	}
	mac, _, err := client.Diffs.DeviceMAC(ctx, "A", "B", "edge-1")
	if err != nil || mac.Diffs[0].Before == nil || mac.Diffs[0].Before.VLAN != "10" || mac.Diffs[0].Before.Ports[0] != "Gi2" {
		t.Fatalf("mac: %+v %v", mac, err)
	}
	if mac.Diffs[1].After.VLAN != "untagged" || mac.Diffs[2].After.VLAN != "" || mac.Stats["UNCHANGED"] != 5 {
		t.Fatalf("mac vlan forms and stats: %+v", mac)
	}
	summary, _, err := client.Diffs.VulnerabilitiesCounts(ctx, "A", "B")
	if err != nil || summary.NewCVECount != 4 || summary.NewExposedVulnerableDevicesCount != nil {
		t.Fatalf("vulnerabilities: %+v %v", summary, err)
	}
	both, _, err := client.Diffs.VulnerabilitiesBidirectional(ctx, "A", "B")
	if err != nil || both.Warnings.CVECount != 4 || both.Warnings.ExposedVulnerableDevicesCount == nil || *both.Warnings.ExposedVulnerableDevicesCount != 1 || both.Improvements.ExposedVulnerableDevicesCount != nil {
		t.Fatalf("bidirectional: %+v %v", both, err)
	}
	loops, _, err := client.Diffs.RoutingLoopsBidirectional(ctx, "A", "B")
	if err != nil || loops.Warnings.Count != 1 || !loops.Improvements.Complete {
		t.Fatalf("loops: %+v %v", loops, err)
	}
	for _, call := range []func() ([]byte, error){
		func() ([]byte, error) { b, _, err := client.Diffs.DeviceNAT(ctx, "A", "B", "edge-1"); return b, err },
		func() ([]byte, error) { b, _, err := client.Diffs.DeviceL3(ctx, "A", "B", "edge-1"); return b, err },
		func() ([]byte, error) { b, _, err := client.Diffs.L3Summary(ctx, "A", "B"); return b, err },
		func() ([]byte, error) {
			b, _, err := client.Diffs.L3PrefixDetails(ctx, "A", "B", "10.0.0.0/8")
			return b, err
		},
		func() ([]byte, error) {
			b, _, err := client.Diffs.L3Prefixes(ctx, "A", "B", L3PrefixOptions{})
			return b, err
		},
		func() ([]byte, error) {
			b, _, err := client.Diffs.L3Prefixes(ctx, "A", "B", L3PrefixOptions{Offset: 100, Limit: 50, PrefixFilter: "10.0.0.0/8"})
			return b, err
		},
	} {
		if body, err := call(); err != nil || string(body) != `{"raw":true}` {
			t.Errorf("raw call: %s %v", body, err)
		}
	}

	want := []string{
		"/api/diffs/A/B/arp?", "/api/diffs/A/B/mac?", "/api/diffs/A/B/nat?", "/api/diffs/A/B/l3?view=devices",
		"/api/diffs/A/B/arp/devices/edge-1?", "/api/diffs/A/B/mac/devices/edge-1?",
		"/api/diffs/A/B/vulnerabilities/counts?", "/api/diffs/A/B/vulnerabilities/counts?view=bidirectional",
		"/api/diffs/A/B/routing-loop/count?view=bidirectional",
		"/api/diffs/A/B/nat/devices/edge-1?", "/api/diffs/A/B/l3/devices/edge-1?", "/api/diffs/A/B/l3/summary?",
		"/api/diffs/A/B/l3/prefixes/10.0.0.0%2F8?", "/api/diffs/A/B/l3?view=prefixes",
		"/api/diffs/A/B/l3?limit=50&offset=100&prefixFilter=10.0.0.0%2F8&view=prefixes",
	}
	if len(uris) != len(want) {
		t.Fatalf("%d requests, want %d: %v", len(uris), len(want), uris)
	}
	for i := range want {
		if uris[i] != want[i] {
			t.Errorf("request %d = %s, want %s", i, uris[i], want[i])
		}
	}
	if _, _, err := client.Diffs.DeviceARP(ctx, "A", "B", " "); err == nil {
		t.Error("a device is required")
	}
	if _, _, err := client.Diffs.L3PrefixDetails(ctx, "A", "B", ""); err == nil {
		t.Error("a prefix is required")
	}
	if _, _, err := client.Diffs.L3Prefixes(ctx, "A", "B", L3PrefixOptions{Limit: -1}); err == nil {
		t.Error("a negative limit is refused")
	}
}
