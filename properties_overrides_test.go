package forward

import (
	"context"
	"io"
	"net/http"
	"testing"
)

// A nil value is how Forward is told to delete a network's override (patchNetworkConfigOverrides partitions on null), so it must reach the
// wire as JSON null, not be dropped; an empty map must send nothing at all.
func TestPatchNetworkOverridesSendsNullToClear(t *testing.T) {
	var method, path, body string
	hits := 0
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
		method, path = r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.WriteHeader(http.StatusNoContent)
	})
	ctx := context.Background()
	if _, err := c.Properties.PatchNetworkOverrides(ctx, "some_net_prop", nil); err != nil || hits != 0 {
		t.Fatalf("empty patch: err %v, %d requests", err, hits)
	}
	if _, err := c.Properties.PatchNetworkOverrides(ctx, "some_net_prop", map[string]any{"n1": true, "n2": nil}); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPatch || path != "/api/config/some_net_prop/overrides" || body != `{"n1":true,"n2":null}` {
		t.Fatalf("got %s %s %s", method, path, body)
	}
}

// A blank network id must be refused before anything is sent: a server that accepts everything would otherwise hide a missing check.
func TestPatchNetworkOverridesRefusesBlankID(t *testing.T) {
	hits := 0
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) { hits++; w.WriteHeader(http.StatusNoContent) })
	if _, err := c.Properties.PatchNetworkOverrides(context.Background(), "p", map[string]any{" ": true}); err == nil || hits != 0 {
		t.Fatalf("err %v, %d requests", err, hits)
	}
}
