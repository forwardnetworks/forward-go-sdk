package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Omitting the snapshot must send no parameter so the appserver applies its own default; stating one must send it.
func TestSecurityZones(t *testing.T) {
	t.Parallel()

	var uri string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uri = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"fw1":["inside","outside"],"fw2":[]}`)
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)

	got, _, err := c.Networks.SecurityZones(context.Background(), "n1", "")
	if err != nil || uri != "/api/networks/n1/security-zones" || len(got["fw1"]) != 2 || got["fw2"] == nil {
		t.Errorf("latest: %s %v %v", uri, got, err)
	}
	if _, _, err := c.Networks.SecurityZones(context.Background(), "n1", "s 1"); err != nil || uri != "/api/networks/n1/security-zones?snapshotId=s+1" {
		t.Errorf("snapshot: %s %v", uri, err)
	}
}
