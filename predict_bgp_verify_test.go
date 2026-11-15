package forward

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A withdrawal Forward could not match comes back as ABSENT in the diffs view and nowhere else, so VerifyBGPStaging must turn it into
// an error that names the key, while a withdrawal that did remove a route, and a stage that landed, report cleanly.
func TestVerifyBGPStaging(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/networks/n1/change-sets/c1/draft/devices/edge/bgp-advertisements" || r.URL.Query().Get("view") != "diffs" {
			t.Errorf("unexpected request %s", r.URL)
		}
		_, _ = w.Write([]byte(`[
		 {"change":"INJECTED","updateAdded":{"vrf":"default","externalPeer":"192.0.2.1","prefix":"203.0.113.0/24","type":"EBGP","nextHop":"192.0.2.1","origin":"IGP","asPath":[64500]}},
		 {"change":"WITHDRAWN","original":{"vrf":"default","externalPeer":"192.0.2.2","prefix":"198.51.100.0/24","type":"EBGP","nextHop":"192.0.2.2","origin":"IGP","asPath":[64501]},"updateRemoved":{"vrf":"default","externalPeer":"192.0.2.2","prefix":"198.51.100.0/24"}},
		 {"change":"ABSENT","updateRemoved":{"vrf":"default","externalPeer":"192.0.2.3","prefix":"192.0.2.99/32"}}
		]`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	stage := []BGPAdvertisement{{VRF: "default", ExternalPeer: "192.0.2.1", Prefix: "203.0.113.0/24"}}

	report, _, err := client.Predict.VerifyBGPStaging(context.Background(), "n1", "c1", "edge", stage,
		[]BGPAdvertisementKey{{VRF: "default", ExternalPeer: "192.0.2.2", Prefix: "198.51.100.0/24"}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Staged[0].Change != BGPChangeInjected || report.Withdrawn[0].Change != BGPChangeWithdrawn {
		t.Fatalf("report = %+v", report)
	}

	// A bare address names the same key as its /32 host route.
	report, _, err = client.Predict.VerifyBGPStaging(context.Background(), "n1", "c1", "edge", nil,
		[]BGPAdvertisementKey{{VRF: "default", ExternalPeer: "192.0.2.3", Prefix: "192.0.2.99"}})
	if !errors.Is(err, ErrBGPKeyMatchedNothing) || !strings.Contains(err.Error(), "192.0.2.3") {
		t.Fatalf("err = %v", err)
	}
	if report == nil || !report.Withdrawn[0].Found || report.Withdrawn[0].Change != BGPChangeAbsent {
		t.Fatalf("report = %+v", report)
	}

	// A key with no row at all means the stage is not in the draft.
	_, _, err = client.Predict.VerifyBGPStaging(context.Background(), "n1", "c1", "edge",
		[]BGPAdvertisement{{VRF: "default", ExternalPeer: "192.0.2.9", Prefix: "203.0.113.0/24"}}, nil)
	if !errors.Is(err, ErrBGPKeyMatchedNothing) {
		t.Fatalf("err = %v", err)
	}

	// A key both staged and withdrawn has two rows; each must be matched to its own kind, never one for the other.
	both := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[
		 {"change":"INJECTED","updateAdded":{"vrf":"default","externalPeer":"192.0.2.1","prefix":"203.0.113.0/24","type":"EBGP","nextHop":"192.0.2.1","origin":"IGP","asPath":[64500]}},
		 {"change":"ABSENT","updateRemoved":{"vrf":"default","externalPeer":"192.0.2.1","prefix":"203.0.113.0/24"}}
		]`))
	}))
	defer both.Close()
	key := BGPAdvertisementKey{VRF: "default", ExternalPeer: "192.0.2.1", Prefix: "203.0.113.0/24"}
	report, _, err = newTestClient(t, both.URL).Predict.VerifyBGPStaging(context.Background(), "n1", "c1", "edge",
		[]BGPAdvertisement{{VRF: key.VRF, ExternalPeer: key.ExternalPeer, Prefix: key.Prefix}}, []BGPAdvertisementKey{key})
	if !errors.Is(err, ErrBGPKeyMatchedNothing) || report.Staged[0].Change != BGPChangeInjected || report.Withdrawn[0].Change != BGPChangeAbsent {
		t.Fatalf("report = %+v err = %v", report, err)
	}
}
