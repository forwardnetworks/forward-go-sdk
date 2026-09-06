package forward

import (
	"context"
	"net/http"
	"testing"
)

// An empty name must never collapse onto the collection route. A PUT there
// replaces every synthetic node of that kind, and a DELETE there is worse --
// both are one missing string away from wiping a network's synthetic devices.
func TestSyntheticNodeNameCannotEscapeToTheCollection(t *testing.T) {
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no request should be sent, got %s %s", r.Method, r.URL.Path)
	})
	ctx := context.Background()
	for _, kind := range []SyntheticNodeKind{SyntheticIntranet, SyntheticL3VPN} {
		if _, _, err := c.SyntheticNodes.Get(ctx, "net-1", kind, "  "); err == nil {
			t.Errorf("%s: an empty name must be refused on get", kind)
		}
		if _, err := c.SyntheticNodes.Put(ctx, "net-1", kind, "", SyntheticNode{}); err == nil {
			t.Errorf("%s: an empty name must be refused on put", kind)
		}
		if _, err := c.SyntheticNodes.Delete(ctx, "net-1", kind, ""); err == nil {
			t.Errorf("%s: an empty name must be refused on delete", kind)
		}
	}
	// The internet node is one per network and has no delete route at all.
	if _, err := c.SyntheticNodes.Delete(ctx, "net-1", SyntheticInternet, ""); err == nil {
		t.Error("the internet node must not be deletable")
	}
}

// Absence is not an error on a read: the caller reads precisely to learn
// whether it must create.
func TestSyntheticNodeGetTreatsAbsenceAsAbsence(t *testing.T) {
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	got, _, err := c.SyntheticNodes.Get(context.Background(), "net-1", SyntheticL3VPN, "vpn-a")
	if err != nil {
		t.Fatalf("a 404 must not be an error: %v", err)
	}
	if got != nil {
		t.Fatalf("a 404 must yield no node, got %+v", got)
	}
}

// Forward returns these as an ARRAY under a per-kind key, even though the
// Java field behind it is a map keyed by name. Its own L3VpnListTest pins the
// serialized form as {"l3Vpns": [...]}.
func TestSyntheticNodeListAcceptsForwardsEnvelopes(t *testing.T) {
	for name, tc := range map[string]struct {
		kind SyntheticNodeKind
		body string
	}{
		"l3 vpns keyed":  {SyntheticL3VPN, `{"l3Vpns":[{"name":"a"},{"name":"b"}]}`},
		"intranet keyed": {SyntheticIntranet, `{"intranetNodes":[{"name":"a"},{"name":"b"}]}`},
		"bare array":     {SyntheticL3VPN, `[{"name":"a"},{"name":"b"}]`},
	} {
		c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(tc.body))
		})
		got, _, err := c.SyntheticNodes.List(context.Background(), "net-1", tc.kind)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(got) != 2 || got[0].Name != "a" {
			t.Fatalf("%s: got %+v", name, got)
		}
	}

	// The internet node has no collection; listing it is empty, not an error.
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the internet node has no collection route")
	})
	got, _, err := c.SyntheticNodes.List(context.Background(), "net-1", SyntheticInternet)
	if err != nil || got != nil {
		t.Fatalf("internet list must be empty and non-error: %v %v", got, err)
	}
}
