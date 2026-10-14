package forward

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Forward serializes a diff entry as {"diffType","a","b"}; reading
// type/before/after left every Devices entry empty. This pins the wire keys:
// a is the older snapshot's object, b the newer one's.
func TestDiffEntryReadsTheAppserversKeys(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/diffs/s1/s2/devices" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"diffType":"ADDED","b":{"name":"edge-2"}},{"diffType":"DELETED","a":{"name":"edge-1"}},{"diffType":"MODIFIED","a":{"name":"x","os":"A"},"b":{"name":"x","os":"B"}}]`))
	}))
	defer server.Close()
	got, _, err := newTestClient(t, server.URL).Diffs.Devices(context.Background(), "s1", "s2")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Type != "ADDED" || got[0].Before != nil || got[0].After == nil || got[0].After.Name != "edge-2" {
		t.Fatalf("added entry decoded wrong: %+v", got)
	}
	if got[1].Type != "DELETED" || got[1].Before == nil || got[1].Before.Name != "edge-1" || got[1].After != nil {
		t.Fatalf("deleted entry decoded wrong: %+v", got[1])
	}
	if got[2].Before.OS != "A" || got[2].After.OS != "B" {
		t.Fatalf("modified entry decoded wrong: %+v", got[2])
	}
}

// ACLDevices is the per-device summary (counts), DeviceACL the entries; both
// are plain GETs with the device name escaped as one path segment, and an
// empty device name must not be sent as the route without a device.
func TestACLDiffDevicesAndEntries(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.EscapedPath()+"?"+r.URL.RawQuery)
		if r.URL.Path == "/api/diffs/s1/s2/acl" {
			_, _ = w.Write([]byte(`{"deviceInfos":[{"name":"fw-1","deviceName":"fw-1","vendor":"PALO_ALTO","hasConfigChange":true,"stats":{"ADDED":2,"DELETED":1}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"diffs":[{"diffType":"ADDED","b":{"name":"allow-web","action":"PERMIT","aclContext":"per interface","matches":[{"field":"IPV4_SRC","values":["10.0.0.0/24"]},{"field":"TP_DST","index":1,"values":["443"],"negated":true}],"inputInterfaces":["eth1"],"fileLines":{"fw-1,CONFIG.txt":[{"start":10,"end":12}]}}},{"diffType":"DELETED","a":{"action":"DENY"}}],"stats":{"ADDED":1,"DELETED":1}}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()

	devices, _, err := client.Diffs.ACLDevices(ctx, "s1", "s2")
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].DeviceName != "fw-1" || !devices[0].HasConfigChange || devices[0].Stats["ADDED"] != 2 || devices[0].Vendor != "PALO_ALTO" {
		t.Fatalf("devices = %+v", devices)
	}

	diff, _, err := client.Diffs.DeviceACL(ctx, "s1", "s2", "fw/1")
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Diffs) != 2 || diff.Stats["DELETED"] != 1 {
		t.Fatalf("diff = %+v", diff)
	}
	added, deleted := diff.Diffs[0], diff.Diffs[1]
	if added.Type != "ADDED" || added.Before != nil || added.After.Action != "PERMIT" || added.After.Name != "allow-web" ||
		added.After.FileLines["fw-1,CONFIG.txt"][0] != (ACLLineRange{Start: 10, End: 12}) {
		t.Fatalf("added = %+v", added)
	}
	if len(added.After.Matches) != 2 || added.After.Matches[0].Field != "IPV4_SRC" || string(added.After.Matches[0].Values) != `["10.0.0.0/24"]` ||
		added.After.Matches[1].Index != 1 || !added.After.Matches[1].Negated || added.After.ACLContext != "per interface" ||
		len(added.After.InputInterfaces) != 1 || added.After.InputInterfaces[0] != "eth1" {
		t.Fatalf("match conditions decoded wrong: %+v", added.After)
	}
	if deleted.Type != "DELETED" || deleted.After != nil || deleted.Before.Action != "DENY" {
		t.Fatalf("deleted = %+v", deleted)
	}

	if want := []string{"/api/diffs/s1/s2/acl?", "/api/diffs/s1/s2/acl/devices/fw%2F1?"}; len(paths) != 2 || paths[0] != want[0] || paths[1] != want[1] {
		t.Fatalf("paths = %q, want %q", paths, want)
	}
	if _, _, err := client.Diffs.DeviceACL(ctx, "s1", "s2", " "); err == nil || len(paths) != 2 {
		t.Fatalf("a blank device must fail before any request (err=%v, requests=%d)", err, len(paths))
	}
}
