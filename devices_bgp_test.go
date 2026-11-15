package forward

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Forward requires vrf and prefix on this read; both must reach the wire, with the snapshot only when given, and the body is the same
// record Predict stages so the two compare field by field.
func TestDevicesBGPAdvertisements(t *testing.T) {
	var gotPath, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.EscapedPath(), r.URL.RawQuery
		_, _ = w.Write([]byte(`[{"vrf":"default","externalPeer":"11.11.11.11","prefix":"10.10.10.0/24","type":"EBGP","nextHop":"11.11.11.12","origin":"EGP","localPref":2,"asPath":[1],"med":0,"communities":["65000:1"]}]`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	ads, _, err := client.Devices.BGPAdvertisements(context.Background(), "n1", "edge 1", "s1", "default", "10.10.10.0/24")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/networks/n1/devices/edge%201/bgp-advertisements" || gotQuery != "prefix=10.10.10.0%2F24&snapshotId=s1&vrf=default" {
		t.Fatalf("request = %s ? %s", gotPath, gotQuery)
	}
	if len(ads) != 1 || ads[0].ExternalPeer != "11.11.11.11" || ads[0].MED == nil || *ads[0].MED != 0 || ads[0].LocalPref == nil || *ads[0].LocalPref != 2 {
		t.Fatalf("ads = %+v", ads)
	}

	if _, _, err := client.Devices.BGPAdvertisements(context.Background(), "n1", "edge", "", "default", "10.0.0.0/8"); err != nil {
		t.Fatal(err)
	}
	if gotQuery != "prefix=10.0.0.0%2F8&vrf=default" {
		t.Fatalf("no snapshotId must be sent when none is given: %s", gotQuery)
	}
	if _, _, err := client.Devices.BGPAdvertisements(context.Background(), "n1", "edge", "", "", "10.0.0.0/8"); err == nil {
		t.Fatal("a missing VRF must be refused before any request")
	}
}
