package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Forward's Predict does model withdrawal (the SDK comment used to say it did
// not): a received advertisement is withdrawn by its key alone, and a staged
// injection, modification or withdrawal is undone with restore. Shutting a
// neighbor with a CLI command removes the session but not the routes learned
// from it, so a de-peering what-if has to withdraw the advertisements. The wire
// is the same route as inject with a different action, a key-only body (single
// object or array), and nothing is sent for a key Forward would refuse.
func TestPredictBGPWithdrawAndRestore(t *testing.T) {
	t.Parallel()

	var calls, bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		bodies = append(bodies, string(body))
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	p := client.Predict
	ctx := context.Background()
	key := BGPAdvertisementKey{VRF: "default", ExternalPeer: "4.16.225.165", Prefix: "0.0.0.0/0"}
	keys := []BGPAdvertisementKey{key, {VRF: "default", ExternalPeer: "4.16.225.165", Prefix: "10.0.0.0/8"}}

	if _, err := p.WithdrawBGPAdvertisement(ctx, "205507", "cs1", "ldc31-rtedg-1", key); err != nil {
		t.Fatal(err)
	}
	if _, err := p.WithdrawBGPAdvertisements(ctx, "205507", "cs1", "ldc31-rtedg-1", keys); err != nil {
		t.Fatal(err)
	}
	if _, err := p.RestoreBGPAdvertisement(ctx, "205507", "cs1", "ldc31-rtedg-1", key); err != nil {
		t.Fatal(err)
	}
	if _, err := p.RestoreBGPAdvertisements(ctx, "205507", "cs1", "ldc31-rtedg-1", keys); err != nil {
		t.Fatal(err)
	}
	base := "POST /api/networks/205507/change-sets/cs1/draft/devices/ldc31-rtedg-1/bgp-advertisements?action="
	wantCalls := []string{base + "remove", base + "bulkRemove", base + "restore", base + "bulkRestore"}
	one := `{"vrf":"default","externalPeer":"4.16.225.165","prefix":"0.0.0.0/0"}`
	wantBodies := []string{one, "[" + one + `,{"vrf":"default","externalPeer":"4.16.225.165","prefix":"10.0.0.0/8"}]`, one, "[" + one + `,{"vrf":"default","externalPeer":"4.16.225.165","prefix":"10.0.0.0/8"}]`}
	if len(calls) != len(wantCalls) {
		t.Fatalf("calls %v", calls)
	}
	for i := range wantCalls {
		if calls[i] != wantCalls[i] || bodies[i] != wantBodies[i] {
			t.Errorf("call %d = %s %s, want %s %s", i, calls[i], bodies[i], wantCalls[i], wantBodies[i])
		}
	}

	sent := len(calls)
	for name, call := range map[string]func() error{
		"no vrf": func() error {
			_, err := p.WithdrawBGPAdvertisement(ctx, "5", "cs1", "d", BGPAdvertisementKey{ExternalPeer: "1.1.1.1", Prefix: "0.0.0.0/0"})
			return err
		},
		"bad peer": func() error {
			_, err := p.WithdrawBGPAdvertisement(ctx, "5", "cs1", "d", BGPAdvertisementKey{VRF: "default", ExternalPeer: "peer", Prefix: "0.0.0.0/0"})
			return err
		},
		"bad prefix": func() error {
			_, err := p.RestoreBGPAdvertisement(ctx, "5", "cs1", "d", BGPAdvertisementKey{VRF: "default", ExternalPeer: "1.1.1.1", Prefix: "ten"})
			return err
		},
		"host bits": func() error {
			_, err := p.WithdrawBGPAdvertisement(ctx, "5", "cs1", "d", BGPAdvertisementKey{VRF: "default", ExternalPeer: "1.1.1.1", Prefix: "10.0.0.1/8"})
			return err
		},
		"duplicate key": func() error {
			_, err := p.WithdrawBGPAdvertisements(ctx, "5", "cs1", "d", []BGPAdvertisementKey{key, key})
			return err
		},
		"no keys": func() error { _, err := p.WithdrawBGPAdvertisements(ctx, "5", "cs1", "d", nil); return err },
		"one bad": func() error {
			_, err := p.RestoreBGPAdvertisements(ctx, "5", "cs1", "d", append(keys, BGPAdvertisementKey{}))
			return err
		},
		"no device": func() error { _, err := p.WithdrawBGPAdvertisement(ctx, "5", "cs1", " ", key); return err },
	} {
		if call() == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if len(calls) != sent {
		t.Fatal("a refused withdrawal reached Forward")
	}

	// A host route is written by Forward as a bare address; it must be accepted, and fields are trimmed before they are sent.
	if _, err := p.WithdrawBGPAdvertisement(ctx, "5", "cs1", "d", BGPAdvertisementKey{VRF: " default ", ExternalPeer: " 4.16.225.165", Prefix: "10.0.0.1 "}); err != nil {
		t.Fatalf("a host route key must be accepted: %v", err)
	}
	if got := bodies[len(bodies)-1]; got != `{"vrf":"default","externalPeer":"4.16.225.165","prefix":"10.0.0.1"}` {
		t.Fatalf("host route body %s", got)
	}
}

// Staging the same key and peer type twice in one bulk call is stored as given and then breaks the run, so the SDK refuses it.
func TestStageBGPAdvertisementsRefusesADuplicateKey(t *testing.T) {
	t.Parallel()

	var sent bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent = true
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ad := BGPAdvertisement{VRF: "default", ExternalPeer: "192.0.2.1", Prefix: "10.0.0.0/8", NextHop: "192.0.2.1", Type: "EBGP", Origin: "IGP", ASPath: []int64{65001}}
	if _, err := client.Predict.StageBGPAdvertisements(context.Background(), "5", "cs1", "d", []BGPAdvertisement{ad, ad}); err == nil || sent {
		t.Fatalf("a repeated key must be refused before the request: err=%v sent=%v", err, sent)
	}
	ibgp := ad
	ibgp.Type = "IBGP"
	if _, err := client.Predict.StageBGPAdvertisements(context.Background(), "5", "cs1", "d", []BGPAdvertisement{ad, ibgp}); err != nil {
		t.Fatalf("the same key with a different peer type is allowed: %v", err)
	}
}
